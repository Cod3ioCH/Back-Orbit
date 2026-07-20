package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "database unreachable"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
