// Package tiff parses TIFF/GeoTIFF byte streams into an IFD directory graph
// while preserving the raw representation: endianness, classic vs BigTIFF,
// raw tag values, external value offsets, IFD link chains and tile/strip
// byte ranges. Parsing is strictly local and never reaches the network.
package tiff

import (
	"encoding/hex"
	"errors"
)

// Flavor identifies the TIFF header variant.
type Flavor string

const (
	FlavorClassic Flavor = "classic"
	FlavorBigTIFF Flavor = "bigtiff"
)

// ByteOrder is the raw on-disk byte order of the TIFF stream.
type ByteOrder string

const (
	LittleEndian ByteOrder = "little-endian"
	BigEndian    ByteOrder = "big-endian"
)

// TIFF field types (TIFF6 / BigTIFF). Sizes are on-disk byte counts.
const (
	TypeByte      = 1
	TypeASCII     = 2
	TypeShort     = 3
	TypeLong      = 4
	TypeRational  = 5
	TypeSByte     = 6
	TypeUndefined = 7
	TypeSShort    = 8
	TypeSLong     = 9
	TypeSRational = 10
	TypeFloat     = 11
	TypeDouble    = 12
	TypeIFD8      = 16 // BigTIFF only
	TypeLong8     = 17 // BigTIFF alias, width 8
	TypeSLong8    = 18
)

var typeWidth = map[int]int{
	TypeByte: 1, TypeASCII: 1, TypeShort: 2, TypeLong: 4, TypeRational: 8,
	TypeSByte: 1, TypeUndefined: 1, TypeSShort: 2, TypeSLong: 4, TypeSRational: 8,
	TypeFloat: 4, TypeDouble: 8, TypeIFD8: 8, TypeLong8: 8, TypeSLong8: 8,
}

// TypeName renders a field type id as its TIFF name.
func TypeName(t int) string {
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
		return "IFD8"
	case TypeLong8:
		return "LONG8"
	case TypeSLong8:
		return "SLONG8"
	default:
		return "UNKNOWN"
	}
}

// Standard tag identifiers used during graph traversal and block analysis.
const (
	TagImageWidth      = 256
	TagImageLength     = 257
	TagBitsPerSample   = 258
	TagCompression     = 259
	TagPhotometric     = 262
	TagStripOffsets    = 273
	TagSamplesPerPixel = 277
	TagRowsPerStrip    = 278
	TagStripByteCounts = 279
	TagTileWidth       = 322
	TagTileLength      = 323
	TagTileOffsets     = 324
	TagTileByteCounts  = 325
	TagSampleFormat    = 339

	TagGeoKeyDirectory    = 34735
	TagGeoDoubleParams    = 34736
	TagGeoAsciiParams     = 34737
	TagModelPixelScale   = 33550
	TagModelTiepoint     = 33922
	TagModelTransformation = 34264
)

// SubIFD tag identifiers (standard + GDAL extended nesting).
var subIFDTags = map[int]bool{330: true, 34665: true, 40965: true, 51020: true}

// Range is a half-open byte range [Start,End) inside the source stream.
type Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func (r Range) overlaps(o Range) bool {
	return r.Start < o.End && o.Start < r.End
}

func (r Range) length() int64 { return r.End - r.Start }

// Header captures the raw TIFF header and its interpretation.
type Header struct {
	Order          ByteOrder `json:"order"`
	Flavor         Flavor    `json:"flavor"`
	Offsetsize     int       `json:"offset_size"`
	FirstIFDOffset int64     `json:"first_ifd_offset"`
	BigVersion     int       `json:"big_version,omitempty"`
	Range          Range     `json:"range"`
}

// Tag preserves a single IFD entry exactly as stored.
type Tag struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Type        int    `json:"type"`
	TypeName    string `json:"type_name"`
	Count       int    `json:"count"`
	Inline      bool   `json:"inline"`
	// RawValue is the value area exactly as stored in the entry (padded).
	RawValue string `json:"raw_value_hex"`
	// ValueOffset is the external data offset for non-inline entries, else -1.
	ValueOffset int64 `json:"value_offset"`
	EntryRange  Range `json:"entry_range"`
	// ValueRange is the decoded logical value (inline area or external run).
	ValueRange Range `json:"value_range"`
	// ExternalRange is set when the value lives outside the IFD entry.
	ExternalRange *Range `json:"external_range,omitempty"`
}

func (t Tag) raw() []byte {
	b, err := hex.DecodeString(t.RawValue)
	if err != nil {
		return nil
	}
	return b
}

// Edge is one IFD -> IFD link (main chain, SubIFDs, EXIF/GPS).
type Edge struct {
	From   int64  `json:"from"`
	To     int64  `json:"to"`
	Tag    int    `json:"tag"`
	Kind   string `json:"kind"`
	Index  int    `json:"index"`
	Visited bool  `json:"visited"`
	Reason string `json:"reason,omitempty"`
}

// IFD is one parsed image file directory.
type IFD struct {
	Offset      int64  `json:"offset"`
	Index       int    `json:"index"`
	EntryCount  int    `json:"entry_count"`
	EntryRange  Range  `json:"entry_range"`
	NextOffset  int64  `json:"next_offset"`
	HasNext     bool   `json:"has_next"`
	Tags        []Tag  `json:"tags"`
	Edges       []Edge `json:"edges"`
}

// TagByID returns the first tag with the given id, if present.
func (d *IFD) TagByID(id int) (*Tag, bool) {
	for i := range d.Tags {
		if d.Tags[i].ID == id {
			return &d.Tags[i], true
		}
	}
	return nil, false
}

var errFatal = errors.New("fatal parse error")

// FatalError describes a stream that could not be parsed at all.
type FatalError struct{ Msg string }

func (e *FatalError) Error() string { return e.Msg }
