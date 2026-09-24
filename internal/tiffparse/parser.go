package tiffparse

import (
	"encoding/binary"
	"fmt"
)

func (r *reader) discover(from, kind string, tagID uint16, index int, target uint64) {
	id := linkID(from, kind, target, kind, tagID, index)
	toID := nodeID(r.sourceID, target)
	if target == 0 {
		r.links = append(r.links, IFDLink{ID: id, From: from, To: toID, Kind: kind, Offset: target, TagID: tagID, Index: index})
		r.diag("zero_ifd_pointer", "warning", "IFD pointer is zero", target, 0, toID, tagID, id)
		return
	}
	if r.open[target] {
		r.links = append(r.links, IFDLink{ID: id, From: from, To: toID, Kind: kind, Offset: target, TagID: tagID, Index: index, Cycle: true})
		r.diag("ifd_cycle", "error", fmt.Sprintf("IFD link closes a cycle to open IFD at offset %d", target), target, 0, toID, tagID, id)
		return
	}
	if r.visited[target] {
		r.links = append(r.links, IFDLink{ID: id, From: from, To: toID, Kind: kind, Offset: target, TagID: tagID, Index: index, Shared: true})
		r.diag("shared_ifd", "info", fmt.Sprintf("IFD at offset %d is reached by more than one path", target), target, 0, toID, tagID, id)
		return
	}
	if r.budget.NodesVisited >= r.budget.NodeLimit {
		r.budget.Exhausted = true
		r.budget.ExhaustedKind = "ifd_nodes"
		r.budget.ExhaustedAtLink = id
		r.links = append(r.links, IFDLink{ID: id, From: from, To: toID, Kind: kind, Offset: target, TagID: tagID, Index: index, BlockedByBudget: true})
		r.diag("ifd_budget_exhausted", "error", fmt.Sprintf("IFD access budget of %d nodes was exhausted before opening offset %d", r.budget.NodeLimit, target), target, 0, toID, tagID, id)
		return
	}
	r.links = append(r.links, IFDLink{ID: id, From: from, To: toID, Kind: kind, Offset: target, TagID: tagID, Index: index})
	r.visit(target, kind, id)
}

func (r *reader) visit(offset uint64, kind, reachedBy string) {
	r.budget.NodesVisited++
	r.visited[offset] = true
	r.open[offset] = true
	id := nodeID(r.sourceID, offset)
	node := IFDNode{ID: id, Offset: offset, Kind: kind, FirstSeenFrom: reachedBy}
	nodeIndex := len(r.ifds)
	r.nodes[offset] = &node
	r.ifds = append(r.ifds, node)
	r.parseIFD(offset, id, nodeIndex)
	delete(r.open, offset)
}

func (r *reader) parseIFD(offset uint64, id string, nodeIndex int) {
	entrySize := uint64(12)
	countSize := uint64(2)
	nextSize := uint64(4)
	pointerSize := uint64(4)
	if r.format == BigTIFF {
		entrySize = 20
		countSize = 8
		nextSize = 8
		pointerSize = 8
	}
	countBytes, ok := r.slice(offset, countSize, "ifd_count")
	if !ok {
		r.ifds[nodeIndex].Bytes = ByteRange{offset, countSize}
		r.ifds[nodeIndex].EntryCount = 0
		r.diag("unparsed_ifd", "error", "IFD entry count is outside submitted bytes", offset, countSize, id, 0, "")
		return
	}
	count := uint64(0)
	if r.format == BigTIFF {
		count = r.order.Uint64(countBytes)
	} else {
		count = uint64(r.order.Uint16(countBytes))
	}
	length := countSize + count*entrySize + nextSize
	raw, ok := r.slice(offset, length, "ifd")
	if !ok {
		r.ifds[nodeIndex].Bytes = ByteRange{offset, length}
		r.ifds[nodeIndex].EntryCount = count
		r.diag("truncated_ifd", "error", "IFD entries or next pointer are outside submitted bytes", offset, length, id, 0, "")
		return
	}
	r.ifds[nodeIndex].Bytes = ByteRange{offset, length}
	r.ifds[nodeIndex].EntryCount = count
	for i := uint64(0); i < count; i++ {
		entryStart := offset + countSize + i*entrySize
		tag := r.order.Uint16(raw[countSize+i*entrySize:])
		typeID := r.order.Uint16(raw[countSize+i*entrySize+2:])
		var valueCount uint64
		var fieldOffset uint64
		if r.format == BigTIFF {
			valueCount = r.order.Uint64(raw[countSize+i*entrySize+4:])
			fieldOffset = countSize + i*entrySize + 12
		} else {
			valueCount = uint64(r.order.Uint32(raw[countSize+i*entrySize+4:]))
			fieldOffset = countSize + i*entrySize + 8
		}
		fieldBytes := append([]byte(nil), raw[fieldOffset:fieldOffset+pointerSize]...)
		parsed := RawTag{
			ID: tag, Name: tagName(tag), TypeID: typeID, TypeName: typeName(typeID),
			Count: valueCount, ValueFieldBytes: fieldBytes, Inline: true,
		}
		elementSize, knownType := typeSizes[typeID]
		if !knownType {
			parsed.Truncated = true
			r.diag("unknown_tag_type", "warning", fmt.Sprintf("tag %d uses unknown TIFF type %d; raw value field retained", tag, typeID), entryStart, entrySize, id, tag, "")
		} else {
			total := elementSize * valueCount
			if total == 0 {
				r.diag("zero_tag_count", "warning", fmt.Sprintf("tag %d has count zero", tag), entryStart, entrySize, id, tag, "")
			} else if total <= pointerSize {
				parsed.ValueBytes = fieldBytes[:total]
				parsed.Values = r.numericValues(parsed.ValueBytes, typeID, valueCount)
			} else {
				if r.budget.ExternalReads >= r.budget.ExternalLimit {
					r.budget.Exhausted = true
					r.budget.ExhaustedKind = "external_values"
					r.budget.ExhaustedAtLink = fmt.Sprintf("%s/tag%d", id, tag)
					parsed.Inline = false
					parsed.Truncated = true
					if r.format == BigTIFF {
						parsed.ExternalOffset = r.order.Uint64(fieldBytes)
					} else {
						parsed.ExternalOffset = uint64(r.order.Uint32(fieldBytes))
					}
					parsed.ExternalLength = total
					r.diag("external_budget_exhausted", "error", fmt.Sprintf("external value access budget exhausted before reading tag %d", tag), parsed.ExternalOffset, total, id, tag, "")
				} else {
					externalOffset := uint64(0)
					if r.format == BigTIFF {
						externalOffset = r.order.Uint64(fieldBytes)
					} else {
						externalOffset = uint64(r.order.Uint32(fieldBytes))
					}
					valueBytes, valid := r.slice(externalOffset, total, "tag_value")
					parsed.Inline = false
					parsed.ExternalOffset = externalOffset
					parsed.ExternalLength = total
					if valid {
						r.budget.ExternalReads++
						parsed.ValueBytes = append([]byte(nil), valueBytes...)
						parsed.Values = r.numericValues(valueBytes, typeID, valueCount)
					} else {
						parsed.Truncated = true
						r.diag("truncated_external_tag_value", "error", fmt.Sprintf("tag %d external value is outside submitted bytes", tag), externalOffset, total, id, tag, "")
					}
				}
			}
		}
		r.ifds[nodeIndex].Tags = append(r.ifds[nodeIndex].Tags, parsed)
	}
	nextRaw := raw[countSize+count*entrySize : countSize+count*entrySize+nextSize]
	nextOffset := uint64(0)
	if r.format == BigTIFF {
		nextOffset = r.order.Uint64(nextRaw)
	} else {
		nextOffset = uint64(r.order.Uint32(nextRaw))
	}
	r.ifds[nodeIndex].NextIFD = nextOffset
	r.collectPointers(nodeIndex, id)
	if nextOffset != 0 {
		r.discover(id, "next", 0, -1, nextOffset)
	}
}

func (r *reader) numericValues(data []byte, typeID uint16, count uint64) []uint64 {
	values := []uint64{}
	pos := uint64(0)
	for i := uint64(0); i < count; i++ {
		size := typeSizes[typeID]
		if typeID == 5 || typeID == 10 {
			if pos+size > uint64(len(data)) {
				return values
			}
			values = append(values, uint64(r.order.Uint32(data[pos:])), uint64(r.order.Uint32(data[pos+4:])))
			pos += size
			continue
		}
		if pos+size > uint64(len(data)) {
			return values
		}
		switch typeID {
		case 1, 2, 6, 7:
			values = append(values, uint64(data[pos]))
		case 3:
			values = append(values, uint64(r.order.Uint16(data[pos:])))
		case 4, 13:
			values = append(values, uint64(r.order.Uint32(data[pos:])))
		case 16, 18:
			values = append(values, r.order.Uint64(data[pos:]))
		case 8:
			values = append(values, uint64(int16(r.order.Uint16(data[pos:]))))
		case 9:
			values = append(values, uint64(int32(r.order.Uint32(data[pos:]))))
		case 17:
			values = append(values, uint64(int64(r.order.Uint64(data[pos:]))))
		}
		pos += size
	}
	return values
}

func (r *reader) collectPointers(nodeIndex int, id string) {
	for tagIndex := range r.ifds[nodeIndex].Tags {
		tag := &r.ifds[nodeIndex].Tags[tagIndex]
		if tag.ID != 330 {
			continue
		}
		r.ifds[nodeIndex].SubIFDs = append(r.ifds[nodeIndex].SubIFDs, tag.Values...)
		for index, target := range tag.Values {
			r.discover(id, "subifd", tag.ID, index, target)
		}
	}
}

var _ binary.ByteOrder
