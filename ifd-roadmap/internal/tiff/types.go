// Package tiff parses raw TIFF/GeoTIFF byte streams into IFD graphs, tag
// byte-range records and tile/strip block maps. Every value is preserved in
// its raw representation: no normalization happens here.
package tiff

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// ByteOrder preserves the raw endian mark found in the first two bytes.
type ByteOrder struct {
	Mark string // "II" or "MM" (raw bytes 49 49 / 4D 4D)
	BE   bool  // true for "MM"
	bo   binary.ByteOrder
}

var (
	LE = ByteOrder{Mark: "II", BE: false, bo: binary.LittleEndian}
	BE = ByteOrder{Mark: "MM", BE: true, bo: binary.BigEndian}
)

func (o ByteOrder) Order() binary.ByteOrder { return o.bo }
func (o ByteOrder) String() string {
	if o.BE {
		return "big-endian (MM)"
	}
	return "little-endian (II)"
}

// Type sizes for TIFF/BigTIFF field types.
var fieldTypeSize = map[uint16]int{
	TypeByte: 1, TypeASCII: 1, TypeShort: 2, TypeLong: 4,
	TypeRational: 8, TypeSByte: 1, TypeUndefined: 1, TypeSShort: 2,
	TypeSLong: 4, TypeSRational: 8, TypeFloat: 4, TypeDouble: 8,
	TypeIFD8: 8, // BigTIFF
}

const (
	TypeByte      uint16 = 1
	TypeASCII     uint16 = 2
	TypeShort     uint16 = 3
	TypeLong      uint16 = 4
	TypeRational  uint16 = 5
	TypeSByte     uint16 = 6
	TypeUndefined uint16 = 7
	TypeSShort    uint16 = 8
	TypeSLong     uint16 = 9
	TypeSRational uint16 = 10
	TypeFloat     uint16 = 11
	TypeDouble    uint16 = 12
	TypeIFD8      uint16 = 16
)

func TypeName(t uint16) string {
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
	case TypeIFD8:
		return "LONG8"
	}
	return fmt.Sprintf("UNKNOWN(%d)", t)
}

// Diagnostic records a non-fatal problem with a precise location.
type Diagnostic struct {
	Object  string `json:"object"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	At      int64  `json:"at,omitempty"`
}

func (d Diagnostic) String() string {
	if d.At != 0 {
		return fmt.Sprintf("%s[%s@%d]: %s", d.Object, d.Kind, d.At, d.Message)
	}
	return fmt.Sprintf("%s[%s]: %s", d.Object, d.Kind, d.Message)
}

// ByteRange is an exact [start,end) interval in the source file.
type ByteRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func (r ByteRange) Len() int64 { return r.End - r.Start }
func (r ByteRange) Valid() bool {
	return r.Start >= 0 && r.End >= r.Start
}

func rawHex(b []byte) string {
	const limit = 64
	if len(b) > limit {
		return hex.EncodeToString(b[:limit]) + fmt.Sprintf("…(%d bytes)", len(b))
	}
	return hex.EncodeToString(b)
}
