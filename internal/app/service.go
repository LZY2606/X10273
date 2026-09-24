// Package app is the application service: it appends immutable events,
// derives provenance-tagged objects with local invalidation, runs
// counterexample trials, records reviewer decisions, and exports/imports
// portable archives.
package app

import (
	"context"
	"sync"
	"time"

	"ifdroom/internal/clock"
	"ifdroom/internal/derive"
	"ifdroom/internal/model"
	"ifdroom/internal/store"
)

type AnalysisView = derive.Analysis

type Service struct {
	st  *store.Store
	clk clock.Clock
	mu  sync.Mutex
}

func New(st *store.Store, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Service{st: st, clk: clk}
}

func (s *Service) Clock() clock.Clock { return s.clk }

func (s *Service) now() time.Time { return s.clk.Now() }

// InitProject creates the single review project if absent (seq 0).
func (s *Service) InitProject(ctx context.Context, id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err := s.st.ProjectExists(ctx, id)
	if err != nil || exists {
		return err
	}
	e := model.Event{
		ID: EventID(id, 0), ProjectID: id, Seq: 0,
		Kind: model.KindProjectCreated, Time: s.now(),
		Actor: "system", Title: "Project created", Summary: name,
		Payload: map[string]any{"name": name},
	}
	if err := s.st.CreateProject(ctx, id, name, 0); err != nil {
		return err
	}
	return s.st.AppendEvent(ctx, store.EventRow{
		ProjectID: id, Seq: 0, ID: e.ID, Kind: string(e.Kind),
		TS: e.Time.Format(time.RFC3339Nano), ParentID: "",
		Actor: e.Actor, Title: e.Title, Summary: e.Summary, Payload: e.Payload,
	})
}
