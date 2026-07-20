// Package migrations embeds the SQL migration files so they ship inside the
// compiled Back-Orbit binary and do not need to be present on disk at
// runtime.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
