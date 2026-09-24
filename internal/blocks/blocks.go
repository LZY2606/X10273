// Package blocks derives tile/strip block maps from retained tag values.
// Overlap, missing and out-of-bounds states are emitted explicitly: the layer
// never fabricates bytes or merges blocks to make the picture look complete.
package blocks

import (
	"fmt"
	"ifdroom/internal/event"
	"ifdroom/internal/tiff"
)

type Kind string

const (
	Tile  Kind = "tile"
	Strip Kind = "strip"
)

type State string

const (
	StateOK      State = "present"
	StateMissing State = "missing"
	StateOOBCount State = "bytecount_out_of_bounds"
	StateZeroSize State = "zero_size"
)

// Block is one pixel-data region, addressed in both grid coordinates and the
// source byte range.
type Block struct {
	ID       string          `json:"id"`
	Kind     Kind            `json:"kind"`
	Index    int             `json:"index"`
	Col      int             `json:"col"`
	Row      int             `json:"row"`
	Offset   int64           `json:"offset"`
	Count    int64           `json:"count"`
	Range    event.ByteRange `json:"range"`
	Expected bool            `json:"expected"`
	State    State           `json:"state"`
	// Overlaps lists ids of other blocks whose byte ranges intersect this one.
	Overlaps []string `json:"overlaps"`
}

type Layout struct {
	IFDID  string   `json:"ifd_id"`
	Kind   Kind     `json:"kind"`
	Width  int64    `json:"width"`
	Length int64    `json:"length"`
	TileW  int64    `json:"tile_width"`
	TileH  int64    `json:"tile_length"`
	Cols   int      `json:"cols"`
	Rows   int      `json:"rows"`
	Blocks []*Block `json:"blocks"`
	// Missing grid cells keyed "col,row": explicit holes, never filled in.
	MissingCells []string          `json:"missing_cells"`
	OverlapPairs [][2]string       `json:"overlap_pairs"`
	Diagnostics  []*tiff.Diagnostic `json:"diagnostics"`
}

type Measured struct {
	Value int64  `json:"value"`
	Unit  string `json:"unit"`
}

// tagInts returns all integer values of a tag in their original order.
func tagInts(t *tiff.Tag) []int64 {
	var out []int64
	for _, v := range t.Values {
		switch x := v.(type) {
		case uint64:
			out = append(out, int64(x))
		case int64:
			out = append(out, x)
		}
	}
	return out
}

func findTag(ifd *tiff.IFD, id uint16) *tiff.Tag {
	for _, t := range ifd.Tags {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func firstInt(ifd *tiff.IFD, id uint16) (int64, bool) {
	if t := findTag(ifd, id); t != nil && len(t.Values) > 0 {
		if vs := tagInts(t); len(vs) > 0 {
			return vs[0], true
		}
	}
	return 0, false
}

func dg(code string, sev tiff.Severity, msg string, br event.ByteRange) *tiff.Diagnostic {
	return &tiff.Diagnostic{Code: code, Severity: sev, Message: msg, Range: br, IFDID: ""}
}

func divCeil(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// Derive computes the block map for one IFD. An absent/malformed storage
// signature yields a Layout carrying diagnostics rather than an error.
func Derive(ifd *tiff.IFD, fileSize int64) *Layout {
	ly := &Layout{IFDID: ifd.ID}
	w, okW := firstInt(ifd, tiff.TagImageWidth)
	l, okL := firstInt(ifd, tiff.TagImageLength)
	if okW {
		ly.Width = w
	}
	if okL {
		ly.Length = l
	}
	if offTag := findTag(ifd, tiff.TagTileOffsets); offTag != nil {
		ly.Kind = Tile
		tw, okTW := firstInt(ifd, tiff.TagTileWidth)
		th, okTH := firstInt(ifd, tiff.TagTileLength)
		if !okTW || !okTH || tw <= 0 || th <= 0 {
			ly.Diagnostics = append(ly.Diagnostics, dg("tile_dims_missing", tiff.SevError,
				"tiled IFD lacks positive TileWidth/TileLength", event.ByteRange{}))
		}
		ly.TileW, ly.TileH = tw, th
		if okW && okL && tw > 0 && th > 0 {
			ly.Cols = int(divCeil(w, tw))
			ly.Rows = int(divCeil(l, th))
		}
		ly.deriveGrid(ifd, offTag, tiff.TagTileByteCounts, fileSize)
		return ly
	}
	if offTag := findTag(ifd, tiff.TagStripOffsets); offTag != nil {
		ly.Kind = Strip
		rps := int64(l)
		if v, ok := firstInt(ifd, tiff.TagRowsPerStrip); ok && v > 0 {
			rps = v
		}
		if !okL || l <= 0 {
			ly.Diagnostics = append(ly.Diagnostics, dg("strip_length_missing", tiff.SevWarning,
				"stripped IFD lacks positive ImageLength; rows per strip unknown", event.ByteRange{}))
		}
		ly.TileH = rps
		if okL && rps > 0 {
			ly.Cols = 1
			ly.Rows = int(divCeil(l, rps))
		}
		ly.deriveGrid(ifd, offTag, tiff.TagStripByteCounts, fileSize)
		return ly
	}
	ly.Diagnostics = append(ly.Diagnostics, dg("no_pixel_offsets", tiff.SevWarning,
		"IFD contains neither TileOffsets nor StripOffsets; no pixel blocks", event.ByteRange{}))
	return ly
}

func (ly *Layout) deriveGrid(ifd *tiff.IFD, offTag *tiff.Tag, countTagID uint16, fileSize int64) {
	offsets := tagInts(offTag)
	var counts []int64
	if ct := findTag(ifd, countTagID); ct != nil {
		counts = tagInts(ct)
	}
	expected := ly.Cols * ly.Rows
	if expected <= 0 {
		expected = len(offsets)
	}
	grid := map[string]bool{}
	for i := 0; i < expected; i++ {
		col, row := i, 0
		if ly.Kind == Tile && ly.Cols > 0 {
			col, row = i%ly.Cols, i/ly.Cols
		}
		id := fmt.Sprintf("%s:%s#%d", ly.IFDID, ly.Kind, i)
		b := &Block{ID: id, Kind: ly.Kind, Index: i, Col: col, Row: row, Expected: true}
		if i >= len(offsets) {
			b.State = StateMissing
			b.Range = event.ByteRange{}
			ly.MissingCells = append(ly.MissingCells, cellKey(col, row))
			ly.Diagnostics = append(ly.Diagnostics, dg("block_missing", tiff.SevError,
				fmt.Sprintf("%s %d (grid c%d,r%d) has no offset entry", ly.Kind, i, col, row), event.ByteRange{}))
		} else {
			b.Offset = offsets[i]
			if i < len(counts) {
				b.Count = counts[i]
			}
			b.Range = event.ByteRange{Lo: b.Offset, Hi: b.Offset + b.Count}
			switch {
			case b.Count == 0:
				b.State = StateZeroSize
				ly.Diagnostics = append(ly.Diagnostics, dg("block_zero_size", tiff.SevWarning,
					fmt.Sprintf("%s %d has zero byte count", ly.Kind, i), b.Range))
			case b.Offset < 0 || b.Offset+b.Count > fileSize || b.Offset >= fileSize:
				b.State = StateOOBCount
				ly.Diagnostics = append(ly.Diagnostics, dg("block_range_oob", tiff.SevError,
					fmt.Sprintf("%s %d byte range [%d,%d) exceeds file of %d bytes", ly.Kind, i, b.Offset, b.Offset+b.Count, fileSize), b.Range))
			default:
				b.State = StateOK
			}
		}
		grid[cellKey(col, row)] = true
		ly.Blocks = append(ly.Blocks, b)
	}
	// Extra offset entries beyond the expected grid are retained as evidence.
	for i := expected; i < len(offsets); i++ {
		id := fmt.Sprintf("%s:%s#%d", ly.IFDID, ly.Kind, i)
		b := &Block{ID: id, Kind: ly.Kind, Index: i, Expected: false, Offset: offsets[i]}
		if i < len(counts) {
			b.Count = counts[i]
		}
		b.Range = event.ByteRange{Lo: b.Offset, Hi: b.Offset + b.Count}
		b.Col, b.Row = -1, -1
		if b.Count == 0 {
			b.State = StateZeroSize
		} else if b.Offset < 0 || b.Offset+b.Count > fileSize {
			b.State = StateOOBCount
		} else {
			b.State = StateOK
		}
		ly.Diagnostics = append(ly.Diagnostics, dg("block_extra_offsets", tiff.SevWarning,
			fmt.Sprintf("%s offset entry %d lies outside expected %d-cell grid", ly.Kind, i, expected), b.Range))
		ly.Blocks = append(ly.Blocks, b)
	}
	ly.findOverlaps()
}

func (ly *Layout) findOverlaps() {
	for i := 0; i < len(ly.Blocks); i++ {
		a := ly.Blocks[i]
		if a.Range.Hi <= a.Range.Lo {
			continue
		}
		for j := i + 1; j < len(ly.Blocks); j++ {
			b := ly.Blocks[j]
			if b.Range.Hi <= b.Range.Lo {
				continue
			}
			if a.Range.Lo < b.Range.Hi && b.Range.Lo < a.Range.Hi {
				a.Overlaps = append(a.Overlaps, b.ID)
				b.Overlaps = append(b.Overlaps, a.ID)
				ly.OverlapPairs = append(ly.OverlapPairs, [2]string{a.ID, b.ID})
				ly.Diagnostics = append(ly.Diagnostics, dg("block_overlap", tiff.SevError,
					fmt.Sprintf("pixel byte ranges overlap: %s [%d,%d) vs %s [%d,%d)",
						a.ID, a.Range.Lo, a.Range.Hi, b.ID, b.Range.Lo, b.Range.Hi),
					event.ByteRange{Lo: max64(a.Range.Lo, b.Range.Lo), Hi: min64(a.Range.Hi, b.Range.Hi)}))
			}
		}
	}
}

func cellKey(col, row int) string { return fmt.Sprintf("%d,%d", col, row) }
func max64(a, b int64) int64 { if a > b { return a }; return b }
func min64(a, b int64) int64 { if a < b { return a }; return b }
