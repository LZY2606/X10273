package tiff_test

import (
	"testing"

	"tiffgraph/internal/tiff"
	"tiffgraph/internal/tiff/tiffbuild"
)

func hasEvent(r *tiff.Result, kind string) bool {
	for _, e := range r.Events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func blockWithStatus(r *tiff.Result, status string) int {
	n := 0
	for _, b := range r.Blocks {
		if b.Status == status {
			n++
		}
	}
	return n
}

func TestBaselinePreservesRawFacts(t *testing.T) {
	s := tiffbuild.BaselineLE()
	r := tiff.Parse(s.Data, tiff.Options{Budget: 64})
	if r.ByteOrder != tiff.LittleEndian {
		t.Fatalf("byte order = %q", r.ByteOrder)
	}
	if r.Variant != tiff.Classic {
		t.Fatalf("variant = %q", r.Variant)
	}
	if len(r.IFDs) != 1 {
		t.Fatalf("ifds = %d", len(r.IFDs))
	}
	// tag 原值与外部值偏移保留
	var tiepoint *tiff.Tag
	for i := range r.IFDs[0].Tags {
		tg := &r.IFDs[0].Tags[i]
		if tg.ID == 33922 {
			tiepoint = tg
		}
		if tg.Inline && tg.ValueRange[0] != tg.EntryRange[0]+8 {
			t.Fatalf("inline tag %d value range not inside entry", tg.ID)
		}
	}
	if tiepoint == nil {
		t.Fatal("ModelTiepoint missing")
	}
	if tiepoint.Inline {
		t.Fatal("tiepoint should be external")
	}
	if tiepoint.ValueField != tiepoint.ValueRange[0] {
		t.Fatalf("external value offset not preserved: field=%d range=%v",
			tiepoint.ValueField, tiepoint.ValueRange)
	}
	if len(tiepoint.Raw) != 48 {
		t.Fatalf("raw tiepoint bytes = %d", len(tiepoint.Raw))
	}
	// GeoKey 版本保留
	if r.Geo == nil || r.Geo.KeyDirectoryVersion != 1 || r.Geo.MinorRevision != 0 {
		t.Fatalf("geokey version not preserved: %+v", r.Geo)
	}
	// tile 范围
	if len(r.Blocks) != 4 || !r.Tiled {
		t.Fatalf("blocks = %d tiled=%v", len(r.Blocks), r.Tiled)
	}
	for _, b := range r.Blocks {
		if b.Status != "ok" {
			t.Fatalf("block %s status %q", b.Identity(), b.Status)
		}
	}
	// 候选: tiepoint+scale
	if len(r.Candidates) != 1 || r.Candidates[0].Kind != "tiepoint_scale" {
		t.Fatalf("candidates = %+v", r.Candidates)
	}
	// 片段哈希存在
	if len(r.Fragments) == 0 {
		t.Fatal("no fragments")
	}
}

func TestBigEndian(t *testing.T) {
	r := tiff.Parse(tiffbuild.BigEndianClassic().Data, tiff.Options{})
	if r.ByteOrder != tiff.BigEndian {
		t.Fatalf("byte order = %q", r.ByteOrder)
	}
	if !hasEvent(r, "header.byte_order") {
		t.Fatal("missing byte order event")
	}
	w := r.FindTagForTest(256)
	if w == nil || w.Uints[0] != 16 {
		t.Fatalf("width tag wrong: %+v", w)
	}
}

func TestBigTIFFWideOffsets(t *testing.T) {
	r := tiff.Parse(tiffbuild.BigTIFFWide().Data, tiff.Options{})
	if r.Variant != tiff.BigTIFF {
		t.Fatalf("variant = %q", r.Variant)
	}
	if !hasEvent(r, "header.bigtiff") {
		t.Fatal("missing bigtiff event")
	}
	if len(r.Blocks) != 4 {
		t.Fatalf("blocks = %d", len(r.Blocks))
	}
	if r.Blocks[0].Offset != 4096 {
		t.Fatalf("wide offset not preserved: %d", r.Blocks[0].Offset)
	}
}

func TestIFDLoop(t *testing.T) {
	r := tiff.Parse(tiffbuild.IFDLoop().Data, tiff.Options{})
	if !hasEvent(r, "ifd.loop") {
		t.Fatalf("loop not detected: %+v", r.Events)
	}
	// 循环不应导致无限解析
	if len(r.IFDs) != 2 {
		t.Fatalf("ifds = %d", len(r.IFDs))
	}
	if !r.IFDs[0].LoopBack {
		t.Fatal("loop-back flag not set on first IFD")
	}
}

func TestSharedSubdir(t *testing.T) {
	r := tiff.Parse(tiffbuild.SharedSubdir().Data, tiff.Options{})
	if !hasEvent(r, "ifd.shared_subdir") {
		t.Fatalf("shared subdir not detected: %+v", r.Events)
	}
	var sub *tiff.IFD
	for _, d := range r.IFDs {
		if d.Offset == 2048 {
			sub = d
		}
	}
	if sub == nil || !sub.Shared {
		t.Fatalf("subdir not marked shared: %+v", sub)
	}
	if len(sub.Via) != 2 {
		t.Fatalf("expected 2 arrival paths, got %v", sub.Via)
	}
}

func TestTileOverlap(t *testing.T) {
	r := tiff.Parse(tiffbuild.TileOverlap().Data, tiff.Options{})
	if !hasEvent(r, "tile.overlap") {
		t.Fatalf("overlap not detected: %+v", r.Events)
	}
	if n := blockWithStatus(r, "overlap"); n != 2 {
		t.Fatalf("overlap blocks = %d", n)
	}
	// 重叠块不被拼凑: 两个块都保留各自范围
	for _, b := range r.Blocks {
		if b.Status == "overlap" && b.ByteCount != 96 {
			t.Fatalf("block mutated: %+v", b)
		}
	}
}

func TestGeoKeyConflictKeepsCandidates(t *testing.T) {
	r := tiff.Parse(tiffbuild.GeoConflict().Data, tiff.Options{})
	if !hasEvent(r, "geokey.conflict") {
		t.Fatalf("conflict not detected: %+v", r.Events)
	}
	if len(r.Candidates) != 2 {
		t.Fatalf("candidates = %d", len(r.Candidates))
	}
	for _, c := range r.Candidates {
		if c.Status != "conflict" {
			t.Fatalf("candidate %s status %q", c.ID, c.Status)
		}
		if c.ID == "" {
			t.Fatal("candidate missing stable identity")
		}
	}
}

func TestBudgetExhaustion(t *testing.T) {
	s := tiffbuild.DeepChain(12)
	r := tiff.Parse(s.Data, tiff.Options{Budget: 5})
	if !r.Budget.Exhausted {
		t.Fatal("budget should be exhausted")
	}
	if !hasEvent(r, "budget.exhausted") {
		t.Fatal("missing budget event")
	}
	if r.Budget.Used != 5 {
		t.Fatalf("used = %d", r.Budget.Used)
	}
	unvisited := 0
	for _, d := range r.IFDs {
		if d.Status == "unvisited" {
			unvisited++
		}
	}
	if unvisited == 0 {
		t.Fatal("no unvisited IFDs recorded")
	}
	// 足够预算时完整解析
	r2 := tiff.Parse(s.Data, tiff.Options{Budget: 64})
	if r2.Budget.Exhausted || len(r2.IFDs) != 12 {
		t.Fatalf("full parse: exhausted=%v ifds=%d", r2.Budget.Exhausted, len(r2.IFDs))
	}
}

func TestMissingStripExplicit(t *testing.T) {
	r := tiff.Parse(tiffbuild.StripMissing().Data, tiff.Options{})
	if !hasEvent(r, "block.missing") {
		t.Fatal("missing strip not reported")
	}
	if n := blockWithStatus(r, "missing"); n != 1 {
		t.Fatalf("missing = %d", n)
	}
	if n := blockWithStatus(r, "ok"); n != 3 {
		t.Fatalf("ok = %d", n)
	}
}

func TestDeterministicParse(t *testing.T) {
	s := tiffbuild.BaselineLE()
	a := tiff.Parse(s.Data, tiff.Options{Budget: 32})
	b := tiff.Parse(s.Data, tiff.Options{Budget: 32})
	if a.ParamFP != b.ParamFP {
		t.Fatal("param fingerprint unstable")
	}
	if len(a.Fragments) != len(b.Fragments) {
		t.Fatal("fragment count unstable")
	}
	for i := range a.Fragments {
		if a.Fragments[i].SHA256 != b.Fragments[i].SHA256 {
			t.Fatal("fragment hash unstable")
		}
	}
	if a.Candidates[0].ID != b.Candidates[0].ID {
		t.Fatal("candidate id unstable")
	}
}

func TestGarbageInput(t *testing.T) {
	r := tiff.Parse([]byte("not a tiff at all"), tiff.Options{})
	if !hasEvent(r, "header.bad_byte_order") {
		t.Fatal("bad input should produce diagnostic event")
	}
	r2 := tiff.Parse([]byte("II"), tiff.Options{})
	if !hasEvent(r2, "header.too_short") {
		t.Fatal("short input should produce diagnostic event")
	}
}
