// Package clock is the single source of timestamps.
//
// Tests and the offline case pack inject a fixed, virtual clock; the server
// uses a real clock. Timestamps are never mixed into content-addressed ids.
package clock

import "time"

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Virtual is a manually advanced clock used by tests and case replay.
type Virtual struct {
	T time.Time
}

func NewVirtual(start time.Time) *Virtual { return &Virtual{T: start.UTC()} }
func (v *Virtual) Now() time.Time         { return v.T }
func (v *Virtual) Advance(d time.Duration) time.Time {
	v.T = v.T.Add(d)
	return v.T
}
