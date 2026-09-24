package domain

import "encoding/json"

// Event types for the append-only log.
const (
	EvtProjectCreated  = "project-created"
	EvtInputCommitted  = "input-committed"
	EvtDerived         = "derivation-recorded"
	EvtDecision        = "decision-recorded"
	EvtDecisionRetract = "decision-retracted"
	EvtParamRevised    = "params-revised"
)

// Event is one append-only, machine-readable fact. Payload carries the
// type-specific body; RawInput is populated only for input commits.
type Event struct {
	Seq       int             `json:"seq"`
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	At        string          `json:"at"`
	InputID   string          `json:"inputId,omitempty"`
	Version   int             `json:"version,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	RawInput  []byte          `json:"-"`
	RawInputRef string        `json:"rawInputRef,omitempty"` // storage key for bytes
}

// InputPayload is EvtInputCommitted.
type InputPayload struct {
	Input        InputVersion `json:"input"`
	Bytes        []byte       `json:"-"`
	StorageRef   string       `json:"storageRef"`
}

// DerivePayload is EvtDerived (the whole derived snapshot for a version).
type DerivePayload struct {
	Derivation Derivation `json:"derivation"`
}

// DecisionPayload covers accept/reject/adjust/retract.
type DecisionPayload struct {
	Decision Decision `json:"decision"`
}

// ParamsPayload is EvtParamRevised. A parameter revision does not mutate
// raw inputs; affected versions are re-derived and emit EvtDerived with the
// new fingerprint.
type ParamsPayload struct {
	Params Params `json:"params"`
	Reason string `json:"reason"`
	// ReDerived lists input versions recomputed under the new parameters.
	ReDerived []int `json:"reDerived"`
}
