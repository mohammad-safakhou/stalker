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
	if item.RequestPreview != "" || item.ResponsePreview != "" {
		t.Fatalf("previews = (%q, %q), want no retained payload data", item.RequestPreview, item.ResponsePreview)
	}
	if item.RequestBodyPath != "" || item.ResponseBodyPath != "" {
		t.Fatalf("body paths = (%q, %q), want no retained body files", item.RequestBodyPath, item.ResponseBodyPath)
	}
}

func TestCaptureTokenizesZstdWithoutRetainingBody(t *testing.T) {
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
	if items[0].RequestPreview != "" || items[0].RequestBodyPath != "" {
		t.Fatalf("retained payload = (%q, %q), want empty", items[0].RequestPreview, items[0].RequestBodyPath)
	}
	report, err := s.TokenReport(context.Background(), items[0].ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) == 0 || report.Runs[0].TokenCount == 0 {
		t.Fatalf("token report = %+v, want tokenized compressed input", report)
	}
}

func TestCaptureIndexesInputOutputTokenBurns(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	capture, err := s.NewCapture(CaptureMeta{
		Method:    "POST",
		Path:      "/v1/responses",
		Route:     "chatgpt-codex-responses",
		TargetURL: "https://chatgpt.com/backend-api/codex/responses",
		Headers:   http.Header{"Content-Type": []string{"application/json"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	reqBody := capture.RequestBody(io.NopCloser(strings.NewReader(`{"input":"alpha alpha beta"}`)))
	if _, err := io.Copy(io.Discard, reqBody); err != nil {
		t.Fatal(err)
	}
	if err := reqBody.Close(); err != nil {
		t.Fatal(err)
	}

	respBody := capture.ResponseBody(
		io.NopCloser(strings.NewReader(`{"output":[{"content":"gamma"}]}`)),
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
	report, err := s.TokenReport(context.Background(), items[0].ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 2 {
		t.Fatalf("token runs = %d, want input and output", len(report.Runs))
	}
	var inputRun TokenRun
	for _, run := range report.Runs {
		if run.Side == "input" {
			inputRun = run
		}
	}
	if inputRun.Tokenizer != tokenizerVersion {
		t.Fatalf("input tokenizer = %q, want %q", inputRun.Tokenizer, tokenizerVersion)
	}
	if inputRun.TokenCount == 0 || inputRun.UniqueTokenCount == 0 || inputRun.DigestSHA256 == "" {
		t.Fatalf("input token run not populated: %+v", inputRun)
	}
	if len(report.Values) != 0 {
		t.Fatalf("per-exchange token values = %d, want 0 for new captures", len(report.Values))
	}

	totals, err := s.TokenTotals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if totals.InputTokens == 0 {
		t.Fatalf("input total = %d, want non-zero", totals.InputTokens)
	}
	if totals.OutputTokens == 0 {
		t.Fatalf("output total = %d, want non-zero", totals.OutputTokens)
	}
	if len(totals.Top.Input) == 0 {
		t.Fatal("top input tokens is empty")
	}
	if totals.Top.Input[0].Token != "alpha" || totals.Top.Input[0].Occurrences != 2 {
		t.Fatalf("top input token = %+v, want alpha with 2 occurrences", totals.Top.Input[0])
	}
	if len(totals.Top.Output) == 0 {
		t.Fatal("top output tokens is empty")
	}
}

func TestCompactRawDataRemovesBodiesAndPerExchangeTokenValues(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	capture, err := s.NewCapture(CaptureMeta{
		Method:    "POST",
		Path:      "/v1/responses",
		Route:     "chatgpt-codex-responses",
		TargetURL: "https://chatgpt.com/backend-api/codex/responses",
		Headers:   http.Header{"Content-Type": []string{"application/json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqBody := capture.RequestBody(io.NopCloser(strings.NewReader(`{"input":"alpha alpha beta"}`)))
	if _, err := io.Copy(io.Discard, reqBody); err != nil {
		t.Fatal(err)
	}
	if err := reqBody.Close(); err != nil {
		t.Fatal(err)
	}
	if err := capture.Save(""); err != nil {
		t.Fatal(err)
	}

	if err := s.CompactRawData(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(context.Background(), ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].RequestPreview != "" || items[0].RequestBodyPath != "" {
		t.Fatalf("payload data retained after compact: %+v", items[0])
	}
	report, err := s.TokenReport(context.Background(), items[0].ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Values) != 0 {
		t.Fatalf("per-exchange token values = %d, want 0 after compact", len(report.Values))
	}
	totals, err := s.TokenTotals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(totals.Top.Input) == 0 {
		t.Fatal("top input tokens empty after compact")
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
