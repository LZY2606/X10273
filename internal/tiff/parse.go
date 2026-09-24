package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Parse reads a TIFF/BigTIFF byte stream under the given access budget. The
// bytes are never modified and malformed regions only add findings.
func Parse(data []byte, budget Budget) *Result {
	res := &Result{Budget: budget}
	p := &parser{data: data, res: res}

	hdr, err := p.parseHeader()
	res.Header = hdr
	if err != nil {
		res.add(Finding{Code: "header_invalid", Severity: SevError, Message: err.Error()})
		return res
	}

	// Traversal state.
	p.byOffset = map[uint64]int{}
	p.parentCount = map[uint64]int{}
	p.queue = []link{{from: "", to: hdr.FirstIFD, tag: 0, kind: "next"}}
	p.queueKind = map[linkKey]bool{}

	for len(p.queue) > 0 {
		lk := p.queue[0]
		p.queue = p.queue[1:]
		key := linkKey{from: lk.from, to: lk.to, tag: lk.tag}
		if p.queueKind[key] {
			delete(p.queueKind, key)
		}
		if p.budgetExhausted(lk) {
			break
		}
		if idx, seen := p.byOffset[lk.to]; seen {
			p.onShared(lk, idx)
			continue
		}
		p.visit(lk)
	}
	if res.BudgetSpent {
		// already recorded
	}
	res.IFDVisits = p.visits
	res.BytesRead = p.bytesRead
	return res
}

type link struct {
	from string
	to   uint64
	tag  uint16
	kind string
}

type linkKey struct {
	from string
	to   uint64
	tag  uint16
}

type parser struct {
	data        []byte
	bo          binary.ByteOrder
	res         *Result
	byOffset    map[uint64]int // IFD offset -> ordinal
	parentCount map[uint64]int
	parents     map[uint64][]string
	queue       []link
	queueKind   map[linkKey]bool
	visits      int
	bytesRead   int
}

func (p *parser) add(f Finding) { p.res.Findings = append(p.res.Findings, f) }

func (r *Result) add(f Finding) { r.Findings = append(r.Findings, f) }
