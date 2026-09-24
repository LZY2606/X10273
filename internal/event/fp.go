package event

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func fingerprint(parts ...interface{}) string {
	h := sha256.New()
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			h.Write([]byte(v))
		case int:
			h.Write([]byte(strconv.Itoa(v)))
		case int64:
			h.Write([]byte(strconv.FormatInt(v, 10)))
		}
		h.Write([]byte{0})
	}
	return "fp-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// HashID returns a stable short identity derived from the given material.
func HashID(prefix string, parts ...interface{}) string {
	return prefix + "-" + fingerprint(parts...)[3:]
}
