package tiff

import "fmt"

func buildBlocks(g *Graph) {
	for _, ifd := range g.IFDs {
		tileOffsets := oneTag(g, ifd.ID, TagTileOffsets)
		stripOffsets := oneTag(g, ifd.ID, TagStripOffsets)
		if tileOffsets != nil {
			buildTiles(g, ifd, tileOffsets)
		} else if stripOffsets != nil {
			buildStrips(g, ifd, stripOffsets)
		}
	}
	sortBlocks(g.Blocks)
	detectBlockOverlaps(g)
}

func buildTiles(g *Graph, ifd IFD, offsets *Tag) {
	width := tagInt(oneTag(g, ifd.ID, TagImageWidth), 0, 0)
	height := tagInt(oneTag(g, ifd.ID, TagImageLength), 0, 0)
	tileWidth := tagInt(oneTag(g, ifd.ID, TagTileWidth), 0, 0)
	tileHeight := tagInt(oneTag(g, ifd.ID, TagTileLength), 0, 0)
	counts := oneTag(g, ifd.ID, TagTileByteCounts)
	if width <= 0 || height <= 0 || tileWidth <= 0 || tileHeight <= 0 {
		addBlockFinding(g, ifd.ID, "invalid_tile_grid", "tile grid dimensions are missing or non-positive")
	}
	columns := ceilDiv(width, tileWidth)
	rows := ceilDiv(height, tileHeight)
	expected := columns * rows
	for i := 0; i < maxInt(expected, len(offsets.Raw.Values)); i++ {
		block := Block{ID: blockID(g.DocumentID, ifd.Offset, i), IFDID: ifd.ID, Kind: "tile", Index: i,
			Column: i % maxInt(columns, 1), Row: i / maxInt(columns, 1),
			Width:      minInt(tileWidth, width-i%maxInt(columns, 1)*tileWidth),
			Height:     minInt(tileHeight, height-(i/maxInt(columns, 1))*tileHeight),
			ImageWidth: width, ImageHeight: height}
		if i >= expected {
			block.State = "unexpected"
			block.Diagnostics = append(block.Diagnostics, "offset has no matching tile-grid cell")
		} else {
			applyBlockRange(&block, offsets, counts, i, g.FileLength)
		}
		g.Blocks = append(g.Blocks, block)
	}
	if len(offsets.Raw.Values) < expected {
		for i := len(offsets.Raw.Values); i < expected; i++ {
			g.Blocks = append(g.Blocks, Block{ID: blockID(g.DocumentID, ifd.Offset, i), IFDID: ifd.ID, Kind: "tile",
				Index: i, Column: i % maxInt(columns, 1), Row: i / maxInt(columns, 1),
				Width: tileWidth, Height: tileHeight, ImageWidth: width, ImageHeight: height,
				State: "missing", Diagnostics: []string{"tile offset is missing; no pixel bytes are inferred"}})
		}
		addBlockFinding(g, ifd.ID, "missing_tiles", fmt.Sprintf("%d of %d tile offsets are missing", expected-len(offsets.Raw.Values), expected))
	}
}

func buildStrips(g *Graph, ifd IFD, offsets *Tag) {
	width := tagInt(oneTag(g, ifd.ID, TagImageWidth), 0, 0)
	height := tagInt(oneTag(g, ifd.ID, TagImageLength), 0, 0)
	rowsPerStrip := tagInt(oneTag(g, ifd.ID, TagRowsPerStrip), 0, maxInt(height, 1))
	expected := ceilDiv(height, rowsPerStrip)
	if width <= 0 || height <= 0 || rowsPerStrip <= 0 {
		addBlockFinding(g, ifd.ID, "invalid_strip_grid", "strip grid dimensions are missing or non-positive")
	}
	for i := 0; i < maxInt(expected, len(offsets.Raw.Values)); i++ {
		top := i * rowsPerStrip
		block := Block{ID: blockID(g.DocumentID, ifd.Offset, i), IFDID: ifd.ID, Kind: "strip", Index: i,
			Column: 0, Row: i, Width: width, Height: minInt(rowsPerStrip, height-top),
			ImageWidth: width, ImageHeight: height}
		if i >= expected {
			block.State = "unexpected"
			block.Diagnostics = append(block.Diagnostics, "offset has no matching strip")
		} else {
			applyBlockRange(&block, offsets, oneTag(g, ifd.ID, TagStripByteCounts), i, g.FileLength)
		}
		g.Blocks = append(g.Blocks, block)
	}
}

func applyBlockRange(block *Block, offsets, counts *Tag, i int, fileLength int64) {
	if i >= len(offsets.Raw.Values) || !offsets.ValuePresent {
		block.State = "missing"
		block.Diagnostics = append(block.Diagnostics, "pixel data offset is absent")
		return
	}
	offset, err := parseUint64(offsets.Raw.Values[i])
	if err != nil {
		block.State = "unknown"
		block.Diagnostics = append(block.Diagnostics, "offset is not an integer")
		return
	}
	var length uint64
	if counts != nil && i < len(counts.Raw.Values) && counts.ValuePresent {
		length, _ = parseUint64(counts.Raw.Values[i])
	}
	r := ByteRange{Offset: int64(offset), Length: int64(length), End: int64(offset + length)}
	block.ByteRange = &r
	if length == 0 {
		block.State = "unknown"
		block.Diagnostics = append(block.Diagnostics, "byte count is zero or unavailable")
	} else if r.Offset < 0 || r.End > fileLength {
		block.State = "out_of-bounds"
		block.Diagnostics = append(block.Diagnostics, fmt.Sprintf("pixel range [%d,%d) exceeds %d byte file", r.Offset, r.End, fileLength))
	} else {
		block.State = "present"
	}
}

func detectBlockOverlaps(g *Graph) {
	for i := range g.Blocks {
		for j := i + 1; j < len(g.Blocks); j++ {
			a, b := g.Blocks[i], g.Blocks[j]
			if a.IFDID != b.IFDID || a.ByteRange == nil || b.ByteRange == nil || a.ByteRange.Length == 0 || b.ByteRange.Length == 0 {
				continue
			}
			if a.ByteRange.Offset < b.ByteRange.End && b.ByteRange.Offset < a.ByteRange.End {
				g.Blocks[i].Overlaps = append(g.Blocks[i].Overlaps, b.ID)
				g.Blocks[j].Overlaps = append(g.Blocks[j].Overlaps, a.ID)
				if g.Blocks[i].State == "present" {
					g.Blocks[i].State = "overlapping"
				}
				if g.Blocks[j].State == "present" {
					g.Blocks[j].State = "overlapping"
				}
				g.Findings = append(g.Findings, Finding{
					ID: stableID(g.DocumentID, "tile_overlap", a.ID, b.ID), Kind: "tile_overlap", Severity: "warning",
					ObjectID: a.ID, Message: fmt.Sprintf("%s and %s byte ranges overlap; both candidates remain visible", a.ID, b.ID),
					ByteRange: a.ByteRange})
			}
		}
	}
}

func addBlockFinding(g *Graph, ifdID, kind, message string) {
	g.Findings = append(g.Findings, Finding{ID: stableID(g.DocumentID, kind, ifdID), Kind: kind, Severity: "error", ObjectID: ifdID, Message: message})
}

func sortBlocks(blocks []Block) {
	for i := range blocks {
		for j := i + 1; j < len(blocks); j++ {
			if blocks[j].IFDID < blocks[i].IFDID || (blocks[j].IFDID == blocks[i].IFDID && blocks[j].Index < blocks[i].Index) {
				blocks[i], blocks[j] = blocks[j], blocks[i]
			}
		}
	}
}

func ceilDiv(a, b int) int {
	if b <= 0 || a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
