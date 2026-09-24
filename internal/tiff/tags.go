package tiff

// Field type sizes in bytes (TIFF 6.0 + BigTIFF types 16-18).
var typeSizes = map[uint16]uint64{
	1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2,
	9: 4, 10: 8, 11: 4, 12: 8, 16: 8, 17: 8, 18: 8,
}

var typeNames = map[uint16]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}

// Tag codes used by the review UI and geo analysis.
const (
	TagImageWidth          = 256
	TagImageLength         = 257
	TagBitsPerSample       = 258
	TagCompression         = 259
	TagPhotometric         = 262
	TagStripOffsets        = 273
	TagSamplesPerPixel     = 277
	TagRowsPerStrip        = 278
	TagStripByteCounts     = 279
	TagPlanarConfig        = 284
	TagTileWidth           = 322
	TagTileLength          = 323
	TagTileOffsets         = 324
	TagTileByteCounts      = 325
	TagSubIFDs             = 330
	TagSampleFormat        = 339
	TagModelPixelScale     = 33550
	TagModelTiepoint       = 33922
	TagModelTransformation = 34264
	TagGeoKeyDirectory     = 34735
	TagGeoDoubleParams     = 34736
	TagGeoAsciiParams      = 34737
)

var tagNames = map[uint16]string{
	256: "ImageWidth", 257: "ImageLength", 258: "BitsPerSample",
	259: "Compression", 262: "PhotometricInterpretation",
	273: "StripOffsets", 277: "SamplesPerPixel", 278: "RowsPerStrip",
	279: "StripByteCounts", 284: "PlanarConfiguration",
	322: "TileWidth", 323: "TileLength", 324: "TileOffsets", 325: "TileByteCounts",
	330: "SubIFDs", 339: "SampleFormat",
	33550: "ModelPixelScaleTag", 33922: "ModelTiepointTag",
	34264: "ModelTransformationTag", 34735: "GeoKeyDirectoryTag",
	34736: "GeoDoubleParamsTag", 34737: "GeoAsciiParamsTag",
}

// TagName returns a human-readable name for a tag code.
func TagName(code uint16) string {
	if n, ok := tagNames[code]; ok {
		return n
	}
	return "Tag"
}

// TypeName returns a human-readable name for a field type.
func TypeName(t uint16) string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	return "UNKNOWN"
}
