// Package clock provides a controllable time source so replays and tests can
// run on virtual time while the server uses a real clock.
package clock

import (
	"sync/atomic"
	"time"
)

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Virtual is a monotonically advancing virtual clock. Advance is safe for
// concurrent use; every Now call advances the cursor so distinct events get
// distinct timestamps even inside a single replay batch.
type Virtual struct {
	cursor atomic.Int64
	step   int64
}

func NewVirtual(start time.Time, step time.Duration) *Virtual {
	v := &Virtual{step: int64(step)}
	v.cursor.Store(start.UnixNano())
	return v
}

func (v *Virtual) Now() time.Time {
	n := v.cursor.Add(v.step)
	return time.Unix(0, n).UTC()
}

func (v *Virtual) Advance(d time.Duration) { v.cursor.Add(int64(d)) }
