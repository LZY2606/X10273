// Package tiff is a deliberately small, provenance-oriented TIFF/BigTIFF
// parser. It never hides the original representation: tag values, external
// offsets and IFD chain edges are all retained for the review UI.
package tiff

import "ifdroom/internal/event"

type Endian string

const (
	LittleEndian Endian = "II"
	BigEndian    Endian = "MM"
)

type Version string

const (
	Classic Version = "classic"
	BigTIFF  Version = "bigtiff"
)

// Severity of a parser/layout diagnostic.
type Severity string

const (
	SevFatal   Severity = "fatal"
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// Header is the exact 8/16-byte TIFF header.
type Header struct {
	Endian        Endian  `json:"endian"`
	Version       Version `json:"version"`
	Magic         uint16  `json:"magic"`
	IFDOffsetSize int     `json:"ifd_offset_size"`
	FirstIFD      int64   `json:"first_ifd"`
	HeaderRange   event.ByteRange `json:"header_range"`
}

// Tag is one directory entry. RawValue keeps the on-disk bytes of inline or
// external data; ExternalRange is set when the value lived outside the IFD.
type Tag struct {
	ID             uint16          `json:"tag_id"`
	Name           string          `json:"name"`
	TypeCode       uint16          `json:"type_code"`
	TypeName       string          `json:"type_name"`
	Count          uint64          `json:"count"`
	EntryRange     event.ByteRange `json:"entry_range"`
	ValueField     []byte          `json:"value_field"`
	External       bool            `json:"external"`
	ExternalRange  event.ByteRange `json:"external_range,omitempty"`
	RawValue       []byte          `json:"raw_value"`
	Values         []interface{}   `json:"values"`
}

// EdgeKind enumerates how one IFD points at another.
type EdgeKind string

const (
	EdgeNext   EdgeKind = "next_ifd"
	EdgeSubIFD EdgeKind = "sub_ifd"
)

type Edge struct {
	From string  `json:"from"`
	To   string  `json:"to"`
	Kind EdgeKind `json:"kind"`
	// OffsetTag is 0 for next-IFD edges, otherwise the containing tag id.
	OffsetTag uint16 `json:"offset_tag,omitempty"`
	// Index disambiguates multiple SubIFD pointers in one tag.
	Index int `json:"index,omitempty"`
}

// IFD is a directory node identified by its file offset (stable across reruns).
type IFD struct {
	ID          string          `json:"id"`
	Offset      int64           `json:"offset"`
	Index       int             `json:"index"`
	EntryCount  int             `json:"entry_count"`
	EntryRange  event.ByteRange `json:"entry_range"`
	NextOffset  int64           `json:"next_offset"`
	NextPresent bool            `json:"next_present"`
	Tags        []*Tag          `json:"tags"`
}

type Diagnostic struct {
	Code     string          `json:"code"`
	Severity Severity        `json:"severity"`
	Message  string          `json:"message"`
	Range    event.ByteRange `json:"range,omitempty"`
	IFDID    string          `json:"ifd_id,omitempty"`
}

// Report is the full parse result: it preserves the graph even when cycles,
// shared subdirectories or budget exhaustion occur.
type Report struct {
	Header          *Header       `json:"header,omitempty"`
	IFDs            []*IFD        `json:"ifds"`
	Edges           []Edge        `json:"edges"`
	Diagnostics     []*Diagnostic `json:"diagnostics"`
	Cycles          [][]string    `json:"cycles"`
	SharedSubIFDs   []string      `json:"shared_sub_ifds"`
	BudgetUsed      int           `json:"budget_used"`
	BudgetExhausted bool          `json:"budget_exhausted"`
	Undiscovered    []int64       `json:"undiscovered_offsets"`
	FileSize        int64         `json:"file_size"`
}

func (r *Report) Fatal() *Diagnostic {
	for _, d := range r.Diagnostics {
		if d.Severity == SevFatal {
			return d
		}
	}
	return nil
}
