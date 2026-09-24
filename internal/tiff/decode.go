package tiff

import (
	"encoding/binary"
	"math"
)

func orderOf(bo ByteOrder) binary.ByteOrder {
	if bo == BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// DecodeIntegers decodes integer tag values using the file byte order.
func (f *File) DecodeIntegers(t *Tag) []int64 {
	bo := orderOf(f.Header.ByteOrder)
	step := int64(t.Value.Type.Size())
	if step <= 0 || t.Value.Count <= 0 {
		return nil
	}
	out := make([]int64, 0, t.Value.Count)
	for i := int64(0); i < t.Value.Count; i++ {
		seg := t.Value.Raw[i*step : (i+1)*step]
		switch t.Value.Type {
		case 3:
			out = append(out, int64(bo.Uint16(seg)))
		case 4, 13:
			out = append(out, int64(bo.Uint32(seg)))
		case 16, 18:
			out = append(out, int64(bo.Uint64(seg)))
		case 8:
			out = append(out, int64(int16(bo.Uint16(seg))))
		case 9:
			out = append(out, int64(int32(bo.Uint32(seg))))
		case 17:
			out = append(out, int64(bo.Uint64(seg)))
		default:
			return nil
		}
	}
	return out
}

// DecodeDoubles interprets a tag as IEEE-754 doubles (DOUBLE / FLOAT).
func (f *File) DecodeDoubles(t *Tag) []float64 {
	bo := orderOf(f.Header.ByteOrder)
	switch t.Value.Type {
	case 12:
		out := make([]float64, 0, t.Value.Count)
		for i := int64(0); i < t.Value.Count; i++ {
			bits := bo.Uint64(t.Value.Raw[i*8 : i*8+8])
			out = append(out, math.Float64frombits(bits))
		}
		return out
	case 11:
		out := make([]float64, 0, t.Value.Count)
		for i := int64(0); i < t.Value.Count; i++ {
			bits := bo.Uint32(t.Value.Raw[i*4 : i*4+4])
			out = append(out, float64(math.Float32frombits(bits)))
		}
		return out
	default:
		return nil
	}
}

// DecodeASCII joins the ASCII tag bytes into a string, trimming terminators.
func (f *File) DecodeASCII(t *Tag) (string, bool) {
	if t.Value.Type != 2 {
		return "", false
	}
	b := t.Value.Raw
	for len(b) > 0 && (b[len(b)-1] == 0 || b[len(b)-1] == '|') {
		b = b[:len(b)-1]
	}
	return string(b), true
}

// TagByID returns the first tag with id in ifd, if present.
func (d *IFD) TagByID(id TagID) *Tag {
	for _, t := range d.Tags {
		if t.ID == id {
			return t
		}
	}
	return nil
}
