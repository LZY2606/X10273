// Package store persists the event log, immutable input blobs and derived
// objects in a local SQLite database. No external database service is used.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  id TEXT NOT NULL PRIMARY KEY,
  kind TEXT NOT NULL,
  ts TEXT NOT NULL,
  parent_id TEXT NOT NULL,
  actor TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS events_project_seq ON events(project_id, seq);
CREATE TABLE IF NOT EXISTS blobs (
  sha TEXT PRIMARY KEY,
  data BLOB NOT NULL,
  size INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS versions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  doc_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  sha TEXT NOT NULL,
  label TEXT NOT NULL,
  origin TEXT NOT NULL,
  import_ok INTEGER NOT NULL,
  parse_error TEXT NOT NULL DEFAULT '',
  created_seq INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS versions_project_doc_ver ON versions(project_id, doc_id, version);
CREATE TABLE IF NOT EXISTS derived (
  id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  version_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  produced_seq INTEGER NOT NULL,
  param_fp TEXT NOT NULL,
  payload TEXT NOT NULL,
  stale INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id, produced_seq)
);
CREATE INDEX IF NOT EXISTS derived_version ON derived(version_id);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  target_kind TEXT NOT NULL,
  target_id TEXT NOT NULL,
  verdict TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  reviewer TEXT NOT NULL,
  ts TEXT NOT NULL,
  active INTEGER NOT NULL DEFAULT 1,
  revoked_by TEXT NOT NULL DEFAULT ''
);
`

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }
