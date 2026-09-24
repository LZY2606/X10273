// Package tiffio parses TIFF/GeoTIFF bytes into a structural IFD graph.
//
// The parser is deliberately evidence-oriented: original tag bytes, byte
// order, classic/BigTIFF flavour, external value offsets and the IFD
// traversal are all retained. No malformed input is repaired; out-of-range
// offsets, cycles and budget exhaustion become explicit diagnostics.
package tiffio

import (
	"encoding/binary"
	"math"
)

// Order is the byte order declared by the TIFF header.
type Order string

const (
	OrderLittle Order = "little-endian"
	OrderBig    Order = "big-endian"
)

// Flavour distinguishes classic TIFF from BigTIFF.
type Flavour string

const (
	Classic Flavour = "classic"
	BigTiff Flavour = "bigtiff"
)

// DiagnosticKind categorises parser diagnostics.
type DiagnosticKind string

const (
	DiagBadMagic       DiagnosticKind = "bad-magic"
	DiagTruncated      DiagnosticKind = "truncated"
	DiagValueRange     DiagnosticKind = "external-value-out-of-range"
	DiagIFDOutOfRange  DiagnosticKind = "ifd-offset-out-of-range"
	DiagCycle          DiagnosticKind = "ifd-cycle"
	DiagShared         DiagnosticKind = "shared-subdirectory"
	DiagBudgetEdge     DiagnosticKind = "visit-budget-exhausted"
	DiagUnknownType    DiagnosticKind = "unknown-type"
	DiagUnsupportedVer DiagnosticKind = "unsupported-version"
)

// Diagnostic records a structural problem together with the bytes involved.
type Diagnostic struct {
	Kind    DiagnosticKind `json:"kind"`
	Message string         `json:"message"`
	Offset  int64          `json:"offset,omitempty"`
	Length  int64          `json:"length,omitempty"`
	IFD     string         `json:"ifd,omitempty"`
}

// TypeKind mirrors the TIFF/BigTIFF data type enumeration.
type TypeKind uint16

const (
	TypeBYTE      TypeKind = 1
	TypeASCII     TypeKind = 2
	TypeSHORT     TypeKind = 3
	TypeLONG      TypeKind = 4
	TypeRATIONAL  TypeKind = 5
	TypeSBYTE     TypeKind = 6
	TypeUNDEFINED TypeKind = 7
	TypeSSHORT    TypeKind = 8
	TypeSLONG     TypeKind = 9
	TypeSRATIONAL TypeKind = 10
	TypeFLOAT     TypeKind = 11
	TypeDOUBLE    TypeKind = 12
	TypeIFD       TypeKind = 13
	TypeLONG8     TypeKind = 16
	TypeSLONG8    TypeKind = 17
	TypeIFD8      TypeKind = 18
)

// Size returns the on-disk size of one element of the type. The second
// result is false for unknown types.
func (t TypeKind) Size(flavour Flavour) (int, bool) {
	switch t {
	case TypeBYTE, TypeASCII, TypeSBYTE, TypeUNDEFINED:
		return 1, true
	case TypeSHORT, TypeSSHORT:
		return 2, true
	case TypeLONG, TypeSLONG, TypeFLOAT, TypeIFD:
		return 4, true
	case TypeRATIONAL, TypeSRATIONAL, TypeDOUBLE:
		return 8, true
	case TypeLONG8, TypeSLONG8, TypeIFD8:
		if flavour == BigTiff {
			return 8, true
		}
	}
	return 0, false
}

// Rational is an unsigned fraction exactly as stored in the file.
type Rational struct {
	Numerator   uint32 `json:"numerator"`
	Denominator uint32 `json:"denominator"`
}

// SRational is a signed fraction exactly as stored.
type SRational struct {
	Numerator   int32 `json:"numerator"`
	Denominator int32 `json:"denominator"`
}

// Entry is one IFD tag entry with its original bytes and decoded value.
type Entry struct {
	ID      string   `json:"id"`
	Tag     uint16   `json:"tag"`
	Type    TypeKind `json:"type"`
	Count   uint64   `json:"count"`
	Value   []any    `json:"value,omitempty"`
	Inline  bool     `json:"inline"`
	Raw     []byte   `json:"raw,omitempty"`
	Offset  int64    `json:"offset,omitempty"`
	Length  int64    `json:"length,omitempty"`
	Present bool     `json:"present"`
}

// SubIFDEdge links an IFD to one of its declared subdirectories.
type SubIFDEdge struct {
	Parent string `json:"parent"`
	Child  string `json:"child"`
	Tag    uint16 `json:"tag"`
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
}

// IFD is one image file directory with its entries and provenance.
type IFD struct {
	ID         string            `json:"id"`
	Offset     int64             `json:"offset"`
	Length     int64             `json:"length"`
	Entries    []*Entry          `json:"entries"`
	EntryByTag map[uint16]*Entry `json:"-"`
	NextOffset int64             `json:"nextOffset"`
	Next       string            `json:"next,omitempty"`
	Root       bool              `json:"root"`
	Order      int               `json:"order"`
}

// Header retains the raw file header exactly.
type Header struct {
	Order         Order   `json:"order"`
	Flavour       Flavour `json:"flavour"`
	Version       uint16  `json:"version"`
	FirstIFD      int64   `json:"firstIfd"`
	OffsetSize    int     `json:"offsetSize"`
	EntrySize     int     `json:"entrySize"`
	BytesizeGuard uint16  `json:"bytesizeGuard,omitempty"`
}

// Graph is the parsed structural view of a TIFF file.
type Graph struct {
	Header      Header        `json:"header"`
	FileSize    int64         `json:"fileSize"`
	IFDs        []*IFD        `json:"ifds"`
	IFDByID     map[string]*IFD `json:"-"`
	SubEdges    []SubIFDEdge  `json:"subEdges"`
	Diagnostics []Diagnostic  `json:"diagnostics"`
	Visits      int           `json:"visits"`
	Budget      int           `json:"budget"`
	Truncated   bool          `json:"truncated"`
}

var bo = struct {
	LE binary.ByteOrder
	BE binary.ByteOrder
}{binary.LittleEndian, binary.BigEndian}

func readU16(b []byte, order Order) uint16 {
	if order == OrderBig {
		return bo.BE.Uint16(b)
	}
	return bo.LE.Uint16(b)
}

func readU32(b []byte, order Order) uint32 {
	if order == OrderBig {
		return bo.BE.Uint32(b)
	}
	return bo.LE.Uint32(b)
}

func readU64(b []byte, order Order) uint64 {
	if order == OrderBig {
		return bo.BE.Uint64(b)
	}
	return bo.LE.Uint64(b)
}

func readS32(b []byte) int32 { return int32(binary.LittleEndian.Uint32(b)) }

func f64fromU64(v uint64) float64 { return math.Float64frombits(v) }
