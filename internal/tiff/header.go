package tiff

import (
	"encoding/binary"
	"fmt"
)

func readFull(src Source, off int64, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := src.ReadAt(b, off); err != nil {
		return nil, err
	}
	return b, nil
}

func parseHeader(src Source) (Header, binary.ByteOrder, bool, string, int64, error) {
	b, err := readFull(src, 0, 8)
	if err != nil {
		return Header{}, nil, false, "", 0, fmt.Errorf("header: %w", err)
	}
	var little bool
	switch string(b[0:2]) {
	case "II":
		little = true
	case "MM":
		little = false
	default:
		return Header{}, nil, false, "", 0, fmt.Errorf("bad byte order mark %q (expected II or MM)", b[0:2])
	}
	bo := orderBo(little)
	magic := int(bo.Uint16(b[2:4]))
	h := Header{Endian: map[bool]string{true: "little", false: "big"}[little], Magic: magic,
		Bytes: ByteRange{Offset: 0, Length: 8, Virtual: src.Virtual()}, Raw4: b}
	switch magic {
	case 42:
		h.Variant = "classic"
		h.Bytes.Length = 8
		first := int64(bo.Uint32(b[4:8]))
		h.FirstIFD = first
		return h, bo, little, "classic", first, nil
	case 43:
		// BigTIFF: II/MM, 43, 0008, 0000, 8-byte offset
		ext, err := readFull(src, 8, 8)
		if err != nil {
			return h, nil, false, "bigtiff", 0, fmt.Errorf("bigtiff header: %w", err)
		}
		h.Variant = "bigtiff"
		h.OffsetSize = int(bo.Uint16(ext[0:2]))
		h.Zero = int(bo.Uint16(ext[2:4]))
		h.Bytes.Length = 16
		h.Raw4 = append(h.Raw4, ext...)
		if h.OffsetSize != 8 {
			return h, nil, false, "bigtiff", 0, fmt.Errorf("bigtiff offsetsize is %d, expected 8", h.OffsetSize)
		}
		if h.Zero != 0 {
			return h, nil, false, "bigtiff", 0, fmt.Errorf("bigtiff reserved word is %d, expected 0", h.Zero)
		}
		first := int64(bo.Uint64(ext[4:12]))
		h.FirstIFD = first
		return h, bo, little, "bigtiff", first, nil
	default:
		return h, nil, false, "", 0, fmt.Errorf("unknown TIFF magic %d (42=classic, 43=BigTIFF)", magic)
	}
}
