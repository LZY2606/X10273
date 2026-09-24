package tiffio

import "fmt"

// Budget bounds parser work. IFDs may legitimately share sub-directories and
// may maliciously loop, so every pointer visit, IFD entry and external byte
// read is metered. Exhaustion is a first-class parse result.
type Budget struct {
	MaxVisits   int64 // pointer/IFD visits
	MaxEntries  int64 // entries examined across all IFDs
	MaxBytes    int64 // external/value bytes read
	VisitsUsed  int64
	EntriesUsed int64
	BytesUsed   int64
}

// DefaultBudget is generous for real images but finite.
func DefaultBudget() *Budget {
	return &Budget{MaxVisits: 4096, MaxEntries: 1_000_000, MaxBytes: 512 << 20}
}

// TightBudget is used by the exhaustion acceptance case: one IFD visit after
// the root guarantees a cycle/shared graph cannot be fully walked.
func TightBudget() *Budget {
	return &Budget{MaxVisits: 2, MaxEntries: 64, MaxBytes: 4096}
}

// ChargeVisit accounts one pointer traversal.
func (b *Budget) ChargeVisit(off int64) error {
	b.VisitsUsed++
	if b.VisitsUsed > b.MaxVisits {
		return &ErrBudget{Kind: "ifd-visits", Used: b.VisitsUsed, Limit: b.MaxVisits, Offset: off}
	}
	return nil
}

// ChargeEntry accounts one IFD entry examined.
func (b *Budget) ChargeEntry(off int64) error {
	b.EntriesUsed++
	if b.EntriesUsed > b.MaxEntries {
		return &ErrBudget{Kind: "ifd-entries", Used: b.EntriesUsed, Limit: b.MaxEntries, Offset: off}
	}
	return nil
}

// ChargeBytes accounts external value/block byte reads.
func (b *Budget) ChargeBytes(n, off int64) error {
	b.BytesUsed += n
	if b.BytesUsed > b.MaxBytes {
		return &ErrBudget{Kind: "bytes", Used: b.BytesUsed, Limit: b.MaxBytes, Offset: off}
	}
	return nil
}

// Snapshot returns an immutable usage summary for diagnostics.
func (b *Budget) Snapshot() BudgetUsage {
	return BudgetUsage{
		MaxVisits: b.MaxVisits, MaxEntries: b.MaxEntries, MaxBytes: b.MaxBytes,
		VisitsUsed: b.VisitsUsed, EntriesUsed: b.EntriesUsed, BytesUsed: b.BytesUsed,
	}
}

// BudgetUsage is a value snapshot of Budget.
type BudgetUsage struct {
	MaxVisits   int64 `json:"max_visits"`
	MaxEntries  int64 `json:"max_entries"`
	MaxBytes    int64 `json:"max_bytes"`
	VisitsUsed  int64 `json:"visits_used"`
	EntriesUsed int64 `json:"entries_used"`
	BytesUsed   int64 `json:"bytes_used"`
}

func (u BudgetUsage) String() string {
	return fmt.Sprintf("visits %d/%d entries %d/%d bytes %d/%d",
		u.VisitsUsed, u.MaxVisits, u.EntriesUsed, u.MaxEntries, u.BytesUsed, u.MaxBytes)
}
