package tiff

// TIFF field type codes and sizes (identical for classic and BigTIFF values).
var typeSizes = map[uint16]int{
	1: 1,  // BYTE
	2: 1,  // ASCII
	3: 2,  // SHORT
	4: 4,  // LONG
	5: 8,  // RATIONAL (two LONGs)
	6: 1,  // SBYTE
	7: 1,  // UNDEFINED
	8: 2,  // SSHORT
	9: 4,  // SLONG
	10: 8, // SRATIONAL
	11: 4, // FLOAT
	12: 8, // DOUBLE
	13: 4, // IFD (classic width)
	16: 8, // LONG8 (BigTIFF)
	17: 8, // SLONG8 (BigTIFF)
	18: 8, // IFD8 (BigTIFF)
}

var typeNames = map[uint16]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}

// Well-known tag ids used by the parser, layout and geo layers.
const (
	TagImageWidth      = 256
	TagImageLength     = 257
	TagBitsPerSample   = 258
	TagCompression     = 259
	TagPhotometric     = 262
	TagStripOffsets    = 273
	TagSamplesPerPixel = 277
	TagRowsPerStrip    = 278
	TagStripByteCounts = 279
	TagTileWidth       = 322
	TagTileLength      = 323
	TagTileOffsets     = 324
	TagTileByteCounts  = 325
	TagSampleFormat    = 339
	TagSubIFDs         = 330
	TagModelPixelScale = 33550
	TagModelTiepoint   = 33922
	TagModelTransform  = 34264
	TagGeoKeyDirectory = 34735
	TagGeoDoubleParams = 34736
	TagGeoAsciiParams  = 34737
)

var tagNames = map[uint16]string{
	256:   "ImageWidth",
	257:   "ImageLength",
	258:   "BitsPerSample",
	259:   "Compression",
	262:   "PhotometricInterpretation",
	273:   "StripOffsets",
	277:   "SamplesPerPixel",
	278:   "RowsPerStrip",
	279:   "StripByteCounts",
	322:   "TileWidth",
	323:   "TileLength",
	324:   "TileOffsets",
	325:   "TileByteCounts",
	330:   "SubIFDs",
	339:   "SampleFormat",
	33550: "ModelPixelScaleTag",
	33922: "ModelTiepointTag",
	34264: "ModelTransformationTag",
	34735: "GeoKeyDirectoryTag",
	34736: "GeoDoubleParamsTag",
	34737: "GeoAsciiParamsTag",
}

func TagName(id uint16) string {
	if n, ok := tagNames[id]; ok {
		return n
	}
	return "Tag" + itoa(id)
}

func itoa(v uint16) string {
	if v == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
