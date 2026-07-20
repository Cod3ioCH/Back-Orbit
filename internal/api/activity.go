package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			limit = parsed
		}
	}

	eventsList, err := s.eventStore.ListRecent(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list audit events")
		return
	}
	writeJSON(w, http.StatusOK, eventsList)
}

// handleActivityStream serves a Server-Sent Events stream of live audit
// events. It stays open until the client disconnects or the server shuts
// down (via request context cancellation).
func (s *Server) handleActivityStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	ch, unsubscribe := s.eventBroker.Subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Action, payload)
			flusher.Flush()
		}
	}
}
