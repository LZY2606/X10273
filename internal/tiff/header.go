package tiff

import "encoding/binary"

// read returns the byte range [off, off+n) and charges the byte budget. A
// short read returns nil and a finding is added by the caller.
func (p *parser) read(off, n uint64, code, msg string) ([]byte, bool) {
	end := off + n
	if off > end || end > uint64(len(p.data)) {
		r := Range{Start: off, End: end}
		if end > uint64(len(p.data)) {
			r.End = uint64(len(p.data))
		}
		p.add(Finding{Code: code, Severity: SevError, Message: msg, Range: &r})
		return nil, false
	}
	p.bytesRead += int(n)
	return p.data[off:end], true
}

func (p *parser) parseHeader() (Header, error) {
	h := Header{}
	if len(p.data) < 8 {
		return h, fmt.Errorf("input shorter than 8 bytes: %d", len(p.data))
	}
	switch string(p.data[0:2]) {
	case "II":
		h.Order = OrderLE
		p.bo = binary.LittleEndian
	case "MM":
		h.Order = OrderBE
		p.bo = binary.BigEndian
	default:
		return h, fmt.Errorf("invalid byte-order mark %q", string(p.data[0:2]))
	}
	h.Magic = p.bo.Uint16(p.data[2:4])
	switch h.Magic {
	case 42:
		h.Kind = KindClassic
		h.Version = 42
		h.HeaderRange = Range{0, 8}
		h.FirstIFDRange = Range{4, 8}
		h.FirstIFD = uint64(p.bo.Uint32(p.data[4:8]))
	case 43:
		if len(p.data) < 16 {
			return h, fmt.Errorf("BigTIFF header shorter than 16 bytes")
		}
		h.Kind = KindBig
		h.Version = 43
		h.OffSize = p.bo.Uint16(p.data[4:6])
		h.Zero = p.bo.Uint16(p.data[6:8])
		h.HeaderRange = Range{0, 16}
		h.FirstIFDRange = Range{8, 16}
		h.FirstIFD = p.bo.Uint64(p.data[8:16])
		if h.OffSize != 8 {
			p.add(Finding{Code: "bigtiff_bad_offsize", Severity: SevError,
				Message: fmt.Sprintf("BigTIFF offset size is %d, expected 8", h.OffSize)})
		}
		if h.Zero != 0 {
			p.add(Finding{Code: "bigtiff_bad_reserved", Severity: SevWarn,
				Message: fmt.Sprintf("BigTIFF reserved word is %d, expected 0", h.Zero)})
		}
	default:
		return h, fmt.Errorf("invalid TIFF magic %d", h.Magic)
	}
	return h, nil
}
