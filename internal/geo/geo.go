// Package geo 解释 GeoKey 目录并推导地理变换候选。
// 规则：ModelTiepoint+ModelPixelScale 与 ModelTransformation 冲突时保留全部候选，
// 无法转换的值留在原位置并附诊断，不为了画图丢弃。
package geo

import (
	"fmt"
	"math"

	"ifdroom/internal/tiff"
)

// AlgoVersion 派生算法版本，随规则变更递增。
const AlgoVersion = "geo/1.0.0"

// Candidate 是一个地理变换候选（一等数据，可接受/拒绝/过期）。
type Candidate struct {
	Kind     string      `json:"kind"` // "tiepoint_scale" | "matrix"
	Datum    string      `json:"datum"`
	Unit     string      `json:"unit"`
	Scale    [3]float64  `json:"scale,omitempty"`
	Tiepoint [6]float64  `json:"tiepoint,omitempty"` // I,J,K,X,Y,Z
	Matrix   [16]float64 `json:"matrix,omitempty"`
	// 由候选推导的仿射参数：X = A*i + B*j + C ; Y = D*i + E*j + F
	A, B, C, D, E, F float64  `json:"a,b,c,d,e,f"`
	SourceTags       []string `json:"source_tags"` // 直接来源 tag 的稳定身份
	Conflict         bool     `json:"conflict"`
	Notes            []string `json:"notes,omitempty"`
}

// epsgNames 仅覆盖案例包用到的基准，其余保留原始代码并附诊断。
var epsgNames = map[uint32]string{
	4326: "EPSG:4326", 32633: "EPSG:32633", 3857: "EPSG:3857",
}

var linearUnits = map[uint32]string{
	9001: "m", 9002: "ft", 9003: "us-ft",
}
var angularUnits = map[uint32]string{
	9101: "rad", 9102: "deg",
}

type keyVal struct {
	loc, count, off uint32
}

// Candidates 从解析结果推导候选与诊断。rawTagID 由调用方提供 tag 身份查询。
func Candidates(p *tiff.Result, tagID func(ifdOrd int, code uint16) (string, bool)) ([]Candidate, []tiff.Diagnostic) {
	var diags []tiff.Diagnostic
	kv := map[uint32]keyVal{}
	version := ""
	if len(p.GeoKeyVersion) == 4 {
		version = fmt.Sprintf("%d.%d.%d", p.GeoKeyVersion[0], p.GeoKeyVersion[1], p.GeoKeyVersion[2])
	}
	for _, k := range p.GeoKeys {
		kv[k.KeyID] = keyVal{k.TIFFTagLocation, k.Count, k.ValueOffset}
	}
	datum := "unknown"
	if v, ok := kv[3072]; ok && v.loc == 0 { // ProjectedCSTypeGeoKey
		if n, ok := epsgNames[v.off]; ok {
			datum = n
		} else {
			datum = fmt.Sprintf("EPSG:%d", v.off)
			diags = append(diags, tiff.Diagnostic{Code: "datum_unresolved", Message: fmt.Sprintf("ProjectedCSTypeGeoKey=%d 不在内置表，保留原值", v.off)})
		}
	} else if v, ok := kv[2048]; ok && v.loc == 0 { // GeographicTypeGeoKey
		if n, ok := epsgNames[v.off]; ok {
			datum = n
		} else {
			datum = fmt.Sprintf("EPSG:%d", v.off)
			diags = append(diags, tiff.Diagnostic{Code: "datum_unresolved", Message: fmt.Sprintf("GeographicTypeGeoKey=%d 不在内置表，保留原值", v.off)})
		}
	} else {
		diags = append(diags, tiff.Diagnostic{Code: "datum_missing", Message: "缺少 Projected/Geographic CS GeoKey，基准未知"})
	}
	unit := "unknown"
	if v, ok := kv[3076]; ok && v.loc == 0 { // ProjLinearUnitsGeoKey
		if u, ok := linearUnits[v.off]; ok {
			unit = u
		} else {
			diags = append(diags, tiff.Diagnostic{Code: "unit_unresolved", Message: fmt.Sprintf("ProjLinearUnitsGeoKey=%d 无法转换，保留原值", v.off)})
		}
	} else if v, ok := kv[2054]; ok && v.loc == 0 { // GeogAngularUnitsGeoKey
		if u, ok := angularUnits[v.off]; ok {
			unit = u
		} else {
			diags = append(diags, tiff.Diagnostic{Code: "unit_unresolved", Message: fmt.Sprintf("GeogAngularUnitsGeoKey=%d 无法转换，保留原值", v.off)})
		}
	}

	var cands []Candidate
	var scaleTag, tieTag, mtxTag *tiff.Tag
	for i := range p.IFDs {
		for j := range p.IFDs[i].Tags {
			t := &p.IFDs[i].Tags[j]
			switch t.Code {
			case tiff.TagModelPixelScale:
				scaleTag = t
			case tiff.TagModelTiepoint:
				tieTag = t
			case tiff.TagModelTransformation:
				mtxTag = t
			}
		}
	}
	src := func(t *tiff.Tag) string {
		if id, ok := tagID(t.IfdOrdinal, t.Code); ok {
			return id
		}
		return fmt.Sprintf("ifd%d/tag%d", t.IfdOrdinal, t.Code)
	}
	if scaleTag != nil && tieTag != nil {
		s := scaleTag.Doubles()
		tp := tieTag.Doubles()
		if len(s) >= 2 && len(tp) >= 6 {
			c := Candidate{Kind: "tiepoint_scale", Datum: datum, Unit: unit,
				SourceTags: []string{src(scaleTag), src(tieTag)}}
			copy(c.Scale[:], s[:min(3, len(s))])
			copy(c.Tiepoint[:], tp[:6])
			// X = X0 + (i-I)*sx ; Y = Y0 - (j-J)*sy
			c.A = c.Scale[0]
			c.B = 0
			c.C = c.Tiepoint[3] - c.Tiepoint[0]*c.Scale[0]
			c.D = 0
			c.E = -c.Scale[1]
			c.F = c.Tiepoint[4] + c.Tiepoint[1]*c.Scale[1]
			c.Notes = append(c.Notes, "geokey_version="+version)
			cands = append(cands, c)
		} else {
			diags = append(diags, tiff.Diagnostic{Code: "geo_params_short", Message: "ModelPixelScale/Tiepoint 数据不足，候选未生成，原值保留"})
		}
	} else if scaleTag != nil || tieTag != nil {
		diags = append(diags, tiff.Diagnostic{Code: "geo_params_partial", Message: "ModelPixelScale 与 ModelTiepoint 未成对出现"})
	}
	if mtxTag != nil {
		m := mtxTag.Doubles()
		if len(m) == 16 {
			c := Candidate{Kind: "matrix", Datum: datum, Unit: unit,
				SourceTags: []string{src(mtxTag)}}
			copy(c.Matrix[:], m)
			c.A, c.B, c.C = m[0], m[1], m[3]
			c.D, c.E, c.F = m[4], m[5], m[7]
			c.Notes = append(c.Notes, "geokey_version="+version)
			cands = append(cands, c)
		} else {
			diags = append(diags, tiff.Diagnostic{Code: "geo_matrix_short", Message: fmt.Sprintf("ModelTransformation 需要 16 个 double，实得 %d", len(m))})
		}
	}
	if scaleTag != nil && tieTag != nil && mtxTag != nil {
		for i := range cands {
			cands[i].Conflict = true
		}
		diags = append(diags, tiff.Diagnostic{Code: "geo_transform_conflict",
			Message: "ModelTiepoint+Scale 与 ModelTransformation 同时存在，保留全部候选由人工裁定"})
	}
	_ = math.NaN // 保留 math 引用位，后续误差计算使用
	return cands, diags
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
