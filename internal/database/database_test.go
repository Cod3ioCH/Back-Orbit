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

// TestOpenEnforcesForeignKeys guards the DSN-based pragma setup: foreign key
// enforcement is per-connection state, so if it ever stopped being applied to
// every pooled connection, ON DELETE CASCADE would silently stop working and
// deleting a user would leave orphaned sessions behind.
func TestOpenEnforcesForeignKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys to be enabled, got %d", foreignKeys)
	}

	now := "2026-01-01T00:00:00Z"
	if _, err := db.Exec(
		`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"user-1", "admin", "hash", now, now,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		"session-1", "user-1", "tokenhash", now, now,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM users WHERE id = ?`, "user-1"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var remainingSessions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&remainingSessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remainingSessions != 0 {
		t.Fatalf("expected the session to be cascade-deleted with its user, got %d remaining", remainingSessions)
	}
}

func TestOpenRejectsUnusablePath(t *testing.T) {
	// A path whose parent directory does not exist cannot be opened; Open
	// must surface that immediately rather than returning a handle that
	// fails later on first use.
	_, err := Open(filepath.Join(t.TempDir(), "no-such-dir", "test.db"))
	if err == nil {
		t.Fatal("expected an error for a database path in a nonexistent directory")
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
