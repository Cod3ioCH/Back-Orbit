// Package database owns the SQLite connection and schema migrations.
package database

import (
	"database/sql"
	"fmt"
	"net/url"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/back-orbit/back-orbit/migrations"
)

// connectionPragmas are applied to *every* connection the driver opens, via
// the DSN, rather than via one-off `PRAGMA` statements after Open. Several of
// these (foreign_keys, busy_timeout, synchronous) are per-connection state,
// not persisted in the database file, so applying them through the DSN is the
// only way to guarantee they hold on every pooled connection — this keeps
// foreign-key cascade deletes (e.g. sessions/audit rows when a user is
// removed) correct even if the connection pool ever grows beyond one.
var connectionPragmas = []string{
	"busy_timeout(5000)",  // wait, don't fail, when the single writer is busy
	"journal_mode(WAL)",   // allow concurrent readers alongside one writer
	"foreign_keys(on)",    // enforce ON DELETE CASCADE / SET NULL constraints
	"synchronous(normal)", // safe with WAL, faster than FULL
}

// Open opens the SQLite database at path with the connection pragmas above
// applied to every connection, and runs any pending migrations.
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?" + url.Values{"_pragma": connectionPragmas}.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// SQLite allows only one writer at a time; a single shared connection
	// avoids "database is locked" contention under concurrent access from
	// Back-Orbit's own goroutines while WAL mode still allows concurrent
	// readers. The pragmas above are applied per-connection via the DSN, so
	// this cap is a performance/simplicity choice, not what keeps them in
	// effect.
	db.SetMaxOpenConns(1)

	// Verify the connection actually works (sql.Open is lazy) and surface a
	// clear error early rather than on the first query.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to sqlite database: %w", err)
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
