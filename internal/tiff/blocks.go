package tiff

import "fmt"

// buildBlocks 构建 tile/strip 像素块地图。重叠、缺失、越界显式标注,不拼凑。
func (p *parser) buildBlocks() {
	r := p.res
	w := tagUint(r, 256)
	h := tagUint(r, 257)
	if w == 0 || h == 0 {
		return
	}
	r.ImageWidth, r.ImageLength = w, h
	tw := tagUint(r, 322)
	th := tagUint(r, 323)
	if tw > 0 && th > 0 {
		r.Tiled = true
		p.tileBlocks(w, h, tw, th)
		return
	}
	rps := tagUint(r, 278)
	if rps == 0 {
		rps = h
	}
	offs := tagUints(r, 273)
	cnt := tagUints(r, 279)
	n := int((h + rps - 1) / rps)
	for i := 0; i < n; i++ {
		y0 := uint32(i) * rps
		y1 := y0 + rps
		if y1 > h {
			y1 = h
		}
		b := Block{Index: i, Kind: "strip", X0: 0, X1: w, Y0: y0, Y1: y1, Status: "ok"}
		if i < len(offs) {
			b.Offset = offs[i]
		}
		if i < len(cnt) {
			b.ByteCount = cnt[i]
		}
		if i >= len(offs) || i >= len(cnt) || b.ByteCount == 0 {
			b.Status = "missing"
			b.Detail = "strip 数组未覆盖该范围或字节数为 0"
		}
		r.Blocks = append(r.Blocks, b)
	}
	p.classifyBlocks()
}

func (p *parser) tileBlocks(w, h, tw, th uint32) {
	r := p.res
	across := (w + tw - 1) / tw
	down := (h + th - 1) / th
	offs := tagUints(r, 324)
	cnt := tagUints(r, 325)
	n := int(across * down)
	for i := 0; i < n; i++ {
		col := uint32(i) % across
		row := uint32(i) / across
		x0, y0 := col*tw, row*th
		x1, y1 := x0+tw, y0+th
		if x1 > w {
			x1 = w
		}
		if y1 > h {
			y1 = h
		}
		b := Block{Index: i, Kind: "tile", X0: x0, Y0: y0, X1: x1, Y1: y1, Status: "ok"}
		if i < len(offs) {
			b.Offset = offs[i]
		}
		if i < len(cnt) {
			b.ByteCount = cnt[i]
		}
		if i >= len(offs) || i >= len(cnt) || b.ByteCount == 0 {
			b.Status = "missing"
			b.Detail = "tile 数组未覆盖该位置或字节数为 0"
		}
		r.Blocks = append(r.Blocks, b)
	}
	p.classifyBlocks()
}

// classifyBlocks 标注越界与字节范围重叠。
func (p *parser) classifyBlocks() {
	r := p.res
	for i := range r.Blocks {
		b := &r.Blocks[i]
		if b.Status == "missing" {
			p.ev("block.missing", "像素块缺失,显式保留不拼凑",
				map[string]any{"block": b.Identity(), "x0": b.X0, "y0": b.Y0, "x1": b.X1, "y1": b.Y1})
			continue
		}
		if b.Offset+b.ByteCount > uint64(r.FileSize) {
			b.Status = "out_of_bounds"
			b.Detail = fmt.Sprintf("字节范围 [%d,%d) 超出文件大小 %d", b.Offset, b.Offset+b.ByteCount, r.FileSize)
			p.ev("block.out_of_bounds", "像素块字节范围越界",
				map[string]any{"block": b.Identity(), "offset": b.Offset, "byteCount": b.ByteCount, "fileSize": r.FileSize})
		}
	}
	// 两两检测字节范围重叠
	for i := 0; i < len(r.Blocks); i++ {
		for j := i + 1; j < len(r.Blocks); j++ {
			a, b := r.Blocks[i], r.Blocks[j]
			if a.ByteCount == 0 || b.ByteCount == 0 {
				continue
			}
			if a.Offset < b.Offset+b.ByteCount && b.Offset < a.Offset+a.ByteCount {
				lo := max64(a.Offset, b.Offset)
				hi := min64(a.Offset+a.ByteCount, b.Offset+b.ByteCount)
				for _, k := range []int{i, j} {
					if r.Blocks[k].Status == "ok" {
						r.Blocks[k].Status = "overlap"
					}
				}
				r.Blocks[i].Detail = fmt.Sprintf("与 %s 在字节 [%d,%d) 重叠", r.Blocks[j].Identity(), lo, hi)
				r.Blocks[j].Detail = fmt.Sprintf("与 %s 在字节 [%d,%d) 重叠", r.Blocks[i].Identity(), lo, hi)
				p.ev("tile.overlap", "像素块字节范围重叠,两个块都保留并标注",
					map[string]any{"blockA": a.Identity(), "blockB": b.Identity(),
						"overlapStart": lo, "overlapEnd": hi})
			}
		}
	}
}

func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func tagUint(r *Result, id uint16) uint32 {
	t := r.findTag(id)
	if t == nil || len(t.Uints) == 0 {
		return 0
	}
	return uint32(t.Uints[0])
}

func tagUints(r *Result, id uint16) []uint64 {
	t := r.findTag(id)
	if t == nil {
		return nil
	}
	return t.Uints
}
