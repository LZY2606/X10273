package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"ifdgraph/internal/clock"
	"ifdgraph/internal/derive"
	"ifdgraph/internal/model"
	"ifdgraph/internal/tiff"
)

// Params is the currently adopted rule surface.
type Params struct {
	ParseBudget     int  `json:"parseBudget"`
	RequireDeclared bool `json:"requireDeclaredCrs"`
}

// DefaultParams is the initial rule revision.
func DefaultParams() Params {
	return Params{ParseBudget: tiff.DefaultParams().StepBudget, RequireDeclared: derive.DefaultTransformParams().RequireDeclaredCRS}
}

// Derivation is the stored parse + transform output with provenance.
type Derivation struct {
	ID         string                   `json:"id"`
	ProjectID  string                   `json:"projectId"`
	EventID    string                   `json:"eventId"`
	SourceID   string                   `json:"sourceId"`
	ParseFP    string                   `json:"parseFp"`
	XformFP    string                   `json:"xformFp"`
	Graph      *model.ParseGraph        `json:"graph"`
	Transforms []derive.TransformResult `json:"transforms"`
	CreatedAt  string                   `json:"createdAt"`
}

// ReviseParams appends a parameter-revision event.
func (s *Store) ReviseParams(ctx context.Context, c *clock.Clock, projectID string, next Params, previousEventID, note string) (*Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ev, err := s.appendEvent(ctx, tx, c, projectID, "param_revision", "", ParamPayload{
		ParseBudget: next.ParseBudget, RequireDeclared: next.RequireDeclared,
		PreviousEventID: previousEventID, Note: note,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ev, nil
}

// ParamsAt replays parameter events up to and including seq. Import events
// do not change parameters; replay gives the rule revision in force at any
// historical point.
func (s *Store) ParamsAt(ctx context.Context, projectID string, seq int64) (Params, string, error) {
	params := DefaultParams()
	activeEvent := ""
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return params, "", err
	}
	for _, e := range events {
		if e.Seq > seq {
			break
		}
		if e.Type == "param_revision" {
			var p ParamPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return params, "", err
			}
			params = Params{ParseBudget: p.ParseBudget, RequireDeclared: p.RequireDeclared}
			activeEvent = e.ID
		}
	}
	return params, activeEvent, nil
}

// CurrentParams returns the latest rule revision.
func (s *Store) CurrentParams(ctx context.Context, projectID string) (Params, string, error) {
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return DefaultParams(), "", err
	}
	var seq int64
	for _, e := range events {
		seq = e.Seq
	}
	return s.ParamsAt(ctx, projectID, seq)
}

// RunDerivation parses a revision under the given rule revision, derives
// transform candidates and stores the result plus an event.
func (s *Store) RunDerivation(ctx context.Context, c *clock.Clock, projectID, revisionID string) (*Derivation, *Event, error) {
	rev, data, err := s.GetRevisionData(ctx, revisionID)
	if err != nil {
		return nil, nil, err
	}
	params, _, err := s.CurrentParams(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	pp := tiff.Params{StepBudget: params.ParseBudget}
	graph := tiff.Parse(rev.SourceID, rev.Rev, data, pp)
	graphID := model.GraphID(rev.SourceID, tiff.AlgoVersion(), pp.Fingerprint())
	tp := derive.TransformParams{RequireDeclaredCRS: params.RequireDeclared}
	xforms := derive.DeriveTransforms(graph, tp)

	graphJSON, err := json.Marshal(graph)
	if err != nil {
		return nil, nil, err
	}
	xJSON, err := json.Marshal(xforms)
	if err != nil {
		return nil, nil, err
	}
	ev, err := s.appendEvent(ctx, tx, c, projectID, "derived", rev.SourceID, DerivedPayload{
		DerivationID: graphID, SourceID: rev.SourceID, RevisionID: revisionID,
		ParseAlgo: tiff.AlgoVersion(), ParseFP: pp.Fingerprint(),
		XformAlgo: derive.TransformAlgoVersion(), XformFP: tp.Fingerprint(),
	})
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO derivations(id,project_id,event_id,source_id,parse_fp,transform_fp,graph_json,transforms_json,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		graphID, projectID, ev.ID, rev.SourceID, pp.Fingerprint(), tp.Fingerprint(),
		graphJSON, xJSON, ev.CreatedAt); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &Derivation{
		ID: graphID, ProjectID: projectID, EventID: ev.ID, SourceID: rev.SourceID,
		ParseFP: pp.Fingerprint(), XformFP: tp.Fingerprint(), Graph: graph,
		Transforms: xforms, CreatedAt: ev.CreatedAt,
	}, ev, nil
}

// LatestDerivation returns the newest derivation for a source.
func (s *Store) LatestDerivation(ctx context.Context, sourceID string) (*Derivation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,project_id,event_id,source_id,parse_fp,transform_fp,graph_json,transforms_json,created_at
		 FROM derivations WHERE source_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, sourceID)
	return scanDerivation(row)
}

// DerivationAt returns the newest derivation at or before seq (historical view).
func (s *Store) DerivationAt(ctx context.Context, projectID string, seq int64) (*Derivation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT d.id,d.project_id,d.event_id,d.source_id,d.parse_fp,d.transform_fp,d.graph_json,d.transforms_json,d.created_at
		 FROM derivations d JOIN events e ON e.id=d.event_id
		 WHERE d.project_id=? AND e.seq<=?
		 ORDER BY e.seq DESC, d.rowid DESC LIMIT 1`, projectID, seq)
	return scanDerivation(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDerivation(row rowScanner) (*Derivation, error) {
	var d Derivation
	var graphJSON, xJSON []byte
	if err := row.Scan(&d.ID, &d.ProjectID, &d.EventID, &d.SourceID, &d.ParseFP, &d.XformFP,
		&graphJSON, &xJSON, &d.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(graphJSON, &d.Graph); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(xJSON, &d.Transforms); err != nil {
		return nil, err
	}
	return &d, nil
}
