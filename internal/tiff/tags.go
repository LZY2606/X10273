package tiff

// Standard TIFF / GeoTIFF tag identifiers used by the parser and block layout.
const (
	TagImageWidth      TagID = 256
	TagImageLength     TagID = 257
	TagBitsPerSample   TagID = 258
	TagCompression     TagID = 259
	TagPhotometric     TagID = 262
	TagStripOffsets    TagID = 273
	TagSamplesPerPixel TagID = 277
	TagRowsPerStrip    TagID = 278
	TagStripByteCounts TagID = 279
	TagXResolution     TagID = 282
	TagYResolution     TagID = 283
	TagPlanarConfig    TagID = 284
	TagTileWidth       TagID = 322
	TagTileLength      TagID = 323
	TagTileOffsets     TagID = 324
	TagTileByteCounts  TagID = 325
	TagSampleFormat    TagID = 339

	TagSubIFDs              TagID = 330
	TagGPSIFD               TagID = 34853
	TagExifIFD              TagID = 34665

	TagModelPixelScale     TagID = 33550
	TagModelTiepoint       TagID = 33922
	TagModelTransformation TagID = 34264
	TagGeoKeyDirectory     TagID = 34735
	TagGeoDoubleParams     TagID = 34736
	TagGeoAsciiParams      TagID = 34737
)

// TagName returns a readable label for known tags; unknown tags keep their id.
func TagName(id TagID) string {
	if n, ok := tagNames[id]; ok {
		return n
	}
	return ""
}

var tagNames = map[TagID]string{
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
	282: "XResolution",
	283: "YResolution",
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
	34665: "ExifIFD",
	34735: "GeoKeyDirectoryTag",
	34736: "GeoDoubleParamsTag",
	34737: "GeoAsciiParamsTag",
	34853: "GPSIFD",
}
