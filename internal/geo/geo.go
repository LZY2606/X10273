// Package geo decodes GeoTIFF GeoKey directories and derives explicit,
// unit-bearing geo-transform candidates. Raw representations are always
// retained; normalization happens only in candidate values and unconvertible
// inputs stay in place with diagnostics instead of being dropped.
package geo

import (
	"fmt"
	"math"

	"ifdroom/internal/tiff"
)

const RuleVersion = "geotiff-rules-1.0.0"

// GeoKey IDs used by the case pack (full GeoTIFF 1.1 subset).
const (
	KeyModelType        uint16 = 1024
	KeyRasterType       uint16 = 1025
	KeyCitation         uint16 = 1026
	KeyGeographicType   uint16 = 2048
	KeyGeogCitation     uint16 = 2049
	KeyGeogAngularUnits uint16 = 2054
	KeyProjectedType    uint16 = 3072
	KeyProjLinearUnits  uint16 = 3076
	KeyVerticalUnits    uint16 = 4099
)

type RawKey struct {
	ID            uint16 `json:"id"`
	Location      uint16 `json:"location"`
	Count         uint16 `json:"count"`
	ValueOrOffset uint16 `json:"valueOrOffset"`
	Value         string `json:"value"`
	ValueKind     string `json:"valueKind"` // "short" | "ascii" | "double"
}

type KeyDirectory struct {
	Version     uint16   `json:"keyDirectoryVersion"`
	Revision    uint16   `json:"keyRevision"`
	Minor       uint16   `json:"minorRevision"`
	KeyCount    uint16   `json:"keyCount"`
	Keys        []RawKey `json:"keys"`
	Diagnostics []string `json:"diagnostics"`
}

// ParseKeyDir decodes tag 34735 plus its 34736/34737 parameter tags.
func ParseKeyDir(ifd *tiff.IFD) *KeyDirectory {
	kd := &KeyDirectory{}
	e := ifd.ByTag[tiff.TagGeoKeyDir]
	if e == nil {
		return nil
	}
	shorts := e.Uints
	if len(shorts) < 4 {
		kd.Diagnostics = append(kd.Diagnostics, "geokey directory header truncated")
		return kd
	}
	kd.KeyCount = uint16(shorts[3])
	kd.Version = uint16(shorts[0])
	kd.Revision = uint16(shorts[1])
	kd.Minor = uint16(shorts[2])
	need := 4 + int(kd.KeyCount)*4
	if len(shorts) < need {
		kd.Diagnostics = append(kd.Diagnostics, fmt.Sprintf(
			"geokey directory declares %d keys but holds %d values", kd.KeyCount, (len(shorts)-4)/4))
	}
	doubles := []float64{}
	if de := ifd.ByTag[tiff.TagGeoDoubleParams]; de != nil {
		doubles = de.Doubles
	}
	for i := 0; i < int(kd.KeyCount) && 4+(i+1)*4 <= len(shorts); i++ {
		base := 4 + i*4
		rk := RawKey{
			ID:            uint16(shorts[base]),
			Location:      uint16(shorts[base+1]),
			Count:         uint16(shorts[base+2]),
			ValueOrOffset: uint16(shorts[base+3]),
		}
		switch rk.Location {
		case 0:
			rk.ValueKind = "short"
			rk.Value = fmt.Sprintf("%d", rk.ValueOrOffset)
		case tiff.TagGeoDoubleParams:
			rk.ValueKind = "double"
			idx := int(rk.ValueOrOffset)
			if idx+int(rk.Count) <= len(doubles) {
				rk.Value = fmt.Sprintf("%v", doubles[idx:idx+int(rk.Count)])
			} else {
				kd.Diagnostics = append(kd.Diagnostics, fmt.Sprintf(
					"key %d double params @%d out of range (have %d)", rk.ID, idx, len(doubles)))
			}
		case tiff.TagGeoAsciiParams:
			rk.ValueKind = "ascii"
			idx := int(rk.ValueOrOffset)
			end := idx + int(rk.Count)
			// ASCII params are NUL separated; tag decoding trims only the
			// trailing NUL, so split inside the blob.
			raw := asciiBlob(ifd)
			if end <= len(raw) {
				rk.Value = trimNUL(raw[idx:end])
			} else {
				kd.Diagnostics = append(kd.Diagnostics, fmt.Sprintf(
					"key %d ascii params @%d:%d out of range (have %d)", rk.ID, idx, end, len(raw)))
			}
		default:
			rk.ValueKind = "unknown-location"
			kd.Diagnostics = append(kd.Diagnostics, fmt.Sprintf(
				"key %d references unknown parameter tag %d", rk.ID, rk.Location))
		}
		kd.Keys = append(kd.Keys, rk)
	}
	return kd
}

func asciiBlob(ifd *tiff.IFD) string {
	if ae := ifd.ByTag[tiff.TagGeoAsciiParams]; ae != nil {
		return ae.RawAscii
	}
	return ""
}

func trimNUL(b string) string {
	for len(b) > 0 && (b[len(b)-1] == 0 || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

var _ = math.NaN
