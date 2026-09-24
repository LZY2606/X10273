package tiff

import (
	"fmt"
	"ifdlantern/internal/digest"
	"ifdlantern/internal/domain"
)

var geoKeyNames = map[int]string{
	1024: "GTModelTypeGeoKey", 1025: "GTRasterTypeGeoKey",
	1026: "GTCitationGeoKey", 2048: "GeographicTypeGeoKey",
	2049: "GeogCitationGeoKey", 3072: "ProjectedCSTypeGeoKey",
	3073: "PCSCitationGeoKey",
}

// Known EPSG table for the offline case pack. Unknown codes are retained but
// left unnormalised with a diagnostic rather than guessed.
var geographicCRS = map[int]bool{
	4326: true, 4269: true, 4267: true, 4258: true, 4490: true,
}
var projectedCRS = map[int]bool{
	3857: true, 32633: true, 32654: true, 32610: true, 32601: true,
}

func analyzeGeo(node *IFD, hdr Header, src Source) *GeoModel {
	gm := &GeoModel{}
	dir := tagByID(node, 34735)
	doubles := tagByID(node, 34736)
	ascii := tagByID(node, 34737)
	if dir == nil {
		return gm
	}
	shorts := shortsOf(dir)
	if len(shorts) < 4 {
		gm.KeyDiags = append(gm.KeyDiags, domain.Diagnostic{
			Code: "geokey-header-short", Severity: domain.SevError,
			Message: fmt.Sprintf("GeoKeyDirectory has %d SHORTs, need at least 4", len(shorts)),
			Evidence: []string{node.ID},
		})
		return gm
	}
	ver, major, minor, n := shorts[0], shorts[1], shorts[2], shorts[3]
	gm.KeyVersion = ver
	gm.KeyRev = fmt.Sprintf("%d.%d", major, minor)
	if ver != 1 {
		gm.KeyDiags = append(gm.KeyDiags, domain.Diagnostic{
			Code: "geokey-version-unknown", Severity: domain.SevError,
			Message: fmt.Sprintf("GeoKey directory version %d is not supported (expected 1)", ver),
			Evidence: []string{node.ID},
		})
	}
	seen := map[int]int{}
	for k := 0; k < n; k++ {
		base := 4 + k*4
		if base+4 > len(shorts) {
			gm.KeyDiags = append(gm.KeyDiags, domain.Diagnostic{
				Code: "geokey-truncated", Severity: domain.SevError,
				Message: fmt.Sprintf("GeoKey entry %d runs past directory", k),
				Evidence: []string{node.ID},
			})
			break
		}
		id, loc, cnt, vo := shorts[base], shorts[base+1], shorts[base+2], shorts[base+3]
		ke := GeoKeyEntry{ID: id, Name: geoKeyNames[id], Loc: loc, Count: cnt,
			Value: fmt.Sprintf("%d", vo)}
		switch {
		case loc == 0:
			ke.ValueN = vo
		case loc == 34736 && doubles != nil:
			ke.Value = doublesText(doubles, vo, cnt)
		case loc == 34737 && ascii != nil:
			ke.Value = asciiText(ascii, vo, cnt)
		default:
			gm.KeyDiags = append(gm.KeyDiags, domain.Diagnostic{
				Code: "geokey-value-missing", Severity: domain.SevError,
				Message: fmt.Sprintf("GeoKey %d points at tag %d which is absent", id, loc),
				Evidence: []string{node.ID},
			})
		}
		if prev, dup := seen[id]; dup {
			gm.KeyDiags = append(gm.KeyDiags, domain.Diagnostic{
				Code: "geokey-duplicate", Severity: domain.SevConflict,
				Message: fmt.Sprintf("GeoKey %d (%s) appears twice with values %q and %q; both retained",
					id, geoKeyNames[id], gm.Keys[prev].Value, ke.Value),
				Evidence: []string{node.ID, fmt.Sprintf("geokey:%d", id)},
			})
		}
		seen[id] = len(gm.Keys)
		gm.Keys = append(gm.Keys, ke)
	}

	crs, crsDiag := resolveCRS(gm.Keys, node.ID)
	gm.KeyDiags = append(gm.KeyDiags, crsDiag...)

	scale := tagByID(node, 33550)
	tie := tagByID(node, 33922)
	trans := tagByID(node, 34264)

	if tie != nil && scale != nil {
		if cand, ok := buildTieScale(node, tie, scale, crs); ok {
			gm.Candidates = append(gm.Candidates, cand)
		}
	} else if tie != nil || scale != nil {
		gm.KeyDiags = append(gm.KeyDiags, domain.Diagnostic{
			Code: "tiescale-incomplete", Severity: domain.SevError,
			Message: "ModelTiepoint and ModelPixelScale must both be present; values left in place, no transform derived",
			Evidence: []string{node.ID},
		})
	}
	if trans != nil {
		if cand, ok := buildTransform(node, trans, crs); ok {
			gm.Candidates = append(gm.Candidates, cand)
		}
	}
	if tie != nil && scale != nil && trans != nil {
		for i := range gm.Candidates {
			gm.Candidates[i].Status = domain.StatusAmbiguous
			gm.Candidates[i].Diags = append(gm.Candidates[i].Diags, domain.Diagnostic{
				Code: "dual-geotransform", Severity: domain.SevConflict,
				Message: "ModelTiepoint+ModelPixelScale and ModelTransformationTag both present; all legal candidates retained",
				Evidence: []string{node.ID, "tag:33922", "tag:33550", "tag:34264"},
			})
		}
	}
	return gm
}

func resolveCRS(keys []GeoKeyEntry, nodeID string) (domain.GeoCRS, []domain.Diagnostic) {
	var model, geog, proj *GeoKeyEntry
	var diags []domain.Diagnostic
	for i := range keys {
		switch keys[i].ID {
		case 1024:
			model = &keys[i]
		case 2048:
			geog = &keys[i]
		case 3072:
			proj = &keys[i]
		}
	}
	crs := domain.GeoCRS{Known: false}
	switch {
	case proj != nil:
		crs.EPSG = proj.ValueN
		switch {
		case projectedCRS[proj.ValueN]:
			crs.Known, crs.Unit, crs.Name = true, "metre", fmt.Sprintf("EPSG:%d", proj.ValueN)
		case geographicCRS[proj.ValueN]:
			crs.Known, crs.Unit, crs.Name = true, "degree", fmt.Sprintf("EPSG:%d", proj.ValueN)
		}
	case geog != nil:
		crs.EPSG = geog.ValueN
		if geographicCRS[geog.ValueN] {
			crs.Known, crs.Unit, crs.Name = true, "degree", fmt.Sprintf("EPSG:%d", geog.ValueN)
		}
	}
	if crs.EPSG != 0 && !crs.Known {
		diags = append(diags, domain.Diagnostic{
			Code: "crs-unknown", Severity: domain.SevWarning,
			Message: fmt.Sprintf("CRS EPSG:%d is not in the offline registry; transform kept with unnormalised unit", crs.EPSG),
			Evidence: []string{nodeID},
		})
	}
	if model != nil {
		switch model.ValueN {
		case 1: // ModelTypeProjected
			if proj == nil {
				diags = append(diags, domain.Diagnostic{
					Code: "geokey-modeltype-conflict", Severity: domain.SevConflict,
					Message: "GTModelType=Projected but ProjectedCSTypeGeoKey is absent",
					Evidence: []string{nodeID, "geokey:1024"},
				})
			}
		case 2: // ModelTypeGeographic
			if geog == nil {
				diags = append(diags, domain.Diagnostic{
					Code: "geokey-modeltype-conflict", Severity: domain.SevConflict,
					Message: "GTModelType=Geographic but GeographicTypeGeoKey is absent",
					Evidence: []string{nodeID, "geokey:1024"},
				})
			}
		}
	}
	return crs, diags
}

func buildTieScale(node *IFD, tie, scale *RawTag, crs domain.GeoCRS) (TransformCandidate, bool) {
	tv, sv := doublesOf(tie), doublesOf(scale)
	if len(tv) < 6 || len(tv)%6 != 0 {
		return TransformCandidate{}, false
	}
	if len(sv) < 3 {
		return TransformCandidate{}, false
	}
	i0, j0, k0, x0, y0, z0 := tv[0], tv[1], tv[2], tv[3], tv[4], tv[5]
	sx, sy, sz := sv[0], sv[1], sv[2]
	if sz == 0 {
		sz = 1
	}
	m := [16]float64{
		sx, 0, 0, x0 - sx*i0,
		0, -sy, 0, y0 + sy*j0,
		0, 0, sz, z0 - sz*k0,
		0, 0, 0, 1,
	}
	cand := TransformCandidate{
		Origin: "ModelTiepointTag+ModelPixelScaleTag",
		Matrix: m, CRS: crs,
		UnitAxes: [2]string{crs.Unit, crs.Unit},
		Status:   domain.StatusCandidate,
	}
	cand.ID = "gt-" + digest.Canonical([]any{
		node.ID, "tiepoint-scale", tie.ValueOff, scale.ValueOff, tv, sv,
	})[:16]
	return cand, true
}

func buildTransform(node *IFD, trans *RawTag, crs domain.GeoCRS) (TransformCandidate, bool) {
	dv := doublesOf(trans)
	if len(dv) != 16 {
		return TransformCandidate{}, false
	}
	var m [16]float64
	copy(m[:], dv)
	cand := TransformCandidate{
		Origin:   "ModelTransformationTag",
		Matrix:   m,
		CRS:      crs,
		UnitAxes: [2]string{crs.Unit, crs.Unit},
		Status:   domain.StatusCandidate,
	}
	cand.ID = "gt-" + digest.Canonical([]any{
		node.ID, "transformation", trans.ValueOff, dv,
	})[:16]
	return cand, true
}

func shortsOf(rt *RawTag) []int {
	out := make([]int, 0, len(rt.Values))
	for _, v := range rt.Values {
		out = append(out, int(v.N))
	}
	return out
}

func doublesOf(rt *RawTag) []float64 {
	out := make([]float64, 0, len(rt.Values))
	for _, v := range rt.Values {
		if v.N2 != 0 {
			out = append(out, v.N/v.N2)
		} else {
			out = append(out, v.N)
		}
	}
	return out
}

func doublesText(rt *RawTag, start, count int) string {
	var s string
	for i := 0; i < count; i++ {
		if start+i < len(rt.Values) {
			if i > 0 {
				s += "|"
			}
			s += rt.Values[start+i].Keep
		}
	}
	return s
}

func asciiText(rt *RawTag, start, count int) string {
	if rt.RawKeep == "" {
		return ""
	}
	b := rt.Raw
	if start < 0 || start > len(b) {
		return ""
	}
	end := start + count
	if end > len(b) {
		end = len(b)
	}
	return trimNul(string(b[start:end]))
}

func trimNul(s string) string {
	for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == '|') {
		s = s[:len(s)-1]
	}
	return s
}
