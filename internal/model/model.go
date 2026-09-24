// Package model defines the immutable domain objects of the IFD road-room:
// TIFF bytes, IFD graphs, tag values, pixel blocks, geo-transform candidates
// and the provenance-bearing event archive.
package model

import "time"

// ByteOrder of a TIFF header.
type ByteOrder string

const (
	OrderLittle ByteOrder = "little-endian"
	OrderBig    ByteOrder = "big-endian"
)

// Kind selects classic TIFF or BigTIFF header/IFD framing.
type Kind string

const (
	KindClassic Kind = "classic"
	KindBigTIFF Kind = "bigtiff"
)

// TagValue is the preserved raw value of a tag entry. Values that fit inside
// the four/eight inline bytes are Inline; otherwise they live at ValueOffset
// and byteRange [ValueOffset, ValueOffset+TotalLen) in the blob.
type TagValue struct {
	TypeID   uint16 `json:"typeId"`
	TypeName string `json:"typeName"`
	Count    uint64 `json:"count"`
	// RawUint/ RawDouble hold the decoded components in file order. Values
	// that cannot be decoded for the claimed type stay in RawBytesHex.
	RawUint      []uint64 `json:"rawUint,omitempty"`
	RawDouble    []float64 `json:"rawDouble,omitempty"`
	RawBytesHex  string   `json:"rawBytesHex,omitempty"`
	Inline       bool     `json:"inline"`
	InlineBytesHex string  `json:"inlineBytesHex"`
	ValueOffset  int64    `json:"valueOffset"` // -1 when inline
	ValueLen     uint64   `json:"valueLen"`    // external payload size
	DecodeNote   string   `json:"decodeNote,omitempty"`
}

// TagEntry is one IFD entry, identity derived from (ifdID, tagID, seq).
type TagEntry struct {
	Seq     int      `json:"seq"`
	TagID   uint16   `json:"tagId"`
	Value   TagValue `json:"value"`
	ByteRange Range  `json:"byteRange"` // the entry itself inside the IFD
}

// SubfileKind classifies an IFD discovered while walking SubIFDs (330).
type SubfileKind string

const (
	SubfileNone   SubfileKind = ""
	SubfileChild  SubfileKind = "subifd"
	SubfileNext   SubfileKind = "next-ifd"
	SubfileRoot   SubfileKind = "root"
)

// IFD is one parsed image file directory. ContentID changes when any entry
// changes, which makes shared/aliased subdirectories observable: identical
// IFDs referenced from two parents share one identity.
type IFD struct {
	ID       string     `json:"id"`
	BlobID   string     `json:"blobId"`
	Offset   int64      `json:"offset"`
	Entries  []TagEntry `json:"entries"`
	NextOffset int64    `json:"nextOffset"` // 0 = terminator
	NextID   string     `json:"nextId,omitempty"`
	Length   uint64     `json:"length"` // bytes occupied by the IFD record
	Notes    []string   `json:"notes,omitempty"`
}

// Range is a half-open byte interval [Start, End).
type Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// Overlaps reports a non-empty intersection.
func (r Range) Overlaps(o Range) bool {
	return r.Start < o.End && o.Start < r.End
}

func (r Range) Empty() bool { return r.End <= r.Start }

// TileShape is the storage geometry declared by an IFD.
type TileShape struct {
	ImageWidth  uint64 `json:"imageWidth"`
	ImageLength uint64 `json:"imageLength"`
	TileWidth   uint64 `json:"tileWidth"`
	TileLength  uint64 `json:"tileLength"`
	RowsPerStrip uint64 `json:"rowsPerStrip"`
	Tiled       bool   `json:"tiled"`
	Stripped    bool   `json:"stripped"`
	Across      uint64 `json:"across"`
	Down        uint64 `json:"down"`
	Expected    uint64 `json:"expected"` // expected block count
}

// BlockState enumerates explicit block coverage states; missing blocks are
// never fabricated to complete a map.
type BlockState string

const (
	BlockPresent BlockState = "present"
	BlockMissing BlockState = "missing"
)

// PixelBlock is one tile or strip. StorageRange is the byte range inside the
// blob; Coverage is the pixel rectangle (x0,y0,x1,y1) the block claims.
type PixelBlock struct {
	Index       uint64     `json:"index"`
	State       BlockState `json:"state"`
	StorageRange Range     `json:"storageRange"`
	DeclaredLen uint64     `json:"declaredLen"`
	Coverage    [4]int64   `json:"coverage"`
	SourceTagID uint16     `json:"sourceTagId"`
	Notes       []string   `json:"notes,omitempty"`
}

// Overlap is evidence that two blocks cannot both occupy the claimed bytes or
// pixels without conflict.
type Overlap struct {
	Kind    string `json:"kind"` // "storage" | "coverage" | "mixed-layout"
	AIndex  uint64 `json:"aIndex"`
	BIndex  uint64 `json:"bIndex"`
	Detail  string `json:"detail"`
}

// BlockMap covers one IFD's tile/strip geometry.
type BlockMap struct {
	IFDID      string       `json:"ifdId"`
	Shape      TileShape    `json:"shape"`
	Blocks     []PixelBlock `json:"blocks"`
	Overlaps   []Overlap    `json:"overlaps"`
	Missing    []uint64     `json:"missing"`
	OutOfRange []OutOfRange `json:"outOfRange"`
}

// OutOfRange is evidence that a claimed byte range escapes the blob.
type OutOfRange struct {
	Object string `json:"object"` // e.g. "block:3" or "tag:273:0"
	Range  Range  `json:"range"`
	BlobSize int64 `json:"blobSize"`
	Detail string `json:"detail"`
}

// GeoKeyEntry preserves one GeoKey directory triple/quad verbatim.
type GeoKeyEntry struct {
	KeyID      uint16 `json:"keyId"`
	TagLoc     uint16 `json:"tagLoc"`
	Count      uint16 `json:"count"`
	ValueOrOffset uint16 `json:"valueOrOffset"`
	Resolved   string `json:"resolved,omitempty"` // textual resolution, raw kept too
}

// GeoKeyDir is the parsed 34735 directory plus version tags and a conflict
// list (duplicated key ids, mismatched header versions).
type GeoKeyDir struct {
	IFDID       string        `json:"ifdId"`
	KeyDirVersion uint16      `json:"keyDirectoryVersion"`
	KeyRevision  uint16       `json:"keyRevision"`
	MinorRevision uint16      `json:"minorRevision"`
	Entries      []GeoKeyEntry `json:"entries"`
	Conflicts    []GeoConflict `json:"conflicts,omitempty"`
	Notes        []string      `json:"notes,omitempty"`
}

// GeoConflict records an unresolved GeoKey-level contradiction. All legal
// candidates survive; the parser never silently picks one.
type GeoConflict struct {
	Kind   string `json:"kind"` // "duplicate-key" | "header-version"
	KeyID  uint16 `json:"keyId,omitempty"`
	Detail string `json:"detail"`
}

// TransformKind distinguishes the two standard Model transformation routes.
type TransformKind string

const (
	TransTiepointScale TransformKind = "tiepoint+scale"
	TransMatrix        TransformKind = "model-transformation"
)

// TransformCandidate is a legal raster-to-model mapping derivable from the
// preserved tags. When both routes exist they conflict and both are retained.
type TransformCandidate struct {
	ID         string        `json:"id"`
	Kind       TransformKind `json:"kind"`
	IFDID      string        `json:"ifdId"`
	Matrix     []float64     `json:"matrix"` // 4x4 row-major, model = M * (i,j,0,1)
	Unit       string        `json:"unit"`
	DatumBasis string        `json:"datumBasis"` // e.g. "GeoKey:3072=...; ModelTiepoint#0"
	Unknowns   []string      `json:"unknowns,omitempty"`
	Diagnostic string        `json:"diagnostic,omitempty"`
	Sources    []SourceRef   `json:"sources"`
}

// SourceRef points at the exact preserved raw object a derived value came
// from: blob -> ifd -> tag -> component index.
type SourceRef struct {
	BlobID string `json:"blobId"`
	IFDID  string `json:"ifdId,omitempty"`
	TagID  uint16 `json:"tagId,omitempty"`
	Index  int    `json:"index,omitempty"`
	Role   string `json:"role,omitempty"`
}

// Rejection is a first-class reason a candidate was not accepted.
type Rejection struct {
	CandidateID string `json:"candidateId"`
	Reason      string `json:"reason"`
	AtEvent     string `json:"atEvent"`
}

// Decision records a human adjudication. DecisionKind: accept | reject |
// parameter-adjust.
type Decision struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	TargetID    string   `json:"targetId"`
	Reason      string   `json:"reason,omitempty"`
	ParamFingerprint string `json:"paramFingerprint,omitempty"`
	Supersedes  string   `json:"supersedes,omitempty"`
}

// Severity levels for diagnostics.
type Severity string

const (
	SevInfo     Severity = "info"
	SevWarning  Severity = "warning"
	SevError    Severity = "error"
	SevBudget   Severity = "budget-exhausted"
)

// Diagnostic carries unconvertible values and structural findings; they stay
// attached at the original location instead of being dropped for display.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Where    string   `json:"where"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	ObjectID string   `json:"objectId,omitempty"`
}

// AnalysisResult is one versioned derivation over one blob.
type AnalysisResult struct {
	ID                string             `json:"id"`
	BlobID            string             `json:"blobId"`
	AlgoVersion       string             `json:"algoVersion"`
	ParamFingerprint  string             `json:"paramFingerprint"`
	Sources           []SourceRef        `json:"sources"`
	Header            HeaderInfo         `json:"header"`
	IFDs              []IFD              `json:"ifds"`
	RootIDs           []string           `json:"rootIds"`
	Edges             []GraphEdge        `json:"edges"`
	SharedSubdirs     []SharedSubdir     `json:"sharedSubdirs"`
	Cycle             *CycleEvidence     `json:"cycle,omitempty"`
	BudgetExhausted   bool               `json:"budgetExhausted"`
	Budget            BudgetReport       `json:"budget"`
	BlockMaps         []BlockMap         `json:"blockMaps"`
	GeoKeyDirs        []GeoKeyDir        `json:"geoKeyDirs"`
	Candidates        []TransformCandidate `json:"candidates"`
	TransformConflict bool               `json:"transformConflict"`
	Diagnostics       []Diagnostic       `json:"diagnostics"`
	CreatedAt         time.Time          `json:"createdAt"`
}

// HeaderInfo preserves the header bytes and framing decisions.
type HeaderInfo struct {
	ByteOrder    ByteOrder `json:"byteOrder"`
	Kind         Kind      `json:"kind"`
	FirstIFDOff  int64     `json:"firstIfdOffset"`
	HeaderBytes  []byte    `json:"headerBytes"`
	Magic        uint16    `json:"magic"`
	Version      uint16    `json:"version,omitempty"`
	Offsetsize   uint16    `json:"offsetSize,omitempty"`
}

// GraphEdge links IFDs in the directory graph.
type GraphEdge struct {
	From   string      `json:"from"`
	To     string      `json:"to"`
	Rel    SubfileKind `json:"rel"`
	SourceTagID uint16  `json:"sourceTagId"`
	Index  int         `json:"index"`
}

// SharedSubdir proves one IFD content object is referenced more than once.
type SharedSubdir struct {
	IFDID   string         `json:"ifdId"`
	Parents []GraphEdge    `json:"parents"`
}

// CycleEvidence records the first cycle and the budget state.
type CycleEvidence struct {
	Path   []string `json:"path"`
	Detail string   `json:"detail"`
}

// BudgetReport makes the visit limit first-class evidence.
type BudgetReport struct {
	MaxVisits  int `json:"maxVisits"`
	Visits     int `json:"visits"`
	RejectedAt string `json:"rejectedAt,omitempty"`
}
