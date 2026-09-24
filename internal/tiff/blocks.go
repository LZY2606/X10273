package tiff

import (
	"fmt"

	"ifdgraph/internal/model"
)

// tagByCode returns the first tag with the code in an IFD (ordinal 0).
func tagByCode(g *model.ParseGraph, ifd *model.IFD, code uint16) *model.RawTag {
	for _, tid := range ifd.TagIDs {
		for _, t := range g.Tags {
			if t.ID == tid && t.Tag == code {
				return t
			}
		}
	}
	return nil
}

func firstU64(t *model.RawTag) (uint64, bool) {
	if t == nil || len(t.Numbers) == 0 {
		return 0, false
	}
	return t.Numbers[0], true
}

// buildBlocks turns strip/tile offset+bytecount tags into an explicit block
// map. Overlaps, short reads, out-of-range blocks and grid holes are all
// first-class statuses rather than being silently repaired.
func buildBlocks(g *model.ParseGraph) {
	for _, ifd := range g.IFDs {
		if !ifd.Reachable {
			continue
		}
		offTag := tagByCode(g, ifd, TagTileOffsets)
		cntTag := tagByCode(g, ifd, TagTileByteCounts)
		kind := model.BlockTile
		gridW, gridH := 1, 1
		if offTag == nil {
			offTag = tagByCode(g, ifd, TagStripOffsets)
			cntTag = tagByCode(g, ifd, TagStripByteCounts)
			kind = model.BlockStrip
		}
		if offTag == nil {
			continue
		}

		if kind == model.BlockTile {
			w, _ := firstU64(tagByCode(g, ifd, TagImageWidth))
			h, _ := firstU64(tagByCode(g, ifd, TagImageLength))
			tw, _ := firstU64(tagByCode(g, ifd, TagTileWidth))
			th, _ := firstU64(tagByCode(g, ifd, TagTileLength))
			if tw > 0 && th > 0 {
				gridW = int((w + tw - 1) / tw)
				gridH = int((h + th - 1) / th)
			}
		}

		var counts []uint64
		if cntTag != nil {
			counts = cntTag.Numbers
		}
		offsets := offTag.Numbers
		blocks := make([]model.PixelBlock, 0, len(offsets))
		for i, off := range offsets {
			b := model.PixelBlock{
				ID:    model.BlockID(g.SourceID+"#"+ifd.ID, kind, i),
				Kind:  kind,
				Index: i,
			}
			if kind == model.BlockTile && gridW > 0 {
				b.TileX = i % gridW
				b.TileY = i / gridW
			}
			b.Offset = int64(off)
			if i < len(counts) {
				b.Count = int64(counts[i])
			}
			b.Range = model.ByteRange{Offset: b.Offset, Length: b.Count}
			switch {
			case cntTag == nil:
				b.Status = model.BlockUnspecified
				b.Diagnostic = "byte counts tag absent"
			case b.Range.End() > g.FileSize:
				b.Status = model.BlockOutOfBounds
				b.Diagnostic = fmt.Sprintf("block range [%d,%d) exceeds file size %d", b.Offset, b.Range.End(), g.FileSize)
			case i >= len(counts) || b.Count == 0:
				b.Status = model.BlockShort
				b.Diagnostic = "declared byte count missing or zero"
			default:
				b.Status = model.BlockOK
			}
			blocks = append(blocks, b)
		}
		// Overlap detection: explicit, no silent repair.
		for i := range blocks {
			for j := i + 1; j < len(blocks); j++ {
				if blocks[i].Range.Length == 0 || blocks[j].Range.Length == 0 {
					continue
				}
				if blocks[i].Range.Offset < blocks[j].Range.End() && blocks[j].Range.Offset < blocks[i].Range.End() {
					blocks[i].Status = model.BlockOverlapping
					blocks[j].Status = model.BlockOverlapping
					blocks[i].Overlaps = append(blocks[i].Overlaps, blocks[j].ID)
					blocks[j].Overlaps = append(blocks[j].Overlaps, blocks[i].ID)
				}
			}
		}
		// Grid holes for tiles: expected cells without a block.
		if kind == model.BlockTile && gridW > 0 && gridH > 0 {
			expected := gridW * gridH
			for len(blocks) < expected {
				i := len(blocks)
				b := model.PixelBlock{
					ID:         model.BlockID(g.SourceID+"#"+ifd.ID, kind, i),
					Kind:       kind,
					Index:      i,
					TileX:      i % gridW,
					TileY:      i / gridW,
					Status:     model.BlockMissing,
					Diagnostic: fmt.Sprintf("tile grid cell (%d,%d) has no offset entry", i%gridW, i/gridW),
				}
				blocks = append(blocks, b)
			}
			if len(blocks) > expected {
				diag(g, ifd.ID, "extra_blocks", fmt.Sprintf("IFD declares %d blocks for a %dx%d grid (%d cells)", len(blocks), gridW, gridH, expected))
			}
		}
		g.Blocks = append(g.Blocks, blocks...)
	}
}
