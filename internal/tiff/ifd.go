package tiff

import (
	"encoding/binary"
	"fmt"
	"ifdroom/internal/event"
	"math"
)

func (p *parser) readIFD(off int64) *IFD {
	data, bo, rep := p.data, p.bo, p.rep
	big := rep.Header.Version == BigTIFF
	entrySize := 12
	countSize := int64(2)
	if big {
		entrySize = 20
		countSize = 8
	}
	if off < 0 || off >= int64(len(data)) {
		rep.dg("ifd_offset_oob", SevError, fmt.Sprintf("IFD offset %d outside file of %d bytes", off, len(data)), event.ByteRange{Lo: off, Hi: off}, "")
		return nil
	}
	if off+countSize > int64(len(data)) {
		rep.dg("ifd_count_truncated", SevError, fmt.Sprintf("IFD at %d: entry count truncated", off), event.ByteRange{Lo: off, Hi: int64(len(data))}, ifdID(off))
		return nil
	}
	var n uint64
	if big {
		n = bo.Uint64(data[off : off+8])
	} else {
		n = uint64(bo.Uint16(data[off : off+2]))
	}
	start := off + countSize
	end := start + int64(n)*int64(entrySize)
	if end > int64(len(data)) {
		rep.dg("ifd_entries_truncated", SevError,
			fmt.Sprintf("IFD at %d: %d entries require %d bytes beyond end of file", off, n, end-int64(len(data))),
			event.ByteRange{Lo: start, Hi: int64(len(data))}, ifdID(off))
		return nil
	}
	ifd := &IFD{
		ID:         ifdID(off),
		Offset:     off,
		Index:      len(rep.IFDs),
		EntryCount: int(n),
		EntryRange: event.ByteRange{Lo: start, Hi: end},
	}
	for i := int64(0); i < int64(n); i++ {
		ep := start + i*int64(entrySize)
		tag := p.readTag(ep, big, ifd.ID)
		if tag != nil {
			ifd.Tags = append(ifd.Tags, tag)
		}
	}
	nextPos := end
	if nextPos+int64(rep.Header.IFDOffsetSize) > int64(len(data)) {
		rep.dg("next_ifd_truncated", SevWarning,
			fmt.Sprintf("IFD at %d: next-IFD pointer truncated", off),
			event.ByteRange{Lo: nextPos, Hi: int64(len(data))}, ifd.ID)
		return ifd
	}
	if big {
		ifd.NextOffset = int64(bo.Uint64(data[nextPos : nextPos+8]))
	} else {
		ifd.NextOffset = int64(bo.Uint32(data[nextPos : nextPos+4]))
	}
	ifd.NextPresent = ifd.NextOffset != 0
	return ifd
}

func (p *parser) readTag(ep int64, big bool, ifdID string) *Tag {
	data, bo, rep := p.data, p.bo, p.rep
	fieldSize := int64(4)
	if big {
		fieldSize = 8
	}
	tid := bo.Uint16(data[ep : ep+2])
	tcode := bo.Uint16(data[ep+2 : ep+4])
	var count uint64
	if big {
		count = bo.Uint64(data[ep+4 : ep+12])
	} else {
		count = uint64(bo.Uint32(data[ep+4 : ep+8]))
	}
	vp := ep + 4 + countSizeField(big)
	t := &Tag{
		ID:         tid,
		Name:       TagName(tid),
		TypeCode:   tcode,
		TypeName:   typeNames[tcode],
		Count:      count,
		EntryRange: event.ByteRange{Lo: ep, Hi: vp + fieldSize},
	}
	if t.TypeName == "" {
		t.TypeName = fmt.Sprintf("UNKNOWN(%d)", tcode)
		rep.dg("unknown_tag_type", SevWarning, fmt.Sprintf("tag %d (%s) has unknown type %d", tid, TagName(tid), tcode), t.EntryRange, ifdID)
	}
	tsize, known := typeSizes[tcode]
	if !known {
		// Preserve the raw value field verbatim; do not fabricate values.
		t.ValueField = append([]byte(nil), data[vp:vp+fieldSize]...)
		rep.dg("undecodable_tag_value", SevWarning, fmt.Sprintf("tag %d uses unsupported type %d; raw field retained", tid, tcode), t.EntryRange, ifdID)
		return t
	}
	total := int64(tsize) * int64(count)
	if total <= fieldSize {
		t.RawValue = append([]byte(nil), data[vp:vp+total]...)
	} else {
		var extOff int64
		if big {
			extOff = int64(bo.Uint64(data[vp : vp+8]))
		} else {
			extOff = int64(bo.Uint32(data[vp : vp+4]))
		}
		t.ValueField = append([]byte(nil), data[vp:vp+fieldSize]...)
		t.External = true
		t.ExternalRange = event.ByteRange{Lo: extOff, Hi: extOff + total}
		if extOff < 0 || extOff+total > int64(len(data)) {
			avail := int64(len(data)) - extOff
			if avail < 0 {
				avail = 0
			}
			rep.dg("external_value_oob", SevError,
				fmt.Sprintf("tag %d (%s) external value at %d needs %d bytes, %d available", tid, TagName(tid), extOff, total, avail),
				event.ByteRange{Lo: extOff, Hi: extOff + total}, ifdID)
			if extOff >= 0 && extOff < int64(len(data)) {
				t.RawValue = append([]byte(nil), data[extOff:min64(extOff+total, int64(len(data)))]...)
			}
			return t
		}
		if p.maxExt > 0 {
			p.maxExt -= total
			if p.maxExt < 0 {
				rep.dg("external_byte_budget_exhausted", SevWarning,
					fmt.Sprintf("external value byte budget exhausted while reading tag %d", tid),
					t.ExternalRange, ifdID)
				return t
			}
		}
		t.RawValue = append([]byte(nil), data[extOff:extOff+total]...)
	}
	t.Values = decodeValues(t, bo)
	return t
}

func countSizeField(big bool) int64 {
	if big {
		return 8
	}
	return 4
}

func decodeValues(t *Tag, bo binary.ByteOrder) []interface{} {
	tsize := typeSizes[t.TypeCode]
	if tsize == 0 || len(t.RawValue) == 0 {
		return nil
	}
	raw := t.RawValue
	n := int(t.Count)
	if len(raw) < n*tsize {
		n = len(raw) / tsize
	}
	out := make([]interface{}, 0, n)
	off := 0
	for i := 0; i < n; i++ {
		switch t.TypeCode {
		case 1, 6, 7:
			out = append(out, raw[off])
		case 2:
			out = append(out, raw[off])
		case 3:
			out = append(out, bo.Uint16(raw[off:]))
		case 4, 13:
			out = append(out, uint64(bo.Uint32(raw[off:])))
		case 16, 18:
			out = append(out, bo.Uint64(raw[off:]))
		case 8:
			out = append(out, int16(bo.Uint16(raw[off:])))
		case 9:
			out = append(out, int32(bo.Uint32(raw[off:])))
		case 17:
			out = append(out, int64(bo.Uint64(raw[off:])))
		case 5:
			num := bo.Uint32(raw[off:])
			den := bo.Uint32(raw[off+4:])
			if den == 0 {
				out = append(out, nil)
			} else {
				out = append(out, float64(num)/float64(den))
			}
		case 10:
			num := int32(bo.Uint32(raw[off:]))
			den := int32(bo.Uint32(raw[off+4:]))
			if den == 0 {
				out = append(out, nil)
			} else {
				out = append(out, float64(num)/float64(den))
			}
		case 11:
			out = append(out, math.Float32frombits(bo.Uint32(raw[off:])))
		case 12:
			out = append(out, math.Float64frombits(bo.Uint64(raw[off:])))
		}
		off += tsize
	}
	return out
}

func ptrValue(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case uint64:
		return int64(x), true
	case int64:
		return x, true
	}
	return 0, false
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// detectSharedAndCycles derives cycle paths and shared-subdirectory evidence
// from the retained edge list; both are structural, never repaired away.
func (p *parser) detectSharedAndCycles() {
	rep := p.rep
	inbound := map[string][]Edge{}
	nodes := map[string]bool{}
	for _, ifd := range rep.IFDs {
		nodes[ifd.ID] = true
	}
	var edges []Edge
	edges = append(edges, rep.Edges...)
	for _, e := range edges {
		inbound[e.To] = append(inbound[e.To], e)
	}
	for to, ins := range inbound {
		if len(ins) > 1 && nodes[to] {
			rep.SharedSubIFDs = append(rep.SharedSubIFDs, to)
		}
	}
	// DFS cycle detection over the retained directed graph.
	color := map[string]int{} // 0 white, 1 gray, 2 black
	var stack []string
	var dfs func(n string)
	adj := map[string][]Edge{}
	for _, e := range edges {
		adj[e.From] = append(adj[e.From], e)
	}
	dfs = func(n string) {
		color[n] = 1
		stack = append(stack, n)
		for _, e := range adj[n] {
			if !nodes[e.To] {
				continue
			}
			switch color[e.To] {
			case 0:
				dfs(e.To)
			case 1:
				var cyc []string
				for i := len(stack) - 1; i >= 0; i-- {
					cyc = append([]string{stack[i]}, cyc...)
					if stack[i] == e.To {
						break
					}
				}
				cyc = append(cyc, e.To)
				rep.Cycles = append(rep.Cycles, cyc)
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = 2
	}
	for _, ifd := range rep.IFDs {
		if color[ifd.ID] == 0 {
			dfs(ifd.ID)
		}
	}
}
