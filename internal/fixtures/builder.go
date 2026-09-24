// Package fixtures builds small, deterministic TIFF/GeoTIFF byte programs for
// the offline case pack. Nothing here touches the network or the host clock;
// every layout is explicit so edge cases (wide offsets, cycles, shared
// sub-IFDs, tile overlap, geo conflict) reproduce byte-for-byte.
package fixtures

import (
	"encoding/binary"
)

// ByteOrder selects the BOM used when emitting the file.
type ByteOrder string

const (
	LE ByteOrder = "II"
	BE ByteOrder = "MM"
)

// Tag is one directory entry to emit. Numeric slices choose the natural field
// type; Ascii/Doubles/SRationals/SLongs force their type. RawExternal stores
// the given bytes out of line. ForceExternal stores even a tiny value out of
// line (used to create wide-offset cases).
type Tag struct {
	ID            uint16
	Type          uint16
	Value         []uint64
	Ascii         string
	Doubles       []float64
	SRationals    [][2]int32
	SLongs        []int32
	SShorts       []int16
	RawExternal   []byte
	ForceExternal bool
}

// IFD is one directory and its outgoing links.
type IFD struct {
	Name       string
	Tags       []Tag
	Next       *IFD
	SubIFDs    []*IFD
	NextOffset *int64 // overrides the next-IFD pointer (malformed cases)
	SubOffsets []int64
}

// File is a configured TIFF build.
type File struct {
	big     bool
	bo      ByteOrder
	root    *IFD
	places  []placement
	minSize int64
}

type placement struct {
	offset int64
	data   []byte
}

// NewBuilder configures variant and endianness.
func NewBuilder(big bool, bo ByteOrder, root *IFD) *File {
	return &File{big: big, bo: bo, root: root}
}

// Place injects raw bytes at an absolute offset (tile/strip payloads).
func (f *File) Place(offset int64, data []byte) *File {
	f.places = append(f.places, placement{offset: offset, data: data})
	return f
}

// Truncate grows (or pads) the file to at least size bytes.
func (f *File) EnsureSize(size int64) *File {
	f.minSize = size
	return f
}

func (f *File) order() binary.ByteOrder {
	if f.bo == BE {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func (f *File) headerLen() int64 {
	if f.big {
		return 16
	}
	return 8
}

func (f *File) entrySize() int64 {
	if f.big {
		return 20
	}
	return 12
}

func (f *File) countLen() int64 {
	if f.big {
		return 8
	}
	return 2
}

func (f *File) ptrLen() int64 {
	if f.big {
		return 8
	}
	return 4
}

func (f *File) slotLen() int64 { return f.ptrLen() }

func (f *File) ifdSize(n *IFD) int64 {
	return f.countLen() + int64(len(n.Tags))*f.entrySize() + f.ptrLen()
}

// assign walks root/next/subs and fixes each unique node's byte offset.
func (f *File) assign() map[*IFD]int64 {
	off := map[*IFD]int64{}
	cursor := f.headerLen()
	var visit func(n *IFD)
	visit = func(n *IFD) {
		if n == nil {
			return
		}
		if _, ok := off[n]; ok {
			return
		}
		off[n] = cursor
		cursor += f.ifdSize(n)
		visit(n.Next)
		for _, s := range n.SubIFDs {
			visit(s)
		}
	}
	visit(f.root)
	return off
}



func fieldSize(typ uint16) int64 {
	switch typ {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11, 13:
		return 4
	case 5, 10, 12, 16, 17, 18:
		return 8
	}
	return 0
}


