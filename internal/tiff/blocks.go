package tiff

import (
	"fmt"
	"ifdlantern/internal/domain"
)

func tagByID(node *IFD, id int) *RawTag {
	for i := range node.Tags {
		if node.Tags[i].Tag == id {
			return &node.Tags[i]
		}
	}
	return nil
}

func intsOf(rt *RawTag) []int64 {
	if rt == nil {
		return nil
	}
	out := make([]int64, 0, len(rt.Values))
	for _, v := range rt.Values {
		out = append(out, int64(v.N))
	}
	return out
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// analyzeBlocks never fabricates missing pixels: absent slots are reported as
// missing cells, aliased offsets as aliases, intersecting byte ranges as
// overlaps, and ranges outside the logical file as out-of-bounds evidence.
func analyzeBlocks(node *IFD, budget Budget, fileSize int64) *BlockLayout {
	bl := &BlockLayout{Mode: "none", Stats: map[string]int{}}
	wv := intsOf(tagByID(node, 256))
	hv := intsOf(tagByID(node, 257))
	if len(wv) == 0 || len(hv) == 0 {
		bl.Diags = append(bl.Diags, domain.Diagnostic{
			Code: "no-image-dimensions", Severity: domain.SevInfo,
			Message: "IFD has no ImageWidth/ImageLength; pixel block map unavailable",
			Evidence: []string{node.ID},
		})
		return bl
	}
	bl.ImageW, bl.ImageH = int(wv[0]), int(hv[0])

	tw := intsOf(tagByID(node, 322))
	th := intsOf(tagByID(node, 323))
	toffs := intsOf(tagByID(node, 324))
	tcounts := intsOf(tagByID(node, 325))
	soffs := intsOf(tagByID(node, 273))
	scounts := intsOf(tagByID(node, 279))

	switch {
	case len(toffs) > 0:
		bl.Mode = "tiled"
		if len(tw) == 0 || len(th) == 0 {
			bl.Diags = append(bl.Diags, domain.Diagnostic{
				Code: "tiles-without-dimensions", Severity: domain.SevError,
				Message: "TileOffsets present but TileWidth/TileLength missing",
				Evidence: []string{node.ID},
			})
			return bl
		}
		bl.TileW, bl.TileH = int(tw[0]), int(th[0])
		buildBlocks(bl, node, "tile", toffs, tcounts, budget, fileSize)
	case len(soffs) > 0:
		bl.Mode = "stripped"
		rps := int64(bl.ImageH)
		if v := intsOf(tagByID(node, 278)); len(v) > 0 {
			rps = v[0]
		}
		bl.RowsPerStrip = int(rps)
		buildStrips(bl, node, soffs, scounts, budget, fileSize)
	default:
		bl.Diags = append(bl.Diags, domain.Diagnostic{
			Code: "no-pixel-offsets", Severity: domain.SevInfo,
			Message: "neither TileOffsets nor StripOffsets present",
			Evidence: []string{node.ID},
		})
	}
	return bl
}

func buildStrips(bl *BlockLayout, node *IFD, offs, counts []int64, budget Budget, fileSize int64) {
	nExpected := ceilDiv(bl.ImageH, max1(bl.RowsPerStrip))
	for i, off := range offs {
		var bc int64
		if i < len(counts) {
			bc = counts[i]
		}
		y := i * bl.RowsPerStrip
		b := Block{Index: i, Kind: "strip",
			DataRange: ByteRange{Offset: off, Length: bc, Virtual: node.Range.Virtual && off >= 0},
			ByteCount: bc, PixelX: 0, PixelY: y, PixelW: bl.ImageW,
			PixelH: min(bl.RowsPerStrip, bl.ImageH-y)}
		if y >= bl.ImageH {
			b.Flags = append(b.Flags, "oob-image")
		}
		bl.Blocks = append(bl.Blocks, b)
	}
	finalize(bl, node, nExpected, len(offs), budget, fileSize)
}

func buildBlocks(bl *BlockLayout, node *IFD, kind string, offs, counts []int64, budget Budget, fileSize int64) {
	tpr := ceilDiv(bl.ImageW, max1(bl.TileW))
	nExpected := tpr * ceilDiv(bl.ImageH, max1(bl.TileH))
	for i, off := range offs {
		var bc int64
		if i < len(counts) {
			bc = counts[i]
		}
		col, row := i%max1(tpr), i/max1(tpr)
		px, py := col*bl.TileW, row*bl.TileH
		b := Block{Index: i, Kind: kind,
			DataRange: ByteRange{Offset: off, Length: bc, Virtual: node.Range.Virtual && off >= 1<<32},
			ByteCount: bc, PixelX: px, PixelY: py,
			PixelW: min(bl.TileW, bl.ImageW-px), PixelH: min(bl.TileH, bl.ImageH-py)}
		if px >= bl.ImageW || py >= bl.ImageH {
			b.Flags = append(b.Flags, "oob-image")
		}
		if px < 0 || py < 0 {
			b.Flags = append(b.Flags, "oob-image")
		}
		bl.Blocks = append(bl.Blocks, b)
	}
	finalize(bl, node, nExpected, len(offs), budget, fileSize)
}

func finalize(bl *BlockLayout, node *IFD, nExpected, nPresent int, budget Budget, fileSize int64) {
	// offset aliases: identical byte start
	aliasOf := map[int64][]int{}
	for i := range bl.Blocks {
		b := &bl.Blocks[i]
		aliasOf[b.DataRange.Offset] = append(aliasOf[b.DataRange.Offset], i)
	}
	for off, idxs := range aliasOf {
		if len(idxs) > 1 {
			for _, i := range idxs {
				b := &bl.Blocks[i]
				for _, j := range idxs {
					if j != i {
						b.Aliases = append(b.Aliases, j)
					}
				}
				b.Flags = addFlag(b.Flags, "alias")
				bl.Stats["alias"]++
			}
			bl.Diags = append(bl.Diags, domain.Diagnostic{
				Code: "block-offset-alias", Severity: domain.SevConflict,
				Message: fmt.Sprintf("%d %ss share data offset %d", len(idxs), bl.Blocks[0].Kind, off),
				Evidence: blockEvidence(node, idxs),
			})
		}
	}
	// byte-range intersections between distinct blocks
	for i := 0; i < len(bl.Blocks); i++ {
		for j := i + 1; j < len(bl.Blocks); j++ {
			a, c := &bl.Blocks[i], &bl.Blocks[j]
			if a.DataRange.Offset == c.DataRange.Offset {
				continue // already an alias
			}
			if a.ByteCount > 0 && c.ByteCount > 0 && a.DataRange.Intersects(c.DataRange) {
				a.Flags = addFlag(a.Flags, "byte-overlap")
				c.Flags = addFlag(c.Flags, "byte-overlap")
				bl.Stats["byte-overlap"]++
				bl.Diags = append(bl.Diags, domain.Diagnostic{
					Code: "block-byte-overlap", Severity: domain.SevConflict,
					Message: fmt.Sprintf("%ss %d and %d have intersecting byte ranges",
						a.Kind, i, j),
					Evidence: blockEvidence(node, []int{i, j}),
				})
			}
		}
		if b := &bl.Blocks[i]; b.ByteCount > 0 && b.DataRange.End() > fileSize {
			b.Flags = addFlag(b.Flags, "oob-file")
			bl.Stats["oob-file"]++
			bl.Diags = append(bl.Diags, domain.Diagnostic{
				Code: "block-out-of-file", Severity: domain.SevError,
				Message: fmt.Sprintf("%s %d bytes %d..%d exceed logical file size %d",
					b.Kind, i, b.DataRange.Offset, b.DataRange.End(), fileSize),
				Evidence: []string{node.ID, fmt.Sprintf("block:%d", i)},
			})
		}
	}
	// missing/extra slots
	if nPresent < nExpected {
		bl.Stats["missing-slots"] += nExpected - nPresent
		bl.Diags = append(bl.Diags, domain.Diagnostic{
			Code: "blocks-missing", Severity: domain.SevError,
			Message: fmt.Sprintf("%d %s(s) present but layout requires %d; missing pixels are not fabricated",
				nPresent, bl.Blocks[0].Kind, nExpected),
			Evidence: []string{node.ID},
		})
	}
	if nPresent > nExpected {
		bl.Stats["extra-slots"] += nPresent - nExpected
		bl.Diags = append(bl.Diags, domain.Diagnostic{
			Code: "blocks-extra", Severity: domain.SevWarning,
			Message: fmt.Sprintf("%d %s(s) present but layout only requires %d",
				nPresent, bl.Blocks[0].Kind, nExpected),
			Evidence: []string{node.ID},
		})
	}
	bl.Stats["total"] = nPresent
	bl.Stats["expected"] = nExpected

	// pixel-slot coverage (one cell per layout slot, capped by budget)
	if nExpected <= budget.MaxGridCells {
		slot := map[int]int{}
		for _, b := range bl.Blocks {
			tpr := max1(ceilDiv(bl.ImageW, max1(bl.TileW)))
			var s int
			if bl.Mode == "tiled" {
				s = (b.PixelY/max1(bl.TileH))*tpr + b.PixelX/max1(bl.TileW)
			} else {
				s = b.PixelY / max1(bl.RowsPerStrip)
			}
			slot[s]++
		}
		for s := 0; s < nExpected; s++ {
			n := slot[s]
			cell := CoverageCell{Blocks: []int{}, State: "missing"}
			if bl.Mode == "tiled" {
				tpr := max1(ceilDiv(bl.ImageW, max1(bl.TileW)))
				cell.X, cell.Y = s%tpr, s/tpr
			} else {
				cell.X, cell.Y = 0, s
			}
			if n == 1 {
				cell.State = "covered"
			}
			if n > 1 {
				cell.State = "overlap"
				bl.Stats["pixel-overlap"]++
			}
			for _, b := range bl.Blocks {
				tpr := max1(ceilDiv(bl.ImageW, max1(bl.TileW)))
				var bs int
				if bl.Mode == "tiled" {
					bs = (b.PixelY/max1(bl.TileH))*tpr + b.PixelX/max1(bl.TileW)
				} else {
					bs = b.PixelY / max1(bl.RowsPerStrip)
				}
				if bs == s {
					cell.Blocks = append(cell.Blocks, b.Index)
				}
			}
			bl.Cells = append(bl.Cells, cell)
		}
	} else {
		bl.CellsCapped = true
	}
}

func blockEvidence(node *IFD, idxs []int) []string {
	ev := []string{node.ID}
	for _, i := range idxs {
		ev = append(ev, fmt.Sprintf("block:%d", i))
	}
	return ev
}

func addFlag(flags []string, f string) []string {
	for _, x := range flags {
		if x == f {
			return flags
		}
	}
	return append(flags, f)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max1(a int) int {
	if a < 1 {
		return 1
	}
	return a
}
