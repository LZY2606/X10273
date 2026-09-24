package tiff

import (
	"encoding/binary"
	"math"
)

// BTag is one directory entry specification for the deterministic builder.
type BTag struct {
	ID     int
	Type   int
	Values []uint64 // rational/string handled via Raw/Rats/S
	S      string
	Rats   [][2]uint32
	Doubles []float64
	Raw    []byte
}

// BIFD is a directory under construction. Sub are indexes into the same build.
type BIFD struct {
	Tags []BTag
	Next *int // index of next IFD
	Subs []int
}

// BuildSpec is a fully deterministic small TIFF file.
type BuildSpec struct {
	Little bool
	Big    bool
	IFDs   []BIFD
	// Pixel bytes keyed by "ifd:block".
	Pixels map[string][]byte
	// Wide places pixel regions for named blocks at >= 2^32 (BigTIFF only).
	WideBlocks map[string]bool
}

// Built is the resulting file plus provenance offsets.
type Built struct {
	Normal []byte
	Region []byte
	RegionStart int64
	Offsets map[string]int64 // e.g. "ifd0", "ifd0:tag324", "ifd0:block0"
	Variant string
	Little bool
}

const wideStart int64 = 1 << 32

type buildPlanner struct {
	spec   *BuildSpec
	bo     binary.ByteOrder
	big    bool
	esize  int
	eslot  int
	hsize  int // header size
}

// Build serialises a deterministic TIFF. Layout order is fixed:
// header, IFD0..n (entries+next), external tag values, normal pixel blocks,
// then optionally a sparse wide region for >=2^32 offsets.
func Build(spec BuildSpec) *Built {
	p := &buildPlanner{spec: &spec, bo: orderBo(spec.Little), big: spec.Big}
	if spec.Big {
		p.esize, p.eslot, p.hsize = 20, 8, 16
	} else {
		p.esize, p.eslot, p.hsize = 12, 4, 8
	}

	type ext struct {
		ifd, tag int
		data     []byte
		wide     bool
	}
	type px struct {
		key  string
		data []byte
		wide bool
	}
	// 1. IFD sizes and positions (contiguous).
	pos := int64(p.hsize)
	ifdPos := make([]int64, len(spec.IFDs))
	ifdLen := make([]int64, len(spec.IFDs))
	for i := range spec.IFDs {
		cnt := int64(len(spec.IFDs[i].Tags))
		cntSz := int64(2)
		ptrSz := int64(4)
		if spec.Big {
			cntSz, ptrSz = 8, 8
		}
		l := cntSz + cnt*int64(p.esize) + ptrSz
		ifdPos[i] = pos
		ifdLen[i] = l
		pos += l
	}
	// 2. external tag allocations
	var exts []ext
	extPos := map[string]int64{}
	for i := range spec.IFDs {
		for _, t := range spec.IFDs[i].Tags {
			data := p.tagBytes(t)
			if int64(len(data)) > int64(p.eslot) {
				key := extKey(i, t.ID)
				wide := false
				if spec.WideBlocks[key] {
					wide = true
				}
				if !wide {
					off := pos
					off = align2(off)
					extPos[key] = off
					pos = off + int64(len(data))
				}
				exts = append(exts, ext{ifd: i, tag: t.ID, data: data, wide: wide})
			}
		}
	}
	// 3. normal pixel blocks
	var pixels []px
	pxPos := map[string]int64{}
	for key, data := range spec.Pixels {
		wide := spec.WideBlocks[key]
		if !wide {
			off := align2(pos)
			pxPos[key] = off
			pos = off + int64(len(data))
		}
		pixels = append(pixels, px{key: key, data: data, wide: wide})
	}
	normalEnd := pos
	// 4. wide region offsets
	var region []byte
	widePos := map[string]int64{}
	addWide := func(data []byte, key string) int64 {
		off := wideStart + int64(len(region))
		widePos[key] = off
		region = append(region, data...)
		return off
	}
	for _, e := range exts {
		if e.wide {
			addWide(e.data, extKey(e.ifd, e.tag))
		}
	}
	for _, pp := range pixels {
		if pp.wide {
			addWide(pp.data, pp.key)
		}
	}

	normal := make([]byte, normalEnd)
	// header
	if spec.Little {
		normal[0], normal[1] = 'I', 'I'
	} else {
		normal[0], normal[1] = 'M', 'M'
	}
	if spec.Big {
		p.bo.PutUint16(normal[2:4], 43)
		p.bo.PutUint16(normal[4:6], 8)
		p.bo.PutUint16(normal[6:8], 0)
		p.bo.PutUint64(normal[8:16], uint64(ifdPos[0]))
	} else {
		p.bo.PutUint16(normal[2:4], 42)
		p.bo.PutUint32(normal[4:8], uint32(ifdPos[0]))
	}
	out := &Built{Offsets: map[string]int64{}, Variant: map[bool]string{true: "bigtiff", false: "classic"}[spec.Big], Little: spec.Little}
	if len(region) > 0 {
		out.Region = region
		out.RegionStart = wideStart
	}
	for i, off := range ifdPos {
		out.Offsets[ifdKey(i)] = off
	}
	for k, v := range extPos {
		out.Offsets[k] = v
	}
	for k, v := range widePos {
		out.Offsets[k] = v
	}
	for k, v := range pxPos {
		out.Offsets[k] = v
	}

	// 5. write IFDs
	for i := range spec.IFDs {
		base := ifdPos[i]
		cntSz := 2
		if spec.Big {
			cntSz = 8
			p.bo.PutUint64(normal[base:base+8], uint64(len(spec.IFDs[i].Tags)))
		} else {
			p.bo.PutUint16(normal[base:base+2], uint16(len(spec.IFDs[i].Tags)))
		}
		eb := base + int64(cntSz)
		for j, t := range spec.IFDs[i].Tags {
			e := eb + int64(j)*int64(p.esize)
			data := p.tagBytes(t)
			p.bo.PutUint16(normal[e:e+2], uint16(t.ID))
			p.bo.PutUint16(normal[e+2:e+4], uint16(t.Type))
			if spec.Big {
				p.bo.PutUint64(normal[e+4:e+12], uint64(p.countOf(t)))
			} else {
				p.bo.PutUint32(normal[e+4:e+8], uint32(p.countOf(t)))
			}
			slotAt := e + int64(p.eslot)
			slot := normal[slotAt : slotAt+int64(p.eslot)]
			if int64(len(data)) <= int64(p.eslot) {
				copy(slot, data)
			} else {
				off := extPos[extKey(i, t.ID)]
				if off == 0 {
					off = widePos[extKey(i, t.ID)]
				}
				if spec.Big {
					p.bo.PutUint64(slot, uint64(off))
				} else {
					p.bo.PutUint32(slot, uint32(off))
				}
				dst := p.destFor(normal, region, off, len(data))
				copy(dst, data)
			}
		}
		nextAt := ifdPos[i] + ifdLen[i] - map[bool]int64{true: 8, false: 4}[spec.Big]
		var nextOff int64
		if spec.IFDs[i].Next != nil {
			nextOff = ifdPos[*spec.IFDs[i].Next]
		}
		if spec.Big {
			p.bo.PutUint64(normal[nextAt:nextAt+8], uint64(nextOff))
		} else {
			p.bo.PutUint32(normal[nextAt:nextAt+4], uint32(nextOff))
		}
	}
	// 6. normal pixels
	for _, pp := range pixels {
		if pp.wide {
			continue
		}
		off := pxPos[pp.key]
		copy(normal[off:off+int64(len(pp.data))], pp.data)
	}
	out.Normal = normal
	return out
}

func (p *buildPlanner) destFor(normal, region []byte, off int64, n int) []byte {
	if off >= wideStart {
		return region[off-wideStart : off-wideStart+int64(n)]
	}
	return normal[off : off+int64(n)]
}

func extKey(ifd, tag int) string { return ifdTagKey(ifd, tag) }
func ifdKey(i int) string       { return "ifd" + itoa(i) }
func ifdTagKey(i, t int) string { return "ifd" + itoa(i) + ":tag" + itoa(t) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	var b [20]byte
	n := len(b)
	for i != 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

func align2(off int64) int64 { return (off + 1) &^ 1 }

func (p *buildPlanner) countOf(t BTag) int64 {
	switch t.Type {
	case 2:
		return int64(len(t.S)) + 1
	case 5, 10:
		return int64(len(t.Rats))
	case 12:
		return int64(len(t.Doubles))
	default:
		return int64(len(t.Values))
	}
}

// tagBytes encodes values in the file's byte order.
func (p *buildPlanner) tagBytes(t BTag) []byte {
	sz := typeSizesClassic[t.Type]
	if p.big {
		sz = typeSizesBig[t.Type]
	}
	switch t.Type {
	case 2:
		return append([]byte(t.S), 0)
	case 5:
		b := make([]byte, 8*len(t.Rats))
		for i, r := range t.Rats {
			p.bo.PutUint32(b[i*8:i*8+4], r[0])
			p.bo.PutUint32(b[i*8+4:i*8+8], r[1])
		}
		return b
	case 12:
		b := make([]byte, 8*len(t.Doubles))
		for i, d := range t.Doubles {
			p.bo.PutUint64(b[i*8:i*8+8], math.Float64bits(d))
		}
		return b
	case 7, 1, 6:
		if t.Raw != nil {
			return t.Raw
		}
		b := make([]byte, len(t.Values))
		for i, v := range t.Values {
			b[i] = byte(v)
		}
		return b
	default:
		b := make([]byte, sz*len(t.Values))
		for i, v := range t.Values {
			switch sz {
			case 2:
				p.bo.PutUint16(b[i*2:i*2+2], uint16(v))
			case 4:
				p.bo.PutUint32(b[i*4:i*4+4], uint32(v))
			case 8:
				p.bo.PutUint64(b[i*8:i*8+8], v)
			}
		}
		return b
	}
}

// AsSource wraps a built file in the appropriate Source.
func (b *Built) AsSource(id string) Source {
	if len(b.Region) > 0 {
		return &WideSource{Normal: b.Normal, RegionStart: b.RegionStart, Region: b.Region, IDv: id}
	}
	return &BytesSource{B: b.Normal, IDv: id}
}
