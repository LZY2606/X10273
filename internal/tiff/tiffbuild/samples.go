package tiffbuild

import (
	"fmt"

	"tiffgraph/internal/tiff"
)

// Sample 是一个带说明的领域样例。
type Sample struct {
	Name string
	Note string
	Data []byte
}

// geoKeys 构造标准 GeoKey 目录(版本 1.1.0)。
func geoKeys() E {
	vals := []uint16{
		1, 1, 0, 4,
		1024, 0, 1, 2, // GTModelType = projected
		1025, 0, 1, 1, // GTRasterType = PixelIsArea
		2048, 0, 1, 4326, // GeographicType = WGS84
		3076, 0, 1, 9001, // ProjLinearUnits = metre
	}
	return Short(34735, vals...)
}

func tiepointScale() []E {
	return []E{
		Double(33922, 0, 0, 0, 100.0, 200.0, 0), // ModelTiepoint
		Double(33550, 0.5, 0.5, 0),              // ModelPixelScale (单位: 地理单位/像素)
	}
}

// BaselineLE 基线: 小端 classic, 2x2 tile, GeoKey + tiepoint/scale。
func BaselineLE() Sample {
	f := &File{Order: tiff.LittleEndian}
	entries := []E{
		Long(256, 64), Long(257, 64),
		Short(258, 8), Short(259, 1), Short(262, 1),
		Short(277, 1), Short(284, 1),
		Short(322, 32), Short(323, 32),
	}
	entries = append(entries, tiepointScale()...)
	entries = append(entries, geoKeys())
	// tile 数组由布局确定: 先占位,构建后回填
	tileData := map[uint64][]byte{}
	f.Tail = tileData
	ifd := &IFD{Entries: entries}
	f.IFDs = []*IFD{ifd}
	// tile 数据放在固定偏移 1024 起(远离 IFD 与外部值区)
	base := uint64(1024)
	offs := []uint32{}
	counts := []uint32{}
	for i := 0; i < 4; i++ {
		tileData[base+uint64(i*64)] = []byte(fmt.Sprintf("tile-%d-payload", i))
		offs = append(offs, uint32(base+uint64(i*64)))
		counts = append(counts, 14)
	}
	ifd.Entries = append(ifd.Entries, Long(324, offs...), Long(325, counts...))
	return Sample{
		Name: "baseline-le-classic.tif",
		Note: "基线: 小端 classic TIFF, 2x2 tile, GeoKey 1.1.0, ModelTiepoint+Scale",
		Data: Build(f),
	}
}

// BigEndianClassic 大端 classic。
func BigEndianClassic() Sample {
	f := &File{Order: tiff.BigEndian}
	ifd := &IFD{Entries: []E{
		Long(256, 16), Long(257, 16),
		Short(258, 8), Short(259, 1),
		Short(278, 16),
		Long(273, 512), Long(279, 256),
	}}
	f.IFDs = []*IFD{ifd}
	f.Tail = map[uint64][]byte{512: make([]byte, 256)}
	return Sample{
		Name: "big-endian-classic.tif",
		Note: "大端(MM) classic TIFF, 单 strip",
		Data: Build(f),
	}
}

// BigTIFFWide BigTIFF 宽偏移。
func BigTIFFWide() Sample {
	f := &File{Order: tiff.LittleEndian, Big: true}
	ifd := &IFD{Entries: []E{
		Long(256, 32), Long(257, 32),
		Short(322, 16), Short(323, 16),
		Long(324, 4096, 4224, 4352, 4480),
		Long(325, 100, 100, 100, 100),
	}}
	f.IFDs = []*IFD{ifd}
	f.Tail = map[uint64][]byte{}
	for _, off := range []uint64{4096, 4224, 4352, 4480} {
		f.Tail[off] = make([]byte, 100)
	}
	return Sample{
		Name: "bigtiff-wide-offsets.tif",
		Note: "BigTIFF: 8 字节宽偏移, 2x2 tile",
		Data: Build(f),
	}
}

// IFDLoop next 指针回指形成循环。
func IFDLoop() Sample {
	f := &File{Order: tiff.LittleEndian}
	a := &IFD{Entries: []E{Long(256, 8), Long(257, 8)}}
	b := &IFD{Entries: []E{Long(256, 8), Long(257, 8)}}
	f.IFDs = []*IFD{a, b}
	// 第一个 IFD 在偏移 8(classic 头部), b 的 next 回指 a 形成循环
	b.Next = Ptr(8)
	return Sample{
		Name: "ifd-loop.tif",
		Note: "IFD 链循环: 第二个 IFD 的 next 指回第一个",
		Data: Build(f),
	}
}

// SharedSubdir 两个 SubIFDs 指向同一子目录。
func SharedSubdir() Sample {
	f := &File{Order: tiff.LittleEndian}
	sub := &IFD{At: Ptr(2048), Entries: []E{Long(256, 4), Long(257, 4)}}
	main := &IFD{Next: Ptr(0), Entries: []E{
		Long(256, 16), Long(257, 16),
		{Tag: 330, Type: 4, Values: []uint64{2048, 2048}, ForceExternal: true},
	}}
	f.IFDs = []*IFD{main, sub}
	return Sample{
		Name: "shared-subdir.tif",
		Note: "共享子目录: SubIFDs 数组中两个指针指向同一偏移 2048",
		Data: Build(f),
	}
}

// TileOverlap 两个 tile 字节范围重叠。
func TileOverlap() Sample {
	f := &File{Order: tiff.LittleEndian}
	ifd := &IFD{Entries: []E{
		Long(256, 64), Long(257, 32),
		Short(322, 32), Short(323, 32),
		Long(324, 1024, 1056), // 第二个 tile 与第一个重叠 [1024,1120) vs [1056,1152)
		Long(325, 96, 96),
	}}
	f.IFDs = []*IFD{ifd}
	f.Tail = map[uint64][]byte{1024: make([]byte, 192)}
	return Sample{
		Name: "tile-overlap.tif",
		Note: "tile 重叠: 两个 tile 的字节范围 [1024,1120) 与 [1056,1152) 重叠",
		Data: Build(f),
	}
}

// GeoConflict tiepoint+scale 与 ModelTransformation 冲突。
func GeoConflict() Sample {
	f := &File{Order: tiff.LittleEndian}
	entries := []E{
		Long(256, 32), Long(257, 32),
		Short(278, 32), Long(273, 1024), Long(279, 1024),
	}
	entries = append(entries, tiepointScale()...)
	entries = append(entries, geoKeys())
	// ModelTransformation 给出不同的平移,制造冲突
	entries = append(entries, Double(34264,
		0.5, 0, 0, 500.0,
		0, -0.5, 0, 900.0,
		0, 0, 0, 0,
		0, 0, 0, 1))
	f.IFDs = []*IFD{{Entries: entries}}
	f.Tail = map[uint64][]byte{1024: make([]byte, 1024)}
	return Sample{
		Name: "geokey-conflict.tif",
		Note: "GeoKey 冲突: ModelTiepoint+Scale 与 ModelTransformation 给出不同变换,两个候选都保留",
		Data: Build(f),
	}
}

// DeepChain 长 IFD 链,用于访问预算耗尽。
func DeepChain(n int) Sample {
	f := &File{Order: tiff.LittleEndian}
	for i := 0; i < n; i++ {
		f.IFDs = append(f.IFDs, &IFD{Entries: []E{
			Long(256, 8), Long(257, 8),
		}})
	}
	return Sample{
		Name: fmt.Sprintf("deep-chain-%d.tif", n),
		Note: fmt.Sprintf("%d 级 IFD 链, 用于检验访问预算耗尽", n),
		Data: Build(f),
	}
}

// StripMissing 缺失 strip(字节数为 0)。
func StripMissing() Sample {
	f := &File{Order: tiff.LittleEndian}
	ifd := &IFD{Entries: []E{
		Long(256, 32), Long(257, 64),
		Short(278, 16),
		Long(273, 1024, 2048, 3072, 4096),
		Long(279, 512, 0, 512, 512), // 第二个 strip 缺失
	}}
	f.IFDs = []*IFD{ifd}
	f.Tail = map[uint64][]byte{1024: make([]byte, 512), 3072: make([]byte, 512), 4096: make([]byte, 512)}
	return Sample{
		Name: "strip-missing.tif",
		Note: "缺失 strip: 第 2 个 strip 字节数为 0, 显式标记 missing 不拼凑",
		Data: Build(f),
	}
}

// BaselineV2 基线的局部修订: 仅 tile 数据变化(地理参考不变)。
func BaselineV2() Sample {
	s := BaselineLE()
	s.Name = "baseline-v2-tiledata.tif"
	s.Note = "基线修订版: 仅 tile 像素数据变化, 地理参考与 IFD 结构不变"
	// 改动一个 tile 的字节内容(不改变结构)
	f := &File{Order: tiff.LittleEndian}
	entries := []E{
		Long(256, 64), Long(257, 64),
		Short(258, 8), Short(259, 1), Short(262, 1),
		Short(277, 1), Short(284, 1),
		Short(322, 32), Short(323, 32),
	}
	entries = append(entries, tiepointScale()...)
	entries = append(entries, geoKeys())
	tileData := map[uint64][]byte{}
	f.Tail = tileData
	ifd := &IFD{Entries: entries}
	f.IFDs = []*IFD{ifd}
	base := uint64(1024)
	offs := []uint32{}
	counts := []uint32{}
	for i := 0; i < 4; i++ {
		payload := fmt.Sprintf("tile-%d-payload", i)
		if i == 2 {
			payload = "tile-2-REVISED!" // 局部源数据改动
		}
		tileData[base+uint64(i*64)] = []byte(payload)
		offs = append(offs, uint32(base+uint64(i*64)))
		counts = append(counts, uint32(len(payload)))
	}
	ifd.Entries = append(ifd.Entries, Long(324, offs...), Long(325, counts...))
	s.Data = Build(f)
	return s
}

// All 返回全部边界样例(固定顺序)。
func All() []Sample {
	return []Sample{
		BaselineLE(), BigEndianClassic(), BigTIFFWide(), IFDLoop(),
		SharedSubdir(), TileOverlap(), GeoConflict(), DeepChain(12), StripMissing(),
	}
}
