// Package store is the append-only, event-sourced local database.
//
// Raw inputs are immutable content blobs; commits, rule revisions, decisions
// and forks are events. Derived facts are never overwritten: a changed direct
// source marks the dependent fact stale while keeping it viewable.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"ifdlantern/internal/clock"
	"ifdlantern/internal/digest"

	_ "modernc.org/sqlite"
)

type Store struct {
	db    *sql.DB
	clock clock.Clock
}

func Open(path string, cl clock.Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, clock: cl}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	slug TEXT NOT NULL UNIQUE,
	title TEXT NOT NULL,
	root_project TEXT,
	created_seq INTEGER
);
CREATE TABLE IF NOT EXISTS blobs (
	id TEXT PRIMARY KEY,
	data BLOB NOT NULL,
	wide_region BLOB,
	wide_start INTEGER,
	virtual INTEGER NOT NULL DEFAULT 0,
	note TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS events (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	id TEXT NOT NULL UNIQUE,
	parent TEXT,
	project TEXT NOT NULL,
	branch TEXT NOT NULL,
	kind TEXT NOT NULL,
	at TEXT NOT NULL,
	actor TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_proj ON events(project, seq);
CREATE TABLE IF NOT EXISTS sources (
	project TEXT NOT NULL,
	branch TEXT NOT NULL,
	label TEXT NOT NULL,
	version INTEGER NOT NULL,
	parent_blob TEXT,
	blob TEXT NOT NULL,
	birth_seq INTEGER NOT NULL,
	PRIMARY KEY(project, branch, label, version)
);
CREATE TABLE IF NOT EXISTS rules (
	project TEXT NOT NULL,
	branch TEXT NOT NULL,
	scope TEXT NOT NULL DEFAULT 'default',
	version INTEGER NOT NULL,
	budget_json TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT '',
	birth_seq INTEGER NOT NULL,
	PRIMARY KEY(project, branch, scope, version)
);
CREATE TABLE IF NOT EXISTS facts (
	project TEXT NOT NULL,
	branch TEXT NOT NULL,
	scope TEXT NOT NULL,
	label TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	key_id TEXT NOT NULL,
	data TEXT NOT NULL,
	source_blob TEXT NOT NULL,
	rule_fp TEXT NOT NULL,
	birth_seq INTEGER NOT NULL,
	PRIMARY KEY(project, branch, scope, label, kind, key_id, source_blob, rule_fp)
);
CREATE INDEX IF NOT EXISTS facts_lookup ON facts(project, branch, scope, label, kind, key_id);
CREATE TABLE IF NOT EXISTS decisions (
	project TEXT NOT NULL,
	branch TEXT NOT NULL,
	candidate TEXT NOT NULL,
	version INTEGER NOT NULL,
	action TEXT NOT NULL,
	params_json TEXT NOT NULL DEFAULT '{}',
	note TEXT NOT NULL DEFAULT '',
	birth_seq INTEGER NOT NULL,
	PRIMARY KEY(project, branch, candidate, version)
);
`

// projectID is content-addressed from the slug so re-importing an identical
// package restores the same id.
func projectID(slug string) string {
	return "prj-" + digest.Hash([]byte(slug))[:16]
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

var _ = fmt.Sprintf
