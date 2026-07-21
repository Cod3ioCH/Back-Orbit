-- +goose Up

-- A backup run is one attempt to back something up, kept whether it succeeded
-- or not. A failed run is the more important record of the two: it is the
-- evidence that a backup someone believes they have does not exist.
CREATE TABLE backup_runs (
    id              TEXT PRIMARY KEY,
    project_id      TEXT REFERENCES projects(id) ON DELETE SET NULL,
    -- Denormalised on purpose. The run has to stay readable after the project
    -- it belonged to is gone, and "which project was that?" is exactly the
    -- question being asked when reviewing an old failure.
    project_name    TEXT NOT NULL,
    repository_id   TEXT REFERENCES repositories(id) ON DELETE SET NULL,
    repository_name TEXT NOT NULL,

    trigger         TEXT NOT NULL,
    status          TEXT NOT NULL,
    phase           TEXT NOT NULL,

    -- What the run was asked to cover, so the record stays meaningful even
    -- when the project's volumes change afterwards.
    volumes_json    TEXT NOT NULL DEFAULT '[]',

    files_total     INTEGER NOT NULL DEFAULT 0,
    bytes_total     INTEGER NOT NULL DEFAULT 0,
    bytes_added     INTEGER NOT NULL DEFAULT 0,

    warnings_json   TEXT NOT NULL DEFAULT '[]',
    error           TEXT NOT NULL DEFAULT '',

    started_at      TEXT NOT NULL,
    ended_at        TEXT,
    created_at      TEXT NOT NULL
);

CREATE INDEX idx_backup_runs_created_at ON backup_runs(created_at DESC);
CREATE INDEX idx_backup_runs_project ON backup_runs(project_id);

-- A snapshot row exists only once the backup behind it has been verified, so
-- the presence of a row means the data was confirmed readable rather than
-- merely reported as written.
CREATE TABLE snapshots (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    repository_id      TEXT REFERENCES repositories(id) ON DELETE SET NULL,

    -- restic's own snapshot id. This is what a restore is performed with, and
    -- what lets someone recover using restic directly if Back-Orbit is gone.
    restic_snapshot_id TEXT NOT NULL,

    -- The versioned manifest: what was in this backup, and the original
    -- ownership of every path. Back-Orbit runs unprivileged and cannot apply
    -- uid/gid while staging, so without this a restore would hand an
    -- application files it does not own.
    manifest_json      TEXT NOT NULL,

    size_bytes         INTEGER NOT NULL DEFAULT 0,
    files_count        INTEGER NOT NULL DEFAULT 0,

    verified_at        TEXT,
    verification_json  TEXT NOT NULL DEFAULT '{}',

    created_at         TEXT NOT NULL
);

CREATE INDEX idx_snapshots_run ON snapshots(run_id);
CREATE INDEX idx_snapshots_repository ON snapshots(repository_id);
CREATE UNIQUE INDEX idx_snapshots_restic_id ON snapshots(repository_id, restic_snapshot_id);

-- +goose Down
DROP TABLE snapshots;
DROP TABLE backup_runs;
