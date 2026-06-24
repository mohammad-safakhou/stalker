package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mohammad-safakhou/stalker/internal/store"
)

func TestPlanRoutesOpenAIResponsesToOpenAIByDefault(t *testing.T) {
	req := newRequest(t, "POST", "http://127.0.0.1:8080/v1/responses?stream=true")

	plan := (&Proxy{}).plan(req)

	if plan.name != "openai-responses" {
		t.Fatalf("plan.name = %q, want openai-responses", plan.name)
	}
	if got, want := plan.target.String(), "https://api.openai.com/v1/responses?stream=true"; got != want {
		t.Fatalf("plan.target = %q, want %q", got, want)
	}
	if plan.chatGPTAuth {
		t.Fatal("plan.chatGPTAuth = true, want false")
	}
}

func TestPlanRoutesChatGPTCodexResponsesByExplicitAccountHeader(t *testing.T) {
	req := newRequest(t, "POST", "http://127.0.0.1:8080/v1/responses/compact?trace=1")
	req.Header.Set("ChatGPT-Account-ID", "acct-explicit")

	plan := (&Proxy{}).plan(req)

	if plan.name != "chatgpt-codex-responses-subpath" {
		t.Fatalf("plan.name = %q, want chatgpt-codex-responses-subpath", plan.name)
	}
	if got, want := plan.target.String(), "https://chatgpt.com/backend-api/codex/responses/compact?trace=1"; got != want {
		t.Fatalf("plan.target = %q, want %q", got, want)
	}
	if !plan.chatGPTAuth || plan.accountIDFrom != "acct-explicit" {
		t.Fatalf("chatgpt auth = (%v, %q), want (true, acct-explicit)", plan.chatGPTAuth, plan.accountIDFrom)
	}
}

func TestPlanRoutesChatGPTCodexResponsesByOAuthJWT(t *testing.T) {
	req := newRequest(t, "POST", "http://127.0.0.1:8080/v1/codex/responses")
	req.Header.Set("Authorization", "Bearer "+jwt(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-from-jwt",
		},
	}))

	plan := (&Proxy{}).plan(req)

	if plan.name != "chatgpt-codex-responses" {
		t.Fatalf("plan.name = %q, want chatgpt-codex-responses", plan.name)
	}
	if got, want := plan.target.String(), "https://chatgpt.com/backend-api/codex/responses"; got != want {
		t.Fatalf("plan.target = %q, want %q", got, want)
	}
	if !plan.chatGPTAuth || plan.accountIDFrom != "acct-from-jwt" {
		t.Fatalf("chatgpt auth = (%v, %q), want (true, acct-from-jwt)", plan.chatGPTAuth, plan.accountIDFrom)
	}
}

func TestPlanRoutesModelsForChatGPTAuthToCodexRegistry(t *testing.T) {
	req := newRequest(t, "GET", "http://127.0.0.1:8080/v1/models?client_version=0.140.0")
	req.Header.Set("ChatGPT-Account-ID", "acct-explicit")

	plan := (&Proxy{}).plan(req)

	if plan.name != "chatgpt-codex-models" {
		t.Fatalf("plan.name = %q, want chatgpt-codex-models", plan.name)
	}
	if got, want := plan.target.String(), "https://chatgpt.com/backend-api/codex/models?client_version=0.140.0"; got != want {
		t.Fatalf("plan.target = %q, want %q", got, want)
	}
}

func TestPlanRoutesBackendAndEvents(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{
			url:  "http://127.0.0.1:8080/backend-api/conversation",
			want: "https://chatgpt.com/backend-api/conversation",
		},
		{
			url:  "http://127.0.0.1:8080/events?foo=bar",
			want: "https://chatgpt.com/backend-api/events?foo=bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			req := newRequest(t, "GET", tt.url)
			plan := (&Proxy{}).plan(req)
			if got := plan.target.String(); got != tt.want {
				t.Fatalf("plan.target = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveChatGPTAuthIgnoresRegularOpenAIBearer(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+jwt(t, map[string]any{
		"aud": []string{"https://api.openai.com/v1"},
	}))

	accountID, ok := resolveChatGPTAuth(headers)

	if ok || accountID != "" {
		t.Fatalf("resolveChatGPTAuth() = (%q, %v), want empty false", accountID, ok)
	}
}

func TestIsUpgradeResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Header:     http.Header{"Upgrade": []string{"websocket"}},
	}
	if !isUpgradeResponse(resp) {
		t.Fatal("isUpgradeResponse() = false, want true")
	}
}

func TestIsWebSocketRequest(t *testing.T) {
	req := newRequest(t, "GET", "http://127.0.0.1:8080/v1/responses")
	req.Header.Set("Connection", "keep-alive, Upgrade")
	req.Header.Set("Upgrade", "websocket")

	if !isWebSocketRequest(req) {
		t.Fatal("isWebSocketRequest() = false, want true")
	}
}

func TestWebSocketTargetURL(t *testing.T) {
	target, err := url.Parse("https://chatgpt.com/backend-api/codex/responses?stream=true")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := websocketTargetURL(target), "wss://chatgpt.com/backend-api/codex/responses?stream=true"; got != want {
		t.Fatalf("websocketTargetURL() = %q, want %q", got, want)
	}
}

func TestFormatWebSocketFrame(t *testing.T) {
	raw := formatWebSocketFrame(websocket.TextMessage, []byte(`{"input":"hello"}`))

	if !strings.Contains(string(raw), `"opcode":"text"`) {
		t.Fatalf("frame = %s, want text opcode", raw)
	}
	if !strings.Contains(string(raw), `"text":"{\"input\":\"hello\"}"`) {
		t.Fatalf("frame = %s, want escaped text payload", raw)
	}
}

func TestProxyRelaysAndCapturesWebSocketFrames(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade: %v", err)
			return
		}
		defer conn.Close()

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("upstream read: %v", err)
			return
		}
		if !strings.Contains(string(payload), "ping") {
			t.Errorf("upstream payload = %q, want ping", payload)
		}
		if err := conn.WriteMessage(messageType, []byte(`{"output":[{"role":"assistant","content":"pong"}]}`)); err != nil {
			t.Errorf("upstream write: %v", err)
		}
	}))
	defer upstream.Close()

	previousCodex := chatGPTCodex
	chatGPTCodex = mustParseURL(upstream.URL + "/backend-api/codex")
	defer func() { chatGPTCodex = previousCodex }()

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	proxyServer := httptest.NewServer(&Proxy{Store: s})
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL.Scheme = "ws"
	proxyURL.Path = "/v1/responses"

	client, _, err := websocket.DefaultDialer.Dial(proxyURL.String(), http.Header{
		"ChatGPT-Account-ID": []string{"acct-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMessage(websocket.TextMessage, []byte(`{"input":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "pong") {
		t.Fatalf("client payload = %q, want pong", payload)
	}

	var rows []store.Exchange
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err = s.List(context.Background(), store.ListFilter{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("captured rows = %d, want 1", len(rows))
	}
	if rows[0].StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", rows[0].StatusCode)
	}
	if rows[0].RequestPreview != "" || rows[0].ResponsePreview != "" {
		t.Fatalf("previews = (%q, %q), want no retained payload data", rows[0].RequestPreview, rows[0].ResponsePreview)
	}
	report, err := s.TokenReport(context.Background(), rows[0].ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 2 {
		t.Fatalf("token runs = %d, want request and response", len(report.Runs))
	}
	totals, err := s.TokenTotals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if totals.InputTokens == 0 || totals.OutputTokens == 0 {
		t.Fatalf("token totals = %+v, want non-zero input and output", totals)
	}
	_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	_ = client.Close()
}

func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	return httptest.NewRequest(method, target, io.Reader(nil))
}

func jwt(t *testing.T, payload map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "none", "typ": "JWT"}
	return encodeJWTPart(t, header) + "." + encodeJWTPart(t, payload) + "."
}

func encodeJWTPart(t *testing.T, part map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(part)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
