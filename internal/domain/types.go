package domain

type UnitRef struct {
	Value interface{} `json:"value"`
	Unit  string      `json:"unit"`
	Basis string      `json:"basis"`
}

type Provenance struct {
	AlgorithmVersion string   `json:"algorithm_version"`
	ParameterHash    string   `json:"parameter_hash"`
	DirectSources    []string `json:"direct_sources"`
	SourceEventID    string   `json:"source_event_id,omitempty"`
}

type Block struct {
	ID         string   `json:"id"`
	IFDID      string   `json:"ifd_id"`
	Index      int      `json:"index"`
	Kind       string   `json:"kind"`
	X          int      `json:"x"`
	Y          int      `json:"y"`
	Width      uint64   `json:"width"`
	Height     uint64   `json:"height"`
	Offset     *uint64  `json:"offset,omitempty"`
	ByteCount  *uint64  `json:"byte_count,omitempty"`
	ByteLength uint64   `json:"byte_length"`
	State      string   `json:"state"`
	Reasons    []string `json:"reasons,omitempty"`
	Overlaps   []string `json:"overlaps,omitempty"`
	OutOfBounds []ByteRange `json:"out_of_bounds,omitempty"`
}

type ByteRange struct {
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

type GeoKey struct {
	ID          uint16      `json:"id"`
	Name        string      `json:"name"`
	Location    uint16      `json:"location"`
	LocationTag uint16      `json:"location_tag"`
	Count       uint16      `json:"count"`
	RawValue    uint16      `json:"raw_value"`
	ResolvedValue interface{} `json:"resolved_value,omitempty"`
	ValueBasis  string      `json:"value_basis"`
	Diagnostic  string      `json:"diagnostic,omitempty"`
}

type GeoKeyDirectory struct {
	ID            string    `json:"id"`
	IFDID         string    `json:"ifd_id"`
	KeyDirectoryVersion uint16 `json:"key_directory_version"`
	KeyRevision   struct {
		Major uint16 `json:"major"`
		Minor uint16 `json:"minor"`
	} `json:"key_revision"`
	Keys []GeoKey `json:"keys"`
	Diagnostics []string `json:"diagnostics"`
}

type AffineTransform struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
	C float64 `json:"c"`
	D float64 `json:"d"`
	E float64 `json:"e"`
	F float64 `json:"f"`
}

type TransformCandidate struct {
	ID          string            `json:"id"`
	IFDID       string            `json:"ifd_id"`
	Kind        string            `json:"kind"`
	Transform   AffineTransform   `json:"transform"`
	State       string            `json:"state"`
	Reasons     []string          `json:"reasons,omitempty"`
	RejectedReason string         `json:"rejected_reason,omitempty"`
	ErrorMargin *UnitRef          `json:"error_margin,omitempty"`
	UnitBasis   string            `json:"unit_basis"`
	GeoCRS      *GeoCRS           `json:"geo_crs,omitempty"`
	Provenance  Provenance        `json:"provenance"`
}

type GeoCRS struct {
	Kind string `json:"kind"`
	Code uint16 `json:"code,omitempty"`
	Name string `json:"name"`
	Basis string `json:"basis"`
}

type IFDView struct {
	ID            string                 `json:"id"`
	Offset        uint64                 `json:"offset"`
	Kind          string                 `json:"kind"`
	Bytes         ByteRange              `json:"bytes"`
	TagRanges     []TagByteRange         `json:"tag_ranges"`
	EntryCount    uint64                 `json:"entry_count"`
	NextIFD       uint64                 `json:"next_ifd"`
	SubIFDs       []uint64               `json:"sub_ifds"`
	Raw           map[string]interface{} `json:"raw"`
	Shared        bool                   `json:"shared"`
	Cycle         bool                   `json:"cycle"`
	Blocked       bool                   `json:"blocked"`
	Provenance    Provenance             `json:"provenance"`
	Diagnostics   []string               `json:"diagnostics,omitempty"`
}

type TagByteRange struct {
	TagID uint16 `json:"tag_id"`
	Name string `json:"name"`
	Entry ByteRange `json:"entry"`
	Value *ByteRange `json:"value,omitempty"`
	Inline bool `json:"inline"`
	Truncated bool `json:"truncated"`
}

type LinkView struct {
	ID string `json:"id"`
	From string `json:"from"`
	To string `json:"to"`
	Kind string `json:"kind"`
	Offset uint64 `json:"offset"`
	TagID uint16 `json:"tag_id,omitempty"`
	Index int `json:"index,omitempty"`
	Shared bool `json:"shared"`
	Cycle bool `json:"cycle"`
	Blocked bool `json:"blocked"`
}

type Evidence struct {
	Code string `json:"code"`
	Severity string `json:"severity"`
	Message string `json:"message"`
	ObjectID string `json:"object_id,omitempty"`
	ByteRange *ByteRange `json:"byte_range,omitempty"`
}

type Analysis struct {
	ID string `json:"id"`
	ProjectID string `json:"project_id"`
	Version int `json:"version"`
	ParameterRevision int `json:"parameter_revision"`
	SourceSet []string `json:"source_set"`
	AlgorithmVersion string `json:"algorithm_version"`
	ParameterHash string `json:"parameter_hash"`
	Endian []string `json:"endian"`
	Formats []string `json:"formats"`
	IFDs []IFDView `json:"ifds"`
	Links []LinkView `json:"links"`
	Blocks []Block `json:"blocks"`
	GeoKeys []GeoKeyDirectory `json:"geo_keys"`
	Transforms []TransformCandidate `json:"transforms"`
	Evidence []Evidence `json:"evidence"`
	Budgets []BudgetView `json:"budgets"`
	Status string `json:"status"`
	Current bool `json:"current"`
}

type BudgetView struct {
	SourceID string `json:"source_id"`
	NodeLimit int `json:"node_limit"`
	NodesVisited int `json:"nodes_visited"`
	ExternalLimit int `json:"external_limit"`
	ExternalReads int `json:"external_reads"`
	Exhausted bool `json:"exhausted"`
	Kind string `json:"kind,omitempty"`
	AtLink string `json:"at_link,omitempty"`
}

type Parameters struct {
	NodeLimit int `json:"node_limit"`
	ExternalLimit int `json:"external_limit"`
	MaxTileOverlapChecks int `json:"max_tile_overlap_checks"`
}

func DefaultParameters() Parameters {
	return Parameters{NodeLimit: 128, ExternalLimit: 4096, MaxTileOverlapChecks: 2048}
}
