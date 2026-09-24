package tiff

// Well known GeoKey ids referenced by name in the UI.
const (
	KeyGTModelType  uint16 = 1024
	KeyGTRasterType uint16 = 1025
	KeyGTCitation   uint16 = 1026
	KeyGeographicType uint16 = 2048
	KeyGeogCitation  uint16 = 2049
	KeyGeogAngularUnits uint16 = 2054
	KeyProjectedCSType = 3072
	KeyPCSCitation     = 3073
	KeyProjLinearUnits = 3076
)

// GeoKeyName covers the keys the case archive explains.
var GeoKeyName = map[uint16]string{
	KeyGTModelType:       "GTModelTypeGeoKey",
	KeyGTRasterType:      "GTRasterTypeGeoKey",
	KeyGTCitation:        "GTCitationGeoKey",
	KeyGeographicType:    "GeographicTypeGeoKey",
	KeyGeogCitation:      "GeogCitationGeoKey",
	KeyGeogAngularUnits:  "GeogAngularUnitsGeoKey",
	KeyProjectedCSType:   "ProjectedCSTypeGeoKey",
	KeyPCSCitation:       "PCSCitationGeoKey",
	KeyProjLinearUnits:   "ProjLinearUnitsGeoKey",
}

// parseGeoKeys reads the GeoKey directory while preserving its version
// header and every key's raw (ID, Location, Count, Value/Offset) tuple.
func parseGeoKeys(r *reader, d *IFD, doc *Document) *GeoKeyDirectory {
	t := d.TagByCode(TagGeoKeyDirectory)
	if t == nil {
		return nil
	}
	vals := t.Decoded
	rng := t.EntryRange
	if t.External != nil {
		rng = *t.External
	}
	if len(vals) < 4 {
		doc.find("geokey", "error", "GeoKey directory shorter than header", &rng)
		return nil
	}
	gk := &GeoKeyDirectory{
		Version:       uint16(vals[0]),
		Revision:      uint16(vals[1]),
		MinorRevision: uint16(vals[2]),
		RawRange:      rng,
	}
	number := vals[3]
	if gk.Version != 1 {
		doc.find("geokey", "warning",
			"unexpected GeoKey KeyDirectoryVersion; raw header retained", &rng)
	}
	if uint64(len(vals)-4) < number*4 {
		doc.find("geokey", "error",
			"GeoKey directory declares more keys than present; partial keys retained", &rng)
	}
	doubleTag := d.TagByCode(TagGeoDoubleParams)
	asciiTag := d.TagByCode(TagGeoAsciiParams)
	for i := uint64(0); i < number; i++ {
		base := 4 + i*4
		if base+4 > uint64(len(vals)) {
			break
		}
		key := GeoKey{
			ID:       uint16(vals[base]),
			Location: uint16(vals[base+1]),
			Count:    uint16(vals[base+2]),
			Raw:      []uint16{uint16(vals[base]), uint16(vals[base+1]), uint16(vals[base+2]), uint16(vals[base+3])},
		}
		switch key.Location {
		case 0:
			key.Value = vals[base+3]
		case TagGeoDoubleParams:
			idx := vals[base+3]
			if doubleTag == nil || idx+uint64(key.Count) > uint64(len(doubleTag.DecodedFloat)) {
				doc.find("geokey", "error",
					"GeoKey points outside GeoDoubleParams; original offset retained", &rng)
				key.Value = vals[base+3]
			} else {
				key.ValueStr = formatFloats(doubleTag.DecodedFloat[idx : idx+uint64(key.Count)])
			}
		case TagGeoAsciiParams:
			idx := vals[base+3]
			if asciiTag == nil || idx >= uint64(len(asciiTag.ASCII)) {
				doc.find("geokey", "error",
					"GeoKey points outside GeoAsciiParams; original offset retained", &rng)
				key.Value = vals[base+3]
			} else {
				end := idx + uint64(key.Count)
				s := asciiTag.ASCII
				if end > uint64(len(s)) {
					end = uint64(len(s))
				}
				key.ValueStr = trimNul(s[idx:end])
			}
		default:
			doc.find("geokey", "warning",
				"GeoKey references unknown location tag; raw tuple retained", &rng)
			key.Value = vals[base+3]
		}
		gk.Keys = append(gk.Keys, key)
	}
	return gk
}

func formatFloats(fs []float64) string {
	out := ""
	for i, f := range fs {
		if i > 0 {
			out += ","
		}
		out += formatFloat(f)
	}
	return out
}

func trimNul(s string) string {
	for len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return s
}
