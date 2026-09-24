package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type EventRow struct {
	ProjectID string
	Seq       int64
	ID        string
	Kind      string
	TS        string
	ParentID  string
	Actor     string
	Title     string
	Summary   string
	Payload   map[string]any
}

func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) CreateProject(ctx context.Context, id, name string, seq int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,name,created_seq) VALUES(?,?,?)`, id, name, seq)
	return err
}

func (s *Store) NextSeq(ctx context.Context, projectID string) (int64, error) {
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM events WHERE project_id=?`, projectID).Scan(&max); err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return max.Int64 + 1, nil
}

func (s *Store) AppendEvent(ctx context.Context, r EventRow) error {
	payload, err := json.Marshal(r.Payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,id,kind,ts,parent_id,actor,title,summary,payload)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.ProjectID, r.Seq, r.ID, r.Kind, r.TS, r.ParentID, r.Actor, r.Title, r.Summary, string(payload))
	return err
}

func (s *Store) ListEvents(ctx context.Context, projectID string) ([]EventRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,id,kind,ts,parent_id,actor,title,summary,payload FROM events
		 WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

type rowScanner interface {
	Scan(...any) error
	Next() bool
}

func scanEvents(rows *sql.Rows) ([]EventRow, error) {
	var out []EventRow
	for rows.Next() {
		var r EventRow
		var payload string
		if err := rows.Scan(&r.Seq, &r.ID, &r.Kind, &r.TS, &r.ParentID,
			&r.Actor, &r.Title, &r.Summary, &payload); err != nil {
			return nil, err
		}
		if payload != "" {
			_ = json.Unmarshal([]byte(payload), &r.Payload)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PutBlob(ctx context.Context, sha string, data []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO blobs(sha,data,size) VALUES(?,?,?)
		 ON CONFLICT(sha) DO NOTHING`, sha, data, len(data))
	return err
}

func (s *Store) GetBlob(ctx context.Context, sha string) ([]byte, error) {
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT data FROM blobs WHERE sha=?`, sha).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

func (s *Store) PutVersion(ctx context.Context, v VersionRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO versions(id,project_id,doc_id,version,parent_id,sha,label,origin,import_ok,parse_error,created_seq)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		v.ID, v.ProjectID, v.DocID, v.Version, v.ParentID, v.SHA, v.Label, v.Origin,
		boolInt(v.ImportOK), v.ParseError, v.CreatedSeq)
	return err
}

type VersionRow struct {
	ID, ProjectID, DocID, ParentID, SHA, Label, Origin, ParseError string
	Version, CreatedSeq                                            int64
	ImportOK                                                       bool
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) ListVersions(ctx context.Context, projectID string) ([]VersionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,doc_id,version,parent_id,sha,label,origin,import_ok,parse_error,created_seq
		 FROM versions WHERE project_id=? ORDER BY created_seq, doc_id, version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VersionRow
	for rows.Next() {
		var v VersionRow
		var ok int
		if err := rows.Scan(&v.ID, &v.DocID, &v.Version, &v.ParentID, &v.SHA,
			&v.Label, &v.Origin, &ok, &v.ParseError, &v.CreatedSeq); err != nil {
			return nil, err
		}
		v.ProjectID = projectID
		v.ImportOK = ok == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) PutDerived(ctx context.Context, id, projectID, versionID, kind string,
	seq int64, fp string, payload []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO derived(id,project_id,version_id,kind,produced_seq,param_fp,payload,stale)
		 VALUES(?,?,?,?,?,?,?,0)
		 ON CONFLICT(id,produced_seq) DO NOTHING`,
		id, projectID, versionID, kind, seq, fp, string(payload))
	return err
}

type DerivedRow struct {
	ID          string
	VersionID   string
	Kind        string
	ProducedSeq int64
	ParamFP     string
	Payload     []byte
	Stale       bool
}

func (s *Store) ListDerived(ctx context.Context, projectID string) ([]DerivedRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,version_id,kind,produced_seq,param_fp,payload,stale FROM derived
		 WHERE project_id=? ORDER BY produced_seq, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DerivedRow
	for rows.Next() {
		var d DerivedRow
		var stale int
		var payload string
		if err := rows.Scan(&d.ID, &d.VersionID, &d.Kind, &d.ProducedSeq,
			&d.ParamFP, &payload, &stale); err != nil {
			return nil, err
		}
		d.Payload = []byte(payload)
		d.Stale = stale == 1
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkStaleByVersion flags every earlier derivation of superseded versions.
func (s *Store) MarkStaleByVersion(ctx context.Context, projectID, headVersionID string) error {
	// Derived rows belong to a concrete version; a view computes currency from
	// heads + parameter fingerprint. The column exists for explicit marking.
	_, err := s.db.ExecContext(ctx,
		`UPDATE derived SET stale=1 WHERE project_id=? AND version_id<>?`,
		projectID, headVersionID)
	return err
}

type DecisionRow struct {
	ID, ProjectID, EventID, TargetKind, TargetID, Verdict, Reason, Reviewer, TS, RevokedBy string
	Active                                                                                 bool
}

func (s *Store) PutDecision(ctx context.Context, d DecisionRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO decisions(id,project_id,event_id,target_kind,target_id,verdict,reason,reviewer,ts,active,revoked_by)
		 VALUES(?,?,?,?,?,?,?,?,?,1,'')
		 ON CONFLICT(id) DO NOTHING`,
		d.ID, d.ProjectID, d.EventID, d.TargetKind, d.TargetID, d.Verdict, d.Reason, d.Reviewer, d.TS)
	return err
}

func (s *Store) RevokeDecision(ctx context.Context, projectID, targetID, revokedBy string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE decisions SET active=0, revoked_by=? WHERE project_id=? AND target_id=?`,
		revokedBy, projectID, targetID)
	return err
}

func (s *Store) ListDecisions(ctx context.Context, projectID string) ([]DecisionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,event_id,target_kind,target_id,verdict,reason,reviewer,ts,active,revoked_by
		 FROM decisions WHERE project_id=? ORDER BY ts, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DecisionRow
	for rows.Next() {
		var d DecisionRow
		var active int
		if err := rows.Scan(&d.ID, &d.EventID, &d.TargetKind, &d.TargetID,
			&d.Verdict, &d.Reason, &d.Reviewer, &d.TS, &active, &d.RevokedBy); err != nil {
			return nil, err
		}
		d.ProjectID = projectID
		d.Active = active == 1
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) AllBlobSHAs(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT sha FROM versions WHERE project_id=? ORDER BY sha`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		out = append(out, sha)
	}
	return out, rows.Err()
}
