package tiff

import "encoding/binary"

// Order is the byte order declared by a TIFF header.
type Order string

const (
	LittleEndian Order = "II"
	BigEndian    Order = "MM"
)

func (o Order) ByteOrder() binary.ByteOrder {
	if o == BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// DataType is a TIFF/BigTIFF field type. Values match the on-disk codes.
type DataType uint16

const (
	TypeByte      DataType = 1
	TypeASCII     DataType = 2
	TypeShort     DataType = 3
	TypeLong      DataType = 4
	TypeRational  DataType = 5
	TypeSByte     DataType = 6
	TypeUndefined DataType = 7
	TypeSShort    DataType = 8
	TypeSLong     DataType = 9
	TypeSRational DataType = 10
	TypeFloat     DataType = 11
	TypeDouble    DataType = 12
	TypeIFD       DataType = 13 // classic 4-byte IFD offset
	TypeLong8     DataType = 16 // BigTIFF 8-byte unsigned
	TypeSLong8    DataType = 17
	TypeIFD8      DataType = 18 // BigTIFF 8-byte IFD offset
)

func (t DataType) Size(big bool) int {
	switch t {
	case TypeByte, TypeASCII, TypeSByte, TypeUndefined:
		return 1
	case TypeShort, TypeSShort:
		return 2
	case TypeLong, TypeSLong, TypeIFD, TypeFloat:
		return 4
	case TypeRational, TypeSRational, TypeDouble, TypeLong8, TypeSLong8, TypeIFD8:
		return 8
	}
	return 0
}

// Well-known tags.
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
	TagSubIFDs         uint16 = 330
	TagGeoKeyDir       uint16 = 34735
	TagGeoDoubleParams uint16 = 34736
	TagGeoAsciiParams  uint16 = 34737
	TagModelPixelScale uint16 = 33550
	TagModelTiepoint   uint16 = 33922
	TagModelTransform  uint16 = 34264
)
