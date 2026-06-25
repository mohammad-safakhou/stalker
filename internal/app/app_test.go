package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mohammad-safakhou/stalker/internal/store"
)

func TestAppRoutesTokenSummaryToUI(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18080/api/tokens/summary", nil)
	rec := httptest.NewRecorder()

	New(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %q", rec.Code, rec.Body.String())
	}
}

func TestAppRoutesTokenStreamToUI(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18080/api/tokens/stream", nil).WithContext(ctx)
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

func TestAppRoutesSyncSnapshotToSyncAPI(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18080/api/v1/sync/snapshot", nil)
	rec := httptest.NewRecorder()

	New(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"device"`) {
		t.Fatalf("body = %q, want sync snapshot", rec.Body.String())
	}
}
