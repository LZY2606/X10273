package tiff

import (
	"encoding/binary"
	"fmt"
	"ifdlantern/internal/digest"
	"ifdlantern/internal/domain"
)

const algoVersion = "1.0.0"

// Config is the parser parameter set. Its fingerprint marks every derived
// fact; changing it expires parser-derived results.
type Config struct {
	Budget Budget
}

func (c Config) Fingerprint() string {
	return digest.Hash([]byte(fmt.Sprintf("ifd-parse@%s|visits=%d|tagbytes=%d|grid=%d",
		algoVersion, c.Budget.MaxIFDVisits, c.Budget.MaxTagBytes, c.Budget.MaxGridCells)))[:16]
}

const (
	colorWhite = 0
	colorGray  = 1 // on the current DFS path (an ancestor)
	colorBlack = 2 // fully expanded
)

type dfsEdge struct {
	to     int64
	kind   string // next|sub
	viaTag int
}

type dfsFrame struct {
	node    *IFD
	nextChi int
}

// Parse walks every reachable IFD from the header, charging the budget on each
// directed edge. A gray target is a cycle back-edge; a black target reached
// from another path is a shared subdirectory (diamond). Neither can loop.
func Parse(src Source, cfg Config) (*ParseReport, error) {
	rep := &ParseReport{
		Layouts:  map[string]*BlockLayout{},
		Geos:     map[string]*GeoModel{},
		AlgoVer:  algoVersion,
		ParamFP:  cfg.Fingerprint(),
		SourceID: src.ID(),
	}
	hdr, bo, little, variant, first, err := parseHeader(src)
	if err != nil {
		return nil, err
	}
	rep.Header = hdr
	rep.Budget.Limit = cfg.Budget

	w := &walker{
		src: src, cfg: cfg, bo: bo, little: little, variant: variant,
		byOff: map[int64]*IFD{}, color: map[int64]int{}, report: rep,
	}

	var stack []*dfsFrame
	pushNode := func(parent *IFD, off int64, kind string, via int) bool {
		rep.Budget.Edges++
		rep.Budget.Visits++
		if rep.Budget.Visits > cfg.Budget.MaxIFDVisits {
			rep.Budget.Exhausted = true
			rep.Budget.StopAt = fmt.Sprintf("offset:%d", off)
			rep.Budget.StopReason = "max-ifd-visits"
			return false
		}
		if node, ok := w.byOff[off]; ok {
			addParent(node, parentID(parent))
			switch w.color[off] {
			case colorGray:
				node.CycleClose = node.ID // back edge -> cycle
				if parent != nil {
					rep.Diags = append(rep.Diags, domain.Diagnostic{
						Code: "ifd-cycle", Severity: domain.SevError,
						Message: fmt.Sprintf("IFD at offset %d closes a cycle back to itself", off),
						Evidence: []string{node.ID, parentID(parent)},
					})
				}
			case colorBlack:
				node.Shared = true // reached from a second path
				if parent != nil {
					rep.Diags = append(rep.Diags, domain.Diagnostic{
						Code: "ifd-shared-subdirectory", Severity: domain.SevWarning,
						Message: fmt.Sprintf("IFD at offset %d is a shared subdirectory", off),
						Evidence: []string{node.ID, parentID(parent)},
					})
				}
			}
			return true
		}
		node, err := w.readIFD(off)
		if err != nil {
			rep.Diags = append(rep.Diags, domain.Diagnostic{
				Code: "ifd-read-failed", Severity: domain.SevError,
				Message: err.Error(), Evidence: []string{src.ID()},
			})
			return true
		}
		addParent(node, parentID(parent))
		w.byOff[off] = node
		w.color[off] = colorGray
		rep.IFDs = append(rep.IFDs, node)
		stack = append(stack, &dfsFrame{node: node})
		return true
	}

	if !pushNode(nil, first, "root", 0) {
		w.finish()
		return rep, nil
	}
	for len(stack) > 0 {
		fr := stack[len(stack)-1]
		children := w.children(fr.node)
		if fr.nextChi >= len(children) {
			w.color[fr.node.Offset] = colorBlack
			stack = stack[:len(stack)-1]
			continue
		}
		e := children[fr.nextChi]
		fr.nextChi++
		if !pushNode(fr.node, e.to, e.kind, e.viaTag) {
			break
		}
	}
	if rep.Budget.Exhausted {
		rep.Diags = append(rep.Diags, domain.Diagnostic{
			Code: "budget-exhausted", Severity: domain.SevBudget,
			Message: fmt.Sprintf("IFD visit budget %d exhausted near %s",
				cfg.Budget.MaxIFDVisits, rep.Budget.StopAt),
			Evidence: []string{"budget:" + cfg.Fingerprint()},
		})
	}
	w.finish()
	return rep, nil
}

func (w *walker) finish() {
	for _, node := range w.report.IFDs {
		w.report.Layouts[node.ID] = analyzeBlocks(node, w.cfg.Budget, w.src.Size())
		w.report.Geos[node.ID] = analyzeGeo(node, w.report.Header, w.src)
	}
}

func parentID(p *IFD) string {
	if p == nil {
		return ""
	}
	return p.ID
}

func addParent(node *IFD, pid string) {
	if pid == "" {
		return
	}
	for _, p := range node.Parents {
		if p == pid {
			return
		}
	}
	node.Parents = append(node.Parents, pid)
}

func (w *walker) children(node *IFD) []dfsEdge {
	var out []dfsEdge
	for _, p := range node.SubPtrs {
		out = append(out, dfsEdge{to: p, kind: "sub", viaTag: 330})
	}
	if node.Next != 0 {
		out = append(out, dfsEdge{to: node.Next, kind: "next", viaTag: 0})
	}
	return out
}

type walker struct {
	src     Source
	cfg     Config
	bo      binary.ByteOrder
	little  bool
	variant string
	byOff   map[int64]*IFD
	color   map[int64]int
	report  *ParseReport
}
