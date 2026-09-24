// Package tiff implements a provenance-oriented TIFF/BigTIFF directory parser.
//
// The parser deliberately keeps both the raw tag values and their external
// byte offsets, records the IFD graph (including cycles and shared
// sub-IFDs), and always runs under an explicit access budget so that
// adversarial directory graphs cannot exhaust the reader.
package tiff

// ByteOrder records the on-disk byte order of the TIFF header.
type ByteOrder string

const (
	LittleEndian ByteOrder = "little-endian"
	BigEndian    ByteOrder = "big-endian"
)

// Kind distinguishes classic TIFF from BigTIFF.
type Kind string

const (
	Classic Kind = "classic"
	BigTIFF Kind = "bigtiff"
)

// ByteRange is a half-open interval [Offset, Offset+Length) inside the
// original TIFF byte stream. All ranges are byte addresses in the raw input
// and therefore preserve the exact provenance chain.
type ByteRange struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
}

// End returns the exclusive end offset.
func (r ByteRange) End() int64 { return r.Offset + r.Length }

// ValueLoc describes where one tag's value bytes live. Inline values point
// at the value slot inside the IFD entry; external values point at the heap
// area the offset field refers to.
type ValueLoc struct {
	External bool      `json:"external"`
	Range    ByteRange `json:"range"`
	// Overrun is non-empty when the declared value range runs past EOF.
	Overrun string `json:"overrun,omitempty"`
}

// RawTag preserves the original on-disk tag entry. Raw holds the original
// bytes inside the entry value slot; ExternalBytes holds the heap bytes for
// external values. Decoded interpretation belongs to a later layer.
type RawTag struct {
	ID       uint16     `json:"id"`
	Name     string     `json:"name"`
	Type     uint16     `json:"type"`
	TypeName string     `json:"type_name"`
	Count    uint64     `json:"count"`
	EntryAt  int64      `json:"entry_at"`
	// OffsetOrValue is the raw uint32/uint64 carried by the entry, retained
	// exactly as stored even when it is an inline value.
	OffsetOrValue uint64   `json:"offset_or_value"`
	Raw           []byte   `json:"raw,omitempty"`
	Loc           ValueLoc `json:"loc"`
}

// EdgeKind enumerates how one IFD points at another.
type EdgeKind string

const (
	EdgeNext   EdgeKind = "next-ifd"
	EdgeSubIFD EdgeKind = "subifd"
)

// Edge is one directed link between two directory nodes.
type Edge struct {
	From   int64    `json:"from"`
	To     int64    `json:"to"`
	Kind   EdgeKind `json:"kind"`
	TagID  uint16   `json:"tag_id,omitempty"`
	Index  int      `json:"index,omitempty"`
	At     int64    `json:"at"` // offset of the pointer word in the raw bytes
	Valid  bool     `json:"valid"`
	Reason string   `json:"reason,omitempty"`
}

// IFDNode is one image file directory.
type IFDNode struct {
	Offset    int64      `json:"offset"`
	Index     int        `json:"index"`     // discovery index (breadth order)
	Range     ByteRange  `json:"range"`     // whole directory bytes
	Tags      []RawTag   `json:"tags"`
	Shared    bool       `json:"shared"` // reached by more than one edge
	Cycle     bool       `json:"cycle"`  // already visited when discovered via edge
	EntrySize int        `json:"entry_size"`
}

// BudgetUsage reports how the access budget was spent.
type BudgetUsage struct {
	IFDsVisited    int `json:"ifds_visited"`
	BytesScanned   int64 `json:"bytes_scanned"`
	IFDLimit       int `json:"ifd_limit"`
	BytesLimit     int64 `json:"bytes_limit"`
	Exhausted      bool  `json:"exhausted"`
	ExhaustedAt    int64 `json:"exhausted_at,omitempty"`
	RemainingAfter int   `json:"remaining_after"`
}

// Diagnostic is a non-fatal parse finding with precise location.
type Diagnostic struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      int64     `json:"at"`
	Range   ByteRange `json:"range,omitempty"`
}

// ParseReport is the complete first-layer reading of a TIFF blob. Nothing is
// normalized here: endianness, kind, tag raw values, external offsets, the
// IFD chain and overruns are all retained verbatim.
type ParseReport struct {
	ByteOrder   ByteOrder   `json:"byte_order"`
	Kind        Kind        `json:"kind"`
	FileSize    int64       `json:"file_size"`
	HeaderRange ByteRange   `json:"header_range"`
	BOMRange    ByteRange   `json:"bom_range"`
	RootOffset  int64       `json:"root_offset"`
	IFDs        []*IFDNode  `json:"ifds"`
	Edges       []Edge      `json:"edges"`
	Cycles      []Edge      `json:"cycles,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Budget      BudgetUsage `json:"budget"`
}

// Options controls parser budgets.
type Options struct {
	MaxIFDs   int
	MaxBytes  int64
}

// DefaultBudget is the deterministic budget used by the demo and tests.
func DefaultBudget() Options {
	return Options{MaxIFDs: 32, MaxBytes: 1 << 20}
}

// TagName returns the textual name for the TIFF/GeoTIFF tags understood by
// this project; unknown tags get a stable numeric label.
func TagName(id uint16) string {
	if n, ok := tagNames[id]; ok {
		return n
	}
	return "Tag" + itoa(id)
}

func itoa(v uint16) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
