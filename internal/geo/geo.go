// Package geo interprets GeoTIFF geokeys and raster-to-model transforms.
//
// Original representations are retained (doubles/ascii), all units and
// coordinate references travel with the values, and conflicting transform
// descriptions coexist as candidates rather than being silently resolved.
package geo

import (
	"fmt"
	"ifdroom/internal/tiff"
)

type Severity = tiff.Severity

const (
	SevFatal   = tiff.SevFatal
	SevError   = tiff.SevError
	SevWarning = tiff.SevWarning
	SevInfo    = tiff.SevInfo
)

type Diagnostic struct {
	Code     string `json:"code"`
	Severity Severity `json:"severity"`
	Message  string `json:"message"`
	IFDID    string `json:"ifd_id,omitempty"`
}

// Measured is a domain number that keeps its unit / coordinate reference.
type Measured struct {
	Raw      float64 `json:"raw"`
	Unit     string  `json:"unit"`     // e.g. "metre", "foot_survey_us", "unknown:32767"
	CRS      string  `json:"crs"`      // e.g. "EPSG:32654" or "unknown"
	Convertible bool  `json:"convertible"`
	Diagnostic string `json:"diagnostic,omitempty"` // set when it must stay put
}

// GeoKey keeps both the raw directory entry and any DOUBLE/ASCII payload.
type GeoKey struct {
	ID          uint16      `json:"id"`
	Name        string      `json:"name"`
	Location    uint16      `json:"location"`
	Count       uint16      `json:"count"`
	ValueOrOffset uint16    `json:"value_or_offset"`
	Resolved    interface{} `json:"resolved"`
	PayloadUnit string      `json:"payload_unit,omitempty"`
}

type KeyDirectory struct {
	IFDID       string     `json:"ifd_id"`
	Version     uint16     `json:"key_version"`
	Revision    uint16     `json:"key_revision"`
	Minor       uint16     `json:"key_minor"`
	VersionRaw  [4]uint16  `json:"version_raw"`
	Keys        []GeoKey   `json:"keys"`
	Diagnostics []*Diagnostic `json:"diagnostics"`
}

// TransformKind enumerates supported raster-to-model descriptions.
type TransformKind string

const (
	KindTiepointScale TransformKind = "tiepoint_scale"
	KindMatrix        TransformKind = "transformation_matrix"
)

// Candidate is one legal transform description plus its provenance tag ids.
type Candidate struct {
	ID         string        `json:"id"`
	Kind       TransformKind `json:"kind"`
	IFDID      string        `json:"ifd_id"`
	Scale      []Measured    `json:"scale,omitempty"`      // Sx,Sy,Sz
	Tiepoint   []float64     `json:"tiepoint,omitempty"`   // 6 raw doubles, raster+model triplets
	Matrix     []float64     `json:"matrix,omitempty"`     // 16 raw doubles, column-major per spec
	ScaleUnits []string      `json:"scale_units,omitempty"`
	Status     string        `json:"status"` // "legal" | "rejected"
	RejectCode string        `json:"reject_code,omitempty"`
	RejectReason string      `json:"reject_reason,omitempty"`
	SourceTags []uint16      `json:"source_tags"`
}

type Result struct {
	IFDID       string        `json:"ifd_id"`
	Directory   *KeyDirectory `json:"directory"`
	Candidates  []*Candidate  `json:"candidates"`
	Conflict    bool          `json:"conflict"`
	ConflictNote string       `json:"conflict_note,omitempty"`
	Diagnostics []*Diagnostic `json:"diagnostics"`
}

func findTag(ifd *tiff.IFD, id uint16) *tiff.Tag {
	for _, t := range ifd.Tags {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func u16s(t *tiff.Tag) []uint16 {
	var out []uint16
	for _, v := range t.Values {
		if x, ok := v.(uint64); ok {
			out = append(out, uint16(x))
		}
	}
	return out
}

func f64s(t *tiff.Tag) []float64 {
	var out []float64
	for _, v := range t.Values {
		if x, ok := v.(float64); ok {
			out = append(out, x)
		}
	}
	return out
}

// well-known geokey ids with names (subset sufficient for the domain material).
var keyNames = map[uint16]string{
	1024: "GTModelTypeGeoKey",
	1025: "GTRasterTypeGeoKey",
	1026: "GTCitationGeoKey",
	2048: "GeographicTypeGeoKey",
	2049: "GeogCitationGeoKey",
	2054: "GeogAngularUnitsGeoKey",
	3072: "ProjectedCSTypeGeoKey",
	3073: "PCSCitationGeoKey",
	3076: "ProjLinearUnitsGeoKey",
}

func KeyName(id uint16) string {
	if n, ok := keyNames[id]; ok {
		return n
	}
	return fmt.Sprintf("GeoKey%d", id)
}

// linearUnits maps a ProjLinearUnitsGeoKey code to a unit name (subset).
var linearUnits = map[uint16]string{
	9001: "metre",
	9002: "foot",
	9003: "foot_survey_us",
	9006: "radian",
}
var angularUnits = map[uint16]string{
	9101: "radian",
	9102: "degree",
	9122: "degree",
}

// Interpret builds the key directory and all legal transform candidates.
func Interpret(ifd *tiff.IFD) *Result {
	res := &Result{IFDID: ifd.ID}
	if dir := parseDirectory(ifd); dir != nil {
		res.Directory = dir
		res.Diagnostics = append(res.Diagnostics, dir.Diagnostics...)
	}
	scaleTag := findTag(ifd, tiff.TagModelPixelScale)
	tieTag := findTag(ifd, tiff.TagModelTiepoint)
	matrixTag := findTag(ifd, tiff.TagModelTransform)

	linearUnit := res.linearUnit()
	hasTS := scaleTag != nil || tieTag != nil
	hasMatrix := matrixTag != nil

	if hasTS {
		c := &Candidate{
			ID:     fmt.Sprintf("%s:cand-tiepoint-scale", ifd.ID),
			Kind:   KindTiepointScale,
			IFDID:  ifd.ID,
			Status: "legal",
		}
		if scaleTag != nil {
			c.SourceTags = append(c.SourceTags, tiff.TagModelPixelScale)
			for _, v := range f64s(scaleTag) {
				c.Scale = append(c.Scale, Measured{Raw: v, Unit: linearUnit.name, Convertible: linearUnit.convertible,
					Diagnostic: linearUnit.diag})
			}
			if len(c.Scale) != 3 {
				c.Status = "rejected"
				c.RejectCode = "scale_count"
				c.RejectReason = fmt.Sprintf("ModelPixelScaleTag must carry 3 doubles, found %d", len(c.Scale))
			}
		}
		if tieTag != nil {
			c.SourceTags = append(c.SourceTags, tiff.TagModelTiepoint)
			c.Tiepoint = f64s(tieTag)
			if len(c.Tiepoint)%6 != 0 {
				c.Status = "rejected"
				c.RejectCode = "tiepoint_count"
				c.RejectReason = fmt.Sprintf("ModelTiepointTag doubles must be a multiple of 6, found %d", len(c.Tiepoint))
			}
		}
		res.Candidates = append(res.Candidates, c)
	}
	if hasMatrix {
		c := &Candidate{
			ID:     fmt.Sprintf("%s:cand-transformation", ifd.ID),
			Kind:   KindMatrix,
			IFDID:  ifd.ID,
			Status: "legal",
			SourceTags: []uint16{tiff.TagModelTransform},
		}
		c.Matrix = f64s(matrixTag)
		if len(c.Matrix) != 16 {
			c.Status = "rejected"
			c.RejectCode = "matrix_count"
			c.RejectReason = fmt.Sprintf("ModelTransformationTag must carry 16 doubles, found %d", len(c.Matrix))
		}
		res.Candidates = append(res.Candidates, c)
	}
	if hasTS && hasMatrix {
		res.Conflict = true
		res.ConflictNote = "ModelTiepoint/ModelPixelScale and ModelTransformationTag are both present: GeoTIFF forbids defining both; all legal candidates are retained pending a human decision"
		for _, c := range res.Candidates {
			if c.Status == "legal" {
				c.RejectCode = "conflicting_model"
				c.RejectReason = "co-present with the alternative transform description; candidate retained but cannot be uniquely chosen"
			}
		}
	}
	if !hasTS && !hasMatrix {
		res.Diagnostics = append(res.Diagnostics, &Diagnostic{
			Code: "no_model_transform", Severity: SevInfo,
			Message: "no ModelTiepoint/PixelScale or ModelTransformation present", IFDID: ifd.ID})
	}
	return res
}

type unitInfo struct {
	name         string
	convertible  bool
	diag         string
}

func (r *Result) linearUnit() unitInfo {
	if r.Directory == nil {
		return unitInfo{name: "unknown", convertible: false, diag: "GeoKeyDirectory absent; linear unit unknown, value retained in original representation"}
	}
	for _, k := range r.Directory.Keys {
		if k.ID == 3076 {
			code := uint16(0)
			if x, ok := k.Resolved.(uint16); ok {
				code = x
			}
			if name, ok := linearUnits[code]; ok {
				return unitInfo{name: name, convertible: true}
			}
			return unitInfo{name: fmt.Sprintf("unknown:%d", code), convertible: false,
				diag: fmt.Sprintf("ProjLinearUnitsGeoKey code %d is not in the known unit table; value kept in place without conversion", code)}
		}
	}
	return unitInfo{name: "unknown", convertible: false, diag: "ProjLinearUnitsGeoKey not set; linear unit unknown, value retained"}
}

func parseDirectory(ifd *tiff.IFD) *KeyDirectory {
	t := findTag(ifd, tiff.TagGeoKeyDirectory)
	if t == nil {
		return nil
	}
	d := &KeyDirectory{IFDID: ifd.ID}
	vals := u16s(t)
	if len(vals) < 4 {
		d.Diagnostics = append(d.Diagnostics, &Diagnostic{Code: "geokey_header_short", Severity: SevError,
			Message: fmt.Sprintf("GeoKeyDirectory needs 4 header values, found %d", len(vals)), IFDID: ifd.ID})
		return d
	}
	d.Version, d.Revision, d.Minor = vals[0], vals[1], vals[2]
	// vals[3] is the number of key entries.
	n := vals[3]
	d.VersionRaw = [4]uint16{vals[0], vals[1], vals[2], vals[3]}
	if d.Version != 1 {
		d.Diagnostics = append(d.Diagnostics, &Diagnostic{Code: "geokey_bad_version", Severity: SevWarning,
			Message: fmt.Sprintf("GeoKey directory version %d is not the supported value 1; entries retained as-is", d.Version), IFDID: ifd.ID})
	}
	need := 4 + int(n)*4
	if len(vals) < need {
		d.Diagnostics = append(d.Diagnostics, &Diagnostic{Code: "geokey_entries_short", Severity: SevError,
			Message: fmt.Sprintf("GeoKeyDirectory declares %d keys (%d values), found %d", n, need, len(vals)), IFDID: ifd.ID})
	}
	count := int(n)
	if 4+count*4 > len(vals) {
		count = (len(vals) - 4) / 4
	}
	doubles := f64s(findTag(ifd, tiff.TagGeoDoubleParams))
	ascii := ""
	if at := findTag(ifd, tiff.TagGeoAsciiParams); at != nil && len(at.RawValue) > 0 {
		ascii = string(at.RawValue)
	}
	for i := 0; i < count; i++ {
		base := 4 + i*4
		keyID, loc, cnt, voff := vals[base], vals[base+1], vals[base+2], vals[base+3]
		k := GeoKey{ID: keyID, Name: KeyName(keyID), Location: loc, Count: cnt, ValueOrOffset: voff}
		switch {
		case loc == 0:
			k.Resolved = voff
		case loc == tiff.TagGeoDoubleParams:
			if int(voff)+int(cnt) <= len(doubles) {
				ds := doubles[voff : voff+uint16(cnt)]
				if cnt == 1 {
					k.Resolved = ds[0]
				} else {
					k.Resolved = ds
				}
			} else {
				k.Resolved = nil
				d.Diagnostics = append(d.Diagnostics, &Diagnostic{Code: "geokey_double_oob", Severity: SevError,
					Message: fmt.Sprintf("key %d double payload offset %d count %d exceeds %d doubles", keyID, voff, cnt, len(doubles)), IFDID: ifd.ID})
			}
		case loc == tiff.TagGeoAsciiParams:
			end := int(voff) + int(cnt)
			if end > len(ascii) {
				end = len(ascii)
			}
			if int(voff) <= len(ascii) {
				k.Resolved = ascii[voff:end]
			}
		default:
			k.Resolved = nil
			d.Diagnostics = append(d.Diagnostics, &Diagnostic{Code: "geokey_unknown_location", Severity: SevWarning,
				Message: fmt.Sprintf("key %d references unknown location tag %d", keyID, loc), IFDID: ifd.ID})
		}
		d.Keys = append(d.Keys, k)
	}
	return d
}
