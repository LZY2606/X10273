package tiff

import (
	"fmt"
	"strings"
)

const (
	keyGTModelType      = 1024
	keyGTRasterType     = 1025
	keyGTCitation       = 1026
	keyGeographicType   = 2048
	keyGeogCitation     = 2049
	keyGeogAngularUnits = 2054
	keyProjectedCSType  = 3072
	keyProjLinearUnits  = 3076
)

func buildGeo(g *Graph) {
	for _, ifd := range g.IFDs {
		keyTag := oneTag(g, ifd.ID, TagGeoKeyDirectory)
		if keyTag == nil || !keyTag.ValuePresent {
			buildTransforms(g, ifd, nil)
			continue
		}
		dir := parseGeoDirectory(g, ifd, keyTag)
		g.GeoDirectories = append(g.GeoDirectories, dir)
		buildTransforms(g, ifd, &dir)
	}
}

func parseGeoDirectory(g *Graph, ifd IFD, keyTag *Tag) GeoDirectory {
	nums := make([]int64, 0, len(keyTag.Raw.Values))
	for _, raw := range keyTag.Raw.Values {
		v, err := parseSignedInt64(raw)
		if err == nil {
			nums = append(nums, v)
		}
	}
	dir := GeoDirectory{IFDID: ifd.ID, KeyRange: keyTag.ValueRange}
	if len(nums) >= 4 {
		dir.Version = int(nums[0])
		dir.Revision = fmt.Sprintf("%d.%d", nums[1], nums[2])
	} else {
		g.Findings = append(g.Findings, Finding{ID: stableID(g.DocumentID, "geokey_truncated", ifd.ID),
			Kind: "geokey_version_missing", Severity: "error", ObjectID: ifd.ID,
			Message: "GeoKey directory does not contain version, revision and minor revision", ByteRange: &keyTag.ValueRange})
	}
	doublesTag := oneTag(g, ifd.ID, TagGeoDoubleParams)
	asciiTag := oneTag(g, ifd.ID, TagGeoAsciiParams)
	seen := map[uint16]int{}
	for i := 4; i+3 < len(nums); i += 4 {
		keyID := uint16(nums[i])
		location := uint16(nums[i+1])
		count := nums[i+2]
		valueOrOffset := nums[i+3]
		duplicate := seen[keyID]
		seen[keyID]++
		rawValues := []string{fmt.Sprintf("%d", valueOrOffset)}
		resolved := fmt.Sprintf("%d", valueOrOffset)
		if location == TagGeoDoubleParams && count == 1 && doublesTag != nil {
			if int(valueOrOffset) < len(doublesTag.Raw.Values) {
				resolved = doublesTag.Raw.Values[valueOrOffset]
				rawValues = []string{resolved}
			}
		}
		if location == TagGeoAsciiParams && asciiTag != nil {
			resolved, rawValues = geoASCII(asciiTag.Raw.Values, int(valueOrOffset), int(count))
		}
		key := GeoKey{ID: geoKeyID(g.DocumentID, ifd.Offset, keyID, uint16(duplicate)), IFDID: ifd.ID,
			Key: keyID, Name: geoKeyName(keyID), LocationTag: int64(location), Count: int(count),
			ValueOrOffset: valueOrOffset, ResolvedValue: resolved, RawValues: rawValues, DuplicateIndex: duplicate}
		dir.Keys = append(dir.Keys, key)
	}
	for keyID, count := range seen {
		if count > 1 {
			g.Findings = append(g.Findings, Finding{ID: stableID(g.DocumentID, "geokey_conflict", ifd.ID, keyID),
				Kind: "geokey_conflict", Severity: "warning", ObjectID: ifd.ID,
				Message: fmt.Sprintf("GeoKey %d (%s) appears %d times; all candidates are retained", keyID, geoKeyName(keyID), count)})
		}
	}
	return dir
}

func geoASCII(values []string, offset, count int) (string, []string) {
	if len(values) == 0 || offset < 0 || count <= 0 {
		return "", nil
	}
	joined := strings.Join(values, "")
	if offset >= len(joined) {
		return "", nil
	}
	end := offset + count
	if end > len(joined) {
		end = len(joined)
	}
	return joined[offset:end], []string{joined[offset:end]}
}

func geoKeyName(key uint16) string {
	names := map[uint16]string{1024: "GTModelTypeGeoKey", 1025: "GTRasterTypeGeoKey", 1026: "GTCitationGeoKey",
		2048: "GeographicTypeGeoKey", 2049: "GeogCitationGeoKey", 2054: "GeogAngularUnitsGeoKey",
		3072: "ProjectedCSTypeGeoKey", 3076: "ProjLinearUnitsGeoKey"}
	if name, ok := names[key]; ok {
		return name
	}
	return fmt.Sprintf("GeoKey_%d", key)
}

func buildTransforms(g *Graph, ifd IFD, dir *GeoDirectory) {
	scaleTag := oneTag(g, ifd.ID, TagModelPixelScale)
	tieTag := oneTag(g, ifd.ID, TagModelTiepoint)
	matrixTag := oneTag(g, ifd.ID, TagModelTransformation)
	index := 0
	if scaleTag != nil && tieTag != nil {
		candidate := TransformCandidate{ID: transformID(g.DocumentID, ifd.Offset, "tiepoint-scale", index),
			IFDID: ifd.ID, Kind: "ModelTiepoint+ModelPixelScale", Status: "candidate",
			SourceTagIDs: []string{tieTag.ID, scaleTag.ID}}
		candidate.Tiepoint = quantities(tieTag, rasterBasis(dir), 6)
		candidate.Scale = scaleQuantities(scaleTag, linearUnit(dir))
		if len(candidate.Tiepoint) != 6 || len(candidate.Scale) < 2 {
			candidate.Status = "rejected"
			candidate.RejectedReason = "tiepoint requires six values and pixel scale requires at least two values"
		}
		g.TransformCandidates = append(g.TransformCandidates, candidate)
		index++
	}
	if matrixTag != nil {
		candidate := TransformCandidate{ID: transformID(g.DocumentID, ifd.Offset, "transformation", 0),
			IFDID: ifd.ID, Kind: "ModelTransformation", Status: "candidate", SourceTagIDs: []string{matrixTag.ID}}
		for _, raw := range matrixTag.Raw.Values {
			v, err := parseFloat(raw)
			if err != nil {
				candidate.Status = "rejected"
				candidate.RejectedReason = "transformation contains an invalid double"
				break
			}
			candidate.Coefficients = append(candidate.Coefficients, v)
		}
		if len(candidate.Coefficients) != 16 {
			candidate.Status = "rejected"
			candidate.RejectedReason = "ModelTransformation requires 16 doubles"
		}
		g.TransformCandidates = append(g.TransformCandidates, candidate)
	}
	if scaleTag != nil && tieTag != nil && matrixTag != nil {
		valid := 0
		for _, c := range g.TransformCandidates {
			if c.IFDID == ifd.ID && c.Status == "candidate" {
				valid++
			}
		}
		if valid > 1 {
			g.Findings = append(g.Findings, Finding{ID: stableID(g.DocumentID, "transform_conflict", ifd.ID),
				Kind: "transform_conflict", Severity: "warning", ObjectID: ifd.ID,
				Message: "ModelTiepoint/ModelPixelScale and ModelTransformation are both present; no candidate is selected automatically"})
		}
	}
}

func quantities(tag *Tag, basis string, expected int) []Quantity {
	if tag == nil {
		return nil
	}
	out := make([]Quantity, 0, len(tag.Raw.Values))
	units := []string{"pixel", "pixel", "pixel", basis, basis, basis}
	for i, raw := range tag.Raw.Values {
		v, err := parseFloat(raw)
		q := Quantity{Raw: raw, Value: v, Basis: basis}
		if i < len(units) {
			q.Unit = units[i]
		}
		if err != nil {
			q.Converted = false
			q.Diagnostic = "numeric conversion failed; raw value retained"
		} else {
			q.Converted = true
		}
		out = append(out, q)
	}
	return out
}

func scaleQuantities(tag *Tag, unit string) []Quantity {
	if tag == nil {
		return nil
	}
	units := []string{unit + "/pixel", unit + "/pixel", unit + "/pixel"}
	out := make([]Quantity, 0, len(tag.Raw.Values))
	for i, raw := range tag.Raw.Values {
		v, err := parseFloat(raw)
		q := Quantity{Raw: raw, Value: v, Basis: "raster-to-model", Converted: err == nil}
		if i < len(units) {
			q.Unit = units[i]
		}
		if err != nil {
			q.Diagnostic = "numeric conversion failed; raw value retained"
		}
		out = append(out, q)
	}
	return out
}

func rasterBasis(dir *GeoDirectory) string {
	if dir == nil {
		return "model-space:unspecified"
	}
	for _, key := range dir.Keys {
		if key.Key == keyGTRasterType {
			return "model-space:raster-type-" + key.ResolvedValue
		}
	}
	return "model-space:unspecified"
}

func linearUnit(dir *GeoDirectory) string {
	if dir != nil {
		for _, key := range dir.Keys {
			if key.Key == keyProjLinearUnits && key.ResolvedValue == "9001" {
				return "metre"
			}
			if key.Key == keyGeogAngularUnits && key.ResolvedValue == "9102" {
				return "degree"
			}
		}
	}
	return "unit-code-unresolved"
}
