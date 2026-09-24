// Package project implements the local-first, event-sourced review project.
//
// Raw imports are immutable: a revision is a new input version linked to its
// parent. Every derived result carries an algorithm version, parameter
// fingerprint and direct sources. Derived results are never overwritten; a
// local input change marks only the results that depended on it stale, and
// stale results remain readable but can never masquerade as current.
package project

import "time"

// EventKind enumerates the machine-readable event vocabulary.
type EventKind string

const (
	EvImport          EventKind = "import"
	EvRevision        EventKind = "revision"
	EvParamRevise     EventKind = "param_revise"
	EvSourcePatch     EventKind = "source_patch"
	EvDecision        EventKind = "decision"
	EvUndo            EventKind = "undo"
	EvDerived         EventKind = "derived"
	EvNote            EventKind = "note"
	EvBudget          EventKind = "access_budget"
)

// Status of a derived result / candidate.
type Status string

const (
	StatusCurrent Status = "current"
	StatusStale   Status = "stale"
	StatusAccepted Status = "accepted"
	StatusRejected Status = "rejected"
	StatusUnknown  Status = "unknown"
)

// Event is one immutable, machine-readable record.
type Event struct {
	ID         string          `json:"id"`
	Seq        int64           `json:"seq"`
	Kind       EventKind       `json:"kind"`
	ParentID   string          `json:"parentId,omitempty"`
	Branch     string          `json:"branch"`
	VirtualAt  time.Time       `json:"at"`
	Actor      string          `json:"actor"`
	InputID    string          `json:"inputId,omitempty"`
	Version    int             `json:"version,omitempty"`
	AlgoVersion string         `json:"algoVersion,omitempty"`
	ParamFP    string          `json:"paramFingerprint,omitempty"`
	SubjectID  string          `json:"subjectId,omitempty"`
	Status     Status          `json:"status,omitempty"`
	Summary    string          `json:"summary"`
	Payload    []byte          `json:"payload,omitempty"`
	StableKey  string          `json:"stableKey,omitempty"`
}

// Input is an immutable raw TIFF submission.
type Input struct {
	ID        string    `json:"id"`
	Version   int       `json:"version"`
	ParentID  string    `json:"parentId,omitempty"`
	Name      string    `json:"name"`
	Blob      []byte    `json:"blob"`
	SHA256    string    `json:"sha256"`
	ByteCount int64     `json:"byteCount"`
	Seed      string    `json:"seed"`
	VirtualAt time.Time `json:"at"`
	// Scenario tags this input exercises (e.g. ifd_cycle).
	Scenarios []string  `json:"scenarios"`
}

// Params are parse/interpretation settings with a fingerprint.
type Params struct {
	AccessBudget int     `json:"accessBudget"`
	Tolerance    float64 `json:"tolerance"`
	// Version lets an algorithm change invalidate derived results explicitly.
	RuleVersion string `json:"ruleVersion"`
}

// Fingerprint deterministically summarizes parameters.
func (p Params) Fingerprint() string {
	return stableHash("params", []byte(p.RuleVersion+":"+itoa64(int64(p.AccessBudget))+":"+floatKey(p.Tolerance)))
}

// DerivedRef links a result to its direct sources.
type DerivedRef struct {
	ResultID    string   `json:"resultId"`
	Kind        string   `json:"kind"` // geo_candidate | block_map | ifd_graph | geokey
	IFDKey      string   `json:"ifdKey,omitempty"`
	StableKey   string   `json:"stableKey"`
	Status      Status   `json:"status"`
	AlgoVersion string   `json:"algoVersion"`
	ParamFP     string   `json:"paramFingerprint"`
	Sources     []string `json:"sources"`
	InputID     string   `json:"inputId"`
	CurrentOn   string   `json:"currentOn"` // input version id for which current
	JSON        []byte   `json:"json"`
	EventID     string   `json:"eventId"`
	// RejectReason / error / unknown kept as first-class data.
	RejectReason string `json:"rejectReason,omitempty"`
	ErrorKind    string `json:"errorKind,omitempty"`
}

// Decision records a human accept/reject/adjust action.
type Decision struct {
	ID         string    `json:"id"`
	SubjectID  string    `json:"subjectId"`
	Action     string    `json:"action"` // accept | reject | adjust
	Reason     string    `json:"reason"`
	BaseResult string    `json:"baseResultId,omitempty"`
	VirtualAt  time.Time `json:"at"`
	Undone     bool      `json:"undone"`
	UndoneBy   string    `json:"undoneBy,omitempty"`
}

// Project is the folded current state plus full history.
type Project struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Seed    string `json:"seed"`
	Params  Params `json:"params"`

	Events   []*Event           `json:"-"`
	Inputs   map[string]*Input  `json:"-"`
	InputVer []string           `json:"-"` // ids in version order (main lineage)
	Results  map[string]*DerivedRef `json:"-"`
	Decisions map[string]*Decision `json:"-"`

	headSeq  int64
	branchTip map[string]string
}

// HeadID returns the latest event id.
func (p *Project) HeadID() string {
	if len(p.Events) == 0 {
		return ""
	}
	return p.Events[len(p.Events)-1].ID
}
