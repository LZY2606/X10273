package tiff

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
	TagXResolution     uint16 = 282
	TagYResolution     uint16 = 283
	TagTileWidth       uint16 = 322
	TagTileLength      uint16 = 323
	TagTileOffsets     uint16 = 324
	TagTileByteCounts  uint16 = 325
	TagSampleFormat    uint16 = 339
	TagSubIFDs         uint16 = 330
	TagJPEGTables      uint16 = 347
	TagGeoKeyDirectory uint16 = 34735
	TagGeoDoubleParams uint16 = 34736
	TagGeoAsciiParams  uint16 = 34737
	TagModelPixelScale  uint16 = 33550
	TagModelTiepoint    uint16 = 33922
	TagModelTransform   uint16 = 34264
)

var tagNames = map[uint16]string{
	TagImageWidth:      "ImageWidth",
	TagImageLength:     "ImageLength",
	TagBitsPerSample:   "BitsPerSample",
	TagCompression:     "Compression",
	TagPhotometric:     "PhotometricInterpretation",
	TagStripOffsets:    "StripOffsets",
	TagSamplesPerPixel: "SamplesPerPixel",
	TagRowsPerStrip:    "RowsPerStrip",
	TagStripByteCounts: "StripByteCounts",
	TagXResolution:     "XResolution",
	TagYResolution:     "YResolution",
	TagTileWidth:       "TileWidth",
	TagTileLength:      "TileLength",
	TagTileOffsets:     "TileOffsets",
	TagTileByteCounts:  "TileByteCounts",
	TagSampleFormat:    "SampleFormat",
	TagSubIFDs:         "SubIFDs",
	TagJPEGTables:      "JPEGTables",
	TagGeoKeyDirectory: "GeoKeyDirectoryTag",
	TagGeoDoubleParams: "GeoDoubleParamsTag",
	TagGeoAsciiParams:  "GeoAsciiParamsTag",
	TagModelPixelScale: "ModelPixelScaleTag",
	TagModelTiepoint:   "ModelTiepointTag",
	TagModelTransform:  "ModelTransformationTag",
}

func TagName(t uint16) string {
	if n, ok := tagNames[t]; ok {
		return n
	}
	return "Tag" + itoa(uint64(t))
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

// pointerTags tag values that are byte offsets into the file.
var pointerTags = map[uint16]bool{
	TagStripOffsets: true, TagTileOffsets: true, TagSubIFDs: true,
}
