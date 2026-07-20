package api

import (
	"log/slog"
	"net"
	"net/http"
	"time"
)

// requestLogger logs one structured line per request. It deliberately never
// logs headers or bodies: request/response headers can carry session
// cookies or CSRF tokens, and bodies can carry passwords — logging only the
// method, path, status, and duration keeps the log allow-listed by
// construction rather than relying on a deny-list of fields to scrub.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_ip", remoteIP(r),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Flush lets statusWriter satisfy http.Flusher when the underlying
// ResponseWriter does, which the SSE handler relies on.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func requestIsSecure(r *http.Request, trustProxyHeaders bool) bool {
	if r.TLS != nil {
		return true
	}
	if trustProxyHeaders && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}
