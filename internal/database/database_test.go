package database

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	expectedTables := []string{"users", "sessions", "audit_events", "projects", "goose_db_version"}
	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("expected table %q to exist after migration: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	db1.Close()

	// Reopening (and thus re-running migrations) against the same database
	// file must not fail or duplicate schema objects.
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer db2.Close()

	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("query users table: %v", err)
	}
}

func TestOpenSetsWALMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("expected WAL journal mode, got %q", mode)
	}
}
