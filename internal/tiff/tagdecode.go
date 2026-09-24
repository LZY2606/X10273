package tiff

import (
	"encoding/binary"
	"fmt"
	"math"

	"ifdgraph/internal/model"
)

// pointerTags are IFD-entry tag codes whose values are interpreted as
// absolute file offsets to other IFDs.
var pointerTags = map[uint16]bool{
	TagSubIFDs: true, TagExifIFD: true, TagGPSIFD: true,
}

func (p *parser) decodeTag(g *model.ParseGraph, ifd *model.IFD, tag, typeCode uint16,
	count uint64, inlineValue, entryOff, entryLen int64, ordinal int) *model.RawTag {

	rt := &model.RawTag{
		ID:         model.TagID(g.SourceID, ifd.Offset, tag, ordinal),
		IFDID:      ifd.ID,
		Tag:        tag,
		TypeCode:   typeCode,
		TypeName:   typeNames[typeCode],
		Count:      count,
		EntryRange: model.ByteRange{Offset: entryOff, Length: entryLen},
	}
	if rt.TypeName == "" {
		rt.TypeName = fmt.Sprintf("TYPE%d", typeCode)
	}
	size, known := typeSizes[typeCode]
	if !known {
		rt.ValueKind = model.ValueMalformed
		rt.InlineBytes = append([]byte(nil), p.data[entryOff+entryLen-4:entryOff+entryLen]...)
		if !p.big {
			rt.InlineBytes = append([]byte(nil), p.data[entryOff+8:entryOff+12]...)
		} else {
			rt.InlineBytes = append([]byte(nil), p.data[entryOff+12:entryOff+20]...)
		}
		rt.Diagnostic = fmt.Sprintf("unknown TIFF type code %d", typeCode)
		g.Tags = append(g.Tags, rt)
		diag(g, rt.ID, "unknown_type", rt.Diagnostic)
		return rt
	}
	total := int64(size) * int64(count)
	if count != 0 && size != 0 && uint64(total/int64(size)) != count {
		rt.ValueKind = model.ValueMalformed
		rt.Diagnostic = "declared value size overflows int64"
		g.Tags = append(g.Tags, rt)
		diag(g, rt.ID, "size_overflow", rt.Diagnostic)
		return rt
	}
	inlineSlot := int64(4)
	if p.big {
		inlineSlot = 8
	}
	payloadOff := entryOff + entryLen - inlineSlot
	if total <= inlineSlot {
		rt.ValueKind = model.ValueInline
		rt.ValueRange = model.ByteRange{Offset: payloadOff, Length: total}
		rt.InlineBytes = append([]byte(nil), p.data[payloadOff:payloadOff+total]...)
	} else {
		rt.ValueKind = model.ValueExternal
		ext := inlineValue
		if ext < 0 || ext+total > int64(len(p.data)) {
			rt.ValueKind = model.ValueMalformed
			rt.ValueRange = model.ByteRange{Offset: ext, Length: 0}
			rt.Diagnostic = fmt.Sprintf("external value at offset %d (%d bytes) outside file of %d bytes", ext, total, len(p.data))
			g.Tags = append(g.Tags, rt)
			diag(g, rt.ID, "external_oob", rt.Diagnostic)
			return rt
		}
		rt.ValueRange = model.ByteRange{Offset: ext, Length: total}
		rt.ExternalData = append([]byte(nil), p.data[ext:ext+total]...)
	}
	raw := rt.InlineBytes
	if rt.ValueKind == model.ValueExternal {
		raw = rt.ExternalData
	}
	decodeValues(p.order, rt, raw)
	rt.PointerTag = pointerTags[tag]
	if len(raw) > 0 && (typeCode == 2) {
		rt.ASCII = string(raw)
	}
	if tag == TagGeoASCIIParams {
		rt.ASCII = string(raw)
	}
	g.Tags = append(g.Tags, rt)
	return rt
}

func decodeValues(order binary.ByteOrder, rt *model.RawTag, raw []byte) {
	switch rt.TypeCode {
	case 1, 7: // BYTE, UNDEFINED
		for _, b := range raw {
			rt.Numbers = append(rt.Numbers, uint64(b))
		}
	case 2: // ASCII handled by caller
	case 3:
		for i := 0; i+2 <= len(raw); i += 2 {
			rt.Numbers = append(rt.Numbers, uint64(order.Uint16(raw[i:i+2])))
		}
	case 4, 13:
		for i := 0; i+4 <= len(raw); i += 4 {
			rt.Numbers = append(rt.Numbers, uint64(order.Uint32(raw[i:i+4])))
		}
	case 16, 18:
		for i := 0; i+8 <= len(raw); i += 8 {
			rt.Numbers = append(rt.Numbers, order.Uint64(raw[i:i+8]))
		}
	case 6:
		for _, b := range raw {
			rt.Signed = append(rt.Signed, int64(int8(b)))
		}
	case 8:
		for i := 0; i+2 <= len(raw); i += 2 {
			rt.Signed = append(rt.Signed, int64(int16(order.Uint16(raw[i:i+2]))))
		}
	case 9:
		for i := 0; i+4 <= len(raw); i += 4 {
			rt.Signed = append(rt.Signed, int64(int32(order.Uint32(raw[i:i+4]))))
		}
	case 17:
		for i := 0; i+8 <= len(raw); i += 8 {
			rt.Signed = append(rt.Signed, int64(order.Uint64(raw[i:i+8])))
		}
	case 5:
		for i := 0; i+8 <= len(raw); i += 8 {
			num := order.Uint32(raw[i : i+4])
			den := order.Uint32(raw[i+4 : i+8])
			if den != 0 {
				rt.Doubles = append(rt.Doubles, float64(num)/float64(den))
			}
			rt.Numbers = append(rt.Numbers, uint64(num), uint64(den))
		}
	case 10:
		for i := 0; i+8 <= len(raw); i += 8 {
			num := int32(order.Uint32(raw[i : i+4]))
			den := int32(order.Uint32(raw[i+4 : i+8]))
			if den != 0 {
				rt.Doubles = append(rt.Doubles, float64(num)/float64(den))
			}
			rt.Signed = append(rt.Signed, int64(num), int64(den))
		}
	case 11:
		for i := 0; i+4 <= len(raw); i += 4 {
			rt.Doubles = append(rt.Doubles, float64(math.Float32frombits(order.Uint32(raw[i:i+4]))))
		}
	case 12:
		for i := 0; i+8 <= len(raw); i += 8 {
			rt.Doubles = append(rt.Doubles, math.Float64frombits(order.Uint64(raw[i:i+8])))
		}
	}
}
