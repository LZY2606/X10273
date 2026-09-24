package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"ifdgraph/internal/clock"
	"ifdgraph/internal/model"
)

// ErrNotFound is returned for unknown projects/objects.
var ErrNotFound = errors.New("not found")

// CreateProject makes a named workspace.
func (s *Store) CreateProject(ctx context.Context, c *clock.Clock, name string) (*Project, error) {
	p := &Project{ID: model.ProjectID(name), Name: name, CreatedAt: c.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id,name,created_at) VALUES(?,?,?)`, p.ID, p.Name, p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProject loads a project.
func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,created_at FROM projects WHERE id=?`, id)
	var p Project
	if err := row.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// ListProjects returns all projects.
func (s *Store) ListProjects(ctx context.Context) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) nextSeq(ctx context.Context, projectID string) (int64, error) {
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id=?`, projectID).Scan(&max); err != nil {
		return 0, err
	}
	return max.Int64 + 1, nil
}

func (s *Store) lastEventID(ctx context.Context, projectID string) (string, error) {
	var id sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id.String, err
}

// appendEvent writes one history event inside the given tx.
func (s *Store) appendEvent(ctx context.Context, tx *sql.Tx, c *clock.Clock, projectID, typ, subject string, payload any) (*Event, error) {
	raw := mustJSON(payload)
	parent, err := lastEventIDTx(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	seq, err := nextSeqTx(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	id := model.EventID(projectID, seq, typ, raw, parent)
	ev := &Event{
		ID: id, ProjectID: projectID, Seq: seq, ParentID: parent,
		Type: typ, SubjectID: subject, Payload: raw,
		CreatedAt: c.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO events(id,project_id,seq,parent_id,type,subject_id,payload,created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		ev.ID, ev.ProjectID, ev.Seq, nullStr(ev.ParentID), ev.Type, ev.SubjectID, ev.Payload, ev.CreatedAt)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func nextSeqTx(ctx context.Context, tx *sql.Tx, projectID string) (int64, error) {
	var max sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id=?`, projectID).Scan(&max); err != nil {
		return 0, err
	}
	return max.Int64 + 1, nil
}

func lastEventIDTx(ctx context.Context, tx *sql.Tx, projectID string) (string, error) {
	var id sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id.String, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SubmitRevision stores an immutable input blob, linked to its parent. Raw
// input can never be overwritten: submitting against an existing revision
// always creates rev+1.
func (s *Store) SubmitRevision(ctx context.Context, c *clock.Clock, projectID, filename, note string, data []byte, parentRev int) (*Revision, *Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var parentID string
	rev := 1
	if parentRev > 0 {
		row := tx.QueryRowContext(ctx, `SELECT id FROM revisions WHERE project_id=? AND rev=?`, projectID, parentRev)
		if err := row.Scan(&parentID); err != nil {
			return nil, nil, fmt.Errorf("parent revision %d: %w", parentRev, err)
		}
		rev = parentRev + 1
	} else {
		var max sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(rev) FROM revisions WHERE project_id=?`, projectID).Scan(&max); err != nil {
			return nil, nil, err
		}
		if max.Valid {
			return nil, nil, fmt.Errorf("project already has revisions; parent revision required")
		}
	}
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	sourceID := model.SourceID(projectID, rev, data)
	revID := model.RevisionID(projectID, rev, data, parentID)
	r := &Revision{
		ID: revID, ProjectID: projectID, Rev: rev, ParentRevision: parentID,
		SourceID: sourceID, Filename: filename, Note: note, SHA256: hexSum,
		Size: int64(len(data)), CreatedAt: c.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO revisions(id,project_id,rev,parent_revision_id,source_id,filename,note,sha256,size,data,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.Rev, nullStr(r.ParentRevision), r.SourceID, r.Filename, r.Note,
		r.SHA256, r.Size, data, r.CreatedAt); err != nil {
		return nil, nil, err
	}
	ev, err := s.appendEvent(ctx, tx, c, projectID, "import", sourceID, ImportPayload{
		RevisionID: revID, Rev: rev, Parent: parentID, SourceID: sourceID,
		Filename: filename, SHA256: hexSum, Size: r.Size, Note: note,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return r, ev, nil
}

// ListRevisions returns the revision chain in order.
func (s *Store) ListRevisions(ctx context.Context, projectID string) ([]*Revision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,rev,COALESCE(parent_revision_id,''),source_id,filename,note,sha256,size,created_at
		 FROM revisions WHERE project_id=? ORDER BY rev`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Rev, &r.ParentRevision, &r.SourceID,
			&r.Filename, &r.Note, &r.SHA256, &r.Size, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// LatestRevision returns the newest revision.
func (s *Store) LatestRevision(ctx context.Context, projectID string) (*Revision, error) {
	rs, err := s.ListRevisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, ErrNotFound
	}
	return rs[len(rs)-1], nil
}

// GetRevisionData returns the immutable bytes of a revision.
func (s *Store) GetRevisionData(ctx context.Context, revisionID string) (*Revision, []byte, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,project_id,rev,COALESCE(parent_revision_id,''),source_id,filename,note,sha256,size,created_at,data
		 FROM revisions WHERE id=?`, revisionID)
	var r Revision
	var data []byte
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Rev, &r.ParentRevision, &r.SourceID,
		&r.Filename, &r.Note, &r.SHA256, &r.Size, &r.CreatedAt, &data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	return &r, data, nil
}

// Events returns the full history in order.
func (s *Store) Events(ctx context.Context, projectID string) ([]*Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,seq,COALESCE(parent_id,''),type,COALESCE(subject_id,''),payload,created_at
		 FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Seq, &e.ParentID, &e.Type, &e.SubjectID, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// EventAt returns a single history event.
func (s *Store) EventAt(ctx context.Context, projectID string, seq int64) (*Event, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,project_id,seq,COALESCE(parent_id,''),type,COALESCE(subject_id,''),payload,created_at
		 FROM events WHERE project_id=? AND seq=?`, projectID, seq)
	var e Event
	if err := row.Scan(&e.ID, &e.ProjectID, &e.Seq, &e.ParentID, &e.Type, &e.SubjectID, &e.Payload, &e.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func decodeEvent[T any](e *Event) (*T, error) {
	var v T
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
