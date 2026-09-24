// Package geo turns preserved TIFF tags into GeoTIFF interpretation. The raw
// representation is owned by package tiff; everything here is a *derived*
// candidate and therefore carries an algorithm version, parameter fingerprint
// and direct sources. When the model is ambiguous (multiple tiepoints, both
// ModelTiepoint+Scale and a 4x4 Transformation, duplicate GeoKey ids) every
// legal candidate is retained; nothing is collapsed to one number.
package geo

// AlgorithmVersion identifies the geo interpretation logic.
const AlgorithmVersion = "geo-interpret-v1"

// KeyHeader preserves the GeoKey directory version triple (spec 4.2.4):
// KeyDirectoryVersion, KeyRevision, MinorRevision, NumberOfKeys.
type KeyHeader struct {
	KeyDirectoryVersion uint16
	KeyRevision         uint16
	MinorRevision       uint16
	NumberOfKeys        uint16
}

// GeoKey is one parsed key entry. The raw 4 SHORT tuple stays in SourceRange;
// the resolved value (when it comes from GeoDoubleParams / GeoAsciiParams)
// additionally references that parameter tag and its index.
type GeoKey struct {
	ID          uint16  `json:"id"`
	Location    uint16  `json:"location"`
	Count       uint16  `json:"count"`
	ValueOffset int64   `json:"valueOffset"`
	// Value is a string/double/short representation for display; RawKind says
	// how it was obtained. Unconvertible values stay empty with a diagnostic.
	Value  string `json:"value,omitempty"`
	Kind   string `json:"kind"` // short | double | ascii | unresolved
	Name   string `json:"name,omitempty"`
	Index  int    `json:"index"`
	Source string `json:"source"` // stable id of the originating tag/value
	ParamSource string `json:"paramSource,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// TransformKind enumerates the legal raster-to-model representations.
type TransformKind string

const (
	KindTiepointScale TransformKind = "tiepoint_scale"
	KindTransformation TransformKind = "matrix4x4"
)

// Matrix is a row-major 4x4 ModelTransformation.
type Matrix [16]float64

// Tiepoint is one (I,J,K -> X,Y,Z) sextet from ModelTiepoint.
type Tiepoint struct {
	I, J, K, X, Y, Z float64
}

// Scale is the (Sx,Sy,Sz) ModelPixelScale triple, carrying its linear unit.
type Scale struct {
	Sx, Sy, Sz float64
	Unit       string `json:"unit"`
	UnitDatum  string `json:"unitDatum"`
}

// TransformCandidate is one legal raster-to-model mapping.
type TransformCandidate struct {
	ID       string        `json:"id"`
	Kind     TransformKind `json:"kind"`
	Accepted bool          `json:"accepted"`
	// Sources are stable tag/value ids this candidate was derived from.
	Sources []string `json:"sources"`
	// Tiepoints / scale apply to KindTiepointScale.
	Tiepoints []Tiepoint `json:"tiepoints,omitempty"`
	Scale      *Scale     `json:"scale,omitempty"`
	Matrix     *Matrix    `json:"matrix,omitempty"`
	// Conflict expresses why this candidate cannot be uniquely selected.
	Conflict string `json:"conflict,omitempty"`
	// RejectReason is set when a candidate was invalid/unconvertible; the
	// candidate is still retained rather than dropped.
	RejectReason string `json:"rejectReason,omitempty"`
	Diagnostic   string `json:"diagnostic,omitempty"`
}

// Analysis is the derived geo view for one IFD.
type Analysis struct {
	IFDKey string `json:"ifdKey"`
	Header *KeyHeader `json:"header,omitempty"`
	Keys   []GeoKey `json:"keys"`
	Candidates []TransformCandidate `json:"candidates"`
	// KeyConflicts records duplicated key ids (per-spec illegal).
	KeyConflicts []KeyConflict `json:"keyConflicts,omitempty"`
	Diagnostics  []string `json:"diagnostics,omitempty"`
}

// KeyConflict records two key entries sharing the same key id.
type KeyConflict struct {
	KeyID   uint16 `json:"keyId"`
	Indexes []int  `json:"indexes"`
	Message string `json:"message"`
}
