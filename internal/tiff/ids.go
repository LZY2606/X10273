package tiff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
)

func stableID(parts ...interface{}) string {
	key := ""
	for _, part := range parts {
		key += fmt.Sprintf("%v|", part)
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16]
}

func ifdID(doc string, offset int64) string {
	return fmt.Sprintf("%s-ifd-%d", doc, offset)
}

func tagIDFor(doc string, ifdOffset int64, tag uint16) string {
	return fmt.Sprintf("%s-tag-%d-%d", doc, ifdOffset, tag)
}

func edgeID(doc, kind string, from, to int64) string {
	return fmt.Sprintf("%s-edge-%s-%d-%d", doc, kind, from, to)
}

func blockID(doc string, ifdOffset int64, index int) string {
	return fmt.Sprintf("%s-block-%d-%d", doc, ifdOffset, index)
}

func geoKeyID(doc string, ifdOffset int64, key, duplicateIndex uint16) string {
	return fmt.Sprintf("%s-geokey-%d-%d-%d", doc, ifdOffset, key, duplicateIndex)
}

func transformID(doc string, ifdOffset int64, kind string, index int) string {
	return fmt.Sprintf("%s-transform-%d-%s-%d", doc, ifdOffset, kind, index)
}

func findingID(doc, kind, objectID string, r *ByteRange) string {
	if r == nil {
		return stableID(doc, kind, objectID)
	}
	return stableID(doc, kind, objectID, r.Offset, r.Length)
}

func integerValues(raw RawValue) []string { return raw.Values }

func parseSignedInt64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

func parseUint64(s string) (uint64, error) { return strconv.ParseUint(s, 10, 64) }

func parseFloat(s string) (float64, error) {
	if n, d, ok := strings_Cut(s, "/"); ok {
		a, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, err
		}
		b, err := strconv.ParseFloat(d, 64)
		if err != nil || b == 0 {
			return 0, fmt.Errorf("invalid rational")
		}
		return a / b, nil
	}
	return strconv.ParseFloat(s, 64)
}

func strings_Cut(s, sep string) (string, string, bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func (p *parser) buildGraph() *Graph {
	offsets := make([]int64, 0, len(p.ifds))
	for offset := range p.ifds {
		offsets = append(offsets, offset)
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	g := &Graph{DocumentID: p.id, FileLength: int64(len(p.data)), Header: p.header,
		Budget: p.budget, AlgorithmVersion: AlgorithmVersion}
	for _, offset := range offsets {
		g.IFDs = append(g.IFDs, *p.ifds[offset])
		tags := append([]Tag(nil), p.tags[offset]...)
		sort.Slice(tags, func(i, j int) bool {
			if tags[i].Tag == tags[j].Tag {
				return tags[i].ValueRange.Offset < tags[j].ValueRange.Offset
			}
			return tags[i].Tag < tags[j].Tag
		})
		g.Tags = append(g.Tags, tags...)
	}
	g.Edges = append(g.Edges, p.edges...)
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].SourceID == g.Edges[j].SourceID {
			return g.Edges[i].TargetID < g.Edges[j].TargetID
		}
		return g.Edges[i].SourceID < g.Edges[j].SourceID
	})
	g.Findings = append(g.Findings, p.findings...)
	sort.Slice(g.Findings, func(i, j int) bool {
		if g.Findings[i].Kind == g.Findings[j].Kind {
			return g.Findings[i].ObjectID < g.Findings[j].ObjectID
		}
		return g.Findings[i].Kind < g.Findings[j].Kind
	})
	return g
}
