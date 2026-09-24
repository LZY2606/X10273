package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
)

type reader struct {
	data []byte
	bo   binary.ByteOrder
}

func (r *reader) errAt(offset uint64, length uint64, what string) error {
	if offset > uint64(len(r.data)) || length > uint64(len(r.data))-offset {
		return fmt.Errorf("%s: range [%d,%d) exceeds file size %d", what, offset, offset+length, len(r.data))
	}
	return nil
}

func (r *reader) u16(at uint64) (uint16, error) {
	if err := r.errAt(at, 2, "uint16"); err != nil {
		return 0, err
	}
	return r.bo.Uint16(r.data[at : at+2]), nil
}

func (r *reader) u32(at uint64) (uint32, error) {
	if err := r.errAt(at, 4, "uint32"); err != nil {
		return 0, err
	}
	return r.bo.Uint32(r.data[at : at+4]), nil
}

func (r *reader) u64(at uint64) (uint64, error) {
	if err := r.errAt(at, 8, "uint64"); err != nil {
		return 0, err
	}
	return r.bo.Uint64(r.data[at : at+8]), nil
}

// decodeValues decodes count items of an IFD type into integers and/or
// floats. Signed types are normalized to uint64 bit patterns only for the
// integer display path; floats land in DecodedFloat.
func (r *reader) decodeValues(typ uint16, count uint64, raw []byte) (ints []uint64, flts []float64, ascii string, err error) {
	size := uint64(TypeSize[typ])
	if typ == 0 || int(typ) >= len(TypeSize) || size == 0 {
		return nil, nil, "", fmt.Errorf("unknown IFD type %d", typ)
	}
	if uint64(len(raw)) < size*count {
		return nil, nil, "", fmt.Errorf("type %d needs %d bytes, have %d", typ, size*count, len(raw))
	}
	pos := uint64(0)
	for i := uint64(0); i < count; i++ {
		chunk := raw[pos : pos+size]
		switch typ {
		case TypeByte, TypeUndefined:
			ints = append(ints, uint64(chunk[0]))
		case TypeSByte:
			ints = append(ints, uint64(int8(chunk[0])))
		case TypeASCII:
			// handled below once
		case TypeShort:
			ints = append(ints, uint64(r.bo.Uint16(chunk)))
		case TypeSShort:
			ints = append(ints, uint64(int16(r.bo.Uint16(chunk))))
		case TypeLong, TypeIFD:
			ints = append(ints, uint64(r.bo.Uint32(chunk)))
		case TypeSLong:
			ints = append(ints, uint64(int32(r.bo.Uint32(chunk))))
		case TypeLong8, TypeIFD8, TypeSLong8:
			ints = append(ints, r.bo.Uint64(chunk))
		case TypeRational:
			num := r.bo.Uint32(chunk[0:4])
			den := r.bo.Uint32(chunk[4:8])
			if den != 0 {
				flts = append(flts, float64(num)/float64(den))
			} else {
				flts = append(flts, math.NaN())
			}
			ints = append(ints, uint64(num), uint64(den))
		case TypeSRational:
			num := int32(r.bo.Uint32(chunk[0:4]))
			den := int32(r.bo.Uint32(chunk[4:8]))
			if den != 0 {
				flts = append(flts, float64(num)/float64(den))
			} else {
				flts = append(flts, math.NaN())
			}
			ints = append(ints, uint64(num), uint64(den))
		case TypeFloat:
			bits := r.bo.Uint32(chunk)
			flts = append(flts, float64(math.Float32frombits(bits)))
		case TypeDouble:
			bits := r.bo.Uint64(chunk)
			flts = append(flts, math.Float64frombits(bits))
		}
		pos += size
	}
	if typ == TypeASCII {
		// Preserve the raw bytes; trim the single NUL terminator TIFF uses.
		b := raw[:size*count]
		for len(b) > 0 && b[len(b)-1] == 0 {
			b = b[:len(b)-1]
		}
		ascii = string(b)
	}
	return ints, flts, ascii, nil
}
