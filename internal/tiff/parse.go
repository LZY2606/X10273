package tiff

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrFormat marks a definite, non-recoverable format error.
var ErrFormat = errors.New("tiff: format error")

// MaxExternalBytes caps how many external value bytes a single tag may claim.
const MaxExternalBytes = 64 << 20

type reader struct {
	data []byte
	bo   ByteOrder
	ord  binary.ByteOrder
	kind Kind
}

func (r *reader) u16(at int64) (uint16, bool) {
	if at < 0 || at+2 > int64(len(r.data)) {
		return 0, false
	}
	return r.ord.Uint16(r.data[at : at+2]), true
}

func (r *reader) u32(at int64) (uint32, bool) {
	if at < 0 || at+4 > int64(len(r.data)) {
		return 0, false
	}
	return r.ord.Uint32(r.data[at : at+4]), true
}

func (u *reader) u64(at int64) (uint64, bool) {
	if at < 0 || at+8 > int64(len(u.data)) {
		return 0, false
	}
	return u.ord.Uint64(u.data[at : at+8]), true
}

// Parse reads a TIFF byte stream into a provenance-preserving report.
func Parse(data []byte, opts Options) (*ParseReport, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: file shorter than 4 bytes (%d)", ErrFormat, len(data))
	}
	var bo ByteOrder
	var ord binary.ByteOrder
	switch {
	case data[0] == 'I' && data[1] == 'I':
		bo, ord = LittleEndian, binary.LittleEndian
	case data[0] == 'M' && data[1] == 'M':
		bo, ord = BigEndian, binary.BigEndian
	default:
		return nil, fmt.Errorf("%w: invalid byte order mark %02x %02x", ErrFormat, data[0], data[1])
	}
	r := &reader{data: data, bo: bo, ord: ord, kind: kind}
	magic, ok := r.u16(2)
	if !ok {
		return nil, fmt.Errorf("%w: truncated magic", ErrFormat)
	}
	var kind Kind
	var headerLen, rootPos int64
	switch magic {
	case 42:
		kind, headerLen, rootPos = Classic, 8, 4
	case 43:
		kind, headerLen = BigTIFF, 16
		// bytes 4-5 zeroes (version), 6-7 offset size (=8), 8-9 zeroes
		if offSize, ok := r.u16(6); !ok || offSize != 8 {
			return nil, fmt.Errorf("%w: BigTIFF offset size must be 8", ErrFormat)
		}
		if z, ok := r.u16(4); !ok || z != 0 {
			return nil, fmt.Errorf("%w: BigTIFF version field must be 0", ErrFormat)
		}
		rootPos = 8
	default:
		return nil, fmt.Errorf("%w: unknown magic %d", ErrFormat, magic)
	}
	if int64(len(data)) < headerLen {
		return nil, fmt.Errorf("%w: truncated %s header", ErrFormat, kind)
	}
	root, ok := r.readOffset(rootPos)
	if !ok {
		return nil, fmt.Errorf("%w: truncated first IFD offset", ErrFormat)
	}

	rep := &ParseReport{
		ByteOrder:   bo,
		Kind:        kind,
		FileSize:    int64(len(data)),
		HeaderRange: ByteRange{0, headerLen},
		BOMRange:    ByteRange{0, 2},
		RootOffset:  int64(root),
	}

	entrySize := 12
	offsetSize := int64(4)
	if kind == BigTIFF {
		entrySize, offsetSize = 20, 8
	}
	usage := BudgetUsage{IFDLimit: opts.MaxIFDs, BytesLimit: opts.MaxBytes}

	type visit struct {
		offset int64
		from   *Edge
	}
	visited := map[int64]*IFDNode{}
	queue := []visit{{offset: int64(root)}}

	chargeBytes := func(n int64, at int64) bool {
		usage.BytesScanned += n
		if usage.BytesScanned > opts.MaxBytes {
			usage.Exhausted = true
			usage.ExhaustedAt = at
			return false
		}
		return true
	}

	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		if v.from != nil {
			rep.Edges = append(rep.Edges, *v.from)
		}
		if node, seen := visited[v.offset]; seen {
			if v.from != nil {
				v.from.Valid = false
				v.from.Reason = "cycle"
				rep.Cycles = append(rep.Cycles, *v.from)
				node.Cycle = true
			}
			continue
		}
		if usage.IFDsVisited >= opts.MaxIFDs {
			usage.Exhausted = true
			usage.ExhaustedAt = v.offset
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Code: "ifd-budget-exhausted",
				Message: fmt.Sprintf("IFD visit budget %d exhausted at offset %d", opts.MaxIFDs, v.offset),
				At:    v.offset,
			})
			break
		}
		if v.offset < 0 || v.offset >= int64(len(data)) {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Code:    "ifd-offset-outside-file",
				Message: fmt.Sprintf("IFD offset %d outside [0,%d)", v.offset, len(data)),
				At:      v.offset,
			})
			continue
		}
		count, ok := r.u64or16(v.offset, kind)
		if !ok || !chargeBytes(2, v.offset) {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Code: "ifd-count-overrun", Message: "IFD entry count overruns file", At: v.offset,
			})
			continue
		}
		entriesStart := v.offset + 2
		if kind == BigTIFF {
			entriesStart = v.offset + 8
		}
		dirLen := int64(entrySize)*int64(count) + offsetSize
		if entriesStart+dirLen > int64(len(data)) {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Code:    "ifd-entries-overrun",
				Message: fmt.Sprintf("%d IFD entries at %d overrun file", count, v.offset),
				At:      v.offset,
				Range:   ByteRange{entriesStart, int64(entrySize) * int64(count)},
			})
			// truncated directory: nothing trustworthy beyond the count
			continue
		}
		if !chargeBytes(dirLen, v.offset) {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Code: "byte-budget-exhausted",
				Message: fmt.Sprintf("byte scan budget %d exhausted while reading IFD at %d", opts.MaxBytes, v.offset),
				At:    v.offset,
			})
			break
		}
		node := &IFDNode{
			Offset: v.offset, Index: len(rep.IFDs), EntrySize: entrySize,
			Range: ByteRange{v.offset, dirLen + (entriesStart - v.offset)},
		}
		rep.IFDs = append(rep.IFDs, node)
		visited[v.offset] = node
		usage.IFDsVisited++

		for i := uint64(0); i < count; i++ {
			eat := entriesStart + int64(i)*int64(entrySize)
			tag := r.ord.Uint16(data[eat : eat+2])
			typ := r.ord.Uint16(data[eat+2 : eat+4])
			var cnt uint64
			var valPos int64
			if kind == Classic {
				cnt = uint64(r.ord.Uint32(data[eat+4 : eat+8]))
				valPos = eat + 8
			} else {
				cnt = r.ord.Uint64(data[eat+4 : eat+12])
				valPos = eat + 12
			}
			rt := RawTag{
				ID: tag, Name: TagName(tag), Type: typ, TypeName: typeName(typ),
				Count: cnt, EntryAt: eat,
			}
			size, knownType := typeSizes[typ]
			total := int64(cnt) * int64(size)
			if !knownType {
				rt.Loc.Overrun = fmt.Sprintf("unknown field type %d", typ)
				rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
					Code: "unknown-tag-type", Message: rt.Loc.Overrun, At: valPos,
				})
			}
			rt.OffsetOrValue, _ = r.readOffset(valPos)
			slotLen := offsetSize
			if kind == BigTIFF {
				slotLen = 8
			}
			rt.Raw = append([]byte(nil), data[valPos:valPos+slotLen]...)
			if knownType && total <= slotLen && total >= 0 {
				// inline value; retain slot bytes, padded per spec
				rt.Loc = ValueLoc{External: false, Range: ByteRange{valPos, slotLen}}
			} else if knownType {
				off := int64(rt.OffsetOrValue)
				if total > MaxExternalBytes {
					rt.Loc = ValueLoc{External: true, Range: ByteRange{off, total},
						Overrun: fmt.Sprintf("external value %d bytes exceeds cap %d", total, MaxExternalBytes)}
				} else if off < 0 || off+total > int64(len(data)) {
					rt.Loc = ValueLoc{External: true, Range: ByteRange{off, total}, Overrun: "external value overruns file"}
					rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
						Code: "external-value-overrun",
						Message: fmt.Sprintf("tag %d (%d bytes at %d) overruns file", tag, total, off),
						At:      off, Range: ByteRange{off, total},
					})
				} else {
					rt.Loc = ValueLoc{External: true, Range: ByteRange{off, total}}
					if !chargeBytes(total, off) {
						rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
							Code: "byte-budget-exhausted",
							Message: fmt.Sprintf("byte scan budget exhausted reading tag %d heap bytes", tag),
							At:      off,
						})
					}
				}
			}
			node.Tags = append(node.Tags, rt)
		}

		// next IFD pointer
		nextPos := entriesStart + int64(count)*int64(entrySize)
		nextOff, ok := r.readOffset(nextPos)
		if ok {
			if nextOff != 0 {
				queue = append(queue, visit{offset: int64(nextOff), from: &Edge{
					From: v.offset, To: int64(nextOff), Kind: EdgeNext, At: nextPos, Valid: true,
				}})
			}
		}
		// SubIFDs pointers
		for i, tg := range node.Tags {
			if tg.ID != TagSubIFDs {
				continue
			}
			size := typeSizes[tg.Type]
			if (tg.Type != 4 && tg.Type != 16 && tg.Type != 18) || size == 0 {
				continue
			}
			for k := uint64(0); k < tg.Count; k++ {
				ptr, at, ok := r.valueWord(tg, int(k), kind)
				if !ok {
					continue
				}
				queue = append(queue, visit{offset: int64(ptr), from: &Edge{
					From: v.offset, To: int64(ptr), Kind: EdgeSubIFD, TagID: tg.ID,
					Index: int(k), At: at, Valid: true,
				}})
			}
			_ = i
		}
	}

	// Mark shared directories: any node entered by more than one valid edge.
	inDegree := map[int64]int{}
	for _, e := range rep.Edges {
		inDegree[e.To]++
	}
	for _, n := range rep.IFDs {
		if inDegree[n.Offset] > 1 {
			n.Shared = true
		}
	}
	usage.RemainingAfter = opts.MaxIFDs - usage.IFDsVisited
	rep.Budget = usage
	return rep, nil
}

func (r *reader) u64or16(at int64, kind Kind) (uint64, bool) {
	if kind == BigTIFF {
		return r.u64(at)
	}
	v, ok := r.u16(at)
	return uint64(v), ok
}

func (r *reader) readOffset(at int64) (uint64, bool) {
	if r.kind == BigTIFF {
		return r.u64(at)
	}
	v, ok := r.u32(at)
	return uint64(v), ok
}

// valueWord reads one pointer-sized word from a tag's inline slot or heap.
func (r *reader) valueWord(t RawTag, idx int, kind Kind) (uint64, int64, bool) {
	word := int64(4)
	if kind == BigTIFF || t.Type == 16 || t.Type == 18 {
		word = 8
	}
	at := t.Loc.Range.Offset + int64(idx)*word
	if t.Loc.External {
		if at+word > int64(len(r.data)) {
			return 0, at, false
		}
	} else {
		// inline offsets only fit one word in classic; multiple SubIFDs are external
		if idx != 0 && kind == Classic {
			return 0, at, false
		}
	}
	if word == 8 {
		v, ok := r.u64(at)
		return v, at, ok
	}
	v, ok := r.u32(at)
	return uint64(v), at, ok
}
