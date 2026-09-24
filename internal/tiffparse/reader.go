package tiffparse

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type reader struct {
	data       []byte
	order      binary.ByteOrder
	format     Format
	offsetSize int
	budget     Budget
	visited    map[uint64]bool
	open       map[uint64]bool
	nodes      map[uint64]*IFDNode
	ifds       []IFDNode
	links      []IFDLink
	diags      []Diagnostic
	sourceID   string
}

type Options struct {
	SourceID      string
	NodeLimit     int
	ExternalLimit int
}

var typeSizes = map[uint16]uint64{
	1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2,
	9: 4, 10: 8, 11: 4, 12: 8, 13: 4, 16: 8, 17: 8, 18: 8,
}

func typeName(id uint16) string {
	names := map[uint16]string{
		1: "BYTE", 2: "ASCII", 3: "SHORT", 4: "LONG", 5: "RATIONAL",
		6: "SBYTE", 7: "UNDEFINED", 8: "SSHORT", 9: "SLONG", 10: "SRATIONAL",
		11: "FLOAT", 12: "DOUBLE", 13: "IFD", 16: "LONG8", 17: "SLONG8", 18: "IFD8",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return "UNKNOWN"
}

func tagName(id uint16) string {
	names := map[uint16]string{
		254: "NewSubfileType", 256: "ImageWidth", 257: "ImageLength",
		258: "BitsPerSample", 259: "Compression", 262: "PhotometricInterpretation",
		273: "StripOffsets", 277: "SamplesPerPixel", 278: "RowsPerStrip",
		279: "StripByteCounts", 322: "TileWidth", 323: "TileLength",
		324: "TileOffsets", 325: "TileByteCounts", 330: "SubIFDs",
		33550: "ModelPixelScaleTag", 33922: "ModelTiepointTag",
		34264: "ModelTransformationTag", 34735: "GeoKeyDirectoryTag",
		34736: "GeoDoubleParamsTag", 34737: "GeoAsciiParamsTag",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return "Tag" + fmt.Sprint(id)
}

func Parse(data []byte, options Options) (*Result, error) {
	if options.NodeLimit <= 0 {
		options.NodeLimit = 128
	}
	if options.ExternalLimit <= 0 {
		options.ExternalLimit = 4096
	}
	if len(data) < 4 {
		return nil, errors.New("tiff input must contain at least four header bytes")
	}
	var order binary.ByteOrder
	switch string(data[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("invalid byte order mark %02x%02x", data[0], data[1])
	}
	magic := order.Uint16(data[2:4])
	var format Format
	var offsetSize int
	var headerLength uint64
	var rootOffset uint64
	switch magic {
	case 42:
		format = ClassicTIFF
		offsetSize = 4
		headerLength = 8
		if uint64(len(data)) < headerLength {
			return nil, errors.New("truncated classic TIFF header")
		}
		rootOffset = uint64(order.Uint32(data[4:8]))
	case 43:
		format = BigTIFF
		offsetSize = 8
		headerLength = 16
		if uint64(len(data)) < headerLength {
			return nil, errors.New("truncated BigTIFF header")
		}
		if order.Uint16(data[4:6]) != 8 || order.Uint16(data[6:8]) != 0 {
			return nil, errors.New("invalid BigTIFF offsetsize or reserved field")
		}
		rootOffset = order.Uint64(data[8:16])
	default:
		return nil, fmt.Errorf("unsupported TIFF magic %d", magic)
	}
	r := &reader{
		data: data, order: order, format: format, offsetSize: offsetSize,
		visited: map[uint64]bool{}, open: map[uint64]bool{}, nodes: map[uint64]*IFDNode{},
		sourceID: options.SourceID,
		budget:   Budget{NodeLimit: options.NodeLimit, ExternalLimit: options.ExternalLimit},
	}
	if rootOffset == 0 {
		r.diag("empty_root_ifd", "warning", "root IFD offset is zero", 0, 0, "", 0, "")
	} else {
		r.discover("ifd:root", "root", 0, 0, rootOffset)
	}
	endian := LittleEndian
	if string(data[0:2]) == "MM" {
		endian = BigEndian
	}
	return &Result{
		SourceID: options.SourceID, Endian: endian, Format: format,
		HeaderBytes: ByteRange{0, headerLength}, RootOffset: rootOffset,
		IFDs: r.ifds, Links: r.links, Diagnostics: r.diags,
		Budget: r.budget, Complete: !r.budget.Exhausted,
	}, nil
}

func (r *reader) diag(code, severity, message string, offset, length uint64, ifdID string, tagID uint16, linkID string) {
	r.diags = append(r.diags, Diagnostic{Code: code, Severity: severity, Message: message, Offset: offset, Length: length, IFDID: ifdID, TagID: tagID, LinkID: linkID})
}

func (r *reader) slice(offset, length uint64, context string) ([]byte, bool) {
	if offset > uint64(len(r.data)) || length > uint64(len(r.data))-offset {
		r.diag("out_of_bounds_"+context, "error", fmt.Sprintf("byte range [%d,%d) is outside the submitted TIFF", offset, offset+length), offset, length, "", 0, "")
		return nil, false
	}
	return r.data[offset : offset+length], true
}

func (r *reader) readUint(offset uint64, size int) (uint64, bool) {
	b, ok := r.slice(offset, uint64(size), "scalar")
	if !ok {
		return 0, false
	}
	switch size {
	case 2:
		return uint64(r.order.Uint16(b)), true
	case 4:
		return uint64(r.order.Uint32(b)), true
	case 8:
		return r.order.Uint64(b), true
	}
	return 0, false
}

func nodeID(sourceID string, offset uint64) string {
	if sourceID == "" {
		return fmt.Sprintf("ifd@%d", offset)
	}
	return fmt.Sprintf("%s/ifd@%d", sourceID, offset)
}

func linkID(sourceID, from string, offset uint64, kind string, tagID uint16, index int) string {
	if tagID != 0 {
		return fmt.Sprintf("%s:%s->tag%d[%d]@%d", from, kind, tagID, index, offset)
	}
	return fmt.Sprintf("%s:%s@%d", from, kind, offset)
}
