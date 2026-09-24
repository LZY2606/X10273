// Package tiffio reads raw TIFF / BigTIFF byte structures.
//
// It deliberately stays below semantic interpretation: it preserves byte
// order, the exact tag values, external value offsets, the IFD pointer graph
// and byte ranges. Higher layers (blocks, geotiff) consume these facts.
package tiffio

import (
	"encoding/binary"
	"fmt"
)

// Order is the on-disk byte order of a TIFF stream.
type Order int

const (
	UnknownEndian Order = iota
	LittleEndian
	BigEndian
)

func (o Order) String() string {
	switch o {
	case LittleEndian:
		return "little-endian"
	case BigEndian:
		return "big-endian"
	default:
		return "unknown-endian"
	}
}

// ShortName is II / MM, used in stable identities and fixtures.
func (o Order) ShortName() string {
	switch o {
	case LittleEndian:
		return "II"
	case BigEndian:
		return "MM"
	default:
		return "??"
	}
}

func (o Order) bo() binary.ByteOrder {
	if o == BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// FieldType is a TIFF/BigTIFF field type code. Values are preserved verbatim;
// unknown codes are retained rather than normalised away.
type FieldType uint16

const (
	TypeByte      FieldType = 1
	TypeASCII     FieldType = 2
	TypeShort     FieldType = 3
	TypeLong      FieldType = 4
	TypeRational  FieldType = 5
	TypeSByte     FieldType = 6
	TypeUndefined FieldType = 7
	TypeSShort    FieldType = 8
	TypeSLong     FieldType = 9
	TypeSRational FieldType = 10
	TypeFloat     FieldType = 11
	TypeDouble    FieldType = 12
	TypeIFD       FieldType = 13 // BigTIFF only
	TypeLong8     FieldType = 16 // BigTIFF only
	TypeSLong8    FieldType = 17 // BigTIFF only
	TypeIFD8      FieldType = 18 // BigTIFF only
)

// Size returns the per-element size in bytes. Unknown types report 0.
func (t FieldType) Size() int {
	switch t {
	case TypeByte, TypeASCII, TypeSByte, TypeUndefined:
		return 1
	case TypeShort, TypeSShort:
		return 2
	case TypeLong, TypeSLong, TypeIFD, TypeFloat:
		return 4
	case TypeRational, TypeSRational, TypeDouble, TypeLong8, TypeSLong8, TypeIFD8:
		return 8
	}
	return 0
}

func (t FieldType) String() string {
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
	}
	return fmt.Sprintf("UNKNOWN(%d)", uint16(t))
}
