package fixture

import "fmt"

// Samples builds every domain sample by name.
func Samples(name string) ([]byte, error) {
	switch name {
	case SampleBaseline:
		return baseline(LE, false, 0x200)
	case SampleBigEndian:
		return baseline(BE, false, 0x200)
	case SampleBigTIFFWideOffset:
		return bigTIFFWide()
	case SampleIFDCycle:
		return ifdCycle()
	case SampleSharedSubIFD:
		return sharedSubIFD()
	case SampleTileOverlap:
		return tileOverlap()
	case SampleGeoKeyConflict:
		return geoKeyConflict()
	case SampleBudgetChain:
		return budgetChain(40)
	case SampleBadHeader:
		return []byte("XX\x00\x2a\x00\x00\x00\x08"), nil
	case SampleExternalOOB:
		return externalOOB()
	}
	return nil, fmt.Errorf("unknown sample %q", name)
}

// AllSampleNames lists every sample in stable order.
func AllSampleNames() []string {
	return []string{
		SampleBaseline, SampleBigEndian, SampleBigTIFFWideOffset,
		SampleIFDCycle, SampleSharedSubIFD, SampleTileOverlap,
		SampleGeoKeyConflict, SampleBudgetChain, SampleBadHeader, SampleExternalOOB,
	}
}

func tileAndGeo(dataStart int64, geoWithTransform bool) []Entry {
	es := tileGeometryEntries()
	es = append(es, Long(tagTileOffsets,
		uint64(dataStart), uint64(dataStart+128),
		uint64(dataStart+256), uint64(dataStart+384)))
	es = append(es, Long(tagTileByteCounts, 128, 128, 128, 128))
	es = append(es, baseGeoEntries(geoWithTransform)...)
	return es
}

func baseline(bo BO, big bool, dataStart int64) ([]byte, error) {
	b := NewBuilder(bo, big).Add(Node{ID: "ifd0", Entries: tileAndGeo(dataStart, false)})
	raw, err := b.Build()
	if err != nil {
		return nil, err
	}
	return padTo(raw, dataStart+512), nil
}

func bigTIFFWide() ([]byte, error) {
	es := tileGeometryEntries()
	// The external LONG8 TileOffsets array holds offsets above 2^32. In
	// BigTIFF these are legal 64-bit pointers; here the payloads are absent,
	// which must surface as explicit out-of-bounds blocks rather than
	// invented pixels. The 64-bit decode path is what is under test.
	es = append(es, Long8(tagTileOffsets,
		0x1_0000_1000, 0x1_0000_2000, 0x1_0000_3000, 0x1_0000_4000))
	es = append(es, Long8(tagTileByteCounts, 128, 128, 128, 128))
	es = append(es, baseGeoEntries(false)...)
	b := NewBuilder(LE, true).Add(Node{ID: "ifd0", Entries: es})
	return b.Build()
}

func ifdCycle() ([]byte, error) {
	entries := tileAndGeo(2000, false)
	entries = append(entries, Pointer(tagSubIFDs, "subA"))
	b := NewBuilder(LE, false).
		Add(Node{ID: "ifd0", Entries: entries}).
		Add(Node{ID: "subA", Entries: []Entry{Short(tagImageWidth, 16), Pointer(tagSubIFDs, "subB")}}).
		Add(Node{ID: "subB", Entries: []Entry{Short(tagImageLength, 16), Pointer(tagSubIFDs, "ifd0")}})
	raw, err := b.Build()
	if err != nil {
		return nil, err
	}
	return padTo(raw, 2600), nil
}

func sharedSubIFD() ([]byte, error) {
	base := append(tileGeometryEntries(),
		Long(tagTileOffsets, 300, 500, 700, 900))
	base = append(base, Long(tagTileByteCounts, 128, 128, 128, 128))
	shared := []Entry{Short(tagImageWidth, 8), Short(tagImageLength, 8)}
	b := NewBuilder(BE, false).
		Add(Node{ID: "ifd0", Entries: append(base, Pointer(tagSubIFDs, "shared")), Next: "ifd1"}).
		Add(Node{ID: "ifd1", Entries: []Entry{Short(tagImageWidth, 24), Pointer(tagSubIFDs, "shared")}}).
		Add(Node{ID: "shared", Entries: shared})
	raw, err := b.Build()
	if err != nil {
		return nil, err
	}
	return padTo(raw, 1100), nil
}

func tileOverlap() ([]byte, error) {
	es := tileGeometryEntries()
	es = append(es, Long(tagTileOffsets, 256, 384, 448, 700))
	es = append(es, Long(tagTileByteCounts, 128, 128, 128, 128))
	es = append(es, baseGeoEntries(false)...)
	b := NewBuilder(LE, false).Add(Node{ID: "ifd0", Entries: es})
	raw, err := b.Build()
	if err != nil {
		return nil, err
	}
	return padTo(raw, 900), nil
}

func geoKeyConflict() ([]byte, error) {
	es := tileGeometryEntries()
	es = append(es, Long(tagTileOffsets, 256, 400, 544, 688))
	es = append(es, Long(tagTileByteCounts, 128, 128, 128, 128))
	es = append(es, baseGeoEntries(true)...)
	b := NewBuilder(LE, false).Add(Node{ID: "ifd0", Entries: es})
	raw, err := b.Build()
	if err != nil {
		return nil, err
	}
	return padTo(raw, 900), nil
}

func budgetChain(n int) ([]byte, error) {
	b := NewBuilder(LE, false)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("lvl%02d", i)
		var es []Entry
		if i+1 < n {
			es = append(es, Pointer(tagSubIFDs, fmt.Sprintf("lvl%02d", i+1)))
		}
		if i == 0 {
			es = append(es, tileGeometryEntries()...)
			es = append(es, Long(tagTileOffsets, 2000))
			es = append(es, Long(tagTileByteCounts, 64))
		}
		b.Add(Node{ID: id, Entries: es})
	}
	raw, err := b.Build()
	if err != nil {
		return nil, err
	}
	return padTo(raw, 2100), nil
}

func externalOOB() ([]byte, error) {
	bad := geoKeyDirectory(1, 1, 0, 1024, 0, 1, 1)
	bad.ExternalForced = 0x4000
	es := tileGeometryEntries()
	es = append(es, Long(tagTileOffsets, 256))
	es = append(es, Long(tagTileByteCounts, 128))
	es = append(es, baseGeoEntries(false)...)
	es = append(es, bad)
	b := NewBuilder(LE, false).Add(Node{ID: "ifd0", Entries: es})
	return b.Build()
}

func padTo(b []byte, size int64) []byte {
	if int64(len(b)) >= size {
		return b
	}
	out := make([]byte, size)
	copy(out, b)
	return out
}
