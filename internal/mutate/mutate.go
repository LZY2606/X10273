// Package mutate performs exactly-one-direct-source edits on TIFF bytes.
// Each mutator changes a single structural source; everything else is
// preserved byte-for-byte so impact scoping can be reviewed.
package mutate

import (
	"encoding/binary"
	"fmt"

	"ifdroom/internal/tiff"
)

// FlipEndian flips one byte of the byte-order marker (II<->MM).
func FlipEndian(data []byte) []byte {
	out := append([]byte(nil), data...)
	if out[0] == 'I' {
		out[0] = 'M'
	} else {
		out[0] = 'I'
	}
	return out
}

// WideOffsetOOB rewrites the BigTIFF TileOffsets entry to an 8-byte LONG8
// array whose external pointer is a wide offset beyond EOF. One entry slot is
// edited; the rest of the file is untouched.
func WideOffsetOOB(data []byte) ([]byte, error) {
	f, err := tiff.Parse(data, tiff.DefaultOptions())
	if err != nil {
		return nil, err
	}
	if !f.BigTIFF {
		return nil, fmt.Errorf("wide offset mutation requires BigTIFF")
	}
	for _, ifd := range f.IFDs {
		e := ifd.ByTag[tiff.TagTileOffsets]
		if e == nil {
			continue
		}
		entryAt := e.SlotOffset - 12 // BigTIFF: tag2 type2 count8 value8
		if _, ok := readU16(data, entryAt, f.Order); !ok {
			continue
		}
		out := append([]byte(nil), data...)
		bo := f.Order.ByteOrder()
		bo.PutUint16(out[entryAt+2:entryAt+4], uint16(tiff.TypeLong8))
		// count unchanged; force external read at an unreachable offset
		bo.PutUint64(out[e.SlotOffset:e.SlotOffset+8], 0x7FFFFFFFFFFFFFF0)
		return out, nil
	}
	return nil, fmt.Errorf("no TileOffsets tag found")
}

// PatchNextCycle points the next-IFD pointer of srcIFD at targetIFD (one
// pointer edit), producing a structural cycle.
func PatchNextCycle(data []byte, srcIFD, targetIFD int64) ([]byte, error) {
	f, err := tiff.Parse(data, tiff.DefaultOptions())
	if err != nil {
		return nil, err
	}
	ifd, ok := f.ByOffset[srcIFD]
	if !ok {
		return nil, fmt.Errorf("source IFD@%d not found", srcIFD)
	}
	nextAt := ifd.ByteEnd - int64(f.OffBytes)
	if nextAt < 0 || nextAt+int64(f.OffBytes) > int64(len(data)) {
		return nil, fmt.Errorf("next pointer outside file")
	}
	out := append([]byte(nil), data...)
	bo := f.Order.ByteOrder()
	if f.BigTIFF {
		bo.PutUint64(out[nextAt:nextAt+8], uint64(targetIFD))
	} else {
		bo.PutUint32(out[nextAt:nextAt+4], uint32(targetIFD))
	}
	return out, nil
}

// PatchSubIFDSlot replaces one 330-pointer slot with another target offset
// (one pointer edit), producing a shared subdirectory edge.
func PatchSubIFDSlot(data []byte, ifdOff int64, slot int, targetIFD int64) ([]byte, error) {
	f, err := tiff.Parse(data, tiff.DefaultOptions())
	if err != nil {
		return nil, err
	}
	ifd, ok := f.ByOffset[ifdOff]
	if !ok {
		return nil, fmt.Errorf("IFD@%d not found", ifdOff)
	}
	e := ifd.ByTag[tiff.TagSubIFDs]
	if e == nil || slot >= len(e.IFDLinks) {
		return nil, fmt.Errorf("IFD@%d has no subifd slot %d", ifdOff, slot)
	}
	ptrAt := subifdSlotByteOffset(f, e, slot)
	out := append([]byte(nil), data...)
	bo := f.Order.ByteOrder()
	width := 4
	if f.BigTIFF || e.Type == tiff.TypeIFD8 || e.Type == tiff.TypeLong8 {
		width = 8
	}
	if width == 8 {
		bo.PutUint64(out[ptrAt:ptrAt+8], uint64(targetIFD))
	} else {
		bo.PutUint32(out[ptrAt:ptrAt+4], uint32(targetIFD))
	}
	return out, nil
}

func subifdSlotByteOffset(f *tiff.File, e *tiff.Entry, slot int) int64 {
	base := e.SlotOffset
	if e.External {
		base = e.ExtOffset
	}
	return base + int64(slot*e.Type.Size(f.BigTIFF))
}

// TileOverlap rewrites one tile offset to equal another tile's offset (one
// array element edit), producing a byte-range overlap.
func TileOverlap(data []byte, ifdOff int64, dst, src int) ([]byte, error) {
	f, err := tiff.Parse(data, tiff.DefaultOptions())
	if err != nil {
		return nil, err
	}
	ifd, ok := f.ByOffset[ifdOff]
	if !ok {
		return nil, fmt.Errorf("IFD@%d not found", ifdOff)
	}
	e := ifd.ByTag[tiff.TagTileOffsets]
	if e == nil || src >= len(e.Uints) || dst >= len(e.Uints) {
		return nil, fmt.Errorf("tile index out of range")
	}
	width := e.Type.Size(f.BigTIFF)
	base := e.SlotOffset
	if e.External {
		base = e.ExtOffset
	}
	out := append([]byte(nil), data...)
	bo := f.Order.ByteOrder()
	at := base + int64(dst*width)
	if width == 8 {
		bo.PutUint64(out[at:at+8], e.Uints[src])
	} else {
		bo.PutUint32(out[at:at+4], uint32(e.Uints[src]))
	}
	return out, nil
}

func readU16(b []byte, at int64, order tiff.Order) (uint16, bool) {
	if at < 0 || at+2 > int64(len(b)) {
		return 0, false
	}
	var bo binary.ByteOrder = binary.LittleEndian
	if order == tiff.BigEndian {
		bo = binary.BigEndian
	}
	return bo.Uint16(b[at : at+2]), true
}
