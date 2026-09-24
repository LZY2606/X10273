// Package model defines the durable state of the IFD waypoint archive.
//
// All identities in this package are content/position-derived so that an
// export followed by database deletion and import reproduces the exact same
// ids, ordering and provenance edges. Nothing here is randomly generated.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// StableID is a deterministic, export-stable identifier.
type StableID string

// ByteOrder of a TIFF header.
type ByteOrder string

const (
	LittleEndian ByteOrder = "LE"
	BigEndian    ByteOrder = "BE"
)

// TIFFClass distinguishes classic TIFF (4-byte offsets) from BigTIFF (8-byte).
type TIFFClass string

const (
	Classic TIFFClass = "classic"
	BigTIFF TIFFClass = "bigtiff"
)

// Header is the 8/16 byte TIFF header.
type Header struct {
	ByteOrder     ByteOrder `json:"byteOrder"`
	Magic         uint16    `json:"magic"`
	IFDOffset     uint64    `json:"ifd0Offset"`
	Class         TIFFClass `json:"class"`
	BigTIFFOffset []uint64  `json:"bigtiffGuardOffsets,omitempty"`
	OffsetSize    int       `json:"offsetSize"`
	EntrySize     int       `json:"entrySize"`
	SizeBytes     int       `json:"headerSize"`
	BlobBytes     int       `json:"blobBytes"`
	HeaderOK      bool      `json:"headerOK"`
}

// ValueKind says whether a tag value lives inline in the IFD entry or at an
// external data offset.
type ValueKind string

const (
	Inline   ValueKind = "inline"
	External ValueKind = "external"
)

// TagEntry keeps the raw tag value exactly as encoded.
type TagEntry struct {
	KeyID        StableID      `json:"keyId"`
	Tag          uint16        `json:"tag"`
	TagName      string        `json:"tagName"`
	TypeID       uint16        `json:"typeId"`
	TypeName     string        `json:"typeName"`
	Count        uint64        `json:"count"`
	RawValueHex  string        `json:"rawValueHex"`
	InlineHex    string        `json:"inlineHex"`
	Kind         ValueKind     `json:"kind"`
	ValueOffset  uint64        `json:"valueOffset,omitempty"`
	ExternalSize int           `json:"externalSize,omitempty"`
	EntryStart   int64         `json:"entryStart"`
	EntryEnd     int64         `json:"entryEnd"`
	ExternalEnd  int64         `json:"externalEnd,omitempty"`
	Values       []interface{} `json:"values"`
	OutOfBlob    bool          `json:"outOfBlob,omitempty"`
	Diagnostics  []Diagnosis   `json:"diagnostics,omitempty"`
}

// EdgeKind enumerates provenance edges in the IFD graph.
type EdgeKind string

const (
	EdgeNext   EdgeKind = "next"
	EdgeSubIFD EdgeKind = "subifd"
	EdgeExif   EdgeKind = "exif"
	EdgeGPS    EdgeKind = "gps"
)

// IFDEdge is a directed edge between two IFD nodes.
type IFDEdge struct {
	EdgeID    StableID `json:"edgeId"`
	From      StableID `json:"from"`
	To        StableID `json:"to"`
	Kind      EdgeKind `json:"kind"`
	Offset    uint64   `json:"offset"`
	Cyclic    bool     `json:"cyclic"`
	Order     int      `json:"order"`
	SourceTag uint16   `json:"sourceTag,omitempty"`
}

// IFDNode is one image file directory.
type IFDNode struct {
	NodeID      StableID    `json:"nodeId"`
	Offset      uint64      `json:"offset"`
	Order       int         `json:"order"`
	EntryStart  int64       `json:"entryStart"`
	EntriesEnd  int64       `json:"entriesEnd"`
	NextOffset  uint64      `json:"nextOffset,omitempty"`
	NextPresent bool        `json:"nextPresent"`
	OutOfBlob   bool        `json:"outOfBlob,omitempty"`
	Shared      bool        `json:"shared"`
	Reachable   bool        `json:"reachable"`
	Entries     []TagEntry  `json:"entries"`
	Diagnostics []Diagnosis `json:"diagnostics,omitempty"`
}

// Severity of a diagnostic.
type Severity string

const (
	SevInfo   Severity = "info"
	SevWarn   Severity = "warn"
	SevError  Severity = "error"
	SevBudget Severity = "budget"
)

// Diagnosis is a machine-readable finding attached to a node/tag/run.
type Diagnosis struct {
	Code     string                 `json:"code"`
	Severity Severity               `json:"severity"`
	Message  string                 `json:"message"`
	Detail   map[string]interface{} `json:"detail,omitempty"`
}

// BudgetCounters records the access budget spent by the parser.
type BudgetCounters struct {
	MaxIFDVisits    int    `json:"maxIfdVisits"`
	IFDVisits       int    `json:"ifdVisits"`
	MaxPointerSteps int    `json:"maxPointerSteps"`
	PointerSteps    int    `json:"pointerSteps"`
	MaxBytes        int64  `json:"maxBytes"`
	ExternalBytes   int64  `json:"externalBytes"`
	Exhausted       bool   `json:"exhausted"`
	ExhaustedCode   string `json:"exhaustedCode,omitempty"`
}

// ParseRun is the output of parsing one raw input.
type ParseRun struct {
	RunID            StableID       `json:"runId"`
	BlobID           StableID       `json:"blobId"`
	InputVersion     int            `json:"inputVersion"`
	AlgoVersion      string         `json:"algoVersion"`
	ParamFingerprint string         `json:"paramFingerprint"`
	Header           Header         `json:"header"`
	RootNodeID       StableID       `json:"rootNodeId"`
	Nodes            []IFDNode      `json:"nodes"`
	Edges            []IFDEdge      `json:"edges"`
	Cycles           [][]StableID   `json:"cycles"`
	SharedNodes      []StableID     `json:"sharedNodes"`
	Budget           BudgetCounters `json:"budget"`
	Diagnostics      []Diagnosis    `json:"diagnostics"`
	Partial          bool           `json:"partial"`
	ParseError       string         `json:"parseError,omitempty"`
}

// BlockLayout identifies tile vs strip storage.
type BlockLayout string

const (
	LayoutTile  BlockLayout = "tile"
	LayoutStrip BlockLayout = "strip"
)

// BlockStatus classifies a pixel block's evidence.
type BlockStatus string

const (
	BlockOK            BlockStatus = "ok"
	BlockOutOfBlob     BlockStatus = "out_of_blob"
	BlockOverlap       BlockStatus = "overlap"
	BlockMissing       BlockStatus = "missing"
	BlockCountMismatch BlockStatus = "count_mismatch"
)

// PixelBlock is one tile or strip with byte range evidence.
type PixelBlock struct {
	BlockID      StableID    `json:"blockId"`
	NodeID       StableID    `json:"nodeId"`
	Index        int         `json:"index"`
	Col          int         `json:"col"`
	Row          int         `json:"row"`
	Offset       int64       `json:"offset"`
	ByteCount    int64       `json:"byteCount"`
	Start        int64       `json:"start"`
	End          int64       `json:"end"`
	Status       BlockStatus `json:"status"`
	OverlapsWith []StableID  `json:"overlapsWith,omitempty"`
	Diagnostics  []Diagnosis `json:"diagnostics,omitempty"`
}

// BlockMap groups blocks for one IFD.
type BlockMap struct {
	MapID       StableID     `json:"mapId"`
	NodeID      StableID     `json:"nodeId"`
	Layout      BlockLayout  `json:"layout"`
	ImageWidth  uint32       `json:"imageWidth"`
	ImageLength uint32       `json:"imageLength"`
	BlockWidth  uint32       `json:"blockWidth"`
	BlockLength uint32       `json:"blockLength"`
	Cols        int          `json:"cols"`
	Rows        int          `json:"rows"`
	Expected    int          `json:"expected"`
	Blocks      []PixelBlock `json:"blocks"`
	HasGaps     bool         `json:"hasGaps"`
	HasOverlap  bool         `json:"hasOverlap"`
}

// GeoKey is one key from the GeoKeyDirectoryTag.
type GeoKey struct {
	KeyID         StableID    `json:"keyId"`
	ID            uint16      `json:"id"`
	Name          string      `json:"name"`
	Location      uint16      `json:"location"`
	Count         uint16      `json:"count"`
	ValueOrOffset uint16      `json:"valueOrOffset"`
	Resolved      interface{} `json:"resolved,omitempty"`
	ResolvedTag   uint16      `json:"resolvedTag,omitempty"`
	Unit          string      `json:"unit,omitempty"`
}

// GeoDirectory is the resolved 65535 directory for an IFD.
type GeoDirectory struct {
	DirectoryID StableID    `json:"directoryId"`
	NodeID      StableID    `json:"nodeId"`
	Version     uint16      `json:"keyDirectoryVersion"`
	Revision    uint16      `json:"keyRevision"`
	Minor       uint16      `json:"minorRevision"`
	Keys        []GeoKey    `json:"keys"`
	Duplicates  [][]GeoKey  `json:"duplicates,omitempty"`
	Diagnostics []Diagnosis `json:"diagnostics,omitempty"`
}

// TransformKind for raster-to-model transforms.
type TransformKind string

const (
	KindTiepointScale TransformKind = "tiepoint_scale"
	KindMatrix        TransformKind = "model_transformation"
)

// TransformCandidate is a legal but possibly conflicting geo transform.
type TransformCandidate struct {
	CandidateID  StableID      `json:"candidateId"`
	NodeID       StableID      `json:"nodeId"`
	Kind         TransformKind `json:"kind"`
	Matrix       [16]float64   `json:"matrix"`
	Valid        bool          `json:"valid"`
	ConflictKey  string        `json:"conflictKey"`
	SourceTagIDs []StableID    `json:"sourceTagIds"`
	Unit         string        `json:"unit"`
	DatumBasis   string        `json:"datumBasis"`
	Diagnostics  []Diagnosis   `json:"diagnostics,omitempty"`
}

// Finding is the derived analysis for one parse run.
type Finding struct {
	FindingID        StableID             `json:"findingId"`
	RunID            StableID             `json:"runId"`
	NodeOrder        []StableID           `json:"nodeOrder"`
	BlockMaps        []BlockMap           `json:"blockMaps"`
	GeoDirectories   []GeoDirectory       `json:"geoDirectories"`
	Candidates       []TransformCandidate `json:"candidates"`
	Conflicts        []GeoConflict        `json:"conflicts"`
	Diagnostics      []Diagnosis          `json:"diagnostics"`
	AlgoVersion      string               `json:"algoVersion"`
	ParamFingerprint string               `json:"paramFingerprint"`
}

// GeoConflict ties competing candidates/keys together.
type GeoConflict struct {
	ConflictID StableID   `json:"conflictId"`
	NodeID     StableID   `json:"nodeId"`
	Kind       string     `json:"kind"`
	SubjectIDs []StableID `json:"subjectIds"`
	Message    string     `json:"message"`
}

// Decision records an accept/reject/param human action.
type Decision struct {
	DecisionID StableID `json:"decisionId"`
	EventID    StableID `json:"eventId"`
	Action     string   `json:"action"`
	TargetID   StableID `json:"targetId"`
	ConflictID StableID `json:"conflictId,omitempty"`
	Note       string   `json:"note"`
	Supersedes StableID `json:"supersedes,omitempty"`
	Active     bool     `json:"active"`
}

// ParseParams are parser budget/limit parameters.
type ParseParams struct {
	MaxIFDVisits    int    `json:"maxIfdVisits"`
	MaxPointerSteps int    `json:"maxPointerSteps"`
	MaxBytes        int64  `json:"maxBytes"`
	Fingerprint     string `json:"fingerprint"`
	Version         int    `json:"version"`
	ParentVersion   int    `json:"parentVersion,omitempty"`
}

// RawInput is an immutable submitted blob.
type RawInput struct {
	BlobID        StableID  `json:"blobId"`
	SHA256        string    `json:"sha256"`
	ByteCount     int       `json:"byteCount"`
	ByteOrderHint ByteOrder `json:"byteOrderHint,omitempty"`
	Label         string    `json:"label"`
	Version       int       `json:"inputVersion"`
	ParentVersion int       `json:"parentVersion,omitempty"`
	Rejected      bool      `json:"rejected,omitempty"`
	RejectReason  string    `json:"rejectReason,omitempty"`
}

// Event types.
const (
	EvProjectCreated = "project.created"
	EvInputSubmitted = "input.submitted"
	EvInputRejected  = "input.rejected"
	EvParamsRevised  = "params.revised"
	EvParsed         = "parse.completed"
	EvDerived        = "analysis.completed"
	EvDecided        = "decision.recorded"
	EvDecisionUndone = "decision.undone"
	EvCaseReplayed   = "case.replayed"
)

// Event is the append-only unit of history.
type Event struct {
	EventID    StableID        `json:"eventId"`
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Time       string          `json:"time"`
	ClockSeq   int             `json:"clockSeq"`
	Payload    interface{}     `json:"payload"`
	PayloadRaw json.RawMessage `json:"-"`
}

// HashID builds a stable hex id from tagged components.
func HashID(prefix string, parts ...interface{}) StableID {
	h := sha256.New()
	fmt.Fprintf(h, "%s|", prefix)
	for _, p := range parts {
		b, _ := json.Marshal(p)
		h.Write(b)
		h.Write([]byte{0})
	}
	return StableID(prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16])
}

// HashString returns a short stable hash of a string.
func HashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// SortedStableIDs returns a deterministic sorted id list.
func SortedStableIDs(ids []StableID) []StableID {
	out := append([]StableID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// JoinIDs renders ids joined with '|' for fingerprinting.
func JoinIDs(ids []StableID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = string(id)
	}
	return strings.Join(parts, "|")
}
