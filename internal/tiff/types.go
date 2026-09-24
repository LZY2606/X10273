// Package tiff implements a provenance-oriented TIFF/BigTIFF directory parser.
//
// The parser deliberately does not rasterize pixels. It records the exact byte
// layout: endianness, classic vs BigTIFF, the raw tag values (inline value or
// external offset), the IFD graph (including cycles and shared sub-IFDs) and
// tile/strip byte ranges. Every emitted object keeps the byte range that
// produced it so the review UI can point back into the submitted bytes.
package tiff

// Order is the on-disk byte ordering declared by the TIFF header.
type Order uint8

const (
	OrderUnknown Order = 0
	OrderLE      Order = 1 // little-endian ("II", 0x4949)
	OrderBE      Order = 2 // big-endian ("MM", 0x4D4D)
)

func (o Order) String() string {
	switch o {
	case OrderLE:
		return "little-endian"
	case OrderBE:
		return "big-endian"
	default:
		return "unknown"
	}
}

// Kind selects classic TIFF (32-bit offsets) or BigTIFF (64-bit offsets).
type Kind uint8

const (
	KindClassic Kind = 1
	KindBig     Kind = 2
)

func (k Kind) String() string {
	switch k {
	case KindClassic:
		return "classic"
	case KindBig:
		return "bigtiff"
	default:
		return "unknown"
	}
}

// DataType is the TIFF field TYPE stored in each directory entry.
type DataType uint16

const (
	TypeByte      DataType = 1
	TypeASCII     DataType = 2
	TypeShort     DataType = 3
	TypeLong      DataType = 4
	TypeRational  DataType = 5
	TypeSByte     DataType = 6
	TypeUndefined DataType = 7
	TypeSShort    DataType = 8
	TypeSLong     DataType = 9
	TypeSRational DataType = 10
	TypeFloat     DataType = 11
	TypeDouble    DataType = 12
	TypeIFD       DataType = 13
	TypeLong8     DataType = 16
	TypeSLong8    DataType = 17
	TypeIFD8      DataType = 18
)

// Size returns the on-disk size in bytes of one element of the type. A size
// of zero marks an unsupported/unknown type code.
func (t DataType) Size() int {
	switch t {
	case TypeByte, TypeASCII, TypeSByte, TypeUndefined:
		return 1
	case TypeShort, TypeSShort:
		return 2
	case TypeLong, TypeSLong, TypeFloat, TypeIFD:
		return 4
	case TypeRational, TypeSRational, TypeDouble, TypeLong8, TypeSLong8, TypeIFD8:
		return 8
	default:
		return 0
	}
}

func (t DataType) String() string {
	switch t {
	case TypeByte:
		return "BYTE"
	case TypeASCII:
		return "ASCII"
	case TypeShort:
		return "SHORT"
	case TypeLong:
		return "LONG"
	case TypeRational:
		return "RATIONAL"
	case TypeSByte:
		return "SBYTE"
	case TypeUndefined:
		return "UNDEFINED"
	case TypeSShort:
		return "SSHORT"
	case TypeSLong:
		return "SLONG"
	case TypeSRational:
		return "SRATIONAL"
	case TypeFloat:
		return "FLOAT"
	case TypeDouble:
		return "DOUBLE"
	case TypeIFD:
		return "IFD"
	case TypeLong8:
		return "LONG8"
	case TypeSLong8:
		return "SLONG8"
	case TypeIFD8:
		return "IFD8"
	default:
		return "UNKNOWN"
	}
}

// Range is a half-open byte interval [Start, End) inside the submitted bytes.
type Range struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

func (r Range) Len() uint64 { return r.End - r.Start }

func (r Range) Overlaps(o Range) bool {
	return r.Start < o.End && o.Start < r.End
}

// Header records the TIFF/BigTIFF header exactly as it appeared.
type Header struct {
	Order         Order  `json:"order"`
	Kind          Kind   `json:"kind"`
	Magic         uint16 `json:"magic"`
	Version       uint16 `json:"version"` // 42 for classic, 43 for BigTIFF
	OffSize       uint16 `json:"offsize"` // BigTIFF offset size, always 8
	Zero          uint16 `json:"zero"`    // BigTIFF reserved word
	FirstIFD      uint64 `json:"first_ifd"`
	HeaderRange   Range  `json:"header_range"`
	FirstIFDRange Range  `json:"first_ifd_range"` // bytes holding the first-IFD pointer
}

// Entry is one raw IFD entry. ValueRange covers the entry bytes; ExternalRange
// is set when the value did not fit inline and was read through an offset.
type Entry struct {
	Index         int      `json:"index"`
	Tag           uint16   `json:"tag"`
	Type          DataType `json:"type"`
	Count         uint64   `json:"count"`
	RawValue      []byte   `json:"raw_value"` // exact value/offset bytes as stored
	External      bool     `json:"external"`
	ExternalOff   uint64   `json:"external_off"`
	EntryRange    Range    `json:"entry_range"`
	ValueRange    Range    `json:"value_range"` // inline bytes or pointed-to bytes
	ExternalRange Range    `json:"external_range"`
	Decoded       []any    `json:"decoded"` // best-effort decode; raw bytes stay authoritative
	Note          string   `json:"note,omitempty"`
}

// IFD is one raw image file directory as found in the bytes.
type IFD struct {
	Ordinal     int      `json:"ordinal"` // discovery order, 0-based
	ID          string   `json:"id"`     // stable per-byte-layout identity
	Offset      uint64   `json:"offset"` // offset of the entry-count word
	Count       uint64   `json:"count"`
	Range       Range    `json:"range"`
	Entries     []Entry  `json:"entries"`
	NextOffset  uint64   `json:"next_offset"` // raw next-IFD pointer, 0 means none
	NextPresent bool     `json:"next_present"`
	Parents     []string `json:"parents"` // IFD ids that reach this IFD
	Shared      bool     `json:"shared"`  // reached from more than one parent/edge
	Cyclic      bool     `json:"cyclic"`  // an edge closes a cycle
}

// Edge marks one directed link in the IFD graph.
type Edge struct {
	From   string `json:"from"` // IFD id, "" for the file header
	To     string `json:"to"`
	Tag    uint16 `json:"tag"` // 0 = next-IFD chain, 330 = SubIFD
	Kind   string `json:"kind"` // "next" or "subifd"
	Cyclic bool   `json:"cyclic"`
}

// Severity levels for machine-readable findings.
type Severity uint8

const (
	SevInfo Severity = iota + 1
	SevWarn
	SevError
)

func (s Severity) String() string {
	switch s {
	case SevInfo:
		return "info"
	case SevWarn:
		return "warn"
	case SevError:
		return "error"
	default:
		return "unknown"
	}
}

// Finding is a machine-readable judgment produced while parsing. Findings never
// stop the parser from retaining the raw evidence.
type Finding struct {
	Code     string            `json:"code"`
	Severity Severity          `json:"severity"`
	Message  string            `json:"message"`
	IFD      string            `json:"ifd,omitempty"`
	Tag      uint16            `json:"tag,omitempty"`
	Range    *Range            `json:"range,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
}

// Budget controls how much of an adversarial IFD graph the parser is allowed to
// visit. Both limits abort traversal with a recorded finding.
type Budget struct {
	MaxIFDVisits int `json:"max_ifd_visits"`
	MaxBytesRead int `json:"max_bytes_read"`
}

// DefaultBudget is deliberately small so cycles are cheap to demonstrate.
func DefaultBudget() Budget {
	return Budget{MaxIFDVisits: 64, MaxBytesRead: 1 << 20}
}

// Result is the full parse output.
type Result struct {
	Header      Header    `json:"header"`
	IFDs        []IFD     `json:"ifds"`
	Edges       []Edge    `json:"edges"`
	Findings    []Finding `json:"findings"`
	IFDVisits   int       `json:"ifd_visits"`
	BytesRead   int       `json:"bytes_read"`
	Budget      Budget    `json:"budget"`
	BudgetSpent bool      `json:"budget_spent"`
}
