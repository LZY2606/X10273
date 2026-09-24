package store

import (
	"ifdlantern/internal/domain"
	"ifdlantern/internal/tiff"
)

type Project struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	RootProject string `json:"root_project,omitempty"`
	CreatedSeq  int    `json:"created_seq"`
}

type SourceVersion struct {
	Project    string `json:"project"`
	Branch     string `json:"branch"`
	Label      string `json:"label"`
	Version    int    `json:"version"`
	Blob       string `json:"blob"`
	ParentBlob string `json:"parent_blob,omitempty"`
	BirthSeq   int    `json:"birth_seq"`
	Note       string `json:"note,omitempty"`
	Virtual    bool   `json:"virtual"`
}

type RuleVersion struct {
	Project     string      `json:"project"`
	Branch      string      `json:"branch"`
	Scope       string      `json:"scope"`
	Version     int         `json:"version"`
	Budget      tiff.Budget `json:"budget"`
	Fingerprint string      `json:"fingerprint"`
	Note        string      `json:"note,omitempty"`
	BirthSeq    int         `json:"birth_seq"`
}

type Decision struct {
	Project   string         `json:"project"`
	Branch    string         `json:"branch"`
	Candidate string         `json:"candidate"`
	Version   int            `json:"version"`
	Action    string         `json:"action"` // accept|reject|params
	Params    map[string]any `json:"params,omitempty"`
	Note      string         `json:"note,omitempty"`
	BirthSeq  int            `json:"birth_seq"`
}

type Fact struct {
	Project    string `json:"project"`
	Branch     string `json:"branch"`
	Scope      string `json:"scope"`
	Label      string `json:"label"`
	Kind       string `json:"kind"`
	KeyID      string `json:"key_id"`
	Data       string `json:"data"`
	SourceBlob string `json:"source_blob"`
	RuleFP     string `json:"rule_fp"`
	BirthSeq   int    `json:"birth_seq"`
}

// Envelope is the export/import package. Facts are intentionally omitted:
// import replays events, exercising the same deterministic derivation.
type Envelope struct {
	Version int        `json:"version"`
	Project Project    `json:"project"`
	Blobs   []BlobRec  `json:"blobs"`
	Events  []EventRec `json:"events"`
}

type BlobRec struct {
	ID        string `json:"id"`
	DataB64   string `json:"data_b64"`
	RegionB64 string `json:"region_b64,omitempty"`
	WideStart int64  `json:"wide_start,omitempty"`
	Virtual   bool   `json:"virtual,omitempty"`
	Note      string `json:"note,omitempty"`
}

type EventRec struct {
	Seq     int            `json:"seq"`
	ID      string         `json:"id"`
	Parent  string         `json:"parent,omitempty"`
	Project string         `json:"project"`
	Branch  string         `json:"branch"`
	Kind    string         `json:"kind"`
	At      string         `json:"at"`
	Actor   string         `json:"actor"`
	Payload map[string]any `json:"payload"`
}

func (e EventRec) Domain() domain.Event {
	return domain.Event{Seq: e.Seq, ID: e.ID, Parent: e.Parent, Branch: e.Branch,
		Kind: e.Kind, At: e.At, Actor: e.Actor, Payload: e.Payload}
}
