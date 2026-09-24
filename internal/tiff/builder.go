package tiff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

type EntrySpec struct {
	Tag     uint16
	Type    uint16
	Ints    []int64
	UInts   []uint64
	Doubles []float64
	Bytes   []byte
}

type IFDSpec struct {
	Offset     int64
	Entries    []EntrySpec
	NextTarget int
}

type FileSpec struct {
	Big     bool
	Little  bool
	IFDs    []IFDSpec
	Padding int64
}

type BuildInfo struct {
	FirstIFD      int64
	ExternalStart int64
	TagRanges     map[string]ByteRange
}

type builtEntry struct {
	spec  EntrySpec
	typ   uint16
	count int
	data  []byte
}

func Build(spec FileSpec) ([]byte, BuildInfo, error) {
	if len(spec.IFDs) == 0 {
		return nil, BuildInfo{}, fmt.Errorf("at least one IFD is required")
	}
	info := BuildInfo{TagRanges: map[string]ByteRange{}}
	var bo binary.ByteOrder = binary.BigEndian
	if spec.Little {
		bo = binary.LittleEndian
	}
	built := make([][]builtEntry, len(spec.IFDs))
	externalStart := int64(0)
	for ifdIndex, ifd := range spec.IFDs {
		end := ifd.Offset + ifdSize(spec.Big, len(ifd.Entries)) + int64(offBytes(spec.Big))
		if end > externalStart {
			externalStart = end
		}
		for _, entry := range ifd.Entries {
			data, typ, count, err := encodeEntry(entry, bo)
			if err != nil {
				return nil, info, err
			}
			built[ifdIndex] = append(built[ifdIndex], builtEntry{entry, typ, count, data})
		}
	}
	info.FirstIFD = spec.IFDs[0].Offset
	info.ExternalStart = externalStart
	cursor := externalStart
	allocations := map[string]ByteRange{}
	for ifdIndex := range spec.IFDs {
		for _, entry := range built[ifdIndex] {
			if int64(len(entry.data)) > int64(offBytes(spec.Big)) {
				r := ByteRange{cursor, int64(len(entry.data)), cursor + int64(len(entry.data))}
				allocations[fmt.Sprintf("%d:%d", ifdIndex, entry.spec.Tag)] = r
				cursor = r.End
			}
		}
	}
	size := spec.Padding
	if cursor > size {
		size = cursor
	}
	buf := make([]byte, size)
	if spec.Little {
		buf[0], buf[1] = 'I', 'I'
	} else {
		buf[0], buf[1] = 'M', 'M'
	}
	if !spec.Big {
		bo.PutUint16(buf[2:4], 42)
		bo.PutUint32(buf[4:8], uint32(info.FirstIFD))
	} else {
		bo.PutUint16(buf[2:4], 43)
		bo.PutUint16(buf[4:6], 8)
		bo.PutUint16(buf[6:8], 0)
		bo.PutUint64(buf[8:16], uint64(info.FirstIFD))
	}
	for ifdIndex, ifd := range spec.IFDs {
		countPos := ifd.Offset
		if spec.Big {
			bo.PutUint64(buf[countPos:countPos+8], uint64(len(ifd.Entries)))
		} else {
			bo.PutUint16(buf[countPos:countPos+2], uint16(len(ifd.Entries)))
		}
		entryStart := countPos + 2
		entrySize := int64(12)
		if spec.Big {
			entryStart = countPos + 8
			entrySize = 20
		}
		for entryIndex, entry := range built[ifdIndex] {
			entryPos := entryStart + int64(entryIndex)*entrySize
			bo.PutUint16(buf[entryPos:entryPos+2], entry.spec.Tag)
			bo.PutUint16(buf[entryPos+2:entryPos+4], entry.typ)
			if spec.Big {
				bo.PutUint64(buf[entryPos+4:entryPos+12], uint64(entry.count))
			} else {
				bo.PutUint32(buf[entryPos+4:entryPos+8], uint32(entry.count))
			}
			slot := entryPos + 4
			if spec.Big {
				slot = entryPos + 12
			}
			key := fmt.Sprintf("%d:%d", ifdIndex, entry.spec.Tag)
			if r, ok := allocations[key]; ok {
				copy(buf[r.Offset:r.End], entry.data)
				if spec.Big {
					bo.PutUint64(buf[slot:slot+8], uint64(r.Offset))
				} else {
					bo.PutUint32(buf[slot:slot+4], uint32(r.Offset))
				}
				info.TagRanges[key] = r
			} else {
				copy(buf[slot:slot+int64(len(entry.data))], entry.data)
				info.TagRanges[key] = ByteRange{slot, int64(len(entry.data)), slot + int64(len(entry.data))}
			}
		}
		nextPos := entryStart + int64(len(ifd.Entries))*entrySize
		next := int64(0)
		if ifd.NextTarget >= 0 && ifd.NextTarget < len(spec.IFDs) {
			next = spec.IFDs[ifd.NextTarget].Offset
		}
		if spec.Big {
			bo.PutUint64(buf[nextPos:nextPos+8], uint64(next))
		} else {
			bo.PutUint32(buf[nextPos:nextPos+4], uint32(next))
		}
	}
	return buf, info, nil
}

func offBytes(big bool) int {
	if big {
		return 8
	}
	return 4
}

func ifdSize(big bool, entries int) int64 {
	if big {
		return 8 + int64(entries)*20
	}
	return 2 + int64(entries)*12
}

func encodeEntry(entry EntrySpec, bo binary.ByteOrder) ([]byte, uint16, int, error) {
	typ := entry.Type
	if typ == 0 {
		typ = 4
	}
	var out bytes.Buffer
	putUint := func(v uint64, size int) {
		buf := make([]byte, 8)
		bo.PutUint64(buf, v)
		out.Write(buf[8-size:])
	}
	switch typ {
	case 1, 2, 6, 7:
		if len(entry.Bytes) > 0 {
			out.Write(entry.Bytes)
		} else {
			for _, v := range entry.Ints {
				out.WriteByte(byte(v))
			}
		}
	case 3:
		for _, v := range entry.Ints {
			putUint(uint64(uint16(v)), 2)
		}
	case 4:
		for _, v := range entry.Ints {
			putUint(uint64(uint32(v)), 4)
		}
	case 16, 18:
		values := entry.UInts
		for _, v := range entry.Ints {
			values = append(values, uint64(v))
		}
		for _, v := range values {
			putUint(v, 8)
		}
	case 9:
		for _, v := range entry.Ints {
			putUint(uint64(int32(v)), 4)
		}
	case 5, 10:
		for i := 0; i+1 < len(entry.Ints); i += 2 {
			putUint(uint64(uint32(entry.Ints[i])), 4)
			putUint(uint64(uint32(entry.Ints[i+1])), 4)
		}
	case 11:
		for _, v := range entry.Doubles {
			putUint(uint64(math.Float32bits(float32(v))), 4)
		}
	case 12:
		for _, v := range entry.Doubles {
			var buf [8]byte
			bo.PutUint64(buf[:], math.Float64bits(v))
			out.Write(buf[:])
		}
	default:
		return nil, 0, 0, fmt.Errorf("unsupported builder type %d", typ)
	}
	count := len(entry.Bytes)
	switch typ {
	case 1, 2, 6, 7:
		if count == 0 {
			count = len(entry.Ints)
		}
	case 5, 10:
		count = len(entry.Ints) / 2
	case 11, 12:
		count = len(entry.Doubles)
	case 16, 18:
		count = len(entry.UInts) + len(entry.Ints)
	default:
		count = len(entry.Ints)
	}
	return out.Bytes(), typ, count, nil
}
