package tiff

import "encoding/binary"

// Value is one tag value to place in an IFD.
type Value struct {
	Tag uint16
	// Type and the typed slices encode normal values.
	Type    DataType
	Uints   []uint64
	Sints   []int64
	Doubles []float64
	Ascii   string
	// Raw overrides the typed payload and the type/count header fields. It is
	// used for malformed fixtures (for example a wide slot pointing far beyond
	// EOF, or an unsupported type code).

	Raw      []byte
	RawType  DataType
	RawCount uint64
	// External forces the value payload out of the inline slot. ExternalAt>=0
	// pins the payload offset; -1 lets layout append it after the IFDs. A Raw
	// slot pointing beyond EOF is intentionally not backed by file bytes.
	External   bool
	ExternalAt int64
}

// IFDSpec builds one directory.
type IFDSpec struct {
	NextIndex int   // index of next IFD, -1 for none
	NextSelf  bool  // next pointer targets this IFD (self cycle)
	Subs      []int // indices reached through tag 330
	Values    []Value
}

// Spec is a deterministic TIFF file layout request.
type Spec struct {
	Order Order
	Big   bool
	IFDs  []IFDSpec
	Extra []byte // opaque trailing bytes
}

// Build serializes the spec into TIFF bytes. Placement is deterministic:
// header, IFD directories in declared order, then external payloads in
// (IFD index, value index) order, then Extra.
func Build(s Spec) []byte {
	bo := s.Order.ByteOrder()
	n := len(s.IFDs)
	var offBytes, headerLen, countLen, entrySize int64 = 4, 8, 2, 12
	if s.Big {
		offBytes, headerLen, countLen, entrySize = 8, 16, 8, 20
	}

	type place struct {
		off, end int64
		valAt    []int64
	}
	places := make([]place, n)
	cursor := headerLen
	for i := range s.IFDs {
		body := countLen + int64(len(s.IFDs[i].Values))*entrySize + offBytes
		p := place{off: cursor, end: cursor + body, valAt: make([]int64, len(s.IFDs[i].Values))}
		at := cursor + countLen
		for j := range s.IFDs[i].Values {
			p.valAt[j] = at
			at += entrySize
		}
		places[i] = p
		cursor += body
	}

	type external struct {
		ifd, val int
		at       int64
		payload  []byte
		beyond   bool // intentionally beyond EOF
	}
	exts := map[[2]int]*external{}
	var ordered [][2]int
	for i := range s.IFDs {
		for j := range s.IFDs[i].Values {
			v := &s.IFDs[i].Values[j]
			payload := encodeValue(&s.IFDs[i].Values[j], bo, s.Big)
			typ, count := effectiveTypeCount(v)
			totalLen := int64(typ.Size(s.Big)) * int64(count)
			if !v.External && v.Raw == nil && totalLen <= offBytes {
				continue
			}
			key := [2]int{i, j}
			e := &external{ifd: i, val: j, payload: payload}
			if v.ExternalAt >= 0 {
				e.at = v.ExternalAt
				if e.at+int64(len(payload)) > cursor {
					e.beyond = true
				}
			} else {
				e.at = cursor
				cursor += int64(len(payload))
			}
			exts[key] = e
			ordered = append(ordered, key)
		}
	}
	root := int64(0)
	if n > 0 {
		root = places[0].off
	}
	total := cursor + int64(len(s.Extra))
	buf := make([]byte, total)
	if s.Order == LittleEndian {
		buf[0], buf[1] = 'I', 'I'
	} else {
		buf[0], buf[1] = 'M', 'M'
	}
	if !s.Big {
		bo.PutUint16(buf[2:4], 42)
		bo.PutUint32(buf[4:8], uint32(root))
	} else {
		bo.PutUint16(buf[2:4], 43)
		bo.PutUint16(buf[4:6], 8)
		bo.PutUint16(buf[6:8], 0)
		bo.PutUint64(buf[8:16], uint64(root))
	}
	for i := range s.IFDs {
		p := places[i]
		if !s.Big {
			bo.PutUint16(buf[p.off:p.off+2], uint16(len(s.IFDs[i].Values)))
		} else {
			bo.PutUint64(buf[p.off:p.off+8], uint64(len(s.IFDs[i].Values)))
		}
		for j := range s.IFDs[i].Values {
			v := &s.IFDs[i].Values[j]
			at := p.valAt[j]
			typ, count := effectiveTypeCount(v)
			bo.PutUint16(buf[at:at+2], v.Tag)
			bo.PutUint16(buf[at+2:at+4], uint16(typ))
			if !s.Big {
				bo.PutUint32(buf[at+4:at+8], uint32(count))
			} else {
				bo.PutUint64(buf[at+4:at+12], count)
			}
			slotAt := at + 8
			if s.Big {
				slotAt = at + 12
			}
			if e, ok := exts[[2]int{i, j}]; ok {
				if v.Raw != nil {
					copy(buf[slotAt:slotAt+offBytes], v.Raw)
				} else {
					putOffset(bo, buf[slotAt:slotAt+offBytes], uint64(e.at), s.Big)
				}
				if !e.beyond && e.at >= 0 && e.at+int64(len(e.payload)) <= int64(len(buf)) {
					copy(buf[e.at:], e.payload)
				}
			} else {
				copy(buf[slotAt:], e.payload)
			}
		}
		nextAt := p.end - offBytes
		var np int64
		switch {
		case s.IFDs[i].NextSelf:
			np = p.off
		case s.IFDs[i].NextIndex >= 0 && s.IFDs[i].NextIndex < n:
			np = places[s.IFDs[i].NextIndex].off
		}
		putOffset(bo, buf[nextAt:nextAt+offBytes], uint64(np), s.Big)
	}
	copy(buf[cursor:], s.Extra)
	return buf
}

func effectiveTypeCount(v *Value) (DataType, uint64) {
	if v.Raw != nil {
		return v.RawType, v.RawCount
	}
	return v.Type, valueCount(v)
}

func putOffset(bo binary.ByteOrder, p []byte, v uint64, big bool) {
	if big {
		bo.PutUint64(p, v)
	} else {
		bo.PutUint32(p, uint32(v))
	}
}

func valueCount(v *Value) uint64 {
	switch v.Type {
	case TypeASCII:
		return uint64(len(v.Ascii)) + 1
	case TypeDouble:
		return uint64(len(v.Doubles))
	case TypeRational, TypeSRational:
		return uint64(len(v.Doubles))
	default:
		switch {
		case len(v.Uints) > 0:
			return uint64(len(v.Uints))
		case len(v.Sints) > 0:
			return uint64(len(v.Sints))
		default:
			return 1
		}
	}
}

func encodeValue(v *Value, bo binary.ByteOrder, big bool) []byte {
	if v.Raw != nil {
		return v.Raw
	}
	count := valueCount(v)
	ts := v.Type.Size(big)
	b := make([]byte, int(ts)*int(count))
	switch v.Type {
	case TypeShort, TypeSShort:
		for i, x := range v.Uints {
			bo.PutUint16(b[2*i:], uint16(x))
		}
	case TypeLong, TypeSLong, TypeIFD, TypeLong8, TypeSLong8, TypeIFD8:
		for i, x := range v.Uints {
			if ts == 8 {
				bo.PutUint64(b[8*i:], x)
			} else {
				bo.PutUint32(b[4*i:], uint32(x))
			}
		}
	case TypeByte, TypeSByte, TypeUndefined:
		for i, x := range v.Uints {
			b[i] = byte(x)
		}
	case TypeDouble:
		for i, x := range v.Doubles {
			bo.PutUint64(b[8*i:], float64bits(x))
		}
	case TypeRational, TypeSRational:
		for i := range v.Doubles {
			num, den := rationals(v.Uints, i)
			bo.PutUint32(b[8*i:], uint32(num))
			bo.PutUint32(b[8*i+4:], uint32(den))
		}
	case TypeASCII:
		copy(b, v.Ascii)
	}
	return b
}

func rationals(u []uint64, i int) (uint64, uint64) {
	if 2*i+1 < len(u) {
		return u[2*i], u[2*i+1]
	}
	return 0, 1
}
