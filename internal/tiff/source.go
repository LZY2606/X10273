package tiff

import (
	"encoding/binary"
	"fmt"
)

// Source is the byte window a parser reads from. WideSource maps a sparse
// logical region at >= 2^32 onto a tiny physical backing buffer, so BigTIFF
// wide offsets can be exercised without allocating four gigabytes.
type Source interface {
	ReadAt(b []byte, off int64) (int, error)
	Size() int64
	ID() string
	// MapOffset translates logical offsets (virtual sparse files).
	MapOffset(off int64) (physical int64, ok bool)
	Virtual() bool
}

// BytesSource is an ordinary, contiguous byte file.
type BytesSource struct {
	IDv string
	B   []byte
}

func (s *BytesSource) ID() string                            { return s.IDv }
func (s *BytesSource) Size() int64                           { return int64(len(s.B)) }
func (s *BytesSource) Virtual() bool                         { return false }
func (s *BytesSource) MapOffset(off int64) (int64, bool)     { return off, true }
func (s *BytesSource) ReadAt(b []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(s.B)) {
		return 0, fmt.Errorf("read past end of file: offset %d, size %d", off, len(s.B))
	}
	n := copy(b, s.B[off:])
	if n < len(b) {
		return n, fmt.Errorf("short read at offset %d: wanted %d, got %d", off, len(b), n)
	}
	return n, nil
}

// WideSource places Region bytes at logical RegionStart (>= 2^32) while the
// header/IFDs live in Normal. Reads are sparse; Size() reports the logical
// end so out-of-range evidence is measured against it.
type WideSource struct {
	Normal      []byte
	RegionStart int64
	Region      []byte
	IDv         string
}

func (s *WideSource) ID() string    { return s.IDv }
func (s *WideSource) Virtual() bool { return true }
func (s *WideSource) Size() int64   { return s.RegionStart + int64(len(s.Region)) }

func (s *WideSource) MapOffset(off int64) (int64, bool) {
	if off >= s.RegionStart && off < s.RegionStart+int64(len(s.Region)) {
		return off - s.RegionStart, true
	}
	if off >= 0 && off < int64(len(s.Normal)) {
		return off, false
	}
	return 0, false
}

func (s *WideSource) ReadAt(b []byte, off int64) (int, error) {
	if off >= s.RegionStart {
		end := off + int64(len(b))
		rs, re := s.RegionStart, s.RegionStart+int64(len(s.Region))
		if off >= re || end <= rs {
			return 0, fmt.Errorf("read outside sparse region: offset %d", off)
		}
		if off < rs || end > re {
			return 0, fmt.Errorf("read crosses sparse region boundary: offset %d", off)
		}
		return copy(b, s.Region[off-rs:]), nil
	}
	src := &BytesSource{B: s.Normal}
	return src.ReadAt(b, off)
}

// ByteRange is a provenance-grade byte span. Virtual ranges live behind the
// wide-offset shim.
type ByteRange struct {
	Offset  int64 `json:"offset"`
	Length  int64 `json:"length"`
	Virtual bool  `json:"virtual,omitempty"`
}

func (r ByteRange) End() int64 { return r.Offset + r.Length }

func (r ByteRange) Intersects(o ByteRange) bool {
	return r.Offset < o.End() && o.Offset < r.End()
}

// order bo returns the binary order.
func orderBo(little bool) binary.ByteOrder {
	if little {
		return binary.LittleEndian
	}
	return binary.BigEndian
}
