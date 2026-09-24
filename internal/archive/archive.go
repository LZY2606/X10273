// Package archive serializes the whole project (events, sources,
// derived rows, decisions, params) into a portable JSON document and
// restores it into a fresh database. Stable IDs and sequence numbers
// are preserved verbatim, so identity, order and provenance survive a
// delete-and-reimport round trip.
package archive

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"ifdmap/internal/store"
)

// Format identifies the archive document.
const Format = "ifdmap-archive"
const Version = 1

// Archive is the exported project snapshot.
type Archive struct {
	Format     string            `json:"format"`
	Version    int               `json:"version"`
	ExportedAt time.Time         `json:"exportedAt"`
	Events     []*store.Event    `json:"events"`
	Sources    []Source          `json:"sources"`
	Derived    []*store.Derived  `json:"derived"`
	Decisions  []*store.Decision `json:"decisions"`
	KV         map[string]string `json:"kv"`
}

// Source mirrors store.Source with portable byte encoding.
type Source struct {
	ID         string `json:"id"`
	Version    int    `json:"version"`
	ParentID   string `json:"parentId,omitempty"`
	Name       string `json:"name"`
	Sha256     string `json:"sha256"`
	BytesB64   string `json:"bytesB64"`
	CreatedSeq int64  `json:"createdSeq"`
	Status     string `json:"status"`
}

// Export dumps the full project state.
func Export(st *store.Store, at time.Time) (*Archive, error) {
	a := &Archive{Format: Format, Version: Version, ExportedAt: at.UTC(), KV: map[string]string{}}
	var err error
	if a.Events, err = st.Events(0); err != nil {
		return nil, err
	}
	srcs, err := st.Sources(false)
	if err != nil {
		return nil, err
	}
	for _, s := range srcs {
		a.Sources = append(a.Sources, Source{
			ID: s.ID, Version: s.Version, ParentID: s.ParentID, Name: s.Name,
			Sha256: s.Sha256, BytesB64: base64.StdEncoding.EncodeToString(s.Bytes),
			CreatedSeq: s.CreatedSeq, Status: s.Status,
		})
	}
	if a.Derived, err = st.DerivedAll(); err != nil {
		return nil, err
	}
	if a.Decisions, err = st.DecisionsAll(); err != nil {
		return nil, err
	}
	if v, err := st.GetKV("params"); err == nil && v != "" {
		a.KV["params"] = v
	}
	return a, nil
}

// Marshal renders the archive as indented JSON.
func (a *Archive) Marshal() ([]byte, error) { return json.MarshalIndent(a, "", "  ") }

// Import restores an archive into a (possibly fresh) store. Inserts are
// idempotent: existing IDs are kept, never overwritten.
func Import(st *store.Store, data []byte) (*Archive, error) {
	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	if a.Format != Format {
		return nil, fmt.Errorf("archive: unsupported format %q", a.Format)
	}
	if a.Version != Version {
		return nil, fmt.Errorf("archive: unsupported version %d", a.Version)
	}
	for _, e := range a.Events {
		if err := st.PutEvent(e); err != nil {
			return nil, err
		}
	}
	for _, s := range a.Sources {
		raw, err := base64.StdEncoding.DecodeString(s.BytesB64)
		if err != nil {
			return nil, fmt.Errorf("archive: source %s: %w", s.ID, err)
		}
		if err := st.PutSource(&store.Source{
			ID: s.ID, Version: s.Version, ParentID: s.ParentID, Name: s.Name,
			Sha256: s.Sha256, Bytes: raw, CreatedSeq: s.CreatedSeq, Status: s.Status,
		}); err != nil {
			return nil, err
		}
	}
	for _, d := range a.Derived {
		if err := st.PutDerived(d); err != nil {
			return nil, err
		}
	}
	for _, d := range a.Decisions {
		if err := st.PutDecision(d); err != nil {
			return nil, err
		}
	}
	for k, v := range a.KV {
		if err := st.SetKV(k, v); err != nil {
			return nil, err
		}
	}
	return &a, nil
}
