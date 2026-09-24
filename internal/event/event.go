// Package event defines the append-only, machine-readable provenance log.
//
// Every fact about an input, a derived object, a diagnostic or a human
// decision is an event. Raw inputs are immutable; revisions are expressed as
// new events with a parent link. Derived events carry an algorithm version, a
// parameter fingerprint and the identity of their direct sources.
package event

import "encoding/json"

// AlgoVersion is bumped whenever parser/block/geo derivation rules change.
const AlgoVersion = "ifdroom-algo-1.0.0"

// ArchiveVersion is the export container version.
const ArchiveVersion = "ifdroom-archive/v1"

type Kind string

const (
	KindProjectCreated   Kind = "project.created"
	KindInputCommitted   Kind = "input.committed"
	KindParamsRevised    Kind = "params.revised"
	KindAnalysisStarted  Kind = "analysis.started"
	KindHeaderRead       Kind = "analysis.header_read"
	KindIFDDiscovered    Kind = "analysis.ifd_discovered"
	KindIFDVisited       Kind = "analysis.ifd_visited"
	KindTagRead          Kind = "analysis.tag_read"
	KindBlockDerived     Kind = "analysis.block_derived"
	KindLayoutResult     Kind = "analysis.layout_result"
	KindGeoKeyDir        Kind = "analysis.geokey_dir"
	KindGeoCandidate     Kind = "analysis.geo_candidate"
	KindDiagnostic       Kind = "analysis.diagnostic"
	KindAnalysisComplete Kind = "analysis.complete"
	KindDecisionMade     Kind = "decision.made"
	KindDecisionRevoked  Kind = "decision.revoked"
	KindPrincipleEvidence Kind = "principle.evidence"
	KindPackManifest     Kind = "pack.manifest"
)

// Event is one immutable log entry. Seq is the per-project stable order and
// ID is the stable, export-surviving identity.
type Event struct {
	Seq              int64           `json:"seq"`
	ID               string          `json:"id"`
	ProjectID        string          `json:"project_id"`
	Kind             Kind            `json:"kind"`
	Time             string          `json:"time"`
	InputID          string          `json:"input_id,omitempty"`
	AnalysisID       string          `json:"analysis_id,omitempty"`
	SubjectID        string          `json:"subject_id,omitempty"`
	ParentEventID    string          `json:"parent_event_id,omitempty"`
	AlgoVersion      string          `json:"algo_version,omitempty"`
	ParamFingerprint string          `json:"param_fp,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}

// ByteRange is an inclusive-exclusive half-open interval [Lo,Hi) of file bytes.
type ByteRange struct {
	Lo int64 `json:"lo"`
	Hi int64 `json:"hi"`
}

// Params are the revision-controlled parser parameters.
type Params struct {
	ID          string `json:"id"`
	VisitBudget int    `json:"visit_budget"`
	// MaxExternalBytes bounds how many external value bytes may be read.
	MaxExternalBytes int64 `json:"max_external_bytes"`
}

// Fingerprint is a deterministic canonical hash input of the parameters.
func (p Params) Fingerprint() string {
	return fingerprint("visit_budget", p.VisitBudget, "max_external_bytes", p.MaxExternalBytes)
}
