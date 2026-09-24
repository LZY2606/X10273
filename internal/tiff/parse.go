package tiff

import (
	"encoding/binary"
	"fmt"
)

// reader is a bounds-checked, byte-order-aware cursor over the TIFF bytes.
type reader struct {
	data []byte
	bo   binary.ByteOrder
}

func (r *reader) u16(off int64) (uint16, bool) {
	if off < 0 || off+2 > int64(len(r.data)) {
		return 0, false
	}
	return r.bo.Uint16(r.data[off : off+2]), true
}

func (r *reader) u32(off int64) (uint32, bool) {
	if off < 0 || off+4 > int64(len(r.data)) {
		return 0, false
	}
	return r.bo.Uint32(r.data[off : off+4]), true
}

func (r *reader) u64(off int64) (uint64, bool) {
	if off < 0 || off+8 > int64(len(r.data)) {
		return 0, false
	}
	return r.bo.Uint64(r.data[off : off+8]), true
}

func (r *reader) slice(off, length int64) ([]byte, bool) {
	if off < 0 || length < 0 || off+length > int64(len(r.data)) || off+length < off {
		return nil, false
	}
	return r.data[off : off+length], true
}

// Parse parses TIFF/GeoTIFF bytes under the supplied access budget.
func Parse(data []byte, opts ParseOptions) (*Graph, error) {
	if opts.NodeBudget <= 0 {
		opts = DefaultParseOptions()
	}
	if opts.BytesBudget <= 0 {
		opts.BytesBudget = DefaultParseOptions().BytesBudget
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("input shorter than 8 bytes (%d)", len(data))
	}
	g := &Graph{FileSize: int64(len(data)), NodeBudget: opts.NodeBudget, BytesBudget: opts.BytesBudget}

	var bo binary.ByteOrder
	switch {
	case data[0] == 'I' && data[1] == 'I':
		bo = binary.LittleEndian
		g.ByteOrder = LittleEndian
	case data[0] == 'M' && data[1] == 'M':
		bo = binary.BigEndian
		g.ByteOrder = BigEndian
	default:
		return nil, fmt.Errorf("bad byte-order-mark %02x %02x", data[0], data[1])
	}
	g.BOM = []byte{data[0], data[1]}
	r := &reader{data: data, bo: bo}

	magic, ok := r.u16(2)
	if !ok {
		return nil, fmt.Errorf("truncated magic")
	}
	g.Magic = magic
	switch magic {
	case 42:
		g.Variant = Classic
		g.HeaderLen = 8
		off32, ok := r.u32(4)
		if !ok {
			return nil, fmt.Errorf("truncated first IFD offset")
		}
		g.FirstIFD = int64(off32)
	case 43:
		g.Variant = BigTIFF
		g.HeaderLen = 16
		if z, ok := r.u16(6); !ok || z != 0 {
			g.Findings = append(g.Findings, Finding{
				Code: FindingBadMagic, Severity: SeverityError,
				Message: fmt.Sprintf("BigTIFF offset-size word is %d, want 0", z),
			})
		}
		off64, ok := r.u64(8)
		if !ok {
			return nil, fmt.Errorf("truncated BigTIFF first IFD offset")
		}
		g.FirstIFD = int64(off64)
	default:
		return g, fmt.Errorf("unknown TIFF magic %d", magic)
	}

	p := &parser{r: r, g: g, opts: opts, nodes: map[int64]*IFDNode{}}
	p.walk(g.FirstIFD, "", "root", 0, map[string]bool{})
	return g, nil
}

type parser struct {
	r     *reader
	g     *Graph
	opts  ParseOptions
	nodes map[int64]*IFDNode
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func (p *parser) findCycle(id, parent, via string, offset int64, path []string) {
	p.g.Findings = append(p.g.Findings, Finding{
		Code: FindingIFDCycle, Severity: SeverityError,
		IFDID: id, Offset: offset,
		Message: fmt.Sprintf("IFD cycle: %s reached again from %s via %s", id, parent, via),
		Detail: map[string]any{"ifd": id, "parent": parent, "via": via, "path": append([]string{}, path...)},
	})
}

// walk visits the IFD at offset reached from parent through via. ancestors is
// the DFS stack of node ids on the current path, used to separate cycles from
// shared (diamond) sub-IFDs.
func (p *parser) walk(offset int64, parent, via string, depth int, ancestors map[string]bool) {
	if p.g.NodeVisits >= p.opts.NodeBudget {
		p.g.BudgetExhausted = true
		p.g.Findings = append(p.g.Findings, Finding{
			Code: FindingBudgetExhausted, Severity: SeverityError, Offset: offset,
			Message: fmt.Sprintf("node visit budget %d exhausted at offset %d (via %s from %s)",
				p.opts.NodeBudget, offset, via, parent),
			Detail: map[string]any{
				"budget": p.opts.NodeBudget, "visits": p.g.NodeVisits,
				"via": via, "parent": parent, "depth": depth,
			},
		})
		return
	}
	p.g.NodeVisits++

	if offset < 0 || offset >= p.g.FileSize {
		p.g.Findings = append(p.g.Findings, Finding{
			Code: FindingWideOffsetOutOfRange, Severity: SeverityError, Offset: offset,
			Message: fmt.Sprintf("IFD offset %d outside file of %d bytes", offset, p.g.FileSize),
			Detail: map[string]any{"file_size": p.g.FileSize, "via": via, "parent": parent},
		})
		return
	}

	id := nodeID("", offset)
	if existing, seen := p.nodes[offset]; seen {
		if ancestors[id] || parent == id {
			p.findCycle(id, parent, via, offset, mapKeys(ancestors))
		}
		if parent != "" && !contains(existing.Parents, parent) {
			existing.Parents = append(existing.Parents, parent)
			p.g.Findings = append(p.g.Findings, Finding{
				Code: FindingSharedSubIFD, Severity: SeverityWarning,
				IFDID: existing.ID, Offset: offset,
				Message: fmt.Sprintf("shared sub-IFD %s reached from %s via %s", existing.ID, parent, via),
				Detail: map[string]any{"ifd": existing.ID, "parents": append([]string{}, existing.Parents...), "via": via},
			})
		}
		return
	}

	node := &IFDNode{ID: id, Offset: offset, Index: len(p.g.Nodes)}
	if parent != "" {
		node.Parents = append(node.Parents, parent)
	}
	p.nodes[offset] = node
	p.g.Nodes = append(p.g.Nodes, node)

	childAncestors := cloneSet(ancestors)
	childAncestors[id] = true
	p.readIFD(node, depth, childAncestors)
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func cloneSet(m map[string]bool) map[string]bool {
	c := make(map[string]bool, len(m)+1)
	for k := range m {
		c[k] = true
	}
	return c
}

var _ = fmt.Sprintf
