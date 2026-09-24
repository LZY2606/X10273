// Package ids derives stable, content-addressed identities for events,
// inputs and derived results. Identities survive export/import because
// they are a pure function of the content they name.
package ids

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Of hashes the parts (in order, length-prefixed) into a stable id with
// the given prefix, e.g. "evt", "inp", "drv".
func Of(prefix string, parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		var lb [8]byte
		n := uint64(len(p))
		for i := 0; i < 8; i++ {
			lb[i] = byte(n >> (8 * i))
		}
		h.Write(lb[:])
		h.Write(p)
	}
	sum := h.Sum(nil)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(sum[:16]))
}

// OfStrings is a convenience wrapper over Of for string parts.
func OfStrings(prefix string, parts ...string) string {
	bs := make([][]byte, len(parts))
	for i, s := range parts {
		bs[i] = []byte(s)
	}
	return Of(prefix, bs...)
}
