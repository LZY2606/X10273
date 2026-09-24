package tiff

import "ifdlantern/internal/domain"

// Header preserves the raw leading bytes and variant.
type Header struct {
	Endian   string    `json:"endian"`     // "little" | "big"
	Variant  string    `json:"variant"`    // "classic" | "bigtiff"
	Magic    int       `json:"magic"`
	FirstIFD int64     `json:"first_ifd"`
	Bytes    ByteRange `json:"bytes"`
	Raw4     []byte    `json:"raw4"`
	// BigTIFF-only offsetsize/zero are kept verbatim.
	OffsetSize int    `json:"offset_size,omitempty"`
	Zero       int    `json:"zero,omitempty"`
}

// RawTag keeps the tag's original integer representation plus where its bytes
// live. External offsets are never hidden behind normalisation.
type RawTag struct {
	Tag       int       `json:"tag"`
	Name      string    `json:"name,omitempty"`
	Type      int       `json:"type"`
	TypeName  string    `json:"type_name"`
	Count     int64     `json:"count"`
	Inline    bool      `json:"inline"`
	ValueOff  int64     `json:"value_offset"` // interpreted offset/value field
	EntryRange ByteRange `json:"entry_range"`    // the directory entry itself
	Range     ByteRange `json:"range"`        // external bytes (length 0 if inline)
	Raw       []byte    `json:"raw"`          // preserved bytes (capped)
	Truncated bool      `json:"truncated,omitempty"`
	RawKeep   string    `json:"raw_keep,omitempty"` // original text for ASCII
	Values    []Value   `json:"values,omitempty"`   // typed values, derived
}

// Value is the derived typed representation of one element.
type Value struct {
	N    float64 `json:"n,omitempty"`
	N2   float64 `json:"n2,omitempty"` // denominator for rationals
	S    string  `json:"s,omitempty"`
	Keep string  `json:"keep"` // original textual representation
}

// IFD is one directory node.
type IFD struct {
	ID         string    `json:"id"`     // stable: ifd-<16 hex>
	Offset     int64     `json:"offset"` // logical offset within blob
	Range      ByteRange `json:"range"`
	Tags       []RawTag  `json:"tags"`
	Next       int64     `json:"next"` // next IFD pointer
	SubPtrs    []int64   `json:"sub_ptrs"`
	Shared     bool      `json:"shared"`           // reachable via >1 parent chain
	Parents    []string  `json:"parents"`
	CycleClose string    `json:"cycle_close,omitempty"` // id of earlier node this closes to
}

// Block is one tile/strip pixel chunk.
type Block struct {
	Index      int       `json:"index"`
	Kind       string    `json:"kind"` // "tile" | "strip"
	DataRange  ByteRange `json:"data_range"`
	ByteCount  int64     `json:"byte_count"`
	PixelX     int       `json:"pixel_x"`
	PixelY     int       `json:"pixel_y"`
	PixelW     int       `json:"pixel_w"`
	PixelH     int       `json:"pixel_h"`
	Aliases    []int     `json:"aliases,omitempty"`
	Flags      []string  `json:"flags"` // overlap/alias/missing/oob-*
}

// CoverageCell records a pixel cell's block membership state.
type CoverageCell struct {
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Blocks []int `json:"blocks"`
	State  string `json:"state"` // covered|overlap|missing
}

// BlockLayout is the pixel-storage analysis for one IFD.
type BlockLayout struct {
	Mode        string         `json:"mode"` // tiled|stripped|none
	ImageW      int            `json:"image_w"`
	ImageH      int            `json:"image_h"`
	TileW       int            `json:"tile_w,omitempty"`
	TileH       int            `json:"tile_h,omitempty"`
	RowsPerStrip int           `json:"rows_per_strip,omitempty"`
	Blocks      []Block        `json:"blocks"`
	Cells       []CoverageCell `json:"cells,omitempty"` // capped grid
	CellsCapped bool           `json:"cells_capped,omitempty"`
	Stats       map[string]int `json:"stats"`
	Diags       []domain.Diagnostic `json:"diags"`
}

// GeoKeyEntry is one resolved GeoKey.
type GeoKeyEntry struct {
	ID     int    `json:"id"`
	Name   string `json:"name,omitempty"`
	Loc    int    `json:"loc"`
	Count  int    `json:"count"`
	Value  string `json:"value"`          // original
	ValueN int    `json:"value_n,omitempty"`
}

// TransformCandidate is a legal geo transform interpretation.
type TransformCandidate struct {
	ID         string         `json:"id"`
	Origin     string         `json:"origin"` // tag-set
	Matrix     [16]float64    `json:"matrix"` // row-major 4x4, raster->CRS
	CRS        domain.GeoCRS  `json:"crs"`
	UnitAxes   [2]string      `json:"unit_axes"`
	Status     domain.Status  `json:"status"`
	Diags      []domain.Diagnostic `json:"diags,omitempty"`
}

// GeoModel is the GeoTIFF analysis for one IFD.
type GeoModel struct {
	KeyVersion int                 `json:"key_version"`
	KeyRev     string              `json:"key_revision,omitempty"`
	Keys       []GeoKeyEntry       `json:"keys"`
	KeyDiags   []domain.Diagnostic `json:"key_diags,omitempty"`
	Candidates []TransformCandidate `json:"candidates"`
}

// ParseReport is the complete provenance graph extracted from one blob.
type ParseReport struct {
	Header   Header              `json:"header"`
	IFDs     []*IFD              `json:"ifds"`
	Layouts  map[string]*BlockLayout `json:"layouts"`
	Geos     map[string]*GeoModel    `json:"geos"`
	Diags    []domain.Diagnostic `json:"diags"`
	Budget   BudgetReport        `json:"budget"`
	AlgoVer  string              `json:"algo_version"`
	ParamFP  string              `json:"param_fingerprint"`
	SourceID string              `json:"source_id"`
}

// Budget constrains parser work so cyclic/shared IFD graphs cannot loop forever.
type Budget struct {
	MaxIFDVisits int // charge per directed edge traversal into an IFD
	MaxTagBytes  int64
	MaxGridCells int
}

// BudgetReport records what was spent and where the limit stopped the walk.
type BudgetReport struct {
	Limit  Budget `json:"limit"`
	Visits int    `json:"visits"`
	Edges  int    `json:"edges"`
	Exhausted bool   `json:"exhausted"`
	StopAt string `json:"stop_at,omitempty"` // stable id of node
	StopReason string `json:"stop_reason,omitempty"`
}

func DefaultBudget() Budget {
	return Budget{MaxIFDVisits: 4096, MaxTagBytes: 1 << 20, MaxGridCells: 4096}
}

// StrictBudget is used by budget-exhaustion cases.
func StrictBudget() Budget {
	return Budget{MaxIFDVisits: 4, MaxTagBytes: 1 << 20, MaxGridCells: 4096}
}
