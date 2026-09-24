package tiff

import (
	"encoding/hex"
	"fmt"
	"model"
)

const ParserAlgoVersion = "tiff-parse/1.0.0"

// Params controls the parser access budget.
type Params = model.ParseParams

// DefaultParams returns the fixed default access budget.
func DefaultParams() Params {
	p := Params{MaxIFDVisits: 32, MaxPointerSteps: 64, MaxBytes: 1 << 20, Version: 1}
	p.Fingerprint = Fingerprint(p)
	return p
}

// Fingerprint deterministically fingerprints a parameter set.
func Fingerprint(p Params) string {
	return fmt.Sprintf("visits=%d;steps=%d;bytes=%d", p.MaxIFDVisits, p.MaxPointerSteps, p.MaxBytes)
}

// Reject describes a blob that fails format validation before parsing.
type Reject struct {
	Reason      string
	ByteOrder   model.ByteOrder
	FragmentHex string
	Offset      int64
}

// ValidateHeader checks the 2-byte BOM + magic without building the graph.
func ValidateHeader(data []byte) (model.ByteOrder, *Reject) {
	if len(data) < 4 {
		return "", &Reject{Reason: "header_too_short", FragmentHex: hex.EncodeToString(data)}
	}
	var bo model.ByteOrder
	switch {
	case data[0] == 0x49 && data[1] == 0x49:
		bo = model.LittleEndian
	case data[0] == 0x4d && data[1] == 0x4d:
		bo = model.BigEndian
	default:
		return "", &Reject{Reason: "bad_byte_order_mark", FragmentHex: hex.EncodeToString(data[:min(8, len(data))]), Offset: 0}
	}
	r := newReader(data, bo)
	magic, ok := r.u16(2)
	if !ok {
		return bo, &Reject{Reason: "magic_unreadable", ByteOrder: bo, FragmentHex: r.hex(0, 4)}
	}
	switch magic {
	case 42, 43:
	default:
		return bo, &Reject{Reason: "bad_magic", ByteOrder: bo, FragmentHex: r.hex(0, 4), Offset: 2}
	}
	minLen := 8
	if magic == 43 {
		minLen = 16
	}
	if len(data) < minLen {
		return bo, &Reject{Reason: "header_truncated", ByteOrder: bo, FragmentHex: r.hex(0, minLen)}
	}
	return bo, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Parse builds the IFD graph. blobID and inputVersion anchor stable identities.
func Parse(data []byte, blobID model.StableID, inputVersion int, params Params) *model.ParseRun {
	bo, rej := ValidateHeader(data)
	run := &model.ParseRun{
		BlobID:       blobID,
		InputVersion: inputVersion,
		AlgoVersion:  ParserAlgoVersion,
	}
	params.Fingerprint = Fingerprint(params)
	run.ParamFingerprint = params.Fingerprint
	run.Budget.MaxIFDVisits = params.MaxIFDVisits
	run.Budget.MaxPointerSteps = params.MaxPointerSteps
	run.Budget.MaxBytes = params.MaxBytes

	if rej != nil {
		run.Header.ByteOrder = rej.ByteOrder
		run.Header.BlobBytes = len(data)
		run.ParseError = rej.Reason
		run.Partial = true
		run.Diagnostics = append(run.Diagnostics, model.Diagnosis{
			Code: "reject:" + rej.Reason, Severity: model.SevError,
			Message: "format error: " + rej.Reason,
			Detail: map[string]interface{}{"fragmentHex": rej.FragmentHex, "offset": rej.Offset},
		})
		return run
	}

	r := newReader(data, bo)
	magic, _ := r.u16(2)
	hdr := model.Header{ByteOrder: bo, Magic: magic, BlobBytes: len(data)}
	if magic == 42 {
		hdr.Class = model.Classic
		hdr.OffsetSize = 4
		hdr.EntrySize = 12
		hdr.SizeBytes = 8
		off, _ := r.u32(4)
		hdr.IFDOffset = uint64(off)
	} else {
		hdr.Class = model.BigTIFF
		hdr.OffsetSize = 8
		hdr.EntrySize = 20
		hdr.SizeBytes = 16
		// BigTIFF guard: bytesize-of-offset(2)=8 at +4, reserved(2)=0 at +6
		if b, ok := r.u16(4); ok {
			hdr.BigTIFFOffset = append(hdr.BigTIFFOffset, uint64(b))
		}
		if b, ok := r.u16(6); ok {
			hdr.BigTIFFOffset = append(hdr.BigTIFFOffset, uint64(b))
		}
		off, _ := r.u64(8) // IFD0 offset starts at byte 8
		hdr.IFDOffset = off
	}
	run.Header = hdr

	p := &parser{
		r:       r,
		run:     run,
		params:  params,
		nodes:   map[uint64]*model.IFDNode{},
		edges:   map[string]*model.IFDEdge{},
		inEdges: map[uint64][]string{},
	}

	// Root offset out of blob is explicit evidence, not a silent failure.
	if !r.in(int64(hdr.IFDOffset), 0) {
		run.Diagnostics = append(run.Diagnostics, model.Diagnosis{
			Code: "ifd0_out_of_blob", Severity: model.SevError,
			Message: fmt.Sprintf("IFD0 offset %d beyond blob length %d", hdr.IFDOffset, len(data)),
			Detail: map[string]interface{}{"offset": hdr.IFDOffset},
		})
		run.Partial = true
	} else {
		rootID := p.nodeID(hdr.IFDOffset)
		run.RootNodeID = rootID
		p.visit(hdr.IFDOffset, []uint64{}, []model.EdgeKind{})
	}

	run.Nodes = p.orderedNodes()
	run.Edges = p.orderedEdges()
	run.Cycles = p.cycleList()
	run.SharedNodes = p.sharedList()
	if run.Budget.Exhausted {
		run.Partial = true
	}
	run.RunID = model.HashID("run", blobID, inputVersion, ParserAlgoVersion, params.Fingerprint,
		run.Header, run.Budget, len(run.Nodes), len(run.Edges))
	return run
}

type parser struct {
	r       *reader
	run     *model.ParseRun
	params  Params
	nodes   map[uint64]*model.IFDNode
	edges   map[string]*model.IFDEdge
	inEdges map[uint64][]string
	order   []uint64
	edgeOrd []string
}

func (p *parser) nodeID(off uint64) model.StableID {
	return model.HashID("ifd", p.run.BlobID, off)
}

func (p *parser) edgeID(from, off uint64, tag uint16, order int) model.StableID {
	return model.HashID("edge", p.run.BlobID, from, tag, order, off)
}

func (p *parser) diag(code string, sev model.Severity, msg string, detail map[string]interface{}) {
	p.run.Diagnostics = append(p.run.Diagnostics, model.Diagnosis{Code: code, Severity: sev, Message: msg, Detail: detail})
}

// walkFrame is one pending pointer traversal.
type walkFrame struct {
	offset uint64
	from   uint64 // source node offset; root uses itself convention via parent set
	fromOK bool
	tag    uint16
	kind   model.EdgeKind
	order  int
}
