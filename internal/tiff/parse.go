package tiff

import (
	"encoding/binary"
	"fmt"
)

// DefaultAccessBudget bounds how many IFD visits a parse will perform. TIFF IFD
// graphs can be cyclic or share subdirectories, so parsing must always run
// under an explicit access budget rather than assuming a finite tree.
const DefaultAccessBudget = 256

// Options controls a parse.
type Options struct {
	// AccessBudget is the maximum number of IFD visits (pointer followings).
	AccessBudget int
}

// byteReader provides endian-aware bounded reads over the import blob.
type byteReader struct {
	b    []byte
	bo   binary.ByteOrder
}

func (r *byteReader) need(off, n int64) error {
	if off < 0 || n < 0 || off > int64(len(r.b)) || off+n > int64(len(r.b)) {
		return &ParseError{Offset: off, Msg: fmt.Sprintf("read of %d bytes exceeds file length %d", n, len(r.b))}
	}
	return nil
}

func (r *byteReader) u16(off int64) (uint16, error) {
	if err := r.need(off, 2); err != nil {
		return 0, err
	}
	return r.bo.Uint16(r.b[off : off+2]), nil
}

func (r *byteReader) u32(off int64) (uint32, error) {
	if err := r.need(off, 4); err != nil {
		return 0, err
	}
	return r.bo.Uint32(r.b[off : off+4]), nil
}

func (r *byteReader) u64(off int64) (uint64, error) {
	if err := r.need(off, 8); err != nil {
		return 0, err
	}
	return r.bo.Uint64(r.b[off : off+8]), nil
}

// Parse decodes blob into an IFD graph under the default access budget.
func Parse(blob []byte) (*File, error) {
	return ParseWithOptions(blob, Options{AccessBudget: DefaultAccessBudget})
}

// ParseWithOptions decodes blob with an explicit access budget.
func ParseWithOptions(blob []byte, opt Options) (*File, error) {
	budget := opt.AccessBudget
	if budget <= 0 {
		budget = DefaultAccessBudget
	}
	if len(blob) < 8 {
		return nil, &ParseError{Offset: 0, Msg: "file shorter than 8-byte TIFF header"}
	}

	var order binary.ByteOrder
	var bo ByteOrder
	switch string(blob[0:2]) {
	case "II":
		order = binary.LittleEndian
		bo = LittleEndian
	case "MM":
		order = binary.BigEndian
		bo = BigEndian
	default:
		return nil, &ParseError{Offset: 0, Msg: fmt.Sprintf("invalid byte-order mark %q", blob[0:2])}
	}

	rd := &byteReader{b: blob, bo: order}
	magic, err := rd.u16(2)
	if err != nil {
		return nil, err
	}

	f := &File{Blob: blob, IFDs: map[string]*IFD{}}
	switch magic {
	case 42:
		if len(blob) < 8 {
			return nil, &ParseError{Offset: 0, Msg: "classic TIFF header truncated"}
		}
		first, err := rd.u32(4)
		if err != nil {
			return nil, err
		}
		f.Header = Header{ByteOrder: bo, Version: Classic, Magic: 42, FirstIFD: int64(first), OffsetsAre8: false, Range: ByteRange{0, 8}}
	case 43:
		if len(blob) < 16 {
			return nil, &ParseError{Offset: 0, Msg: "BigTIFF header shorter than 16 bytes"}
		}
		bitsize, err := rd.u16(4)
		if err != nil {
			return nil, err
		}
		if bitsize != 8 {
			return nil, &ParseError{Offset: 4, Msg: fmt.Sprintf("BigTIFF offset size is %d, only 8 supported", bitsize)}
		}
		zero, err := rd.u16(6)
		if err != nil {
			return nil, err
		}
		if zero != 0 {
			return nil, &ParseError{Offset: 6, Msg: fmt.Sprintf("BigTIFF reserved field is %d, expected 0", zero)}
		}
		first, err := rd.u64(8)
		if err != nil {
			return nil, err
		}
		f.Header = Header{ByteOrder: bo, Version: BigTIFF, Magic: 43, FirstIFD: int64(first), OffsetsAre8: true, Range: ByteRange{0, 16}}
	default:
		return nil, &ParseError{Offset: 2, Msg: fmt.Sprintf("unknown TIFF magic %d", magic)}
	}

	p := &parser{
		File:   f,
		rd:     rd,
		seen:   map[int64]string{},
		edges:  map[string]bool{},
	}
	p.walk(f.Header.FirstIFD, budget)
	p.resolveBlocks()
	return f, nil
}

type parser struct {
	*File
	rd    *byteReader
	seen  map[int64]string // offset -> IFD key
	edges map[string]bool
	idx   int
}

func ifdKey(off int64) string { return fmt.Sprintf("ifd@%d", off) }

type pointerRef struct {
	from   string
	tag    TagID // 0 means next-IFD chain
	to     int64
	source ByteRange
}

// walk performs the budgeted IFD traversal. Parsing one IFD reveals more
// pointers; revisiting an offset marks a cycle/shared node but never reparses.
func (p *parser) walk(first int64, budget int) {
	queue := []pointerRef{{from: "", tag: 0, to: first, source: p.Header.Range}}
	exhausted := false

	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]

		if budget <= 0 {
			exhausted = true
			p.Notes = append(p.Notes, StructuralNote{
				Kind: "access_budget_exhausted",
				At:   ref.source,
				Message: fmt.Sprintf("IFD visit budget exhausted while following pointer to offset %d; remaining pointers not traversed", ref.to),
			})
			break
		}
		budget--

		key := ifdKey(ref.to)
		if existing, ok := p.seen[ref.to]; ok {
			_ = key
			noteKind := "shared_subdirectory"
			if p.reaches(existing, ref.to) || existing == ref.from {
				noteKind = "ifd_cycle"
			}
			p.Notes = append(p.Notes, StructuralNote{
				Kind:    noteKind,
				At:      ref.source,
				Message: fmt.Sprintf("IFD at offset %d reached again from %s (edge tag %d)", ref.to, orRoot(ref.from), ref.tag),
			})
			p.link(ref.from, ref.tag, existing, ref.source)
			continue
		}

		ifd, err := p.parseIFD(ref.to)
		if err != nil {
			p.Notes = append(p.Notes, StructuralNote{
				Kind: "ifd_parse_error", At: ByteRange{Offset: ref.to},
				Message: err.Error(),
			})
			continue
		}
		ifd.Index = p.idx
		p.idx++
		p.seen[ref.to] = ifd.Key
		p.IFDs[ifd.Key] = ifd
		p.Order = append(p.Order, ifd.Key)
		if ref.from == "" {
			p.Roots = append(p.Roots, ifd.Key)
		} else {
			p.link(ref.from, ref.tag, ifd.Key, ref.source)
		}

		queue = append(queue, pointerRef{from: ifd.Key, tag: 0, to: ifd.NextOffset, source: p.nextPtrRange(ref.to)})
		for _, t := range ifd.Tags {
			switch t.ID {
			case TagSubIFDs, TagExifIFD, TagGPSIFD:
				// SubIFDs carries an array of IFD pointers; Exif/GPS one.
				ints := p.DecodeIntegers(t)
				for _, v := range ints {
					queue = append(queue, pointerRef{from: ifd.Key, tag: t.ID, to: v, source: t.Value.ValueRange})
				}
			}
		}
	}
	_ = exhausted
}

func orRoot(k string) string {
	if k == "" {
		return "header"
	}
	return k
}

// reaches reports whether there is already a path from key `from` to target
// offset (used to classify cycles vs shared-but-acyclic subdirectories).
func (p *parser) reaches(from string, targetOff int64) bool {
	if from == "" {
		return false
	}
	visited := map[string]bool{}
	var dfs func(k string) bool
	dfs = func(k string) bool {
		if k == ifdKey(targetOff) {
			return true
		}
		if visited[k] {
			return false
		}
		visited[k] = true
		ifd := p.IFDs[k]
		if ifd == nil {
			return false
		}
		if ifd.NextKey != "" && dfs(ifd.NextKey) {
			return true
		}
		for _, s := range ifd.SubKeys {
			if dfs(s.Key) {
				return true
			}
		}
		return false
	}
	return dfs(from)
}

func (p *parser) link(from string, tag TagID, to string, _ ByteRange) {
	if from == "" {
		return
	}
	src := p.IFDs[from]
	if src == nil {
		return
	}
	edge := fmt.Sprintf("%s:%d:%s", from, tag, to)
	if p.edges[edge] {
		return
	}
	p.edges[edge] = true
	if tag == 0 {
		src.NextKey = to
	} else {
		src.SubKeys = append(src.SubKeys, SubLink{TagID: tag, Key: to})
	}
}

func (p *parser) nextPtrRange(ifdOff int64) ByteRange {
	if p.Header.OffsetsAre8 {
		return ByteRange{Offset: ifdOff, Length: 8}
	}
	return ByteRange{Offset: ifdOff, Length: 4}
}

// parseIFD reads the count, entries and next pointer of one IFD. It does not
// follow any links; walk() drives traversal so the budget is enforced.
func (p *parser) parseIFD(off int64) (*IFD, error) {
	eight := p.Header.OffsetsAre8
	var count uint64
	var countLen int64
	var entryLen int64
	var totalLen int64
	if eight {
		c, err := p.rd.u64(off)
		if err != nil {
			return nil, err
		}
		count = uint64(c)
		countLen = 8
		entryLen = 20
		totalLen = 8 + int64(c)*20 + 8
	} else {
		c, err := p.rd.u16(off)
		if err != nil {
			return nil, err
		}
		count = uint64(c)
		countLen = 2
		entryLen = 12
		totalLen = 2 + int64(c)*12 + 4
	}
	if err := p.rd.need(off, totalLen); err != nil {
		return nil, &ParseError{Offset: off, Msg: "IFD structure overruns file: " + err.Error()}
	}

	ifd := &IFD{Key: ifdKey(off), Offset: off, Range: ByteRange{Offset: off, Length: totalLen}}
	cur := off + countLen
	for i := uint64(0); i < count; i++ {
		tag, err := p.parseEntry(cur, entryLen, eight)
		if err != nil {
			return nil, err
		}
		ifd.Tags = append(ifd.Tags, tag)
		cur += entryLen
	}

	if eight {
		next, err := p.rd.u64(cur)
		if err != nil {
			return nil, err
		}
		ifd.NextOffset = int64(next)
	} else {
		next, err := p.rd.u32(cur)
		if err != nil {
			return nil, err
		}
		ifd.NextOffset = int64(next)
	}
	return ifd, nil
}

func (p *parser) parseEntry(off, entryLen int64, eight bool) (*Tag, error) {
	id, err := p.rd.u16(off)
	if err != nil {
		return nil, err
	}
	typeCode, err := p.rd.u16(off + 2)
	if err != nil {
		return nil, err
	}
	ft := FieldType(typeCode)
	ts := ft.Size()
	if ts == 0 {
		return nil, &ParseError{Offset: off + 2, Msg: fmt.Sprintf("tag %d uses unknown field type %d", id, typeCode)}
	}

	var count uint64
	var valueFieldOff int64
	if eight {
		count, err = p.rd.u64(off + 4)
		if err != nil {
			return nil, err
		}
		valueFieldOff = off + 12
	} else {
		c, err := p.rd.u32(off + 4)
		if err != nil {
			return nil, err
		}
		count = uint64(c)
		valueFieldOff = off + 8
	}
	valueLen := int64(ts) * int64(count)
	if valueLen < 0 {
		return nil, &ParseError{Offset: off + 4, Msg: "tag value length overflows"}
	}
	inlineCap := int64(4)
	if eight {
		inlineCap = 8
	}

	rv := RawValue{Type: ft, Count: int64(count)}
	if valueLen <= inlineCap {
		rv.Inline = true
		rv.ValueRange = ByteRange{Offset: valueFieldOff, Length: valueLen}
		rv.ExternalRange = nil
		if err := p.rd.need(valueFieldOff, valueLen); err != nil {
			return nil, err
		}
		rv.Raw = append([]byte(nil), p.rd.b[valueFieldOff:valueFieldOff+valueLen]...)
	} else {
		rv.Inline = false
		var extOff int64
		if eight {
			v, err := p.rd.u64(valueFieldOff)
			if err != nil {
				return nil, err
			}
			extOff = int64(v)
		} else {
			v, err := p.rd.u32(valueFieldOff)
			if err != nil {
				return nil, err
			}
			extOff = int64(v)
		}
		er := ByteRange{Offset: extOff, Length: valueLen}
		if err := p.rd.need(extOff, valueLen); err != nil {
			return nil, &ParseError{Offset: extOff, Msg: fmt.Sprintf("external value of tag %d (%s) overruns file", id, TagName(TagID(id)))}
		}
		rv.ValueRange = er
		rv.ExternalRange = &er
		rv.Raw = append([]byte(nil), p.rd.b[extOff:extOff+valueLen]...)
	}

	return &Tag{
		ID:    TagID(id),
		Name:  TagName(TagID(id)),
		Range: ByteRange{Offset: off, Length: entryLen},
		Value: rv,
	}, nil
}
