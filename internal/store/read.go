package store

import (
	"context"
	"database/sql"
)

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,slug,title,COALESCE(root_project,''),COALESCE(created_seq,0)
		 FROM projects ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.RootProject, &p.CreatedSeq); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) ProjectBySlug(ctx context.Context, slug string) (*Project, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,slug,title,COALESCE(root_project,''),COALESCE(created_seq,0)
		 FROM projects WHERE id=?`, projectID(slug))
	var p Project
	if err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.RootProject, &p.CreatedSeq); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) Events(ctx context.Context, slug, branch string) ([]EventRec, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,id,COALESCE(parent,''),project,branch,kind,at,COALESCE(actor,''),payload
		 FROM events WHERE project=? AND branch=? ORDER BY seq`,
		projectID(slug), orDefault(branch, "main"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// EventsAsOf returns the timeline prefix up to and including seq, used to
// reopen any historical view without overwriting it with latest results.
func (s *Store) EventsAsOf(ctx context.Context, slug, branch string, seq int) ([]EventRec, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,id,COALESCE(parent,''),project,branch,kind,at,COALESCE(actor,''),payload
		 FROM events WHERE project=? AND branch=? AND seq<=? ORDER BY seq`,
		projectID(slug), orDefault(branch, "main"), seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

type rowsScanner interface {
	Next() bool
	Scan(...any) error
}

func scanEvents(rows *sql.Rows) ([]EventRec, error) {
	var out []EventRec
	for rows.Next() {
		var e EventRec
		var payload string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Parent, &e.Project, &e.Branch,
			&e.Kind, &e.At, &e.Actor, &payload); err != nil {
			return nil, err
		}
		out = append(out, e)
		if payload != "" {
			_ = jsonUnmarshal(payload, &e.Payload)
			out[len(out)-1].Payload = e.Payload
		}
	}
	return out, nil
}

// Sources returns all committed source versions (parent chain included).
func (s *Store) Sources(ctx context.Context, slug, branch string) ([]SourceVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project,branch,label,version,COALESCE(parent_blob,''),blob,birth_seq
		 FROM sources WHERE project=? AND branch=? ORDER BY label,version`,
		projectID(slug), orDefault(branch, "main"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceVersion
	for rows.Next() {
		var v SourceVersion
		if err := rows.Scan(&v.Project, &v.Branch, &v.Label, &v.Version,
			&v.ParentBlob, &v.Blob, &v.BirthSeq); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) Rules(ctx context.Context, slug, branch string) ([]RuleVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project,branch,scope,version,budget_json,fingerprint,COALESCE(note,''),birth_seq
		 FROM rules WHERE project=? AND branch=? ORDER BY scope,version`,
		projectID(slug), orDefault(branch, "main"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuleVersion
	for rows.Next() {
		var r RuleVersion
		var budgetJSON string
		if err := rows.Scan(&r.Project, &r.Branch, &r.Scope, &r.Version,
			&budgetJSON, &r.Fingerprint, &r.Note, &r.BirthSeq); err != nil {
			return nil, err
		}
		_ = jsonUnmarshal(budgetJSON, &r.Budget)
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) Decisions(ctx context.Context, slug, branch string) ([]Decision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT project,branch,candidate,version,action,params_json,COALESCE(note,''),birth_seq
		 FROM decisions WHERE project=? AND branch=? ORDER BY candidate,version`,
		projectID(slug), orDefault(branch, "main"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		var params string
		if err := rows.Scan(&d.Project, &d.Branch, &d.Candidate, &d.Version,
			&d.Action, &params, &d.Note, &d.BirthSeq); err != nil {
			return nil, err
		}
		_ = jsonUnmarshal(params, &d.Params)
		out = append(out, d)
	}
	return out, nil
}

// FactView is one fact row with current/stale evaluation.
type FactView struct {
	Fact
	Current bool `json:"current"`
}

// FactsAt returns facts visible at a point in history. A fact is "current" iff
// it was born by seq, its source blob is still the latest committed at seq and
// its rule fingerprint matches the active rule at seq.
func (s *Store) FactsAt(ctx context.Context, slug, branch string, seq int) ([]FactView, error) {
	pid := projectID(slug)
	br := orDefault(branch, "main")
	rows, err := s.db.QueryContext(ctx,
		`SELECT project,branch,scope,label,kind,key_id,data,source_blob,rule_fp,birth_seq
		 FROM facts WHERE project=? AND branch=? AND birth_seq<=?
		 ORDER BY label,kind,key_id,birth_seq`, pid, br, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FactView
	for rows.Next() {
		var f Fact
		if err := rows.Scan(&f.Project, &f.Branch, &f.Scope, &f.Label, &f.Kind,
			&f.KeyID, &f.Data, &f.SourceBlob, &f.RuleFP, &f.BirthSeq); err != nil {
			return nil, err
		}
		out = append(out, FactView{Fact: f})
	}
	// evaluate currentness with a second pass over state-at-seq
	if err := s.markCurrent(pid, br, seq, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) markCurrent(pid, br string, seq int, views []FactView) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	latestBlob := map[string]string{}
	latestAt := map[string]int{}
	rows, err := tx.Query(
		`SELECT label,blob,birth_seq FROM sources WHERE project=? AND branch=? AND birth_seq<=?`,
		pid, br, seq)
	if err != nil {
		return err
	}
	for rows.Next() {
		var label, blob string
		var bseq int
		if err := rows.Scan(&label, &blob, &bseq); err != nil {
			rows.Close()
			return err
		}
		if latestAt[label] <= bseq {
			latestBlob[label], latestAt[label] = blob, bseq
		}
	}
	rows.Close()
	activeFP := map[string]string{}
	ruleAt := map[string]int{}
	rrows, err := tx.Query(
		`SELECT scope,fingerprint,birth_seq FROM rules WHERE project=? AND branch=? AND birth_seq<=?`,
		pid, br, seq)
	if err != nil {
		return err
	}
	for rrows.Next() {
		var scope, fp string
		var bseq int
		if err := rrows.Scan(&scope, &fp, &bseq); err != nil {
			rrows.Close()
			return err
		}
		if ruleAt[scope] <= bseq {
			activeFP[scope], ruleAt[scope] = fp, bseq
		}
	}
	rrows.Close()
	defaultFP := activeFP["default"]
	for i := range views {
		v := &views[i]
		if latestBlob[v.Label] != v.SourceBlob {
			continue
		}
		fp := defaultFP
		if x, ok := activeFP[v.Scope]; ok {
			fp = x
		}
		v.Current = v.RuleFP == fp
	}
	return nil
}
