package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"ifdlantern/internal/digest"
	"ifdlantern/internal/tiff"
)

type CommitInput struct {
	Slug, Title, Label, Note, Actor string
	Data                            []byte
	Region                          []byte
	WideStart                       int64
	Virtual                         bool
}

type CommitResult struct {
	Project string
	Event   EventRec
	Sources []SourceVersion
	Report  *tiff.ParseReport
}

// ensureProject returns the project id, creating it on first use.
func (s *Store) ensureProject(tx *sql.Tx, slug, title, rootProject string, seq int) (string, error) {
	id := projectID(slug)
	if _, err := tx.Exec(
		`INSERT INTO projects(id, slug, title, root_project, created_seq)
		 VALUES(?,?,?,?,?) ON CONFLICT(slug) DO NOTHING`,
		id, slug, title, rootProject, seq); err != nil {
		return "", err
	}
	return id, nil
}

// CreateProject emits a project_created event.
func (s *Store) CreateProject(ctx context.Context, slug, title, actor, note string) (EventRec, error) {
	return s.appendEvent(ctx, EventRec{
		Project: projectID(slug), Branch: "main", Kind: "project_created", Actor: actor,
		Payload: map[string]any{"slug": slug, "title": title, "note": note},
	}, func(tx *sql.Tx, ev EventRec) error {
		_, err := tx.Exec(
			`INSERT INTO projects(id, slug, title, created_seq)
			 VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING`,
			ev.Project, slug, title, ev.Seq)
		return err
	})
}

// PutBlob stores immutable bytes.
func (s *Store) PutBlob(ctx context.Context, id string, data, region []byte, wideStart int64, virtual bool, note string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT OR IGNORE INTO blobs(id,data,wide_region,wide_start,virtual,note)
			 VALUES(?,?,?,?,?,?)`, id, data, region, wideStart, b2i(virtual), note)
		return err
	})
}

// CommitSource imports an immutable new version of a labelled source, derives
// its facts under the active rule, and emits one commit_source event.
func (s *Store) CommitSource(ctx context.Context, in CommitInput) (*CommitResult, error) {
	blobID := "blob-" + digest.Hash(in.Data)[:16]
	var res *CommitResult
	ev, err := s.appendEvent(ctx, EventRec{
		Project: projectID(in.Slug), Branch: "main", Kind: "commit_source", Actor: actorOr(in.Actor, "importer"),
		Payload: map[string]any{
			"label": in.Label, "blob": blobID, "note": in.Note, "virtual": in.Virtual,
			"bytes": len(in.Data),
		},
	}, func(tx *sql.Tx, ev EventRec) error {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO blobs(id,data,wide_region,wide_start,virtual,note)
			 VALUES(?,?,?,?,?,?)`, blobID, in.Data, in.Region, in.WideStart, b2i(in.Virtual), in.Note); err != nil {
			return err
		}
		if _, err := s.ensureProject(tx, in.Slug, in.Title, "", ev.Seq); err != nil {
			return err
		}
		parent, _ := s.latestSource(tx, ev.Project, ev.Branch, in.Label)
		ver := 1
		var parentBlob string
		if parent != nil {
			ver = parent.Version + 1
			parentBlob = parent.Blob
		}
		if _, err := tx.Exec(
			`INSERT INTO sources(project,branch,label,version,parent_blob,blob,birth_seq)
			 VALUES(?,?,?,?,?,?,?)`,
			ev.Project, ev.Branch, in.Label, ver, parentBlob, blobID, ev.Seq); err != nil {
			return err
		}
		// derive under the active rule revision
		rule, err := s.activeRule(tx, ev.Project, ev.Branch, in.Label)
		if err != nil {
			return err
		}
		rep, err := s.derive(tx, ev.Project, ev.Branch, in.Label, blobID, rule, ev.Seq)
		if err != nil {
			return err
		}
		res = &CommitResult{Project: ev.Project, Event: ev, Report: rep}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		res = &CommitResult{Project: ev.Project, Event: ev}
	}
	return res, nil
}

func (s *Store) latestSource(tx *sql.Tx, project, branch, label string) (*SourceVersion, error) {
	row := tx.QueryRow(
		`SELECT project,branch,label,version,COALESCE(parent_blob,''),blob,birth_seq
		 FROM sources WHERE project=? AND branch=? AND label=?
		 ORDER BY version DESC LIMIT 1`, project, branch, label)
	var sv SourceVersion
	if err := row.Scan(&sv.Project, &sv.Branch, &sv.Label, &sv.Version,
		&sv.ParentBlob, &sv.Blob, &sv.BirthSeq); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &sv, nil
}

// activeRule returns the latest revision covering label; absent explicit
// revisions it returns the default parser budget with a deterministic fp.
func (s *Store) activeRule(tx *sql.Tx, project, branch, label string) (RuleVersion, error) {
	row := tx.QueryRow(
		`SELECT scope,version,budget_json,fingerprint,COALESCE(note,''),birth_seq
		 FROM rules WHERE project=? AND branch=? AND scope IN ('default',?)
		 ORDER BY CASE scope WHEN ? THEN 0 ELSE 1 END, version DESC LIMIT 1`,
		project, branch, label, label)
	var scope string
	var ver, birthSeq int
	var budgetJSON, fp, note string
	err := row.Scan(&scope, &ver, &budgetJSON, &fp, &note, &birthSeq)
	if err == sql.ErrNoRows {
		cfg := tiff.Config{Budget: tiff.DefaultBudget()}
		return RuleVersion{Project: project, Branch: branch, Scope: "default",
			Version: 0, Budget: tiff.DefaultBudget(), Fingerprint: cfg.Fingerprint(), BirthSeq: 0}, nil
	}
	if err != nil {
		return RuleVersion{}, err
	}
	var b tiff.Budget
	if err := json.Unmarshal([]byte(budgetJSON), &b); err != nil {
		return RuleVersion{}, err
	}
	return RuleVersion{Project: project, Branch: branch, Scope: scope, Version: ver,
		Budget: b, Fingerprint: fp, Note: note, BirthSeq: birthSeq}, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
func actorOr(a, def string) string {
	if a == "" {
		return def
	}
	return a
}

var _ = fmt.Sprintf
