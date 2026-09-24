// Package tiff parses TIFF/GeoTIFF bytes into a directed IFD graph while
// preserving raw tag values, external value offsets, byte ranges and the
// classic/BigTIFF distinction. Traversal is bounded by an explicit access
// budget so cyclic and shared sub-IFD graphs terminate deterministically.
package tiff

import "fmt"

// ByteOrder is the on-disk byte order declared by the TIFF header BOM.
type ByteOrder string

const (
	LittleEndian ByteOrder = "little-endian"
	BigEndian    ByteOrder = "big-endian"
)

// Variant distinguishes classic TIFF from BigTIFF.
type Variant string

const (
	Classic Variant = "classic"
	BigTIFF Variant = "bigtiff"
)

// TagKind classifies a raw tag entry.
type TagKind string

const (
	KindInline     TagKind = "inline"
	KindExternal   TagKind = "external"
	KindIFDPointer TagKind = "ifd-pointer"
)

// RawValue keeps the original representation before any normalization.
type RawValue struct {
	Type   string  `json:"type"`
	Count  uint64  `json:"count"`
	Inline bool    `json:"inline"`
	// InlineBytes is the raw 4/8 byte value/offset slot exactly as stored.
	InlineBytes []byte `json:"inline_bytes,omitempty"`
	// Values are decoded using the declared on-disk byte order; rational and
	// SRational values are kept as integer numerator/denominator pairs.
	Values []string `json:"values,omitempty"`
	// External is populated only when the value does not fit inline.
	External *ExternalValue `json:"external,omitempty"`
}

// ExternalValue records the out-of-line value location and its raw bytes.
type ExternalValue struct {
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
	Bytes  []byte `json:"bytes,omitempty"`
}

// TagEntry is one preserved IFD tag with a stable source-relative identity.
type TagEntry struct {
	ID        string   `json:"id"`
	Tag       uint16   `json:"tag"`
	Name      string   `json:"name"`
	Kind      TagKind  `json:"kind"`
	EntryOff  int64    `json:"entry_offset"`
	ValueOff  int64    `json:"value_offset"`
	ValueLen  int64    `json:"value_len"`
	Raw       RawValue `json:"raw"`
	PointsTo  string   `json:"points_to,omitempty"`
}

// IFDNode is one image file directory in the graph.
type IFDNode struct {
	ID         string     `json:"id"`
	Offset     int64      `json:"offset"`
	Index      int        `json:"index"`
	Reachable  bool       `json:"reachable"`
	EntryCount uint64     `json:"entry_count"`
	Tags       []*TagEntry `json:"tags"`
	// Next is the next-IFD chain link (may be empty or form a cycle).
	Next string `json:"next,omitempty"`
	// Parents enumerates every edge that reaches this node; a shared sub-IFD
	// therefore has more than one parent.
	Parents []string `json:"parents,omitempty"`
}

// FindingCode enumerates deterministic parser diagnostics.
type FindingCode string

const (
	FindingEndiannessSuspicious FindingCode = "endianness_suspicious"
	FindingWideOffsetOutOfRange FindingCode = "wide_offset_out_of_range"
	FindingIFDCycle             FindingCode = "ifd_cycle"
	FindingSharedSubIFD         FindingCode = "shared_sub_ifd"
	FindingBudgetExhausted      FindingCode = "access_budget_exhausted"
	FindingTruncatedIFD         FindingCode = "truncated_ifd"
	FindingBadMagic             FindingCode = "bad_magic"
	FindingTagValueOutOfRange   FindingCode = "tag_value_out_of_range"
)

// Severity levels for findings.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
)

// Finding is a machine-readable diagnostic anchored to source locations.
type Finding struct {
	Code     FindingCode     `json:"code"`
	Severity Severity        `json:"severity"`
	Message  string          `json:"message"`
	IFDID    string          `json:"ifd_id,omitempty"`
	TagID    string          `json:"tag_id,omitempty"`
	Offset   int64           `json:"offset,omitempty"`
	Detail   map[string]any  `json:"detail,omitempty"`
}

// ParseOptions bounds traversal.
type ParseOptions struct {
	// NodeBudget caps the number of IFD node visits (a visit per reached edge).
	NodeBudget int
	// BytesBudget caps total bytes read while resolving external values.
	BytesBudget int64
}

// DefaultParseOptions is used when callers omit a budget.
func DefaultParseOptions() ParseOptions {
	return ParseOptions{NodeBudget: 64, BytesBudget: 1 << 20}
}

// Graph is the parse result: nodes plus diagnostics and budget accounting.
type Graph struct {
	ByteOrder  ByteOrder   `json:"byte_order"`
	Variant    Variant     `json:"variant"`
	BOM        []byte      `json:"bom"`
	Magic      uint16      `json:"magic"`
	HeaderLen  int64       `json:"header_len"`
	FileSize   int64       `json:"file_size"`
	FirstIFD   int64       `json:"first_ifd"`
	Nodes      []*IFDNode  `json:"nodes"`
	Findings   []Finding   `json:"findings"`
	NodeVisits int         `json:"node_visits"`
	BytesRead  int64       `json:"bytes_read"`
	NodeBudget int         `json:"node_budget"`
	BytesBudget int64      `json:"bytes_budget"`
	BudgetExhausted bool   `json:"budget_exhausted"`
}

// NodeByID looks up a node.
func (g *Graph) NodeByID(id string) *IFDNode {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func nodeID(fileHash string, offset int64) string {
	return fmt.Sprintf("ifd@%x", offset)
}
