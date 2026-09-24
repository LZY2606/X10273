package tiff

import (
	"encoding/binary"
	"math"
)

func endianBo(o Order) binary.ByteOrder {
	if o == BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// decodeValues derives values from preserved raw bytes. Decoding failures are
// reported on the tag rather than dropping the raw representation.
func decodeValues(t *RawTag, data []byte, o Order) {
	if len(data) == 0 {
		return
	}
	bo := endianBo(o)
	ts := t.Type.Size()
	if ts == 0 {
		t.Unread = "" // raw bytes preserved, type unknown
		return
	}
	if t.Type == TASCII {
		s := string(data)
		if len(s) > 0 && s[len(s)-1] == 0 {
			s = s[:len(s)-1]
		}
		t.Values = []any{s}
		return
	}
	if t.Type == TUNDEFINED || t.Type == TBYTE || t.Type == TSBYTE {
		t.Values = []any{data}
		return
	}
	n := int(t.Count)
	if n*ts > len(data) {
		t.Unread = "value bytes shorter than declared count"
		return
	}
	vals := make([]any, 0, n)
	for i := 0; i < n; i++ {
		b := data[i*ts : (i+1)*ts]
		var v any
		switch t.Type {
		case TSHORT:
			v = uint64(bo.Uint16(b))
		case TSSHORT:
			v = int64(int16(bo.Uint16(b)))
		case TLONG:
			v = uint64(bo.Uint32(b))
		case TSLONG:
			v = int64(int32(bo.Uint32(b)))
		case TLONG8, TIFD8:
			v = bo.Uint64(b)
		case TSLONG8:
			v = int64(bo.Uint64(b))
		case TRATIONAL:
			v = Fraction{Num: bo.Uint32(b[:4]), Den: bo.Uint32(b[4:])}
		case TSRATIONAL:
			v = SFraction{Num: int64(int32(bo.Uint32(b[:4]))), Den: int64(int32(bo.Uint32(b[4:])))}
		case TFLOAT:
			v = math.Float32frombits(bo.Uint32(b))
		case TDOUBLE:
			v = math.Float64frombits(bo.Uint64(b))
		}
		vals = append(vals, v)
	}
	t.Values = vals
}
