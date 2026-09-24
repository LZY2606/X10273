package tiff

import (
	"fmt"
	"strconv"
)

func (p *parser) readIFD(node *IFDNode, depth int, ancestors map[string]bool) {
	r := p.r
	big := p.g.Variant == BigTIFF
	var entrySize int64 = 12
	if big {
		entrySize = 20
	}

	cntOff := node.Offset
	var count uint64
	if big {
		c64, ok := r.u64(cntOff)
		if !ok {
			p.truncated(node, "entry count", cntOff)
			return
		}
		count = c64
	} else {
		c16, ok := r.u16(cntOff)
		if !ok {
			p.truncated(node, "entry count", cntOff)
			return
		}
		count = uint64(c16)
	}
	node.EntryCount = count

	countFieldLen := int64(2)
	if big {
		countFieldLen = 8
	}
	entriesStart := cntOff + countFieldLen
	endOfEntries := entriesStart + int64(count)*entrySize
	if endOfEntries > p.g.FileSize {
		p.truncated(node, fmt.Sprintf("%d entries", count), entriesStart)
		avail := p.g.FileSize - entriesStart
		if entriesStart < 0 || avail < 0 {
			count = 0
		} else {
			count = uint64(avail / entrySize)
		}
		endOfEntries = entriesStart + int64(count)*entrySize
	}

	type subTarget struct {
		tag uint16
		off int64
	}
	var subs []subTarget

	for i := uint64(0); i < count; i++ {
		eoff := entriesStart + int64(i)*entrySize
		tag, _ := r.u16(eoff)
		var typ uint16
		var n uint64
		var valueOff int64
		if big {
			typ, _ = r.u16(eoff + 2)
			n, _ = r.u64(eoff + 4)
			v, _ := r.u64(eoff + 12)
			valueOff = int64(v)
		} else {
			typ, _ = r.u16(eoff + 2)
			c32, _ := r.u32(eoff + 4)
			n = uint64(c32)
			v, _ := r.u32(eoff + 8)
			valueOff = int64(v)
		}

		slotSize := int64(4)
		if big {
			slotSize = 8
		}
		slot, _ := r.slice(eoff+entrySize-slotSize, slotSize)

		te := &TagEntry{
			ID:       fmt.Sprintf("%s.tag%04x#%d", node.ID, tag, i),
			Tag:      tag,
			Name:     tagName(tag),
			EntryOff: eoff,
			ValueOff: valueOff,
			ValueLen: typeSize(typ) * int64(n),
		}
		te.Raw = p.decodeValue(te, typ, n, valueOff, slot, big, node)
		node.Tags = append(node.Tags, te)

		if pointerTags[tag] {
			te.Kind = KindIFDPointer
			for _, off := range pointerOffsets(&te.Raw, big) {
				te.PointsTo = nodeID("", off)
				subs = append(subs, subTarget{tag: tag, off: off})
			}
		} else if te.Raw.External != nil {
			te.Kind = KindExternal
		} else {
			te.Kind = KindInline
		}
	}

	// Next-IFD chain link (0 means end of chain).
	var hasNext bool
	var nextOff int64
	if big {
		v, ok := r.u64(endOfEntries)
		hasNext = ok
		nextOff = int64(v)
	} else {
		v, ok := r.u32(endOfEntries)
		hasNext = ok
		nextOff = int64(v)
	}
	if !hasNext && endOfEntries <= p.g.FileSize {
		p.truncated(node, "next IFD pointer", endOfEntries)
	}
	if hasNext && nextOff != 0 {
		node.Next = nodeID("", nextOff)
		p.walk(nextOff, node.ID, "next", depth+1, ancestors)
	}

	// Chain first, then sub-IFDs in tag/array order for stable node ordering.
	for _, st := range subs {
		p.walk(st.off, node.ID, "subifd:"+strconv.Itoa(int(st.tag)), depth+1, ancestors)
	}
}

func (p *parser) truncated(node *IFDNode, what string, off int64) {
	p.g.Findings = append(p.g.Findings, Finding{
		Code: FindingTruncatedIFD, Severity: SeverityError, IFDID: node.ID, Offset: off,
		Message: fmt.Sprintf("truncated %s at offset %d", what, off),
	})
}

func typeSize(typ uint16) int64 {
	if ti, ok := fieldTypes[typ]; ok {
		return ti.size
	}
	return 0
}

// pointerOffsets extracts integer offsets carried by an IFD-pointer tag.
func pointerOffsets(raw *RawValue, big bool) []int64 {
	var out []int64
	for _, s := range raw.Values {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, int64(v))
	}
	return out
}
