// Package casepack ships the offline domain case "IFD 路标室" (IFD
// Landmark Room): a deterministic set of TIFF/GeoTIFF samples and a
// narrative manifest covering baseline import, one parameter revision,
// one local source change and one conflicting human decision. All bytes
// are generated deterministically from fixed specs — no network, no
// host files, no randomness.
package casepack

import (
	"ifdmap/internal/tiff"
)

// Sample is one named TIFF byte stream with its review intent.
type Sample struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Bytes       []byte `json:"-"`
}

// Expectation binds a sample to the diagnostic codes a replay must hit.
type Expectation struct {
	Sample   string   `json:"sample"`
	Scenario string   `json:"scenario"`
	Codes    []string `json:"codes"`
	// Budget, when non-zero, is the visit budget the replay must use.
	Budget int `json:"budget,omitempty"`
}

// Step is one narrative beat of the case.
type Step struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // import | verify | revise-params | revise-source | decide | undo
	Summary string `json:"summary"`
	Sample  string `json:"sample,omitempty"`
	Parent  string `json:"parent,omitempty"`
}

// Manifest is the machine-readable case script.
type Manifest struct {
	Name         string        `json:"name"`
	Title        string        `json:"title"`
	Narrative    string        `json:"narrative"`
	Steps        []Step        `json:"steps"`
	Expectations []Expectation `json:"expectations"`
}

// Samples returns the deterministic sample set in stable order.
func Samples() []Sample {
	return []Sample{
		{
			Name:        "baseline-landmark",
			Description: "基线：classic 小端，IFD 链 + 子目录，tile 范围与 GeoKey v1.1.0 完整保留",
			Bytes:       tiff.Build(baseline()),
		},
		{
			Name:        "bigtiff-wide-offsets",
			Description: "BigTIFF 大端：8 字节宽偏移与 LONG8 原值",
			Bytes:       tiff.Build(bigTIFFWide()),
		},
		{
			Name:        "ifd-cycle",
			Description: "IFD 链成环：next 指针回指，解析必须终止",
			Bytes:       tiff.Build(ifdCycle()),
		},
		{
			Name:        "shared-subdir",
			Description: "共享子目录：两个父 IFD 引用同一个 SubIFD",
			Bytes:       tiff.Build(sharedSubdir()),
		},
		{
			Name:        "tile-overlap",
			Description: "tile 重叠与缺失：重叠范围显式呈现，不拼凑",
			Bytes:       tiff.Build(tileOverlap()),
		},
		{
			Name:        "geokey-conflict",
			Description: "GeoKey 冲突：Tiepoint/Scale 与 Transformation 给出不同变换",
			Bytes:       tiff.Build(geoConflict()),
		},
		{
			Name:        "budget-exhaustion",
			Description: "访问预算耗尽：12 级 IFD 链在预算 4 处截断",
			Bytes:       tiff.Build(longChain(12)),
		},
	}
}

// SampleBytes returns one sample by name.
func SampleBytes(name string) []byte {
	for _, s := range Samples() {
		if s.Name == name {
			return s.Bytes
		}
	}
	return nil
}

func baseline() *tiff.FileSpec {
	return baselineV2([]uint64{256, 256, 256, 256})
}

// baselineV2 is the local source change of the case: identical to the
// baseline except tile byte counts, which now overlap.
func baselineV2(counts []uint64) *tiff.FileSpec {
	geo := []uint64{
		1, 1, 0, 3,
		1024, 0, 1, 2,
		1025, 0, 1, 1,
		2048, 0, 1, 4326,
	}
	return &tiff.FileSpec{
		Endian: tiff.LittleEndian,
		IFDs: []*tiff.IFDSpec{
			{
				Next: 1,
				Tags: []tiff.TagSpec{
					{Code: tiff.TagImageWidth, Values: []uint64{64}},
					{Code: tiff.TagImageLength, Values: []uint64{64}},
					{Code: tiff.TagBitsPerSample, Values: []uint64{8}},
					{Code: tiff.TagCompression, Values: []uint64{1}},
					{Code: tiff.TagTileWidth, Values: []uint64{32}},
					{Code: tiff.TagTileLength, Values: []uint64{32}},
					{Code: tiff.TagTileOffsets, Values: []uint64{1024, 1280, 1536, 1792}},
					{Code: tiff.TagTileByteCounts, Values: counts},
					{Code: tiff.TagSubIFDs, Refs: []int{2}},
					{Code: tiff.TagModelPixelScale, Floats: []float64{0.5, 0.5, 0}},
					{Code: tiff.TagModelTiepoint, Floats: []float64{0, 0, 0, 114.0, 32.0, 0}},
					{Code: tiff.TagGeoKeyDirectory, Values: geo},
				},
			},
			{
				Next: -1,
				Tags: []tiff.TagSpec{
					{Code: tiff.TagImageWidth, Values: []uint64{16}},
					{Code: tiff.TagImageLength, Values: []uint64{16}},
					{Code: tiff.TagRowsPerStrip, Values: []uint64{8}},
					{Code: tiff.TagStripOffsets, Values: []uint64{2048, 2176}},
					{Code: tiff.TagStripByteCounts, Values: []uint64{128, 128}},
				},
			},
			{
				Next: -1,
				Tags: []tiff.TagSpec{
					{Code: tiff.TagImageWidth, Values: []uint64{8}},
					{Code: tiff.TagImageLength, Values: []uint64{8}},
				},
			},
		},
		FileSize: 4096,
	}
}

// BaselineRevision returns the locally-changed v2 bytes (tile byte
// counts now overlap) used by the case's source-revision step.
func BaselineRevision() []byte {
	return tiff.Build(baselineV2([]uint64{256, 300, 256, 256}))
}

func bigTIFFWide() *tiff.FileSpec {
	return &tiff.FileSpec{
		Endian: tiff.BigEndian,
		Big:    true,
		IFDs: []*tiff.IFDSpec{
			{
				Next: -1,
				Tags: []tiff.TagSpec{
					{Code: tiff.TagImageWidth, Type: 16, Values: []uint64{1 << 20}},
					{Code: tiff.TagImageLength, Type: 16, Values: []uint64{1 << 19}},
					{Code: tiff.TagTileWidth, Values: []uint64{256}},
					{Code: tiff.TagTileLength, Values: []uint64{256}},
					{Code: tiff.TagTileOffsets, Type: 16, Values: []uint64{4096, 8192, 12288}},
					{Code: tiff.TagTileByteCounts, Type: 16, Values: []uint64{1024, 1024, 1024}},
				},
			},
		},
		FileSize: 16384,
	}
}

func ifdCycle() *tiff.FileSpec {
	return &tiff.FileSpec{
		Endian: tiff.LittleEndian,
		IFDs: []*tiff.IFDSpec{
			{Next: 1, Tags: []tiff.TagSpec{{Code: tiff.TagImageWidth, Values: []uint64{10}}}},
			{Next: 0, Tags: []tiff.TagSpec{{Code: tiff.TagImageWidth, Values: []uint64{20}}}},
		},
	}
}

func sharedSubdir() *tiff.FileSpec {
	return &tiff.FileSpec{
		Endian: tiff.LittleEndian,
		IFDs: []*tiff.IFDSpec{
			{Next: 1, Tags: []tiff.TagSpec{
				{Code: tiff.TagImageWidth, Values: []uint64{64}},
				{Code: tiff.TagSubIFDs, Refs: []int{2}},
			}},
			{Next: -1, Tags: []tiff.TagSpec{
				{Code: tiff.TagImageWidth, Values: []uint64{32}},
				{Code: tiff.TagSubIFDs, Refs: []int{2}},
			}},
			{Next: -1, Tags: []tiff.TagSpec{
				{Code: tiff.TagImageWidth, Values: []uint64{8}},
				{Code: tiff.TagImageLength, Values: []uint64{8}},
			}},
		},
	}
}

func tileOverlap() *tiff.FileSpec {
	return &tiff.FileSpec{
		Endian: tiff.LittleEndian,
		IFDs: []*tiff.IFDSpec{
			{
				Next: -1,
				Tags: []tiff.TagSpec{
					{Code: tiff.TagImageWidth, Values: []uint64{64}},
					{Code: tiff.TagImageLength, Values: []uint64{64}},
					{Code: tiff.TagTileWidth, Values: []uint64{32}},
					{Code: tiff.TagTileLength, Values: []uint64{32}},
					{Code: tiff.TagTileOffsets, Values: []uint64{512, 560, 700, 800}},
					{Code: tiff.TagTileByteCounts, Values: []uint64{80, 80, 0, 64}},
				},
			},
		},
		FileSize: 1024,
	}
}

func geoConflict() *tiff.FileSpec {
	return &tiff.FileSpec{
		Endian: tiff.LittleEndian,
		IFDs: []*tiff.IFDSpec{
			{
				Next: -1,
				Tags: []tiff.TagSpec{
					{Code: tiff.TagImageWidth, Values: []uint64{64}},
					{Code: tiff.TagImageLength, Values: []uint64{64}},
					{Code: tiff.TagRowsPerStrip, Values: []uint64{64}},
					{Code: tiff.TagStripOffsets, Values: []uint64{2048}},
					{Code: tiff.TagStripByteCounts, Values: []uint64{4096}},
					{Code: tiff.TagModelPixelScale, Floats: []float64{0.5, 0.5, 0}},
					{Code: tiff.TagModelTiepoint, Floats: []float64{0, 0, 0, 114.0, 32.0, 0}},
					// Transformation matrix disagrees: scale 2.0, origin (10, 20).
					{Code: tiff.TagModelTransformation, Floats: []float64{
						2, 0, 0, 10,
						0, -2, 0, 20,
						0, 0, 0, 0,
						0, 0, 0, 1,
					}},
					{Code: tiff.TagGeoKeyDirectory, Values: []uint64{
						1, 1, 0, 2,
						1024, 0, 1, 2,
						2048, 0, 1, 4326,
					}},
				},
			},
		},
		FileSize: 8192,
	}
}

func longChain(n int) *tiff.FileSpec {
	var ifds []*tiff.IFDSpec
	for i := 0; i < n; i++ {
		next := i + 1
		if i == n-1 {
			next = -1
		}
		ifds = append(ifds, &tiff.IFDSpec{
			Next: next,
			Tags: []tiff.TagSpec{{Code: tiff.TagImageWidth, Values: []uint64{uint64(i + 1)}}},
		})
	}
	return &tiff.FileSpec{Endian: tiff.LittleEndian, IFDs: ifds}
}

// CaseManifest returns the case script for "IFD 路标室".
func CaseManifest() *Manifest {
	return &Manifest{
		Name:  "ifd-landmark-room",
		Title: "IFD 路标室：TIFF/GeoTIFF 目录图、块数据与地理变换证据",
		Narrative: "影像数据团队导入 TIFF 字节、多级 IFD、tile/strip 与 GeoKey，" +
			"审查像素存储与地理参考的来源链。初始导入完整保留大小端、classic/BigTIFF、" +
			"tag 原值、外部值偏移、IFD 链、tile/strip 范围与 GeoKey 版本；随后一次参数修订、" +
			"一次局部源数据改动与一次互相冲突的人工决定，全部写入事件历史。",
		Steps: []Step{
			{ID: "s1", Kind: "import", Sample: "baseline-landmark", Summary: "导入基线并核对七项保留清单"},
			{ID: "s2", Kind: "verify", Sample: "baseline-landmark", Summary: "基线核对：大小端/变体/tag 原值/外部偏移/IFD 链/tile 范围/GeoKey 版本"},
			{ID: "s3", Kind: "revise-params", Summary: "参数修订：IFD 访问预算 256 → 4"},
			{ID: "s4", Kind: "revise-source", Sample: "baseline-landmark", Parent: "baseline-landmark", Summary: "局部源改动：仅 tile 字节数变化（产生重叠），geo 派生不应过期"},
			{ID: "s5", Kind: "decide", Sample: "geokey-conflict", Summary: "冲突裁定：两位审阅者分别接受不同候选，随后撤销其一"},
		},
		Expectations: []Expectation{
			{Sample: "baseline-landmark", Scenario: "基线保留", Codes: nil},
			{Sample: "bigtiff-wide-offsets", Scenario: "BigTIFF 宽偏移", Codes: nil},
			{Sample: "ifd-cycle", Scenario: "IFD 循环", Codes: []string{tiff.DiagIFDCycle}},
			{Sample: "shared-subdir", Scenario: "共享子目录", Codes: []string{tiff.DiagSharedSubIFD}},
			{Sample: "tile-overlap", Scenario: "tile 重叠与缺失", Codes: []string{"BLOCK_OVERLAP", "BLOCK_MISSING"}},
			{Sample: "geokey-conflict", Scenario: "GeoKey 冲突", Codes: []string{}},
			{Sample: "budget-exhaustion", Scenario: "访问预算耗尽", Codes: []string{tiff.DiagBudgetExhausted}, Budget: 4},
		},
	}
}
