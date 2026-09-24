package tiff

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Options controls parser behavior. BudgetVisits bounds the number of IFD
// visits so cyclic/shared directory graphs cannot cause unbounded work.
type Options struct {
	BudgetVisits int
}

// DefaultBudget is the access budget for a normal review import.
const DefaultBudget = 64

// Parse decodes a TIFF/GeoTIFF byte slice. A partially parseable document
// still returns a Document carrying findings; a document that is not TIFF
// at all returns an error.
func Parse(data []byte, opts Options) (*Document, error) {
	if opts.BudgetVisits <= 0 {
		opts.BudgetVisits = DefaultBudget
	}
	doc := &Document{
		FileSize:   uint64(len(data)),
		Blocks:     map[uint64]*Blocks{},
		GeoKeys:    map[uint64]*GeoKeyDirectory{},
		Transforms: map[uint64][]ModelTransform{},
	}
	if len(data) < 8 {
		return nil, errors.New("not a TIFF: fewer than 8 header bytes")
	}
	r := &reader{data: data}
	switch {
	case data[0] == 'I' && data[1] == 'I':
		r.bo = binary.LittleEndian
		doc.Header.ByteOrder = LittleEndian
	case data[0] == 'M' && data[1] == 'M':
		r.bo = binary.BigEndian
		doc.Header.ByteOrder = BigEndian
	default:
		return nil, fmt.Errorf("not a TIFF: byte order marker is %q (want II or MM)", string(data[0:2]))
	}
	magic, err := r.u16(2)
	if err != nil {
		return nil, err
	}
	doc.Header.Magic = magic
	switch magic {
	case 42:
		first, err := r.u32(4)
		if err != nil {
			return nil, err
		}
		doc.Header.FirstIFD = uint64(first)
		doc.Header.Range = Range{0, 8}
	case 43:
		if len(data) < 16 {
			return nil, errors.New("BigTIFF header truncated: need 16 bytes")
		}
		doc.Header.BigTIFF = true
		if doc.Header.BitsVersion, err = r.u16(4); err != nil {
			return nil, err
		}
		if doc.Header.Bytesize, err = r.u16(6); err != nil {
			return nil, err
		}
		if doc.Header.BitsVersion != 0 || doc.Header.Bytesize != 8 {
			doc.find("header", "error",
				fmt.Sprintf("BigTIFF header fields invalid: BitsVersion=%d Bytesize=%d",
					doc.Header.BitsVersion, doc.Header.Bytesize), nil)
		}
		// bytes 8-9 are reserved zeros.
		if data[8] != 0 || data[9] != 0 {
			doc.find("header", "warning", "BigTIFF reserved bytes are non-zero", &Range{8, 2})
		}
		if doc.Header.FirstIFD, err = r.u64(10); err != nil {
			return nil, err
		}
		doc.Header.Range = Range{0, 16}
		if doc.Header.FirstIFD == 0 {
			doc.find("header", "warning", "BigTIFF IFD8 offset is zero", nil)
		}
	default:
		return nil, fmt.Errorf("not a TIFF: magic %d (want 42 or 43)", magic)
	}

	doc.walkIFDs(r, opts)
	for _, d := range doc.IFDs {
		doc.Blocks[d.Offset] = parseBlocks(r, d, doc)
		if gk := parseGeoKeys(r, d, doc); gk != nil {
			doc.GeoKeys[d.Offset] = gk
		}
		doc.Transforms[d.Offset] = parseTransforms(r, d, doc, doc.GeoKeys[d.Offset])
	}
	doc.Budget.Unvisited = doc.finishBudget(opts)
	return doc, nil
}

func (doc *Document) find(cat, sev, msg string, rng *Range) {
	doc.Findings = append(doc.Findings, Finding{Category: cat, Severity: sev, Message: msg, Range: rng})
}

// walkIFDs performs a bounded DFS of the IFD graph starting at the header
// pointer, following SubIFDs (in order) then NextIFD. Three-color DFS
// distinguishes a back edge (IFD cycle, target gray) from an edge to a
// finished directory (shared subdirectory). Every declared link becomes an
// edge even when duplicated, so sharing through repeated SubIFDs entries
// stays visible. The visit budget bounds parsing of distinct directories.
func (doc *Document) walkIFDs(r *reader, opts Options) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	type link struct {
		from uint64
		to   uint64
		kind string
	}
	type frame struct {
		from   uint64
		offset uint64
		kind   string
		links  []link
		cursor int
	}
	color := map[uint64]int{}
	byOffset := map[uint64]*IFD{}

	stack := []frame{{from: 0, offset: doc.Header.FirstIFD, kind: "next"}}
	for len(stack) > 0 {
		fi := len(stack) - 1
		f := &stack[fi]
		if f.offset == 0 {
			stack = stack[:fi]
			continue
		}
		if color[f.offset] == white {
			if doc.Budget.Visits >= opts.BudgetVisits {
				doc.Budget.Exhausted = true
				doc.Budget.Truncated = true
				doc.find("budget", "error",
					fmt.Sprintf("IFD access budget %d exhausted before visiting IFD@%d; remaining graph not parsed",
						opts.BudgetVisits, f.offset), nil)
				doc.Budget.Unvisited = append(doc.Budget.Unvisited, f.offset)
				doc.Edges = append(doc.Edges, Edge{From: f.from, To: f.offset, Kind: f.kind})
				stack = stack[:fi]
				continue
			}
			d := &IFD{Offset: f.offset, Index: len(doc.IFDs), Visits: 1}
			byOffset[f.offset] = d
			color[f.offset] = gray
			doc.IFDs = append(doc.IFDs, d)
			doc.Budget.Visits++
			doc.Edges = append(doc.Edges, Edge{From: f.from, To: f.offset, Kind: f.kind})
			doc.parseOneIFD(r, d)
			for _, sub := range d.SubIFDs {
				f.links = append(f.links, link{d.Offset, sub, "subifd"})
			}
			if d.NextIFD != nil {
				f.links = append(f.links, link{d.Offset, *d.NextIFD, "next"})
			}
		}
		if f.cursor < len(f.links) {
			l := f.links[f.cursor]
			f.cursor++
			edge := Edge{From: l.from, To: l.to, Kind: l.kind}
			switch color[l.to] {
			case white:
				stack = append(stack, frame{from: l.from, offset: l.to, kind: l.kind})
			case gray:
				edge.Cycle = true
				if t := byOffset[l.to]; t != nil {
					t.OnCycle = true
				}
				doc.Edges = append(doc.Edges, edge)
			case black:
				if t := byOffset[l.to]; t != nil {
					t.Shared = true
					t.Visits++
				}
				doc.Edges = append(doc.Edges, edge)
			}
			continue
		}
		color[f.offset] = black
		stack = stack[:fi]
	}
}

func (doc *Document) finishBudget(opts Options) []uint64 {
	doc.Budget.Limit = opts.BudgetVisits
	return doc.Budget.Unvisited
}

func (doc *Document) parseOneIFD(r *reader, d *IFD) {
	big := doc.Header.BigTIFF
	entrySize := uint64(12)
	if big {
		entrySize = 20
	}
	countOff := d.Offset
	count, err := (func() (uint64, error) {
		if big {
			return r.u64(countOff)
		}
		v, e := r.u16(countOff)
		return uint64(v), e
	})()
	if err != nil {
		doc.find("ifd", "error", fmt.Sprintf("IFD@%d: %v", d.Offset, err), &Range{countOff, 0})
		return
	}
	d.EntryCount = count
	entriesStart := countOff + uint64(2)
	if big {
		entriesStart = countOff + 8
	}
	nextOff := entriesStart + count*entrySize
	d.Range = Range{d.Offset, nextOff - d.Offset + func() uint64 {
		if big {
			return 8
		}
		return 4
	}()}
	if err := r.errAt(d.Offset, d.Range.Length, "IFD"); err != nil {
		doc.find("ifd", "error", fmt.Sprintf("IFD@%d: %v", d.Offset, err), &Range{d.Offset, d.Range.Length})
	}
	for i := uint64(0); i < count; i++ {
		eo := entriesStart + i*entrySize
		code, err1 := r.u16(eo)
		typ, err2 := r.u16(eo + 2)
		if err1 != nil || err2 != nil {
			doc.find("ifd", "error", fmt.Sprintf("IFD@%d entry %d header unreadable", d.Offset, i),
				&Range{eo, entrySize})
			break
		}
		var cnt, valOff uint64
		if big {
			cnt, err = r.u64(eo + 4)
			if err == nil {
				valOff, err = r.u64(eo + 12)
			}
		} else {
			var c uint32
			c, err = r.u32(eo + 4)
			cnt = uint64(c)
			if err == nil {
				var v uint32
				v, err = r.u32(eo + 8)
				valOff = uint64(v)
			}
		}
		if err != nil {
			doc.find("ifd", "error", fmt.Sprintf("IFD@%d entry %d truncated", d.Offset, i),
				&Range{eo, entrySize})
			break
		}
		t := &Tag{
			Code: code, Name: TagName[code], Type: typ,
			TypeName: TypeName[typ], Count: cnt,
			EntryRange: Range{eo, entrySize},
		}
		size := uint64(TypeSize[typ])
		if typ == 0 || int(typ) >= len(TypeSize) || size == 0 {
			doc.find("ifd", "warning",
				fmt.Sprintf("tag %d has unknown type %d; raw entry retained", code, typ),
				&t.EntryRange)
			if !big {
				t.RawInline = append([]byte(nil), r.data[eo+8:eo+12]...)
			} else {
				t.RawInline = append([]byte(nil), r.data[eo+12:eo+20]...)
			}
			d.Tags = append(d.Tags, t)
			continue
		}
		total := size * cnt
		slot := uint64(4)
		if big {
			slot = 8
		}
		if total <= slot {
			t.Inline = true
			start := eo + func() uint64 {
				if big {
					return 12
				}
				return 8
			}()
			t.RawInline = append([]byte(nil), r.data[start:start+total]...)
		} else {
			t.Inline = false
			ext := Range{valOff, total}
			t.External = &ext
			if err := r.errAt(valOff, total, "external tag value"); err != nil {
				doc.find("offset", "error",
					fmt.Sprintf("tag %d (%s) external value out of range: %v", code, t.Name, err),
					&ext)
			} else {
				ints, flts, asc, derr := r.decodeValues(typ, cnt, r.data[valOff:valOff+total])
				if derr != nil {
					doc.find("ifd", "warning",
						fmt.Sprintf("tag %d decode failed: %v", code, derr), &ext)
				}
				t.Decoded, t.DecodedFloat, t.ASCII = ints, flts, asc
			}
		}
		if t.Inline {
			ints, flts, asc, derr := r.decodeValues(typ, cnt, t.RawInline)
			if derr != nil {
				doc.find("ifd", "warning",
					fmt.Sprintf("tag %d decode failed: %v", code, derr), &t.EntryRange)
			}
			t.Decoded, t.DecodedFloat, t.ASCII = ints, flts, asc
		}
		d.Tags = append(d.Tags, t)
		switch code {
		case TagSubIFDs:
			d.SubIFDs = append(d.SubIFDs, t.Decoded...)
		}
	}
	// Next IFD pointer lives after the entries.
	if err := r.errAt(nextOff, func() uint64 {
		if big {
			return 8
		}
		return 4
	}(), "next IFD pointer"); err != nil {
		doc.find("ifd", "error", fmt.Sprintf("IFD@%d next pointer: %v", d.Offset, err),
			&Range{nextOff, 0})
		return
	}
	var next uint64
	if big {
		next, _ = r.u64(nextOff)
	} else {
		v, _ := r.u32(nextOff)
		next = uint64(v)
	}
	if next != 0 {
		d.NextIFD = &next
	}
}
