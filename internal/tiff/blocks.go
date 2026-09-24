package tiff

// BlockIssue flags problems discovered while mapping pixel data blocks.
type BlockIssue struct {
	Kind    string `json:"kind"`
	Block   int    `json:"block"`
	Message string `json:"message"`
}

// resolveBlocks builds the explicit strip/tile block map for every IFD. It
// preserves overlapping and missing blocks as first-class states instead of
// merging them, and records out-of-range data ranges as evidence.
func (p *parser) resolveBlocks() {
	for _, key := range p.Order {
		ifd := p.IFDs[key]
		if t := ifd.TagByID(TagTileOffsets); t != nil {
			p.resolveTiles(ifd, t, ifd.TagByID(TagTileByteCounts))
		} else if s := ifd.TagByID(TagStripOffsets); s != nil {
			p.resolveStrips(ifd, s, ifd.TagByID(TagStripByteCounts))
		}
	}
}

func firstInt(f *File, t *Tag, def int64) int64 {
	if t == nil {
		return def
	}
	v := f.DecodeIntegers(t)
	if len(v) == 0 {
		return def
	}
	return v[0]
}

func (p *parser) resolveStrips(ifd *IFD, offTag, cntTag *Tag) {
	offs := p.DecodeIntegers(offTag)
	cnts := p.DecodeIntegers(cntTag)
	imageLength := firstInt(p.File, ifd.TagByID(TagImageLength), 0)
	rowsPerStrip := firstInt(p.File, ifd.TagByID(TagRowsPerStrip), imageLength)
	if rowsPerStrip <= 0 {
		rowsPerStrip = imageLength
	}
	expected := int64(len(offs))
	if imageLength > 0 && rowsPerStrip > 0 {
		expected = (imageLength + rowsPerStrip - 1) / rowsPerStrip
	}
	p.buildBlocks(ifd, BlockStrip, offs, cnts, expected, 1, offTag, cntTag)
}

func (p *parser) resolveTiles(ifd *IFD, offTag, cntTag *Tag) {
	offs := p.DecodeIntegers(offTag)
	cnts := p.DecodeIntegers(cntTag)
	width := firstInt(p.File, ifd.TagByID(TagImageWidth), 0)
	length := firstInt(p.File, ifd.TagByID(TagImageLength), 0)
	tw := firstInt(p.File, ifd.TagByID(TagTileWidth), width)
	tl := firstInt(p.File, ifd.TagByID(TagTileLength), length)
	var cols, rows int64
	if tw > 0 && width > 0 {
		cols = (width + tw - 1) / tw
	}
	if tl > 0 && length > 0 {
		rows = (length + tl - 1) / tl
	}
	expected := cols * rows
	if expected == 0 {
		expected = int64(len(offs))
	}
	p.buildBlocks(ifd, BlockTile, offs, cnts, expected, cols, offTag, cntTag)
}

func (p *parser) buildBlocks(ifd *IFD, kind BlockKind, offs, cnts []int64, expected, cols int64, offTag, cntTag *Tag) {
	blobLen := int64(len(p.File.Blob))
	n := expected
	if int64(len(offs)) > n {
		n = int64(len(offs))
	}
	step := int64(offTag.Value.Type.Size())
	cstep := int64(1)
	if cntTag != nil {
		cstep = int64(cntTag.Value.Type.Size())
	}
	for i := int64(0); i < n; i++ {
		blk := &Block{Index: int(i), Kind: kind}
		if cols > 0 {
			blk.TileX = int(i % cols)
			blk.TileY = int(i / cols)
		}
		blk.OffsetSource = ByteRange{
			Offset: offTag.Value.ValueRange.Offset + i*step,
			Length: step,
		}
		if cntTag != nil {
			blk.CountSource = ByteRange{
				Offset: cntTag.Value.ValueRange.Offset + i*cstep,
				Length: cstep,
			}
		}
		if i < int64(len(offs)) && i < int64(len(cnts)) {
			blk.Present = true
			blk.DataRange = ByteRange{Offset: offs[i], Length: cnts[i]}
			if offs[i] < 0 || cnts[i] < 0 || offs[i]+cnts[i] > blobLen {
				p.Notes = append(p.Notes, StructuralNote{
					Kind: "block_out_of_range",
					At:   blk.OffsetSource,
					Message: blockName(kind, i) + " data " + blk.DataRange.String() +
						" exceeds file length " + itoa(blobLen),
				})
			}
		} else {
			blk.Present = false
			p.Notes = append(p.Notes, StructuralNote{
				Kind:    "block_missing",
				At:      blk.OffsetSource,
				Message: blockName(kind, i) + " expected by layout has no offset/count pair",
			})
		}
		ifd.Blocks = append(ifd.Blocks, blk)
	}

	// Overlap detection: two present blocks claim the same pixel location or
	// overlapping byte ranges. Report, never merge.
	for i := int64(0); i < int64(len(ifd.Blocks)); i++ {
		a := ifd.Blocks[i]
		if !a.Present {
			continue
		}
		for j := i + 1; j < int64(len(ifd.Blocks)); j++ {
			b := ifd.Blocks[j]
			if !b.Present {
				continue
			}
			if kind == BlockTile && a.TileX == b.TileX && a.TileY == b.TileY {
				p.Notes = append(p.Notes, StructuralNote{
					Kind: "tile_overlap", At: a.OffsetSource,
					Message: blockName(kind, i) + " and " + blockName(kind, j) + " occupy the same tile coordinate",
				})
			}
			if a.DataRange.Length > 0 && b.DataRange.Length > 0 && a.DataRange.Overlaps(b.DataRange) {
				p.Notes = append(p.Notes, StructuralNote{
					Kind: "block_byte_overlap", At: a.OffsetSource,
					Message: blockName(kind, i) + " bytes " + a.DataRange.String() + " overlap " + blockName(kind, j) + " " + b.DataRange.String(),
				})
			}
		}
	}
}

func blockName(kind BlockKind, i int64) string {
	if kind == BlockTile {
		return "tile#" + itoa(i)
	}
	return "strip#" + itoa(i)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [24]byte
	pos := len(b)
	for v > 0 {
		pos--
		b[pos] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
