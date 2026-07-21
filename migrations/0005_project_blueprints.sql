-- +goose Up
CREATE TABLE project_blueprints (
    project_id            TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    schema_version        INTEGER NOT NULL,
    fingerprint           TEXT NOT NULL,
    analysis_json         TEXT NOT NULL,
    analyzed_at           TEXT NOT NULL,
    confirmed_fingerprint TEXT,
    confirmed_at          TEXT
);

-- +goose Down
DROP TABLE project_blueprints;
