// Package tiff parses TIFF/BigTIFF byte streams into an IFD graph while
// preserving raw provenance: endianness, variant, raw tag value fields,
// external value offsets, IFD chain links, tile/strip byte ranges and
// GeoKey directory versions. Parsing is budget-limited so cyclic or
// shared IFD structures cannot hang or loop the parser.
package tiff

import "fmt"

// Endianness of the file header.
type Endianness string

const (
	LittleEndian Endianness = "little"
	BigEndian    Endianness = "big"
)

// Variant distinguishes classic TIFF from BigTIFF.
type Variant string

const (
	Classic Variant = "classic"
	BigTIFF Variant = "bigtiff"
)

// Range is a half-open byte range [Start, End) inside the source file.
type Range struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

func (r Range) String() string { return fmt.Sprintf("[%d,%d)", r.Start, r.End) }

// Len returns the number of bytes covered by the range.
func (r Range) Len() uint64 {
	if r.End <= r.Start {
		return 0
	}
	return r.End - r.Start
}

// Overlaps reports whether two ranges share at least one byte.
func (r Range) Overlaps(o Range) bool { return r.Start < o.End && o.Start < r.End }

// Diagnostic codes emitted by the parser and block analysis.
const (
	DiagBadHeader        = "BAD_HEADER"
	DiagIFDCycle         = "IFD_CYCLE"
	DiagSharedSubIFD     = "SHARED_SUBIFD"
	DiagBudgetExhausted  = "BUDGET_EXHAUSTED"
	DiagValueOutOfBounds = "VALUE_OUT_OF_BOUNDS"
	DiagIFDOutOfBounds   = "IFD_OUT_OF_BOUNDS"
	DiagTagTruncated     = "TAG_TABLE_TRUNCATED"
	DiagUnknownType      = "UNKNOWN_FIELD_TYPE"
)

// Diagnostic is a machine-readable finding tied to a stable subject ID.
type Diagnostic struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"` // "error" | "warning" | "info"
	Code      string `json:"code"`
	Message   string `json:"message"`
	SubjectID string `json:"subjectId"`
	Offset    uint64 `json:"offset,omitempty"`
	Evidence  []byte `json:"evidence,omitempty"` // raw bytes read back as proof
}

// Tag is one IFD entry. The raw 4/8-byte value-or-offset field is always
// preserved; when the value lives externally, its offset and byte range
// are recorded alongside the decoded values.
type Tag struct {
	ID          string    `json:"id"`
	Code        uint16    `json:"code"`
	Name        string    `json:"name"`
	Type        uint16    `json:"type"`
	TypeName    string    `json:"typeName"`
	Count       uint64    `json:"count"`
	RawField    []byte    `json:"rawField"` // raw inline value/offset field, as stored
	External    bool      `json:"external"`
	ValueOffset uint64    `json:"valueOffset,omitempty"`
	ByteRange   Range     `json:"byteRange"`          // where the value bytes live
	RawValue    []byte    `json:"rawValue,omitempty"` // value bytes read back (truncated on OOB)
	Values      []uint64  `json:"values,omitempty"`   // decoded integer values
	Floats      []float64 `json:"floats,omitempty"`   // decoded float values
	Text        string    `json:"text,omitempty"`     // decoded ASCII value
	OutOfBounds bool      `json:"outOfBounds,omitempty"`
}

// IFD is one image file directory node in the parsed graph.
type IFD struct {
	ID         string   `json:"id"` // ifd@<offset>
	Offset     uint64   `json:"offset"`
	VisitIndex int      `json:"visitIndex"`
	Depth      int      `json:"depth"`
	Tags       []*Tag   `json:"tags"`
	NextOffset uint64   `json:"nextOffset"`
	NextID     string   `json:"nextId,omitempty"`
	NextCycle  bool     `json:"nextCycle,omitempty"` // next points to an already-visited IFD
	SubIFDIDs  []string `json:"subIfdIds,omitempty"`
	ParentIDs  []string `json:"parentIds,omitempty"` // set when shared by multiple parents
}

// TagByCode returns the first tag with the given code, or nil.
func (d *IFD) TagByCode(code uint16) *Tag {
	for _, t := range d.Tags {
		if t.Code == code {
			return t
		}
	}
	return nil
}

// Budget limits parser work so cyclic/shared structures terminate.
type Budget struct {
	MaxIFDs int `json:"maxIfds"`
	MaxTags int `json:"maxTags"`
}

// DefaultBudget is generous enough for ordinary files.
func DefaultBudget() Budget { return Budget{MaxIFDs: 256, MaxTags: 4096} }

// BudgetReport records how the budget was consumed.
type BudgetReport struct {
	Limit     Budget `json:"limit"`
	IFDsSeen  int    `json:"ifdsSeen"`
	TagsSeen  int    `json:"tagsSeen"`
	Exhausted bool   `json:"exhausted"`
}

// File is the parsed result: header facts, the IFD graph in visit order,
// and all diagnostics.
type File struct {
	Endianness  Endianness   `json:"endianness"`
	Variant     Variant      `json:"variant"`
	Size        uint64       `json:"size"`
	FirstIFD    uint64       `json:"firstIfd"`
	IFDs        []*IFD       `json:"ifds"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Budget      BudgetReport `json:"budget"`
}

// IFDByID looks up a visited IFD by its stable ID.
func (f *File) IFDByID(id string) *IFD {
	for _, d := range f.IFDs {
		if d.ID == id {
			return d
		}
	}
	return nil
}
