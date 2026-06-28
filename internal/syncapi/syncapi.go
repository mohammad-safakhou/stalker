package syncapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mohammad-safakhou/stalker/internal/store"
)

type Handler struct {
	Store *store.Store
}

func New(s *store.Store) *Handler {
	return &Handler{Store: s}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/sync/health":
		h.health(w, r)
	case "/api/v1/sync/snapshot":
		h.snapshot(w, r)
	case "/api/v1/sync/stream":
		h.stream(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	health, err := h.Store.SyncHealth(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, health)
}

func (h *Handler) snapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.Store.SyncSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, snapshot)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming is not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func() bool {
		snapshot, err := h.Store.SyncSnapshot(r.Context())
		if err != nil {
			_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonString(map[string]string{"error": err.Error()}))
			flusher.Flush()
			return false
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonString(snapshot))
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func jsonString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
