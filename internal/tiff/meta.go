package tiff

// TIFF field type codes and sizes (same for classic and BigTIFF except LONG8).
type typeSpec struct {
	Name string
	Size int
}

var typeTable = map[uint16]typeSpec{
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

// Tag names limited to those the archive reasons about; unknown tags keep id.
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
	326: "JPEGInterchangeFormat",
	330: "SubIFDs",
	339: "SampleFormat",
	34665: "ExifIFD",
	34853: "GPSInfoIFD",
	33550: "ModelPixelScaleTag",
	33922: "ModelTiepointTag",
	34264: "ModelTransformationTag",
	34735: "GeoKeyDirectoryTag",
	34736: "GeoDoubleParamsTag",
	34737: "GeoAsciiParamsTag",
	42112: "GDAL_METADATA",
	42113: "GDAL_NODATA",
}

func tagName(t uint16) string {
	if n, ok := tagNames[t]; ok {
		return n
	}
	return "Tag" + itoa(int(t))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Pointer tags that create IFD graph edges.
var subIFDPointerTags = map[uint16]bool{
	330:   true,
	34665: true,
	34853: true,
}
