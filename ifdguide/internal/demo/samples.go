package demo

import "encoding/binary"

const (
	TByte      = 1
	TASCII     = 2
	TShort     = 3
	TLong      = 4
	TDouble    = 12
	TLong8     = 16
	TIFD8      = 18
)

// Sample identity names used by the case pack and tests.
const (
	SampleBaselineLE   = "01-baseline-classic-le"
	SampleBaselineBE   = "02-baseline-classic-be"
	SampleBigTIFFWide  = "03-bigtiff-wide-offsets"
	SampleIFDCycle     = "04-ifd-cycle"
	SampleSharedSubIFD = "05-shared-subifd"
	SampleTileOverlap  = "06-tile-overlap-missing"
	SampleGeoConflict  = "07-geokey-transform-conflict"
	SampleBudgetBurst  = "08-budget-exhaustion"
	SampleMalformed    = "09-malformed"
)

// geoKeyDir builds the GeoKeyDirectory SHORT sequence: header + 4 per key.
func geoKeyDir(version, rev, minor uint16, keys ...[4]uint16) []uint64 {
	out := []uint64{uint64(version), uint64(rev), uint64(minor), uint64(len(keys))}
	for _, k := range keys {
		out = append(out, uint64(k[0]), uint64(k[1]), uint64(k[2]), uint64(k[3]))
	}
	return out
}

// baselineIFD returns a clean tiled 32x16, 16x16 tiles IFD (4 tiles) with
// a projected CRS and tiepoint+scale georeferencing.
func baselineIFD(name string, fill byte, conflict bool) IFDSpec {
	entries := []EntrySpec{
		{Tag: 256, Type: TLong, Values: []uint64{32}},                // ImageWidth
		{Tag: 257, Type: TLong, Values: []uint64{16}},                // ImageLength
		{Tag: 258, Type: TShort, Values: []uint64{8}},                // BitsPerSample
		{Tag: 259, Type: TShort, Values: []uint64{1}},                // Compression none
		{Tag: 262, Type: TShort, Values: []uint64{1}},                // Photometric
		{Tag: 277, Type: TShort, Values: []uint64{1}},                // SamplesPerPixel
		{Tag: 322, Type: TLong, Values: []uint64{16}},                // TileWidth
		{Tag: 323, Type: TLong, Values: []uint64{16}},                // TileLength
		{Tag: 324, Type: TLong, Ptrs: []Ptr{                          // TileOffsets
			{Block: name + "-t0", BlockSize: 256, Fill: fill},
			{Block: name + "-t1", BlockSize: 256, Fill: fill + 1},
			{Block: name + "-t2", BlockSize: 256, Fill: fill + 2},
			{Block: name + "-t3", BlockSize: 256, Fill: fill + 3},
		}},
		{Tag: 325, Type: TLong, Values: []uint64{256, 256, 256, 256}}, // TileByteCounts
		{Tag: 34735, Type: TShort, Wide: true, Values: geoKeyDir(1, 1, 0,
			[4]uint16{1024, 0, 1, 1},     // GTModelType = ModelTypeProjected
			[4]uint16{1025, 0, 1, 1},     // GTRasterType = PixelIsArea
			[4]uint16{3072, 0, 1, 32632}, // ProjectedCSType EPSG:32632
			[4]uint16{3076, 0, 1, 9001},  // ProjLinearUnits = metre
		)},
		{Tag: 33550, Type: TDouble, Floats: []float64{0.25, 0.25, 0}}, // ModelPixelScale
		{Tag: 33922, Type: TDouble, Floats: []float64{0, 0, 0, 500000, 5000000, 0}}, // tiepoint
	}
	if conflict {
		// A second, deliberately different affine candidate.
		entries = append(entries, EntrySpec{Tag: 34264, Type: TDouble, Floats: []float64{
			0.3, 0, 0, 500010,
			0, -0.3, 0, 4999990,
			0, 0, 1, 0,
			0, 0, 0, 1,
		}})
	}
	return IFDSpec{Name: name, Entries: entries}
}

// Samples returns the full offline sample set as buildable specs plus a
// flag for the "malformed bytes" sample (built raw, not via layout).
func Samples() map[string]Spec {
	le := baselineIFD("ifd0", 0x10, false)
	be := baselineIFD("ifd0", 0x20, false)
	bigIFD := baselineIFD("ifd0", 0x30, false)
	// Demonstrate a wide external pointer value (8-byte LONG8 geometry).
	bigIFD.Entries = append([]EntrySpec{
		{Tag: 254, Type: TLong8, Values: []uint64{0}}, // NewSubfileType
	}, bigIFD.Entries...)

	out := map[string]Spec{}
	out[SampleBaselineLE] = Spec{Name: SampleBaselineLE, Endian: binary.LittleEndian, IFDs: []IFDSpec{le}}
	beSpec := Spec{Name: SampleBaselineBE, Endian: binary.BigEndian, IFDs: []IFDSpec{be}}
	out[SampleBaselineBE] = beSpec
	out[SampleBigTIFFWide] = Spec{Name: SampleBigTIFFWide, BigTIFF: true,
		Endian: binary.LittleEndian, IFDs: []IFDSpec{bigIFD}}

	// IFD cycle: A -> B -> A via NextIFD.
	cycleA := IFDSpec{Name: "a", Next: "b", Entries: []EntrySpec{
		{Tag: 256, Type: TShort, Values: []uint64{8}},
		{Tag: 257, Type: TShort, Values: []uint64{8}},
	}}
	cycleB := IFDSpec{Name: "b", Next: "a", Entries: []EntrySpec{
		{Tag: 256, Type: TShort, Values: []uint64{4}},
		{Tag: 257, Type: TShort, Values: []uint64{4}},
	}}
	out[SampleIFDCycle] = Spec{Name: SampleIFDCycle, Endian: binary.LittleEndian, IFDs: []IFDSpec{cycleA, cycleB}}

	// Shared subdirectory: root lists the same child twice via SubIFDs and
	// also reaches it through a next-level IFD.
	sharedChild := baselineIFD("child", 0x40, false)
	root := IFDSpec{Name: "root", SubIFDs: []string{"child", "child"}, Entries: []EntrySpec{
		{Tag: 256, Type: TLong, Values: []uint64{32}},
		{Tag: 257, Type: TLong, Values: []uint64{16}},
		{Tag: 330, Type: TLong, Values: []uint64{0, 0}, Wide: true},
	}}
	mid := IFDSpec{Name: "mid", SubIFDs: []string{"child"}, Entries: []EntrySpec{
		{Tag: 330, Type: TLong, Values: []uint64{0}, Wide: true},
	}}
	root.Next = "mid"
	out[SampleSharedSubIFD] = Spec{Name: SampleSharedSubIFD, Endian: binary.LittleEndian,
		IFDs: []IFDSpec{root, mid, sharedChild}}

	// Tile overlap + missing state + an out-of-bounds wide pointer.
	oob := uint64(0x0000_00FF_FFFF_FFFF)
	overlap := IFDSpec{Name: "ifd0", Entries: []EntrySpec{
		{Tag: 256, Type: TLong, Values: []uint64{32}},
		{Tag: 257, Type: TLong, Values: []uint64{32}},
		{Tag: 322, Type: TLong, Values: []uint64{16}},
		{Tag: 323, Type: TLong, Values: []uint64{16}},
		{Tag: 324, Type: TLong, Ptrs: []Ptr{
			{Block: "ov0", BlockSize: 300, Fill: 0x51}, // overlaps next block
			{Block: "ov1", BlockSize: 200, Fill: 0x52},
			{Block: "ov2", BlockSize: 256, Fill: 0x53},
			{Raw: &oob}, // 4th tile offset points past EOF
		}},
		// only 3 byte counts: 4th tile is explicitly Missing
		{Tag: 325, Type: TLong, Values: []uint64{300, 200, 256}},
	}}
	out[SampleTileOverlap] = Spec{Name: SampleTileOverlap, Endian: binary.LittleEndian, IFDs: []IFDSpec{overlap}}

	// GeoKey + transform conflict: two legal model transforms, plus a
	// disagreeing GeoKey revision header retained verbatim.
	conf := baselineIFD("ifd0", 0x60, true)
	for i := range conf.Entries {
		if conf.Entries[i].Tag == 34735 {
			conf.Entries[i].Values = geoKeyDir(1, 2, 4, // GeoTIFF 1.2.4 header preserved
				[4]uint16{1024, 0, 1, 1},
				[4]uint16{1025, 0, 1, 1},
				[4]uint16{3072, 0, 1, 32632},
				[4]uint16{3076, 0, 1, 9001},
			)
		}
	}
	out[SampleGeoConflict] = Spec{Name: SampleGeoConflict, Endian: binary.LittleEndian, IFDs: []IFDSpec{conf}}

	// Budget exhaustion: a chain of 80 next-linked IFDs exceeds default 64.
	budgetIFDs := make([]IFDSpec, 0, 80)
	for i := 0; i < 80; i++ {
		d := IFDSpec{Name: ifdName(i), Entries: []EntrySpec{
			{Tag: 256, Type: TShort, Values: []uint64{2}},
			{Tag: 257, Type: TShort, Values: []uint64{2}},
		}}
		if i < 79 {
			d.Next = ifdName(i + 1)
		}
		budgetIFDs = append(budgetIFDs, d)
	}
	out[SampleBudgetBurst] = Spec{Name: SampleBudgetBurst, Endian: binary.LittleEndian, IFDs: budgetIFDs}

	return out
}

func ifdName(i int) string {
	return "ifd-" + string(rune('a'+i/26)) + string(rune('a'+i%26))
}

// MalformedBytes returns raw invalid inputs for the explicit-format-errors
// import path: not TIFF, truncated classic, bad magic, truncated BigTIFF.
func MalformedBytes() map[string][]byte {
	return map[string][]byte{
		"not-tiff":        []byte("This is definitely not a TIFF file."),
		"classic-truncated": []byte{'I', 'I', 42, 0, 0x10, 0, 0, 0}, // IFD offset past EOF
		"bad-magic":       {'M', 'M', 0x01, 0x00, 0, 0, 0, 8},
		"bigtiff-truncated": append([]byte{'I', 'I', 43, 0}, make([]byte, 6)...),
	}
}
