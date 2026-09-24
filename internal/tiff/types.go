package tiff

// TIFF/BigTIFF field type registry. Sizes follow the BigTIFF proposal:
// types 13/16/17/18 are eight bytes in BigTIFF.
var typeSizesClassic = map[int]int{
	1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1,
	8: 2, 9: 4, 10: 8, 11: 4, 12: 8,
}
var typeSizesBig = map[int]int{
	1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1,
	8: 2, 9: 4, 10: 8, 11: 4, 12: 8,
	13: 8, 16: 8, 17: 8, 18: 8,
}

var typeNames = map[int]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}

// tagNames is the small domain registry the review UI relies on. Unknown tags
// stay numeric; nothing is dropped for presentation.
var tagNames = map[int]string{
	254: "NewSubfileType", 255: "SubfileType", 256: "ImageWidth",
	257: "ImageLength", 258: "BitsPerSample", 259: "Compression",
	262: "PhotometricInterpretation", 273: "StripOffsets",
	277: "SamplesPerPixel", 278: "RowsPerStrip", 279: "StripByteCounts",
	284: "PlanarConfiguration", 322: "TileWidth", 323: "TileLength",
	324: "TileOffsets", 325: "TileByteCounts", 330: "SubIFDs",
	339: "SampleFormat", 347: "JPEGTables",
	33550: "ModelPixelScaleTag", 33922: "ModelTiepointTag",
	34264: "ModelTransformationTag", 34735: "GeoKeyDirectoryTag",
	34736: "GeoDoubleParamsTag", 34737: "GeoAsciiParamsTag",
}

func tagName(id int) string {
	if n, ok := tagNames[id]; ok {
		return n
	}
	return ""
}
