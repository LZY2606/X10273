package geo

import (
	"fmt"

	"tiffifd/internal/tiff"
)

// SourceRef identifies a preserved piece of source evidence within one import.
type SourceRef struct {
	TagID tiff.TagID
	// Entry is the IFD entry byte range; Value is the value byte range.
	Entry  tiff.ByteRange
	Value  tiff.ByteRange
	External bool
}

// String yields a stable id usable across export/import.
func (s SourceRef) String() string {
	loc := "inline"
	if s.External {
		loc = "ext"
	}
	return fmt.Sprintf("tag%d@entry%d:%d/%s@%d:%d",
		s.TagID, s.Entry.Offset, s.Entry.Length, loc, s.Value.Offset, s.Value.Length)
}

// Analyze derives the geo view for one IFD. file supplies byte order and
// parameter tags. All references are to preserved raw values; nothing here is
// assumed unique.
func Analyze(file *tiff.File, ifd *tiff.IFD) *Analysis {
	a := &Analysis{IFDKey: ifd.Key}

	scaleTag := ifd.TagByID(tiff.TagModelPixelScale)
	tieTag := ifd.TagByID(tiff.TagModelTiepoint)
	matTag := ifd.TagByID(tiff.TagModelTransformation)
	keyTag := ifd.TagByID(tiff.TagGeoKeyDirectory)
	dblTag := ifd.TagByID(tiff.TagGeoDoubleParams)
	ascTag := ifd.TagByID(tiff.TagGeoAsciiParams)

	// GeoKey directory ------------------------------------------------------
	if keyTag != nil {
		a.parseKeys(file, keyTag, dblTag, ascTag)
	}

	linearUnit := resolveLinearUnit(a)

	// Candidate construction ------------------------------------------------
	var hasTS, hasMatrix bool
	if tieTag != nil || scaleTag != nil {
		c := TransformCandidate{ID: "cand:tiepoint-scale:" + ifd.Key, Kind: KindTiepointScale}
		if tieTag != nil {
			c.Sources = append(c.Sources, refOf(file, ifd, tieTag).String())
			vals := file.DecodeDoubles(tieTag)
			if len(vals)%6 != 0 {
				c.RejectReason = "tiepoint_count_not_multiple_of_6"
				c.Diagnostic = fmt.Sprintf("ModelTiepoint has %d doubles, expected a multiple of 6", len(vals))
			}
			for i := 0; i+5 < len(vals); i += 6 {
				c.Tiepoints = append(c.Tiepoints, Tiepoint{
					I: vals[i], J: vals[i+1], K: vals[i+2],
					X: vals[i+3], Y: vals[i+4], Z: vals[i+5],
				})
			}
		}
		if scaleTag != nil {
			c.Sources = append(c.Sources, refOf(file, ifd, scaleTag).String())
			vals := file.DecodeDoubles(scaleTag)
			if len(vals) < 3 {
				c.RejectReason = joinReject(c.RejectReason, "pixel_scale_count_below_3")
				if c.Diagnostic == "" {
					c.Diagnostic = fmt.Sprintf("ModelPixelScale has %d doubles, expected 3", len(vals))
				}
			} else {
				c.Scale = &Scale{Sx: vals[0], Sy: vals[1], Sz: vals[2],
					Unit: linearUnit.name, UnitDatum: linearUnit.datum}
			}
		}
		a.Candidates = append(a.Candidates, c)
		hasTS = true
	}

	if matTag != nil {
		c := TransformCandidate{ID: "cand:matrix:" + ifd.Key, Kind: KindTransformation}
		c.Sources = append(c.Sources, refOf(file, ifd, matTag).String())
		vals := file.DecodeDoubles(matTag)
		if len(vals) != 16 {
			c.RejectReason = "matrix_count_not_16"
			c.Diagnostic = fmt.Sprintf("ModelTransformation has %d doubles, expected 16", len(vals))
		} else {
			var m Matrix
			copy(m[:], vals)
			c.Matrix = &m
		}
		a.Candidates = append(a.Candidates, c)
		hasMatrix = true
	}

	// The core conflict: ModelTiepoint/Scale and ModelTransformation must not
	// both be present. Both legal candidates are retained and flagged.
	if hasTS && hasMatrix {
		for i := range a.Candidates {
			a.Candidates[i].Conflict = "tiepoint_scale_vs_transformation"
		}
		a.Diagnostics = append(a.Diagnostics,
			"both ModelTiepoint/ModelPixelScale and ModelTransformation present; per GeoTIFF spec they are mutually exclusive; all candidates retained")
	}

	// Multiple tiepoints cannot be reduced to a unique raster origin mapping
	// under a single scale-only model; surface as non-unique candidates.
	if hasTS {
		if n := countTiepoints(a.Candidates); n > 1 {
			for i := range a.Candidates {
				if a.Candidates[i].Kind == KindTiepointScale {
					a.Candidates[i].Conflict = joinConflict(a.Candidates[i].Conflict, "multiple_tiepoints")
				}
			}
			a.Diagnostics = append(a.Diagnostics,
				fmt.Sprintf("%d tiepoints present; no unique single-origin transform, all legal candidates retained", n))
		}
	}

	return a
}

func refOf(_ *tiff.File, _ *tiff.IFD, t *tiff.Tag) SourceRef {
	return SourceRef{
		TagID: t.ID, Entry: t.Range, Value: t.Value.ValueRange,
		External: t.Value.ExternalRange != nil,
	}
}

func joinReject(a, b string) string {
	if a == "" {
		return b
	}
	return a + ";" + b
}

func joinConflict(a, b string) string { return joinReject(a, b) }

func countTiepoints(cs []TransformCandidate) int {
	for _, c := range cs {
		if c.Kind == KindTiepointScale {
			return len(c.Tiepoints)
		}
	}
	return 0
}

type unitInfo struct{ name, datum string }

func resolveLinearUnit(a *Analysis) unitInfo {
	for _, k := range a.Keys {
		if k.ID == keyProjLinearUnits && k.Value != "" {
			return unitInfo{name: k.Value, datum: "ProjLinearUnits GeoKey"}
		}
		if k.ID == keyVerticalUnits && k.Value != "" {
			return unitInfo{name: k.Value, datum: "VerticalUnits GeoKey"}
		}
	}
	return unitInfo{name: "unknown_linear_unit", datum: "ModelPixelScale tag (unit unspecified)"}
}
