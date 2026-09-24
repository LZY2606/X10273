// Package fixture builds small, deterministic TIFF/GeoTIFF byte samples
// (including deliberately malformed edge cases) without network access.
package fixture

import (
	"bytes"
	"encoding/binary"
	"math"
	"sort"
)

// BO selects byte order.
type BO int

const (
	LE BO = iota
	BE
)

func (b BO) order() binary.ByteOrder {
	if b == BE {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func (b BO) marker() []byte {
	if b == BE {
		return []byte{'M', 'M'}
	}
	return []byte{'I', 'I'}
}

// Entry is one IFD entry to be laid out.
type Entry struct {
	Tag     uint16
	Type    uint16
	Values  []uint64
	Doubles []float64
	ASCII   string
	// PointerTo references another node by ID (SubIFDs/Exif/GPS).
	PointerTo string
	// ExternalForced pins the value bytes at a fixed offset.
	ExternalForced int64
	// RawExternal overrides external payload bytes.
	RawExternal []byte
	// EntryTypeOverride allows hand-built malformed entries.
}

// Node is an IFD to be laid out.
type Node struct {
	ID           string
	Entries      []Entry
	Next         string
	NextForced   int64
	OffsetForced int64
}

type extSlot struct {
	data   []byte
	forced int64 // >=0 means fixed
}

// Builder lays nodes and external data out deterministically.
type Builder struct {
	order         BO
	big           bool
	nodes         []*Node
	sparseRegions []SparseRegion
}

// SparseRegion places data at an absolute offset without filling the gap.
type SparseRegion struct {
	Offset int64
	Data   []byte
}

// Sparse adds physically-backed bytes at a declared absolute offset.
func (b *Builder) Sparse(off int64, data []byte) *Builder {
	b.sparseRegions = append(b.sparseRegions, SparseRegion{Offset: off, Data: data})
	return b
}

// Add appends a node.
func (b *Builder) Add(n Node) *Builder {
	b.nodes = append(b.nodes, &n)
	return b
}

// Short makes a SHORT entry.
func Short(tag uint16, v ...uint64) Entry { return Entry{Tag: tag, Type: 3, Values: v} }

// Long makes a LONG entry.
func Long(tag uint16, v ...uint64) Entry { return Entry{Tag: tag, Type: 4, Values: v} }

// Long8 makes a LONG8 (BigTIFF) entry.
func Long8(tag uint16, v ...uint64) Entry { return Entry{Tag: tag, Type: 16, Values: v} }

// Double makes a DOUBLE entry.
func Double(tag uint16, d ...float64) Entry { return Entry{Tag: tag, Type: 12, Doubles: d} }

// Ascii makes an ASCII entry (NUL appended by encoder).
func Ascii(tag uint16, s string) Entry { return Entry{Tag: tag, Type: 2, ASCII: s} }

// Pointer makes an IFD-offset entry pointing at node id.
func Pointer(tag uint16, to string) Entry { return Entry{Tag: tag, Type: 4, PointerTo: to} }

type entryLayout struct {
	node    *Node
	no      int
	payload []byte // full logical value bytes
	forced  int64
}

func (b *Builder) headerLen() int64 {
	if b.big {
		return 16
	}
	return 8
}

func (b *Builder) entryLen() int64 {
	if b.big {
		return 20
	}
	return 12
}

func typeSize(t uint16) int64 {
	switch t {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11, 13:
		return 4
	case 5, 10, 12, 16, 17, 18:
		return 8
	}
	return 1
}

func (b *Builder) encodePayload(e Entry) []byte {
	ord := b.order.order()
	if e.ASCII != "" {
		return []byte(e.ASCII)
	}
	var buf bytes.Buffer
	write := func(v uint64, d float64) {
		size := typeSize(e.Type)
		bs := make([]byte, size)
		if e.Type == 12 {
			ord.PutUint64(bs, math.Float64bits(d))
		} else {
			putInt(ord, bs, e.Type, v)
		}
		buf.Write(bs)
	}
	n := len(e.Values)
	if len(e.Doubles) > n {
		n = len(e.Doubles)
	}
	for i := 0; i < n; i++ {
		var v uint64
		var d float64
		if i < len(e.Values) {
			v = e.Values[i]
		}
		if i < len(e.Doubles) {
			d = e.Doubles[i]
		}
		write(v, d)
	}
	return buf.Bytes()
}

func putInt(ord binary.ByteOrder, bs []byte, typ uint16, v uint64) {
	switch typ {
	case 1, 2, 6, 7:
		bs[0] = byte(v)
	case 3, 8:
		ord.PutUint16(bs, uint16(v))
	case 4, 9, 11, 13:
		ord.PutUint32(bs, uint32(v))
	case 16, 17, 18:
		ord.PutUint64(bs, v)
	}
}

func (b *Builder) countOf(e Entry) uint64 {
	if e.ASCII != "" {
		return uint64(len(e.ASCII))
	}
	n := uint64(len(e.Values))
	if uint64(len(e.Doubles)) > n {
		n = uint64(len(e.Doubles))
	}
	if n == 0 {
		n = 1 // pointer entries carry one offset
	}
	return n
}

var _ = sort.Strings
