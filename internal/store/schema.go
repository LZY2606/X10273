package store

const schema = `
CREATE TABLE IF NOT EXISTS projects (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS revisions (
    id                 TEXT PRIMARY KEY,
    project_id         TEXT NOT NULL REFERENCES projects(id),
    rev                INTEGER NOT NULL,
    parent_revision_id TEXT,
    source_id          TEXT NOT NULL,
    filename           TEXT NOT NULL,
    note               TEXT NOT NULL DEFAULT '',
    sha256             TEXT NOT NULL,
    size               INTEGER NOT NULL,
    data               BLOB NOT NULL,
    created_at         TEXT NOT NULL,
    UNIQUE(project_id, rev)
);
CREATE TABLE IF NOT EXISTS events (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    seq         INTEGER NOT NULL,
    parent_id   TEXT,
    type        TEXT NOT NULL,
    subject_id  TEXT NOT NULL DEFAULT '',
    payload     BLOB NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE(project_id, seq)
);
CREATE TABLE IF NOT EXISTS decisions (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    event_id    TEXT NOT NULL REFERENCES events(id),
    object_id   TEXT NOT NULL,
    action      TEXT NOT NULL,
    rationale   TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS derivations (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id),
    event_id         TEXT NOT NULL REFERENCES events(id),
    source_id        TEXT NOT NULL,
    parse_fp         TEXT NOT NULL,
    transform_fp     TEXT NOT NULL,
    graph_json       BLOB NOT NULL,
    transforms_json  BLOB NOT NULL,
    created_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
CREATE INDEX IF NOT EXISTS idx_revisions_project ON revisions(project_id, rev);
CREATE INDEX IF NOT EXISTS idx_derivations_project ON derivations(project_id, source_id);
`
