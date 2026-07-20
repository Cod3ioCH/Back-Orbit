// Package database owns the SQLite connection and schema migrations.
package database

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/back-orbit/back-orbit/migrations"
)

// Open opens the SQLite database at path, configures it for safe concurrent
// access (WAL journal mode, a busy timeout so concurrent writers block
// instead of failing immediately, and foreign key enforcement), and applies
// any pending migrations.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// SQLite allows only one writer at a time; a single shared connection
	// avoids "database is locked" errors under concurrent access from
	// Back-Orbit's own goroutines while WAL mode still allows concurrent
	// readers.
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA synchronous = NORMAL;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply pragma %q: %w", p, err)
		}
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
