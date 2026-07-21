-- +goose Up

-- repositories are the destinations snapshots are written to.
--
-- There is deliberately no password column. A repository's password lives in
-- the secret store, encrypted, keyed by this row's id — so a copy of this
-- table on its own reveals where backups go, but never how to read them.
CREATE TABLE repositories (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    kind             TEXT NOT NULL,
    location         TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'unknown',
    last_error       TEXT NOT NULL DEFAULT '',
    last_checked_at  TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_repositories_name ON repositories (name);

-- +goose Down
DROP TABLE repositories;
