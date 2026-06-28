package store

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSyncSnapshotBuildsBucketsFromTokenRuns(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	capture, err := s.NewCapture(CaptureMeta{
		Method:    "POST",
		Path:      "/v1/responses",
		Route:     "openai-responses",
		TargetURL: "https://api.openai.com/v1/responses",
		Headers:   http.Header{"Content-Type": []string{"application/json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqBody := capture.RequestBody(io.NopCloser(strings.NewReader(`{"input":"one two two"}`)))
	if _, err := io.Copy(io.Discard, reqBody); err != nil {
		t.Fatal(err)
	}
	if err := reqBody.Close(); err != nil {
		t.Fatal(err)
	}
	respBody := capture.ResponseBody(
		io.NopCloser(strings.NewReader(`{"output":[{"content":"three"}]}`)),
		http.StatusOK,
		http.Header{"Content-Type": []string{"application/json"}},
		true,
	)
	if _, err := io.Copy(io.Discard, respBody); err != nil {
		t.Fatal(err)
	}
	if err := respBody.Close(); err != nil {
		t.Fatal(err)
	}
	waitForTokenRuns(t, s, capture.ex.ID, 2)

	snapshot, err := s.SyncSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Device.ID == "" || snapshot.Cursor == "" {
		t.Fatalf("snapshot identifiers not populated: %+v", snapshot)
	}
	if snapshot.Live.InputTokens == 0 || snapshot.Live.OutputTokens == 0 {
		t.Fatalf("live = %+v, want non-zero input and output", snapshot.Live)
	}
	if len(snapshot.Hourly) != 1 || len(snapshot.Daily) != 1 {
		t.Fatalf("bucket lengths = hourly %d daily %d, want 1 each", len(snapshot.Hourly), len(snapshot.Daily))
	}
	if snapshot.Hourly[0].Requests != 1 || snapshot.Hourly[0].Streams != 1 {
		t.Fatalf("hourly bucket = %+v, want one streaming request", snapshot.Hourly[0])
	}
}
