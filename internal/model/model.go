// Package model defines the event-sourced, content-addressed review model:
// immutable source inputs, version chains, provenance-tagged derived objects,
// counterexample trials and reviewer decisions.
package model

import "time"

// Kind enumerates the persisted event types.
type Kind string

const (
	KindProjectCreated  Kind = "project_created"
	KindBlobImported    Kind = "blob_imported"
	KindParamRevision   Kind = "params_revised"
	KindSourceMutation  Kind = "source_mutated"
	KindTrialRun        Kind = "trial_run"
	KindDecision        Kind = "decision"
	KindDecisionRevoked Kind = "decision_revoked"
	KindArchiveImported Kind = "archive_imported"
)

// Event is the unit of history. IDs are deterministic from (project, seq)
// so export/delete/import preserves identity, order and ancestry.
type Event struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Seq       int64           `json:"seq"`
	Kind      Kind            `json:"kind"`
	Time      time.Time       `json:"time"`
	ParentID  string          `json:"parentId"`
	Actor     string          `json:"actor"` // "importer" | "reviewer:<name>" | "system"
	Title     string          `json:"title"`
	Summary   string          `json:"summary"`
	Payload   map[string]any  `json:"payload"`
}

// Provenance links a derived object to its direct sources, algorithm and
// parameter fingerprint.
type Provenance struct {
	AlgorithmVersion string   `json:"algorithmVersion"`
	ParamFingerprint string   `json:"paramFingerprint"`
	DirectSources    []Source `json:"directSources"`
	EventID          string   `json:"producedByEvent"`
}

type Source struct {
	Kind       string `json:"kind"` // "blob_version" | "ifd" | "tag" | "block" | "decision"
	ID         string `json:"id"`
	VersionID  string `json:"versionId,omitempty"`
	ByteOffset int64  `json:"byteOffset,omitempty"`
	ByteLength int64  `json:"byteLength,omitempty"`
}

// BlobVersion is an immutable TIFF input revision.
type BlobVersion struct {
	ID         string `json:"id"`
	DocID      string `json:"docId"`
	Version    int    `json:"version"`
	ParentID   string `json:"parentId,omitempty"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Label      string `json:"label"`
	Origin     string `json:"origin"` // "import" | "casepack" | "trial_mutation"
	ImportOK   bool   `json:"importOk"`
	ParseError string `json:"parseError,omitempty"`
}

// Decision is a reviewer adjudication on a disputed object. Revoking writes
// a new event; the original decision remains in history.
type Decision struct {
	ID          string    `json:"id"`
	EventID     string    `json:"eventId"`
	TargetKind  string    `json:"targetKind"` // "transform_candidate" | "trial"
	TargetID    string    `json:"targetId"`
	Verdict     string    `json:"verdict"` // "accepted" | "rejected"
	Reason      string    `json:"reason"`
	Reviewer    string    `json:"reviewer"`
	Time        time.Time `json:"time"`
	RevokedBy   string    `json:"revokedByEventId,omitempty"`
	Active      bool      `json:"active"`
}

// Trial is a counterexample run: mutate exactly one direct source and test
// whether impact stays within the expected scope.
type Trial struct {
	ID           string   `json:"id"`
	EventID      string   `json:"eventId"`
	Name         string   `json:"name"`
	Code         string   `json:"code"` // endian_flip, wide_oob, ifd_cycle, shared_subifd, tile_overlap, geokey_conflict, budget_exhausted, ...
	BaselineVer  string   `json:"baselineVersionId"`
	MutatedVer   string   `json:"mutatedVersionId"`
	Mutation     string   `json:"mutationDescription"`
	ParamChange  string   `json:"paramChange,omitempty"`
	// ScopeClaim lists objects expected to be affected; everything else must
	// stay current.
	ScopeClaim   []string `json:"expectedAffected"`
	Observed     []string `json:"observedAffected"`
	Contained    bool     `json:"impactContained"`
	Verdict      string   `json:"verdict"` // "holds" | "fails" | "inconclusive"
	Standing     string   `json:"standing"` // adopted | rejected | undecided (reviewer)
	Evidence     []string `json:"evidence"`
}

// CriterionEvent is the machine-readable statement of a standing judgment
// (the three domain rules the archive must preserve).
type CriterionEvent struct {
	Rule       string   `json:"rule"`
	RuleVersion string  `json:"ruleVersion"`
	Statement  string   `json:"statement"`
	Subjects   []string `json:"subjects"`
	Holds      bool     `json:"holds"`
	Evidence   []string `json:"evidence"`
	EventID    string   `json:"eventId"`
}
