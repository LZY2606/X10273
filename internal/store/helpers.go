package store

import (
	"encoding/json"
	"strconv"
	"strings"
)

func decodePayload(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

// targetSeq extracts the seq from an event reference used in undo payloads.
// Decisions store the undone event seq as a string in Undoes.
func targetSeq(ref string) int64 {
	ref = strings.TrimSpace(ref)
	n, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return -1
	}
	return n
}
