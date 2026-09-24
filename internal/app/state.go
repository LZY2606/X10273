package app

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"ifdroom/internal/derive"
	"ifdroom/internal/model"
	"ifdroom/internal/store"
)

type VersionView struct {
	ID         string `json:"id"`
	DocID      string `json:"docId"`
	Version    int    `json:"version"`
	ParentID   string `json:"parentId"`
	SHA        string `json:"sha"`
	Size       int64  `json:"size"`
	Label      string `json:"label"`
	Origin     string `json:"origin"`
	ImportOK   bool   `json:"importOk"`
	ParseError string `json:"parseError,omitempty"`
	Seq        int64  `json:"createdSeq"`
}

type DerivedView struct {
	ID        string        `json:"id"`
	VersionID string        `json:"versionId"`
	Kind      string        `json:"kind"`
	Seq       int64         `json:"producedSeq"`
	ParamFP   string        `json:"paramFingerprint"`
	Current   bool          `json:"current"`
	Stale     bool          `json:"stale"`
	Analysis  *AnalysisView `json:"analysis,omitempty"`
}

type State struct {
	ProjectID string                 `json:"projectId"`
	Name      string                 `json:"name"`
	Params    derive.Params          `json:"params"`
	Events    []model.Event          `json:"events"`
	Versions  []VersionView          `json:"versions"`
	Derived   []DerivedView          `json:"derived"`
	Decisions []model.Decision       `json:"decisions"`
	Trials    []model.Trial          `json:"trials"`
	Criteria  []model.CriterionEvent `json:"criteria"`
	AtSeq     int64                  `json:"atSeq"`
}

func (s *Service) LoadState(ctx context.Context, projectID string, atSeq int64) (*State, error) {
	rows, err := s.st.ListEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	st := &State{ProjectID: projectID, Params: derive.DefaultParams(), AtSeq: atSeq}
	paramSeq := int64(-1)
	heads := map[string]string{} // doc -> version id (latest import event <= at)
	headSeq := map[string]int64{}
	for _, r := range rows {
		if atSeq >= 0 && r.Seq > atSeq {
			continue
		}
		e := rowToEvent(r)
		st.Events = append(st.Events, e)
		st.Name = e.Summary
		switch e.Kind {
		case model.KindProjectCreated:
			if n, ok := e.Payload["name"].(string); ok {
				st.Name = n
			}
		case model.KindParamRevision:
			st.Params = paramsFromPayload(e.Payload)
			paramSeq = e.Seq
		case model.KindBlobImported, model.KindSourceMutation:
			doc, _ := e.Payload["docId"].(string)
			vid, _ := e.Payload["versionId"].(string)
			if vid != "" {
				heads[doc] = vid
				headSeq[doc] = e.Seq
			}
		case model.KindTrialRun:
			if t, ok := trialFromEvent(e); ok {
				st.Trials = append(st.Trials, t)
			}
		case model.KindDecision:
			if d, ok := decisionFromEvent(e); ok {
				st.Decisions = append(st.Decisions, d)
			}
		case model.KindDecisionRevoked:
			target, _ := e.Payload["targetId"].(string)
			for i := range st.Decisions {
				if st.Decisions[i].TargetID == target {
					st.Decisions[i].Active = false
					st.Decisions[i].RevokedBy = e.ID
				}
			}
		}
	}
	if atSeq >= 0 && len(st.Events) > 0 {
		st.AtSeq = st.Events[len(st.Events)-1].Seq
	}
	// Versions.
	vrs, err := s.st.ListVersions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range vrs {
		if atSeq >= 0 && v.CreatedSeq > st.AtSeq {
			continue
		}
		st.Versions = append(st.Versions, VersionView{
			ID: v.ID, DocID: v.DocID, Version: int(v.Version), ParentID: v.ParentID,
			SHA: v.SHA, Label: v.Label, Origin: v.Origin, ImportOK: v.ImportOK,
			ParseError: v.ParseError, Seq: v.CreatedSeq,
		})
	}
	// Derived cache, then mark currency.
	drs, err := s.st.ListDerived(ctx, projectID)
	if err != nil {
		return nil, err
	}
	currentFP := st.Params.Fingerprint()
	for _, d := range drs {
		if atSeq >= 0 && d.ProducedSeq > st.AtSeq {
			continue
		}
		dv := DerivedView{ID: d.ID, VersionID: d.VersionID, Kind: d.Kind,
			Seq: d.ProducedSeq, ParamFP: d.ParamFP, Stale: d.Stale}
		var ver *VersionView
		for i := range st.Versions {
			if st.Versions[i].ID == d.VersionID {
				ver = &st.Versions[i]
			}
		}
		if ver != nil {
			headAt, isHead := headSeq[ver.DocID], heads[ver.DocID] == ver.ID
			paramsFresh := d.ProducedSeq >= paramSeq
			_ = headAt
			dv.Current = isHead && d.ParamFP == currentFP && paramsFresh
			if !dv.Current {
				dv.Stale = true
			}
		}
		if dv.Current || atSeq < 0 || true {
			var an AnalysisView
			if json.Unmarshal(d.Payload, &an) == nil {
				dv.Analysis = &an
			}
		}
		st.Derived = append(st.Derived, dv)
	}
	sort.SliceStable(st.Derived, func(i, j int) bool {
		if st.Derived[i].VersionID == st.Derived[j].VersionID {
			return st.Derived[i].Seq < st.Derived[j].Seq
		}
		return st.Derived[i].VersionID < st.Derived[j].VersionID
	})
	dcs, err := s.st.ListDecisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, d := range dcs {
		seq, _ := strconv.ParseInt(firstNum(d.EventID), 10, 64)
		_ = seq
		ts, _ := time.Parse(time.RFC3339Nano, d.TS)
		active := d.Active
		if atSeq >= 0 {
			// find revocation
			active = active && !revokedBefore(st.Events, d.TargetID, d.EventID)
		}
		st.Decisions = appendIfMissingDecision(st.Decisions, model.Decision{
			ID: d.ID, EventID: d.EventID, TargetKind: d.TargetKind, TargetID: d.TargetID,
			Verdict: d.Verdict, Reason: d.Reason, Reviewer: d.Reviewer, Time: ts,
			RevokedBy: d.RevokedBy, Active: active,
		})
	}
	sort.SliceStable(st.Decisions, func(i, j int) bool { return st.Decisions[i].Time.Before(st.Decisions[j].Time) })
	// Criteria events from the log.
	for _, e := range st.Events {
		if e.Kind == model.KindTrialRun || e.Kind == model.KindDecision {
			if raw, ok := e.Payload["criteria"]; ok {
				items := criteriaFromAny(raw, e.ID)
				st.Criteria = append(st.Criteria, items...)
			}
		}
	}
	return st, nil
}

func revokedBefore(evts []model.Event, targetID, decisionEventID string) bool {
	revoked := false
	decisionSeen := false
	for _, e := range evts {
		if e.ID == decisionEventID {
			decisionSeen = true
		}
		if decisionSeen && e.Kind == model.KindDecisionRevoked {
			if t, ok := e.Payload["targetId"].(string); ok && t == targetID {
				revoked = true
			}
		}
	}
	return revoked
}

func appendIfMissingDecision(ds []model.Decision, d model.Decision) []model.Decision {
	for _, x := range ds {
		if x.ID == d.ID {
			return ds
		}
	}
	return append(ds, d)
}

func firstNum(id string) string {
	dash := 0
	start := -1
	for i := 0; i < len(id); i++ {
		if id[i] == '-' {
			dash++
			continue
		}
		if dash == 1 && id[i] >= '0' && id[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return "0"
	}
	end := start
	for end < len(id) && id[end] >= '0' && id[end] <= '9' {
		end++
	}
	return id[start:end]
}

func rowToEvent(r store.EventRow) model.Event {
	return model.Event{ID: r.ID, ProjectID: r.ProjectID, Seq: r.Seq,
		Kind: model.Kind(r.Kind), Time: parseTime(r.TS), ParentID: r.ParentID,
		Actor: r.Actor, Title: r.Title, Summary: r.Summary, Payload: r.Payload}
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
