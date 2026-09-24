package tiff

// intTag returns the integer value list of a tag, preferring external data
// (already decoded) and falling back to inline decoded values.
func intTag(d *IFD, code uint16) []uint64 {
	if t := d.TagByCode(code); t != nil {
		return t.Decoded
	}
	return nil
}

func firstInt(d *IFD, code uint16, fallback int) int {
	v := intTag(d, code)
	if len(v) > 0 {
		return int(v[0])
	}
	return fallback
}

// parseBlocks reconstructs the tile/strip map WITHOUT filling gaps: when
// offsets and byte counts disagree, the unmatched entries stay explicitly
// Missing so reviewers see the defect instead of a tidy picture.
func parseBlocks(r *reader, d *IFD, doc *Document) *Blocks {
	b := &Blocks{Kind: "none"}
	b.ImageW = firstInt(d, TagImageWidth, 0)
	b.ImageH = firstInt(d, TagImageLength, 0)

	offT := d.TagByCode(TagTileOffsets)
	cntT := d.TagByCode(TagTileByteCounts)
	offS := d.TagByCode(TagStripOffsets)
	cntS := d.TagByCode(TagStripByteCounts)

	switch {
	case offT != nil || cntT != nil:
		b.Kind = "tile"
		b.TileW = firstInt(d, TagTileWidth, 16)
		b.TileH = firstInt(d, TagTileLength, 16)
		buildBlockMap(b, offT, cntT, doc, true)
	case offS != nil || cntS != nil:
		b.Kind = "strip"
		b.RowsPerStrip = firstInt(d, TagRowsPerStrip, b.ImageH)
		buildBlockMap(b, offS, cntS, doc, false)
	default:
		return b
	}
	return b
}

func buildBlockMap(b *Blocks, offTag, cntTag *Tag, doc *Document, tiles bool) {
	var offs, cnts []uint64
	if offTag != nil {
		offs = offTag.Decoded
	}
	if cntTag != nil {
		cnts = cntTag.Decoded
	}
	n := len(offs)
	if len(cnts) > n {
		n = len(cnts)
	}
	if len(offs) != len(cnts) {
		b.MismatchedCounts = true
		b.CountMismatch = [2]int{len(offs), len(cnts)}
	}
	for i := 0; i < n; i++ {
		blk := Block{Index: i, Kind: b.Kind}
		if tiles {
			cols := (b.ImageW + b.TileW - 1) / b.TileW
			if cols < 1 {
				cols = 1
			}
			blk.Col = i % cols
			blk.Row = i / cols
			blk.X = blk.Col * b.TileW
			blk.Y = blk.Row * b.TileH
			blk.W = minInt(b.TileW, b.ImageW-blk.X)
			blk.H = minInt(b.TileH, b.ImageH-blk.Y)
		} else {
			rps := b.RowsPerStrip
			if rps <= 0 {
				rps = b.ImageH
			}
			blk.Row = i
			blk.Y = i * rps
			blk.X = 0
			blk.W = b.ImageW
			blk.H = minInt(rps, b.ImageH-blk.Y)
		}
		hasOff := i < len(offs)
		hasCnt := i < len(cnts)
		if hasOff && hasCnt {
			blk.Range = Range{offs[i], cnts[i]}
		} else {
			blk.Missing = true
		}
		b.Items = append(b.Items, blk)
	}
	// Range validation + pairwise overlap detection (explicit evidence).
	for i := range b.Items {
		blk := &b.Items[i]
		if blk.Missing {
			continue
		}
		if blk.Range.End() > doc.FileSize || blk.Range.Offset > doc.FileSize {
			b.OutOfBounds = append(b.OutOfBounds, OutOfBounds{
				Block: i, Range: blk.Range, FileSize: doc.FileSize,
			})
			doc.find("block", "error",
				"tile/strip byte range exceeds file size", &blk.Range)
		}
		for j := 0; j < i; j++ {
			if b.Items[j].Missing {
				continue
			}
			if ov, ok := intersect(b.Items[j].Range, blk.Range); ok {
				b.Overlaps = append(b.Overlaps, Overlap{A: j, B: i, Range: ov})
				doc.find("block", "warning",
					"tile/strip byte ranges overlap; pixels are ambiguous", &ov)
			}
		}
	}
}

func intersect(a, c Range) (Range, bool) {
	start := maxU(a.Offset, c.Offset)
	end := minU(a.End(), c.End())
	if start < end {
		return Range{start, end - start}, true
	}
	return Range{}, false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minU(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func maxU(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
