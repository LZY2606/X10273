// Package model defines the domain objects of the IFD evidence graph.
//
// All identities are content-derived (see id.go) so that exporting a
// database, deleting it and importing the export again preserves identity,
// ordering and provenance.
package model

// ByteOrder is the on-disk TIFF byte order as observed in the header.
type ByteOrder string

const (
	LittleEndian ByteOrder = "II" // 0x4949
	BigEndian    ByteOrder = "MM" // 0x4D4D
)

// TiffVersion distinguishes classic TIFF (32-bit offsets) from BigTIFF.
type TiffVersion string

const (
	Classic TiffVersion = "classic" // magic 42
	BigTIFF TiffVersion = "bigtiff" // magic 43
)

// ByteRange is a half-open interval of source bytes [Offset, Offset+Length).
type ByteRange struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
}

// End returns the exclusive end offset.
func (r ByteRange) End() int64 { return r.Offset + r.Length }

func (r ByteRange) overlaps(o ByteRange) bool {
	return r.Offset < o.End() && o.Offset < r.End()
}

// ValueKind says whether a tag value is stored inline in the IFD entry or
// externally behind an offset.
type ValueKind string

const (
	ValueInline    ValueKind = "inline"
	ValueExternal  ValueKind = "external"
	ValueMalformed ValueKind = "malformed"
)

// RawTag keeps the original on-disk representation of a single IFD entry:
// raw value bytes are never normalized away at the import layer.
type RawTag struct {
	ID           string    `json:"id"`
	IFDID        string    `json:"ifdId"`
	Tag          uint16    `json:"tag"`
	TypeCode     uint16    `json:"typeCode"`
	TypeName     string    `json:"typeName"`
	Count        uint64    `json:"count"`
	EntryRange   ByteRange `json:"entryRange"`
	ValueKind    ValueKind `json:"valueKind"`
	ValueRange   ByteRange `json:"valueRange"` // inline payload slot or external range
	InlineBytes  []byte    `json:"inlineBytes,omitempty"`
	ExternalData []byte    `json:"externalBytes,omitempty"`
	// Numeric keeps decoded numbers in the declared type. Strings keep the
	// raw ASCII bytes including NUL terminators.
	Numbers    []uint64 `json:"numbers,omitempty"`
	Signed     []int64  `json:"signed,omitempty"`
	Doubles    []float64 `json:"doubles,omitempty"`
	ASCII      string   `json:"ascii,omitempty"`
	PointerTag bool     `json:"pointerTag,omitempty"` // value points at a sub-IFD
	Diagnostic string   `json:"diagnostic,omitempty"`
}

// EdgeKind identifies how one IFD points at another.
type EdgeKind string

const (
	EdgeNextIFD EdgeKind = "next_ifd"     // IFD offset pointer at end of IFD
	EdgeSubIFD  EdgeKind = "sub_ifd"      // tag 330 SubIFDs
	EdgeExifIFD EdgeKind = "exif_ifd"     // tag 34665
	EdgeGPSIFD  EdgeKind = "gps_ifd"      // tag 34853
)

// IFDEdge is a directed reference between IFDs.
type IFDEdge struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Kind   EdgeKind `json:"kind"`
	Tag    uint16   `json:"tag,omitempty"`
	Index  int      `json:"index,omitempty"` // ordinal within a multi-value pointer tag
	Offset int64    `json:"offset"`          // target offset as stored on disk
	// Resolved is false when the target was not visited (budget exhausted or
	// invalid offset); the edge still records what the source claimed.
	Resolved bool `json:"resolved"`
}

// IFD is one image file directory node in the graph.
type IFD struct {
	ID        string    `json:"id"`
	SourceID  string    `json:"sourceId"`
	Index     int       `json:"index"`     // discovery order within the parse
	Offset    int64     `json:"offset"`    // absolute file offset
	Range     ByteRange `json:"range"`     // entry block incl. count and next pointer
	Level     int       `json:"level"`     // depth from IFD0
	ParentID  string    `json:"parentId,omitempty"`
	Reachable bool      `json:"reachable"` // visited within the access budget
	Cycle     bool      `json:"cycle"`     // target offset had already been seen
	Shared    bool      `json:"shared"`    // referenced from more than one edge
	TagIDs    []string  `json:"tagIds"`
}

// BlockKind distinguishes strips from tiles.
type BlockKind string

const (
	BlockStrip BlockKind = "strip"
	BlockTile  BlockKind = "tile"
)

// BlockStatus makes overlap/missing state explicit; the UI never invents
// pixels to fill gaps.
type BlockStatus string

const (
	BlockOK            BlockStatus = "ok"
	BlockOverlapping   BlockStatus = "overlapping"
	BlockShort         BlockStatus = "short"      // byte count smaller than declared footprint
	BlockOutOfBounds   BlockStatus = "oob"        // byte range runs past EOF
	BlockMissing       BlockStatus = "missing"    // grid cell with no block entry
	BlockUnspecified   BlockStatus = "unspecified" // offsets tag absent
)

// PixelBlock is one strip/tile byte region.
type PixelBlock struct {
	ID       string      `json:"id"`
	Kind     BlockKind   `json:"kind"`
	Index    int         `json:"index"` // block ordinal
	TileX    int         `json:"tileX,omitempty"`
	TileY    int         `json:"tileY,omitempty"`
	Offset   int64       `json:"offset"`
	Count    int64       `json:"count"`
	Range    ByteRange   `json:"range"`
	Status   BlockStatus `json:"status"`
	Overlaps []string    `json:"overlaps,omitempty"`
	Diagnostic string    `json:"diagnostic,omitempty"`
}

// GeoKeyVersion records the four header version fields of the GeoKey
// directory exactly as imported.
type GeoKeyVersion struct {
	KeyDirectoryVersion uint16 `json:"keyDirectoryVersion"`
	KeyRevision        uint16 `json:"keyRevision"`
	MinorRevision      uint16 `json:"minorRevision"`
	NumberOfKeys       uint16 `json:"numberOfKeys"`
}

// GeoKey is one entry of tag 34735 in its original (key, location, count,
// value/index) shape.
type GeoKey struct {
	ID       string `json:"id"`
	Key      uint16 `json:"key"`
	Location uint16 `json:"location"`
	Count    uint16 `json:"count"`
	Value    uint16 `json:"value"` // inline value or index into params
	Resolved any    `json:"resolved,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// GeoData is the raw GeoTIFF material attached to one IFD.
type GeoData struct {
	IFDID string `json:"ifdId"`
	// ModelTiepoint (33922): groups of 6 doubles [I,J,K, X,Y,Z], kept raw.
	Tiepoints [][]float64 `json:"tiepoints,omitempty"`
	// ModelPixelScale (33550): 3 doubles with their declared meaning.
	PixelScale []float64 `json:"pixelScale,omitempty"`
	// ModelTransformation (34264): 16 doubles, row-major 4x4.
	Transformation []float64 `json:"transformation,omitempty"`
	GeoDoubleParams []float64 `json:"geoDoubleParams,omitempty"`
	GeoASCIIParams  string    `json:"geoAsciiParams,omitempty"`
	Version         *GeoKeyVersion `json:"version,omitempty"`
	Keys            []GeoKey `json:"keys,omitempty"`
}

// Diagnostic is a non-fatal note attached to an object. Unconvertible
// values stay in place; they are never dropped to make a picture.
type Diagnostic struct {
	ObjectID string `json:"objectId"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// ParseGraph is the immutable result of parsing one source revision.
type ParseGraph struct {
	SourceID  string       `json:"sourceId"`
	Revision  int          `json:"revision"`
	ByteOrder ByteOrder    `json:"byteOrder"`
	Version   TiffVersion  `json:"version"`
	Header    ByteRange    `json:"header"`
	FileSize  int64        `json:"fileSize"`
	IFD0Off   int64        `json:"ifd0Offset"`
	IFDs      []*IFD       `json:"ifds"`
	Tags      []*RawTag    `json:"tags"`
	Edges     []IFDEdge    `json:"edges"`
	Blocks    []PixelBlock `json:"blocks"`
	Geo       []GeoData    `json:"geo"`
	Diags     []Diagnostic `json:"diagnostics"`
	// Budget state at the end of the parse.
	StepsUsed   int    `json:"stepsUsed"`
	StepsBudget int    `json:"stepsBudget"`
	BudgetExhausted bool `json:"budgetExhausted"`
	// Fatal parse error, if the header itself was unusable.
	Fatal string `json:"fatal,omitempty"`
}
