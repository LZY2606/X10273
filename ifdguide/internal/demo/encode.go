package demo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

var sizeTable = [19]uint64{0, 1, 1, 2, 4, 8, 1, 1, 2, 4, 8, 4, 8, 4, 0, 0, 8, 8, 8}

// entryCount returns the TIFF count for an entry.
func entryCount(e EntrySpec) uint64 {
	switch e.Type {
	case 2:
		return uint64(len(e.ASCII) + 1)
	case 12:
		return uint64(len(e.Floats))
	default:
		if len(e.Ptrs) > 0 {
			return uint64(len(e.Ptrs))
		}
		return uint64(len(e.Values))
	}
}

// entryValue serializes an entry's value area. When ptrVals is non-empty it
// overrides Values for pointer-typed entries.
func entryValue(e EntrySpec, big bool, bo binary.ByteOrder, ptrVals []uint64) ([]byte, error) {
	if e.Type == 0 || int(e.Type) >= len(sizeTable) || sizeTable[e.Type] == 0 {
		return nil, fmt.Errorf("unsupported type %d", e.Type)
	}
	var b bytes.Buffer
	ints := e.Values
	if len(ptrVals) > 0 {
		ints = ptrVals
	}
	switch e.Type {
	case 1, 7:
		for _, v := range ints {
			b.WriteByte(byte(v))
		}
	case 2:
		b.WriteString(e.ASCII)
		b.WriteByte(0)
	case 3:
		var tmp [2]byte
		for _, v := range ints {
			bo.PutUint16(tmp[:], uint16(v))
			b.Write(tmp[:])
		}
	case 4, 13:
		var tmp [4]byte
		for _, v := range ints {
			bo.PutUint32(tmp[:], uint32(v))
			b.Write(tmp[:])
		}
	case 16, 18:
		var tmp [8]byte
		for _, v := range ints {
			bo.PutUint64(tmp[:], v)
			b.Write(tmp[:])
		}
	case 12:
		var tmp [8]byte
		for _, f := range e.Floats {
			bo.PutUint64(tmp[:], math.Float64bits(f))
			b.Write(tmp[:])
		}
	default:
		return nil, fmt.Errorf("builder: type %d not used by samples", e.Type)
	}
	_ = big
	return b.Bytes(), nil
}
