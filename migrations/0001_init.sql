-- +goose Up
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    ip         TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

CREATE TABLE audit_events (
    id             TEXT PRIMARY KEY,
    actor_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    action         TEXT NOT NULL,
    target_type    TEXT NOT NULL DEFAULT '',
    target_id      TEXT NOT NULL DEFAULT '',
    metadata_json  TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL
);

CREATE INDEX idx_audit_events_created_at ON audit_events (created_at);
CREATE INDEX idx_audit_events_action ON audit_events (action);

CREATE TABLE projects (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL,
    compose_path       TEXT NOT NULL DEFAULT '',
    compose_files_json TEXT NOT NULL DEFAULT '[]',
    source             TEXT NOT NULL DEFAULT 'discovered',
    status             TEXT NOT NULL DEFAULT 'unprotected',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_projects_name ON projects (name);

-- +goose Down
DROP TABLE projects;
DROP TABLE audit_events;
DROP TABLE sessions;
DROP TABLE users;
