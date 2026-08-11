package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	after, ok := parseAfter(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	items, err := a.events.ListAfter(r.Context(), after, limit)
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": items})
}

func (a *API) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "stream_unavailable", "streaming is unavailable")
		return
	}
	after, valid := parseAfter(w, r)
	if !valid {
		return
	}
	if value := strings.TrimSpace(r.Header.Get("Last-Event-ID")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_after", "Last-Event-ID must be an unsigned integer")
			return
		}
		if parsed > after {
			after = parsed
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		items, err := a.events.ListAfter(r.Context(), after, 100)
		if err != nil {
			return
		}
		for _, event := range items {
			payload, err := json.Marshal(event)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, payload); err != nil {
				return
			}
			after = event.Sequence
		}
		if len(items) > 0 {
			flusher.Flush()
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// handleModelStream fans out live provider StreamEvents (typewriter).
// Query: session_id (optional filter), run_id (optional filter).
func (a *API) handleModelStream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if a.modelStream == nil {
		writeError(w, http.StatusServiceUnavailable, "stream_unavailable", "model stream is not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "stream_unavailable", "streaming is unavailable")
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	ch, cancel := a.modelStream.Subscribe(sessionID, runID, 128)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// Initial comment so clients know the stream is open.
	if _, err := fmt.Fprintf(w, ": model-stream ready\n\n"); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case env, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(env)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: model\ndata: %s\n\n", env.Seq, payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
