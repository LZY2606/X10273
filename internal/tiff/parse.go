// Package tiff builds and parses TIFF and BigTIFF byte streams at the IFD
// level. The parser preserves the raw on-disk representation (byte order,
// tag codes, value slots and external offsets), models the IFD graph
// including cycles and shared subdirectories, and bounds all work with an
// explicit access budget.
package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
)

type Severity string

const (
	SevInfo Severity = "info"
	SevWarn Severity = "warning"
	SevErr  Severity = "error"
)

// Diagnostic records a bounded, machine-readable parse observation.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	AtOffset int64    `json:"atOffset"`
	IFD      int64    `json:"ifdOffset"`
	Tag      uint16   `json:"tag,omitempty"`
}

// Entry is one IFD directory entry with its raw representation retained.
type Entry struct {
	Tag         uint16    `json:"tag"`
	Type        DataType  `json:"type"`
	TypeValid   bool      `json:"typeValid"`
	Count       uint64    `json:"count"`
	SlotOffset  int64     `json:"slotOffset"`
	SlotEnd     int64     `json:"slotEnd"`
	InlineRaw   []byte    `json:"inlineRaw"`
	External    bool      `json:"external"`
	ExtOffset   int64     `json:"extOffset"`
	ValueRaw    []byte    `json:"valueRaw"`
	OutOfBounds bool      `json:"outOfBounds"`
	Uints       []uint64  `json:"uints,omitempty"`
	Sints       []int64   `json:"sints,omitempty"`
	Doubles     []float64 `json:"doubles,omitempty"`
	Ascii       string    `json:"ascii,omitempty"`
	RawAscii    string    `json:"rawAscii,omitempty"`
	IFDLinks    []int64   `json:"ifdLinks,omitempty"`
}

// Edge is a structural link between IFDs (next IFD or a SubIFD slot).
type Edge struct {
	From    int64  `json:"from"`
	To      int64  `json:"to"`
	Kind    string `json:"kind"`
	Index   int    `json:"index"`
	Cycle   bool   `json:"cycle"`
	Shared  bool   `json:"shared"`
	Reached bool   `json:"reached"`
}

type IFD struct {
	Offset     int64             `json:"offset"`
	Order      int               `json:"order"`
	Entries    []*Entry          `json:"entries"`
	ByteStart  int64             `json:"byteStart"`
	ByteEnd    int64             `json:"byteEnd"`
	NextOffset int64             `json:"nextOffset"`
	HasNext    bool              `json:"hasNext"`
	SubOffsets []int64           `json:"subOffsets"`
	ByTag      map[uint16]*Entry `json:"-"`
}

type File struct {
	Order       Order          `json:"order"`
	BigTIFF     bool           `json:"bigTIFF"`
	Magic       uint16         `json:"magic"`
	OffBytes    int            `json:"offsetBytes"`
	RootOffset  int64          `json:"rootOffset"`
	HeaderEnd   int64          `json:"headerEnd"`
	Size        int64          `json:"size"`
	IFDs        []*IFD         `json:"ifds"`
	ByOffset    map[int64]*IFD `json:"-"`
	Edges       []*Edge        `json:"edges"`
	Diagnostics []Diagnostic   `json:"diagnostics"`
	Budget      BudgetTrace    `json:"budget"`
}

type BudgetTrace struct {
	Limit      int  `json:"limit"`
	IFDVisits  int  `json:"ifdVisits"`
	ValueReads int  `json:"valueReads"`
	Exhausted  bool `json:"exhausted"`
	Unreached  int  `json:"unreachedIFDs"`
}

// Options controls parsing. BudgetIfdVisit bounds IFD traversal steps;
// BudgetValueRead bounds external value-slot fetches.
type Options struct {
	BudgetIfdVisit  int
	BudgetValueRead int
}

func DefaultOptions() Options {
	return Options{BudgetIfdVisit: 256, BudgetValueRead: 4096}
}

type reader struct {
	b          []byte
	big        bool
	offBytes   int
	bo         binary.ByteOrder
	opts       Options
	f          *File
	ifdVisits  int
	valueReads int
	unreached  int
}

func (r *reader) diag(d Diagnostic) { r.f.Diagnostics = append(r.f.Diagnostics, d) }

// Parse parses a complete TIFF byte slice. A malformed header yields an
// error; later malformations are recorded as diagnostics while as much of
// the directory graph as is safely reachable is preserved.
func Parse(data []byte, opts Options) (*File, error) {
	if opts.BudgetIfdVisit <= 0 {
		opts.BudgetIfdVisit = DefaultOptions().BudgetIfdVisit
	}
	if opts.BudgetValueRead <= 0 {
		opts.BudgetValueRead = DefaultOptions().BudgetValueRead
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("tiff: %d bytes shorter than 8 byte header", len(data))
	}
	var order Order
	switch string(data[0:2]) {
	case "II":
		order = LittleEndian
	case "MM":
		order = BigEndian
	default:
		return nil, fmt.Errorf("tiff: invalid byte order marker %q", data[0:2])
	}
	bo := order.ByteOrder()
	magic := bo.Uint16(data[2:4])
	var root int64
	var headerEnd int64
	big := false
	offBytes := 4
	switch magic {
	case 42:
		root = int64(bo.Uint32(data[4:8]))
		headerEnd = 8
	case 43:
		big = true
		if len(data) < 16 {
			return nil, fmt.Errorf("tiff: BigTIFF header truncated at %d bytes", len(data))
		}
		if bo.Uint16(data[4:6]) != 8 {
			return nil, fmt.Errorf("tiff: BigTIFF offset size %d, want 8", bo.Uint16(data[4:6]))
		}
		if bo.Uint16(data[6:8]) != 0 {
			return nil, fmt.Errorf("tiff: BigTIFF constant field %d, want 0", bo.Uint16(data[6:8]))
		}
		root = int64(bo.Uint64(data[8:16]))
		headerEnd = 16
		offBytes = 8
	default:
		return nil, fmt.Errorf("tiff: invalid version magic %d for byte order %s", magic, order)
	}
	f := &File{Order: order, BigTIFF: big, Magic: magic, OffBytes: offBytes,
		RootOffset: root, HeaderEnd: headerEnd, Size: int64(len(data)),
		ByOffset: map[int64]*IFD{}}
	r := &reader{b: data, big: big, offBytes: offBytes, bo: bo, opts: opts, f: f}
	r.traverse(root)
	f.Budget = BudgetTrace{Limit: opts.BudgetIfdVisit, IFDVisits: r.ifdVisits,
		ValueReads: r.valueReads, Exhausted: r.ifdVisits >= opts.BudgetIfdVisit,
		Unreached: r.unreached}
	return f, nil
}

type pendingEdge struct {
	from  int64
	to    int64
	kind  string
	index int
}

func (r *reader) traverse(root int64) {
	// Iterative DFS over next/subifd links. Gray nodes are on the stack
	// (back-edge = cycle), black nodes were fully expanded (cross-edge =
	// shared subdirectory).
	const white, gray, black = 0, 1, 2
	color := map[int64]int{}
	var stack []pendingEdge
	stack = append(stack, pendingEdge{to: root})
	addEdge := func(e *Edge) { r.f.Edges = append(r.f.Edges, e) }
	for len(stack) > 0 {
		pe := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		to := pe.to
		if to <= 0 {
			continue
		}
		var incoming *Edge
		if pe.from != 0 {
			incoming = &Edge{From: pe.from, To: to, Kind: pe.kind, Index: pe.index}
		}
		switch color[to] {
		case gray:
			if incoming != nil {
				incoming.Cycle = true
				incoming.Reached = true
				addEdge(incoming)
			}
			continue
		case black:
			if incoming != nil {
				incoming.Shared = true
				incoming.Reached = true
				addEdge(incoming)
			}
			continue
		}
		if r.ifdVisits >= r.opts.BudgetIfdVisit {
			r.unreached++
			if incoming != nil {
				incoming.Reached = false
				addEdge(incoming)
			}
			r.diag(Diagnostic{Code: "budget_exhausted", Severity: SevErr,
				Message:  fmt.Sprintf("access budget %d exhausted before IFD@%d", r.opts.BudgetIfdVisit, to),
				AtOffset: to})
			continue
		}
		if incoming != nil {
			incoming.Reached = true
			addEdge(incoming)
		}
		if _, seen := r.f.ByOffset[to]; seen {
			color[to] = black
			continue
		}
		color[to] = gray
		r.ifdVisits++
		ifd, readable := r.parseIFD(to)
		if ifd == nil {
			ifd = &IFD{Offset: to, ByTag: map[uint16]*Entry{}}
		}
		ifd.Order = len(r.f.IFDs)
		r.f.ByOffset[to] = ifd
		r.f.IFDs = append(r.f.IFDs, ifd)
		if readable {
			for i := len(ifd.SubOffsets) - 1; i >= 0; i-- {
				stack = append(stack, pendingEdge{from: to, to: ifd.SubOffsets[i], kind: "subifd", index: i})
			}
			if ifd.HasNext {
				if ifd.NextOffset == to {
					addEdge(&Edge{From: to, To: to, Kind: "next", Cycle: true, Reached: true})
				} else {
					stack = append(stack, pendingEdge{from: to, to: ifd.NextOffset, kind: "next"})
				}
			}
		}
		color[to] = black
	}
}

var _ = math.MaxInt64
