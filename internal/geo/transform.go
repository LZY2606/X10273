package geo

import (
	"fmt"
	"math"

	"ifdroom/internal/tiff"
)

// Matrix3 is a raster-to-model affine transform:
//
//	[ A B C ]   [ I ]   [ X ]
//	[ D E F ] * [ J ] = [ Y ]
//	[ 0 0 1 ]   [ 1 ]   [ 1 ]
type Matrix3 struct {
	A, B, C float64
	D, E, F float64
	Unit    Unit `json:"unit"`
}

func (m Matrix3) Apply(i, j float64) (float64, float64) {
	return m.A*i + m.B*j + m.C, m.D*i + m.E*j + m.F
}

type Candidate struct {
	ID        string  `json:"id"`
	IFDOffset int64   `json:"ifdOffset"`
	Kind      string  `json:"kind"` // "tiepoint_scale" | "transformation"
	Matrix    Matrix3 `json:"matrix"`
	CRS       CRSRef  `json:"crs"`
	Status    string  `json:"status"` // "candidate" | "conflicting" | "invalid"
	Reject    string  `json:"rejectReason,omitempty"`
	// Raw source values are retained verbatim (tag 33922/33550/34264).
	RawSources  []RawSource `json:"rawSources"`
	Diagnostics []string    `json:"diagnostics"`

	tieI, tieJ, tieX, tieY float64
	sx, sy                 float64
}

type RawSource struct {
	Tag     uint16    `json:"tag"`
	Name    string    `json:"name"`
	Doubles []float64 `json:"doubles,omitempty"`
}

type TransformResult struct {
	Candidates  []Candidate `json:"candidates"`
	Conflict    bool        `json:"conflict"`
	Message     string      `json:"message"`
	CRS         CRSRef      `json:"crs"`
	Diagnostics []string    `json:"diagnostics"`
}

// DeriveTransforms extracts every legal geo-transform representation in the
// IFD. When both ModelTiepoint/ModelPixelScale and ModelTransformation are
// present they are returned as conflicting candidates instead of picking one.
func DeriveTransforms(ifd *tiff.IFD, kd *KeyDirectory) TransformResult {
	res := TransformResult{}
	geog, proj, geogUnit, projUnit := keyCodes(kd)
	crs := ResolveCRS(geog, proj, geogUnit, projUnit)
	res.CRS = crs
	unit := crs.Linear
	if crs.Type == "geographic" {
		unit = crs.Angular
	}
	hasTie := ifd.ByTag[tiff.TagModelTiepoint] != nil
	hasScale := ifd.ByTag[tiff.TagModelPixelScale] != nil
	hasTrans := ifd.ByTag[tiff.TagModelTransform] != nil

	if hasTrans {
		c := Candidate{ID: fmt.Sprintf("ifd%d-transformation", ifd.Offset),
			IFDOffset: ifd.Offset, Kind: "transformation", CRS: crs}
		e := ifd.ByTag[tiff.TagModelTransform]
		c.RawSources = []RawSource{{Tag: tiff.TagModelTransform, Name: "ModelTransformationTag", Doubles: append([]float64(nil), e.Doubles...)}}
		if len(e.Doubles) != 16 {
			c.Status = "invalid"
			c.Reject = fmt.Sprintf("ModelTransformationTag needs 16 doubles, has %d", len(e.Doubles))
		} else {
			// column-major GeoTIFF matrix; take the raster (x/y) rows.
			m := e.Doubles
			c.Matrix = Matrix3{A: m[0], B: m[4], C: m[3], D: m[1], E: m[5], F: m[7], Unit: unit}
			c.Status = "candidate"
			if !finiteMatrix(c.Matrix) {
				c.Status = "invalid"
				c.Reject = "transformation contains non-finite coefficients"
			}
		}
		res.Candidates = append(res.Candidates, c)
	}
	if hasTie || hasScale {
		c := Candidate{ID: fmt.Sprintf("ifd%d-tiepoint-scale", ifd.Offset),
			IFDOffset: ifd.Offset, Kind: "tiepoint_scale", CRS: crs}
		if hasTie {
			e := ifd.ByTag[tiff.TagModelTiepoint]
			c.RawSources = append(c.RawSources, RawSource{Tag: tiff.TagModelTiepoint, Name: "ModelTiepointTag", Doubles: append([]float64(nil), e.Doubles...)})
			if len(e.Doubles)%6 != 0 {
				c.Diagnostics = append(c.Diagnostics, fmt.Sprintf("ModelTiepointTag length %d not multiple of 6", len(e.Doubles)))
			}
			if len(e.Doubles) >= 6 {
				if len(e.Doubles) > 6 {
					c.Diagnostics = append(c.Diagnostics, fmt.Sprintf("%d tiepoints present; first raster origin tiepoint used", len(e.Doubles)/6))
				}
				c.tieI, c.tieJ, c.tieX, c.tieY = e.Doubles[0], e.Doubles[1], e.Doubles[3], e.Doubles[4]
			}
		}
		if hasScale {
			e := ifd.ByTag[tiff.TagModelPixelScale]
			c.RawSources = append(c.RawSources, RawSource{Tag: tiff.TagModelPixelScale, Name: "ModelPixelScaleTag", Doubles: append([]float64(nil), e.Doubles...)})
			if len(e.Doubles) < 2 {
				c.Diagnostics = append(c.Diagnostics, "ModelPixelScaleTag needs ScaleX, ScaleY")
			} else {
				c.sx, c.sy = e.Doubles[0], e.Doubles[1]
			}
		}
		switch {
		case !hasTie:
			c.Status = "invalid"
			c.Reject = "ModelPixelScaleTag without ModelTiepointTag is incomplete"
		case !hasScale:
			c.Status = "invalid"
			c.Reject = "ModelTiepointTag without ModelPixelScaleTag is incomplete"
		default:
			c.Matrix = Matrix3{A: c.sx, B: 0, C: c.tieX - c.sx*c.tieI,
				D: 0, E: -c.sy, F: c.tieY + c.sy*c.tieJ, Unit: unit}
			c.Status = "candidate"
			if !finiteMatrix(c.Matrix) {
				c.Status = "invalid"
				c.Reject = "tiepoint/scale produced non-finite coefficients"
			}
		}
		res.Candidates = append(res.Candidates, c)
	}
	if hasTie && hasTrans || hasTrans && hasScale {
		res.Conflict = true
		for i := range res.Candidates {
			if res.Candidates[i].Status == "candidate" {
				res.Candidates[i].Status = "conflicting"
			}
		}
		res.Message = "ModelTiepoint/ModelPixelScale and ModelTransformation are both defined: both legal candidates retained pending human decision"
	}
	if len(res.Candidates) == 0 {
		res.Message = "no geo-transform tags present"
	}
	return res
}

func keyCodes(kd *KeyDirectory) (geog, proj, geogUnit, projLinear uint16) {
	if kd == nil {
		return 0, 0, 0, 0
	}
	get := func(id uint16) uint16 {
		for _, k := range kd.Keys {
			if k.ID == id && k.ValueKind == "short" {
				var v uint16
				fmt.Sscanf(k.Value, "%d", &v)
				return v
			}
		}
		return 0
	}
	return get(KeyGeographicType), get(KeyProjectedType), get(KeyGeogAngularUnits), get(KeyProjLinearUnits)
}

func finiteMatrix(m Matrix3) bool {
	for _, v := range [6]float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}
