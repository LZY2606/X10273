package tiff

import (
	"fmt"

	"ifdgraph/internal/model"
)

// buildGeo extracts the raw GeoTIFF material. Values stay in their original
// representation; normalization to candidate transforms happens in derive.
func buildGeo(g *model.ParseGraph) {
	for _, ifd := range g.IFDs {
		if !ifd.Reachable {
			continue
		}
		gd := model.GeoData{IFDID: ifd.ID}
		has := false
		if t := tagByCode(g, ifd, TagModelPixelScale); t != nil && len(t.Doubles) >= 3 {
			gd.PixelScale = append(gd.PixelScale, t.Doubles[:3]...)
			has = true
		}
		if t := tagByCode(g, ifd, TagModelTiepoint); t != nil {
			for i := 0; i+6 <= len(t.Doubles); i += 6 {
				gd.Tiepoints = append(gd.Tiepoints, append([]float64(nil), t.Doubles[i:i+6]...))
			}
			has = true
		}
		if t := tagByCode(g, ifd, TagModelTransform); t != nil && len(t.Doubles) >= 16 {
			gd.Transformation = append([]float64(nil), t.Doubles[:16]...)
			has = true
		}
		if t := tagByCode(g, ifd, TagGeoDoubleParams); t != nil {
			gd.GeoDoubleParams = append(gd.GeoDoubleParams, t.Doubles...)
			has = true
		}
		if t := tagByCode(g, ifd, TagGeoASCIIParams); t != nil {
			gd.GeoASCIIParams = t.ASCII
			has = true
		}
		if t := tagByCode(g, ifd, TagGeoKeyDir); t != nil {
			nums := t.Numbers
			if len(nums) >= 4 {
				gd.Version = &model.GeoKeyVersion{
					KeyDirectoryVersion: uint16(nums[0]),
					KeyRevision:         uint16(nums[1]),
					MinorRevision:       uint16(nums[2]),
					NumberOfKeys:        uint16(nums[3]),
				}
				expected := 4 + int(gd.Version.NumberOfKeys)*4
				if len(nums) < expected {
					diag(g, ifd.ID, "geokey_truncated",
						fmt.Sprintf("GeoKey directory declares %d keys but has %d SHORT values", gd.Version.NumberOfKeys, (len(nums)-4)/4))
				}
				for i := 0; i < int(gd.Version.NumberOfKeys) && 4+(i+1)*4 <= len(nums); i++ {
					base := 4 + i*4
					k := model.GeoKey{
						ID:       model.GeoKeyID(g.SourceID, ifd.Offset, uint16(nums[base])),
						Key:      uint16(nums[base]),
						Location: uint16(nums[base+1]),
						Count:    uint16(nums[base+2]),
						Value:    uint16(nums[base+3]),
					}
					resolveGeoKey(&k, gd.GeoDoubleParams, gd.GeoASCIIParams, &g)
					gd.Keys = append(gd.Keys, k)
				}
				has = true
			} else {
				diag(g, ifd.ID, "geokey_header", "GeoKey directory shorter than 4 SHORTs")
			}
		}
		if has {
			g.Geo = append(g.Geo, gd)
		}
	}
}

func resolveGeoKey(k *model.GeoKey, doubles []float64, ascii string, g **model.ParseGraph) {
	switch k.Location {
	case 0:
		k.Resolved = k.Value
	case TagGeoDoubleParams:
		idx := int(k.Value)
		end := idx + int(k.Count)
		if end <= len(doubles) {
			k.Resolved = doubles[idx:end]
		} else {
			k.Diagnostic = fmt.Sprintf("GeoKey references GeoDoubleParams[%d:%d] but only %d values present", idx, end, len(doubles))
		}
	case TagGeoASCIIParams:
		idx := int(k.Value)
		end := idx + int(k.Count)
		if end <= len(ascii) {
			k.Resolved = ascii[idx:end]
		} else {
			k.Diagnostic = fmt.Sprintf("GeoKey references GeoASCIIParams[%d:%d] but only %d bytes present", idx, end, len(ascii))
		}
	default:
		k.Diagnostic = fmt.Sprintf("GeoKey location tag %d is not a recognized parameter tag", k.Location)
	}
	if k.Diagnostic != "" {
		diag(*g, k.ID, "geokey_unresolved", k.Diagnostic)
	}
}
