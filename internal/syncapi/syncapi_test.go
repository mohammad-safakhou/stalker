package syncapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohammad-safakhou/stalker/internal/store"
)

func TestSnapshotReturnsAggregateOnlyPayload(t *testing.T) {
	s := testStoreWithCapture(t)
	defer s.Close()

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18080/api/v1/sync/snapshot", nil)
	rec := httptest.NewRecorder()

	New(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{
		"request_headers",
		"response_headers",
		"request_preview",
		"response_preview",
		"request_body_path",
		"response_body_path",
		"secret prompt",
		"secret response",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("sync snapshot leaked %q in body: %s", forbidden, body)
		}
	}

	var snapshot store.SyncSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Device.ID == "" {
		t.Fatal("device id is empty")
	}
	if snapshot.Totals.InputTokens == 0 || snapshot.Totals.OutputTokens == 0 {
		t.Fatalf("totals = %+v, want non-zero input and output", snapshot.Totals)
	}
	if len(snapshot.Hourly) == 0 || len(snapshot.Daily) == 0 {
		t.Fatalf("buckets = hourly %d daily %d, want both populated", len(snapshot.Hourly), len(snapshot.Daily))
	}
}

func TestStreamReturnsSnapshotEvent(t *testing.T) {
	s := testStoreWithCapture(t)
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18080/api/v1/sync/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		New(s).ServeHTTP(rec, req)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "data:") {
			cancel()
			<-done
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
				t.Fatalf("content-type = %q, want text/event-stream", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("stream body = %q, want data event", rec.Body.String())
}

func testStoreWithCapture(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	capture, err := s.NewCapture(store.CaptureMeta{
		Method:    "POST",
		Path:      "/v1/responses",
		Route:     "openai-responses",
		TargetURL: "https://api.openai.com/v1/responses",
		Headers:   http.Header{"Content-Type": []string{"application/json"}, "Authorization": []string{"Bearer secret-token"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqBody := capture.RequestBody(io.NopCloser(strings.NewReader(`{"input":"secret prompt alpha alpha"}`)))
	if _, err := io.Copy(io.Discard, reqBody); err != nil {
		t.Fatal(err)
	}
	if err := reqBody.Close(); err != nil {
		t.Fatal(err)
	}
	respBody := capture.ResponseBody(
		io.NopCloser(strings.NewReader(`{"output":[{"content":"secret response beta"}]}`)),
		http.StatusOK,
		http.Header{"Content-Type": []string{"application/json"}},
		false,
	)
	if _, err := io.Copy(io.Discard, respBody); err != nil {
		t.Fatal(err)
	}
	if err := respBody.Close(); err != nil {
		t.Fatal(err)
	}
	return s
}
