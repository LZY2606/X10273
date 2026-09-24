package tiff

// Range is an inclusive byte range in the source document, identified by
// offset and length. Every parsed object carries one so the UI can show
// provenance down to the exact source bytes.
type Range struct {
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

// End returns the first byte after the range (exclusive end).
func (r Range) End() uint64 { return r.Offset + r.Length }

// Header is the raw 8- or 16-byte TIFF/BigTIFF header.
type Header struct {
	ByteOrder   ByteOrder `json:"byteOrder"`
	Magic       uint16    `json:"magic"`        // 42 classic, 43 BigTIFF
	BigTIFF     bool      `json:"bigTIFF"`
	BitsVersion uint16    `json:"bitsVersion"`  // BigTIFF only
	Bytesize    uint16    `json:"bytesize"`     // BigTIFF only, must be 8
	FirstIFD    uint64    `json:"firstIFD"`
	Range       Range     `json:"range"`
}

// Tag is one IFD entry. RawInline stores the value bytes when they fit in
// the entry value slot; External stores the offset range otherwise.
type Tag struct {
	Code      uint16 `json:"code"`
	Name      string `json:"name"`
	Type      uint16 `json:"type"`
	TypeName  string `json:"typeName"`
	Count     uint64 `json:"count"`
	EntryRange Range `json:"entryRange"`
	Inline    bool   `json:"inline"`
	RawInline []byte `json:"rawInline,omitempty"`
	External  *Range `json:"external,omitempty"`
	// Decoded is a best-effort, byte-order-aware decoding. Raw evidence is
	// always authoritative; Decoded may carry a Diagnostic instead.
	Decoded    []uint64  `json:"decoded,omitempty"`
	DecodedFloat []float64 `json:"decodedFloat,omitempty"`
	ASCII      string    `json:"ascii,omitempty"`
}

// IFD is one image file directory with its tags and graph links.
type IFD struct {
	Offset       uint64   `json:"offset"`
	Index        int      `json:"index"`          // discovery order (not an identity)
	EntryCount   uint64   `json:"entryCount"`
	Range        Range    `json:"range"`
	Tags         []*Tag   `json:"tags"`
	NextIFD      *uint64  `json:"nextIFD,omitempty"`
	SubIFDs      []uint64 `json:"subIFDs"`
	Shared       bool     `json:"shared"`        // reached via >1 edge
	OnCycle      bool     `json:"onCycle"`
	Visits       int      `json:"visits"`
}

// TagByCode returns the first tag with that code, if present.
func (d *IFD) TagByCode(code uint16) *Tag {
	for _, t := range d.Tags {
		if t.Code == code {
			return t
		}
	}
	return nil
}

// Block is one tile or strip: index-space origin plus the raw byte range.
type Block struct {
	Kind    string `json:"kind"`             // "tile" | "strip"
	Index   int    `json:"index"`
	Col     int    `json:"col,omitempty"`
	Row     int    `json:"row,omitempty"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Range   Range  `json:"range"`
	Missing bool   `json:"missing,omitempty"`
}

// Overlap records two blocks whose byte ranges intersect.
type Overlap struct {
	A int   `json:"a"`
	B int   `json:"b"`
	Range Range `json:"range"`
}

// OutOfBounds records a block whose range leaves the document.
type OutOfBounds struct {
	Block int    `json:"block"`
	Range Range `json:"range"`
	FileSize uint64 `json:"fileSize"`
}

// Blocks aggregates tile/strip geometry for one IFD.
type Blocks struct {
	Kind        string        `json:"kind"` // "tile" | "strip" | "none"
	ImageW      int           `json:"imageW"`
	ImageH      int           `json:"imageH"`
	TileW       int           `json:"tileW,omitempty"`
	TileH       int           `json:"tileH,omitempty"`
	RowsPerStrip int         `json:"rowsPerStrip,omitempty"`
	Items       []Block       `json:"items"`
	Overlaps    []Overlap     `json:"overlaps"`
	OutOfBounds []OutOfBounds `json:"outOfBounds"`
	// MismatchedCounts is true when offsets/counts arrays disagree; the
	// missing blocks are then rendered explicitly rather than guessed.
	MismatchedCounts bool `json:"mismatchedCounts"`
	CountMismatch    [2]int `json:"countMismatch,omitempty"`
}

// GeoKey is one entry of the GeoKey directory (versioned metadata).
type GeoKey struct {
	ID       uint16 `json:"id"`
	Location uint16 `json:"location"` // 0 = SHORT inline; else tag holding values
	Count    uint16 `json:"count"`
	Value    uint64 `json:"value,omitempty"`
	ValueStr string `json:"valueStr,omitempty"`
	Raw      []uint16 `json:"raw"`
}

// GeoKeyDirectory preserves the header (KeyDirectoryVersion, Revision,
// MinorRevision) and the key entries in original order.
type GeoKeyDirectory struct {
	Version       uint16   `json:"version"`
	Revision      uint16   `json:"revision"`
	MinorRevision uint16   `json:"minorRevision"`
	Keys          []GeoKey `json:"keys"`
	RawRange      Range    `json:"rawRange"`
}

// ModelTransform is one explicit georeferencing representation.
type ModelTransform struct {
	Kind  string     `json:"kind"` // "tiepoint-scale" | "transformation"
	// 4x4 matrix, row major; derived for tiepoint+scale when both present.
	Matrix [16]float64 `json:"matrix"`
	Tiepoint []float64 `json:"tiepoint,omitempty"`
	Scale     []float64 `json:"scale,omitempty"`
	Sources   []Range   `json:"sources"`
	Unit      string    `json:"unit"`
	CRSCode   int       `json:"crsCode,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// Finding is a parse diagnostic with a stable category and source range.
type Finding struct {
	Category string `json:"category"` // header|offset|ifd|block|geokey|budget
	Severity string `json:"severity"` // error|warning
	Message  string `json:"message"`
	Range    *Range `json:"range,omitempty"`
}

// Document is the full parse result plus the budget outcome.
type Document struct {
	FileSize uint64   `json:"fileSize"`
	Header   Header   `json:"header"`
	IFDs     []*IFD   `json:"ifds"`
	Edges    []Edge   `json:"edges"`
	Blocks   map[uint64]*Blocks `json:"blocks"` // keyed by IFD offset
	GeoKeys  map[uint64]*GeoKeyDirectory `json:"geoKeys"`
	Transforms map[uint64][]ModelTransform `json:"transforms"`
	Findings []Finding `json:"findings"`
	Budget   BudgetReport `json:"budget"`
}

// Edge is one directed link in the IFD graph.
type Edge struct {
	From   uint64 `json:"from"` // 0 = file header
	To     uint64 `json:"to"`
	Kind   string `json:"kind"` // "next" | "subifd"
	Cycle  bool   `json:"cycle"`
}

// BudgetReport records access-budget enforcement.
type BudgetReport struct {
	Limit      int  `json:"limit"`
	Visits     int  `json:"visits"`
	Exhausted  bool `json:"exhausted"`
	Truncated  bool `json:"truncated"`
	Unvisited  []uint64 `json:"unvisited,omitempty"`
}
