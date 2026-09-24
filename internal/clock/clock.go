// Package clock provides a virtual time source so that every event
// timestamp is deterministic under tests and replay.
package clock

import "time"

// Clock is a source of virtual time. All domain timestamps come from a
// Clock so tests can pin time and replays stay reproducible.
type Clock interface {
	Now() time.Time
}

// System is the wall-clock implementation used by the server binary.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Fixed is a deterministic clock. Each call to Now advances by Step,
// starting from Start. It is safe for single-threaded test use.
type Fixed struct {
	Start time.Time
	Step  time.Duration
	cur   time.Time
}

// NewFixed returns a Fixed clock anchored at a stable, arbitrary epoch.
func NewFixed() *Fixed {
	return &Fixed{
		Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Step:  time.Second,
	}
}

func (c *Fixed) Now() time.Time {
	if c.cur.IsZero() {
		c.cur = c.Start
		return c.cur
	}
	c.cur = c.cur.Add(c.Step)
	return c.cur
}
