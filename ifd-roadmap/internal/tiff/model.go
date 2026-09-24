package tiff

// File is the raw structural parse of a TIFF/GeoTIFF byte stream. Nothing is
// normalized: byte orders, raw tag values and external offsets are preserved.
type File struct {
	BigTIFF     bool         `json:"bigTiff"`
	Order       ByteOrder    `json:"order"`
	HeaderRange ByteRange    `json:"headerRange"`
	Magic       uint16       `json:"magic"`
	VersionRaw  uint16       `json:"versionRaw"` // 42 for classic, 43 for BigTIFF
	FirstIFDOff int64        `json:"firstIfdOffset"`
	IFDOffSize  int          `json:"ifdOffsetSize"` // 4 classic, 8 BigTIFF
	IFDs        []*IFD       `json:"ifds"`
	ByOffset    map[int64]*IFD `json:"-"`
	Edges       []Edge       `json:"edges"`
	Roots       []string     `json:"roots"` // IFD node ids reached from the header
	Diags       []Diagnostic `json:"diags"`
	Size        int64        `json:"size"`
}

// Entry is one tag record, preserving the raw on-disk fields and the exact
// byte range of both the inline value slot and the external value area.
type Entry struct {
	ID            string    `json:"id"`
	Tag           uint16    `json:"tag"`
	TagName       string    `json:"tagName"`
	Type          uint16    `json:"type"`
	TypeName      string    `json:"typeName"`
	Count         uint64    `json:"count"`
	InlineRaw     string    `json:"inlineRawHex"` // raw bytes of the 4/8 byte value slot
	ValueOffset   int64     `json:"valueOffset"`  // offset field, interpreted only for external values
	ValueOffsets []int64   `json:"valueOffsets,omitempty"`
	EntryRange    ByteRange `json:"entryRange"`
	ValueRange    ByteRange `json:"valueRange"`      // external area; empty when inline
	External      bool      `json:"external"`
	ValueInBounds bool      `json:"valueInBounds"`
	Values        []Value   `json:"values"`
	RawValues     []string  `json:"rawValues"` // per-element raw representation, unnormalized
}

// Value carries both the decoded value and its raw textual representation.
type Value struct {
	Text string      `json:"text"`
	Raw  interface{} `json:"raw"` // int64, uint64, float64, Rational, string
}

type Rational struct {
	Numerator   int64  `json:"numerator"`
	Denominator int64  `json:"denominator"`
	Raw         string `json:"raw"`
}

// IFD is one image file directory (a node in the IFD graph).
type IFD struct {
	ID          string    `json:"id"`
	Offset      int64     `json:"offset"`
	Range       ByteRange `json:"range"`
	Entries     []*Entry  `json:"entries"`
	EntryCount  uint64    `json:"entryCount"`
	NextIFDOff  int64     `json:"nextIfdOffset"`
	Shared      bool      `json:"shared"`          // reached from more than one parent
	RootReach   int       `json:"rootReachCount"`  // number of graph paths reaching it
	InCycle     bool      `json:"inCycle"`
	VisitRoles  []string  `json:"visitRoles"`      // e.g. "next", "subifd#330[1]"
	Unparsable  bool      `json:"unparsable"`
	Diags       []Diagnostic `json:"diags,omitempty"`
}

// Edge is one directed link in the IFD graph.
type Edge struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Tag    uint16 `json:"tag"`
	Kind   string `json:"kind"` // "next" or "subifd"
	Index  int    `json:"index,omitempty"`
	BackRef bool  `json:"backRef"` // closes a cycle
}

func (f *File) Entry(tag uint16) (*Entry, *IFD) {
	for _, d := range f.IFDs {
		for _, e := range d.Entries {
			if e.Tag == tag {
				return e, d
			}
		}
	}
	return nil, nil
}

func (d *IFD) Entry(tag uint16) *Entry {
	for _, e := range d.Entries {
		if e.Tag == tag {
			return e
		}
	}
	return nil
}
