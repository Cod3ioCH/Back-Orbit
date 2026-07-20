// Package api implements Back-Orbit's HTTP surface: the REST API, Server-Sent
// Event streams, and (in production) serving the embedded frontend build.
package api

import (
	"encoding/json"
	"net/http"
)

// maxRequestBodyBytes bounds the size of JSON request bodies the API will
// read, as a basic denial-of-service guard.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// decodeJSON reads and decodes a JSON request body into v, rejecting
// unknown fields and oversized bodies.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
