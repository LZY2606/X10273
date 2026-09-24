// Package domain is the event-sourced review core. Raw inputs are immutable;
// every derived value carries algorithm version, a parameter fingerprint
// and direct source references. Candidates (transforms), rejections and
// unknown states are first-class records. The project state is a pure
// projection of its event log, so any past sequence can be replayed.
package domain

// AlgorithmVersion identifies parser/derivation semantics for provenance.
const AlgorithmVersion = "ifdguide-derive-1.0.0"

// Params are reviewer-adjustable derivation parameters. Budget bounds IFD
// visits; changing them records a parameter revision and only invalidates
// derived results (raw inputs stay valid and untouched).
type Params struct {
	BudgetVisits int    `json:"budgetVisits"`
}

// Fingerprint deterministically summarizes parameters.
func (p Params) Fingerprint() string {
	return "budget=" + itoa(p.BudgetVisits)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [24]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

// Kind discriminants for derived objects.
const (
	ObjHeader    = "header"
	ObjIFD       = "ifd"
	ObjTag       = "tag"
	ObjEdge      = "edge"
	ObjBlock     = "block"
	ObjOverlap   = "overlap"
	ObjOOB       = "out-of-bounds"
	ObjGeoKeyDir = "geokey-dir"
	ObjGeoKey    = "geokey"
	ObjTransform = "transform-candidate"
	ObjFinding   = "finding"
	ObjBudget    = "budget"
)

// Unit annotates a measured value with its unit or coordinate basis.
type Unit struct {
	Raw    string `json:"raw"`              // as encoded (e.g. GeoKey code name)
	Normal string `json:"normal,omitempty"` // normalized name, if convertible
	CRS    int    `json:"crs,omitempty"`
}

// Source is a direct provenance pointer to bytes in an input version.
type Source struct {
	InputID  string `json:"inputId"`
	Offset   uint64 `json:"offset"`
	Length   uint64 `json:"length"`
	Tag      uint16 `json:"tag,omitempty"`
	IFD      uint64 `json:"ifdOffset,omitempty"`
}

// Object is one stable, provenance-carrying derived record.
type Object struct {
	ID        string         `json:"id"`        // stable identity
	PathKey   string         `json:"pathKey"`   // structural path within an input
	Kind      string         `json:"kind"`
	Version   int            `json:"version"`   // input version number
	InputID   string         `json:"inputId"`
	Label     string         `json:"label"`
	Sources   []Source       `json:"sources"`
	Unit      *Unit          `json:"unit,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Status    string         `json:"status"` // ok|unknown|error|overlap|missing|oob|conflict|budget
	Diagnosis []string       `json:"diagnosis,omitempty"`
	// Superseded is set by the current projection when a later version has
	// an object with the same PathKey. It stays viewable but is not current.
	Superseded bool `json:"superseded,omitempty"`
}

// CandidateStatus is the decision state machine for transform candidates.
const (
	CandPending  = "pending"
	CandAccepted = "accepted"
	CandRejected = "rejected"
	CandAdjusted = "adjusted"
)

// Candidate is a geotransform candidate with decision history.
type Candidate struct {
	Object
	Decision *Decision `json:"decision,omitempty"`
}

// Decision is one human verdict on a candidate. ParentSeq supports undo.
type Decision struct {
	Seq        int      `json:"seq"`
	CandidateID string  `json:"candidateId"`
	Action     string   `json:"action"` // accept|reject|adjust|retract
	Reason     string   `json:"reason"`
	Params     *Params  `json:"params,omitempty"`
	At         string   `json:"at"` // virtual timestamp
	ParentSeq  int      `json:"parentSeq"`
	Supersedes int      `json:"supersedes,omitempty"`
}

// InputVersion is an immutable submitted raw input.
type InputVersion struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	ParentID string `json:"parentId,omitempty"`
	Name     string `json:"name"`
	SampleID string `json:"sampleId,omitempty"`
	ByteSize int    `json:"byteSize"`
	SHA256   string `json:"sha256"`
	SubmittedAt string `json:"submittedAt"`
	// RevisionKind: "initial" | "parameter" | "source" | "conflict-decision"
	RevisionKind string `json:"revisionKind"`
	Note string `json:"note,omitempty"`
}

// Derivation is the computed layer for one input version.
type Derivation struct {
	Version        int               `json:"version"`
	InputID        string            `json:"inputId"`
	Algorithm      string            `json:"algorithm"`
	Params         Params            `json:"params"`
	ParamFingerprint string          `json:"paramFingerprint"`
	DerivedAt      string            `json:"derivedAt"`
	Objects        []*Object         `json:"objects"`
	Candidates     []*Candidate      `json:"candidates"`
	// ParseError is non-empty when the bytes were not a parseable TIFF.
	// The raw input is still retained; derivation holds the diagnostic.
	ParseError string `json:"parseError,omitempty"`
}

// Current marks which versions are live in the current projection.
func (d *Derivation) objectIndex() map[string]*Object {
	m := map[string]*Object{}
	for _, o := range d.Objects {
		m[o.PathKey] = o
	}
	return m
}
