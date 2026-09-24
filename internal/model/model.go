// Package model defines the append-only event vocabulary and the projected
// review state for the IFD waypoint room. Every displayed value is addressable
// by a stable, source-derived identity that survives export/re-import.
package model

import "time"

// EventType enumerates the immutable log entries.
type EventType string

const (
	EvProjectCreated    EventType = "project_created"
	EvSourceImported    EventType = "source_imported"
	EvParameterRevised  EventType = "parameter_revised"
	EvSourcePatched     EventType = "source_patched"
	EvDerivationRecorded EventType = "derivation_recorded"
	EvDecisionRecorded  EventType = "decision_recorded"
	EvDecisionUndone    EventType = "decision_undone"
)

// Envelope is one append-only fact.
type Envelope struct {
	Seq        int64          `json:"seq"`
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	Type       EventType      `json:"type"`
	OccurredAt time.Time      `json:"occurred_at"`
	ParentIDs  []string       `json:"parent_ids"`
	Payload    map[string]any `json:"payload"`
}

// ParameterFingerprint captures the rule set used for a derivation.
type ParameterFingerprint struct {
	AlgorithmVersion string         `json:"algorithm_version"`
	NodeBudget       int            `json:"node_budget"`
	BytesBudget      int64          `json:"bytes_budget"`
	OverlapPolicy    string         `json:"overlap_policy"`
	Extra            map[string]any `json:"extra,omitempty"`
}

// Provenance ties a derived object to its direct sources.
type Provenance struct {
	DirectSources []string             `json:"direct_sources"`
	Fingerprint   ParameterFingerprint `json:"fingerprint"`
	DerivedFrom   []string             `json:"derived_from_events"`
}

// SourceRef points at an immutable import version.
type SourceRef struct {
	SourceID string `json:"source_id"`
	Version  int    `json:"version"`
	BlobHash string `json:"blob_hash"`
	ByteSize int64  `json:"byte_size"`
	Label    string `json:"label"`
}

// Decision is an accepted/rejected/adjusted human verdict.
type Decision struct {
	ID         string    `json:"id"`
	TargetKind string    `json:"target_kind"` // geo_candidate | block | ifd
	TargetID   string    `json:"target_id"`
	Verdict    string    `json:"verdict"` // accepted | rejected | adjusted
	Reason     string    `json:"reason"`
	Active     bool      `json:"active"`
	Supersedes string    `json:"supersedes,omitempty"`
	EventID    string    `json:"event_id"`
	CreatedAt  time.Time `json:"created_at"`
}
