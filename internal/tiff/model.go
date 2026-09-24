// Package tiff parses raw TIFF/GeoTIFF bytes into an explicit IFD graph while
// preserving original representations: byte order, classic vs BigTIFF, raw tag
// values, external value offsets, the IFD link graph and tile/strip ranges.
//
// Nothing here is normalized: values keep their on-disk type and bytes. Geo
// interpretation lives in package geo.
package tiff

import "fmt"

// ByteOrder is the on-disk byte order recorded in the TIFF header.
type ByteOrder string

const (
	LittleEndian ByteOrder = "II"
	BigEndian    ByteOrder = "MM"
)

// Version distinguishes classic TIFF (42) from BigTIFF (43).
type Version string

const (
	Classic Version = "classic"
	BigTIFF Version = "bigtiff"
)

// ByteRange is a half-open byte interval [Offset, Offset+Length) inside the
// original import blob. Lengths are always in bytes.
type ByteRange struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
}

func (r ByteRange) End() int64 { return r.Offset + r.Length }

func (r ByteRange) Overlaps(o ByteRange) bool {
	return r.Offset < o.End() && o.Offset < r.End()
}

func (r ByteRange) String() string {
	return fmt.Sprintf("[%d,%d)", r.Offset, r.End())
}

// TagID is a TIFF tag identifier as stored on disk.
type TagID uint16

// FieldType is the TIFF type code recorded in an IFD entry.
type FieldType uint16

func (t FieldType) Size() int {
	if int(t) < len(typeSizes) {
		return typeSizes[t]
	}
	return 0
}

func (t FieldType) Name() string {
	if int(t) < len(typeNames) && typeNames[t] != "" {
		return typeNames[t]
	}
	return fmt.Sprintf("type%d", int(t))
}

var typeSizes = [...]int{
	0: 0, // invalid
	1: 1, // BYTE
	2: 1, // ASCII
	3: 2, // SHORT
	4: 4, // LONG
	5: 8, // RATIONAL
	6: 1, // SBYTE
	7: 1, // UNDEFINED
	8: 2, // SSHORT
	9: 4, // SLONG
	10: 8, // SRATIONAL
	11: 4, // FLOAT
	12: 8, // DOUBLE
	13: 4, // IFD
	16: 8, // LONG8 (BigTIFF)
	17: 8, // SLONG8 (BigTIFF)
	18: 8, // IFD8 (BigTIFF)
}

var typeNames = [...]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}

// RawValue is the value of a tag exactly as represented on disk.
type RawValue struct {
	Type   FieldType `json:"type"`
	Count  int64     `json:"count"`
	Inline bool      `json:"inline"`
	// ValueRange covers the bytes that carry the value: the 4/8 entry bytes
	// when inline, or the external location otherwise.
	ValueRange ByteRange `json:"valueRange"`
	// ExternalRange is non-nil only when the value did not fit in the entry
	// and lives elsewhere in the blob.
	ExternalRange *ByteRange `json:"externalRange,omitempty"`
	// Raw holds the exact on-disk value bytes (already contiguous), copied so
	// the original representation survives independently of the blob.
	Raw []byte `json:"raw"`
}

// Tag is one IFD entry: identity, location and untouched value.
type Tag struct {
	ID    TagID    `json:"id"`
	Name  string   `json:"name"`
	Range ByteRange `json:"entryRange"`
	Value RawValue `json:"value"`
}

// BlockKind identifies how a block of pixel data is stored.
type BlockKind string

const (
	BlockStrip BlockKind = "strip"
	BlockTile  BlockKind = "tile"
)

// Block is one strip or tile with its data byte range and tile-space index.
// Offsets/counts stay as their raw source tag/value references; the resolved
// range is a derived ByteRange.
type Block struct {
	Index     int       `json:"index"`
	Kind      BlockKind `json:"kind"`
	TileX     int       `json:"tileX"`
	TileY     int       `json:"tileY"`
	DataRange ByteRange `json:"dataRange"`
	// OffsetTagValue/CountTagValue point at the per-block raw tag values.
	OffsetSource ByteRange `json:"offsetSource"`
	CountSource  ByteRange `json:"countSource"`
	// Present is false when the block is expected from the tiling layout but
	// no data exists (missing tile). Missing is never synthesized.
	Present bool `json:"present"`
}

// IFD is one image file directory in the IFD graph.
type IFD struct {
	Key    string    `json:"key"`
	Offset int64     `json:"offset"`
	Index  int       `json:"index"`
	Range  ByteRange `json:"range"`
	Tags   []*Tag    `json:"tags"`
	Blocks []*Block  `json:"blocks,omitempty"`
	// NextOffset is the raw value of the "next IFD" pointer (0 == none).
	NextOffset int64 `json:"nextOffset"`
	// NextKey is the resolved IFD key reached via NextOffset ("" if none).
	NextKey string `json:"nextKey,omitempty"`
	// SubKeys are child IFDs discovered through pointer tags (SubIFDs, Exif...).
	SubKeys []SubLink `json:"subKeys,omitempty"`
}

// SubLink records one edge to a subdirectory and the tag it came from.
type SubLink struct {
	TagID TagID  `json:"tagId"`
	Key   string `json:"key"`
}

// Header is the preserved TIFF header.
type Header struct {
	ByteOrder    ByteOrder `json:"byteOrder"`
	Version      Version   `json:"version"`
	Magic        uint16    `json:"magic"`
	FirstIFD     int64     `json:"firstIFD"`
	Range        ByteRange `json:"range"`
	OffsetsAre8  bool      `json:"offsetsAre8"`
}

// File is a fully parsed, un-normalized TIFF.
type File struct {
	Header Header            `json:"header"`
	IFDs   map[string]*IFD   `json:"ifds"`
	Roots  []string          `json:"roots"`
	Order  []string          `json:"order"`
	Blob   []byte            `json:"-"`
	Notes  []StructuralNote  `json:"notes,omitempty"`
}

// StructuralNote records a structural observation (cycle, shared node, budget
// exhaustion, out-of-range pointer) without aborting the parse.
type StructuralNote struct {
	Kind    string    `json:"kind"`
	At      ByteRange `json:"at"`
	Message string    `json:"message"`
}

// ParseError describes a definite format error with its byte location.
type ParseError struct {
	Offset int64
	Msg    string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("tiff: %s at offset %d", e.Msg, e.Offset)
}
