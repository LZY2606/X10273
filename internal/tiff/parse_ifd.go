package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
)

func (r *reader) u16(at int64) (uint16, bool) {
	if at < 0 || int64(len(r.b)) < at+2 {
		return 0, false
	}
	return r.bo.Uint16(r.b[at : at+2]), true
}

func (r *reader) u32(at int64) (uint32, bool) {
	if at < 0 || int64(len(r.b)) < at+4 {
		return 0, false
	}
	return r.bo.Uint32(r.b[at : at+4]), true
}

func (r *reader) u64(at int64) (uint64, bool) {
	if at < 0 || int64(len(r.b)) < at+8 {
		return 0, false
	}
	return r.bo.Uint64(r.b[at : at+8]), true
}

func (r *reader) bytes(at, n int64) ([]byte, bool) {
	if at < 0 || n < 0 || at+n > int64(len(r.b)) || at+n < at {
		return nil, false
	}
	return r.b[at : at+n], true
}

// parseIFD reads one directory. It returns (nil,false) only when even the
// entry count cannot be read. Partial directories are still returned.
func (r *reader) parseIFD(off int64) (*IFD, bool) {
	ifd := &IFD{Offset: off, ByTag: map[uint16]*Entry{}}
	var count uint64
	var entrySize, nextSize int64
	if !r.big {
		c, ok := r.u16(off)
		if !ok {
			r.diag(Diagnostic{Code: "ifd_count_truncated", Severity: SevErr,
				Message: fmt.Sprintf("IFD entry count unreadable @%d", off), AtOffset: off})
			return nil, false
		}
		count = uint64(c)
		entrySize = 12
		nextSize = 4
	} else {
		c, ok := r.u64(off)
		if !ok {
			r.diag(Diagnostic{Code: "ifd_count_truncated", Severity: SevErr,
				Message: fmt.Sprintf("BigTIFF IFD entry count unreadable @%d", off), AtOffset: off})
			return nil, false
		}
		count = c
		entrySize = 20
		nextSize = 8
	}
	ifd.ByteStart = off
	entriesStart := off + r.countBytes()
	totalBytes := int64(count)*entrySize + nextSize
	if count > uint64(len(r.b)) {
		r.diag(Diagnostic{Code: "ifd_count_overflow", Severity: SevErr,
			Message:  fmt.Sprintf("IFD@%d declares %d entries beyond file capacity", off, count),
			AtOffset: off})
	}
	ifd.ByteEnd = entriesStart + totalBytes
	for i := uint64(0); i < count; i++ {
		entryAt := entriesStart + int64(i)*entrySize
		e := r.parseEntry(entryAt, off)
		if e == nil {
			r.diag(Diagnostic{Code: "entry_truncated", Severity: SevErr,
				Message: fmt.Sprintf("IFD@%d entry %d unreadable", off, i), AtOffset: entryAt, IFD: off})
			ifd.ByteEnd = entryAt + entrySize + nextSize
			break
		}
		ifd.Entries = append(ifd.Entries, e)
		ifd.ByTag[e.Tag] = e
		if e.Tag == TagSubIFDs {
			ifd.SubOffsets = append(ifd.SubOffsets, e.IFDLinks...)
		}
	}
	nextAt := entriesStart + int64(count)*entrySize
	if r.big {
		v, ok := r.u64(nextAt)
		if ok {
			ifd.NextOffset = int64(v)
			ifd.HasNext = v != 0
		} else {
			r.diag(Diagnostic{Code: "next_ifd_truncated", Severity: SevErr, IFD: off,
				Message: fmt.Sprintf("next IFD pointer unreadable @%d", nextAt), AtOffset: nextAt})
		}
	} else {
		v, ok := r.u32(nextAt)
		if ok {
			ifd.NextOffset = int64(v)
			ifd.HasNext = v != 0
		} else {
			r.diag(Diagnostic{Code: "next_ifd_truncated", Severity: SevErr, IFD: off,
				Message: fmt.Sprintf("next IFD pointer unreadable @%d", nextAt), AtOffset: nextAt})
		}
	}
	if nextAt+nextSize > ifd.ByteEnd {
		ifd.ByteEnd = nextAt + nextSize
	}
	return ifd, true
}

func (r *reader) countBytes() int64 {
	if r.big {
		return 8
	}
	return 2
}

func (r *reader) parseEntry(at, ifdOff int64) *Entry {
	tag, ok1 := r.u16(at)
	if !ok1 {
		return nil
	}
	var typ uint16
	var count uint64
	var valueAt int64
	if !r.big {
		t, ok := r.u16(at + 2)
		if !ok {
			return nil
		}
		typ = t
		c, ok := r.u32(at + 4)
		if !ok {
			return nil
		}
		count = uint64(c)
		valueAt = at + 8
	} else {
		t, ok := r.u16(at + 2)
		if !ok {
			return nil
		}
		typ = t
		c, ok := r.u64(at + 4)
		if !ok {
			return nil
		}
		count = c
		valueAt = at + 12
	}
	e := &Entry{Tag: tag, Type: DataType(typ), Count: count, SlotOffset: valueAt,
		SlotEnd: valueAt + int64(r.offBytes)}
	typeSize := e.Type.Size(r.big)
	if typeSize == 0 {
		e.TypeValid = false
		raw, _ := r.bytes(valueAt, int64(r.offBytes))
		e.InlineRaw = raw
		r.diag(Diagnostic{Code: "unknown_type", Severity: SevWarn, IFD: ifdOff, Tag: tag,
			Message:  fmt.Sprintf("tag %d uses unknown type %d (raw slot retained)", tag, typ),
			AtOffset: valueAt})
		return e
	}
	e.TypeValid = true
	total, overflow := mulCheck(typeSize, count)
	if overflow || total > uint64(math.MaxInt64) {
		raw, _ := r.bytes(valueAt, int64(r.offBytes))
		e.InlineRaw = raw
		r.diag(Diagnostic{Code: "value_count_overflow", Severity: SevErr, IFD: ifdOff, Tag: tag,
			Message: fmt.Sprintf("tag %d count %d overflows", tag, count), AtOffset: valueAt})
		return e
	}
	slot := int64(r.offBytes)
	var inline []byte
	if total <= uint64(slot) {
		inline, _ = r.bytes(valueAt, int64(total))
		e.InlineRaw = append([]byte(nil), inline...)
	} else {
		// external: value slot holds the wide offset, preserving raw bytes.
		slotRaw, _ := r.bytes(valueAt, slot)
		e.InlineRaw = append([]byte(nil), slotRaw...)
		e.External = true
		var off uint64
		if r.big {
			off, _ = r.u64(valueAt)
		} else {
			v, _ := r.u32(valueAt)
			off = uint64(v)
		}
		e.ExtOffset = int64(off)
		r.valueReads++
		if r.valueReads > r.opts.BudgetValueRead {
			r.diag(Diagnostic{Code: "value_budget_exhausted", Severity: SevErr, IFD: ifdOff, Tag: tag,
				Message:  fmt.Sprintf("external value fetch budget %d exhausted", r.opts.BudgetValueRead),
				AtOffset: e.ExtOffset})
			return e
		}
		payload, ok := r.bytes(int64(off), int64(total))
		if !ok {
			e.OutOfBounds = true
			r.diag(Diagnostic{Code: "external_value_oob", Severity: SevErr, IFD: ifdOff, Tag: tag,
				Message: fmt.Sprintf("tag %d external value @%d len %d outside file (%d bytes)",
					tag, off, total, len(r.b)), AtOffset: int64(off)})
			return e
		}
		e.ValueRaw = append([]byte(nil), payload...)
		inline = e.ValueRaw
	}
	r.decode(e, inline)
	return e
}

func mulCheck(a int, b uint64) (uint64, bool) {
	if b != 0 && uint64(a) > math.MaxUint64/b {
		return 0, true
	}
	return uint64(a) * b, false
}

func (r *reader) decode(e *Entry, raw []byte) {
	bo := r.bo
	ts := e.Type.Size(r.big)
	if len(raw) < ts || len(raw)%ts != 0 {
		return
	}
	n := len(raw) / ts
	switch e.Type {
	case TypeShort, TypeSShort:
		for i := 0; i < n; i++ {
			v := bo.Uint16(raw[2*i:])
			e.Uints = append(e.Uints, uint64(v))
			e.Sints = append(e.Sints, int64(int16(v)))
		}
	case TypeLong, TypeSLong, TypeIFD:
		for i := 0; i < n; i++ {
			v := bo.Uint32(raw[4*i:])
			e.Uints = append(e.Uints, uint64(v))
			e.Sints = append(e.Sints, int64(int32(v)))
			if e.Tag == TagSubIFDs {
				e.IFDLinks = append(e.IFDLinks, int64(v))
			}
		}
	case TypeLong8, TypeSLong8, TypeIFD8:
		for i := 0; i < n; i++ {
			v := bo.Uint64(raw[8*i:])
			e.Uints = append(e.Uints, v)
			e.Sints = append(e.Sints, int64(v))
			if e.Tag == TagSubIFDs {
				e.IFDLinks = append(e.IFDLinks, int64(v))
			}
		}
	case TypeByte, TypeSByte, TypeUndefined:
		for _, v := range raw {
			e.Uints = append(e.Uints, uint64(v))
			e.Sints = append(e.Sints, int64(int8(v)))
		}
	case TypeRational:
		for i := 0; i < n; i++ {
			num := bo.Uint32(raw[8*i:])
			den := bo.Uint32(raw[8*i+4:])
			if den != 0 {
				e.Doubles = append(e.Doubles, float64(num)/float64(den))
			} else {
				e.Doubles = append(e.Doubles, math.NaN())
			}
			e.Uints = append(e.Uints, uint64(num), uint64(den))
		}
	case TypeSRational:
		for i := 0; i < n; i++ {
			num := int32(bo.Uint32(raw[8*i:]))
			den := int32(bo.Uint32(raw[8*i+4:]))
			if den != 0 {
				e.Doubles = append(e.Doubles, float64(num)/float64(den))
			} else {
				e.Doubles = append(e.Doubles, math.NaN())
			}
			e.Sints = append(e.Sints, int64(num), int64(den))
		}
	case TypeDouble:
		for i := 0; i < n; i++ {
			bits := bo.Uint64(raw[8*i:])
			e.Doubles = append(e.Doubles, math.Float64frombits(bits))
		}
	case TypeFloat:
		for i := 0; i < n; i++ {
			bits := bo.Uint32(raw[4*i:])
			e.Doubles = append(e.Doubles, float64(math.Float32frombits(bits)))
		}
	case TypeASCII:
		e.RawAscii = string(raw)
		s := e.RawAscii
		for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == ' ') {
			s = s[:len(s)-1]
		}
		e.Ascii = s
	}
}

var _ = binary.BigEndian
