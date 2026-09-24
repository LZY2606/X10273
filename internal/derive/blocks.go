package derive

import (
	"fmt"

	"ifdroom/internal/tiff"
)

type BlockState string

const (
	StatePresent     BlockState = "present"
	StateMissing     BlockState = "missing"      // listed but not in the file
	StateUnlisted    BlockState = "unlisted_gap" // grid cell without an entry
	StateOverlapping BlockState = "overlapping"  // byte range collides with another block
	StateValueOOB    BlockState = "value_oob"    // offset/count tag itself unreadable
)

type Block struct {
	ID       string     `json:"id"`
	IFD      int64      `json:"ifdOffset"`
	Index    int        `json:"index"`
	Col      int        `json:"col"`
	Row      int        `json:"row"`
	Offset   int64      `json:"offset"`
	Length   int64      `json:"length"`
	State    BlockState `json:"state"`
	Overlaps []string   `json:"overlaps,omitempty"`
	Kind     string     `json:"kind"` // "tile" | "strip"
	// PixelSpan is the on-disk pixel region covered (col/row plus tile size),
	// kept separate from byte range overlap.
	PixelCol0 int `json:"pixelCol0"`
	PixelRow0 int `json:"pixelRow0"`
	PixelCol1 int `json:"pixelCol1"`
	PixelRow1 int `json:"pixelRow1"`
}

type BlockMap struct {
	IFD         int64    `json:"ifdOffset"`
	Kind        string   `json:"kind"`
	Cols        int      `json:"cols"`
	Rows        int      `json:"rows"`
	TileWidth   int      `json:"tileWidth"`
	TileLength  int      `json:"tileLength"`
	ImageWidth  int      `json:"imageWidth"`
	ImageLength int      `json:"imageLength"`
	Blocks      []*Block `json:"blocks"`
	Overlap     bool     `json:"overlap"`
	HasMissing  bool     `json:"hasMissing"`
	Evidence    []string `json:"evidence"`
}

func u1(ifd *tiff.IFD, tag uint16) (int, bool) {
	e := ifd.ByTag[tag]
	if e == nil || len(e.Uints) == 0 {
		return 0, false
	}
	return int(e.Uints[0]), true
}

// BuildBlockMap constructs the explicit block grid for one IFD. Missing
// tiles and overlapping byte ranges are reported as states rather than
// repaired or hidden.
func BuildBlockMap(ifd *tiff.IFD, fileSize int64) *BlockMap {
	tiled := ifd.ByTag[tiff.TagTileOffsets] != nil
	kind := "strip"
	var offsetTag, countTag uint16 = tiff.TagStripOffsets, tiff.TagStripByteCounts
	if tiled {
		kind = "tile"
		offsetTag, countTag = tiff.TagTileOffsets, tiff.TagTileByteCounts
	}
	oe := ifd.ByTag[offsetTag]
	ce := ifd.ByTag[countTag]
	bm := &BlockMap{IFD: ifd.Offset, Kind: kind}
	width, _ := u1(ifd, tiff.TagImageWidth)
	length, _ := u1(ifd, tiff.TagImageLength)
	bm.ImageWidth, bm.ImageLength = width, length
	if tiled {
		bm.TileWidth, _ = u1(ifd, tiff.TagTileWidth)
		bm.TileLength, _ = u1(ifd, tiff.TagTileLength)
		if bm.TileWidth > 0 && bm.TileLength > 0 {
			bm.Cols = (width + bm.TileWidth - 1) / bm.TileWidth
			bm.Rows = (length + bm.TileLength - 1) / bm.TileLength
		}
	} else {
		rps, ok := u1(ifd, tiff.TagRowsPerStrip)
		if !ok || rps <= 0 {
			rps = length
		}
		if rps > 0 && length > 0 {
			bm.Rows = (length + rps - 1) / rps
			bm.Cols = 1
			bm.TileLength = rps
			bm.TileWidth = width
		}
	}
	if oe == nil {
		bm.Evidence = append(bm.Evidence, fmt.Sprintf("IFD@%d has no %s offset tag", ifd.Offset, kind))
		return bm
	}
	if oe.OutOfBounds {
		bm.Evidence = append(bm.Evidence, fmt.Sprintf("%s offset tag external value outside file", kind))
	}
	n := len(oe.Uints)
	for i := 0; i < n; i++ {
		off := int64(oe.Uints[i])
		var cnt int64 = -1
		state := StatePresent
		var overlaps []string
		if ce != nil && i < len(ce.Uints) {
			cnt = int64(ce.Uints[i])
		}
		if ce == nil || i >= len(ce.Uints) {
			state = StateValueOOB
			bm.Evidence = append(bm.Evidence, fmt.Sprintf("%s %d missing byte count entry", kind, i))
		} else if ce.OutOfBounds {
			state = StateValueOOB
		} else if off < 0 || cnt < 0 || off+cnt > fileSize || off < 0 {
			state = StateMissing
			bm.HasMissing = true
			bm.Evidence = append(bm.Evidence, fmt.Sprintf(
				"%s %d @%d len %d exceeds file size %d", kind, i, off, cnt, fileSize))
		}
		col, row := 0, i
		if tiled && bm.Cols > 0 {
			col, row = i%bm.Cols, i/bm.Cols
		}
		b := &Block{ID: fmt.Sprintf("ifd%d-%s%d", ifd.Offset, kind, i), IFD: ifd.Offset,
			Index: i, Col: col, Row: row, Offset: off, Length: cnt, State: state, Kind: kind,
			Overlaps: overlaps}
		pc0, pr0 := 0, 0
		if tiled {
			pc0, pr0 = col*bm.TileWidth, row*bm.TileLength
		} else {
			pr0 = i * bm.TileLength
		}
		b.PixelCol0, b.PixelRow0 = pc0, pr0
		b.PixelCol1 = minInt(width, pc0+bm.TileWidth)
		b.PixelRow1 = minInt(length, pr0+bm.TileLength)
		bm.Blocks = append(bm.Blocks, b)
	}
	if tiled && bm.Cols > 0 && bm.Rows > 0 {
		want := bm.Cols * bm.Rows
		if n < want {
			bm.HasMissing = true
			present := map[int]bool{}
			for _, b := range bm.Blocks {
				present[b.Index] = true
			}
			for idx := n; idx < want; idx++ {
				col, row := idx%bm.Cols, idx/bm.Cols
				pc0, pr0 := col*bm.TileWidth, row*bm.TileLength
				bm.Blocks = append(bm.Blocks, &Block{
					ID: fmt.Sprintf("ifd%d-tile%d", ifd.Offset, idx), IFD: ifd.Offset,
					Index: idx, Col: col, Row: row, State: StateUnlisted, Kind: kind,
					PixelCol0: pc0, PixelRow0: pr0,
					PixelCol1: minInt(width, pc0+bm.TileWidth),
					PixelRow1: minInt(length, pr0+bm.TileLength),
				})
				bm.Evidence = append(bm.Evidence, fmt.Sprintf("tile grid cell [%d,%d] (index %d) has no offset entry", row, col, idx))
			}
		}
	}
	markOverlaps(bm)
	return bm
}

func markOverlaps(bm *BlockMap) {
	bs := bm.Blocks
	for i := 0; i < len(bs); i++ {
		if bs[i].State != StatePresent || bs[i].Length <= 0 {
			continue
		}
		for j := i + 1; j < len(bs); j++ {
			if bs[j].State != StatePresent || bs[j].Length <= 0 {
				continue
			}
			a0, a1 := bs[i].Offset, bs[i].Offset+bs[i].Length
			b0, b1 := bs[j].Offset, bs[j].Offset+bs[j].Length
			if a0 < b1 && b0 < a1 {
				bs[i].State = StateOverlapping
				bs[j].State = StateOverlapping
				bs[i].Overlaps = append(bs[i].Overlaps, bs[j].ID)
				bs[j].Overlaps = append(bs[j].Overlaps, bs[i].ID)
				bm.Overlap = true
				bm.Evidence = append(bm.Evidence, fmt.Sprintf(
					"%s byte ranges overlap: %s[@%d:%d] and %s[@%d:%d]",
					bs[i].Kind, bs[i].ID, a0, a1, bs[j].ID, b0, b1))
			}
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
