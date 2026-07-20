// Package dbtest provides a shared helper for tests across the codebase
// that need a real, migrated SQLite database. It is only ever imported from
// _test.go files, so it never ends up in the production binary.
package dbtest

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/back-orbit/back-orbit/internal/database"
)

// Open creates a fresh, migrated SQLite database in a temporary directory
// that is cleaned up automatically when the test completes.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "back-orbit-test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}
