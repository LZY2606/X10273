package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// TagSpec describes one IFD entry for the deterministic builder.
type TagSpec struct {
	Code          uint16
	Type          uint16 // 0 = infer SHORT/LONG from Values
	Values        []uint64
	Floats        []float64
	Text          string
	Refs          []int   // IFD indexes whose offsets become the value (e.g. SubIFDs)
	ForceExternal bool    // store value externally even if it fits inline
	ExternalAt    *uint64 // write this offset into the field instead of the real one
}

// IFDSpec describes one directory. Next is the index of the next IFD in
// the chain (-1 for none); arbitrary graphs including cycles are allowed.
type IFDSpec struct {
	Tags []TagSpec
	Next int
}

// FileSpec is a deterministic TIFF/BigTIFF recipe.
type FileSpec struct {
	Endian   Endianness
	Big      bool
	IFDs     []*IFDSpec
	FileSize uint64 // pad the file to at least this size with fill bytes
}

// Build serializes a FileSpec into bytes. Layout is deterministic:
// header, IFD tables in spec order, then external tag values in the
// order encountered. Pixel bytes are not synthesized; tile/strip ranges
// point into the zero-filled padding region.
func Build(spec *FileSpec) []byte {
	big := spec.Big
	var order binary.ByteOrder = binary.LittleEndian
	if spec.Endian == BigEndian {
		order = binary.BigEndian
	}
	headSize := uint64(2)
	entrySize := uint64(12)
	fieldSize := uint64(4)
	headerSize := uint64(8)
	if big {
		headSize, entrySize, fieldSize, headerSize = 8, 20, 8, 16
	}

	// Pass 1: assign IFD offsets.
	ifdOff := make([]uint64, len(spec.IFDs))
	cursor := headerSize
	for i, s := range spec.IFDs {
		ifdOff[i] = cursor
		cursor += headSize + uint64(len(s.Tags))*entrySize + fieldSize
	}

	// Pass 2: assign external value offsets.
	type extKey struct{ ifd, tag int }
	extOff := map[extKey]uint64{}
	for i, s := range spec.IFDs {
		for j, t := range s.Tags {
			raw := tagRawValue(&t, ifdOff, order)
			fs := fieldSize
			if uint64(len(raw)) > fs || t.ForceExternal {
				if t.ExternalAt == nil {
					extOff[extKey{i, j}] = cursor
					cursor += uint64(len(raw))
				}
			}
		}
	}

	total := cursor
	if spec.FileSize > total {
		total = spec.FileSize
	}
	buf := make([]byte, total)

	// Header.
	if spec.Endian == BigEndian {
		copy(buf[0:2], "MM")
	} else {
		copy(buf[0:2], "II")
	}
	if big {
		order.PutUint16(buf[2:4], 43)
		order.PutUint16(buf[4:6], 8)
		order.PutUint16(buf[6:8], 0)
		order.PutUint64(buf[8:16], ifdOff[0])
	} else {
		order.PutUint16(buf[2:4], 42)
		order.PutUint32(buf[4:8], uint32(ifdOff[0]))
	}

	// IFD tables.
	for i, s := range spec.IFDs {
		base := ifdOff[i]
		if big {
			order.PutUint64(buf[base:], uint64(len(s.Tags)))
		} else {
			order.PutUint16(buf[base:], uint16(len(s.Tags)))
		}
		// Sort tags by code, as real writers do.
		idx := make([]int, len(s.Tags))
		for k := range idx {
			idx[k] = k
		}
		sort.SliceStable(idx, func(a, b int) bool { return s.Tags[idx[a]].Code < s.Tags[idx[b]].Code })
		for pos, j := range idx {
			t := s.Tags[j]
			entry := base + headSize + uint64(pos)*entrySize
			raw := tagRawValue(&t, ifdOff, order)
			typ := t.Type
			if typ == 0 {
				typ = inferType(&t)
			}
			order.PutUint16(buf[entry:], t.Code)
			order.PutUint16(buf[entry+2:], typ)
			var count uint64
			switch typ {
			case 2:
				count = uint64(len(t.Text) + 1)
			case 12:
				count = uint64(len(t.Floats))
			default:
				count = uint64(len(t.Values) + len(t.Refs))
			}
			fieldOff := entry + 8
			if big {
				order.PutUint64(buf[entry+4:], count)
				fieldOff = entry + 12
			} else {
				order.PutUint32(buf[entry+4:], uint32(count))
			}
			if uint64(len(raw)) <= fieldSize && !t.ForceExternal {
				copy(buf[fieldOff:], raw)
			} else {
				var off uint64
				if t.ExternalAt != nil {
					off = *t.ExternalAt
				} else {
					off = extOff[extKey{i, j}]
					copy(buf[off:], raw)
				}
				if big {
					order.PutUint64(buf[fieldOff:], off)
				} else {
					order.PutUint32(buf[fieldOff:], uint32(off))
				}
			}
		}
		nextOff := base + headSize + uint64(len(s.Tags))*entrySize
		var next uint64
		if s.Next >= 0 {
			next = ifdOff[s.Next]
		}
		if big {
			order.PutUint64(buf[nextOff:], next)
		} else {
			order.PutUint32(buf[nextOff:], uint32(next))
		}
	}
	return buf
}

func inferType(t *TagSpec) uint16 {
	if t.Text != "" {
		return 2
	}
	if len(t.Floats) > 0 {
		return 12
	}
	if len(t.Refs) > 0 {
		return 4
	}
	for _, v := range t.Values {
		if v > 0xFFFF {
			return 4
		}
	}
	return 3
}

// tagRawValue renders the value bytes of a tag in file byte order.
func tagRawValue(t *TagSpec, ifdOff []uint64, order binary.ByteOrder) []byte {
	typ := t.Type
	if typ == 0 {
		typ = inferType(t)
	}
	var out []byte
	put := func(b []byte) { out = append(out, b...) }
	switch typ {
	case 1, 6, 7:
		for _, v := range t.Values {
			out = append(out, byte(v))
		}
	case 2:
		out = append(out, []byte(t.Text)...)
		out = append(out, 0)
	case 3, 8:
		for _, v := range t.Values {
			b := make([]byte, 2)
			order.PutUint16(b, uint16(v))
			put(b)
		}
	case 4, 9:
		vals := t.Values
		if len(t.Refs) > 0 {
			vals = nil
			for _, r := range t.Refs {
				if r < 0 || r >= len(ifdOff) {
					panic(fmt.Sprintf("bad IFD ref %d", r))
				}
				vals = append(vals, ifdOff[r])
			}
		}
		for _, v := range vals {
			b := make([]byte, 4)
			order.PutUint32(b, uint32(v))
			put(b)
		}
	case 16, 17, 18:
		for _, v := range t.Values {
			b := make([]byte, 8)
			order.PutUint64(b, v)
			put(b)
		}
	case 12:
		for _, v := range t.Floats {
			b := make([]byte, 8)
			order.PutUint64(b, math.Float64bits(v))
			put(b)
		}
	case 11:
		for _, v := range t.Floats {
			b := make([]byte, 4)
			order.PutUint32(b, math.Float32bits(float32(v)))
			put(b)
		}
	default:
		panic(fmt.Sprintf("builder: unsupported type %d", typ))
	}
	return out
}
