// Package demo builds small deterministic TIFF/GeoTIFF byte samples for the
// offline case pack. It is a byte-level builder with explicit layout so
// samples can demonstrate exact defects: BigTIFF wide offsets, IFD cycles,
// shared subdirectories, overlapping tile ranges, GeoKey conflicts and
// access-budget exhaustion.
package demo

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// Ptr is an explicit value-pointer for offset-bearing tags. When Block is
// non-empty the value points at an auto-allocated block of BlockSize bytes.
// Raw forces an arbitrary wide value (used to point past EOF).
type Ptr struct {
	Block     string
	BlockSize uint64
	Fill      byte
	Raw       *uint64
}

// EntrySpec is one IFD entry. Values are integers; Floats for DOUBLE; ASCII
// for ASCII. Ptrs supplies explicit pointers for LONG/IFD/LONG8/IFD8
// offset arrays. Wide forces external placement even when inline fits.
type EntrySpec struct {
	Tag    uint16
	Type   uint16
	Values []uint64
	Floats []float64
	ASCII  string
	Ptrs   []Ptr
	Wide   bool
}

// IFDSpec is one directory; links use names resolved at layout time.
type IFDSpec struct {
	Name    string
	Entries []EntrySpec
	Next    string
	SubIFDs []string
}

// Spec is a complete document.
type Spec struct {
	Name     string
	BigTIFF  bool
	Endian   binary.ByteOrder
	IFDs     []IFDSpec
	PadTo    uint64 // optional final file size (PadTo beyond layout = gaps)
}

type laidIFD struct {
	spec      *IFDSpec
	offset    uint64
	structEnd uint64
	extSize   uint64
	// entry external offsets (0 means inline)
	extOff []uint64
}

// Build serializes s into TIFF bytes.
func Build(s Spec) ([]byte, error) {
	if len(s.IFDs) == 0 {
		return nil, fmt.Errorf("sample %q: no IFDs", s.Name)
	}
	big := s.BigTIFF
	bo := s.Endian
	if bo == nil {
		bo = binary.LittleEndian
	}
	byName := map[string]*laidIFD{}
	for i := range s.IFDs {
		n := s.IFDs[i].Name
		if _, dup := byName[n]; dup {
			return nil, fmt.Errorf("sample %q: duplicate IFD %q", s.Name, n)
		}
		byName[n] = &laidIFD{spec: &s.IFDs[i]}
	}
	hdr := 8
	if big {
		hdr = 16
	}
	entrySize := 12
	if big {
		entrySize = 20
	}
	// Pass 1: external value area size per IFD (with 2-byte alignment).
	cursor := uint64(hdr)
	for i := range s.IFDs {
		ld := byName[s.IFDs[i].Name]
		ld.offset = cursor
		structLen := uint64(2+len(ld.spec.Entries)*entrySize+4)
		if big {
			structLen = uint64(8 + len(ld.spec.Entries)*entrySize + 8)
		}
		ld.structEnd = cursor + structLen
		ld.extOff = make([]uint64, len(ld.spec.Entries))
		run := ld.structEnd
		for ei, e := range ld.spec.Entries {
			raw, err := entryValue(e, big, bo, nil)
			if err != nil {
				return nil, err
			}
			slot := uint64(4)
			if big {
				slot = 8
			}
			if uint64(len(raw)) > slot || e.Wide {
				ld.extOff[ei] = run
				run += uint64(len(raw))
				if run%2 == 1 {
					run++
				}
			}
		}
		ld.extSize = run - ld.structEnd
		cursor = run
	}
	// Pass 2: pixel block pool after all IFD areas.
	type blockAlloc struct {
		offset uint64
		size   uint64
		fill   byte
	}
	blocks := map[string]*blockAlloc{}
	pool := cursor
	resolvePtr := func(p Ptr) (uint64, error) {
		if p.Raw != nil {
			return *p.Raw, nil
		}
		if p.Block == "" {
			return 0, fmt.Errorf("empty pointer")
		}
		if a, ok := blocks[p.Block]; ok {
			return a.offset, nil
		}
		if p.BlockSize == 0 {
			return 0, fmt.Errorf("block %q has zero size", p.Block)
		}
		a := &blockAlloc{offset: pool, size: p.BlockSize, fill: p.Fill}
		pool += p.BlockSize
		if pool%2 == 1 {
			pool++
		}
		blocks[p.Block] = a
		return a.offset, nil
	}
	resolveName := func(n string) (uint64, error) {
		if n == "" {
			return 0, nil
		}
		ld, ok := byName[n]
		if !ok {
			return 0, fmt.Errorf("sample %q: unknown IFD %q", s.Name, n)
		}
		return ld.offset, nil
	}

	size := pool
	if s.PadTo > size {
		size = s.PadTo
	}
	out := make([]byte, size)
	// Header.
	if bo == binary.LittleEndian {
		out[0], out[1] = 'I', 'I'
	} else {
		out[0], out[1] = 'M', 'M'
	}
	if big {
		bo.PutUint16(out[2:4], 43)
		bo.PutUint16(out[4:6], 0) // BitsVersion
		bo.PutUint16(out[6:8], 8) // Bytesize
		bo.PutUint64(out[10:18], byName[s.IFDs[0].Name].offset)
	} else {
		bo.PutUint16(out[2:4], 42)
		bo.PutUint32(out[4:8], uint32(byName[s.IFDs[0].Name].offset))
	}
	// IFDs.
	for i := range s.IFDs {
		ld := byName[s.IFDs[i].Name]
		p := ld.offset
		if big {
			bo.PutUint64(out[p:p+8], uint64(len(ld.spec.Entries)))
			p += 8
		} else {
			bo.PutUint16(out[p:p+2], uint16(len(ld.spec.Entries)))
			p += 2
		}
		// entries sorted ascending by tag (TIFF requirement).
		order := make([]int, len(ld.spec.Entries))
		for k := range order {
			order[k] = k
		}
		sort.SliceStable(order, func(a, b int) bool {
			return ld.spec.Entries[order[a]].Tag < ld.spec.Entries[order[b]].Tag
		})
		for _, oi := range order {
			e := ld.spec.Entries[oi]
			ep := p + uint64(oi)*uint64(entrySize)
			bo.PutUint16(out[ep:ep+2], e.Tag)
			bo.PutUint16(out[ep+2:ep+4], e.Type)
			ptrVals := make([]uint64, len(e.Ptrs))
			for k, pp := range e.Ptrs {
				v, err := resolvePtr(pp)
				if err != nil {
					return nil, err
				}
				ptrVals[k] = v
			}
			raw, err := entryValue(e, big, bo, ptrVals)
			if err != nil {
				return nil, err
			}
			cnt := entryCount(e)
			if big {
				bo.PutUint64(out[ep+4:ep+12], cnt)
			} else {
				bo.PutUint32(out[ep+4:ep+8], uint32(cnt))
			}
			slot := uint64(4)
			if big {
				slot = 8
			}
			if ld.extOff[oi] != 0 && (uint64(len(raw)) > slot || e.Wide) {
				off := ld.extOff[oi]
				if big {
					bo.PutUint64(out[ep+12:ep+20], off)
				} else {
					bo.PutUint32(out[ep+8:ep+12], uint32(off))
				}
				copy(out[off:], raw)
			} else {
				base := ep + 8
				if big {
					base = ep + 12
				}
				copy(out[base:], raw)
			}
		}
		// SubIFDs external array patch with resolved name offsets.
		for ei, e := range ld.spec.Entries {
			if e.Tag != 330 || ld.extOff[ei] == 0 {
				continue
			}
			off := ld.extOff[ei]
			width := uint64(4)
			if big {
				width = 8
			}
			for k, n := range ld.spec.SubIFDs {
				v, err := resolveName(n)
				if err != nil {
					return nil, err
				}
				if big {
					bo.PutUint64(out[off+uint64(k)*width:], v)
				} else {
					bo.PutUint32(out[off+uint64(k)*width:], uint32(v))
				}
			}
		}
		// Next pointer.
		next, err := resolveName(ld.spec.Next)
		if err != nil {
			return nil, err
		}
		nl := ld.structEnd
		if big {
			nl -= 8
			bo.PutUint64(out[nl:nl+8], next)
		} else {
			nl -= 4
			bo.PutUint32(out[nl:nl+4], uint32(next))
		}
	}
	for _, a := range blocks {
		for i := uint64(0); i < a.size; i++ {
			out[a.offset+i] = a.fill
		}
	}
	return out, nil
}
