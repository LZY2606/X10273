package tiff

// Tag names that matter for pixel storage and georeferencing. Unknown tags are
// preserved numerically; the name is left as "Tag<id>".
var tagNames = map[uint16]string{
	254: "NewSubfileType",
	255: "SubfileType",
	256: "ImageWidth",
	257: "ImageLength",
	258: "BitsPerSample",
	259: "Compression",
	262: "PhotometricInterpretation",
	273: "StripOffsets",
	277: "SamplesPerPixel",
	278: "RowsPerStrip",
	279: "StripByteCounts",
	284: "PlanarConfiguration",
	322: "TileWidth",
	323: "TileLength",
	324: "TileOffsets",
	325: "TileByteCounts",
	330: "SubIFDs",
	339: "SampleFormat",
	33550: "ModelPixelScaleTag",
	33922: "ModelTiepointTag",
	34264: "ModelTransformationTag",
	34735: "GeoKeyDirectoryTag",
	34736: "GeoDoubleParamsTag",
	34737: "GeoAsciiParamsTag",
	34738: "GeoTIFFVersionGeoKey",
}

func tagName(t uint16) string {
	if n, ok := tagNames[t]; ok {
		return n
	}
	return "Tag" + itoa(uint64(t))
}

// pointerTags name tag ids whose values are interpreted as IFD pointers.
var pointerTags = map[uint16]bool{
	330: true, // SubIFDs
}

// TIFF field types; sizes are per-element on disk.
type typeInfo struct {
	name string
	size int64
}

var fieldTypes = map[uint16]typeInfo{
	1:  {"BYTE", 1},
	2:  {"ASCII", 1},
	3:  {"SHORT", 2},
	4:  {"LONG", 4},
	5:  {"RATIONAL", 8},
	6:  {"SBYTE", 1},
	7:  {"UNDEFINED", 1},
	8:  {"SSHORT", 2},
	9:  {"SLONG", 4},
	10: {"SRATIONAL", 8},
	11: {"FLOAT", 4},
	12: {"DOUBLE", 8},
	13: {"IFD", 4},
	16: {"LONG8", 8},
	17: {"SLONG8", 8},
	18: {"IFD8", 8},
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
