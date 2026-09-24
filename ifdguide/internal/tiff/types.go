// Package tiff is a dependency-free TIFF/GeoTIFF byte parser.
//
// The parser preserves raw evidence: byte order, classic/BigTIFF header,
// every tag's raw value (inline or external offset), the IFD link graph,
// tile/strip byte ranges, and the GeoKey directory. It never silently
// repairs a malformed document; every ambiguity is reported as a finding.
package tiff

// ByteOrder is the raw II/MM marker from the first two bytes.
type ByteOrder string

const (
	LittleEndian ByteOrder = "little-endian (II)"
	BigEndian    ByteOrder = "big-endian (MM)"
)

// TypeSize maps TIFF IFD type codes to their size in bytes (0 = unknown).
var TypeSize = [18]uint64{0, 1, 1, 2, 4, 8, 1, 1, 2, 4, 8, 4, 8, 4, 8, 8, 8, 8}

const (
	TypeByte      = 1
	TypeASCII     = 2
	TypeShort     = 3
	TypeLong      = 4
	TypeRational  = 5
	TypeSByte     = 6
	TypeUndefined = 7
	TypeSShort    = 8
	TypeSLong     = 9
	TypeSRational = 10
	TypeFloat     = 11
	TypeDouble    = 12
	TypeIFD       = 13
	TypeLong8     = 16
	TypeSLong8    = 17
	TypeIFD8      = 18
)

// Known TIFF/GeoTIFF tags used by the parser and the UI.
const (
	TagNewSubfileType    uint16 = 254
	TagImageWidth        uint16 = 256
	TagImageLength       uint16 = 257
	TagBitsPerSample     uint16 = 258
	TagCompression       uint16 = 259
	TagPhotometric       uint16 = 262
	TagStripOffsets      uint16 = 273
	TagSamplesPerPixel   uint16 = 277
	TagRowsPerStrip      uint16 = 278
	TagStripByteCounts   uint16 = 279
	TagXResolution       uint16 = 282
	TagYResolution       uint16 = 283
	TagPlanarConfig      uint16 = 284
	TagTileWidth         uint16 = 322
	TagTileLength        uint16 = 323
	TagTileOffsets       uint16 = 324
	TagTileByteCounts    uint16 = 325
	TagSubIFDs           uint16 = 330
	TagSampleFormat      uint16 = 339
	TagJPEGTables        uint16 = 347
	TagGeoKeyDirectory   uint16 = 34735
	TagGeoDoubleParams   uint16 = 34736
	TagGeoAsciiParams    uint16 = 34737
	TagModelPixelScale   uint16 = 33550
	TagModelTiepoint     uint16 = 33922
	TagModelTransformation = 34264
)

// TagName maps the subset of tags the case archive references by name.
var TagName = map[uint16]string{
	TagNewSubfileType:      "NewSubfileType",
	TagImageWidth:          "ImageWidth",
	TagImageLength:         "ImageLength",
	TagBitsPerSample:       "BitsPerSample",
	TagCompression:         "Compression",
	TagPhotometric:         "PhotometricInterpretation",
	TagStripOffsets:        "StripOffsets",
	TagSamplesPerPixel:    "SamplesPerPixel",
	TagRowsPerStrip:       "RowsPerStrip",
	TagStripByteCounts:    "StripByteCounts",
	TagXResolution:        "XResolution",
	TagYResolution:        "YResolution",
	TagPlanarConfig:       "PlanarConfiguration",
	TagTileWidth:          "TileWidth",
	TagTileLength:         "TileLength",
	TagTileOffsets:        "TileOffsets",
	TagTileByteCounts:     "TileByteCounts",
	TagSubIFDs:            "SubIFDs",
	TagSampleFormat:       "SampleFormat",
	TagJPEGTables:         "JPEGTables",
	TagGeoKeyDirectory:    "GeoKeyDirectoryTag",
	TagGeoDoubleParams:    "GeoDoubleParamsTag",
	TagGeoAsciiParams:     "GeoAsciiParamsTag",
	TagModelPixelScale:    "ModelPixelScaleTag",
	TagModelTiepoint:      "ModelTiepointTag",
	TagModelTransformation:"ModelTransformationTag",
}

// TypeName maps IFD type codes to their symbolic name.
var TypeName = map[uint16]string{
	1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
	6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
	11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
}
