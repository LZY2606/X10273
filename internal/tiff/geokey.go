package tiff

import "fmt"

// GeoKey is one entry of the GeoKeyDirectory, with its raw location
// preserved (directory slot, double-params or ascii-params).
type GeoKey struct {
	ID              string    `json:"id"` // geokey@<ifdID>:<keyID>
	KeyID           uint64    `json:"keyId"`
	Name            string    `json:"name"`
	TIFFTagLocation uint64    `json:"tiffTagLocation"` // 0 = directory, 34736 = doubles, 34737 = ascii
	Count           uint64    `json:"count"`
	ValueOffset     uint64    `json:"valueOffset"`
	Values          []uint64  `json:"values,omitempty"`
	Floats          []float64 `json:"floats,omitempty"`
	Text            string    `json:"text,omitempty"`
}

// GeoKeyDirectory is the parsed GeoKey header plus entries. The
// directory version triple is always preserved.
type GeoKeyDirectory struct {
	IFDID         string    `json:"ifdId"`
	Version       uint64    `json:"version"` // KeyDirectoryVersion
	KeyRevision   uint64    `json:"keyRevision"`
	MinorRevision uint64    `json:"minorRevision"`
	Keys          []*GeoKey `json:"keys"`
}

// GeoKeyNames covers the keys this project surfaces in the UI.
var GeoKeyNames = map[uint64]string{
	1024: "GTModelTypeGeoKey", 1025: "GTRasterTypeGeoKey",
	2048: "GeographicTypeGeoKey", 2050: "GeogGeodeticDatumGeoKey",
	2054: "GeogAngularUnitsGeoKey", 2056: "GeogEllipsoidGeoKey",
	2057: "GeogSemiMajorAxisGeoKey", 2059: "GeogInvFlatteningGeoKey",
	3072: "ProjectedCSTypeGeoKey", 3074: "ProjectionGeoKey",
	3075: "ProjCoordTransGeoKey", 3076: "ProjLinearUnitsGeoKey",
	4099: "VerticalUnitsGeoKey",
}

// GeoKeyName returns a readable name for a GeoKey ID.
func GeoKeyName(id uint64) string {
	if n, ok := GeoKeyNames[id]; ok {
		return n
	}
	return fmt.Sprintf("GeoKey%d", id)
}

// ParseGeoKeys extracts the GeoKey directory of an IFD, resolving
// values that live in GeoDoubleParams / GeoAsciiParams. Returns nil when
// the IFD carries no GeoKeyDirectoryTag.
func ParseGeoKeys(f *File, ifd *IFD) *GeoKeyDirectory {
	t := ifd.TagByCode(TagGeoKeyDirectory)
	if t == nil || len(t.Values) < 4 {
		return nil
	}
	dir := &GeoKeyDirectory{
		IFDID:         ifd.ID,
		Version:       t.Values[0],
		KeyRevision:   t.Values[1],
		MinorRevision: t.Values[2],
	}
	n := int(t.Values[3])
	doubles := ifd.TagByCode(TagGeoDoubleParams)
	ascii := ifd.TagByCode(TagGeoAsciiParams)
	for i := 0; i < n; i++ {
		base := 4 + i*4
		if base+4 > len(t.Values) {
			f.appendDiag(Diagnostic{
				Severity: "error", Code: "GEOKEY_TRUNCATED", SubjectID: t.ID,
				Message: fmt.Sprintf("GeoKeyDirectory declares %d keys but only %d slots fit", n, (len(t.Values)-4)/4),
			})
			break
		}
		k := &GeoKey{
			KeyID:           t.Values[base],
			TIFFTagLocation: t.Values[base+1],
			Count:           t.Values[base+2],
			ValueOffset:     t.Values[base+3],
		}
		k.ID = fmt.Sprintf("geokey@%s:%d", ifd.ID, k.KeyID)
		k.Name = GeoKeyName(k.KeyID)
		switch k.TIFFTagLocation {
		case 0:
			k.Values = []uint64{k.ValueOffset}
		case TagGeoDoubleParams:
			if doubles != nil {
				end := int(k.ValueOffset + k.Count)
				if end <= len(doubles.Floats) {
					k.Floats = doubles.Floats[k.ValueOffset:end]
				} else {
					f.appendDiag(Diagnostic{
						Severity: "error", Code: "GEOKEY_OUT_OF_BOUNDS", SubjectID: k.ID,
						Message: "GeoKey double-params slice exceeds GeoDoubleParamsTag",
					})
				}
			}
		case TagGeoAsciiParams:
			if ascii != nil {
				end := int(k.ValueOffset + k.Count)
				if end <= len(ascii.Text) {
					k.Text = ascii.Text[k.ValueOffset:end]
				} else {
					f.appendDiag(Diagnostic{
						Severity: "error", Code: "GEOKEY_OUT_OF_BOUNDS", SubjectID: k.ID,
						Message: "GeoKey ascii-params slice exceeds GeoAsciiParamsTag",
					})
				}
			}
		default:
			f.appendDiag(Diagnostic{
				Severity: "warning", Code: "GEOKEY_UNKNOWN_LOCATION", SubjectID: k.ID,
				Message: fmt.Sprintf("GeoKey %d uses unknown TIFFTagLocation %d", k.KeyID, k.TIFFTagLocation),
			})
		}
		dir.Keys = append(dir.Keys, k)
	}
	return dir
}
