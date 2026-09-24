package app

import (
	"context"
	"time"

	"ifdroom/internal/model"
	"ifdroom/internal/store"
)

func (s *Service) appendEventLocked(ctx context.Context, projectID string, kind model.Kind,
	actor, title, summary string, payload map[string]any) (model.Event, error) {
	seq, err := s.st.NextSeq(ctx, projectID)
	if err != nil {
		return model.Event{}, err
	}
	parent := ""
	if seq > 0 {
		parent = EventID(projectID, seq-1)
	}
	e := model.Event{ID: EventID(projectID, seq), ProjectID: projectID, Seq: seq,
		Kind: kind, Time: s.now(), ParentID: parent, Actor: actor,
		Title: title, Summary: summary, Payload: payload}
	err = s.st.AppendEvent(ctx, store.EventRow{
		ProjectID: projectID, Seq: seq, ID: e.ID, Kind: string(kind),
		TS: e.Time.Format(time.RFC3339Nano), ParentID: parent,
		Actor: actor, Title: title, Summary: summary, Payload: payload,
	})
	return e, err
}

// appendAt imports a history event at an exact seq/time (archive replay).
func (s *Service) appendAt(ctx context.Context, e model.Event) error {
	return s.st.AppendEvent(ctx, store.EventRow{
		ProjectID: e.ProjectID, Seq: e.Seq, ID: e.ID, Kind: string(e.Kind),
		TS: e.Time.UTC().Format(time.RFC3339Nano), ParentID: e.ParentID,
		Actor: e.Actor, Title: e.Title, Summary: e.Summary, Payload: e.Payload,
	})
}
