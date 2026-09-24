// Package tiff parses TIFF/GeoTIFF byte containers into a provenance-rich IFD
// graph. Raw bytes, byte ranges, external value offsets and the IFD chain are
// always preserved; derived decodings never replace the original representation.
package tiff

import (
	"encoding/json"
	"fmt"
)

// Order is the on-disk byte order declared by the TIFF header.
type Order string

const (
	LittleEndian Order = "II"
	BigEndian    Order = "MM"
)

func (o Order) String() string {
	switch o {
	case LittleEndian:
		return "little-endian"
	case BigEndian:
		return "big-endian"
	}
	return "unknown"
}

// Version selects classic TIFF (magic 42) or BigTIFF (magic 43).
type Version int

const (
	ClassicTIFF Version = 42
	BigTIFF     Version = 43
)

func (v Version) String() string {
	switch v {
	case ClassicTIFF:
		return "classic"
	case BigTIFF:
		return "BigTIFF"
	}
	return fmt.Sprintf("unknown(%d)", int(v))
}

// TypeCode is a TIFF field type.
type TypeCode uint16

const (
	TBYTE      TypeCode = 1
	TASCII     TypeCode = 2
	TSHORT     TypeCode = 3
	TLONG      TypeCode = 4
	TRATIONAL  TypeCode = 5
	TSBYTE     TypeCode = 6
	TUNDEFINED TypeCode = 7
	TSSHORT    TypeCode = 8
	TSLONG     TypeCode = 9
	TSRATIONAL TypeCode = 10
	TFLOAT     TypeCode = 11
	TDOUBLE    TypeCode = 12
	TLONG8     TypeCode = 16
	TSLONG8    TypeCode = 17
	TIFD8      TypeCode = 18
)

// Size returns the on-disk size of one value of the type. Unknown types have
// size 0 and are left undecoded.
func (t TypeCode) Size() int {
	switch t {
	case TBYTE, TASCII, TSBYTE, TUNDEFINED:
		return 1
	case TSHORT, TSSHORT:
		return 2
	case TLONG, TSLONG, TFLOAT:
		return 4
	case TRATIONAL, TSRATIONAL, TLONG8, TSLONG8, TIFD8, TDOUBLE:
		return 8
	}
	return 0
}

func (t TypeCode) String() string {
	switch t {
	case TBYTE:
		return "BYTE"
	case TASCII:
		return "ASCII"
	case TSHORT:
		return "SHORT"
	case TLONG:
		return "LONG"
	case TRATIONAL:
		return "RATIONAL"
	case TSBYTE:
		return "SBYTE"
	case TUNDEFINED:
		return "UNDEFINED"
	case TSSHORT:
		return "SSHORT"
	case TSLONG:
		return "SLONG"
	case TSRATIONAL:
		return "SRATIONAL"
	case TFLOAT:
		return "FLOAT"
	case TDOUBLE:
		return "DOUBLE"
	case TLONG8:
		return "LONG8"
	case TSLONG8:
		return "SLONG8"
	case TIFD8:
		return "IFD8"
	}
	return fmt.Sprintf("TYPE%d", uint16(t))
}

// ByteRange is a half-open byte window [Offset, Offset+Length) inside the file.
type ByteRange struct {
	Offset int64 `json:"offset"`
	Length int   `json:"length"`
}

func (r ByteRange) End() int64 { return r.Offset + int64(r.Length) }

// Fraction is a preserved RATIONAL value.
type Fraction struct {
	Num uint64 `json:"num"`
	Den uint64 `json:"den"`
}

// SFraction is a preserved SRATIONAL value.
type SFraction struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

// RawTag keeps the original tag representation: slot bytes, actual value bytes
// and their byte range, plus a derived decoding.
type RawTag struct {
	ID       uint16    `json:"id"`
	Name     string    `json:"name"`
	Type     TypeCode  `json:"type"`
	Count    uint64    `json:"count"`
	Inline   bool      `json:"inline"`
	Slot     []byte    `json:"slot"`          // raw value/offset field as it appears in the IFD entry
	SlotRange ByteRange `json:"slotRange"`     // location of the 4/8 byte value field
	ValueRange ByteRange `json:"valueRange"`   // where the actual value bytes live
	External bool      `json:"external"`      // value lives outside the IFD entry
	Data     []byte    `json:"data,omitempty"` // actual value bytes (when read)
	Values   []any     `json:"values,omitempty"`
	Unread   string    `json:"unread,omitempty"` // reason actual bytes were not read
}

// IFD is one parsed image file directory and its provenance coordinates.
type IFD struct {
	NodeID     string    `json:"nodeId"`
	Address    int64     `json:"address"`
	Range      ByteRange `json:"range"`
	Tags       []RawTag  `json:"tags"`
	NextOffset int64     `json:"nextOffset"`
	HasNext    bool      `json:"hasNext"`
}

// EdgeKind classifies a traversed IFD link.
type EdgeKind string

const (
	EdgeRoot  EdgeKind = "root"
	EdgeNext  EdgeKind = "next"
	EdgeSub   EdgeKind = "subifd"
)

// Visit records one traversal attempt, including cycles, shared subdirectories,
// out-of-bounds pointers and budget stops.
type Visit struct {
	Step    int      `json:"step"`
	Address int64    `json:"address"`
	Parent  int64    `json:"parent"`
	ViaTag  uint16   `json:"viaTag"`
	Kind    EdgeKind `json:"kind"`
	Outcome string   `json:"outcome"` // parsed|cycle|shared|null|out_of_bounds|budget|malformed
	NodeID  string   `json:"nodeId,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Edge is a directed link between two IFD addresses (either may be unparsed).
type Edge struct {
	From   int64    `json:"from"`
	To     int64    `json:"to"`
	Kind   EdgeKind `json:"kind"`
	ViaTag uint16   `json:"viaTag,omitempty"`
	Status string   `json:"status"` // ok|cycle|shared|out_of_bounds|null|budget
}

// Diagnostic is machine-readable evidence of an anomaly or boundary event.
type Diagnostic struct {
	Code     string    `json:"code"`
	Severity string    `json:"severity"` // error|warning|info
	Message  string    `json:"message"`
	Offset   int64     `json:"offset,omitempty"`
	Range    *ByteRange `json:"range,omitempty"`
}

// Budget caps parser work so cyclic/shared IFD graphs cannot exhaust memory.
type Budget struct {
	MaxIFDs       int   `json:"maxIfds"`
	MaxValueBytes int64 `json:"maxValueBytes"`
	IFDsVisited   int   `json:"ifdsVisited"`
	IFDsAccepted  int   `json:"ifdsAccepted"`
	ValueBytes    int64 `json:"valueBytes"`
	IFDsStopped   bool  `json:"ifdsStopped"`
	ValuesStopped bool  `json:"valuesStopped"`
}

// Result is the full parse outcome.
type Result struct {
	Order           Order        `json:"order"`
	Version         Version      `json:"version"`
	FileSize        int64        `json:"fileSize"`
	HeaderRange     ByteRange    `json:"headerRange"`
	RootIFDOffset   int64        `json:"rootIfdOffset"`
	IFDs            []*IFD       `json:"ifds"`
	Visits          []Visit      `json:"visits"`
	Edges           []Edge       `json:"edges"`
	Diagnostics     []Diagnostic `json:"diagnostics"`
	Budget          Budget       `json:"budget"`
	ByteOrderedSwap bool         `json:"byteOrderSwapped,omitempty"`
}

// HasCode reports whether a diagnostic code is present.
func (r *Result) HasCode(code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// Tag finds the first tag with the given id across all IFDs.
func (r *Result) Tag(id uint16) (*IFD, *RawTag) {
	for _, ifd := range r.IFDs {
		for i := range ifd.Tags {
			if ifd.Tags[i].ID == id {
				return ifd, &ifd.Tags[i]
			}
		}
	}
	return nil, nil
}

func (r *Result) addDiag(code, sev, msg string, off int64) {
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: code, Severity: sev, Message: msg, Offset: off})
}

func (r *Result) addRangeDiag(code, sev, msg string, rng ByteRange) {
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: code, Severity: sev, Message: msg, Offset: rng.Offset, Range: &rng})
}

// JSON is a compact helper used by tests and export tooling.
func (r *Result) JSON() string {
	b, _ := json.Marshal(r)
	return string(b)
}
