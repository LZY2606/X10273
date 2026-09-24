package tiff

import "strconv"

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// parseTransforms extracts georeferencing candidates. When both
// ModelTiepoint+ModelPixelScale and ModelTransformationTag are present they
// are BOTH retained with a conflict diagnostic; reviewers decide.
func parseTransforms(r *reader, d *IFD, doc *Document, gk *GeoKeyDirectory) []ModelTransform {
	var out []ModelTransform
	scaleTag := d.TagByCode(TagModelPixelScale)
	tieTag := d.TagByCode(TagModelTiepoint)
	matTag := d.TagByCode(TagModelTransformation)
	unit, crs := unitAndCRS(gk)

	if scaleTag != nil && tieTag != nil {
		s := scaleTag.DecodedFloat
		t := tieTag.DecodedFloat
		mt := ModelTransform{Kind: "tiepoint-scale", Unit: unit, CRSCode: crs,
			Sources: sourcesOf(scaleTag, tieTag)}
		if len(s) < 3 {
			mt.Diagnostics = append(mt.Diagnostics,
				"ModelPixelScaleTag has fewer than 3 doubles; original values retained")
		}
		// Each 6-tuple is one tiepoint (I,J,K -> X,Y,Z). We keep the raw
		// tiepoints and build a matrix from the first complete one.
		mt.Scale = append([]float64(nil), s...)
		mt.Tiepoint = append([]float64(nil), t...)
		if len(t) >= 6 && len(s) >= 3 {
			sx, sy := s[0], s[1]
			rx, ry := t[3], t[4]
			mt.Matrix = [16]float64{
				sx, 0, 0, rx,
				0, -sy, 0, ry,
				0, 0, s[2], t[5],
				0, 0, 0, 1,
			}
		} else {
			mt.Diagnostics = append(mt.Diagnostics,
				"tiepoint/scale incomplete: affine matrix not uniquely derivable; candidate values retained")
		}
		if matTag != nil {
			mt.Diagnostics = append(mt.Diagnostics,
				"conflicts with ModelTransformationTag: both representations present, neither dropped")
		}
		out = append(out, mt)
	} else if scaleTag != nil || tieTag != nil {
		mt := ModelTransform{Kind: "tiepoint-scale", Unit: unit, CRSCode: crs}
		if scaleTag != nil {
			mt.Sources = append(mt.Sources, sourcesOf(scaleTag)...)
			mt.Scale = append([]float64(nil), scaleTag.DecodedFloat...)
		}
		if tieTag != nil {
			mt.Sources = append(mt.Sources, sourcesOf(tieTag)...)
			mt.Tiepoint = append([]float64(nil), tieTag.DecodedFloat...)
		}
		mt.Diagnostics = append(mt.Diagnostics,
			"only one of ModelPixelScale/ModelTiepoint present; transform underdetermined, no matrix fabricated")
		out = append(out, mt)
	}

	if matTag != nil {
		mt := ModelTransform{
			Kind: "transformation", Unit: unit, CRSCode: crs,
			Sources: sourcesOf(matTag),
		}
		f := matTag.DecodedFloat
		if len(f) != 16 {
			mt.Diagnostics = append(mt.Diagnostics,
				"ModelTransformationTag must hold 16 doubles; raw values retained in place")
		} else {
			copy(mt.Matrix[:], f)
		}
		if scaleTag != nil && tieTag != nil {
			mt.Diagnostics = append(mt.Diagnostics,
				"conflicts with ModelTiepoint+ModelPixelScale: both representations present, neither dropped")
		}
		out = append(out, mt)
	}
	return out
}

func sourcesOf(tags ...*Tag) []Range {
	var rs []Range
	for _, t := range tags {
		if t.External != nil {
			rs = append(rs, *t.External)
		} else {
			rs = append(rs, t.EntryRange)
		}
	}
	return rs
}

// unitAndCRS reads unit/CRS facts from the GeoKey directory WITHOUT network
// lookups. Unknown codes remain as raw key values elsewhere; here they are
// labeled unknown so no unit is fabricated for plotting.
func unitAndCRS(gk *GeoKeyDirectory) (string, int) {
	if gk == nil {
		return "unknown: no GeoKey directory", 0
	}
	unit := "unknown unit"
	crs := 0
	for _, k := range gk.Keys {
		if k.Location != 0 {
			continue
		}
		switch k.ID {
		case KeyProjectedCSType:
			crs = int(k.Value)
		case KeyGeographicType:
			if crs == 0 {
				crs = int(k.Value)
			}
		case KeyProjLinearUnits:
			unit = linearUnitName(uint16(k.Value))
		case KeyGeogAngularUnits:
			if unit == "unknown unit" {
				unit = angularUnitName(uint16(k.Value))
			}
		}
	}
	if crs == 0 {
		return unit + " (CRS unresolved; GeoKey raw values retained)", 0
	}
	return unit, crs
}

func linearUnitName(code uint16) string {
	switch code {
	case 9001:
		return "metre"
	case 9002:
		return "foot"
	case 9003:
		return "US survey foot"
	default:
		return "linear unit GeoKey code " + strconv.Itoa(int(code))
	}
}

func angularUnitName(code uint16) string {
	switch code {
	case 9102:
		return "radian"
	case 9108:
		return "degree"
	default:
		return "angular unit GeoKey code " + strconv.Itoa(int(code))
	}
}
