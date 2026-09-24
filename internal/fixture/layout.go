package fixture

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

// Build serializes the nodes. External values are allocated after all IFDs;
// pointer entries resolve to node offsets. Forced offsets let cases create
// wide BigTIFF offsets, cycles and out-of-bounds external references.
func (b *Builder) Build() ([]byte, error) {
	ord := b.order.order()
	nodeByID := map[string]*Node{}
	for _, n := range b.nodes {
		nodeByID[n.ID] = n
	}

	// Determine IFD offsets.
	type alloc struct{ start, end int64 }
	var allocs []alloc
	cursor := b.headerLen()
	offOf := map[string]int64{}
	// Nodes with forced offsets first to reserve their slots.
	order := append([]*Node(nil), b.nodes...)
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].OffsetForced != 0 || order[j].OffsetForced != 0 {
			return order[i].OffsetForced != 0
		}
		return false
	})
	for _, n := range order {
		size := b.ifdSize(n)
		var off int64
		if n.OffsetForced != 0 {
			off = n.OffsetForced // sparse: physically placed via SparseRegions
		} else {
			cursor = align2(cursor)
			off = cursor
			cursor += size
		}
		offOf[n.ID] = off
		allocs = append(allocs, alloc{off, off + size})
	}

	// Lay out external payloads, collecting entry metadata.
	type ext struct {
		key    string
		data   []byte
		forced int64
	}
	var exts []ext
	extOff := map[string]int64{}
	for _, n := range b.nodes {
		for i := range n.Entries {
			e := &n.Entries[i]
			if e.PointerTo != "" {
				if _, ok := offOf[e.PointerTo]; !ok {
					return nil, fmt.Errorf("entry tag %d points to unknown node %q", e.Tag, e.PointerTo)
				}
				continue
			}
			payload := e.RawExternal
			if payload == nil {
				payload = b.encodePayload(*e)
			}
			slot := int64(4)
			if b.big {
				slot = 8
			}
			if int64(len(payload)) <= slot && e.ExternalForced == 0 {
				continue
			}
			key := n.ID + "#" + fmt.Sprint(i)
			if e.ExternalForced != 0 {
				extOff[key] = e.ExternalForced // sparse / intentionally beyond EOF
				exts = append(exts, ext{key, payload, e.ExternalForced})
			} else {
				exts = append(exts, ext{key, payload, 0})
			}
		}
	}
	for _, ex := range exts {
		if ex.forced != 0 {
			continue // sparse: do not physically back forced offsets
		}
		cursor = align2(cursor)
		extOff[ex.key] = cursor
		cursor += int64(len(ex.data))
	}

	physicalEnd := cursor
	regions := append([]SparseRegion(nil), b.sparseRegions...)
	for _, n := range b.nodes {
		if n.OffsetForced != 0 {
			regions = append(regions, SparseRegion{
				Offset: n.OffsetForced,
				Data:   make([]byte, b.ifdSize(n)),
			})
		}
	}
	for _, r := range regions {
		if e := r.Offset + int64(len(r.Data)); e > physicalEnd {
			physicalEnd = e
		}
	}
	buf := make([]byte, physicalEnd)
	// Header.
	copy(buf[0:2], b.order.marker())
	if b.big {
		ord.PutUint16(buf[2:4], 43)
		ord.PutUint16(buf[4:6], 8)
		ord.PutUint16(buf[6:8], 0)
		if n0 := b.nodes[0]; n0 != nil {
			ord.PutUint64(buf[8:16], uint64(offOf[n0.ID]))
		}
	} else {
		ord.PutUint16(buf[2:4], 42)
		if n0 := b.nodes[0]; n0 != nil {
			ord.PutUint32(buf[4:8], uint32(offOf[n0.ID]))
		}
	}

	// IFDs.
	for _, n := range b.nodes {
		base := offOf[n.ID]
		if b.big {
			ord.PutUint64(buf[base:base+8], uint64(len(n.Entries)))
		} else {
			ord.PutUint16(buf[base:base+2], uint16(len(n.Entries)))
		}
		es := b.entryLen()
		countLen := int64(2)
		if b.big {
			countLen = 8
		}
		for i := range n.Entries {
			e := &n.Entries[i]
			at := base + countLen + int64(i)*es
			ord.PutUint16(buf[at:at+2], e.Tag)
			typ := e.Type
			ord.PutUint16(buf[at+2:at+4], typ)
			count := b.countOf(*e)
			if b.big {
				ord.PutUint64(buf[at+4:at+12], count)
			} else {
				ord.PutUint32(buf[at+4:at+8], uint32(count))
			}
			valAt := at + es - valSlot(b.big)
			var target int64
			if e.PointerTo != "" {
				target = offOf[e.PointerTo]
			} else {
				payload := e.RawExternal
				if payload == nil {
					payload = b.encodePayload(*e)
				}
				if int64(len(payload)) <= valSlot(b.big) && e.ExternalForced == 0 {
					copy(buf[valAt:valAt+int64(len(payload))], payload)
					continue
				}
				key := n.ID + "#" + fmt.Sprint(i)
				target = extOff[key]
				if target < int64(len(buf)) {
					copy(buf[target:], payload)
				}
			}
			if b.big {
				ord.PutUint64(buf[valAt:valAt+8], uint64(target))
			} else {
				if target > 0xFFFFFFFF {
					return nil, fmt.Errorf("node %q requires 64-bit offset but file is classic TIFF", n.ID)
				}
				ord.PutUint32(buf[valAt:valAt+4], uint32(target))
			}
		}
		nextPtr := base + countLen + int64(len(n.Entries))*es
		var next int64
		if n.NextForced != 0 {
			next = n.NextForced
		} else if n.Next != "" {
			next = offOf[n.Next]
		}
		if b.big {
			ord.PutUint64(buf[nextPtr:nextPtr+8], uint64(next))
		} else {
			ord.PutUint32(buf[nextPtr:nextPtr+4], uint32(next))
		}
	}

	// External forced slots beyond EOF stay omitted (parser must diagnose
	// out-of-bounds instead of crashing); in-range forced slots are copied.
	for _, ex := range exts {
		if off := extOff[ex.key]; off >= 0 && off+int64(len(ex.data)) <= int64(len(buf)) {
			copy(buf[off:], ex.data)
		}
	}
	for _, r := range regions {
		if r.Offset < 0 || r.Offset+int64(len(r.Data)) > int64(len(buf)) {
			continue
		}
		copy(buf[r.Offset:], r.Data)
	}
	return buf, nil
}

func valSlot(big bool) int64 {
	if big {
		return 8
	}
	return 4
}

func (b *Builder) ifdSize(n *Node) int64 {
	countLen := int64(2)
	ptrLen := int64(4)
	if b.big {
		countLen, ptrLen = 8, 8
	}
	return countLen + int64(len(n.Entries))*b.entryLen() + ptrLen
}

func align2(v int64) int64 { return (v + 1) &^ 1 }
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

var _ = binary.LittleEndian
var _ = bytes.MinRead
