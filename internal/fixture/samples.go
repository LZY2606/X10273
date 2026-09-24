package fixture

// Sample names used by the offline case pack.
const (
	SampleBaseline          = "baseline_le_classic.tiff"
	SampleBigEndian         = "bigendian_mm.tiff"
	SampleBigTIFFWideOffset = "bigtiff_wide_offset.tiff"
	SampleIFDCycle          = "ifd_cycle.tiff"
	SampleSharedSubIFD      = "shared_subifd.tiff"
	SampleTileOverlap       = "tile_overlap.tiff"
	SampleGeoKeyConflict    = "geokey_conflict.tiff"
	SampleBudgetChain       = "budget_chain.tiff"
	SampleBadHeader         = "bad_header.tiff"
	SampleExternalOOB       = "external_oob.tiff"
)

const (
	tagImageWidth      = 256
	tagImageLength     = 257
	tagBitsPerSample   = 258
	tagCompression     = 259
	tagStripOffsets    = 273
	tagSamplesPerPixel = 277
	tagRowsPerStrip    = 278
	tagStripByteCounts = 279
	tagTileWidth       = 322
	tagTileLength      = 323
	tagTileOffsets     = 324
	tagTileByteCounts  = 325
	tagSubIFDs         = 330
	tagModelPixelScale = 33550
	tagModelTiepoint   = 33922
	tagModelTransform  = 34264
	tagGeoKeyDir       = 34735
	tagGeoDoubleParams = 34736
)

// geoKeyHeader builds the 4 SHORT header followed by key quads.
func geoKeyDirectory(version, revision, minor uint16, keys ...uint16) Entry {
	vals := []uint64{uint64(version), uint64(revision), uint64(minor), uint64(len(keys) / 4)}
	for _, k := range keys {
		vals = append(vals, uint64(k))
	}
	return Short(tagGeoKeyDir, vals...)
}

// baseGeoEntries returns baseline geographic tags (tiepoint + scale and a
// ProjectedCSTypeGeoKey EPSG:32654 = WGS84 / UTM 54N).
func baseGeoEntries(withTransform bool) []Entry {
	es := []Entry{
		Double(tagModelPixelScale, 0.25, 0.25, 0),
		Double(tagModelTiepoint, 0, 0, 0, 500000, 4000000, 0),
		geoKeyDirectory(1, 1, 0,
			1024, 0, 1, 1, // GTModelType = ModelTypeProjected
			2048, 0, 1, 0, // GeographicType unset
			3072, 0, 1, 32654, // ProjectedType EPSG:32654
		),
	}
	if withTransform {
		es = append(es, Double(tagModelTransform,
			0.25, 0, 0, 500000,
			0, -0.25, 0, 4000002,
			0, 0, 0, 0,
			0, 0, 0, 1))
	}
	return es
}

func tileGeometryEntries() []Entry {
	return []Entry{
		Short(tagImageWidth, 32),
		Short(tagImageLength, 32),
		Short(tagBitsPerSample, 8),
		Short(tagCompression, 1),
		Short(tagSamplesPerPixel, 1),
		Long(tagTileWidth, 16),
		Long(tagTileLength, 16),
	}
}

// tileBlockEntries references 4 tiles (16x16 each). dataStart is where
// pixel payload begins; per-tile size is tileBytes.
func tileBlockEntries(offsets, counts []uint64) []Entry {
	return []Entry{
		Long(tagTileOffsets, offsets...),
		Long(tagTileByteCounts, counts...),
	}
}
