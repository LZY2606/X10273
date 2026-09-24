package tiff

// Type sizes for classic TIFF and BigTIFF value types.
var typeSizes = map[uint16]uint64{
	1:  1, // BYTE
	2:  1, // ASCII
	3:  2, // SHORT
	4:  4, // LONG
	5:  8, // RATIONAL (2x LONG)
	6:  1, // SBYTE
	7:  1, // UNDEFINED
	8:  2, // SSHORT
	9:  4, // SLONG
	10: 8, // SRATIONAL
	11: 4, // FLOAT
	12: 8, // DOUBLE
	13: 8, // IFD (classic)
	16: 8, // LONG8 (BigTIFF)
	17: 8, // SLONG8
	18: 8, // IFD8
}

var typeNames = map[uint16]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}

// Tag numbers used while building the graph and block map.
const (
	TagImageWidth      = 256
	TagImageLength     = 257
	TagBitsPerSample   = 258
	TagCompression     = 259
	TagStripOffsets    = 273
	TagSamplesPerPixel = 277
	TagRowsPerStrip    = 278
	TagStripByteCounts = 279
	TagTileWidth       = 322
	TagTileLength      = 323
	TagTileOffsets     = 324
	TagTileByteCounts  = 325
	TagSubIFDs         = 330
	TagGeoKeyDir       = 34735
	TagGeoDoubleParams = 34736
	TagGeoASCIIParams  = 34737
	TagModelPixelScale = 33550
	TagModelTiepoint   = 33922
	TagModelTransform  = 34264
	TagExifIFD         = 34665
	TagGPSIFD          = 34853
)
