package tiff

// typeSizes maps TIFF field type codes to their on-disk element size.
var typeSizes = map[uint16]int{
	1: 1,  // BYTE
	2: 1,  // ASCII
	3: 2,  // SHORT
	4: 4,  // LONG
	5: 8,  // RATIONAL
	6: 1,  // SBYTE
	7: 1,  // UNDEFINED
	8: 2,  // SSHORT
	9: 4,  // SLONG
	10: 8, // SRATIONAL
	11: 4, // FLOAT
	12: 8, // DOUBLE
	13: 8, // IFD (BigTIFF)
	16: 8, // LONG8 (BigTIFF)
	17: 8, // SLONG8 (BigTIFF)
	18: 8, // IFD8 (BigTIFF)
}

func typeName(t uint16) string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	return "Type" + itoa(t)
}

var typeNames = map[uint16]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}

const (
	TagImageWidth      uint16 = 256
	TagImageLength     uint16 = 257
	TagBitsPerSample   uint16 = 258
	TagCompression     uint16 = 259
	TagPhotometric     uint16 = 262
	TagStripOffsets    uint16 = 273
	TagSamplesPerPixel uint16 = 277
	TagRowsPerStrip    uint16 = 278
	TagStripByteCounts uint16 = 279
	TagTileWidth       uint16 = 322
	TagTileLength      uint16 = 323
	TagTileOffsets     uint16 = 324
	TagTileByteCounts  uint16 = 325
	TagSampleFormat    uint16 = 339
	TagSubIFDs         uint16 = 330
	TagGeoKeyDirectory uint16 = 34735
	TagGeoDoubleParams uint16 = 34736
	TagGeoAsciiParams  uint16 = 34737
	TagModelPixelScale uint16 = 33550
	TagModelTiepoint   uint16 = 33922
	TagModelTransform  uint16 = 34264
	TagGeoNAD          uint16 = 33920 // GeoTIFF coordinate system NAD GeoKeys (rare)
)

var tagNames = map[uint16]string{
	256:  "ImageWidth",
	257:  "ImageLength",
	258:  "BitsPerSample",
	259:  "Compression",
	262:  "PhotometricInterpretation",
	273:  "StripOffsets",
	277:  "SamplesPerPixel",
	278:  "RowsPerStrip",
	279:  "StripByteCounts",
	322:  "TileWidth",
	323:  "TileLength",
	324:  "TileOffsets",
	325:  "TileByteCounts",
	339:  "SampleFormat",
	330:  "SubIFDs",
	33550: "ModelPixelScaleTag",
	33922: "ModelTiepointTag",
	34264: "ModelTransformationTag",
	34735: "GeoKeyDirectoryTag",
	34736: "GeoDoubleParamsTag",
	34737: "GeoAsciiParamsTag",
}
