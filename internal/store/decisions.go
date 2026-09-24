package store

import (
	"context"

	"ifdgraph/internal/clock"
)

// Decision is a recorded human verdict on a candidate/object.
type Decision struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	EventID   string `json:"eventId"`
	ObjectID  string `json:"objectId"`
	Action    string `json:"action"`
	Rationale string `json:"rationale"`
	CreatedAt string `json:"createdAt"`
}

// RecordDecision appends a decision event. An undo links the event it
// reverses; the historical view at the old event remains unchanged.
func (s *Store) RecordDecision(ctx context.Context, c *clock.Clock, projectID string, p DecisionPayload) (*Decision, *Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	ev, err := s.appendEvent(ctx, tx, c, projectID, "decision", p.ObjectID, p)
	if err != nil {
		return nil, nil, err
	}
	d := &Decision{
		ID: ev.ID, ProjectID: projectID, EventID: ev.ID,
		ObjectID: p.ObjectID, Action: p.Action, Rationale: p.Rationale,
		CreatedAt: ev.CreatedAt,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO decisions(id,project_id,event_id,object_id,action,rationale,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.EventID, d.ObjectID, d.Action, d.Rationale, d.CreatedAt); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return d, ev, nil
}

// DecisionsAt returns decisions visible at a point in history, with undone
// decisions filtered out. Stale decisions (object produced by a since-changed
// derivation) are still returned; callers mark them stale by object id.
func (s *Store) DecisionsAt(ctx context.Context, projectID string, seq int64) ([]DecisionPayload, error) {
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var active []DecisionPayload
	undone := map[int64]bool{}
	for _, e := range events {
		if e.Seq > seq {
			break
		}
		if e.Type != "decision" {
			continue
		}
		var p DecisionPayload
		if err := decodePayload(e.Payload, &p); err != nil {
			return nil, err
		}
		p.EventSeq = e.Seq
		if p.Action == "undo" && p.Undoes != "" {
			undone[targetSeq(p.Undoes)] = true
			continue
		}
		active = append(active, p)
	}
	var out []DecisionPayload
	for _, p := range active {
		if undone[p.EventSeq] {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
