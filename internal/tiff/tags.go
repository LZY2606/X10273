package tiff

func tagName(tag uint16) string {
	names := map[uint16]string{
		254: "NewSubfileType", 256: "ImageWidth", 257: "ImageLength", 258: "BitsPerSample",
		259: "Compression", 262: "PhotometricInterpretation", 273: "StripOffsets",
		277: "SamplesPerPixel", 278: "RowsPerStrip", 279: "StripByteCounts",
		282: "XResolution", 283: "YResolution", 284: "PlanarConfiguration",
		322: "TileWidth", 323: "TileLength", 324: "TileOffsets", 325: "TileByteCounts",
		330: "SubIFDs", 339: "SampleFormat", 347: "JPEGTables",
		33550: "ModelPixelScaleTag", 33922: "ModelTiepointTag", 34264: "ModelTransformationTag",
		34735: "GeoKeyDirectoryTag", 34736: "GeoDoubleParamsTag", 34737: "GeoAsciiParamsTag",
	}
	if name, ok := names[tag]; ok {
		return name
	}
	return "UnknownTag"
}

func findTags(g *Graph, ifdID string, tag uint16) []Tag {
	var out []Tag
	for _, item := range g.Tags {
		if item.IFDID == ifdID && item.Tag == tag {
			out = append(out, item)
		}
	}
	return out
}

func oneTag(g *Graph, ifdID string, tag uint16) *Tag {
	items := findTags(g, ifdID, tag)
	if len(items) == 0 {
		return nil
	}
	return &items[0]
}

func tagInt(tag *Tag, index int, fallback int) int {
	if tag == nil || index >= len(tag.Raw.Values) {
		return fallback
	}
	v, err := parseUint64(tag.Raw.Values[index])
	if err != nil {
		return fallback
	}
	return int(v)
}
