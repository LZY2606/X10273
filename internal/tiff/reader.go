package tiff

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"model"
)

// reader is a bounds-checked, endian-aware view over an immutable blob.
type reader struct {
	data []byte
	bo   model.ByteOrder
	boErr error
	order binary.ByteOrder
}

func newReader(data []byte, bo model.ByteOrder) *reader {
	r := &reader{data: data, bo: bo}
	if bo == model.BigEndian {
		r.order = binary.BigEndian
	} else {
		r.order = binary.LittleEndian
	}
	return r
}

func (r *reader) len() int64 { return int64(len(r.data)) }

// in reports whether [off,off+n) is fully inside the blob.
func (r *reader) in(off int64, n int) bool {
	return off >= 0 && n >= 0 && off <= r.len() && r.len()-off >= int64(n)
}

func (r *reader) u16(off int64) (uint16, bool) {
	if !r.in(off, 2) {
		return 0, false
	}
	return r.order.Uint16(r.data[off : off+2]), true
}

func (r *reader) u32(off int64) (uint32, bool) {
	if !r.in(off, 4) {
		return 0, false
	}
	return r.order.Uint32(r.data[off : off+4]), true
}

func (r *reader) u64(off int64) (uint64, bool) {
	if !r.in(off, 8) {
		return 0, false
	}
	return r.order.Uint64(r.data[off : off+8]), true
}

func (r *reader) bytes(off int64, n int) ([]byte, bool) {
	if !r.in(off, n) {
		return nil, false
	}
	return r.data[off : off+n], true
}

func (r *reader) hex(off int64, n int) string {
	b, ok := r.bytes(off, n)
	if !ok {
		// keep whatever tail exists, explicitly marked
		if off < 0 || off >= r.len() {
			return ""
		}
		b = r.data[off:]
	}
	return hex.EncodeToString(b)
}

// decodeValues decodes count values of the given type starting at off.
// It returns the decoded values plus an error flag when the range is OOB.
func (r *reader) decodeValues(off int64, typ uint16, count uint64) ([]interface{}, bool) {
	spec, ok := typeTable[typ]
	if !ok {
		return nil, r.in(off, int(uintSize(typ, count)))
	}
	total := int64(spec.Size) * int64(count)
	inb := r.in(off, int(total))
	values := make([]interface{}, 0, count)
	for i := uint64(0); i < count; i++ {
		p := off + int64(i)*int64(spec.Size)
		switch typ {
		case 1, 6, 7:
			if b, ok := r.bytes(p, 1); ok {
				values = append(values, b[0])
			} else {
				values = append(values, nil)
			}
		case 2:
			if b, ok := r.bytes(p, 1); ok {
				values = append(values, string(b[0]))
			} else {
				values = append(values, nil)
			}
		case 3:
			if v, ok := r.u16(p); ok {
				values = append(values, v)
			} else {
				values = append(values, nil)
			}
		case 4, 13:
			if v, ok := r.u32(p); ok {
				values = append(values, v)
			} else {
				values = append(values, nil)
			}
		case 8:
			if v, ok := r.u16(p); ok {
				values = append(values, int16(v))
			} else {
				values = append(values, nil)
			}
		case 9:
			if v, ok := r.u32(p); ok {
				values = append(values, int32(v))
			} else {
				values = append(values, nil)
			}
		case 5:
			if n, ok1 := r.u32(p); ok1 {
				if d, ok2 := r.u32(p + 4); ok2 {
					if d == 0 {
						values = append(values, nil)
					} else {
						values = append(values, float64(n)/float64(d))
					}
				} else {
					values = append(values, nil)
				}
			} else {
				values = append(values, nil)
			}
		case 10:
			if n, ok1 := r.u32(p); ok1 {
				if d, ok2 := r.u32(p + 4); ok2 {
					if d == 0 {
						values = append(values, nil)
					} else {
						values = append(values, float64(int32(n))/float64(int32(d)))
					}
				} else {
					values = append(values, nil)
				}
			} else {
				values = append(values, nil)
			}
		case 11:
			if b, ok := r.bytes(p, 4); ok {
				bits := r.order.Uint32(b)
				values = append(values, math.Float32frombits(bits))
			} else {
				values = append(values, nil)
			}
		case 12:
			if b, ok := r.bytes(p, 8); ok {
				bits := r.order.Uint64(b)
				values = append(values, math.Float64frombits(bits))
			} else {
				values = append(values, nil)
			}
		case 16:
			if v, ok := r.u64(p); ok {
				values = append(values, v)
			} else {
				values = append(values, nil)
			}
		case 17:
			if v, ok := r.u64(p); ok {
				values = append(values, int64(v))
			} else {
				values = append(values, nil)
			}
		case 18:
			if v, ok := r.u64(p); ok {
				values = append(values, v)
			} else {
				values = append(values, nil)
			}
		}
	}
	return values, inb
}

func uintSize(typ uint16, count uint64) uint64 {
	spec, ok := typeTable[typ]
	if !ok {
		return 0
	}
	return uint64(spec.Size) * count
}
