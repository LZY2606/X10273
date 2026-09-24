// Package domain holds the typed, evidence-bearing model shared by the TIFF
// layer, the event store and the review UI.
//
// Every domain quantity carries its unit or coordinate reference. Raw
// representations imported from TIFF bytes are preserved on the parser layer;
// normalisation happens only on derived layers and never deletes a value that
// could not be converted.
package domain

// Quantity is a value with its measurement unit. Keep is the original
// textual representation from the source so normalisation is never lossy.
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Keep  string  `json:"keep,omitempty"` // original representation
}

// GeoCRS names a coordinate reference system.
type GeoCRS struct {
	EPSG  int    `json:"epsg,omitempty"`
	Name  string `json:"name,omitempty"`
	Unit  string `json:"unit,omitempty"` // "degree" | "metre" | ""
	Known bool   `json:"known"`
}

// Status is the lifecycle of a derived result.
type Status string

const (
	StatusCurrent   Status = "current"   // valid for the current basis
	StatusStale     Status = "stale"     // viewable history, not current
	StatusCandidate Status = "candidate" // one legal option among many
	StatusAccepted  Status = "accepted"
	StatusRejected  Status = "rejected"
	StatusAmbiguous Status = "ambiguous" // cannot be uniquely decided
)

// Severity of a diagnostic.
type Severity string

const (
	SevInfo     Severity = "info"
	SevWarning  Severity = "warning"
	SevError    Severity = "error"
	SevBudget   Severity = "budget-exhausted"
	SevConflict Severity = "conflict"
)

// Diagnostic explains a rejected/unknown/partial result. It is first-class
// evidence and never silently discarded.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Evidence []string `json:"evidence,omitempty"` // stable ids
}

// Provenance ties a derived value to its direct sources, algorithm and params.
type Provenance struct {
	Algorithm string   `json:"algorithm"`
	AlgoVer   string   `json:"algo_version"`
	ParamFP   string   `json:"param_fingerprint"`
	Sources   []string `json:"sources"` // stable source ids (blobs/ifds/rules)
}

// Event is one append-only log entry. Ids are content-addressed and include
// the parent id, so order and ancestry survive export/import.
type Event struct {
	Seq      int            `json:"seq"`
	ID       string         `json:"id"`
	Parent   string         `json:"parent"`
	Branch   string         `json:"branch"`
	Kind     string         `json:"kind"`
	At       string         `json:"at"` // RFC3339, recorded not hashed-via-clock
	Actor    string         `json:"actor"`
	Payload  map[string]any `json:"payload"`
}
