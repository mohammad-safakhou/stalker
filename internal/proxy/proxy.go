package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/mohammad-safakhou/stalker/internal/store"
)

var (
	openAIBase         = mustParseURL(env("OPENAI_BASE_URL", "https://api.openai.com/v1"))
	chatGPTBackend     = mustParseURL(env("CHATGPT_BACKEND_URL", "https://chatgpt.com/backend-api"))
	chatGPTCodex       = mustParseURL(env("CHATGPT_CODEX_URL", "https://chatgpt.com/backend-api/codex"))
	defaultUpstreamURL = mustParseURL(env("DEFAULT_BASE_URL", openAIBase.String()))
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		log.Fatalf("invalid upstream URL %q: %v", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		log.Fatalf("upstream URL must include scheme and host: %q", raw)
	}
	return u
}

func Start(addr string, s *store.Store) {
	log.Printf("Proxy starting on %s", addr)
	log.Printf("  OpenAI API      -> %s", openAIBase)
	log.Printf("  ChatGPT backend -> %s", chatGPTBackend)
	log.Printf("  Codex backend   -> %s", chatGPTCodex)
	log.Printf("  default         -> %s", defaultUpstreamURL)

	server := &http.Server{
		Addr:              addr,
		Handler:           &Proxy{Store: s},
		ReadHeaderTimeout: 30 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("proxy server error: %v", err)
	}
}

type Proxy struct {
	Store *store.Store
}

type routePlan struct {
	name          string
	target        *url.URL
	chatGPTAuth   bool
	accountIDFrom string
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	plan := p.plan(r)
	var capture *store.Capture

	if p.Store != nil {
		var err error
		capture, err = p.Store.NewCapture(store.CaptureMeta{
			Method:      r.Method,
			Path:        r.URL.Path,
			Query:       r.URL.RawQuery,
			Route:       plan.name,
			TargetURL:   plan.target.String(),
			ChatGPTAuth: plan.chatGPTAuth,
			Headers:     r.Header,
		})
		if err != nil {
			log.Printf("capture setup failed: %v", err)
		} else if r.Body != nil {
			r.Body = capture.RequestBody(r.Body)
		}
	}

	log.Printf(
		">>> %s %s -> %s (%s, chatgpt_auth=%v, account_id=%s, upgrade=%q)",
		r.Method,
		r.URL.RequestURI(),
		plan.target.String(),
		plan.name,
		plan.chatGPTAuth,
		redact(plan.accountIDFrom),
		r.Header.Get("Upgrade"),
	)

	if isWebSocketRequest(r) {
		p.serveWebSocket(w, r, plan, capture, start)
		return
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = plan.target.Scheme
			req.URL.Host = plan.target.Host
			req.URL.Path = plan.target.Path
			req.URL.RawPath = plan.target.RawPath
			req.URL.RawQuery = plan.target.RawQuery
			req.URL.ForceQuery = plan.target.ForceQuery
			req.Host = plan.target.Host
			req.Header.Del("Host")
			if plan.chatGPTAuth && plan.accountIDFrom != "" && req.Header.Get("ChatGPT-Account-ID") == "" {
				req.Header.Set("ChatGPT-Account-ID", plan.accountIDFrom)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			isStream := isStreamingResponse(resp)
			log.Printf("<<< %d %s (stream=%v)", resp.StatusCode, r.URL.Path, isStream)
			if isUpgradeResponse(resp) {
				if capture != nil {
					capture.ResponseMeta(resp.StatusCode, resp.Header, true)
				}
				return nil
			}
			if capture != nil {
				resp.Body = capture.ResponseBody(resp.Body, resp.StatusCode, resp.Header, isStream)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			log.Printf("!!! %s %s proxy error: %v", req.Method, req.URL.RequestURI(), err)
			if capture != nil {
				_ = capture.Save(err.Error())
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
		FlushInterval: -1,
	}

	rp.ServeHTTP(w, r)
	if capture != nil {
		log.Printf("  captured in %s", time.Since(start).Round(time.Millisecond))
	}
}

func (p *Proxy) serveWebSocket(w http.ResponseWriter, r *http.Request, plan routePlan, capture *store.Capture, start time.Time) {
	dialer := websocket.Dialer{
		Proxy:             http.ProxyFromEnvironment,
		HandshakeTimeout:  30 * time.Second,
		EnableCompression: true,
		Subprotocols:      websocket.Subprotocols(r),
	}

	target := websocketTargetURL(plan.target)
	upstream, resp, err := dialer.Dial(target, websocketDialHeaders(r.Header, plan))
	if err != nil {
		status := http.StatusBadGateway
		if resp != nil && resp.StatusCode > 0 {
			status = resp.StatusCode
		}
		log.Printf("!!! %s %s websocket dial error: %v", r.Method, r.URL.RequestURI(), err)
		if capture != nil {
			if resp != nil {
				capture.ResponseMeta(resp.StatusCode, resp.Header, true)
			}
			_ = capture.Save(err.Error())
		}
		http.Error(w, err.Error(), status)
		return
	}
	defer upstream.Close()

	if capture != nil {
		headers := http.Header{}
		if resp != nil {
			headers = resp.Header
		}
		capture.ResponseMeta(http.StatusSwitchingProtocols, headers, true)
	}

	subprotocols := []string(nil)
	if subprotocol := upstream.Subprotocol(); subprotocol != "" {
		subprotocols = []string{subprotocol}
	}
	upgrader := websocket.Upgrader{
		CheckOrigin:       func(*http.Request) bool { return true },
		EnableCompression: true,
		Subprotocols:      subprotocols,
	}
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("!!! %s %s websocket upgrade error: %v", r.Method, r.URL.RequestURI(), err)
		if capture != nil {
			_ = capture.Save(err.Error())
		}
		return
	}
	defer client.Close()

	log.Printf("<<< 101 %s (websocket)", r.URL.Path)
	errc := make(chan error, 2)
	go relayWebSocket(client, upstream, "request", capture, errc)
	go relayWebSocket(upstream, client, "response", capture, errc)

	err = <-errc
	_ = client.Close()
	_ = upstream.Close()
	select {
	case <-errc:
	case <-time.After(2 * time.Second):
	}

	errorMessage := ""
	if err != nil && !isNormalWebSocketClose(err) {
		errorMessage = err.Error()
		log.Printf("!!! %s %s websocket relay ended: %v", r.Method, r.URL.RequestURI(), err)
	}
	if capture != nil {
		_ = capture.Save(errorMessage)
		log.Printf("  captured websocket in %s", time.Since(start).Round(time.Millisecond))
	}
}

func relayWebSocket(src, dst *websocket.Conn, side string, capture *store.Capture, errc chan<- error) {
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			errc <- err
			return
		}
		frame := formatWebSocketFrame(messageType, payload)
		if capture != nil {
			if side == "request" {
				capture.WriteRequest(frame)
			} else {
				capture.WriteResponse(frame)
			}
		}
		if err := dst.WriteMessage(messageType, payload); err != nil {
			errc <- err
			return
		}
	}
}

func websocketTargetURL(target *url.URL) string {
	u := *target
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	return u.String()
}

func websocketDialHeaders(headers http.Header, plan routePlan) http.Header {
	out := make(http.Header, len(headers))
	for key, values := range headers {
		if skipWebSocketDialHeader(key) {
			continue
		}
		for _, value := range values {
			out.Add(key, value)
		}
	}
	if plan.chatGPTAuth && plan.accountIDFrom != "" && out.Get("ChatGPT-Account-ID") == "" {
		out.Set("ChatGPT-Account-ID", plan.accountIDFrom)
	}
	return out
}

func skipWebSocketDialHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "host", "proxy-connection", "sec-websocket-accept",
		"sec-websocket-extensions", "sec-websocket-key", "sec-websocket-protocol",
		"sec-websocket-version", "upgrade":
		return true
	default:
		return false
	}
}

func formatWebSocketFrame(messageType int, payload []byte) []byte {
	frame := map[string]any{
		"ts":     time.Now().UTC().Format(time.RFC3339Nano),
		"opcode": websocketOpcode(messageType),
		"bytes":  len(payload),
	}
	if messageType == websocket.TextMessage && utf8.Valid(payload) {
		frame["text"] = string(payload)
	} else {
		frame["base64"] = base64.StdEncoding.EncodeToString(payload)
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		return []byte(fmt.Sprintf(`{"opcode":"%s","bytes":%d,"error":"%s"}`+"\n", websocketOpcode(messageType), len(payload), err))
	}
	return append(raw, '\n')
}

func websocketOpcode(messageType int) string {
	switch messageType {
	case websocket.TextMessage:
		return "text"
	case websocket.BinaryMessage:
		return "binary"
	case websocket.CloseMessage:
		return "close"
	case websocket.PingMessage:
		return "ping"
	case websocket.PongMessage:
		return "pong"
	default:
		return fmt.Sprintf("opcode-%d", messageType)
	}
}

func isNormalWebSocketClose(err error) bool {
	if err == nil {
		return true
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

func (p *Proxy) plan(r *http.Request) routePlan {
	accountID, chatGPTAuth := resolveChatGPTAuth(r.Header)
	path := r.URL.Path

	switch {
	case path == "/v1/responses" || path == "/v1/codex/responses" ||
		path == "/backend-api/responses" || path == "/backend-api/codex/responses":
		if chatGPTAuth {
			return p.route("chatgpt-codex-responses", chatGPTCodex, "/responses", r.URL, chatGPTAuth, accountID)
		}
		return p.route("openai-responses", openAIBase, "/responses", r.URL, chatGPTAuth, accountID)

	case hasPrefixPath(path, "/v1/responses") || hasPrefixPath(path, "/v1/codex/responses") ||
		hasPrefixPath(path, "/backend-api/responses") || hasPrefixPath(path, "/backend-api/codex/responses"):
		subPath := codexResponsesSubPath(path)
		if chatGPTAuth {
			return p.route("chatgpt-codex-responses-subpath", chatGPTCodex, "/responses"+subPath, r.URL, chatGPTAuth, accountID)
		}
		return p.route("openai-responses-subpath", openAIBase, "/responses"+subPath, r.URL, chatGPTAuth, accountID)

	case path == "/v1/models" || hasPrefixPath(path, "/v1/models"):
		suffix := strings.TrimPrefix(path, "/v1/models")
		if chatGPTAuth {
			return p.route("chatgpt-codex-models", chatGPTCodex, "/models"+suffix, r.URL, chatGPTAuth, accountID)
		}
		return p.route("openai-models", openAIBase, "/models"+suffix, r.URL, chatGPTAuth, accountID)

	case path == "/v1/images" || hasPrefixPath(path, "/v1/images"):
		suffix := strings.TrimPrefix(path, "/v1/images")
		if chatGPTAuth {
			return p.route("chatgpt-codex-images", chatGPTCodex, "/images"+suffix, r.URL, chatGPTAuth, accountID)
		}
		return p.route("openai-images", openAIBase, "/images"+suffix, r.URL, chatGPTAuth, accountID)

	case path == "/v1" || hasPrefixPath(path, "/v1"):
		suffix := strings.TrimPrefix(path, "/v1")
		return p.route("openai-v1", openAIBase, suffix, r.URL, chatGPTAuth, accountID)

	case path == "/backend-api/codex" || hasPrefixPath(path, "/backend-api/codex"):
		suffix := strings.TrimPrefix(path, "/backend-api/codex")
		return p.route("chatgpt-codex", chatGPTCodex, suffix, r.URL, chatGPTAuth, accountID)

	case path == "/backend-api" || hasPrefixPath(path, "/backend-api"):
		suffix := strings.TrimPrefix(path, "/backend-api")
		return p.route("chatgpt-backend", chatGPTBackend, suffix, r.URL, chatGPTAuth, accountID)

	case path == "/events" || hasPrefixPath(path, "/events"):
		return p.route("chatgpt-events", chatGPTBackend, path, r.URL, chatGPTAuth, accountID)

	default:
		return p.route("default", defaultUpstreamURL, path, r.URL, chatGPTAuth, accountID)
	}
}

func (p *Proxy) route(name string, base *url.URL, path string, original *url.URL, chatGPTAuth bool, accountID string) routePlan {
	target := *base
	target.Path = joinPaths(base.Path, path)
	target.RawPath = ""
	target.RawQuery = original.RawQuery
	target.ForceQuery = original.ForceQuery
	return routePlan{
		name:          name,
		target:        &target,
		chatGPTAuth:   chatGPTAuth,
		accountIDFrom: accountID,
	}
}

func hasPrefixPath(path, prefix string) bool {
	prefix = strings.TrimRight(prefix, "/")
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func codexResponsesSubPath(path string) string {
	for _, prefix := range []string{
		"/backend-api/codex/responses",
		"/backend-api/responses",
		"/v1/codex/responses",
		"/v1/responses",
	} {
		if path == prefix {
			return ""
		}
		if strings.HasPrefix(path, prefix+"/") {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return ""
}

func joinPaths(basePath, suffix string) string {
	if basePath == "" {
		if suffix == "" {
			return "/"
		}
		return suffix
	}
	if suffix == "" || suffix == "/" {
		return basePath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(suffix, "/")
}

func resolveChatGPTAuth(headers http.Header) (string, bool) {
	if accountID := headers.Get("ChatGPT-Account-ID"); strings.TrimSpace(accountID) != "" {
		return strings.TrimSpace(accountID), true
	}
	accountID := accountIDFromBearerJWT(headers.Get("Authorization"))
	if accountID != "" {
		return accountID, true
	}
	return "", false
}

func accountIDFromBearerJWT(auth string) string {
	scheme, token, ok := strings.Cut(auth, " ")
	if !ok || !strings.EqualFold(scheme, "bearer") || strings.Count(token, ".") < 2 {
		return ""
	}

	parts := strings.SplitN(token, ".", 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return ""
	}
	claims, ok := data["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	accountID, ok := claims["chatgpt_account_id"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(accountID)
}

func redact(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 6 {
		return "***"
	}
	return value[:3] + "..." + value[len(value)-3:]
}

func isStreamingResponse(resp *http.Response) bool {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.Contains(contentType, "text/event-stream") ||
		strings.Contains(contentType, "application/x-ndjson") ||
		resp.ContentLength == -1
}

func isWebSocketRequest(r *http.Request) bool {
	return headerHasToken(r.Header, "Connection", "upgrade") &&
		strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

func headerHasToken(headers http.Header, key, token string) bool {
	for _, value := range headers.Values(key) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func isUpgradeResponse(resp *http.Response) bool {
	return resp.StatusCode == http.StatusSwitchingProtocols ||
		strings.EqualFold(resp.Header.Get("Upgrade"), "websocket")
}
