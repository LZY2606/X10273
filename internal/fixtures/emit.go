package fixtures

import (
	"encoding/binary"
	"math"
)

type scalar struct {
	u  uint64
	i  int64
	r  [2]int64
	is bool
}

type encoded struct {
	typ   uint16
	count uint64
	size  int64
	ascii []byte
	ints  []scalar
	doub  []float64
}

func encodeTag(t *Tag) encoded {
	switch {
	case t.Ascii != "":
		b := append([]byte(t.Ascii), 0)
		return encoded{typ: 2, count: uint64(len(b)), size: int64(len(b)), ascii: b}
	case len(t.Doubles) > 0:
		return encoded{typ: 12, count: uint64(len(t.Doubles)), size: 8 * int64(len(t.Doubles)), doub: t.Doubles}
	case len(t.SRationals) > 0:
		s := make([]scalar, len(t.SRationals))
		for i, pr := range t.SRationals {
			s[i] = scalar{r: [2]int64{int64(pr[0]), int64(pr[1])}, is: true}
		}
		return encoded{typ: 10, count: uint64(len(s)), size: 8 * int64(len(s)), ints: s}
	case len(t.SLongs) > 0:
		s := make([]scalar, len(t.SLongs))
		for i, v := range t.SLongs {
			s[i] = scalar{i: int64(v), is: true}
		}
		return encoded{typ: 9, count: uint64(len(s)), size: 4 * int64(len(s)), ints: s}
	case len(t.SShorts) > 0:
		s := make([]scalar, len(t.SShorts))
		for i, v := range t.SShorts {
			s[i] = scalar{i: int64(v), is: true}
		}
		return encoded{typ: 8, count: uint64(len(s)), size: 2 * int64(len(s)), ints: s}
	case len(t.RawExternal) > 0:
		s := make([]scalar, len(t.RawExternal))
		for i, b := range t.RawExternal {
			s[i] = scalar{u: uint64(b)}
		}
		return encoded{typ: t.Type, count: uint64(len(s)), size: int64(len(s)), ints: s}
	default:
		s := make([]scalar, len(t.Value))
		for i, v := range t.Value {
			s[i] = scalar{u: v}
		}
		return encoded{typ: t.Type, count: uint64(len(s)), size: fieldSize(t.Type) * int64(len(s)), ints: s}
	}
}

func (f *File) encodeInto(dst []byte, e encoded, bo binary.ByteOrder) {
	switch e.typ {
	case 2:
		copy(dst, e.ascii)
	case 12:
		for i, v := range e.doub {
			bo.PutUint64(dst[i*8:], math.Float64bits(v))
		}
	case 5:
		for i, s := range e.ints {
			bo.PutUint32(dst[i*8:], uint32(s.u))
			bo.PutUint32(dst[i*8+4:], uint32(s.u>>32))
		}
	case 10:
		for i, s := range e.ints {
			bo.PutUint32(dst[i*8:], uint32(s.r[0]))
			bo.PutUint32(dst[i*8+4:], uint32(s.r[1]))
		}
	default:
		sz := fieldSize(e.typ)
		for i, s := range e.ints {
			v := s.u
			if s.is {
				v = uint64(s.i)
			}
			putField(dst[int64(i)*sz:], v, sz, bo)
		}
	}
}

func putField(b []byte, v uint64, sz int64, bo binary.ByteOrder) {
	switch sz {
	case 1:
		b[0] = byte(v)
	case 2:
		bo.PutUint16(b, uint16(v))
	case 4:
		bo.PutUint32(b, uint32(v))
	case 8:
		bo.PutUint64(b, v)
	}
}

// Emit produces the final deterministic TIFF byte program.
func (f *File) Emit() []byte {
	bo := f.order()
	off := f.assign()

	heap := f.headerLen()
	for n, o := range off {
		if end := o + f.ifdSize(n); end > heap {
			heap = end
		}
	}
	total := heap
	for _, pl := range f.places {
		if end := pl.offset + int64(len(pl.data)); end > total {
			total = end
		}
	}
	if f.minSize > total {
		total = f.minSize
	}

	buf := make([]byte, total)
	if f.bo == LE {
		buf[0], buf[1] = 'I', 'I'
	} else {
		buf[0], buf[1] = 'M', 'M'
	}
	if f.big {
		bo.PutUint16(buf[2:], 43)
		bo.PutUint16(buf[6:], 0)
		bo.PutUint64(buf[8:], uint64(off[f.root]))
	} else {
		bo.PutUint16(buf[2:], 42)
		bo.PutUint32(buf[4:], uint32(off[f.root]))
	}

	// External-value heap, allocated in deterministic walk order.
	heapCursor := heap
	extOff := map[*Tag]int64{}
	seenAlloc := map[*IFD]bool{}
	var walkAlloc func(n *IFD)
	walkAlloc = func(n *IFD) {
		if n == nil || seenAlloc[n] {
			return
		}
		seenAlloc[n] = true
		for i := range n.Tags {
			t := &n.Tags[i]
			e := encodeTag(t)
			if e.size > f.slotLen() || t.ForceExternal {
				extOff[t] = heapCursor
				heapCursor += e.size
			}
		}
		walkAlloc(n.Next)
		for _, s := range n.SubIFDs {
			walkAlloc(s)
		}
	}
	walkAlloc(f.root)

	seenEmit := map[*IFD]bool{}
	var walkEmit func(n *IFD)
	walkEmit = func(n *IFD) {
		if n == nil || seenEmit[n] {
			return
		}
		seenEmit[n] = true
		f.emitNode(buf, n, bo, off, extOff)
		walkEmit(n.Next)
		for _, s := range n.SubIFDs {
			walkEmit(s)
		}
	}
	walkEmit(f.root)

	for _, pl := range f.places {
		copy(buf[pl.offset:pl.offset+int64(len(pl.data))], pl.data)
	}
	return buf
}

func (f *File) emitNode(buf []byte, n *IFD, bo binary.ByteOrder, off map[*IFD]int64, extOff map[*Tag]int64) {
	base := off[n]
	if f.big {
		bo.PutUint64(buf[base:], uint64(len(n.Tags)))
	} else {
		bo.PutUint16(buf[base:], uint16(len(n.Tags)))
	}
	es := f.entrySize()
	start := base + f.countLen()
	for i := range n.Tags {
		t := &n.Tags[i]
		e := encodeTag(t)
		ep := start + int64(i)*es
		bo.PutUint16(buf[ep:], t.ID)
		bo.PutUint16(buf[ep+2:], e.typ)
		if f.big {
			bo.PutUint64(buf[ep+4:], e.count)
		} else {
			bo.PutUint32(buf[ep+4:], uint32(e.count))
		}
		slotAt := ep + es - f.slotLen()
		f.writeTagValue(buf, n, t, e, slotAt, bo, off, extOff)
	}
	nextAt := start + int64(len(n.Tags))*es
	var nextPtr int64
	switch {
	case n.NextOffset != nil:
		nextPtr = *n.NextOffset
	case n.Next != nil:
		nextPtr = off[n.Next]
	}
	if f.big {
		bo.PutUint64(buf[nextAt:], uint64(nextPtr))
	} else {
		bo.PutUint32(buf[nextAt:], uint32(nextPtr))
	}
}

func (f *File) writeTagValue(buf []byte, n *IFD, t *Tag, e encoded, slotAt int64, bo binary.ByteOrder, off map[*IFD]int64, extOff map[*Tag]int64) {
	slot := f.slotLen()
	if t.ID == 330 {
		ptrs := make([]uint64, len(n.SubIFDs))
		for i, s := range n.SubIFDs {
			if i < len(n.SubOffsets) {
				ptrs[i] = uint64(n.SubOffsets[i])
			} else {
				ptrs[i] = uint64(off[s])
			}
		}
		f.writePtrArray(buf, t, slotAt, slot, ptrs, bo, extOff)
		return
	}
	if eo, ok := extOff[t]; ok {
		if f.big {
			bo.PutUint64(buf[slotAt:], uint64(eo))
		} else {
			bo.PutUint32(buf[slotAt:], uint32(eo))
		}
		f.encodeInto(buf[eo:eo+e.size], e, bo)
		return
	}
	f.encodeInto(buf[slotAt:slotAt+e.size], e, bo)
}

func (f *File) writePtrArray(buf []byte, t *Tag, slotAt, slot int64, vals []uint64, bo binary.ByteOrder, extOff map[*Tag]int64) {
	ptrTyp := uint16(4)
	if f.big {
		ptrTyp = 16
	}
	s := make([]scalar, len(vals))
	for i, v := range vals {
		s[i] = scalar{u: v}
	}
	e := encoded{typ: ptrTyp, count: uint64(len(vals)), size: int64(len(vals)) * f.ptrLen(), ints: s}
	need := e.size
	if eo, ok := extOff[t]; ok && (need > slot || t.ForceExternal) {
		if f.big {
			bo.PutUint64(buf[slotAt:], uint64(eo))
		} else {
			bo.PutUint32(buf[slotAt:], uint32(eo))
		}
		f.encodeInto(buf[eo:eo+need], e, bo)
		return
	}
	f.encodeInto(buf[slotAt:slotAt+need], e, bo)
}
