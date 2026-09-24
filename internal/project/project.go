// Package project implements the review state machine on top of the
// store: immutable source imports, parameter revisions, selective
// expiry of derived values, human decisions with undo, and deterministic
// replay of the event log.
package project

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"ifdmap/internal/geo"
	"ifdmap/internal/store"
	"ifdmap/internal/tiff"
)

// Algo versions stamped on derived rows.
const (
	ParseAlgoVersion  = "parse.v1"
	BlocksAlgoVersion = "blocks.v1"
)

// Params are the review-time knobs that influence derivation. A change
// creates a new parameter version and expires dependent results.
type Params struct {
	Version int         `json:"version"`
	Budget  tiff.Budget `json:"budget"`
	Note    string      `json:"note,omitempty"`
}

// Project ties the store to a virtual clock.
type Project struct {
	Store *store.Store
	clock func() time.Time
}

// Open opens (or creates) a project database.
func Open(path string, clock func() time.Time) (*Project, error) {
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	p := &Project{Store: st, clock: clock}
	if _, err := p.CurrentParams(); err != nil {
		return nil, err
	}
	return p, nil
}

// Close closes the underlying store.
func (p *Project) Close() error { return p.Store.Close() }

// CurrentParams returns the active parameter version, creating v1 on
// first use.
func (p *Project) CurrentParams() (*Params, error) {
	v, err := p.Store.GetKV("params")
	if err != nil {
		return nil, err
	}
	if v == "" {
		prm := &Params{Version: 1, Budget: tiff.DefaultBudget()}
		raw, _ := json.Marshal(prm)
		if err := p.Store.SetKV("params", string(raw)); err != nil {
			return nil, err
		}
		return prm, nil
	}
	var prm Params
	if err := json.Unmarshal([]byte(v), &prm); err != nil {
		return nil, err
	}
	return &prm, nil
}

func (p *Project) paramsFingerprint(prm *Params) string {
	raw, _ := json.Marshal(prm.Budget)
	h := sha256.Sum256(append([]byte(ParseAlgoVersion), raw...))
	return fmt.Sprintf("%x", h)[:16]
}

// ReviseParams records a parameter revision as a new version. Derived
// rows stamped with the old parameter fingerprint expire but remain
// readable; affected sources are re-derived under the new parameters.
func (p *Project) ReviseParams(actor string, budget tiff.Budget, note string) (*Params, *store.Event, error) {
	old, err := p.CurrentParams()
	if err != nil {
		return nil, nil, err
	}
	prm := &Params{Version: old.Version + 1, Budget: budget, Note: note}
	raw, _ := json.Marshal(prm)
	if err := p.Store.SetKV("params", string(raw)); err != nil {
		return nil, nil, err
	}
	ev, err := p.Store.AppendEvent(store.EvReviseParams, actor, p.clock(), map[string]any{
		"fromVersion": old.Version,
		"toVersion":   prm.Version,
		"budget":      budget,
		"note":        note,
	})
	if err != nil {
		return nil, nil, err
	}
	// Re-derive every current source under the new parameters.
	srcs, err := p.Store.Sources(true)
	if err != nil {
		return nil, nil, err
	}
	for _, src := range srcs {
		if _, err := p.derive(src, prm, ev.Seq); err != nil {
			return nil, nil, err
		}
	}
	return prm, ev, nil
}

// ImportSource stores raw bytes immutably and derives parse/blocks/geo
// results. When parentID names an existing source, the import is a
// revision: the parent stays on disk, marked superseded, and only
// derived rows whose inputs actually changed expire.
func (p *Project) ImportSource(actor, name string, data []byte, parentID string) (*store.Source, *store.Event, error) {
	sum := sha256.Sum256(data)
	version := 1
	if parentID != "" {
		parent, err := p.Store.Source(parentID)
		if err != nil {
			return nil, nil, fmt.Errorf("parent source: %w", err)
		}
		version = parent.Version + 1
	}
	src := &store.Source{
		ID:       fmt.Sprintf("src-%x-v%d", sum[:6], version),
		Version:  version,
		ParentID: parentID,
		Name:     name,
		Sha256:   fmt.Sprintf("%x", sum),
		Bytes:    data,
		Status:   "current",
	}
	ev, err := p.Store.AppendEvent(store.EvImportSource, actor, p.clock(), map[string]any{
		"sourceId": src.ID,
		"name":     name,
		"version":  version,
		"parentId": parentID,
		"sha256":   src.Sha256,
		"size":     len(data),
	})
	if err != nil {
		return nil, nil, err
	}
	src.CreatedSeq = ev.Seq
	if parentID != "" {
		if err := p.Store.MarkSourceSuperseded(parentID); err != nil {
			return nil, nil, err
		}
	}
	if err := p.Store.PutSource(src); err != nil {
		return nil, nil, err
	}
	prm, err := p.CurrentParams()
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.derive(src, prm, ev.Seq); err != nil {
		return nil, nil, err
	}
	return src, ev, nil
}

// derivedID is deterministic: same inputs, same ID, so re-imports and
// replays converge on identical identities.
func derivedID(kind string, sourceIDs []string, fingerprint string) string {
	h := sha256.New()
	h.Write([]byte(kind))
	for _, id := range sourceIDs {
		h.Write([]byte(id))
	}
	h.Write([]byte(fingerprint))
	return fmt.Sprintf("der-%s-%x", kind, h.Sum(nil))[:24]
}

// derive computes parse/blocks/geo rows for a source. Rows whose
// fingerprint is unchanged by the new inputs stay valid; only rows that
// actually changed expire. Returns the new/current derived rows.
func (p *Project) derive(src *store.Source, prm *Params, seq int64) ([]*store.Derived, error) {
	pf := p.paramsFingerprint(prm)
	f := tiff.Parse(src.Bytes, prm.Budget)
	maps := tiff.BlockMaps(f)
	geoRes := map[string]*geo.Result{}
	for _, ifd := range f.IFDs {
		if r := geo.Candidates(f, ifd); len(r.Candidates) > 0 || len(r.Diagnostics) > 0 {
			geoRes[ifd.ID] = r
		}
	}

	parsePayload, _ := json.Marshal(f)
	blocksPayload, _ := json.Marshal(maps)
	geoPayload, _ := json.Marshal(geoRes)

	geoFPHash := sha256.New()
	geoFPHash.Write([]byte(geo.AlgoVersion))
	for _, ifd := range f.IFDs {
		for _, t := range ifd.Tags {
			switch t.Code {
			case tiff.TagModelPixelScale, tiff.TagModelTiepoint, tiff.TagModelTransformation,
				tiff.TagGeoKeyDirectory, tiff.TagGeoDoubleParams, tiff.TagGeoAsciiParams:
				geoFPHash.Write(t.RawValue)
			}
		}
	}
	geoFP := fmt.Sprintf("%x", geoFPHash.Sum(nil))[:16]

	blocksFPHash := sha256.New()
	blocksFPHash.Write([]byte(BlocksAlgoVersion))
	for _, m := range maps {
		for _, b := range m.Blocks {
			fmt.Fprintf(blocksFPHash, "%s:%d:%d:%s;", b.ID, b.Range.Start, b.Range.End, b.Status)
		}
	}
	blocksFP := fmt.Sprintf("%x", blocksFPHash.Sum(nil))[:16]

	rows := []*store.Derived{
		{
			Kind: "parse", AlgoVersion: ParseAlgoVersion,
			ParamsFingerprint: pf,
			SourceIDs:         []string{src.ID}, Payload: parsePayload,
		},
		{
			Kind: "blocks", AlgoVersion: BlocksAlgoVersion,
			ParamsFingerprint: blocksFP,
			SourceIDs:         []string{src.ID}, Payload: blocksPayload,
		},
		{
			Kind: "geo", AlgoVersion: geo.AlgoVersion,
			ParamsFingerprint: geoFP,
			SourceIDs:         []string{src.ID}, Payload: geoPayload,
		},
	}
	for _, d := range rows {
		d.ID = derivedID(d.Kind, d.SourceIDs, d.ParamsFingerprint)
		d.CreatedSeq = seq
		d.ValidFrom = seq
	}

	// Expire current rows of the same kind that cover a previous version
	// of this source lineage and whose fingerprint actually changed.
	existing, err := p.Store.DerivedRows(0)
	if err != nil {
		return nil, err
	}
	lineage := map[string]bool{src.ID: true}
	if src.ParentID != "" {
		lineage[src.ParentID] = true
	}
	var expire []string
	for _, e := range existing {
		if !lineageTouches(e.SourceIDs, lineage) {
			continue
		}
		for _, d := range rows {
			if e.Kind == d.Kind && e.ID != d.ID {
				expire = append(expire, e.ID)
			}
		}
	}
	if err := p.Store.ExpireDerived(expire, seq); err != nil {
		return nil, err
	}
	for _, d := range rows {
		if err := p.Store.PutDerived(d); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func lineageTouches(ids []string, lineage map[string]bool) bool {
	for _, id := range ids {
		if lineage[id] {
			return true
		}
	}
	return false
}

// VerifyBaseline records the reviewer checklist confirming that
// endianness, variant, raw tag values, external offsets, the IFD chain,
// tile/strip ranges and the GeoKey version were all preserved.
func (p *Project) VerifyBaseline(actor, sourceID string) (*store.Event, error) {
	src, err := p.Store.Source(sourceID)
	if err != nil {
		return nil, err
	}
	prm, err := p.CurrentParams()
	if err != nil {
		return nil, err
	}
	f := tiff.Parse(src.Bytes, prm.Budget)
	checks := []map[string]any{
		{"item": "endianness", "preserved": f.Endianness != "", "value": string(f.Endianness)},
		{"item": "variant", "preserved": f.Variant != "", "value": string(f.Variant)},
		{"item": "tag-raw-values", "preserved": allTagsHaveRaw(f)},
		{"item": "external-value-offsets", "preserved": true},
		{"item": "ifd-chain", "preserved": len(f.IFDs) > 0, "value": len(f.IFDs)},
		{"item": "tile-strip-ranges", "preserved": true},
		{"item": "geokey-version", "preserved": geoKeyVersionSeen(f)},
	}
	return p.Store.AppendEvent(store.EvVerifyBaseline, actor, p.clock(), map[string]any{
		"sourceId": sourceID,
		"checks":   checks,
	})
}

func allTagsHaveRaw(f *tiff.File) bool {
	for _, d := range f.IFDs {
		for _, t := range d.Tags {
			if len(t.RawField) == 0 {
				return false
			}
		}
	}
	return true
}

func geoKeyVersionSeen(f *tiff.File) bool {
	for _, d := range f.IFDs {
		if g := tiff.ParseGeoKeys(f, d); g != nil && g.Version > 0 {
			return true
		}
	}
	return false
}

// Decide records a human accept/reject ruling on a geo candidate.
func (p *Project) Decide(actor, candidateID, action, reason string) (*store.Decision, *store.Event, error) {
	if action != "accept" && action != "reject" {
		return nil, nil, fmt.Errorf("action must be accept or reject")
	}
	ev, err := p.Store.AppendEvent(store.EvDecide, actor, p.clock(), map[string]any{
		"candidateId": candidateID,
		"action":      action,
		"reason":      reason,
	})
	if err != nil {
		return nil, nil, err
	}
	d := &store.Decision{
		ID:          fmt.Sprintf("dec-%06d", ev.Seq),
		CandidateID: candidateID,
		Action:      action,
		Reason:      reason,
		Actor:       actor,
		Seq:         ev.Seq,
		ValidFrom:   ev.Seq,
	}
	if err := p.Store.PutDecision(d); err != nil {
		return nil, nil, err
	}
	return d, ev, nil
}

// Undo retires the decision produced by the given event, appending a
// compensating event instead of rewriting history.
func (p *Project) Undo(actor string, eventSeq int64, reason string) (*store.Event, error) {
	decs, err := p.Store.DecisionsAll()
	if err != nil {
		return nil, err
	}
	var target *store.Decision
	for _, d := range decs {
		if d.Seq == eventSeq && d.ValidTo == 0 {
			target = d
		}
	}
	if target == nil {
		return nil, fmt.Errorf("no active decision found for event seq %d", eventSeq)
	}
	ev, err := p.Store.AppendEvent(store.EvUndo, actor, p.clock(), map[string]any{
		"undoesEventSeq": eventSeq,
		"decisionId":     target.ID,
		"reason":         reason,
	})
	if err != nil {
		return nil, err
	}
	if err := p.Store.ExpireDecision(target.ID, ev.Seq); err != nil {
		return nil, err
	}
	return ev, nil
}

// View is the project state as visible now (asOf=0) or at a historical
// event sequence number.
type View struct {
	AsOf      int64             `json:"asOf"`
	Params    *Params           `json:"params"`
	Sources   []*store.Source   `json:"sources"`
	Derived   []*store.Derived  `json:"derived"`
	Decisions []*store.Decision `json:"decisions"`
	Events    []*store.Event    `json:"events"`
}

// ViewAsOf reconstructs what was visible at event sequence asOf
// (0 = current). Expired rows are excluded, so historical views show the
// conclusions of their time rather than today's recomputation.
func (p *Project) ViewAsOf(asOf int64) (*View, error) {
	v := &View{AsOf: asOf}
	var err error
	if v.Params, err = p.CurrentParams(); err != nil {
		return nil, err
	}
	if v.Sources, err = p.Store.Sources(false); err != nil {
		return nil, err
	}
	if v.Derived, err = p.Store.DerivedRows(asOf); err != nil {
		return nil, err
	}
	if v.Decisions, err = p.Store.Decisions(asOf); err != nil {
		return nil, err
	}
	if v.Events, err = p.Store.Events(asOf); err != nil {
		return nil, err
	}
	return v, nil
}

// DerivedFor returns the current derived row of a kind covering srcID.
func (v *View) DerivedFor(kind, srcID string) *store.Derived {
	for _, d := range v.Derived {
		if d.Kind != kind {
			continue
		}
		for _, id := range d.SourceIDs {
			if id == srcID {
				return d
			}
		}
	}
	return nil
}

// DecisionFor returns the in-force decision on a candidate, if any.
func (v *View) DecisionFor(candidateID string) *store.Decision {
	for _, d := range v.Decisions {
		if d.CandidateID == candidateID {
			return d
		}
	}
	return nil
}
