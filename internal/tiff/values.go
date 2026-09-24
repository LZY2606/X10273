package tiff

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// Decoder reads typed values out of raw tag bytes without discarding the raw
// representation: callers keep both Tag.RawValue and the decoded values.
type Decoder struct {
	order ByteOrder
}

func NewDecoder(order ByteOrder) Decoder { return Decoder{order: order} }

func (d Decoder) bo() binary.ByteOrder {
	if d.order == BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func (d Decoder) uint16(b []byte) uint16 { return d.bo().Uint16(b) }
func (d Decoder) uint32(b []byte) uint32 { return d.bo().Uint32(b) }
func (d Decoder) uint64(b []byte) uint64 { return d.bo().Uint64(b) }

// Numbers decodes a tag's count-managed scalar/rational array. It is the
// single normalization point from the raw representation and is only used by
// derived layers; the tag bytes remain authoritative.
func (d Decoder) Numbers(t Tag) ([]float64, error) {
	raw := t.raw()
	w, ok := typeWidth[t.Type]
	if !ok {
		return nil, fmt.Errorf("unknown field type %d for tag %d", t.Type, t.ID)
	}
	need := w * t.Count
	if len(raw) < need {
		return nil, fmt.Errorf("tag %d needs %d value bytes, %d present", t.ID, need, len(raw))
	}
	out := make([]float64, 0, t.Count)
	for i := 0; i < t.Count; i++ {
		v := raw[i*w : (i+1)*w]
		switch t.Type {
		case TypeByte, TypeShort, TypeLong, TypeIFD8, TypeLong8, TypeUndefined:
			switch w {
			case 1:
				out = append(out, float64(v[0]))
			case 2:
				out = append(out, float64(d.uint16(v)))
			case 4:
				out = append(out, float64(d.uint32(v)))
			case 8:
				out = append(out, float64(d.uint64(v)))
			}
		case TypeSByte:
			out = append(out, float64(int8(v[0])))
		case TypeSShort:
			out = append(out, float64(int16(d.uint16(v))))
		case TypeSLong:
			out = append(out, float64(int32(d.uint32(v))))
		case TypeSLong8:
			out = append(out, float64(int64(d.uint64(v))))
		case TypeFloat:
			out = append(out, float64(math.Float32frombits(d.uint32(v))))
		case TypeDouble:
			out = append(out, math.Float64frombits(d.uint64(v)))
		case TypeRational, TypeSRational:
			num := d.uint32(v[:4])
			den := d.uint32(v[4:])
			if den == 0 {
				return nil, fmt.Errorf("tag %d rational with zero denominator", t.ID)
			}
			if t.Type == TypeRational {
				out = append(out, float64(num)/float64(den))
			} else {
				out = append(out, float64(int32(num))/float64(int32(den)))
			}
		case TypeASCII:
			// ASCII arrays are handled by String.
			return out, nil
		}
	}
	return out, nil
}

// Ints is Numbers specialized for offset/count tags; it surfaces the
// conversion error instead of silently dropping unreadable values.
func (d Decoder) Ints(t Tag) ([]int64, error) {
	nums, err := d.Numbers(t)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(nums))
	for i, n := range nums {
		out[i] int64(n)
		out[i] = int64(n)
	}
	return out, nil
}

// String returns ASCII tag text with trailing NUL padding removed.
func (d Decoder) String(t Tag) string {
	s := string(t.raw())
	s = strings.TrimRight(s, "\x00")
	return s
}

var tagNames = map[int]string{
	TagImageWidth: "ImageWidth", TagImageLength: "ImageLength",
	TagBitsPerSample: "BitsPerSample", TagCompression: "Compression",
	TagPhotometric: "PhotometricInterpretation", TagStripOffsets: "StripOffsets",
	TagSamplesPerPixel: "SamplesPerPixel", TagRowsPerStrip: "RowsPerStrip",
	TagStripByteCounts: "StripByteCounts", TagTileWidth: "TileWidth",
	TagTileLength: "TileLength", TagTileOffsets: "TileOffsets",
	TagTileByteCounts: "TileByteCounts", TagSampleFormat: "SampleFormat",
	TagGeoKeyDirectory: "GeoKeyDirectoryTag", TagGeoDoubleParams: "GeoDoubleParamsTag",
	TagGeoAsciiParams: "GeoAsciiParamsTag", TagModelPixelScale: "ModelPixelScaleTag",
	TagModelTiepoint: "ModelTiepointTag", TagModelTransformation: "ModelTransformationTag",
	330: "SubIFDs", 34665: "ExifIFD", 40965: "GPSIFD", 51020: "GDAL_METADATA",
	284: "PlanarConfiguration", 33922: "ModelTiepointTag",
}

func tagName(id int) string {
	if n, ok := tagNames[id]; ok {
		return n
	}
	return fmt.Sprintf("Tag%d", id)
}
