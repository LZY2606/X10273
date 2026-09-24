package tiff

import (
	"bytes"
	"testing"
)

func u64(v uint64) *uint64 { return &v }

func diagCodes(f *File) map[string]int {
	m := map[string]int{}
	for _, d := range f.Diagnostics {
		m[d.Code]++
	}
	return m
}

func TestParseClassicLittleEndianPreservesRaw(t *testing.T) {
	spec := &FileSpec{
		Endian: LittleEndian,
		IFDs: []*IFDSpec{{
			Next: -1,
			Tags: []TagSpec{
				{Code: TagImageWidth, Values: []uint64{64}},
				{Code: TagImageLength, Values: []uint64{32}},
				{Code: TagStripOffsets, Values: []uint64{900}, ForceExternal: false},
				{Code: 270, Text: "baseline"}, // ImageDescription, external (9 bytes > 4)
			},
		}},
		FileSize: 1024,
	}
	data := Build(spec)
	f := Parse(data, DefaultBudget())
	if f.Endianness != LittleEndian || f.Variant != Classic {
		t.Fatalf("header = %v/%v", f.Endianness, f.Variant)
	}
	if len(f.IFDs) != 1 {
		t.Fatalf("ifds = %d", len(f.IFDs))
	}
	ifd := f.IFDs[0]
	w := ifd.TagByCode(TagImageWidth)
	if w == nil || len(w.Values) != 1 || w.Values[0] != 64 {
		t.Fatalf("width tag = %+v", w)
	}
	if len(w.RawField) != 4 {
		t.Fatalf("classic raw field len = %d", len(w.RawField))
	}
	desc := ifd.TagByCode(270)
	if !desc.External || desc.Text != "baseline" {
		t.Fatalf("desc = %+v", desc)
	}
	if desc.ValueOffset == 0 || desc.ByteRange.Len() != 9 {
		t.Fatalf("external range = %+v", desc)
	}
	// Raw bytes at the recorded range must read back the original text.
	got := data[desc.ByteRange.Start:desc.ByteRange.End]
	if !bytes.Contains(got, []byte("baseline")) {
		t.Fatalf("readback = %q", got)
	}
	if len(f.Diagnostics) != 0 {
		t.Fatalf("diags = %+v", f.Diagnostics)
	}
}

func TestParseBigTIFFBigEndianWideOffsets(t *testing.T) {
	spec := &FileSpec{
		Endian: BigEndian,
		Big:    true,
		IFDs: []*IFDSpec{{
			Next: -1,
			Tags: []TagSpec{
				{Code: TagImageWidth, Type: 16, Values: []uint64{1 << 20}}, // LONG8 inline
				{Code: TagImageLength, Values: []uint64{8}},
			},
		}},
	}
	data := Build(spec)
	f := Parse(data, DefaultBudget())
	if f.Variant != BigTIFF || f.Endianness != BigEndian {
		t.Fatalf("header = %v/%v", f.Variant, f.Endianness)
	}
	w := f.IFDs[0].TagByCode(TagImageWidth)
	if len(w.RawField) != 8 || w.Values[0] != 1<<20 {
		t.Fatalf("wide field = %+v", w)
	}
}

func TestIFDCycleDetectedAndTerminates(t *testing.T) {
	spec := &FileSpec{
		Endian: LittleEndian,
		IFDs: []*IFDSpec{
			{Next: 1, Tags: []TagSpec{{Code: TagImageWidth, Values: []uint64{1}}}},
			{Next: 0, Tags: []TagSpec{{Code: TagImageWidth, Values: []uint64{2}}}},
		},
	}
	f := Parse(Build(spec), DefaultBudget())
	if len(f.IFDs) != 2 {
		t.Fatalf("ifds = %d (cycle must terminate)", len(f.IFDs))
	}
	if !f.IFDs[1].NextCycle {
		t.Fatalf("cycle not flagged: %+v", f.IFDs[1])
	}
	if diagCodes(f)[DiagIFDCycle] != 1 {
		t.Fatalf("diags = %v", diagCodes(f))
	}
}

func TestSharedSubIFDReported(t *testing.T) {
	spec := &FileSpec{
		Endian: LittleEndian,
		IFDs: []*IFDSpec{
			{Next: 1, Tags: []TagSpec{{Code: TagSubIFDs, Refs: []int{2}}}},
			{Next: -1, Tags: []TagSpec{{Code: TagSubIFDs, Refs: []int{2}}}},
			{Next: -1, Tags: []TagSpec{{Code: TagImageWidth, Values: []uint64{7}}}},
		},
	}
	f := Parse(Build(spec), DefaultBudget())
	if len(f.IFDs) != 3 {
		t.Fatalf("ifds = %d", len(f.IFDs))
	}
	shared := f.IFDs[2]
	if len(shared.ParentIDs) != 2 {
		t.Fatalf("parents = %v", shared.ParentIDs)
	}
	if diagCodes(f)[DiagSharedSubIFD] != 1 {
		t.Fatalf("diags = %v", diagCodes(f))
	}
}

func TestBudgetExhaustion(t *testing.T) {
	var ifds []*IFDSpec
	for i := 0; i < 10; i++ {
		next := i + 1
		if i == 9 {
			next = -1
		}
		ifds = append(ifds, &IFDSpec{Next: next, Tags: []TagSpec{{Code: TagImageWidth, Values: []uint64{1}}}})
	}
	f := Parse(Build(&FileSpec{Endian: LittleEndian, IFDs: ifds}), Budget{MaxIFDs: 4, MaxTags: 100})
	if !f.Budget.Exhausted {
		t.Fatalf("budget not exhausted: %+v", f.Budget)
	}
	if len(f.IFDs) != 4 {
		t.Fatalf("ifds = %d", len(f.IFDs))
	}
	if diagCodes(f)[DiagBudgetExhausted] != 1 {
		t.Fatalf("diags = %v", diagCodes(f))
	}
}

func TestExternalValueOutOfBoundsEvidence(t *testing.T) {
	spec := &FileSpec{
		Endian: LittleEndian,
		IFDs: []*IFDSpec{{
			Next: -1,
			Tags: []TagSpec{
				{Code: TagImageWidth, Values: []uint64{1}},
				{Code: TagStripOffsets, Values: []uint64{10, 20, 30}, ExternalAt: u64(1 << 30)},
			},
		}},
		FileSize: 256,
	}
	f := Parse(Build(spec), DefaultBudget())
	codes := diagCodes(f)
	if codes[DiagValueOutOfBounds] != 1 {
		t.Fatalf("diags = %v", codes)
	}
	var d Diagnostic
	for _, x := range f.Diagnostics {
		if x.Code == DiagValueOutOfBounds {
			d = x
		}
	}
	if d.SubjectID == "" || d.Offset != 1<<30 {
		t.Fatalf("diag = %+v", d)
	}
	tag := f.IFDs[0].TagByCode(TagStripOffsets)
	if !tag.OutOfBounds || tag.ValueOffset != 1<<30 {
		t.Fatalf("tag = %+v", tag)
	}
}

func TestTileOverlapAndMissingExplicit(t *testing.T) {
	spec := &FileSpec{
		Endian: LittleEndian,
		IFDs: []*IFDSpec{{
			Next: -1,
			Tags: []TagSpec{
				{Code: TagImageWidth, Values: []uint64{64}},
				{Code: TagImageLength, Values: []uint64{64}},
				{Code: TagTileWidth, Values: []uint64{32}},
				{Code: TagTileLength, Values: []uint64{32}},
				// 4 tiles: tile0 [512,592), tile1 [560,640) overlaps tile0,
				// tile2 bytecount 0 (missing), tile3 ok.
				{Code: TagTileOffsets, Values: []uint64{512, 560, 700, 800}},
				{Code: TagTileByteCounts, Values: []uint64{80, 80, 0, 64}},
			},
		}},
		FileSize: 1024,
	}
	f := Parse(Build(spec), DefaultBudget())
	maps := BlockMaps(f)
	if len(maps) != 1 || len(maps[0].Blocks) != 4 {
		t.Fatalf("maps = %+v", maps)
	}
	b := maps[0].Blocks
	if b[0].Status != BlockOverlap || b[1].Status != BlockOverlap {
		t.Fatalf("overlap status = %s/%s", b[0].Status, b[1].Status)
	}
	if b[2].Status != BlockMissing {
		t.Fatalf("missing status = %s", b[2].Status)
	}
	if b[3].Status != BlockOK {
		t.Fatalf("ok status = %s", b[3].Status)
	}
	codes := diagCodes(f)
	if codes["BLOCK_OVERLAP"] != 1 || codes["BLOCK_MISSING"] != 1 {
		t.Fatalf("diags = %v", codes)
	}
}

func TestGeoKeyVersionPreserved(t *testing.T) {
	spec := &FileSpec{
		Endian: LittleEndian,
		IFDs: []*IFDSpec{{
			Next: -1,
			Tags: []TagSpec{
				{Code: TagGeoKeyDirectory, Values: []uint64{
					1, 1, 2, 2, // version 1.1.2, two keys
					1024, 0, 1, 2, // ModelType = geographic
					2048, 0, 1, 4326, // GeographicType = WGS84
				}},
			},
		}},
	}
	f := Parse(Build(spec), DefaultBudget())
	dir := ParseGeoKeys(f, f.IFDs[0])
	if dir == nil || dir.Version != 1 || dir.KeyRevision != 1 || dir.MinorRevision != 2 {
		t.Fatalf("dir = %+v", dir)
	}
	if len(dir.Keys) != 2 || dir.Keys[1].Values[0] != 4326 {
		t.Fatalf("keys = %+v", dir.Keys)
	}
}
