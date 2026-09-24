package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	return sha256Hex(b)[:16]
}

// EventID is deterministic from (project, seq) so an exported and re-imported
// archive keeps identical event identities and order.
func EventID(projectID string, seq int64) string {
	return fmt.Sprintf("evt-%s-%04d", projectID, seq)
}

func VersionID(projectID, docID string, version int) string {
	return fmt.Sprintf("ver-%s-%s-v%d", projectID, docID, version)
}

func TrialID(projectID, code string, seq int64) string {
	return fmt.Sprintf("trial-%s-%s-%04d", projectID, code, seq)
}

func DecisionID(projectID, targetID string, seq int64) string {
	return fmt.Sprintf("dec-%s-%s-%04d", projectID, shortHash(targetID), seq)
}

func shortHash(s string) string { return sha256Hex([]byte(s))[:10] }

// ObjectIDs returns the stable, addressable objects produced by an analysis,
// used to diff observed impact of a mutation. Order is deterministic.
func ObjectIDs(a AnalysisView) []string {
	var ids []string
	if !a.ParseOK {
		return []string{"file:header"}
	}
	ids = append(ids, "file:graph", "file:diagnostics", "file:budget")
	for _, ifd := range a.IFDs {
		ids = append(ids, ifd.ID, ifd.ID+":tags", ifd.ID+":blocks", ifd.ID+":transforms")
		for _, t := range ifd.Tags {
			ids = append(ids, t.ID)
		}
		if ifd.BlockMap != nil {
			for _, b := range ifd.BlockMap.Blocks {
				ids = append(ids, b.ID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}
