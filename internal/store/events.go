package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"ifdlantern/internal/digest"
	"ifdlantern/internal/tiff"
)

// appendEvent persists one event. The event id is a hash of
// (parent-id, project, branch, kind, canonical-payload), so replaying the
// identical history anywhere restores ids and ordering.
func (s *Store) appendEvent(ctx context.Context, e EventRec, apply func(*sql.Tx, EventRec) error) (EventRec, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		parent, err := lastEventID(tx, e.Project, e.Branch)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`INSERT INTO events(parent,project,branch,kind,at,actor,payload)
			 VALUES(?,?,?,?,?,?,?)`,
			parent, e.Project, e.Branch, e.Kind,
			s.clock.Now().Format("2006-01-02T15:04:05Z07:00"),
			actorOr(e.Actor, "user"), mustJSON(e.Payload))
		if err != nil {
			return err
		}
		seq, _ := res.LastInsertId()
		id := eventID(parent, e.Project, e.Branch, e.Kind, e.Payload)
		if _, err := tx.Exec(`UPDATE events SET id=? WHERE seq=?`, id, seq); err != nil {
			return err
		}
		var at string
		row := tx.QueryRow(`SELECT at FROM events WHERE seq=?`, seq)
		if err := row.Scan(&at); err != nil {
			return err
		}
		e.Seq, e.ID, e.Parent, e.At = int(seq), id, parent, at
		if apply != nil {
			if err := apply(tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	return e, err
}

func mustJSON(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func lastEventID(tx *sql.Tx, project, branch string) (string, error) {
	var id string
	err := tx.QueryRow(
		`SELECT id FROM events WHERE project=? AND branch=? ORDER BY seq DESC LIMIT 1`,
		project, branch).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func eventID(parent, project, branch, kind string, payload map[string]any) string {
	return "evt-" + digest.Canonical([]any{parent, project, branch, kind, payload})[:16]
}

// ReviseInput updates parser parameters (e.g. the IFD visit budget). Only
// parser-derived facts under the scope are marked stale by changing their
// rule fingerprint; decisions are unaffected.
type ReviseInput struct {
	Slug, Branch, Scope, Note, Actor string
	Budget                           tiff.Budget
}

func (s *Store) ReviseRules(ctx context.Context, in ReviseInput) (EventRec, error) {
	budgetJSON, _ := json.Marshal(in.Budget)
	cfg := tiff.Config{Budget: in.Budget}
	fp := cfg.Fingerprint()
	branch := orDefault(in.Branch, "main")
	scope := orDefault(in.Scope, "default")
	return s.appendEvent(ctx, EventRec{
		Project: projectID(in.Slug), Branch: branch, Kind: "rule_revised",
		Actor: actorOr(in.Actor, "reviewer"),
		Payload: map[string]any{
			"scope": scope, "budget": in.Budget, "fingerprint": fp, "note": in.Note,
		},
	}, func(tx *sql.Tx, ev EventRec) error {
		ver, err := nextRuleVersion(tx, ev.Project, ev.Branch, scope)
		if err != nil {
			return err
		}
		_, err = tx.Exec(
			`INSERT INTO rules(project,branch,scope,version,budget_json,fingerprint,note,birth_seq)
			 VALUES(?,?,?,?,?,?,?,?)`,
			ev.Project, ev.Branch, scope, ver, string(budgetJSON), fp, in.Note, ev.Seq)
		if err != nil {
			return err
		}
		// Re-derive all current labels under this scope under the new rule.
		labels, err := currentLabels(tx, ev.Project, ev.Branch, scope)
		if err != nil {
			return err
		}
		for _, label := range labels {
			sv, err := s.latestSource(tx, ev.Project, ev.Branch, label)
			if err != nil || sv == nil {
				continue
			}
			rule := RuleVersion{Project: ev.Project, Branch: ev.Branch, Scope: scope,
				Version: ver, Budget: in.Budget, Fingerprint: fp, BirthSeq: ev.Seq}
			if _, err := s.derive(tx, ev.Project, ev.Branch, label, sv.Blob, rule, ev.Seq); err != nil {
				return err
			}
		}
		return nil
	})
}

func nextRuleVersion(tx *sql.Tx, project, branch, scope string) (int, error) {
	var v int
	err := tx.QueryRow(
		`SELECT COALESCE(MAX(version),0)+1 FROM rules WHERE project=? AND branch=? AND scope=?`,
		project, branch, scope).Scan(&v)
	return v, err
}

// currentLabels returns labels whose active rule matches scope.
func currentLabels(tx *sql.Tx, project, branch, scope string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT DISTINCT label FROM sources WHERE project=? AND branch=?`, project, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		if scope == "default" || l == scope {
			out = append(out, l)
		}
	}
	return out, nil
}

type DecisionInput struct {
	Slug, Branch, CandidateID, Action, Note, Actor string
	Params                                         map[string]any
}

// Decide records accept/reject/params on one candidate. Decisions never mutate
// candidates; a new decision version changes which candidate is live.
func (s *Store) Decide(ctx context.Context, in DecisionInput) (EventRec, error) {
	branch := orDefault(in.Branch, "main")
	if in.Action != "accept" && in.Action != "reject" && in.Action != "params" {
		return EventRec{}, fmt.Errorf("invalid decision action %q", in.Action)
	}
	return s.appendEvent(ctx, EventRec{
		Project: projectID(in.Slug), Branch: branch, Kind: "decision_made",
		Actor: actorOr(in.Actor, "reviewer"),
		Payload: map[string]any{
			"candidate": in.CandidateID, "action": in.Action,
			"params": orEmpty(in.Params), "note": in.Note,
		},
	}, func(tx *sql.Tx, ev EventRec) error {
		ver, err := nextDecisionVersion(tx, ev.Project, ev.Branch, in.CandidateID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(
			`INSERT INTO decisions(project,branch,candidate,version,action,params_json,note,birth_seq)
			 VALUES(?,?,?,?,?,?,?,?)`,
			ev.Project, ev.Branch, in.CandidateID, ver, in.Action,
			mustJSON(orEmpty(in.Params)), in.Note, ev.Seq)
		return err
	})
}

func nextDecisionVersion(tx *sql.Tx, project, branch, cand string) (int, error) {
	var v int
	err := tx.QueryRow(
		`SELECT COALESCE(MAX(version),0)+1 FROM decisions
		 WHERE project=? AND branch=? AND candidate=?`, project, branch, cand).Scan(&v)
	return v, err
}

// Fork creates a new branch at an event. Events are copied (same ids/order);
// sources/rules/facts/decisions up to that point are copied too. Subsequent
// work on the fork never disturbs the original line of review.
func (s *Store) Fork(ctx context.Context, slug, newSlug, newTitle, atEventID, actor, note string) (string, error) {
	newID := projectID(newSlug)
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		pid := projectID(slug)
		evs, err := eventsUntil(tx, pid, "main", atEventID)
		if err != nil {
			return err
		}
		if len(evs) == 0 {
			return fmt.Errorf("fork point %s not found", atEventID)
		}
		last := evs[len(evs)-1]
		// project row with link to root
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO projects(id,slug,title,root_project,created_seq)
			 VALUES(?,?,?,?,?)`, newID, newSlug, newTitle, pid, last.Seq); err != nil {
			return err
		}
		for _, ev := range evs {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO events(seq,id,parent,project,branch,kind,at,actor,payload)
				 VALUES(?,?,?,?,?,?,?,?,?)`,
				ev.Seq, ev.ID, ev.Parent, newID, "main", ev.Kind, ev.At, ev.Actor, mustJSON(ev.Payload)); err != nil {
				return err
			}
		}
		if err := copyBranchState(tx, pid, "main", newID, "main", last.Seq); err != nil {
			return err
		}
		return nil
	})
	return newID, err
}

func eventsUntil(tx *sql.Tx, project, branch, atID string) ([]EventRec, error) {
	rows, err := tx.Query(
		`SELECT seq,id,COALESCE(parent,''),kind,at,actor,payload FROM events
		 WHERE project=? AND branch=? ORDER BY seq`, project, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRec
	for rows.Next() {
		var e EventRec
		var payload string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Parent, &e.Kind, &e.At, &e.Actor, &payload); err != nil {
			return nil, err
		}
		e.Project, e.Branch = project, branch
		_ = json.Unmarshal([]byte(payload), &e.Payload)
		out = append(out, e)
		if e.ID == atID {
			break
		}
	}
	return out, nil
}

func copyBranchState(tx *sql.Tx, op, ob, np, nb string, upToSeq int) error {
	for _, q := range []string{
		`INSERT OR IGNORE INTO sources
		 SELECT ?,?,label,version,parent_blob,blob,birth_seq FROM sources
		 WHERE project=? AND branch=? AND birth_seq<=?`,
		`INSERT OR IGNORE INTO rules
		 SELECT ?,?,scope,version,budget_json,fingerprint,note,birth_seq FROM rules
		 WHERE project=? AND branch=? AND birth_seq<=?`,
		`INSERT OR IGNORE INTO facts
		 SELECT ?,?,scope,label,kind,key_id,data,source_blob,rule_fp,birth_seq FROM facts
		 WHERE project=? AND branch=? AND birth_seq<=?`,
		`INSERT OR IGNORE INTO decisions
		 SELECT ?,?,candidate,version,action,params_json,note,birth_seq FROM decisions
		 WHERE project=? AND branch=? AND birth_seq<=?`,
	} {
		if _, err := tx.Exec(q, np, nb, op, ob, upToSeq); err != nil {
			return err
		}
	}
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
