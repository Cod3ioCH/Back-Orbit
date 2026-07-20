// Package web embeds the built frontend (web/dist, produced by `npm run
// build`) so it ships inside the Go binary. See docs/adr/0010-embedded-frontend-build.md.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend build, rooted at its contents (i.e.
// index.html is at the root, not under "dist/").
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
