package tiff

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
)

var typeSizes = map[uint16]uint64{
	1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8,
	11: 4, 12: 8, 16: 8, 17: 8, 18: 8,
}

var typeNames = map[uint16]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}

var tagNames = map[uint16]string{
	256: "ImageWidth", 257: "ImageLength", 258: "BitsPerSample",
	259: "Compression", 262: "PhotometricInterpretation",
	273: "StripOffsets", 277: "SamplesPerPixel", 278: "RowsPerStrip",
	279: "StripByteCounts", 284: "PlanarConfiguration",
	317: "Predictor", 322: "TileWidth", 323: "TileLength",
	324: "TileOffsets", 325: "TileByteCounts", 330: "SubIFDs",
	33550: "ModelPixelScale", 33922: "ModelTiepoint",
	34264: "ModelTransformation", 34735: "GeoKeyDirectory",
	34736: "GeoDoubleParams", 34737: "GeoAsciiParams",
}

// ParamFP 计算参数指纹。
func ParamFP(o Options) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|budget=%d", AlgoVersion, o.Budget)))
	return hex.EncodeToString(h[:8])
}

type parser struct {
	data   []byte
	order  binary.ByteOrder
	res    *Result
	visited map[uint64]bool
	budget  int
}

// Parse 解析 TIFF 字节,保留全部原始表示并输出机器可读取事件。
func Parse(data []byte, o Options) *Result {
	if o.Budget <= 0 {
		o.Budget = DefaultBudget
	}
	res := &Result{
		AlgoVersion: AlgoVersion,
		ParamFP:     ParamFP(o),
		FileSize:    len(data),
	}
	p := &parser{data: data, res: res, visited: map[uint64]bool{}, budget: o.Budget}
	res.Budget.Limit = o.Budget
	p.parseHeader()
	if res.ByteOrder != "" {
		p.walkIFDs()
		p.buildFragments()
		p.buildBlocks()
		p.buildGeo()
	}
	return res
}

func (p *parser) ev(kind, msg string, detail map[string]any) {
	p.res.Events = append(p.res.Events, Event{Kind: kind, Message: msg, Detail: detail})
}

func (p *parser) parseHeader() {
	d := p.data
	r := p.res
	if len(d) < 8 {
		p.ev("header.too_short", "文件不足 8 字节,无法读取 TIFF 头", map[string]any{"size": len(d)})
		r.Diagnostics = append(r.Diagnostics, "header too short")
		return
	}
	switch string(d[:2]) {
	case "II":
		r.ByteOrder = LittleEndian
		p.order = binary.LittleEndian
	case "MM":
		r.ByteOrder = BigEndian
		p.order = binary.BigEndian
	default:
		p.ev("header.bad_byte_order", "未知大小端标记,保留原始字节", map[string]any{"bytes": hex.EncodeToString(d[:2])})
		r.Diagnostics = append(r.Diagnostics, "unknown byte order mark")
		return
	}
	p.ev("header.byte_order", fmt.Sprintf("大小端: %s", r.ByteOrder), map[string]any{"byteOrder": string(r.ByteOrder)})
	magic := p.order.Uint16(d[2:4])
	switch magic {
	case 42:
		r.Variant = Classic
		r.FirstIFD = uint64(p.order.Uint32(d[4:8]))
		r.HeaderRange = [2]uint64{0, 8}
	case 43:
		if len(d) < 16 {
			p.ev("header.too_short", "BigTIFF 头不足 16 字节", nil)
			return
		}
		offSize := p.order.Uint16(d[4:6])
		r.Variant = BigTIFF
		r.FirstIFD = p.order.Uint64(d[8:16])
		r.HeaderRange = [2]uint64{0, 16}
		p.ev("header.bigtiff", "BigTIFF 宽偏移(8 字节)", map[string]any{"offsetSize": offSize})
	default:
		p.ev("header.bad_magic", "未知 magic,保留原值", map[string]any{"magic": magic})
		r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("unknown magic %d", magic))
		return
	}
	p.ev("header.variant", fmt.Sprintf("格式变体: %s, 首个 IFD 偏移 %d", r.Variant, r.FirstIFD),
		map[string]any{"variant": string(r.Variant), "firstIFD": r.FirstIFD})
}

// walkIFDs 沿 IFD 链与 SubIFDs 遍历,检测循环、共享子目录并执行访问预算。
func (p *parser) walkIFDs() {
	r := p.res
	type item struct {
		off uint64
		via string
	}
	queue := []item{{r.FirstIFD, "main"}}
	seen := map[uint64]bool{}
	order := []uint64{}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.off == 0 {
			continue
		}
		if p.budget <= 0 {
			r.Budget.Exhausted = true
			p.ev("budget.exhausted", "IFD 访问预算耗尽,剩余偏移保持未访问状态",
				map[string]any{"remainingOffset": it.off, "via": it.via, "limit": r.Budget.Limit})
			r.IFDs = append(r.IFDs, &IFD{Offset: it.off, Index: -1, Status: "unvisited", Via: []string{it.via}})
			continue
		}
		if seen[it.off] {
			// 已访问:区分链循环与共享子目录
			for _, prev := range r.IFDs {
				if prev.Offset == it.off {
					prev.Shared = true
					prev.Via = append(prev.Via, it.via)
				}
			}
			if it.via == "main" {
				p.ev("ifd.loop", "IFD next 指针回指已访问目录,停止跟随",
					map[string]any{"offset": it.off})
				for _, prev := range r.IFDs {
					if prev.Offset == it.off {
						prev.LoopBack = true
					}
				}
			} else {
				p.ev("ifd.shared_subdir", "子目录被多个父节点共享,只解析一次",
					map[string]any{"offset": it.off, "via": it.via})
			}
			continue
		}
		p.budget--
		r.Budget.Used++
		seen[it.off] = true
		order = append(order, it.off)
		ifd := p.parseIFD(it.off, len(order)-1, it.via)
		r.IFDs = append(r.IFDs, ifd)
		if ifd.Status == "ok" || ifd.Status == "loop" {
			if ifd.NextOffset != 0 {
				queue = append(queue, item{ifd.NextOffset, "main"})
			}
			for _, t := range ifd.Tags {
				if t.ID == 330 { // SubIFDs
					for _, off := range t.Uints {
						if off != 0 {
							queue = append(queue, item{off, fmt.Sprintf("subifd:%s", t.Identity(ifd.Offset))})
						}
					}
				}
			}
		}
	}
}

func (p *parser) parseIFD(off uint64, index int, via string) *IFD {
	r := p.res
	ifd := &IFD{Offset: off, Index: index, Visited: true, Status: "ok", Via: []string{via}}
	d := p.data
	big := r.Variant == BigTIFF
	var count uint64
	var entriesAt, nextAt uint64
	if big {
		if off+8 > uint64(len(d)) {
			ifd.Status = "unvisited"
			p.ev("ifd.out_of_bounds", "IFD 计数超出文件范围", map[string]any{"offset": off})
			return ifd
		}
		count = p.order.Uint64(d[off : off+8])
		entriesAt = off + 8
	} else {
		if off+2 > uint64(len(d)) {
			ifd.Status = "unvisited"
			p.ev("ifd.out_of_bounds", "IFD 计数超出文件范围", map[string]any{"offset": off})
			return ifd
		}
		count = uint64(p.order.Uint16(d[off : off+2]))
		entriesAt = off + 2
	}
	entrySize := uint64(12)
	nextSize := uint64(4)
	if big {
		entrySize = 20
		nextSize = 8
	}
	if count > 4096 {
		p.ev("ifd.count_suspicious", "IFD 条目数异常,按预算截断", map[string]any{"offset": off, "count": count})
		count = 4096
	}
	nextAt = entriesAt + count*entrySize
	ifd.ByteRange = [2]uint64{off, nextAt + nextSize}
	if nextAt+nextSize > uint64(len(d)) {
		p.ev("ifd.truncated", "IFD 条目区超出文件范围,按可读部分解析",
			map[string]any{"offset": off, "declaredCount": count})
		count = 0
		if entriesAt < uint64(len(d)) {
			count = (uint64(len(d)) - entriesAt) / entrySize
		}
		ifd.Status = "truncated"
	}
	for i := uint64(0); i < count; i++ {
		eoff := entriesAt + i*entrySize
		ifd.Tags = append(ifd.Tags, p.parseTag(eoff, off))
	}
	// next 指针
	if big {
		if nextAt+8 <= uint64(len(d)) {
			ifd.NextOffset = p.order.Uint64(d[nextAt : nextAt+8])
		}
	} else {
		if nextAt+4 <= uint64(len(d)) {
			ifd.NextOffset = uint64(p.order.Uint32(d[nextAt : nextAt+4]))
		}
	}
	return ifd
}

func (p *parser) parseTag(eoff, ifdOff uint64) Tag {
	r := p.res
	d := p.data
	big := r.Variant == BigTIFF
	t := Tag{}
	t.EntryRange = [2]uint64{eoff, eoff + 12}
	if big {
		t.EntryRange[1] = eoff + 20
	}
	t.ID = p.order.Uint16(d[eoff : eoff+2])
	t.Type = p.order.Uint16(d[eoff+2 : eoff+4])
	t.Name = tagNames[t.ID]
	if t.Name == "" {
		t.Name = fmt.Sprintf("Tag%d", t.ID)
	}
	t.TypeName = typeNames[t.Type]
	if t.TypeName == "" {
		t.TypeName = fmt.Sprintf("TYPE%d", t.Type)
		t.Diag = append(t.Diag, fmt.Sprintf("未知类型 %d,保留原始字节", t.Type))
	}
	var valFieldAt uint64
	if big {
		t.Count = p.order.Uint64(d[eoff+4 : eoff+12])
		t.ValueField = p.order.Uint64(d[eoff+12 : eoff+20])
		valFieldAt = eoff + 12
	} else {
		t.Count = uint64(p.order.Uint32(d[eoff+4 : eoff+8]))
		t.ValueField = uint64(p.order.Uint32(d[eoff+8 : eoff+12]))
		valFieldAt = eoff + 8
	}
	sz, ok := typeSizes[t.Type]
	if !ok {
		t.ValueRange = [2]uint64{valFieldAt, t.EntryRange[1]}
		t.Raw = append([]byte(nil), d[valFieldAt:t.EntryRange[1]]...)
		return t
	}
	total := sz * t.Count
	inlineCap := uint64(4)
	if big {
		inlineCap = 8
	}
	var valAt uint64
	if total <= inlineCap {
		t.Inline = true
		valAt = valFieldAt
		t.ValueRange = [2]uint64{valFieldAt, valFieldAt + total}
	} else {
		valAt = t.ValueField
		t.ValueRange = [2]uint64{t.ValueField, t.ValueField + total}
		if t.ValueField+total > uint64(len(d)) {
			t.Diag = append(t.Diag,
				fmt.Sprintf("外部值偏移 %d + %d 字节超出文件大小 %d,保留原偏移不解码",
					t.ValueField, total, len(d)))
			p.ev("tag.value_out_of_bounds", "tag 外部值偏移越界,保留原值",
				map[string]any{"tag": t.Identity(ifdOff), "offset": t.ValueField, "need": total, "fileSize": len(d)})
			return t
		}
	}
	if valAt+total <= uint64(len(d)) {
		t.Raw = append([]byte(nil), d[valAt:valAt+total]...)
	}
	p.decodeValues(&t)
	return t
}

func (p *parser) decodeValues(t *Tag) {
	if len(t.Raw) == 0 {
		return
	}
	o := p.order
	switch t.Type {
	case 1, 6, 7:
		for _, b := range t.Raw {
			t.Uints = append(t.Uints, uint64(b))
		}
	case 3, 8:
		for i := 0; i+2 <= len(t.Raw); i += 2 {
			t.Uints = append(t.Uints, uint64(o.Uint16(t.Raw[i:])))
		}
	case 4, 9:
		for i := 0; i+4 <= len(t.Raw); i += 4 {
			t.Uints = append(t.Uints, uint64(o.Uint32(t.Raw[i:])))
		}
	case 16, 17, 18:
		for i := 0; i+8 <= len(t.Raw); i += 8 {
			t.Uints = append(t.Uints, o.Uint64(t.Raw[i:]))
		}
	case 2:
		t.Text = string(t.Raw)
	case 5, 10:
		for i := 0; i+8 <= len(t.Raw); i += 8 {
			num := o.Uint32(t.Raw[i:])
			den := o.Uint32(t.Raw[i+4:])
			if den == 0 {
				t.Diag = append(t.Diag, "RATIONAL 分母为 0,保留原始字节")
				continue
			}
			t.Doubles = append(t.Doubles, float64(num)/float64(den))
		}
	case 11:
		for i := 0; i+4 <= len(t.Raw); i += 4 {
			t.Doubles = append(t.Doubles, float64(float32FromBits(o.Uint32(t.Raw[i:]))))
		}
	case 12:
		for i := 0; i+8 <= len(t.Raw); i += 8 {
			t.Doubles = append(t.Doubles, float64FromBits(o.Uint64(t.Raw[i:])))
		}
	}
}

func (p *parser) buildFragments() {
	r := p.res
	add := func(id string, rng [2]uint64) {
		if rng[1] > uint64(len(p.data)) || rng[0] > rng[1] {
			return
		}
		h := sha256.Sum256(p.data[rng[0]:rng[1]])
		r.Fragments = append(r.Fragments, Fragment{ID: id, ByteRange: rng, SHA256: hex.EncodeToString(h[:])})
	}
	add("header", r.HeaderRange)
	for _, ifd := range r.IFDs {
		if !ifd.Visited {
			continue
		}
		add(fmt.Sprintf("ifd@%d", ifd.Offset), ifd.ByteRange)
		for _, t := range ifd.Tags {
			switch t.ID {
			case 34735:
				add("geokeys", t.ValueRange)
			case 33922:
				add("tiepoint", t.ValueRange)
			case 33550:
				add("scale", t.ValueRange)
			case 34264:
				add("transform", t.ValueRange)
			case 324, 325, 273, 279, 322, 323, 278, 256, 257:
				add(fmt.Sprintf("tiling:%d", t.ID), t.ValueRange)
			}
		}
	}
	sort.Slice(r.Fragments, func(i, j int) bool { return r.Fragments[i].ID < r.Fragments[j].ID })
}

// FindTagForTest 导出 findTag 供外部测试与界面使用。
func (r *Result) FindTagForTest(id uint16) *Tag { return r.findTag(id) }

// findTag 在已访问 IFD 中查找第一个指定 tag。
func (r *Result) findTag(id uint16) *Tag {
	for _, ifd := range r.IFDs {
		if !ifd.Visited {
			continue
		}
		for i := range ifd.Tags {
			if ifd.Tags[i].ID == id {
				return &ifd.Tags[i]
			}
		}
	}
	return nil
}
