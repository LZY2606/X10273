package tiff

import (
	"encoding/binary"
	"fmt"
	"ifdlantern/internal/digest"
	"ifdlantern/internal/domain"
	"strings"
)

func (w *walker) readIFD(off int64) (*IFD, error) {
	entrySize := map[bool]int{true: 12, false: 20}[w.variant == "bigtiff"]
	countSize := map[bool]int{true: 8, false: 2}[w.variant == "bigtiff"]
	hdrN := int64(countSize)
	cb, err := readFull(w.src, off, int(hdrN))
	if err != nil {
		return nil, fmt.Errorf("ifd@%d: count: %w", off, err)
	}
	var count int64
	if w.variant == "bigtiff" {
		count = int64(w.bo.Uint64(cb))
	} else {
		count = int64(w.bo.Uint16(cb))
	}
	totalLen := int64(hdrN + count*int64(entrySize) + map[bool]int64{true: 8, false: 4}[w.variant == "bigtiff"])
	body, err := readFull(w.src, off, int(totalLen))
	if err != nil {
		return nil, fmt.Errorf("ifd@%d: body (%d bytes): %w", off, totalLen, err)
	}
	_, vRange := w.src.MapOffset(off)
	node := &IFD{
		Offset: off,
		Range:  ByteRange{Offset: off, Length: totalLen, Virtual: w.src.Virtual() && vRange},
	}
	node.ID = "ifd-" + digest.Hash(append([]byte(w.src.ID()), body...))[:16]
	for i := int64(0); i < count; i++ {
		e := body[hdrN+i*int64(entrySize):]
		tag := int(w.bo.Uint16(e[0:2]))
		ttype := int(w.bo.Uint16(e[2:4]))
		var cnt, valueOff int64
		if w.variant == "bigtiff" {
			cnt = int64(w.bo.Uint64(e[4:12]))
			valueOff = int64(w.bo.Uint64(e[12:20]))
		} else {
			cnt = int64(w.bo.Uint32(e[4:8]))
			valueOff = int64(w.bo.Uint32(e[8:12]))
		}
		rt := RawTag{
			Tag: tag, Name: tagName(tag), Type: ttype,
			TypeName: typeNames[ttype], Count: cnt, ValueOff: valueOff,
			EntryRange: ByteRange{
				Offset: off + hdrN + i*int64(entrySize), Length: int64(entrySize),
				Virtual: w.src.Virtual() && vRange,
			},
		}
		w.readTagValue(&rt, entryBody(e, w.variant == "bigtiff"), vRange)
		node.Tags = append(node.Tags, rt)
		if tag == 330 {
			node.SubPtrs = append(node.SubPtrs, offsetsOf(rt)...)
		}
	}
	nb := body[totalLen-int64(map[bool]int{true: 8, false: 4}[w.variant == "bigtiff"]):]
	if w.variant == "bigtiff" {
		node.Next = int64(w.bo.Uint64(nb))
	} else {
		node.Next = int64(w.bo.Uint32(nb))
	}
	if node.Next == off {
		node.CycleClose = node.ID // self-loop visible structurally
	}
	return node, nil
}

func entryBody(e []byte, big bool) []byte {
	if big {
		return e[12:20]
	}
	return e[8:12]
}

func offsetsOf(rt RawTag) []int64 {
	var out []int64
	for _, v := range rt.Values {
		out = append(out, int64(v.N))
	}
	return out
}

// readTagValue decides inline vs external, preserves original bytes and
// derives typed values. BigTIFF's eight-byte value slot means LONG8/IFD8
// values are always inline.
func (w *walker) readTagValue(rt *RawTag, slot []byte, slotVirtual bool) {
	sizes := typeSizesClassic
	slotLen := 4
	if w.variant == "bigtiff" {
		sizes = typeSizesBig
		slotLen = 8
	}
	tsize, known := sizes[rt.Type]
	if !known {
		rt.Raw = append([]byte(nil), slot...)
		rt.Inline = true
		w.report.Diags = append(w.report.Diags, domainDiag("unknown-tag-type", rt, tsize))
		return
	}
	n := int64(tsize) * rt.Count
	if n <= int64(slotLen) {
		rt.Inline = true
		rt.Raw = append([]byte(nil), slot[:n]...)
		rt.Range = ByteRange{}
	} else {
		rt.Inline = false
		off := rt.ValueOff
		_, vr := w.src.MapOffset(off)
		want := n
		capN := int64(w.cfg.Budget.MaxTagBytes)
		trunc := false
		if want > capN {
			want = capN
			trunc = true
		}
		data, err := readFull(w.src, off, int(want))
		rt.Range = ByteRange{Offset: off, Length: n, Virtual: w.src.Virtual() && vr}
		if err != nil {
			w.report.Diags = append(w.report.Diags, domain.Diagnostic{
				Code: "external-value-unreadable", Severity: domain.SevError,
				Message: fmt.Sprintf("tag %d external value at %d: %s", rt.Tag, off, err),
			})
			return
		}
		rt.Raw = data
		rt.Truncated = trunc
	}
	rt.Values, rt.RawKeep = decodeValues(rt.Type, rt.Count, rt.Raw, w.bo, w.little)
}

func domainDiag(code string, rt *RawTag, _ int) domain.Diagnostic {
	return domain.Diagnostic{
		Code: code, Severity: domain.SevError,
		Message: fmt.Sprintf("tag %d uses unknown field type %d", rt.Tag, rt.Type),
	}
}

func decodeValues(ttype int, count int64, raw []byte, bo binary.ByteOrder, little bool) ([]Value, string) {
	sizes := typeSizesClassic
	var vals []Value
	var ascii strings.Builder
	switch ttype {
	case 2:
		s := strings.TrimRight(string(raw), "\x00")
		return []Value{{S: s, Keep: s}}, s
	case 1, 6, 7:
		for i := int64(0); i < count && i < int64(len(raw)); i++ {
			vals = append(vals, Value{N: float64(raw[i]), Keep: fmt.Sprintf("%d", raw[i])})
		}
	case 3:
		for i := int64(0); i+2 <= int64(len(raw)); i += 2 {
			n := bo.Uint16(raw[i : i+2])
			vals = append(vals, Value{N: float64(n), Keep: fmt.Sprintf("%d", n)})
		}
	case 4, 13:
		width := sizes[4]
		if ttype == 13 {
			width = 4
		}
		for i := int64(0); i+int64(width) <= int64(len(raw)); i += int64(width) {
			n := bo.Uint32(raw[i : i+4])
			vals = append(vals, Value{N: float64(n), Keep: fmt.Sprintf("%d", n)})
		}
	case 16, 18:
		for i := int64(0); i+8 <= int64(len(raw)); i += 8 {
			n := bo.Uint64(raw[i : i+8])
			vals = append(vals, Value{N: float64(n), Keep: fmt.Sprintf("%d", n)})
		}
	case 5:
		for i := int64(0); i+8 <= int64(len(raw)); i += 8 {
			num := bo.Uint32(raw[i : i+4])
			den := bo.Uint32(raw[i+4 : i+8])
			vals = append(vals, Value{N: float64(num), N2: float64(den),
				Keep: fmt.Sprintf("%d/%d", num, den)})
		}
	case 10:
		for i := int64(0); i+8 <= int64(len(raw)); i += 8 {
			num := int32(bo.Uint32(raw[i : i+4]))
			den := bo.Uint32(raw[i+4 : i+8])
			vals = append(vals, Value{N: float64(num), N2: float64(den),
				Keep: fmt.Sprintf("%d/%d", num, den)})
		}
	case 9:
		for i := int64(0); i+4 <= int64(len(raw)); i += 4 {
			n := int32(bo.Uint32(raw[i : i+4]))
			vals = append(vals, Value{N: float64(n), Keep: fmt.Sprintf("%d", n)})
		}
	case 11:
		for i := int64(0); i+4 <= int64(len(raw)); i += 4 {
			bits := bo.Uint32(raw[i : i+4])
			var f float32
			putF32(&f, bits)
			vals = append(vals, Value{N: float64(f), Keep: fmt.Sprintf("%g", f)})
		}
	case 12:
		for i := int64(0); i+8 <= int64(len(raw)); i += 8 {
			bits := bo.Uint64(raw[i : i+8])
			f := f64from(bits)
			vals = append(vals, Value{N: f, Keep: fmt.Sprintf("%g", f)})
		}
	default:
		for i := range raw {
			ascii.WriteString(fmt.Sprintf("%02x", raw[i]))
		}
		return []Value{{S: ascii.String(), Keep: ascii.String()}}, ascii.String()
	}
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, v.Keep)
	}
	return vals, strings.Join(parts, ",")
}
