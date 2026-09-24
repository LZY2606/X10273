package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// Builder constructs deterministic, minimal TIFF blobs for samples and tests.
// It deliberately permits hostile structures (cycles, shared IFDs, overlapping
// blocks, out-of-range offsets) so the parser is exercised against real bytes.
type Builder struct {
	order ByteOrder
	big   bool
	data  []ByteRegion
	ifds  []*BuiltIFD
	first int
}

// ByteRegion is an opaque placed blob (pixel block / external payload).
type ByteRegion struct {
	Offset int64
	Data   []byte
	Kind   string
}

type builtTag struct {
	id         TagID
	typ        FieldType
	count      int64
	raw        []byte
	pointer    bool
	children   []*BuiltIFD
	externalOff int64
}

// BuiltIFD is a handle to an IFD being assembled.
type BuiltIFD struct {
	id          int
	tags        []builtTag
	next        *BuiltIFD
	rawNext     int64
	rawNextSet  bool
	fileOff     int64
}

// NewBuilder creates a builder; big selects BigTIFF.
func NewBuilder(big bool, bo ByteOrder) *Builder {
	return &Builder{order: bo, big: big, first: -1}
}

func (b *Builder) endian() binary.ByteOrder { return orderOf(b.order) }

// NewIFD registers an empty IFD.
func (b *Builder) NewIFD() *BuiltIFD {
	d := &BuiltIFD{id: len(b.ifds)}
	b.ifds = append(b.ifds, d)
	return d
}

// SetFirst marks the root IFD.
func (b *Builder) SetFirst(d *BuiltIFD) { b.first = d.id }

// AddData places an opaque data region and returns its eventual offset via
// RegionOffset after Build; offsets are assigned in layout().
func (b *Builder) AddData(kind string, data []byte) int {
	b.data = append(b.data, ByteRegion{Data: data, Kind: kind})
	return len(b.data) - 1
}

// Tag attaches an integer-valued tag (SHORT/LONG/LONG8/IFD/IFD8...).
func (b *Builder) Tag(d *BuiltIFD, id TagID, typ FieldType, vals ...uint64) *BuiltIFD {
	size := typ.Size()
	o := b.endian()
	raw := make([]byte, 0, size*len(vals))
	for _, v := range vals {
		buf := make([]byte, size)
		switch size {
		case 2:
			o.PutUint16(buf, uint16(v))
		case 4:
			o.PutUint32(buf, uint32(v))
		case 8:
			o.PutUint64(buf, v)
		}
		raw = append(raw, buf...)
	}
	d.tags = append(d.tags, builtTag{id: id, typ: typ, count: int64(len(vals)), raw: raw})
	return d
}

// TagBytes attaches a tag whose logical value is already serialized.
func (b *Builder) TagBytes(d *BuiltIFD, id TagID, typ FieldType, count int64, raw []byte) {
	d.tags = append(d.tags, builtTag{id: id, typ: typ, count: count, raw: raw})
}

// TagDoubles attaches a DOUBLE-array tag.
func (b *Builder) TagDoubles(d *BuiltIFD, id TagID, vals ...float64) {
	b.TagBytes(d, id, 12, int64(len(vals)), Doubles(b.order, vals...))
}

// TagASCII attaches an ASCII tag.
func (b *Builder) TagASCII(d *BuiltIFD, id TagID, s string) {
	raw := ASCII(s)
	b.TagBytes(d, id, 2, int64(len(raw)), raw)
}

// TagShorts attaches a SHORT-array tag (used for GeoKeyDirectory).
func (b *Builder) TagShorts(d *BuiltIFD, id TagID, vals ...uint16) {
	o := b.endian()
	raw := make([]byte, 0, 2*len(vals))
	for _, v := range vals {
		buf := make([]byte, 2)
		o.PutUint16(buf, v)
		raw = append(raw, buf...)
	}
	d.tags = append(d.tags, builtTag{id: id, typ: 3, count: int64(len(vals)), raw: raw})
}

// TagSubIFDs attaches a SubIFDs pointer array.
func (b *Builder) TagSubIFDs(d *BuiltIFD, children ...*BuiltIFD) {
	typ := FieldType(13)
	if b.big {
		typ = 18
	}
	d.tags = append(d.tags, builtTag{
		id: TagSubIFDs, typ: typ, count: int64(len(children)),
		pointer: true, children: children,
	})
}

// Next links the classic next-IFD chain.
func (b *Builder) Next(d, to *BuiltIFD) { d.next = to }

// RawNext forces an arbitrary next-IFD offset (cycles / bad pointers).
func (b *Builder) RawNext(d *BuiltIFD, off int64) { d.rawNext, d.rawNextSet = off, true }

// Doubles serializes doubles.
func Doubles(bo ByteOrder, vals ...float64) []byte {
	o := orderOf(bo)
	out := make([]byte, 0, 8*len(vals))
	for _, v := range vals {
		buf := make([]byte, 8)
		o.PutUint64(buf, math.Float64bits(v))
		out = append(out, buf...)
	}
	return out
}

// ASCII serializes a NUL-terminated ASCII value.
func ASCII(s string) []byte { return append([]byte(s), 0) }

// RegionOffset returns the assigned offset of a data region after layout.
func (b *Builder) RegionOffset(idx int) int64 { return b.data[idx].Offset }

// DataRegion exposes a placed region.
func (b *Builder) DataRegion(idx int) ByteRegion { return b.data[idx] }

func align2(v int64) int64 {
	if v%2 == 1 {
		return v + 1
	}
	return v
}

func (b *Builder) entryLen() int64 {
	if b.big {
		return 20
	}
	return 12
}

func (b *Builder) ifdSize(d *BuiltIFD) int64 {
	head := int64(2)
	if b.big {
		head = 8
	}
	next := int64(4)
	if b.big {
		next = 8
	}
	return head + int64(len(d.tags))*b.entryLen() + next
}

func valueLen(t *builtTag) int64 {
	if t.pointer {
		return t.count * int64(t.typ.Size())
	}
	return int64(len(t.raw))
}

// layout assigns absolute offsets deterministically: header, opaque data,
// external tag values, then IFDs in registration order.
func (b *Builder) layout() {
	hdr := int64(8)
	if b.big {
		hdr = 16
	}
	cursor := hdr
	for i := range b.data {
		cursor = align2(cursor)
		b.data[i].Offset = cursor
		cursor += int64(len(b.data[i].Data))
	}
	cap := int64(4)
	if b.big {
		cap = 8
	}
	for _, d := range b.ifds {
		for ti := range d.tags {
			t := &d.tags[ti]
			if valueLen(t) > cap {
				cursor = align2(cursor)
				t.externalOff = cursor
				cursor = align2(cursor + valueLen(t))
			}
		}
	}
	for _, d := range b.ifds {
		cursor = align2(cursor)
		d.fileOff = cursor
		cursor += b.ifdSize(d)
	}
}

// Build serializes the planned file and returns the blob.
func (b *Builder) Build() ([]byte, error) {
	if b.first < 0 || b.first >= len(b.ifds) {
		return nil, fmt.Errorf("builder: no first IFD set")
	}
	b.layout()
	total := b.ifds[len(b.ifds)-1].fileOff + b.ifdSize(b.ifds[len(b.ifds)-1])
	for _, r := range b.data {
		if end := r.Offset + int64(len(r.Data)); end > total {
			total = end
		}
	}
	for _, d := range b.ifds {
		for ti := range d.tags {
			t := &d.tags[ti]
			if t.externalOff != 0 {
				if end := align2(t.externalOff + valueLen(t)); end > total {
					total = end
				}
			}
		}
	}

	buf := make([]byte, total)
	o := b.endian()
	if b.order == LittleEndian {
		buf[0], buf[1] = 'I', 'I'
	} else {
		buf[0], buf[1] = 'M', 'M'
	}
	if !b.big {
		o.PutUint16(buf[2:], 42)
		o.PutUint32(buf[4:], uint32(b.ifds[b.first].fileOff))
	} else {
		o.PutUint16(buf[2:], 43)
		o.PutUint16(buf[4:], 8)
		o.PutUint16(buf[6:], 0)
		o.PutUint64(buf[8:], uint64(b.ifds[b.first].fileOff))
	}
	for _, r := range b.data {
		copy(buf[r.Offset:], r.Data)
	}
	for _, d := range b.ifds {
		b.serializeIFD(buf, d)
	}
	return buf, nil
}

func (b *Builder) serializeIFD(buf []byte, d *BuiltIFD) {
	o := b.endian()
	off := d.fileOff
	if b.big {
		o.PutUint64(buf[off:], uint64(len(d.tags)))
		off += 8
	} else {
		o.PutUint16(buf[off:], uint16(len(d.tags)))
		off += 2
	}
	order := make([]int, len(d.tags))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return d.tags[order[i]].id < d.tags[order[j]].id })

	for _, i := range order {
		t := d.tags[i]
		o.PutUint16(buf[off:], uint16(t.id))
		o.PutUint16(buf[off+2:], uint16(t.typ))
		valueField := off + 8
		if b.big {
			o.PutUint64(buf[off+4:], uint64(t.count))
			valueField = off + 12
		} else {
			o.PutUint32(buf[off+4:], uint32(t.count))
		}
		val := b.serializeValue(&t)
		if t.externalOff != 0 {
			copy(buf[t.externalOff:], val)
			if b.big {
				o.PutUint64(buf[valueField:], uint64(t.externalOff))
			} else {
				o.PutUint32(buf[valueField:], uint32(t.externalOff))
			}
		} else {
			copy(buf[valueField:], val)
		}
		off += b.entryLen()
	}
	var nextOff int64
	switch {
	case d.rawNextSet:
		nextOff = d.rawNext
	case d.next != nil:
		nextOff = d.next.fileOff
	}
	if b.big {
		o.PutUint64(buf[off:], uint64(nextOff))
	} else {
		o.PutUint32(buf[off:], uint32(nextOff))
	}
}

func (b *Builder) serializeValue(t *builtTag) []byte {
	if !t.pointer {
		return t.raw
	}
	o := b.endian()
	size := t.typ.Size()
	out := make([]byte, 0, size*len(t.children))
	for _, c := range t.children {
		buf := make([]byte, size)
		switch size {
		case 4:
			o.PutUint32(buf, uint32(c.fileOff))
		case 8:
			o.PutUint64(buf, uint64(c.fileOff))
		}
		out = append(out, buf...)
	}
	return out
}
