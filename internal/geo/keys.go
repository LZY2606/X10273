package geo

import (
	"fmt"

	"tiffifd/internal/tiff"
)

// GeoKey parameter-location tags (the `TIFFTagLocation` field points at one).
const (
	locZero       uint16 = 0
	locGeoDouble  uint16 = 34736
	locGeoASCII   uint16 = 34737
)

// Selected GeoKey ids relevant to units/CRS labelling.
const (
	keyGTModelType      uint16 = 1024
	keyGTRasterType     uint16 = 1025
	keyGTCitation       uint16 = 1026
	keyGeographicType   uint16 = 2048
	keyGeogCitation     uint16 = 2049
	keyProjectedType    uint16 = 3072
	keyProjLinearUnits  uint16 = 3076
	keyVerticalUnits    uint16 = 4099
)

var keyNames = map[uint16]string{
	1024: "GTModelTypeGeoKey",
	1025: "GTRasterTypeGeoKey",
	1026: "GTCitationGeoKey",
	2048: "GeographicTypeGeoKey",
	2049: "GeogCitationGeoKey",
	3072: "ProjectedCSTypeGeoKey",
	3076: "ProjLinearUnitsGeoKey",
	4099: "VerticalUnitsGeoKey",
}

var linearUnitNames = map[uint16]string{
	9001: "metre",
	9002: "foot",
	9003: "us_survey_foot",
	9007: "kilometre",
}

func keyName(id uint16) string {
	if n, ok := keyNames[id]; ok {
		return n
	}
	return fmt.Sprintf("GeoKey%d", id)
}

// parseKeys reads the GeoKey directory and resolves values against the double
// and ascii parameter tags. Unconvertible / out-of-range references stay in
// place with a diagnostic instead of being dropped.
func (a *Analysis) parseKeys(file *tiff.File, keyTag, dblTag, ascTag *tiff.Tag) {
	shorts := file.DecodeIntegers(keyTag)
	if len(shorts) < 4 {
		a.Diagnostics = append(a.Diagnostics, "GeoKeyDirectory shorter than 4-word header")
		return
	}
	a.Header = &KeyHeader{
		KeyDirectoryVersion: uint16(shorts[0]),
		KeyRevision:         uint16(shorts[1]),
		MinorRevision:       uint16(shorts[2]),
		NumberOfKeys:        uint16(shorts[3]),
	}
	if shorts[0] != 1 {
		a.Diagnostics = append(a.Diagnostics,
			fmt.Sprintf("unexpected GeoKey directory version %d (expected 1)", shorts[0]))
	}

	dbl := []float64{}
	if dblTag != nil {
		dbl = file.DecodeDoubles(dblTag)
	}
	asc := ""
	if ascTag != nil {
		asc, _ = file.DecodeASCII(ascTag)
	}

	seen := map[uint16][]int{}
	n := int(a.Header.NumberOfKeys)
	for i := 0; i < n; i++ {
		base := 4 + i*4
		if base+3 >= len(shorts) {
			a.Diagnostics = append(a.Diagnostics,
				fmt.Sprintf("GeoKey entry %d truncated (directory declares %d keys)", i, n))
			break
		}
		id := uint16(shorts[base])
		loc := uint16(shorts[base+1])
		cnt := uint16(shorts[base+2])
		vo := shorts[base+3]

		entryStep := int64(keyTag.Value.Type.Size())
		src := fmt.Sprintf("tag%d@entry%d:%d/ext@%d:%d#key%d",
			keyTag.ID, keyTag.Range.Offset, keyTag.Range.Length,
			keyTag.Value.ValueRange.Offset, keyTag.Value.ValueRange.Length, i)
		_ = entryStep

		k := GeoKey{
			ID: id, Location: loc, Count: cnt, ValueOffset: vo,
			Name: keyName(id), Index: i, Source: src,
		}
		k.resolve(loc, cnt, vo, dbl, asc, dblTag, ascTag)
		a.Keys = append(a.Keys, k)
		seen[id] = append(seen[id], i)
	}

	for id, idxs := range seen {
		if len(idxs) > 1 {
			a.KeyConflicts = append(a.KeyConflicts, KeyConflict{
				KeyID: id, Indexes: idxs,
				Message: fmt.Sprintf("GeoKey %d (%s) appears %d times; duplicate ids are illegal, all entries retained",
					id, keyName(id), len(idxs)),
			})
		}
	}
}

func (k *GeoKey) resolve(loc, cnt uint16, vo int64, dbl []float64, asc string, dblTag, ascTag *tiff.Tag) {
	switch loc {
	case locZero:
		k.Kind = "short"
		k.Value = fmt.Sprintf("%d", vo)
		if k.ID == keyProjLinearUnits {
			if name, ok := linearUnitNames[uint16(vo)]; ok {
				k.Value = name
			}
		}
	case locGeoDouble:
		k.Kind = "double"
		k.ParamSource = fmt.Sprintf("tag%d@ext@%d:%d", dblTag.ID, dblTag.Value.ValueRange.Offset, dblTag.Value.ValueRange.Length)
		if vo < 0 || int(vo)+int(cnt) > len(dbl) {
			k.Kind = "unresolved"
			k.Diagnostic = fmt.Sprintf("GeoDoubleParams index %d count %d out of range (have %d doubles)", vo, cnt, len(dbl))
			return
		}
		if cnt == 1 {
			k.Value = fmt.Sprintf("%g", dbl[vo])
		} else {
			k.Value = fmt.Sprintf("%v", dbl[vo:vo+int64(cnt)])
		}
	case locGeoASCII:
		k.Kind = "ascii"
		k.ParamSource = fmt.Sprintf("tag%d@ext@%d:%d", ascTag.ID, ascTag.Value.ValueRange.Offset, ascTag.Value.ValueRange.Length)
		if vo < 0 || vo >= int64(len(asc)) {
			k.Kind = "unresolved"
			k.Diagnostic = fmt.Sprintf("GeoAsciiParams offset %d out of range (length %d)", vo, len(asc))
			return
		}
		end := vo + int64(cnt)
		if end > int64(len(asc)) {
			end = int64(len(asc))
			k.Diagnostic = "GeoAsciiParams value exceeds parameter length; kept the in-range prefix"
		}
		k.Value = asc[vo:end]
	default:
		k.Kind = "unresolved"
		k.Diagnostic = fmt.Sprintf("unknown TIFFTagLocation %d for GeoKey %d", loc, k.ID)
	}
}
