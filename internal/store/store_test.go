package store

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestCapturePersistsExchange(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	capture, err := s.NewCapture(CaptureMeta{
		Method:    "POST",
		Path:      "/v1/chat/completions",
		Route:     "openai-v1",
		TargetURL: "https://api.openai.com/v1/chat/completions",
		Headers:   http.Header{"Content-Type": []string{"application/json"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	reqBody := capture.RequestBody(io.NopCloser(strings.NewReader(`{"messages":[{"role":"user","content":"ping"}]}`)))
	if _, err := io.Copy(io.Discard, reqBody); err != nil {
		t.Fatal(err)
	}
	if err := reqBody.Close(); err != nil {
		t.Fatal(err)
	}

	respBody := capture.ResponseBody(
		io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`)),
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

	items, err := s.List(context.Background(), ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0]
	if item.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", item.StatusCode)
	}
	if !strings.Contains(item.RequestPreview, "ping") {
		t.Fatalf("request preview = %q, want ping", item.RequestPreview)
	}
	if !strings.Contains(item.ResponsePreview, "pong") {
		t.Fatalf("response preview = %q, want pong", item.ResponsePreview)
	}
}

func TestCaptureDecodesZstdPreviewAndBody(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	encoded := zstdBytes(t, `{"input":"hello from compressed codex"}`)
	capture, err := s.NewCapture(CaptureMeta{
		Method:    "POST",
		Path:      "/v1/responses",
		Route:     "chatgpt-codex-responses",
		TargetURL: "https://chatgpt.com/backend-api/codex/responses",
		Headers: http.Header{
			"Content-Encoding": []string{"zstd"},
			"Content-Type":     []string{"application/json"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	reqBody := capture.RequestBody(io.NopCloser(bytes.NewReader(encoded)))
	if _, err := io.Copy(io.Discard, reqBody); err != nil {
		t.Fatal(err)
	}
	if err := reqBody.Close(); err != nil {
		t.Fatal(err)
	}
	if err := capture.Save(""); err != nil {
		t.Fatal(err)
	}

	items, err := s.List(context.Background(), ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if !strings.Contains(items[0].RequestPreview, "hello from compressed codex") {
		t.Fatalf("request preview = %q, want decoded text", items[0].RequestPreview)
	}

	info, err := s.Body(context.Background(), items[0].ID, "request")
	if err != nil {
		t.Fatal(err)
	}
	body, err := s.OpenBody(info)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	decoded, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "hello from compressed codex") {
		t.Fatalf("decoded body = %q, want decoded text", decoded)
	}
}

func zstdBytes(t *testing.T, raw string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
