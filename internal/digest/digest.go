// Package digest provides content-addressed stable identifiers.
//
// Identities are derived purely from canonical content, never from
// wall-clock time or insertion order, so that exporting a database and
// importing it elsewhere preserves every id.
package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Hash returns the hex SHA-256 of data.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Canonical serialises a value deterministically and returns its hex hash.
// Go's encoding/json sorts map keys, and callers use slices for ordered data,
// which is sufficient canonicalisation for this local tool.
func Canonical(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
