package tiffparse

type Endian string

const (
	LittleEndian Endian = "little"
	BigEndian    Endian = "big"
)

type Format string

const (
	ClassicTIFF Format = "classic"
	BigTIFF     Format = "big_tiff"
)

type RawTag struct {
	ID              uint16   `json:"id"`
	Name            string   `json:"name"`
	TypeID          uint16   `json:"type_id"`
	TypeName        string   `json:"type_name"`
	Count           uint64   `json:"count"`
	ValueFieldBytes []byte   `json:"value_field_bytes"`
	Values          []uint64 `json:"values,omitempty"`
	Inline          bool     `json:"inline"`
	ExternalOffset  uint64   `json:"external_offset,omitempty"`
	ExternalLength  uint64   `json:"external_length,omitempty"`
	ValueBytes      []byte   `json:"value_bytes,omitempty"`
	Truncated       bool     `json:"truncated"`
}

type IFDNode struct {
	ID            string    `json:"id"`
	Offset        uint64    `json:"offset"`
	Kind          string    `json:"kind"`
	EntryCount    uint64    `json:"entry_count"`
	FirstSeenFrom string    `json:"first_seen_from"`
	Bytes         ByteRange `json:"bytes"`
	Tags          []RawTag  `json:"tags"`
	NextIFD       uint64    `json:"next_ifd"`
	SubIFDs       []uint64  `json:"sub_ifds"`
}

type IFDLink struct {
	ID              string `json:"id"`
	From            string `json:"from"`
	To              string `json:"to"`
	Kind            string `json:"kind"`
	Offset          uint64 `json:"offset"`
	TagID           uint16 `json:"tag_id,omitempty"`
	Index           int    `json:"index,omitempty"`
	Shared          bool   `json:"shared,omitempty"`
	Cycle           bool   `json:"cycle,omitempty"`
	BlockedByBudget bool   `json:"blocked_by_budget,omitempty"`
}

type ByteRange struct {
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

type Budget struct {
	NodeLimit       int    `json:"node_limit"`
	NodesVisited    int    `json:"nodes_visited"`
	ExternalLimit   int    `json:"external_limit"`
	ExternalReads   int    `json:"external_reads"`
	Exhausted       bool   `json:"exhausted"`
	ExhaustedKind   string `json:"exhausted_kind,omitempty"`
	ExhaustedAtLink string `json:"exhausted_at_link,omitempty"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Offset   uint64 `json:"offset,omitempty"`
	Length   uint64 `json:"length,omitempty"`
	IFDID    string `json:"ifd_id,omitempty"`
	TagID    uint16 `json:"tag_id,omitempty"`
	LinkID   string `json:"link_id,omitempty"`
}

type Result struct {
	SourceID    string       `json:"source_id"`
	Endian      Endian       `json:"endian"`
	Format      Format       `json:"format"`
	HeaderBytes ByteRange    `json:"header_bytes"`
	RootOffset  uint64       `json:"root_offset"`
	IFDs        []IFDNode    `json:"ifds"`
	Links       []IFDLink    `json:"links"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Budget      Budget       `json:"budget"`
	Complete    bool         `json:"complete"`
}
