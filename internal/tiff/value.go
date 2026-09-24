package tiff

import (
	"fmt"
	"math"
	"strconv"
)

func (p *parser) decodeValue(te *TagEntry, typ uint16, count uint64, valueOff int64, slot []byte, big bool, node *IFDNode) RawValue {
	ti, known := fieldTypes[typ]
	if !known {
		return RawValue{
			Type: "UNKNOWN(" + strconv.Itoa(int(typ)) + ")", Count: count,
			Inline: true, InlineBytes: clone(slot),
		}
	}
	raw := RawValue{Type: ti.name, Count: count, InlineBytes: clone(slot)}

	total := ti.size * int64(count)
	slotSize := int64(4)
	if big {
		slotSize = 8
	}
	external := total > slotSize
	raw.Inline = !external

	var payload []byte
	if external {
		raw.External = &ExternalValue{Offset: valueOff, Length: total}
		b, ok := p.r.slice(valueOff, total)
		if !ok {
			p.g.Findings = append(p.g.Findings, Finding{
				Code: FindingTagValueOutOfRange, Severity: SeverityError,
				IFDID: node.ID, TagID: te.ID, Offset: valueOff,
				Message: fmt.Sprintf("tag %s external value @%d len %d exceeds file", te.Name, valueOff, total),
				Detail: map[string]any{"length": total, "file_size": p.g.FileSize},
			})
			// Preserve only the in-range prefix; never fabricate missing bytes.
			payload = p.availablePrefix(valueOff, total)
			raw.External.Bytes = clone(payload)
			raw.Values = decodePayload(payload, typ, p.r.bo)
			return raw
		}
		if p.g.BytesRead+total > p.opts.BytesBudget {
			p.g.BudgetExhausted = true
			p.g.Findings = append(p.g.Findings, Finding{
				Code: FindingBudgetExhausted, Severity: SeverityError,
				IFDID: node.ID, TagID: te.ID, Offset: valueOff,
				Message: fmt.Sprintf("bytes budget %d exhausted reading tag %s", p.opts.BytesBudget, te.Name),
				Detail: map[string]any{"bytes_budget": p.opts.BytesBudget, "needed": total, "read": p.g.BytesRead},
			})
			return raw
		}
		p.g.BytesRead += total
		payload = b
		raw.External.Bytes = clone(b)
	} else {
		if int64(len(slot)) < total {
			payload = slot
		} else {
			payload = slot[:total]
		}
	}

	raw.Values = decodePayload(payload, typ, p.r.bo)
	return raw
}

func (p *parser) availablePrefix(off, total int64) []byte {
	if off < 0 || off >= p.g.FileSize {
		return nil
	}
	end := off + total
	if end > p.g.FileSize {
		end = p.g.FileSize
	}
	return p.r.data[off:end]
}

func clone(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

type wordReader interface {
	Uint16([]byte) uint16
	Uint32([]byte) uint32
	Uint64([]byte) uint64
}

// decodePayload decodes preserved bytes into original-representation strings.
// Rationals stay as "num/den"; floats use round-trip exact repr. ASCII is one
// logical NUL-trimmed string. It decodes what is present and stops at the end.
func decodePayload(b []byte, typ uint16, bo wordReader) []string {
	var out []string
	if typ == 2 {
		return []string{trimASCII(b)}
	}
	for pos := int64(0); ; {
		switch typ {
		case 1, 7:
			if pos+1 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.Itoa(int(b[pos])))
			pos++
		case 6:
			if pos+1 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.Itoa(int(int8(b[pos]))))
			pos++
		case 3:
			if pos+2 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.FormatUint(uint64(bo.Uint16(b[pos:])), 10))
			pos += 2
		case 8:
			if pos+2 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.Itoa(int(int16(bo.Uint16(b[pos:])))))
			pos += 2
		case 4, 13:
			if pos+4 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.FormatUint(uint64(bo.Uint32(b[pos:])), 10))
			pos += 4
		case 9:
			if pos+4 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.Itoa(int(int32(bo.Uint32(b[pos:])))))
			pos += 4
		case 16, 18:
			if pos+8 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.FormatUint(bo.Uint64(b[pos:]), 10))
			pos += 8
		case 17:
			if pos+8 > int64(len(b)) {
				return out
			}
			out = append(out, strconv.Itoa(int(int64(bo.Uint64(b[pos:])))))
			pos += 8
		case 5:
			if pos+8 > int64(len(b)) {
				return out
			}
			out = append(out, fmt.Sprintf("%d/%d", bo.Uint32(b[pos:]), bo.Uint32(b[pos+4:])))
			pos += 8
		case 10:
			if pos+8 > int64(len(b)) {
				return out
			}
			out = append(out, fmt.Sprintf("%d/%d", int32(bo.Uint32(b[pos:])), int32(bo.Uint32(b[pos+4:]))))
			pos += 8
		case 11:
			if pos+4 > int64(len(b)) {
				return out
			}
			out = append(out, formatFloat(float64(math.Float32frombits(bo.Uint32(b[pos:])))))
			pos += 4
		case 12:
			if pos+8 > int64(len(b)) {
				return out
			}
			out = append(out, formatFloat(math.Float64frombits(bo.Uint64(b[pos:]))))
			pos += 8
		default:
			return out
		}
	}
}

func trimASCII(b []byte) string {
	end := len(b)
	for end > 0 && b[end-1] == 0 {
		end--
	}
	return string(b[:end])
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
