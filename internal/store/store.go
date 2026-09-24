// Package store is the local-first SQLite persistence layer. It is an
// append-only evidence log: raw input is immutable after submission, and
// every later change is a new version linked to its parent.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps the application database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for read-only queries in tests/handlers.
func (s *Store) DB() *sql.DB { return s.db }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Project is one investigation workspace.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// Revision is an immutable submitted input version.
type Revision struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	Rev            int    `json:"rev"`
	ParentRevision string `json:"parentRevisionId,omitempty"`
	SourceID       string `json:"sourceId"`
	Filename       string `json:"filename"`
	Note           string `json:"note"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	CreatedAt      string `json:"createdAt"`
}

// Event is one append-only history entry (import, parameter revision,
// source change, human decision, undo).
type Event struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Seq       int64           `json:"seq"`
	ParentID  string          `json:"parentId,omitempty"`
	Type      string          `json:"type"`
	SubjectID string          `json:"subjectId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"createdAt"`
}

// Event payload types ----------------------------------------------------

// ImportPayload records a submitted immutable revision.
type ImportPayload struct {
	RevisionID string `json:"revisionId"`
	Rev        int    `json:"rev"`
	Parent     string `json:"parentRevisionId,omitempty"`
	SourceID   string `json:"sourceId"`
	Filename   string `json:"filename"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Note       string `json:"note"`
}

// ParamPayload records a parser/derivation parameter revision.
type ParamPayload struct {
	ParseBudget     int    `json:"parseBudget"`
	RequireDeclared bool   `json:"requireDeclaredCrs"`
	PreviousEventID string `json:"previousEventId,omitempty"`
	Note            string `json:"note"`
}

// DerivedPayload records that a derivation ran, with all provenance.
type DerivedPayload struct {
	DerivationID string `json:"derivationId"`
	SourceID     string `json:"sourceId"`
	RevisionID   string `json:"revisionId"`
	ParseAlgo    string `json:"parseAlgo"`
	ParseFP      string `json:"parseFp"`
	XformAlgo    string `json:"xformAlgo"`
	XformFP      string `json:"xformFp"`
}

// DecisionPayload records a human accept/reject/adjust/undo.
type DecisionPayload struct {
	ObjectID  string `json:"objectId"`
	Action    string `json:"action"` // accept | reject | adjust | undo
	Kind      string `json:"kind"`   // transform_candidate | block | geokey
	Choice    string `json:"choice,omitempty"`
	Rationale string `json:"rationale"`
	Undoes    string `json:"undoes,omitempty"`
	EventSeq  int64  `json:"eventSeq,omitempty"`
}
