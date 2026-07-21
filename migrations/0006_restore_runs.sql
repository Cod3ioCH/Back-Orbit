-- +goose Up
CREATE TABLE restore_runs (
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
CREATE INDEX idx_restore_runs_created_at ON restore_runs(created_at DESC);

-- +goose Down
DROP TABLE restore_runs;
