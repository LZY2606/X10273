package tiff

import (
	"encoding/binary"
	"fmt"
	"ifdroom/internal/event"
)

// Parse parses raw TIFF/BigTIFF bytes with a hard visit budget. The budget
// bounds the number of distinct IFD offsets the parser may visit; when it is
// exhausted, pending offsets are retained as undiscovered evidence instead of
// being silently dropped.
func Parse(data []byte, budget int, maxExternalBytes int64) *Report {
	rep := &Report{FileSize: int64(len(data))}
	if budget <= 0 {
		budget = 1
	}
	if len(data) < 4 {
		rep.Diagnostics = append(rep.Diagnostics, &Diagnostic{
			Code: "header_truncated", Severity: SevFatal,
			Message: fmt.Sprintf("file shorter than 4 bytes (%d)", len(data)),
		})
		return rep
	}
	var bo binary.ByteOrder
	switch string(data[0:2]) {
	case "II":
		bo = binary.LittleEndian
		rep.Header = &Header{Endian: LittleEndian}
	case "MM":
		bo = binary.BigEndian
		rep.Header = &Header{Endian: BigEndian}
	default:
		rep.Diagnostics = append(rep.Diagnostics, &Diagnostic{
			Code: "bad_byte_order", Severity: SevFatal,
			Message: fmt.Sprintf("byte order marker %q is neither II nor MM", string(data[0:2])),
			Range:   event.ByteRange{Lo: 0, Hi: 2},
		})
		return rep
	}
	magic := bo.Uint16(data[2:4])
	hdr := rep.Header
	hdr.Magic = magic
	switch magic {
	case 42:
		hdr.Version = Classic
		hdr.IFDOffsetSize = 4
		hdr.HeaderRange = event.ByteRange{Lo: 0, Hi: 8}
	case 43:
		hdr.Version = BigTIFF
		hdr.IFDOffsetSize = 8
		hdr.HeaderRange = event.ByteRange{Lo: 0, Hi: 16}
	default:
		rep.Diagnostics = append(rep.Diagnostics, &Diagnostic{
			Code: "bad_magic", Severity: SevFatal,
			Message: fmt.Sprintf("magic %d is neither 42 (classic) nor 43 (BigTIFF)", magic),
			Range:   event.ByteRange{Lo: 2, Hi: 4},
		})
		return rep
	}
	if int64(len(data)) < hdr.HeaderRange.Hi {
		rep.dg("header_truncated", SevFatal, fmt.Sprintf("header needs %d bytes, have %d", hdr.HeaderRange.Hi, len(data)), event.ByteRange{Lo: 0, Hi: int64(len(data))}, "")
		return rep
	}
	if hdr.Version == Classic {
		hdr.FirstIFD = int64(bo.Uint32(data[4:8]))
	} else {
		// Bytes 4-5: offsetsize (must be 8), 6-7: constant 0, 8-15: first IFD.
		offSize := bo.Uint16(data[4:6])
		zero := bo.Uint16(data[6:8])
		if offSize != 8 {
			rep.dg("bigtiff_bad_offset_size", SevFatal, fmt.Sprintf("BigTIFF offset size %d is not 8", offSize), event.ByteRange{Lo: 4, Hi: 6}, "")
			return rep
		}
		if zero != 0 {
			rep.dg("bigtiff_bad_constant", SevError, fmt.Sprintf("BigTIFF constant field is %d, expected 0", zero), event.ByteRange{Lo: 6, Hi: 8}, "")
		}
		hdr.FirstIFD = int64(bo.Uint64(data[8:16]))
	}
	if hdr.FirstIFD == 0 {
		rep.dg("no_ifd", SevError, "first IFD offset is 0", event.ByteRange{Lo: int64(hdr.HeaderRange.Hi) - int64(hdr.IFDOffsetSize), Hi: int64(hdr.HeaderRange.Hi)}, "")
		return rep
	}

	p := &parser{
		data: data, bo: bo, rep: rep, budget: budget,
		maxExt: maxExternalBytes,
		seen:   map[int64]*IFD{},
		// Each offset carries the provenance of the pointer that found it.
		backEdge: map[int64]Edge{},
		backKnown: map[int64]bool{},
	}
	p.walk(hdr.FirstIFD, "", "", -1)
	rep.BudgetUsed = p.used
	rep.BudgetExhausted = p.exhausted
	rep.Undiscovered = append(rep.Undiscovered, p.queue...)
	p.detectSharedAndCycles()
	return rep
}

type pendingPtr struct {
	offset int64
	edge   Edge
}

type parser struct {
	data      []byte
	bo        binary.ByteOrder
	rep       *Report
	budget    int
	maxExt    int64
	used      int
	exhausted bool
	seen      map[int64]*IFD
	order     []int64
	queue     []int64
	queuedSet map[int64]bool
	// incoming pointer evidence recorded at discovery time
	backEdge  map[int64]Edge
	backKnown map[int64]bool
}

func (p *parser) walk(first int64, from, toPlaceholder string, _ int) {
	p.queuedSet = map[int64]bool{}
	p.enqueue(first, Edge{From: "", To: ifdID(first), Kind: EdgeNext})
	for len(p.queue) > 0 {
		off := p.queue[0]
		p.queue = p.queue[1:]
		delete(p.queuedSet, off)
		if _, ok := p.seen[off]; ok {
			continue
		}
		if p.used >= p.budget {
			p.exhausted = true
			p.rep.dg("visit_budget_exhausted", SevWarning,
				fmt.Sprintf("visit budget %d reached; %d IFD offset(s) left unvisited", p.budget, len(p.queue)+1),
				event.ByteRange{}, "")
			p.queue = append(p.queue, off)
			return
		}
		p.used++
		ifd := p.readIFD(off)
		if ifd == nil {
			continue
		}
		p.seen[off] = ifd
		p.order = append(p.order, off)
		p.rep.IFDs = append(p.rep.IFDs, ifd)
		if e, ok := p.backEdge[off]; ok && e.From != "" {
			p.rep.Edges = append(p.rep.Edges, e)
		}
		// Next IFD pointer (self pointers are retained as cycle evidence).
		if ifd.NextPresent {
			p.enqueue(ifd.NextOffset, Edge{From: ifd.ID, To: ifdID(ifd.NextOffset), Kind: EdgeNext})
		}
		// SubIFD pointers.
		for _, tag := range ifd.Tags {
			if tag.ID != TagSubIFDs {
				continue
			}
			for i, v := range tag.Values {
				sub, ok := ptrValue(v)
				if !ok {
					continue
				}
				p.enqueue(sub, Edge{From: ifd.ID, To: ifdID(sub), Kind: EdgeSubIFD, OffsetTag: TagSubIFDs, Index: i})
			}
		}
	}
}

func (p *parser) enqueue(off int64, e Edge) {
	if off == 0 {
		return
	}
	if _, ok := p.seen[off]; ok {
		// Pointer to an already-visited directory: cycle or shared evidence.
		if e.From != "" {
			p.rep.Edges = append(p.rep.Edges, e)
		}
		return
	}
	if !p.backKnown[off] {
		p.backKnown[off] = true
		p.backEdge[off] = e
	} else if e.From != "" {
		// Second inbound pointer: shared subdirectory candidate.
		p.rep.Edges = append(p.rep.Edges, e)
	}
	if !p.queuedSet[off] {
		p.queuedSet[off] = true
		p.queue = append(p.queue, off)
	}
}

func ifdID(off int64) string {
	return fmt.Sprintf("ifd@%d", off)
}

func (r *Report) dg(code string, sev Severity, msg string, br event.ByteRange, ifd string) {
	r.Diagnostics = append(r.Diagnostics, &Diagnostic{Code: code, Severity: sev, Message: msg, Range: br, IFDID: ifd})
}
