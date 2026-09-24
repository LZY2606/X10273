package tiff

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

func float64FromBits(b uint64) float64 { return math.Float64frombits(b) }
func float32FromBits(b uint32) float32 { return math.Float32frombits(b) }

var geoKeyNames = map[uint16]string{
	1024: "GTModelType", 1025: "GTRasterType", 1026: "GTCitation",
	2048: "GeographicType", 2049: "GeogCitation", 2050: "GeogGeodeticDatum",
	2054: "GeogAngularUnits", 2056: "GeogEllipsoid", 2057: "GeogSemiMajorAxis",
	3072: "ProjectedCSType", 3074: "Projection", 3075: "ProjCoordTrans",
	3076: "ProjLinearUnits", 4096: "VerticalCSType", 4099: "VerticalUnits",
}

// buildGeo 解析 GeoKey 目录并生成地理变换候选;冲突时保留全部候选。
func (p *parser) buildGeo() {
	r := p.res
	gk := r.findTag(34735)
	if gk == nil || len(gk.Uints) < 4 {
		return
	}
	u := gk.Uints
	raw16 := make([]uint16, len(u))
	for i, v := range u {
		raw16[i] = uint16(v)
	}
	geo := &Geo{
		KeyDirectoryVersion: int(u[0]),
		KeyRevision:         int(u[1]),
		MinorRevision:       int(u[2]),
		NumberOfKeys:        int(u[3]),
		Raw:                 raw16,
	}
	p.ev("geokey.version", fmt.Sprintf("GeoKey 目录版本 %d.%d.%d, %d 个键",
		geo.KeyDirectoryVersion, geo.KeyRevision, geo.MinorRevision, geo.NumberOfKeys),
		map[string]any{"version": geo.KeyDirectoryVersion, "revision": geo.KeyRevision,
			"minor": geo.MinorRevision, "keys": geo.NumberOfKeys})
	doubles := r.findTag(34736)
	ascii := r.findTag(34737)
	n := geo.NumberOfKeys
	if 4+4*n > len(u) {
		p.ev("geokey.truncated", "GeoKey 目录声明的键数超出实际数据",
			map[string]any{"declared": n, "available": (len(u) - 4) / 4})
		n = (len(u) - 4) / 4
	}
	for i := 0; i < n; i++ {
		k := GeoKey{
			ID:       uint16(u[4+4*i]),
			Location: uint16(u[5+4*i]),
			Count:    uint16(u[6+4*i]),
			Offset:   uint16(u[7+4*i]),
		}
		k.Name = geoKeyNames[k.ID]
		switch k.Location {
		case 0:
			k.Resolved = fmt.Sprintf("%d", k.Offset)
		case 34736:
			if doubles != nil && int(k.Offset)+int(k.Count) <= len(doubles.Doubles) {
				parts := []string{}
				for _, v := range doubles.Doubles[k.Offset : int(k.Offset)+int(k.Count)] {
					parts = append(parts, fmt.Sprintf("%g", v))
				}
				k.Resolved = strings.Join(parts, ",")
			} else {
				k.Resolved = "无法解析: GeoDoubleParams 越界"
			}
		case 34737:
			if ascii != nil && int(k.Offset)+int(k.Count) <= len(ascii.Text) {
				k.Resolved = strings.TrimRight(ascii.Text[k.Offset:int(k.Offset)+int(k.Count)], "|")
			} else {
				k.Resolved = "无法解析: GeoAsciiParams 越界"
			}
		default:
			k.Resolved = fmt.Sprintf("未知位置 %d,保留原值", k.Location)
		}
		geo.Keys = append(geo.Keys, k)
	}
	r.Geo = geo
	p.buildCandidates(gk)
}

// buildCandidates 由 ModelTiepoint+Scale 与 ModelTransformation 各生成一个候选。
func (p *parser) buildCandidates(gk *Tag) {
	r := p.res
	tp := r.findTag(33922)
	sc := r.findTag(33550)
	tr := r.findTag(34264)
	var candA, candB *Candidate
	if tp != nil && sc != nil && len(tp.Doubles) >= 6 && len(sc.Doubles) >= 2 {
		d := tp.Doubles
		i0, j0, x0, y0 := d[0], d[1], d[3], d[4]
		sx, sy := sc.Doubles[0], sc.Doubles[1]
		m := [6]float64{sx, 0, x0 - i0*sx, 0, -sy, y0 + j0*sy}
		candA = &Candidate{
			Kind:   "tiepoint_scale",
			Units:  "像素(i,j) → 地理坐标(X,Y),基准见 GeoKey GeographicType/ProjectedCSType",
			Matrix: m,
			Source: []string{tp.Identity(firstIFDOffset(r)), sc.Identity(firstIFDOffset(r))},
			Status: "pending",
		}
	}
	if tr != nil && len(tr.Doubles) >= 16 {
		m := tr.Doubles
		candB = &Candidate{
			Kind:  "transformation",
			Units: "像素(i,j) → 地理坐标(X,Y),ModelTransformation 4x4 矩阵的仿射部分",
			Matrix: [6]float64{m[0], m[1], m[3], m[4], m[5], m[7]},
			Source: []string{tr.Identity(firstIFDOffset(r))},
			Status: "pending",
		}
	}
	if candA == nil && candB == nil {
		return
	}
	if candA != nil && candB != nil {
		// 在 (1,1) 处评估两候选,冲突时全部保留
		ax, ay := eval(candA.Matrix, 1, 1)
		bx, by := eval(candB.Matrix, 1, 1)
		if math.Abs(ax-bx) > 1e-9 || math.Abs(ay-by) > 1e-9 {
			candA.Status = "conflict"
			candB.Status = "conflict"
			p.ev("geokey.conflict",
				"ModelTiepoint/Scale 与 ModelTransformation 冲突,保留全部候选",
				map[string]any{
					"tiepointScaleAt1_1": []float64{ax, ay},
					"transformationAt1_1": []float64{bx, by},
					"tiepointScaleId": candidateID(candA),
					"transformationId": candidateID(candB),
				})
		} else {
			candA.Status = "consistent"
			candB.Status = "consistent"
			p.ev("geokey.consistent", "两个地理变换来源一致", nil)
		}
	}
	for _, c := range []*Candidate{candA, candB} {
		if c != nil {
			c.ID = candidateID(c)
			r.Candidates = append(r.Candidates, *c)
		}
	}
}

func eval(m [6]float64, i, j float64) (float64, float64) {
	return m[0]*i + m[1]*j + m[2], m[3]*i + m[4]*j + m[5]
}

func candidateID(c *Candidate) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%v", c.Kind, c.Matrix)))
	return "cand:" + c.Kind + ":" + hex.EncodeToString(h[:6])
}

func firstIFDOffset(r *Result) uint64 {
	for _, ifd := range r.IFDs {
		if ifd.Visited {
			return ifd.Offset
		}
	}
	return 0
}

// bitsToBytes 辅助:构造 GeoKey 目录 SHORT 数组。
func GeoKeyDirectoryBytes(order binary.ByteOrder, keys [][4]uint16, version, rev, minor uint16) []byte {
	buf := make([]byte, 2*(4+4*len(keys)))
	order.PutUint16(buf[0:], version)
	order.PutUint16(buf[2:], rev)
	order.PutUint16(buf[4:], minor)
	order.PutUint16(buf[6:], uint16(len(keys)))
	for i, k := range keys {
		for j := 0; j < 4; j++ {
			order.PutUint16(buf[8+8*i+2*j:], k[j])
		}
	}
	return buf
}
