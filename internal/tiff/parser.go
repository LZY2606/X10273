package tiff

import (
	"encoding/binary"
	"fmt"
	"math"

	"ifdgraph/internal/model"
)

// Params is the parameter fingerprint surface of the parser.
type Params struct {
	StepBudget int `json:"step_budget"`
}

// DefaultParams returns the standard access budget.
func DefaultParams() Params { return Params{StepBudget: 4096} }

// Fingerprint serializes parameters that can change derived results.
func (p Params) Fingerprint() string {
	return fmt.Sprintf("steps=%d", p.StepBudget)
}

const algoVersion = "tiff-parse-1.0.0"

// AlgoVersion returns the algorithm version stamped on every parse result.
func AlgoVersion() string { return algoVersion }

type parser struct {
	data   []byte
	order  binary.ByteOrder
	bo     model.ByteOrder
	big    bool
	params Params

	steps int

	offsets  map[int64]int // offset -> index into g.IFDs
	refCount map[int64]int
}

func edgeOnStack(q []childVisit, offset int64) bool {
	for _, cv := range q {
		if cv.offset == offset {
			return true
		}
	}
	return false
}

type childVisit struct {
	offset, parent int64
	level          int
	parentID       string
	edge           model.IFDEdge
	hasEdge        bool
}

// Parse walks the IFD graph of data without trusting any pointer. Every
// traversal step is charged against the access budget; cycles and shared
// subdirectories remain visible in the output.
func Parse(sourceID string, revision int, data []byte, params Params) *model.ParseGraph {
	g := &model.ParseGraph{
		SourceID:    sourceID,
		Revision:    revision,
		FileSize:    int64(len(data)),
		StepsBudget: params.StepBudget,
	}
	if len(data) < 4 {
		g.Fatal = "file shorter than TIFF byte-order header"
		return g
	}
	switch string(data[0:2]) {
	case "II":
		g.ByteOrder = model.LittleEndian
	case "MM":
		g.ByteOrder = model.BigEndian
	default:
		g.Fatal = fmt.Sprintf("invalid byte-order marker %02x%02x", data[0], data[1])
		return g
	}
	p := &parser{
		data: data, order: orderFor(g.ByteOrder), bo: g.ByteOrder,
		params: params, offsets: map[int64]int{}, refCount: map[int64]int{},
	}
	magic := p.order.Uint16(data[2:4])
	g.Header = model.ByteRange{Offset: 0, Length: 8}
	switch magic {
	case 42:
		g.Version = model.Classic
	case 43:
		g.Version = model.BigTIFF
		g.Header.Length = 16
	default:
		g.Fatal = fmt.Sprintf("unknown TIFF magic %d", magic)
		return g
	}
	if int64(len(data)) < g.Header.Length {
		g.Fatal = "file shorter than declared TIFF header"
		return g
	}
	if g.Version == model.BigTIFF {
		if p.order.Uint16(data[4:6]) != 8 || p.order.Uint16(data[6:8]) != 0 {
			g.Fatal = "invalid BigTIFF constant words"
			return g
		}
		g.IFD0Off = int64(p.order.Uint64(data[8:16]))
	} else {
		g.IFD0Off = int64(p.order.Uint32(data[4:8]))
	}

	queue := []childVisit{{offset: g.IFD0Off, level: 0}}

	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		if v.offset <= 0 {
			continue
		}
		// Edges are recorded even when their target cannot be visited.
		if v.hasEdge {
			p.refCount[v.offset]++
			edge := v.edge
			edge.Offset = v.offset
			if _, seen := p.offsets[v.offset]; seen {
				edge.Resolved = true
			}
			g.Edges = append(g.Edges, edge)
		}
		if idx, seen := p.offsets[v.offset]; seen {
			ifd := g.IFDs[idx]
			// Closing an edge back onto the current traversal path is a
			// cycle. A target already finished earlier in BFS order is a
			// shared subdirectory, not a cycle.
			if v.level > ifd.Level || edgeOnStack(queue, v.offset) {
				ifd.Cycle = true
			}
			continue
		}
		if !p.charge(1) {
			g.BudgetExhausted = true
			diag(g, "", "budget_exhausted",
				fmt.Sprintf("access budget of %d steps exhausted before visiting IFD at offset %d", params.StepBudget, v.offset))
			break
		}
		children, ok := p.parseIFD(g, v.offset, v.level, v.parentID)
		if !ok {
			continue
		}
		queue = append(queue, children...)
	}

	for _, ifd := range g.IFDs {
		if p.refCount[ifd.Offset] > 1 {
			ifd.Shared = true
		}
	}
	g.StepsUsed = p.steps
	buildBlocks(g)
	buildGeo(g)
	return g
}

func orderFor(bo model.ByteOrder) binary.ByteOrder {
	if bo == model.BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func (p *parser) charge(n int) bool {
	p.steps += n
	return p.steps <= p.params.StepBudget
}

func diag(g *model.ParseGraph, obj, code, msg string) {
	g.Diags = append(g.Diags, model.Diagnostic{ObjectID: obj, Code: code, Message: msg})
}

func (p *parser) parseIFD(g *model.ParseGraph, offset int64, level int, parentID string) ([]childVisit, bool) {
	countLen, ptrLen, entryLen := int64(2), int64(4), int64(12)
	if p.big {
		countLen, ptrLen, entryLen = 8, 8, 20
	}
	if offset < 0 || offset+countLen > int64(len(p.data)) {
		diag(g, "", "ifd_oob", fmt.Sprintf("IFD count at offset %d outside file", offset))
		return nil, false
	}
	var n uint64
	if p.big {
		n = p.order.Uint64(p.data[offset : offset+8])
	} else {
		n = uint64(p.order.Uint16(p.data[offset : offset+2]))
	}
	entriesStart := offset + countLen
	end := entriesStart + int64(n)*entryLen + ptrLen
	if end > int64(len(p.data)) {
		diag(g, "", "ifd_oob", fmt.Sprintf("IFD at offset %d claims %d entries beyond EOF", offset, n))
		return nil, false
	}
	index := len(g.IFDs)
	ifd := &model.IFD{
		ID:        model.IFDNodeID(g.SourceID, index, offset),
		SourceID:  g.SourceID,
		Index:     index,
		Offset:    offset,
		Range:     model.ByteRange{Offset: offset, Length: end - offset},
		Level:     level,
		ParentID:  parentID,
		Reachable: true,
	}
	g.IFDs = append(g.IFDs, ifd)
	p.offsets[offset] = index

	type subPtr struct {
		offset  int64
		kind    model.EdgeKind
		tag     uint16
		ordinal int
	}
	var ptrs []subPtr

	for i := uint64(0); i < n; i++ {
		if !p.charge(1) {
			g.BudgetExhausted = true
			diag(g, ifd.ID, "budget_exhausted", "budget exhausted while reading IFD entries")
			break
		}
		entryOff := entriesStart + int64(i)*entryLen
		tag := p.order.Uint16(p.data[entryOff : entryOff+2])
		typeCode := p.order.Uint16(p.data[entryOff+2 : entryOff+4])
		var count uint64
		var valueOff int64
		if p.big {
			count = p.order.Uint64(p.data[entryOff+4 : entryOff+12])
			valueOff = int64(p.order.Uint64(p.data[entryOff+12 : entryOff+20]))
		} else {
			count = uint64(p.order.Uint32(p.data[entryOff+4 : entryOff+8]))
			valueOff = int64(p.order.Uint32(p.data[entryOff+8 : entryOff+12]))
		}
		rt := p.decodeTag(g, ifd, tag, typeCode, count, valueOff, entryOff, entryLen, int(i))
		ifd.TagIDs = append(ifd.TagIDs, rt.ID)
		switch tag {
		case TagSubIFDs:
			for k, v := range rt.Numbers {
				ptrs = append(ptrs, subPtr{int64(v), model.EdgeSubIFD, tag, k})
			}
		case TagExifIFD:
			for _, v := range rt.Numbers {
				ptrs = append(ptrs, subPtr{int64(v), model.EdgeExifIFD, tag, 0})
			}
		case TagGPSIFD:
			for _, v := range rt.Numbers {
				ptrs = append(ptrs, subPtr{int64(v), model.EdgeGPSIFD, tag, 0})
			}
		}
	}

	nextOff := int64(0)
	nextPtrOff := entriesStart + int64(n)*entryLen
	if nextPtrOff+ptrLen <= int64(len(p.data)) {
		if p.big {
			nextOff = int64(p.order.Uint64(p.data[nextPtrOff : nextPtrOff+8]))
		} else {
			nextOff = int64(p.order.Uint32(p.data[nextPtrOff : nextPtrOff+4]))
		}
	}

	var visits []childVisit
	for _, sp := range ptrs {
		visits = append(visits, childVisit{
			offset: sp.offset, parent: offset, level: level + 1, parentID: ifd.ID,
			edge:    model.IFDEdge{From: ifd.ID, Kind: sp.kind, Tag: sp.tag, Index: sp.ordinal},
			hasEdge: true,
		})
	}
	if nextOff != 0 {
		visits = append(visits, childVisit{
			offset: nextOff, parent: offset, level: level, parentID: ifd.ID,
			edge:    model.IFDEdge{From: ifd.ID, Kind: model.EdgeNextIFD},
			hasEdge: true,
		})
	}
	return visits, true
}

var _ = math.Float64frombits
