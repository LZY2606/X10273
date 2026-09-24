// Package store persists the project event log, immutable sources,
// derived values and decisions in a local SQLite database. All rows
// carry stable IDs and sequence numbers so exports can be deleted and
// re-imported without losing identity, order or provenance.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Event kinds.
const (
	EvImportSource   = "import-source"
	EvReviseParams   = "revise-params"
	EvVerifyBaseline = "verify-baseline"
	EvDecide         = "decide"
	EvUndo           = "undo"
	EvExport         = "export"
	EvImportArchive  = "import-archive"
	EvNote           = "note"
)

// Event is one appended entry in the project history.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"` // evt-000123
	Kind    string          `json:"kind"`
	Actor   string          `json:"actor"`
	At      time.Time       `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

// Source is an immutable raw input. Revisions create new rows linked by
// ParentID; old versions are never overwritten.
type Source struct {
	ID        string `json:"id"` // src-<sha12>-v<n>
	Version   int    `json:"version"`
	ParentID  string `json:"parentId,omitempty"`
	Name      string `json:"name"`
	Sha256    string `json:"sha256"`
	Bytes     []byte `json:"-"`
	CreatedSeq int64 `json:"createdSeq"`
	Status    string `json:"status"` // "current" | "superseded"
}

// Derived is a computed value with algorithm version, parameter
// fingerprint and direct sources. ValidTo = 0 means still current;
// expired rows stay readable but cannot pose as current.
type Derived struct {
	ID                string   `json:"id"`
	Kind              string   `json:"kind"` // "parse" | "blocks" | "geo"
	AlgoVersion       string   `json:"algoVersion"`
	ParamsFingerprint string   `json:"paramsFingerprint"`
	SourceIDs         []string `json:"sourceIds"`
	Payload           json.RawMessage `json:"payload"`
	CreatedSeq        int64    `json:"createdSeq"`
	ValidFrom         int64    `json:"validFrom"`
	ValidTo           int64    `json:"validTo"` // 0 = current
}

// Decision records a human ruling on a candidate.
type Decision struct {
	ID          string `json:"id"` // dec-<seq>
	CandidateID string `json:"candidateId"`
	Action      string `json:"action"` // "accept" | "reject"
	Reason      string `json:"reason"`
	Actor       string `json:"actor"`
	Seq         int64  `json:"seq"`
	ValidFrom   int64  `json:"validFrom"`
	ValidTo     int64  `json:"validTo"` // 0 = in force
}

// Store wraps the SQLite handle.
type Store struct {
	db *sql.DB
}

// Open creates or opens the database at path (":memory:" allowed).
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT UNIQUE NOT NULL,
  kind TEXT NOT NULL,
  actor TEXT NOT NULL,
  at TEXT NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sources(
  id TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  bytes BLOB NOT NULL,
  created_seq INTEGER NOT NULL,
  status TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS derived(
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  params_fingerprint TEXT NOT NULL,
  source_ids TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  valid_from INTEGER NOT NULL,
  valid_to INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY,
  candidate_id TEXT NOT NULL,
  action TEXT NOT NULL,
  reason TEXT NOT NULL,
  actor TEXT NOT NULL,
  seq INTEGER NOT NULL,
  valid_from INTEGER NOT NULL,
  valid_to INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS kv(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`)
	return err
}

// NextSeq returns the sequence number the next event will receive.
func (s *Store) NextSeq() (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n.Int64 + 1, nil
}

// AppendEvent inserts an event and returns it with its assigned seq/ID.
func (s *Store) AppendEvent(kind, actor string, at time.Time, payload any) (*Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO events(kind, actor, at, payload) VALUES(?,?,?,?)`,
		kind, actor, at.UTC().Format(time.RFC3339Nano), string(raw))
	if err != nil {
		return nil, err
	}
	seq, _ := res.LastInsertId()
	id := fmt.Sprintf("evt-%06d", seq)
	if _, err := s.db.Exec(`UPDATE events SET id=? WHERE seq=?`, id, seq); err != nil {
		return nil, err
	}
	return &Event{Seq: seq, ID: id, Kind: kind, Actor: actor, At: at.UTC(), Payload: raw}, nil
}

// PutEvent inserts an event with a known ID/seq (archive import).
func (s *Store) PutEvent(e *Event) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO events(seq, id, kind, actor, at, payload) VALUES(?,?,?,?,?,?)`,
		e.Seq, e.ID, e.Kind, e.Actor, e.At.UTC().Format(time.RFC3339Nano), string(e.Payload))
	return err
}

// Events lists events up to maxSeq (0 = all), ordered by seq.
func (s *Store) Events(maxSeq int64) ([]*Event, error) {
	q := `SELECT seq, id, kind, actor, at, payload FROM events`
	args := []any{}
	if maxSeq > 0 {
		q += ` WHERE seq <= ?`
		args = append(args, maxSeq)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		var e Event
		var at, payload string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Kind, &e.Actor, &at, &payload); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		e.Payload = json.RawMessage(payload)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// PutSource inserts an immutable source row.
func (s *Store) PutSource(src *Source) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO sources(id, version, parent_id, name, sha256, bytes, created_seq, status)
		VALUES(?,?,?,?,?,?,?,?)`,
		src.ID, src.Version, src.ParentID, src.Name, src.Sha256, src.Bytes, src.CreatedSeq, src.Status)
	return err
}

// MarkSourceSuperseded flips a source status without touching its bytes.
func (s *Store) MarkSourceSuperseded(id string) error {
	_, err := s.db.Exec(`UPDATE sources SET status='superseded' WHERE id=?`, id)
	return err
}

// Sources lists sources, optionally only current ones.
func (s *Store) Sources(currentOnly bool) ([]*Source, error) {
	q := `SELECT id, version, parent_id, name, sha256, bytes, created_seq, status FROM sources`
	if currentOnly {
		q += ` WHERE status='current'`
	}
	q += ` ORDER BY created_seq, id`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Source
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.ID, &src.Version, &src.ParentID, &src.Name, &src.Sha256, &src.Bytes, &src.CreatedSeq, &src.Status); err != nil {
			return nil, err
		}
		out = append(out, &src)
	}
	return out, rows.Err()
}

// Source loads one source by ID.
func (s *Store) Source(id string) (*Source, error) {
	row := s.db.QueryRow(`SELECT id, version, parent_id, name, sha256, bytes, created_seq, status FROM sources WHERE id=?`, id)
	var src Source
	if err := row.Scan(&src.ID, &src.Version, &src.ParentID, &src.Name, &src.Sha256, &src.Bytes, &src.CreatedSeq, &src.Status); err != nil {
		return nil, err
	}
	return &src, nil
}

// PutDerived inserts a derived row.
func (s *Store) PutDerived(d *Derived) error {
	ids, _ := json.Marshal(d.SourceIDs)
	_, err := s.db.Exec(`INSERT OR IGNORE INTO derived(id, kind, algo_version, params_fingerprint, source_ids, payload, created_seq, valid_from, valid_to)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Kind, d.AlgoVersion, d.ParamsFingerprint, string(ids), string(d.Payload), d.CreatedSeq, d.ValidFrom, d.ValidTo)
	return err
}

// ExpireDerived sets valid_to on rows that are still current.
func (s *Store) ExpireDerived(ids []string, atSeq int64) error {
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE derived SET valid_to=? WHERE id=? AND valid_to=0`, atSeq, id); err != nil {
			return err
		}
	}
	return nil
}

// DerivedRows lists derived rows visible at asOf (0 = current view:
// valid_to = 0). With asOf > 0 it returns the historical view.
func (s *Store) DerivedRows(asOf int64) ([]*Derived, error) {
	var rows *sql.Rows
	var err error
	if asOf > 0 {
		rows, err = s.db.Query(`SELECT id, kind, algo_version, params_fingerprint, source_ids, payload, created_seq, valid_from, valid_to
			FROM derived WHERE valid_from <= ? AND (valid_to = 0 OR valid_to > ?) ORDER BY created_seq, id`, asOf, asOf)
	} else {
		rows, err = s.db.Query(`SELECT id, kind, algo_version, params_fingerprint, source_ids, payload, created_seq, valid_from, valid_to
			FROM derived WHERE valid_to = 0 ORDER BY created_seq, id`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDerived(rows)
}

// DerivedAll lists every derived row including expired ones.
func (s *Store) DerivedAll() ([]*Derived, error) {
	rows, err := s.db.Query(`SELECT id, kind, algo_version, params_fingerprint, source_ids, payload, created_seq, valid_from, valid_to
		FROM derived ORDER BY created_seq, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDerived(rows)
}

func scanDerived(rows *sql.Rows) ([]*Derived, error) {
	var out []*Derived
	for rows.Next() {
		var d Derived
		var ids, payload string
		if err := rows.Scan(&d.ID, &d.Kind, &d.AlgoVersion, &d.ParamsFingerprint, &ids, &payload, &d.CreatedSeq, &d.ValidFrom, &d.ValidTo); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(ids), &d.SourceIDs)
		d.Payload = json.RawMessage(payload)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// PutDecision inserts a decision row.
func (s *Store) PutDecision(d *Decision) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO decisions(id, candidate_id, action, reason, actor, seq, valid_from, valid_to)
		VALUES(?,?,?,?,?,?,?,?)`,
		d.ID, d.CandidateID, d.Action, d.Reason, d.Actor, d.Seq, d.ValidFrom, d.ValidTo)
	return err
}

// ExpireDecision retires a decision (undo).
func (s *Store) ExpireDecision(id string, atSeq int64) error {
	_, err := s.db.Exec(`UPDATE decisions SET valid_to=? WHERE id=? AND valid_to=0`, atSeq, id)
	return err
}

// Decisions lists decisions in force at asOf (0 = now).
func (s *Store) Decisions(asOf int64) ([]*Decision, error) {
	var rows *sql.Rows
	var err error
	if asOf > 0 {
		rows, err = s.db.Query(`SELECT id, candidate_id, action, reason, actor, seq, valid_from, valid_to
			FROM decisions WHERE valid_from <= ? AND (valid_to = 0 OR valid_to > ?) ORDER BY seq`, asOf, asOf)
	} else {
		rows, err = s.db.Query(`SELECT id, candidate_id, action, reason, actor, seq, valid_from, valid_to
			FROM decisions WHERE valid_to = 0 ORDER BY seq`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.CandidateID, &d.Action, &d.Reason, &d.Actor, &d.Seq, &d.ValidFrom, &d.ValidTo); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// DecisionsAll lists every decision including undone ones.
func (s *Store) DecisionsAll() ([]*Decision, error) {
	rows, err := s.db.Query(`SELECT id, candidate_id, action, reason, actor, seq, valid_from, valid_to FROM decisions ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.CandidateID, &d.Action, &d.Reason, &d.Actor, &d.Seq, &d.ValidFrom, &d.ValidTo); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// SetKV stores a key/value pair.
func (s *Store) SetKV(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO kv(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetKV reads a key; returns ("", nil) when absent.
func (s *Store) GetKV(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}
