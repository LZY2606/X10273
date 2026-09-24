package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Parse reads a TIFF/BigTIFF byte stream into a File graph. It never
// loops on cyclic IFD chains: every offset is visited at most once and
// the whole walk is bounded by the given budget.
func Parse(data []byte, budget Budget) *File {
	f := &File{Size: uint64(len(data))}
	var diags []Diagnostic
	diag := func(sev, code, subject, msg string, off uint64, evidence []byte) {
		diags = append(diags, Diagnostic{
			ID:        fmt.Sprintf("diag-%03d", len(diags)+1),
			Severity:  sev,
			Code:      code,
			Message:   msg,
			SubjectID: subject,
			Offset:    off,
			Evidence:  evidence,
		})
	}

	if len(data) < 8 {
		diag("error", DiagBadHeader, "file", "file shorter than 8-byte TIFF header", 0, data)
		f.Diagnostics = diags
		return f
	}

	var order binary.ByteOrder
	switch string(data[0:2]) {
	case "II":
		f.Endianness = LittleEndian
		order = binary.LittleEndian
	case "MM":
		f.Endianness = BigEndian
		order = binary.BigEndian
	default:
		diag("error", DiagBadHeader, "file", "bad byte-order marker", 0, data[0:2])
		f.Diagnostics = diags
		return f
	}

	p := &parser{data: data, order: order, f: f, budget: budget, diag: diag}
	magic := order.Uint16(data[2:4])
	switch magic {
	case 42:
		f.Variant = Classic
		f.FirstIFD = uint64(order.Uint32(data[4:8]))
	case 43:
		f.Variant = BigTIFF
		if len(data) < 16 {
			diag("error", DiagBadHeader, "file", "BigTIFF header truncated", 0, data)
			f.Diagnostics = diags
			return f
		}
		offSize := order.Uint16(data[4:6])
		if offSize != 8 {
			diag("error", DiagBadHeader, "file", fmt.Sprintf("BigTIFF offset size %d, want 8", offSize), 4, data[4:6])
		}
		f.FirstIFD = order.Uint64(data[8:16])
	default:
		diag("error", DiagBadHeader, "file", fmt.Sprintf("unknown magic %d", magic), 2, data[2:4])
		f.Diagnostics = diags
		return f
	}

	p.walk()
	f.Budget = BudgetReport{
		Limit:     budget,
		IFDsSeen:  p.ifdsSeen,
		TagsSeen:  p.tagsSeen,
		Exhausted: p.exhausted,
	}
	f.Diagnostics = diags
	return f
}

type parser struct {
	data      []byte
	order     binary.ByteOrder
	f         *File
	budget    Budget
	diag      func(sev, code, subject, msg string, off uint64, evidence []byte)
	visited   map[uint64]bool
	parents   map[uint64][]string
	ifdsSeen  int
	tagsSeen  int
	exhausted bool
}

type queueItem struct {
	offset uint64
	depth  int
	parent string
}

func (p *parser) walk() {
	p.visited = map[uint64]bool{}
	p.parents = map[uint64][]string{}
	queue := []queueItem{{offset: p.f.FirstIFD, depth: 0}}
	byOffset := map[uint64]*IFD{}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if p.visited[item.offset] {
			// Shared subdirectory: reached again from another parent.
			p.parents[item.offset] = append(p.parents[item.offset], item.parent)
			existing := byOffset[item.offset]
			existing.ParentIDs = p.parents[item.offset]
			p.diag("warning", DiagSharedSubIFD, existing.ID,
				fmt.Sprintf("IFD at offset %d is shared by parents %v", item.offset, existing.ParentIDs),
				item.offset, nil)
			continue
		}
		if p.ifdsSeen >= p.budget.MaxIFDs {
			p.exhausted = true
			p.diag("error", DiagBudgetExhausted, "file",
				fmt.Sprintf("IFD visit budget %d exhausted with %d directories still queued", p.budget.MaxIFDs, len(queue)+1),
				item.offset, nil)
			return
		}
		p.visited[item.offset] = true
		p.ifdsSeen++

		ifd, next, subRefs, ok := p.readIFD(item.offset)
		if !ok {
			continue
		}
		ifd.VisitIndex = p.ifdsSeen - 1
		ifd.Depth = item.depth
		if item.parent != "" {
			p.parents[item.offset] = append(p.parents[item.offset], item.parent)
			ifd.ParentIDs = p.parents[item.offset]
		}
		p.f.IFDs = append(p.f.IFDs, ifd)
		byOffset[item.offset] = ifd

		if next != 0 {
			if p.visited[next] {
				ifd.NextID = byOffset[next].ID
				ifd.NextCycle = true
				p.diag("warning", DiagIFDCycle, ifd.ID,
					fmt.Sprintf("IFD chain loops back to offset %d (%s)", next, ifd.NextID),
					ifd.Offset, nil)
			} else {
				ifd.NextOffset = next
				ifd.NextID = ifdID(next)
				queue = append(queue, queueItem{offset: next, depth: item.depth})
			}
		}
		for _, ref := range subRefs {
			ifd.SubIFDIDs = append(ifd.SubIFDIDs, ifdID(ref))
			queue = append(queue, queueItem{offset: ref, depth: item.depth + 1, parent: ifd.ID})
		}
	}
}

func ifdID(offset uint64) string { return fmt.Sprintf("ifd@%d", offset) }

func (p *parser) readIFD(offset uint64) (*IFD, uint64, []uint64, bool) {
	big := p.f.Variant == BigTIFF
	var count uint64
	headSize := uint64(2)
	if big {
		headSize = 8
	}
	if offset+headSize > uint64(len(p.data)) {
		p.diag("error", DiagIFDOutOfBounds, ifdID(offset),
			fmt.Sprintf("IFD header at offset %d exceeds file size %d", offset, len(p.data)),
			offset, p.readback(offset, headSize))
		return nil, 0, nil, false
	}
	if big {
		count = p.order.Uint64(p.data[offset : offset+8])
	} else {
		count = uint64(p.order.Uint16(p.data[offset : offset+2]))
	}

	entrySize := uint64(12)
	fieldSize := uint64(4)
	if big {
		entrySize = 20
		fieldSize = 8
	}
	tableStart := offset + headSize
	tableEnd := tableStart + count*entrySize
	if count > uint64(p.budget.MaxTags) || tableEnd+fieldSize > uint64(len(p.data)) {
		avail := uint64(0)
		if tableStart < uint64(len(p.data)) {
			avail = (uint64(len(p.data)) - tableStart) / entrySize
		}
		p.diag("error", DiagTagTruncated, ifdID(offset),
			fmt.Sprintf("IFD declares %d entries but only %d fit before EOF/budget", count, avail),
			offset, p.readback(offset, headSize))
		if avail == 0 {
			return nil, 0, nil, false
		}
		count = avail
		tableEnd = tableStart + count*entrySize
	}

	ifd := &IFD{ID: ifdID(offset), Offset: offset}
	var subRefs []uint64
	for i := uint64(0); i < count; i++ {
		entryOff := tableStart + i*entrySize
		tag := p.readTag(ifd, entryOff, fieldSize)
		p.tagsSeen++
		if tag.Code == TagSubIFDs {
			subRefs = append(subRefs, tag.Values...)
		}
		ifd.Tags = append(ifd.Tags, tag)
	}

	nextOff := tableStart + count*entrySize
	var next uint64
	if nextOff+fieldSize <= uint64(len(p.data)) {
		if big {
			next = p.order.Uint64(p.data[nextOff : nextOff+8])
		} else {
			next = uint64(p.order.Uint32(p.data[nextOff : nextOff+4]))
		}
	}
	return ifd, next, subRefs, true
}

func (p *parser) readTag(ifd *IFD, entryOff, fieldSize uint64) *Tag {
	d := p.data[entryOff:]
	code := p.order.Uint16(d[0:2])
	typ := p.order.Uint16(d[2:4])
	var count uint64
	var rawField []byte
	if p.f.Variant == BigTIFF {
		count = p.order.Uint64(d[4:12])
		rawField = append([]byte(nil), d[12:20]...)
	} else {
		count = uint64(p.order.Uint32(d[4:8]))
		rawField = append([]byte(nil), d[8:12]...)
	}

	tag := &Tag{
		ID:       fmt.Sprintf("%s:tag%d@%d", ifd.ID, code, entryOff),
		Code:     code,
		Name:     TagName(code),
		Type:     typ,
		TypeName: TypeName(typ),
		Count:    count,
		RawField: rawField,
	}

	size, known := typeSizes[typ]
	if !known {
		p.diag("warning", DiagUnknownType, tag.ID,
			fmt.Sprintf("unknown field type %d; value left undecoded", typ), entryOff, rawField)
		return tag
	}
	total := size * count
	if total <= fieldSize {
		tag.ByteRange = Range{Start: entryOff + (fieldSize + 4), End: entryOff + (fieldSize + 4) + total}
		if p.f.Variant == BigTIFF {
			tag.ByteRange = Range{Start: entryOff + 12, End: entryOff + 12 + total}
		}
		tag.RawValue = append([]byte(nil), rawField[:total]...)
	} else {
		tag.External = true
		if p.f.Variant == BigTIFF {
			tag.ValueOffset = p.order.Uint64(rawField)
		} else {
			tag.ValueOffset = uint64(p.order.Uint32(rawField))
		}
		tag.ByteRange = Range{Start: tag.ValueOffset, End: tag.ValueOffset + total}
		if tag.ValueOffset+total > uint64(len(p.data)) {
			tag.OutOfBounds = true
			p.diag("error", DiagValueOutOfBounds, tag.ID,
				fmt.Sprintf("external value range %s exceeds file size %d", tag.ByteRange, len(p.data)),
				tag.ValueOffset, p.readback(tag.ValueOffset, min64(total, 16)))
		}
		tag.RawValue = p.readback(tag.ValueOffset, total)
	}
	p.decode(tag, size)
	return tag
}

// readback copies up to n bytes from the file at off; short reads are
// returned as-is so evidence always reflects what is really there.
func (p *parser) readback(off, n uint64) []byte {
	if off >= uint64(len(p.data)) {
		return nil
	}
	end := off + n
	if end > uint64(len(p.data)) {
		end = uint64(len(p.data))
	}
	return append([]byte(nil), p.data[off:end]...)
}

func (p *parser) decode(tag *Tag, typeSize uint64) {
	raw := tag.RawValue
	n := int(tag.Count)
	if uint64(len(raw)) < typeSize*tag.Count {
		n = int(uint64(len(raw)) / typeSize) // truncated readback
	}
	switch tag.Type {
	case 1, 6, 7: // BYTE, SBYTE, UNDEFINED
		for i := 0; i < n; i++ {
			tag.Values = append(tag.Values, uint64(raw[i]))
		}
	case 2: // ASCII
		b := raw
		if len(b) > 0 && b[len(b)-1] == 0 {
			b = b[:len(b)-1]
		}
		tag.Text = string(b)
	case 3, 8: // SHORT, SSHORT
		for i := 0; i < n; i++ {
			tag.Values = append(tag.Values, uint64(p.order.Uint16(raw[i*2:])))
		}
	case 4, 9: // LONG, SLONG
		for i := 0; i < n; i++ {
			tag.Values = append(tag.Values, uint64(p.order.Uint32(raw[i*4:])))
		}
	case 16, 17, 18: // LONG8, SLONG8, IFD8
		for i := 0; i < n; i++ {
			tag.Values = append(tag.Values, p.order.Uint64(raw[i*8:]))
		}
	case 5, 10: // RATIONAL, SRATIONAL
		for i := 0; i < n; i++ {
			num := p.order.Uint32(raw[i*8:])
			den := p.order.Uint32(raw[i*8+4:])
			if den == 0 {
				tag.Floats = append(tag.Floats, math.NaN())
			} else {
				tag.Floats = append(tag.Floats, float64(num)/float64(den))
			}
		}
	case 11: // FLOAT
		for i := 0; i < n; i++ {
			tag.Floats = append(tag.Floats, float64(math.Float32frombits(p.order.Uint32(raw[i*4:]))))
		}
	case 12: // DOUBLE
		for i := 0; i < n; i++ {
			tag.Floats = append(tag.Floats, math.Float64frombits(p.order.Uint64(raw[i*8:])))
		}
	}
}

func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
