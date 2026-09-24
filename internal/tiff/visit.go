package tiff

import (
	"fmt"
	"model"
	"sort"
)

// visit performs an iterative, budget-bounded DFS of the IFD graph.
// start is an offset inside the blob (checked by the caller).
func (p *parser) visit(start uint64, _ []uint64, _ []model.EdgeKind) {
	type frame struct {
		offset uint64
		from   uint64
		hasFrom bool
		tag    uint16
		kind   model.EdgeKind
		edgeOrd int
		path   []uint64 // node offsets currently on the DFS stack
	}
	stack := []frame{{offset: start, path: []uint64{start}}}
	// Track per-offset pending edges so a node can be shared by many parents.
	nextEdgeOrd := 0

	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// Back-edge: close a cycle.
		for _, on := range f.path {
			if on == f.offset && !(len(f.path) == 1 && f.path[0] == f.offset && !f.hasFrom) {
				if f.hasFrom {
					p.recordEdge(f.from, f.offset, f.tag, f.kind, f.edgeOrd, true)
					p.run.Cycles = appendCycle(p.run.Cycles, append(append([]uint64(nil), f.path...), f.offset))
					p.bumpSteps()
				}
				goto done
			}
		}

		if _, seen := p.nodes[f.offset]; seen {
			// Shared subdirectory: node already fully parsed.
			if f.hasFrom {
				p.recordEdge(f.from, f.offset, f.tag, f.kind, f.edgeOrd, false)
				n := p.nodes[f.offset]
				n.Shared = true
				p.bumpSteps()
			}
			goto done
		}

		if p.budgetVisit(f.offset) {
			goto done
		}

		node := p.parseIFD(f.offset)
		node.Reachable = true
		p.nodes[f.offset] = node
		p.order = append(p.order, f.offset)
		if f.hasFrom {
			p.recordEdge(f.from, f.offset, f.tag, f.kind, f.edgeOrd, false)
		}

		if p.run.Budget.Exhausted {
			goto done
		}

		// Collect child pointers in tag order; reverse-push so DFS keeps order.
		type ptr struct {
			off  uint64
			tag  uint16
			kind model.EdgeKind
			ord  int
		}
		var ptrs []ptr
		ord := 0
		for _, e := range node.Entries {
			if subIFDPointerTags[e.Tag] {
				for _, v := range e.Values {
					if u, ok := toOffset(v, p.run.Header.Class); ok && u != 0 {
						ptrs = append(ptrs, ptr{u, e.Tag, ptrKind(e.Tag), ord})
						ord++
					}
				}
			}
		}
		// Next IFD pointer edge gets the first ordinal slot conceptually;
		// to preserve raw layout order we give it ord -1 encoded as tag 0.
		nextPath := append(append([]uint64(nil), f.path...), f.offset)
		// Push subifd children first (reversed), then next (popped last -> next traversed first).
		for i := len(ptrs) - 1; i >= 0; i-- {
			if p.run.Budget.Exhausted {
				break
			}
			cp := ptrs[i]
			nextEdgeOrd++
			if !p.r.in(int64(cp.off), 0) {
				p.recordDanglingEdge(f.offset, cp.off, cp.tag, cp.kind, nextEdgeOrd)
				continue
			}
			stack = append(stack, frame{offset: cp.off, from: f.offset, hasFrom: true,
				tag: cp.tag, kind: cp.kind, edgeOrd: nextEdgeOrd,
				path: append(append([]uint64(nil), nextPath...), cp.off)})
		}
		if node.NextPresent && node.NextOffset != 0 {
			nextEdgeOrd++
			if !p.r.in(int64(node.NextOffset), 0) {
				p.recordDanglingEdge(f.offset, node.NextOffset, 0, model.EdgeNext, nextEdgeOrd)
			} else {
				stack = append(stack, frame{offset: node.NextOffset, from: f.offset, hasFrom: true,
					tag: 0, kind: model.EdgeNext, edgeOrd: nextEdgeOrd,
					path: []uint64{f.offset, node.NextOffset}})
			}
		}
	done:
	}
}

func ptrKind(tag uint16) model.EdgeKind {
	switch tag {
	case 34665:
		return model.EdgeExif
	case 34853:
		return model.EdgeGPS
	default:
		return model.EdgeSubIFD
	}
}

// appendCycle stores a cycle signature once.
func appendCycle(cycles [][]model.StableID, pathOff []uint64) [][]model.StableID {
	sig := make([]model.StableID, len(pathOff))
	for i, o := range pathOff {
		sig[i] = model.HashID("ifd", "", o) // positional signature; blob prefix added later
	}
	// rotate to smallest for dedup
	rot := rotateSmallest(sig)
	for _, c := range cycles {
		if eqIDs(rotateSmallest(append([]model.StableID(nil), c...)), rot) {
			return cycles
		}
	}
	return append(cycles, sig)
}

func rotateSmallest(ids []model.StableID) []model.StableID {
	if len(ids) == 0 {
		return ids
	}
	best := 0
	for i := 1; i < len(ids); i++ {
		if ids[i] < ids[best] {
			best = i
		}
	}
	out := make([]model.StableID, 0, len(ids))
	out = append(out, ids[best:]...)
	out = append(out, ids[:best]...)
	return out
}

func eqIDs(a, b []model.StableID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toOffset(v interface{}, class model.TIFFClass) (uint64, bool) {
	switch t := v.(type) {
	case uint32:
		return uint64(t), true
	case uint64:
		return t, true
	case int64:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	}
	return 0, false
}

func (p *parser) budgetVisit(off uint64) bool {
	p.run.Budget.IFDVisits++
	if p.run.Budget.IFDVisits > p.params.MaxIFDVisits {
		p.run.Budget.Exhausted = true
		p.run.Budget.ExhaustedCode = "max_ifd_visits"
		p.diag("budget_exhausted", model.SevBudget,
			fmt.Sprintf("access budget exhausted: max_ifd_visits=%d", p.params.MaxIFDVisits),
			map[string]interface{}{"limit": p.params.MaxIFDVisits, "offset": off})
		return true
	}
	return false
}

func (p *parser) bumpSteps() {
	p.run.Budget.PointerSteps++
	if p.run.Budget.PointerSteps > p.params.MaxPointerSteps && !p.run.Budget.Exhausted {
		p.run.Budget.Exhausted = true
		p.run.Budget.ExhaustedCode = "max_pointer_steps"
		p.diag("budget_exhausted", model.SevBudget,
			fmt.Sprintf("access budget exhausted: max_pointer_steps=%d", p.params.MaxPointerSteps),
			map[string]interface{}{"limit": p.params.MaxPointerSteps})
	}
}

func (p *parser) recordEdge(from, to uint64, tag uint16, kind model.EdgeKind, ord int, cyclic bool) {
	key := fmt.Sprintf("%d:%d:%d:%d", from, to, tag, ord)
	if _, ok := p.edges[key]; ok {
		return
	}
	e := model.IFDEdge{
		EdgeID: p.edgeID(from, to, tag, ord),
		From:   p.nodeID(from), To: p.nodeID(to),
		Kind: kind, Offset: to, Order: ord, Cyclic: cyclic, SourceTag: tag,
	}
	p.edges[key] = &e
	p.edgeOrd = append(p.edgeOrd, key)
	p.inEdges[to] = append(p.inEdges[to], key)
	if len(p.inEdges[to]) > 1 {
		if n, ok := p.nodes[to]; ok {
			n.Shared = true
		}
	}
	p.bumpSteps()
}

func (p *parser) recordDanglingEdge(from, to uint64, tag uint16, kind model.EdgeKind, ord int) {
	p.recordEdge(from, to, tag, kind, ord, false)
	// Dangling target: create an unreachable marker node so the evidence is visible.
	if _, ok := p.nodes[to]; !ok {
		marker := &model.IFDNode{NodeID: p.nodeID(to), Offset: to, Reachable: false, OutOfBlob: true}
		marker.Diagnostics = append(marker.Diagnostics, model.Diagnosis{
			Code: "ifd_out_of_blob", Severity: model.SevError,
			Message: fmt.Sprintf("IFD pointer target %d exceeds blob length %d", to, p.r.len()),
		})
		p.nodes[to] = marker
		p.order = append(p.order, to)
	}
	p.diag("ifd_pointer_out_of_blob", model.SevError,
		fmt.Sprintf("%s pointer to offset %d is outside the blob", kind, to),
		map[string]interface{}{"from": from, "target": to, "tag": tag})
}

func (p *parser) orderedNodes() []model.IFDNode {
	out := make([]model.IFDNode, 0, len(p.order))
	for _, off := range p.order {
		out = append(out, *p.nodes[off])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Offset < out[j].Offset })
	for i := range out {
		out[i].Order = i
	}
	return out
}

func (p *parser) orderedEdges() []model.IFDEdge {
	out := make([]model.IFDEdge, 0, len(p.edgeOrd))
	for _, k := range p.edgeOrd {
		out = append(out, *p.edges[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Offset != out[j].Offset {
			return out[i].Offset < out[j].Offset
		}
		return out[i].Order < out[j].Order
	})
	return out
}

func (p *parser) cycleList() [][]model.StableID {
	out := make([][]model.StableID, 0, len(p.run.Cycles))
	for _, c := range p.run.Cycles {
		row := make([]model.StableID, len(c))
		for i, sig := range c {
			// Replace positional-only signature with blob-scoped node ids.
			off := extractOffset(sig)
			row[i] = p.nodeID(off)
		}
		row = rotateSmallest(row)
		out = append(out, row)
	}
	return out
}

func (p *parser) sharedList() []model.StableID {
	var out []model.StableID
	for off, keys := range p.inEdges {
		if len(keys) > 1 {
			out = append(out, p.nodeID(off))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
