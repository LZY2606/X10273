package tiff

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
)

const AlgorithmVersion = "tiff-graph-v1"

const (
	TagNewSubfileType      uint16 = 254
	TagImageWidth          uint16 = 256
	TagImageLength         uint16 = 257
	TagBitsPerSample       uint16 = 258
	TagCompression         uint16 = 259
	TagPhotometric         uint16 = 262
	TagStripOffsets        uint16 = 273
	TagSamplesPerPixel     uint16 = 277
	TagRowsPerStrip        uint16 = 278
	TagStripByteCounts     uint16 = 279
	TagXResolution         uint16 = 282
	TagYResolution         uint16 = 283
	TagPlanarConfig        uint16 = 284
	TagTileWidth           uint16 = 322
	TagTileLength          uint16 = 323
	TagTileOffsets         uint16 = 324
	TagTileByteCounts      uint16 = 325
	TagSampleFormat        uint16 = 339
	TagSubIFDs             uint16 = 330
	TagJPEGTables          uint16 = 347
	TagGeoKeyDirectory     uint16 = 34735
	TagGeoDoubleParams     uint16 = 34736
	TagGeoAsciiParams      uint16 = 34737
	TagModelPixelScale     uint16 = 33550
	TagModelTiepoint       uint16 = 33922
	TagModelTransformation uint16 = 34264
)

type valueType struct {
	name string
	size int
}

var classicTypes = map[uint16]valueType{
	1: {"BYTE", 1}, 2: {"ASCII", 1}, 3: {"SHORT", 2}, 4: {"LONG", 4},
	5: {"RATIONAL", 8}, 6: {"SBYTE", 1}, 7: {"UNDEFINED", 1},
	8: {"SSHORT", 2}, 9: {"SLONG", 4}, 10: {"SRATIONAL", 8},
	11: {"FLOAT", 4}, 12: {"DOUBLE", 8},
}

var bigTypes = map[uint16]valueType{
	1: {"BYTE", 1}, 2: {"ASCII", 1}, 3: {"SHORT", 2}, 4: {"LONG", 4},
	5: {"RATIONAL", 8}, 6: {"SBYTE", 1}, 7: {"UNDEFINED", 1},
	8: {"SSHORT", 2}, 9: {"SLONG", 4}, 10: {"SRATIONAL", 8},
	11: {"FLOAT", 4}, 12: {"DOUBLE", 8}, 16: {"LONG8", 8}, 17: {"SLONG8", 8}, 18: {"IFD8", 8},
}

type parser struct {
	id     string
	data   []byte
	bo     binary.ByteOrder
	big    bool
	budget Budget

	header   Header
	ifds     map[int64]*IFD
	tags     map[int64][]Tag
	edges    []Edge
	findings []Finding
	visited  map[int64]bool
	queued   map[int64]bool
	parents  map[int64][]string
}

func Parse(in Input) (*Graph, error) {
	if in.DocumentID == "" {
		return nil, fmt.Errorf("document id is required")
	}
	if len(in.Bytes) < 4 {
		return nil, fmt.Errorf("truncated TIFF header: %d bytes", len(in.Bytes))
	}
	bo, big, err := readHeaderKind(in.Bytes)
	if err != nil {
		return nil, err
	}
	maximum := in.MaxIFDVisits
	if maximum <= 0 {
		maximum = 64
	}
	p := &parser{
		id: in.DocumentID, data: in.Bytes, bo: bo, big: big,
		budget: Budget{MaximumIFDVisits: maximum, Remaining: maximum},
		ifds:   map[int64]*IFD{}, tags: map[int64][]Tag{},
		visited: map[int64]bool{}, queued: map[int64]bool{}, parents: map[int64][]string{},
	}
	if err := p.parseHeader(); err != nil {
		return nil, err
	}
	p.traverse()
	graph := p.buildGraph()
	buildBlocks(graph)
	buildGeo(graph)
	return graph, nil
}

func readHeaderKind(data []byte) (binary.ByteOrder, bool, error) {
	switch {
	case data[0] == 'I' && data[1] == 'I':
		return binary.LittleEndian, false, nil
	case data[0] == 'M' && data[1] == 'M':
		return binary.BigEndian, false, nil
	default:
		return nil, false, fmt.Errorf("invalid byte order mark %02x%02x", data[0], data[1])
	}
}

func (p *parser) parseHeader() error {
	data := p.data
	if len(data) < 8 {
		return fmt.Errorf("truncated classic TIFF header: %d bytes", len(data))
	}
	order := BigEndian
	if p.bo == binary.LittleEndian {
		order = LittleEndian
	}
	magic := p.bo.Uint16(data[2:4])
	var first int64
	if magic == 42 {
		if uint32(p.bo.Uint32(data[4:8])) >= uint32(len(data)) {
			p.find(Finding{Kind: "invalid_first_ifd", Severity: "fatal", ObjectID: p.id,
				Message:   fmt.Sprintf("first IFD offset %d is outside %d bytes", p.bo.Uint32(data[4:8]), len(data)),
				ByteRange: &ByteRange{Offset: 4, Length: 4, End: 8}})
		}
		first = int64(p.bo.Uint32(data[4:8]))
		p.header = Header{ByteOrder: order, Classic: true, Magic: magic, FirstIFD: first,
			ByteRange: ByteRange{0, 8, 8}, FirstIFDRange: ByteRange{4, 4, 8}}
		return nil
	}
	if magic != 43 || len(data) < 16 {
		return fmt.Errorf("invalid TIFF magic %d or truncated BigTIFF header", magic)
	}
	offSize := p.bo.Uint16(data[4:6])
	zero := p.bo.Uint16(data[6:8])
	if offSize != 8 || zero != 0 {
		return fmt.Errorf("invalid BigTIFF offsetsize=%d reserved=%d", offSize, zero)
	}
	first = int64(p.bo.Uint64(data[8:16]))
	if uint64(first) >= uint64(len(data)) {
		p.find(Finding{Kind: "invalid_first_ifd", Severity: "fatal", ObjectID: p.id,
			Message:   fmt.Sprintf("first IFD offset %d is outside %d bytes", first, len(data)),
			ByteRange: &ByteRange{Offset: 8, Length: 8, End: 16}})
	}
	p.header = Header{ByteOrder: order, Classic: false, BigTIFF: true, Magic: magic, FirstIFD: first,
		ByteRange: ByteRange{0, 16, 16}, FirstIFDRange: ByteRange{8, 8, 16}}
	return nil
}

type pendingIFD struct {
	offset int64
	kind   string
	parent int64
	tagID  string
	range_ ByteRange
}

func (p *parser) traverse() {
	root := p.header.FirstIFD
	queue := []pendingIFD{{offset: root, kind: "root", parent: -1, range_: p.header.FirstIFDRange}}
	p.queued[root] = true
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next.offset < 0 || uint64(next.offset) >= uint64(len(p.data)) {
			p.find(Finding{Kind: "ifd_offset_out_of_bounds", Severity: "error", ObjectID: ifdID(p.id, next.offset),
				Message:   fmt.Sprintf("IFD offset %d is outside the %d byte file", next.offset, len(p.data)),
				ByteRange: &next.range_})
			continue
		}
		if p.visited[next.offset] {
			p.ifds[next.offset].Shared = true
			p.edges = append(p.edges, Edge{
				ID:   edgeID(p.id, next.kind, next.parent, next.offset),
				Kind: next.kind, SourceID: ifdID(p.id, next.parent), TargetID: ifdID(p.id, next.offset),
				Offset: next.offset, TagID: next.tagID, ByteRange: next.range_})
			p.find(Finding{Kind: "shared_subifd", Severity: "diagnostic", ObjectID: ifdID(p.id, next.offset),
				Message: "IFD is reachable from more than one directory edge"})
			continue
		}
		if p.budget.Remaining <= 0 {
			p.budget.Exhausted = true
			p.find(Finding{Kind: "access_budget_exhausted", Severity: "error", ObjectID: ifdID(p.id, next.offset),
				Message:   fmt.Sprintf("IFD access budget stopped before offset %d; maximum %d visits", next.offset, p.budget.MaximumIFDVisits),
				ByteRange: &next.range_})
			continue
		}
		tags, err := p.parseIFD(next.offset)
		if err != nil {
			p.find(Finding{Kind: "malformed_ifd", Severity: "fatal", ObjectID: ifdID(p.id, next.offset),
				Message: err.Error(), ByteRange: &next.range_})
			continue
		}
		p.visited[next.offset] = true
		p.budget.IFDVisits++
		p.budget.Remaining--
		if next.parent >= 0 {
			p.edges = append(p.edges, Edge{
				ID:   edgeID(p.id, next.kind, next.parent, next.offset),
				Kind: next.kind, SourceID: ifdID(p.id, next.parent), TargetID: ifdID(p.id, next.offset),
				Offset: next.offset, TagID: next.tagID, ByteRange: next.range_})
		}
		queue = append(queue, p.discoverOffsets(next.offset, tags)...)
	}
}

func (p *parser) discoverOffsets(ifdOffset int64, tags []Tag) []pendingIFD {
	var pending []pendingIFD
	ifd := p.ifds[ifdOffset]
	if ifd.NextOffset != 0 {
		if _, seen := p.ifds[ifd.NextOffset]; seen && p.visited[ifd.NextOffset] {
			p.find(Finding{Kind: "ifd_cycle", Severity: "error", ObjectID: ifdID(p.id, ifd.NextOffset),
				Message:   fmt.Sprintf("IFD next pointer at offset %d returns to known IFD %d", ifdOffset, ifd.NextOffset),
				ByteRange: &ifd.NextRange})
		}
		pending = append(pending, pendingIFD{offset: ifd.NextOffset, kind: "next", parent: ifdOffset, range_: ifd.NextRange})
	}
	for _, tag := range tags {
		if tag.Tag != TagSubIFDs || !tag.ValuePresent {
			continue
		}
		values := integerValues(tag.Raw)
		for idx, raw := range values {
			offset, err := parseSignedInt64(raw)
			if err != nil {
				continue
			}
			r := tag.ValueRange
			unit := int64(p.offsetBytes())
			r.Offset += int64(idx) * unit
			r.Length = unit
			r.End = r.Offset + unit
			pending = append(pending, pendingIFD{offset: offset, kind: "subifd", parent: ifdOffset, tagID: tag.ID, range_: r})
		}
	}
	return pending
}

func (p *parser) parseIFD(offset int64) ([]Tag, error) {
	if existing, ok := p.tags[offset]; ok {
		return existing, nil
	}
	if offset+2 > int64(len(p.data)) {
		return nil, fmt.Errorf("IFD count at offset %d is truncated", offset)
	}
	count := int64(p.bo.Uint16(p.data[offset : offset+2]))
	entrySize := int64(12)
	entryStart := offset + 2
	if p.big {
		if offset+8 > int64(len(p.data)) {
			return nil, fmt.Errorf("BigTIFF count at offset %d is truncated", offset)
		}
		count = int64(p.bo.Uint64(p.data[offset : offset+8]))
		entrySize = 20
		entryStart = offset + 8
	}
	end := entryStart + count*entrySize
	nextEnd := end + int64(p.offsetBytes())
	if count < 0 || end > int64(len(p.data)) {
		return nil, fmt.Errorf("IFD at offset %d claims %d entries beyond file", offset, count)
	}
	if nextEnd > int64(len(p.data)) {
		return nil, fmt.Errorf("IFD next pointer at offset %d is truncated", end)
	}
	var nextOffset int64
	nextRange := ByteRange{end, int64(p.offsetBytes()), nextEnd}
	if p.big {
		nextOffset = int64(p.bo.Uint64(p.data[end:nextEnd]))
	} else {
		nextOffset = int64(p.bo.Uint32(p.data[end:nextEnd]))
	}
	ifd := &IFD{ID: ifdID(p.id, offset), Offset: offset, ByteRange: ByteRange{offset, nextEnd - offset, nextEnd},
		EntryCount: int(count), NextOffset: nextOffset, NextRange: nextRange}
	p.ifds[offset] = ifd

	tags := make([]Tag, 0, count)
	for i := int64(0); i < count; i++ {
		start := entryStart + i*entrySize
		tag, err := p.parseTag(offset, start, entrySize)
		if err != nil {
			p.find(Finding{Kind: "malformed_tag", Severity: "error", ObjectID: fmt.Sprintf("%s/tag/%d", ifd.ID, i),
				Message: err.Error(), ByteRange: &ByteRange{start, entrySize, start + entrySize}})
			continue
		}
		tags = append(tags, tag)
	}
	p.tags[offset] = tags
	return tags, nil
}

func (p *parser) offsetBytes() int {
	if p.big {
		return 8
	}
	return 4
}

func (p *parser) parseTag(ifdOffset, start, entrySize int64) (Tag, error) {
	tagID := p.bo.Uint16(p.data[start : start+2])
	typeID := p.bo.Uint16(p.data[start+2 : start+4])
	types := classicTypes
	if p.big {
		types = bigTypes
	}
	typ, ok := types[typeID]
	if !ok {
		return Tag{}, fmt.Errorf("unknown TIFF value type %d for tag %d", typeID, tagID)
	}
	var count int64
	valueOffsetPos := start + 4
	if p.big {
		count = int64(p.bo.Uint64(p.data[start+4 : start+12]))
		valueOffsetPos = start + 12
	} else {
		count = int64(p.bo.Uint32(p.data[start+4 : start+8]))
		valueOffsetPos = start + 8
	}
	if count < 0 {
		return Tag{}, fmt.Errorf("negative count in tag %d", tagID)
	}
	dataLen := count * int64(typ.size)
	if dataLen > int64(len(p.data)) {
		return Tag{}, fmt.Errorf("tag %d value length %d exceeds file", tagID, dataLen)
	}
	slotLen := int64(p.offsetBytes())
	valueRange := ByteRange{valueOffsetPos, slotLen, valueOffsetPos + slotLen}
	external := dataLen > slotLen
	valuePresent := true
	if external {
		if p.big {
			valueRange.Offset = int64(p.bo.Uint64(p.data[valueOffsetPos : valueOffsetPos+8]))
		} else {
			valueRange.Offset = int64(p.bo.Uint32(p.data[valueOffsetPos : valueOffsetPos+4]))
		}
		valueRange.Length = dataLen
		valueRange.End = valueRange.Offset + dataLen
		if valueRange.Offset < 0 || valueRange.End > int64(len(p.data)) {
			valuePresent = false
			p.find(Finding{Kind: "external_value_out_of_bounds", Severity: "error",
				ObjectID:  fmt.Sprintf("%s/tag/%d", ifdID(p.id, ifdOffset), tagID),
				Message:   fmt.Sprintf("tag %d external value [%d,%d) exceeds %d bytes", tagID, valueRange.Offset, valueRange.End, len(p.data)),
				ByteRange: &valueRange})
		}
	}
	raw := RawValue{Type: typ.name, Count: int(count), Inline: !external}
	if valuePresent {
		buf := p.data[valueRange.Offset:valueRange.End]
		raw.Values = decodeValues(buf, typ, int(count), p.bo)
		raw.Hex = hex.EncodeToString(buf)
	} else {
		raw.Hex = hex.EncodeToString(p.data[valueOffsetPos : valueOffsetPos+slotLen])
	}
	return Tag{
		ID:    tagIDFor(p.id, ifdOffset, tagID),
		IFDID: ifdID(p.id, ifdOffset),
		Tag:   tagID, Name: tagName(tagID),
		EntryRange: ByteRange{start, entrySize, start + entrySize},
		ValueRange: valueRange, External: external,
		ValuePresent: valuePresent, Raw: raw,
	}, nil
}

func decodeValues(buf []byte, typ valueType, count int, bo binary.ByteOrder) []string {
	values := make([]string, 0, count)
	for i := 0; i < count; i++ {
		at := i * typ.size
		switch typ.name {
		case "BYTE", "SBYTE", "UNDEFINED":
			values = append(values, fmt.Sprintf("%d", buf[at]))
		case "ASCII":
			return []string{quoteASCII(buf)}
		case "SHORT":
			values = append(values, fmt.Sprintf("%d", bo.Uint16(buf[at:])))
		case "SSHORT":
			values = append(values, fmt.Sprintf("%d", int16(bo.Uint16(buf[at:]))))
		case "LONG", "LONG8", "IFD8":
			if typ.size == 8 {
				values = append(values, fmt.Sprintf("%d", bo.Uint64(buf[at:])))
			} else {
				values = append(values, fmt.Sprintf("%d", bo.Uint32(buf[at:])))
			}
		case "SLONG", "SLONG8":
			if typ.size == 8 {
				values = append(values, fmt.Sprintf("%d", int64(bo.Uint64(buf[at:]))))
			} else {
				values = append(values, fmt.Sprintf("%d", int32(bo.Uint32(buf[at:]))))
			}
		case "FLOAT":
			values = append(values, formatFloat(float64(math.Float32frombits(bo.Uint32(buf[at:])))))
		case "DOUBLE":
			values = append(values, formatFloat(math.Float64frombits(bo.Uint64(buf[at:]))))
		case "RATIONAL":
			n := bo.Uint32(buf[at:])
			d := bo.Uint32(buf[at+4:])
			values = append(values, fmt.Sprintf("%d/%d", n, d))
		case "SRATIONAL":
			n := int32(bo.Uint32(buf[at:]))
			d := int32(bo.Uint32(buf[at+4:]))
			values = append(values, fmt.Sprintf("%d/%d", n, d))
		}
	}
	return values
}

func quoteASCII(buf []byte) string {
	if len(buf) > 0 && buf[len(buf)-1] == 0 {
		buf = buf[:len(buf)-1]
	}
	return string(buf)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func (p *parser) find(f Finding) {
	if f.ID == "" {
		f.ID = findingID(p.id, f.Kind, f.ObjectID, f.ByteRange)
	}
	p.findings = append(p.findings, f)
}
