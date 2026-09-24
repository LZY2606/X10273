package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// hashID derives a stable, content-based identifier. JSON canonicalization
// uses sorted struct fields (encoding/json is deterministic for structs).
func hashID(prefix string, parts ...any) string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	enc.SetEscapeHTML(false)
	for _, p := range parts {
		if err := enc.Encode(p); err != nil {
			panic(err)
		}
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// SourceID identifies an immutable submitted byte sequence.
func SourceID(projectID string, rev int, data []byte) string {
	return hashID("src", projectID, rev, sha256.Sum256(data))
}

// RevisionID identifies a submitted input revision (bytes + parent).
func RevisionID(projectID string, rev int, data []byte, parentID string) string {
	return hashID("rev", projectID, rev, sha256.Sum256(data), parentID)
}

// GraphID identifies the immutable parse output of a source under an
// algorithm version and parameter fingerprint.
func GraphID(sourceID, algoVersion, paramFP string) string {
	return hashID("graph", sourceID, algoVersion, paramFP)
}

// IFDNodeID derives node identity from source, offset and discovery index.
// Offsets can be shared (shared subdirectory), so index breaks ties while
// shared nodes remain grouped by offset via SharedID.
func IFDNodeID(sourceID string, index int, offset int64) string {
	return hashID("ifd", sourceID, index, offset)
}

// TagID is stable from owning IFD offset, tag code and ordinal.
func TagID(sourceID string, ifdOffset int64, tag uint16, ordinal int) string {
	return hashID("tag", sourceID, ifdOffset, tag, ordinal)
}

// BlockID is stable from kind and ordinal.
func BlockID(sourceID string, kind BlockKind, index int) string {
	return hashID("blk", sourceID, kind, index)
}

// GeoKeyID is stable from owning IFD offset and key code.
func GeoKeyID(sourceID string, ifdOffset int64, key uint16) string {
	return hashID("geokey", sourceID, ifdOffset, key)
}

// DerivedID identifies a derived fact from algorithm, parameters and direct
// sources. Same inputs + same algorithm version + same parameters => same id.
func DerivedID(kind, algoVersion, paramFP string, directSources []string) string {
	src := append([]string(nil), directSources...)
	sort.Strings(src)
	return hashID(kind, algoVersion, paramFP, src)
}

// EventID is the content identity of an append-only history event.
func EventID(projectID string, seq int64, eventType string, payload []byte, parentID string) string {
	return hashID("evt", projectID, seq, eventType, payload, parentID)
}

// DecisionID identifies a human decision.
func DecisionID(projectID string, seq int64, payload []byte) string {
	return hashID("dec", projectID, seq, payload)
}

// ProjectID derives a project identity from its immutable name.
func ProjectID(name string) string { return hashID("proj", name) }

// Short returns the last 12 hex chars for compact display.
func Short(id string) string {
	if len(id) > 12 {
		return id[len(id)-12:]
	}
	return id
}
