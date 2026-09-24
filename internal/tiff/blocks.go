package tiff

import "fmt"

// Block status values. Overlapping and missing blocks are surfaced
// explicitly; the analyzer never stitches or invents pixel data.
const (
	BlockOK          = "ok"
	BlockOverlap     = "overlap"
	BlockMissing     = "missing"
	BlockOutOfBounds = "out-of-bounds"
)

// Block is one tile or strip: a pixel-data byte range in the file plus
// its position in the image grid.
type Block struct {
	ID        string   `json:"id"` // blk@<ifdID>:<index>
	IFDID     string   `json:"ifdId"`
	Kind      string   `json:"kind"` // "tile" | "strip"
	Index     int      `json:"index"`
	GridX     int      `json:"gridX"`
	GridY     int      `json:"gridY"`
	Range     Range    `json:"range"`
	ByteCount uint64   `json:"byteCount"`
	Status    string   `json:"status"`
	Overlaps  []string `json:"overlaps,omitempty"` // IDs of blocks it overlaps
}

// BlockMap is the per-IFD pixel storage layout.
type BlockMap struct {
	IFDID       string   `json:"ifdId"`
	Kind        string   `json:"kind"` // "tiled" | "stripped" | "none"
	ImageWidth  uint64   `json:"imageWidth"`
	ImageLength uint64   `json:"imageLength"`
	BlockWidth  uint64   `json:"blockWidth"`
	BlockHeight uint64   `json:"blockHeight"`
	Cols        int      `json:"cols"`
	Rows        int      `json:"rows"`
	Blocks      []*Block `json:"blocks"`
}

// BlockMaps analyzes tile/strip layout for every IFD in the file and
// appends overlap/missing/out-of-bounds diagnostics to the file.
func BlockMaps(f *File) []*BlockMap {
	var maps []*BlockMap
	for _, ifd := range f.IFDs {
		bm := analyzeBlocks(f, ifd)
		if bm != nil {
			maps = append(maps, bm)
		}
	}
	return maps
}

func tagUint(ifd *IFD, code uint16) (uint64, bool) {
	t := ifd.TagByCode(code)
	if t == nil || len(t.Values) == 0 {
		return 0, false
	}
	return t.Values[0], true
}

func analyzeBlocks(f *File, ifd *IFD) *BlockMap {
	w, okW := tagUint(ifd, TagImageWidth)
	h, okH := tagUint(ifd, TagImageLength)
	if !okW || !okH {
		return nil
	}
	bm := &BlockMap{IFDID: ifd.ID, ImageWidth: w, ImageLength: h}

	var offsets, counts *Tag
	if tw, ok := tagUint(ifd, TagTileWidth); ok {
		th, _ := tagUint(ifd, TagTileLength)
		bm.Kind = "tiled"
		bm.BlockWidth, bm.BlockHeight = tw, th
		offsets = ifd.TagByCode(TagTileOffsets)
		counts = ifd.TagByCode(TagTileByteCounts)
	} else if rps, ok := tagUint(ifd, TagRowsPerStrip); ok {
		bm.Kind = "stripped"
		bm.BlockWidth, bm.BlockHeight = w, rps
		offsets = ifd.TagByCode(TagStripOffsets)
		counts = ifd.TagByCode(TagStripByteCounts)
	} else {
		return nil
	}
	if bm.BlockWidth == 0 || bm.BlockHeight == 0 {
		return nil
	}
	bm.Cols = int((w + bm.BlockWidth - 1) / bm.BlockWidth)
	bm.Rows = int((h + bm.BlockHeight - 1) / bm.BlockHeight)

	n := bm.Cols * bm.Rows
	var offs, cnts []uint64
	if offsets != nil {
		offs = offsets.Values
	}
	if counts != nil {
		cnts = counts.Values
	}
	for i := 0; i < n; i++ {
		blk := &Block{
			ID:     fmt.Sprintf("blk@%s:%d", ifd.ID, i),
			IFDID:  ifd.ID,
			Kind:   "strip",
			Index:  i,
			GridX:  i % bm.Cols,
			GridY:  i / bm.Cols,
			Status: BlockOK,
		}
		if bm.Kind == "tiled" {
			blk.Kind = "tile"
		} else {
			blk.Kind = "strip"
		}
		if i < len(offs) {
			blk.Range.Start = offs[i]
		}
		if i < len(cnts) {
			blk.ByteCount = cnts[i]
			blk.Range.End = blk.Range.Start + cnts[i]
		}
		switch {
		case i >= len(offs) || i >= len(cnts) || blk.ByteCount == 0:
			blk.Status = BlockMissing
			f.appendDiag(Diagnostic{
				Severity: "warning", Code: "BLOCK_MISSING", SubjectID: blk.ID,
				Message: fmt.Sprintf("%s %d has no readable pixel bytes (offset/bytecount absent or zero)", blk.Kind, i),
				Offset:  blk.Range.Start,
			})
		case blk.Range.End > f.Size:
			blk.Status = BlockOutOfBounds
			f.appendDiag(Diagnostic{
				Severity: "error", Code: "BLOCK_OUT_OF_BOUNDS", SubjectID: blk.ID,
				Message: fmt.Sprintf("%s %d range %s exceeds file size %d", blk.Kind, i, blk.Range, f.Size),
				Offset:  blk.Range.Start,
			})
		}
		bm.Blocks = append(bm.Blocks, blk)
	}

	// Pairwise overlap detection; overlapping blocks are kept side by
	// side, never merged.
	for i := 0; i < len(bm.Blocks); i++ {
		for j := i + 1; j < len(bm.Blocks); j++ {
			a, b := bm.Blocks[i], bm.Blocks[j]
			if a.Status == BlockMissing || b.Status == BlockMissing {
				continue
			}
			if a.Range.Len() > 0 && a.Range.Overlaps(b.Range) {
				a.Overlaps = append(a.Overlaps, b.ID)
				b.Overlaps = append(b.Overlaps, a.ID)
				if a.Status == BlockOK {
					a.Status = BlockOverlap
				}
				if b.Status == BlockOK {
					b.Status = BlockOverlap
				}
				f.appendDiag(Diagnostic{
					Severity: "warning", Code: "BLOCK_OVERLAP", SubjectID: a.ID,
					Message: fmt.Sprintf("%s ranges of %s and %s overlap: %s vs %s", bm.Kind, a.ID, b.ID, a.Range, b.Range),
					Offset:  a.Range.Start,
				})
			}
		}
	}
	return bm
}

func (f *File) appendDiag(d Diagnostic) {
	d.ID = fmt.Sprintf("diag-%03d", len(f.Diagnostics)+1)
	f.Diagnostics = append(f.Diagnostics, d)
}
