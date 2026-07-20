package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
)

// mountStatic serves the embedded frontend build, if one was provided to
// NewServer, with SPA-style fallback to index.html for any path that isn't
// an existing static asset (so client-side routes like /projects/abc work
// on a hard refresh). When no static filesystem is configured — local
// development, where Vite serves the frontend on its own port — Back-Orbit
// only serves the API and responds with a clear message on other paths.
func (s *Server) mountStatic(r chi.Router) {
	if s.staticFS == nil {
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "not found (no embedded frontend build in this binary; run the Vite dev server separately)")
		})
		return
	}

	fileServer := http.FileServer(http.FS(s.staticFS))

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}

		if _, err := fs.Stat(s.staticFS, cleanPath); err != nil {
			// Unknown path: fall back to index.html so the SPA's own router
			// handles it.
			r2 := new(http.Request)
			*r2 = *r
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}
