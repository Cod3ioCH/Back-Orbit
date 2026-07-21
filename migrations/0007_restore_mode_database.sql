-- Adds 'database' to the restore modes: replaying one database's export back
-- into the running service it came from.
--
-- SQLite cannot alter a CHECK constraint, so the table is rebuilt. Existing
-- rows are carried over unchanged; a restore run is a record of something that
-- happened, and losing that history would erase the audit trail of every
-- destructive restore performed so far.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE restore_runs_new (
    id TEXT PRIMARY KEY,
    snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
    project_name TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('extract', 'in_place', 'clone', 'database')),
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    target_path TEXT NOT NULL DEFAULT '',
    files_restored INTEGER NOT NULL DEFAULT 0,
    bytes_restored INTEGER NOT NULL DEFAULT 0,
    warnings_json TEXT NOT NULL DEFAULT '[]',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    ended_at TEXT,
    created_at TEXT NOT NULL
);

INSERT INTO restore_runs_new
SELECT id, snapshot_id, project_name, mode, status, target_path, files_restored,
       bytes_restored, warnings_json, error, started_at, ended_at, created_at
FROM restore_runs;

DROP TABLE restore_runs;
ALTER TABLE restore_runs_new RENAME TO restore_runs;
CREATE INDEX idx_restore_runs_created_at ON restore_runs(created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM restore_runs WHERE mode = 'database';

CREATE TABLE restore_runs_old (
    id TEXT PRIMARY KEY,
    snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
    project_name TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('extract', 'in_place', 'clone')),
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    target_path TEXT NOT NULL DEFAULT '',
    files_restored INTEGER NOT NULL DEFAULT 0,
    bytes_restored INTEGER NOT NULL DEFAULT 0,
    warnings_json TEXT NOT NULL DEFAULT '[]',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    ended_at TEXT,
    created_at TEXT NOT NULL
);

INSERT INTO restore_runs_old
SELECT id, snapshot_id, project_name, mode, status, target_path, files_restored,
       bytes_restored, warnings_json, error, started_at, ended_at, created_at
FROM restore_runs;

DROP TABLE restore_runs;
ALTER TABLE restore_runs_old RENAME TO restore_runs;
CREATE INDEX idx_restore_runs_created_at ON restore_runs(created_at DESC);
-- +goose StatementEnd
