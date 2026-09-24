package casepack

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"ifd-roadmap/internal/tiff"
)

type Material struct {
	ID            string `json:"id"`
	Role          string `json:"role"`
	Label         string `json:"label"`
	ParentVersion string `json:"parentVersion,omitempty"`
	Classic       bool   `json:"classic"`
	BigTIFF       bool   `json:"bigTiff"`
	Little        bool   `json:"little"`
	Base64        string `json:"base64"`
}

type Action struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Label           string `json:"label"`
	MaterialID      string `json:"materialId,omitempty"`
	ParentVersion   string `json:"parentVersion,omitempty"`
	NewBudget       int    `json:"newBudget,omitempty"`
	TargetCandidate string `json:"targetCandidate,omitempty"`
	Decision        string `json:"decision,omitempty"`
	Reason          string `json:"reason,omitempty"`
	RuleVersion     string `json:"ruleVersion,omitempty"`
}

type Pack struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Version     string     `json:"version"`
	RuleVersion string     `json:"ruleVersion"`
	Seed        int64      `json:"seed"`
	Summary     string     `json:"summary"`
	Materials   []Material `json:"materials"`
	Actions     []Action   `json:"actions"`
}

func BuiltIn() (*Pack, error) {
	classic, _, err := classicBigEndianCycle()
	if err != nil {
		return nil, err
	}
	big, _, err := littleBigTIFFShared(0)
	if err != nil {
		return nil, err
	}
	overlap, _, err := littleBigTIFFShared(250)
	if err != nil {
		return nil, err
	}
	return &Pack{
		ID:          "ifd-roadmap-offline-case-v1",
		Title:       "IFD 路标室：TIFF/GeoTIFF 目录图、块数据与地理变换证据",
		Version:     "2026-09-24-v1",
		RuleVersion: "rules-v1",
		Seed:        20260924,
		Summary:     "导入大小端 classic/BigTIFF 字节，保留原值与偏移，重放循环、共享、重叠、GeoKey 冲突和访问预算。",
		Materials: []Material{
			{ID: "a1-be-classic-cycle", Role: "baseline", Label: "A1 大端 classic TIFF：next/SubIFD 循环、strips、冲突 GeoKey", Classic: true, Base64: base64.StdEncoding.EncodeToString(classic)},
			{ID: "a2-le-bigtiff-shared", Role: "baseline", Label: "A2 小端 BigTIFF：宽偏移、共享子目录、缺失/越界 tile", BigTIFF: true, Little: true, Base64: base64.StdEncoding.EncodeToString(big)},
			{ID: "a3-le-bigtiff-local-patch", Role: "local-source-change", ParentVersion: "a2-le-bigtiff-shared", Label: "A3 只改 A2 的一个 tile 偏移，制造重叠并验证局部失效", BigTIFF: true, Little: true, Base64: base64.StdEncoding.EncodeToString(overlap)},
			{ID: "a4-truncated", Role: "format-error", Label: "A4 明确格式错误：只有三个字节，无法形成 TIFF 头", Base64: base64.StdEncoding.EncodeToString([]byte("II\x2a"))},
		},
		Actions: []Action{
			{ID: "act-import-a1", Kind: "import", Label: "导入 A1 大端 classic 基线", MaterialID: "a1-be-classic-cycle"},
			{ID: "act-import-a2", Kind: "import", Label: "导入 A2 小端 BigTIFF 基线", MaterialID: "a2-le-bigtiff-shared"},
			{ID: "act-import-a4", Kind: "import", Label: "导入 A4 格式错误，保存诊断", MaterialID: "a4-truncated"},
			{ID: "act-rule-budget", Kind: "parameter-revision", Label: "参数修订：IFD 访问预算从 64 降为 2", MaterialID: "a1-be-classic-cycle", NewBudget: 2, RuleVersion: "rules-v2-budget-2"},
			{ID: "act-local-a3", Kind: "source-revision", Label: "局部源数据改动：A2 的 tile 偏移改为 250", MaterialID: "a3-le-bigtiff-local-patch", ParentVersion: "a2-le-bigtiff-shared"},
			{ID: "act-conflicting-decision", Kind: "decision", Label: "互相冲突的人工决定：接受 matrix，拒绝 tiepoint/scale", MaterialID: "a1-be-classic-cycle", TargetCandidate: "matrix", Decision: "accept", Reason: "离线案例要求演示冲突候选和撤销", RuleVersion: "manual-decision-v1"},
		},
	}, nil
}

func MustJSON() []byte {
	pack, err := BuiltIn()
	if err != nil {
		panic(err)
	}
	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		panic(err)
	}
	return data
}

func classicBigEndianCycle() ([]byte, tiff.BuildInfo, error) {
	geo := []int64{1, 1, 0, 0, 1024, 0, 1, 1, 1025, 0, 1, 1, 3072, 0, 1, 32633, 3072, 0, 1, 32634, 3076, 0, 1, 9001}
	tie := []float64{0, 0, 0, 1000, 5000, 0}
	matrix := identity16()
	spec := tiff.FileSpec{Big: false, Padding: 900, IFDs: []tiff.IFDSpec{
		{Offset: 8, NextTarget: 1, Entries: []tiff.EntrySpec{
			{Tag: 256, Type: 3, Ints: []int64{128}}, {Tag: 257, Type: 3, Ints: []int64{64}},
			{Tag: 258, Type: 3, Ints: []int64{8}}, {Tag: 259, Type: 3, Ints: []int64{1}},
			{Tag: 277, Type: 3, Ints: []int64{1}}, {Tag: 278, Type: 3, Ints: []int64{32}},
			{Tag: 273, Type: 4, Ints: []int64{700, 760}}, {Tag: 279, Type: 4, Ints: []int64{60, 60}},
			{Tag: 330, Type: 4, Ints: []int64{120, 220}},
			{Tag: 33922, Type: 12, Doubles: tie}, {Tag: 34264, Type: 12, Doubles: matrix},
			{Tag: 34735, Type: 3, Ints: geo},
		}},
		{Offset: 120, NextTarget: 2, Entries: []tiff.EntrySpec{
			{Tag: 256, Type: 3, Ints: []int64{64}}, {Tag: 257, Type: 3, Ints: []int64{32}},
			{Tag: 258, Type: 3, Ints: []int64{8}}, {Tag: 259, Type: 3, Ints: []int64{1}},
			{Tag: 273, Type: 4, Ints: []int64{720}}, {Tag: 279, Type: 4, Ints: []int64{40}},
			{Tag: 278, Type: 3, Ints: []int64{32}},
		}},
		{Offset: 220, NextTarget: 1, Entries: []tiff.EntrySpec{
			{Tag: 256, Type: 3, Ints: []int64{32}}, {Tag: 257, Type: 3, Ints: []int64{16}},
			{Tag: 258, Type: 3, Ints: []int64{8}}, {Tag: 259, Type: 3, Ints: []int64{1}},
			{Tag: 273, Type: 4, Ints: []int64{780}}, {Tag: 279, Type: 4, Ints: []int64{40}},
			{Tag: 278, Type: 3, Ints: []int64{16}},
		}},
	}}
	data, info, err := tiff.Build(spec)
	if err != nil {
		return nil, info, err
	}
	if len(data) < 820 {
		return nil, info, fmt.Errorf("classic case allocation exceeded fixture: %d", len(data))
	}
	for i := 700; i < len(data); i++ {
		data[i] = byte(i%251 + 1)
	}
	return data, info, nil
}

func littleBigTIFFShared(firstTileOffset uint64) ([]byte, tiff.BuildInfo, error) {
	geo := []int64{1, 1, 0, 0, 1024, 0, 1, 1, 1025, 0, 1, 1, 2048, 0, 1, 4326, 2054, 0, 1, 9102}
	scale := []float64{0.25, 0.25, 0}
	spec := tiff.FileSpec{Big: true, Little: true, Padding: 900, IFDs: []tiff.IFDSpec{
		{Offset: 16, NextTarget: -1, Entries: []tiff.EntrySpec{
			{Tag: 256, Type: 3, Ints: []int64{32}}, {Tag: 257, Type: 3, Ints: []int64{32}},
			{Tag: 258, Type: 3, Ints: []int64{8}}, {Tag: 259, Type: 3, Ints: []int64{1}},
			{Tag: 277, Type: 3, Ints: []int64{1}}, {Tag: 322, Type: 3, Ints: []int64{16}},
			{Tag: 323, Type: 3, Ints: []int64{16}},
			{Tag: 324, Type: 16, UInts: []uint64{firstTileOffset, 300, 400, 500, 0x100000000}},
			{Tag: 325, Type: 16, UInts: []uint64{40, 40, 40, 40, 8}},
			{Tag: 330, Type: 16, UInts: []uint64{240, 360, 240}},
			{Tag: 33550, Type: 12, Doubles: scale}, {Tag: 34735, Type: 3, Ints: geo},
		}},
		{Offset: 240, NextTarget: -1, Entries: []tiff.EntrySpec{
			{Tag: 256, Type: 3, Ints: []int64{16}}, {Tag: 257, Type: 3, Ints: []int64{16}},
			{Tag: 258, Type: 3, Ints: []int64{8}}, {Tag: 259, Type: 3, Ints: []int64{1}},
			{Tag: 322, Type: 3, Ints: []int64{16}}, {Tag: 323, Type: 3, Ints: []int64{16}},
			{Tag: 324, Type: 16, UInts: []uint64{600}}, {Tag: 325, Type: 16, UInts: []uint64{40}},
		}},
		{Offset: 360, NextTarget: -1, Entries: []tiff.EntrySpec{
			{Tag: 256, Type: 3, Ints: []int64{8}}, {Tag: 257, Type: 3, Ints: []int64{8}},
			{Tag: 258, Type: 3, Ints: []int64{8}}, {Tag: 259, Type: 3, Ints: []int64{1}},
			{Tag: 322, Type: 3, Ints: []int64{8}}, {Tag: 323, Type: 3, Ints: []int64{8}},
			{Tag: 324, Type: 16, UInts: []uint64{650}}, {Tag: 325, Type: 16, UInts: []uint64{40}},
		}},
	}}
	data, info, err := tiff.Build(spec)
	if err != nil {
		return nil, info, err
	}
	for i := 250; i < 690; i++ {
		data[i] = byte(i%239 + 2)
	}
	return data, info, nil
}

func identity16() []float64 {
	values := make([]float64, 16)
	for i := 0; i < 4; i++ {
		values[i*4+i] = 1
	}
	return values
}
