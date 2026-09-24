// Package derive builds normalized, recomputable facts from immutable parse
// graphs. Every derived value carries an algorithm version, a parameter
// fingerprint and its direct sources.
package derive

import (
	"fmt"

	"ifdgraph/internal/model"
	"ifdgraph/internal/tiff"
)

const transformAlgo = "geo-transform-1.0.0"

// TransformParams is the fingerprinted parameter set for transform
// derivation.
type TransformParams struct {
	// RequireDeclaredCRS marks candidates unknown until a CRS GeoKey resolves.
	RequireDeclaredCRS bool `json:"require_declared_crs"`
}

// Fingerprint returns the canonical parameter string.
func (t TransformParams) Fingerprint() string {
	return fmt.Sprintf("reqcrs=%v", t.RequireDeclaredCRS)
}

// DefaultTransformParams is the initially adopted rule revision.
func DefaultTransformParams() TransformParams { return TransformParams{RequireDeclaredCRS: false} }

// CandidateStatus mirrors the requirement that uncertainty is first-class.
type CandidateStatus string

const (
	StatusUnique      CandidateStatus = "unique"
	StatusConflicting CandidateStatus = "conflicting"
	StatusUnknown     CandidateStatus = "unknown"
)

// TransformCandidate is one legal raster->model mapping.
type TransformCandidate struct {
	ID         string          `json:"id"`
	IFDID      string          `json:"ifdId"`
	Kind       string          `json:"kind"` // "tiepoint_scale" | "matrix"
	Status     CandidateStatus `json:"status"`
	Matrix     []float64       `json:"matrix"` // 4x4 row-major
	Units      string          `json:"units"`  // declared/unknown
	CRS        string          `json:"crs"`    // code or "unknown"
	Sources    []string        `json:"sources"`
	Rejected   []string        `json:"rejectedReasons,omitempty"`
	Diagnostic string          `json:"diagnostic,omitempty"`
}

// TransformResult is the per-IFD output of the derivation.
type TransformResult struct {
	IFDID      string               `json:"ifdId"`
	Status     CandidateStatus      `json:"status"`
	Candidates []TransformCandidate `json:"candidates"`
	Sources    []string             `json:"sources"`
}

// geoForIFD locates the raw geo material.
func geoForIFD(g *model.ParseGraph, ifdID string) *model.GeoData {
	for i := range g.Geo {
		if g.Geo[i].IFDID == ifdID {
			return &g.Geo[i]
		}
	}
	return nil
}

func tagIDFor(g *model.ParseGraph, ifdID string, code uint16) string {
	for _, ifd := range g.IFDs {
		if ifd.ID != ifdID {
			continue
		}
		for _, tid := range ifd.TagIDs {
			for _, t := range g.Tags {
				if t.ID == tid && t.Tag == code {
					return t.ID
				}
			}
		}
	}
	return ""
}

func crsLabel(gd *model.GeoData) (string, bool) {
	for _, k := range gd.Keys {
		if k.Key == 2048 || k.Key == 2049 { // GeographicType / ProjectedType
			if v, ok := k.Resolved.(uint16); ok && v != 0 {
				return fmt.Sprintf("EPSG:%d", v), true
			}
		}
	}
	return "unknown", false
}

// DeriveTransforms keeps every legal candidate; it never collapses a
// Tiepoint/Scale vs. Transformation conflict to one number.
func DeriveTransforms(g *model.ParseGraph, params TransformParams) []TransformResult {
	var out []TransformResult
	for _, ifd := range g.IFDs {
		if !ifd.Reachable {
			continue
		}
		gd := geoForIFD(g, ifd.ID)
		if gd == nil {
			continue
		}
		res := TransformResult{IFDID: ifd.ID}
		crs, crsKnown := crsLabel(gd)
		// Candidate A: ModelTiepoint + ModelPixelScale.
		if len(gd.Tiepoints) > 0 && len(gd.PixelScale) >= 3 {
			tp := gd.Tiepoints[0]
			sx, sy := gd.PixelScale[0], gd.PixelScale[1]
			matrix := []float64{
				sx, 0, 0, tp[3] - tp[0]*sx,
				0, -sy, 0, tp[4] + tp[1]*sy,
				0, 0, gd.PixelScale[2], tp[5] - tp[2]*gd.PixelScale[2],
				0, 0, 0, 1,
			}
			c := TransformCandidate{
				IFDID:   ifd.ID,
				Kind:    "tiepoint_scale",
				Matrix:  matrix,
				Units:   "model-units per pixel (ModelPixelScale; units declared by CRS or unknown)",
				CRS:     crs,
				Sources: nonEmpty(tagIDFor(g, ifd.ID, tiff.TagModelTiepoint), tagIDFor(g, ifd.ID, tiff.TagModelPixelScale)),
			}
			if len(gd.Tiepoints) > 1 {
				c.Rejected = append(c.Rejected, "multiple ModelTiepoint tuples: spec-consistent only when identical; retained but not collapsed")
			}
			if !crsKnown {
				c.Rejected = append(c.Rejected, "no resolvable GeographicType/ProjectedType GeoKey: coordinate reference unknown")
				if params.RequireDeclaredCRS {
					c.Diagnostic = "held unknown: rule revision requires a declared CRS"
				}
			}
			res.Candidates = append(res.Candidates, c)
		}
		// Candidate B: ModelTransformation matrix.
		if len(gd.Transformation) == 16 {
			c := TransformCandidate{
				IFDID:   ifd.ID,
				Kind:    "matrix",
				Matrix:  append([]float64(nil), gd.Transformation...),
				Units:   "model units (matrix; units declared by CRS or unknown)",
				CRS:     crs,
				Sources: nonEmpty(tagIDFor(g, ifd.ID, tiff.TagModelTransform)),
			}
			if !crsKnown {
				c.Rejected = append(c.Rejected, "no resolvable GeographicType/ProjectedType GeoKey: coordinate reference unknown")
				if params.RequireDeclaredCRS {
					c.Diagnostic = "held unknown: rule revision requires a declared CRS"
				}
			}
			res.Candidates = append(res.Candidates, c)
		}
		switch len(res.Candidates) {
		case 0:
			res.Status = StatusUnknown
		case 1:
			if res.Candidates[0].Diagnostic != "" {
				res.Candidates[0].Status = StatusUnknown
				res.Status = StatusUnknown
			} else {
				res.Candidates[0].Status = StatusUnique
				res.Status = StatusUnique
			}
		default:
			res.Status = StatusConflicting
			for i := range res.Candidates {
				res.Candidates[i].Status = StatusConflicting
				res.Candidates[i].Rejected = append(res.Candidates[i].Rejected,
					"ModelTiepoint/Scale and ModelTransformation coexist: GeoTIFF forbids both; conflict requires a human decision")
			}
		}
		for i := range res.Candidates {
			src := append([]string(nil), res.Candidates[i].Sources...)
			res.Candidates[i].ID = model.DerivedID("xform", transformAlgo, params.Fingerprint(), src)
			res.Sources = append(res.Sources, src...)
		}
		out = append(out, res)
	}
	return out
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// TransformAlgoVersion exposes the version stamped on derived candidates.
func TransformAlgoVersion() string { return transformAlgo }
