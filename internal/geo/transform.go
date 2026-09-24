// Package geo derives georeferencing transform candidates from preserved
// TIFF tags. Conflicting sources (ModelTiepoint+ModelPixelScale versus
// ModelTransformation) are never merged: every legal candidate is kept
// with its provenance until a human decision resolves the conflict.
package geo

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"

	"ifdmap/internal/tiff"
)

// Algorithm identifiers and the current derivation version. Bumping
// AlgoVersion invalidates previously derived candidates on replay.
const (
	AlgoTiepointScale  = "tiepoint-scale"
	AlgoTransformation = "transformation-matrix"
	AlgoVersion        = "geo.v1"
)

// Candidate status values.
const (
	StatusCandidate = "candidate"
	StatusAccepted  = "accepted"
	StatusRejected  = "rejected"
)

// DiagConflict is emitted when both transform sources exist and disagree.
const DiagConflict = "GEO_TRANSFORM_CONFLICT"

// Candidate is one possible pixel→CRS affine transform.
// Matrix maps pixel (col,row) to CRS coordinates:
//
//	x = M[0]*col + M[1]*row + M[2]
//	y = M[3]*col + M[4]*row + M[5]
type Candidate struct {
	ID                string     `json:"id"`
	IFDID             string     `json:"ifdId"`
	Algo              string     `json:"algo"`
	AlgoVersion       string     `json:"algoVersion"`
	ParamsFingerprint string     `json:"paramsFingerprint"`
	SourceTagIDs      []string   `json:"sourceTagIds"`
	Matrix            [6]float64 `json:"matrix"`
	CRS               string     `json:"crs"` // e.g. "EPSG:4326", from GeoKeys; "" if unknown
	Status            string     `json:"status"`
	RejectReason      string     `json:"rejectReason,omitempty"`
}

// Diagnostic is a machine-readable geo finding.
type Diagnostic struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	SubjectID string `json:"subjectId"`
}

// Result bundles candidates with diagnostics.
type Result struct {
	Candidates  []*Candidate `json:"candidates"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func fingerprint(algo string, tags ...*tiff.Tag) string {
	h := sha256.New()
	h.Write([]byte(AlgoVersion))
	h.Write([]byte(algo))
	for _, t := range tags {
		h.Write([]byte(t.ID))
		h.Write(t.RawValue)
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func crsFromGeoKeys(dir *tiff.GeoKeyDirectory) string {
	if dir == nil {
		return ""
	}
	for _, k := range dir.Keys {
		if k.KeyID == 3072 && len(k.Values) > 0 {
			return fmt.Sprintf("EPSG:%d", k.Values[0])
		}
	}
	for _, k := range dir.Keys {
		if k.KeyID == 2048 && len(k.Values) > 0 {
			return fmt.Sprintf("EPSG:%d", k.Values[0])
		}
	}
	return ""
}

// Candidates derives all transform candidates for one IFD. Both sources
// are evaluated independently; disagreement keeps both as candidates and
// records a conflict diagnostic.
func Candidates(f *tiff.File, ifd *tiff.IFD) *Result {
	res := &Result{}
	crs := crsFromGeoKeys(tiff.ParseGeoKeys(f, ifd))

	scale := ifd.TagByCode(tiff.TagModelPixelScale)
	tiepoint := ifd.TagByCode(tiff.TagModelTiepoint)
	trans := ifd.TagByCode(tiff.TagModelTransformation)

	var fromTiepoint, fromMatrix *Candidate

	if scale != nil && tiepoint != nil {
		if len(scale.Floats) >= 2 && len(tiepoint.Floats) >= 6 {
			sx, sy := scale.Floats[0], scale.Floats[1]
			tp := tiepoint.Floats
			c := &Candidate{
				IFDID:             ifd.ID,
				Algo:              AlgoTiepointScale,
				AlgoVersion:       AlgoVersion,
				ParamsFingerprint: fingerprint(AlgoTiepointScale, scale, tiepoint),
				SourceTagIDs:      []string{scale.ID, tiepoint.ID},
				Matrix: [6]float64{
					sx, 0, tp[3] - tp[0]*sx,
					0, -sy, tp[4] + tp[1]*sy,
				},
				CRS:    crs,
				Status: StatusCandidate,
			}
			c.ID = fmt.Sprintf("cand@%s:tiepoint-scale", ifd.ID)
			fromTiepoint = c
		} else {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Code: "GEO_SOURCE_INCOMPLETE", SubjectID: ifd.ID,
				Message: "ModelPixelScale/ModelTiepoint present but too short to derive a transform",
			})
		}
	}

	if trans != nil {
		if len(trans.Floats) >= 16 {
			m := trans.Floats
			c := &Candidate{
				IFDID:             ifd.ID,
				Algo:              AlgoTransformation,
				AlgoVersion:       AlgoVersion,
				ParamsFingerprint: fingerprint(AlgoTransformation, trans),
				SourceTagIDs:      []string{trans.ID},
				Matrix:            [6]float64{m[0], m[1], m[3], m[4], m[5], m[7]},
				CRS:               crs,
				Status:            StatusCandidate,
			}
			c.ID = fmt.Sprintf("cand@%s:transformation-matrix", ifd.ID)
			fromMatrix = c
		} else {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Code: "GEO_SOURCE_INCOMPLETE", SubjectID: ifd.ID,
				Message: "ModelTransformation present but has fewer than 16 doubles",
			})
		}
	}

	if fromTiepoint != nil && fromMatrix != nil && !matricesClose(fromTiepoint.Matrix, fromMatrix.Matrix, 1e-9) {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code: DiagConflict, SubjectID: ifd.ID,
			Message: fmt.Sprintf("ModelTiepoint/Scale transform %v conflicts with ModelTransformation %v; both candidates retained",
				fromTiepoint.Matrix, fromMatrix.Matrix),
		})
	}
	if fromTiepoint != nil {
		res.Candidates = append(res.Candidates, fromTiepoint)
	}
	if fromMatrix != nil {
		res.Candidates = append(res.Candidates, fromMatrix)
	}
	return res
}

func matricesClose(a, b [6]float64, eps float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > eps {
			return false
		}
	}
	return true
}

// FingerprintUnchanged reports whether a stored candidate still matches
// the current source tags (used to detect stale derived values).
func FingerprintUnchanged(c *Candidate, ifd *tiff.IFD) bool {
	var tags []*tiff.Tag
	for _, id := range c.SourceTagIDs {
		var found *tiff.Tag
		for _, t := range ifd.Tags {
			if t.ID == id {
				found = t
				break
			}
		}
		if found == nil {
			return false
		}
		tags = append(tags, found)
	}
	return fingerprint(c.Algo, tags...) == c.ParamsFingerprint
}

// MatrixBytes serializes a matrix deterministically for fingerprints.
func MatrixBytes(m [6]float64) []byte {
	buf := make([]byte, 48)
	for i, v := range m {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(v))
	}
	return buf
}
