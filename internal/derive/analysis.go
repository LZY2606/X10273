package derive

import (
	"fmt"

	"ifdroom/internal/geo"
	"ifdroom/internal/tiff"
)

type TagView struct {
	ID          string        `json:"id"`
	IFD         int64         `json:"ifdOffset"`
	Tag         uint16        `json:"tag"`
	Name        string        `json:"name"`
	Type        tiff.DataType `json:"type"`
	TypeValid   bool          `json:"typeValid"`
	Count       uint64        `json:"count"`
	SlotOffset  int64         `json:"slotOffset"`
	SlotEnd     int64         `json:"slotEnd"`
	External    bool          `json:"external"`
	ExtOffset   int64         `json:"extOffset"`
	OutOfBounds bool          `json:"outOfBounds"`
	Ascii       string        `json:"ascii,omitempty"`
	Uints       []uint64      `json:"uints,omitempty"`
	Doubles     []float64     `json:"doubles,omitempty"`
}

type IFDView struct {
	ID         string              `json:"id"`
	Offset     int64               `json:"offset"`
	Order      int                 `json:"order"`
	ByteStart  int64               `json:"byteStart"`
	ByteEnd    int64               `json:"byteEnd"`
	Tags       []TagView           `json:"tags"`
	KeyDir     *geo.KeyDirectory   `json:"keyDirectory,omitempty"`
	Transforms geo.TransformResult `json:"transforms"`
	BlockMap   *BlockMap           `json:"blockMap"`
}

type Analysis struct {
	ParseOK     bool              `json:"parseOk"`
	ParseError  string            `json:"parseError,omitempty"`
	File        *tiff.File        `json:"file"`
	IFDs        []IFDView         `json:"ifds"`
	Diagnostics []tiff.Diagnostic `json:"diagnostics"`
	Parameters  Params            `json:"parameters"`
}

var tagNames = map[uint16]string{
	tiff.TagImageWidth: "ImageWidth", tiff.TagImageLength: "ImageLength",
	tiff.TagBitsPerSample: "BitsPerSample", tiff.TagCompression: "Compression",
	tiff.TagPhotometric: "PhotometricInterpretation", tiff.TagStripOffsets: "StripOffsets",
	tiff.TagSamplesPerPixel: "SamplesPerPixel", tiff.TagRowsPerStrip: "RowsPerStrip",
	tiff.TagStripByteCounts: "StripByteCounts", tiff.TagTileWidth: "TileWidth",
	tiff.TagTileLength: "TileLength", tiff.TagTileOffsets: "TileOffsets",
	tiff.TagTileByteCounts: "TileByteCounts", tiff.TagSubIFDs: "SubIFDs",
	tiff.TagGeoKeyDir: "GeoKeyDirectory", tiff.TagGeoDoubleParams: "GeoDoubleParams",
	tiff.TagGeoAsciiParams: "GeoAsciiParams", tiff.TagModelPixelScale: "ModelPixelScaleTag",
	tiff.TagModelTiepoint: "ModelTiepointTag", tiff.TagModelTransform: "ModelTransformationTag",
}

func TagName(t uint16) string {
	if n, ok := tagNames[t]; ok {
		return n
	}
	return fmt.Sprintf("Tag%d", t)
}

// Analyze parses raw TIFF bytes under params and produces all per-IFD views.
// Header-level parse failure is returned with ParseOK=false; later issues
// live in diagnostics, never silently dropped.
func Analyze(data []byte, params Params) Analysis {
	a := Analysis{Parameters: params}
	f, err := tiff.Parse(data, tiff.Options{
		BudgetIfdVisit:  params.IfdBudget,
		BudgetValueRead: params.ValueBudget,
	})
	if err != nil {
		a.ParseError = err.Error()
		return a
	}
	a.ParseOK = true
	a.File = f
	a.Diagnostics = f.Diagnostics
	for _, ifd := range f.IFDs {
		v := IFDView{ID: fmt.Sprintf("ifd%d", ifd.Offset), Offset: ifd.Offset,
			Order: ifd.Order, ByteStart: ifd.ByteStart, ByteEnd: ifd.ByteEnd}
		for _, e := range ifd.Entries {
			v.Tags = append(v.Tags, TagView{
				ID: fmt.Sprintf("ifd%d-tag%d", ifd.Offset, e.Tag), IFD: ifd.Offset,
				Tag: e.Tag, Name: TagName(e.Tag), Type: e.Type, TypeValid: e.TypeValid,
				Count: e.Count, SlotOffset: e.SlotOffset, SlotEnd: e.SlotEnd,
				External: e.External, ExtOffset: e.ExtOffset, OutOfBounds: e.OutOfBounds,
				Ascii: e.Ascii, Uints: e.Uints, Doubles: e.Doubles,
			})
		}
		if kd := geo.ParseKeyDir(ifd); kd != nil {
			v.KeyDir = kd
		}
		v.Transforms = geo.DeriveTransforms(ifd, v.KeyDir)
		v.BlockMap = BuildBlockMap(ifd, f.Size)
		a.IFDs = append(a.IFDs, v)
	}
	return a
}
