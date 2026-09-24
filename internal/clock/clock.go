// Package clock provides a controllable clock for deterministic replays.
package clock

import (
	"fmt"
	"sync"
	"time"
)

// Clock returns wall time in tests/demo but can be advanced virtually.
type Clock struct {
	mu      sync.Mutex
	base    time.Time
	fixed   bool
	seq     int
}

// New creates a clock anchored at the fixed ISO seed.
func New() *Clock {
	base, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	return &Clock{base: base, fixed: true}
}

// NewWall returns a real wall clock.
func NewWall() *Clock {
	return &Clock{base: time.Now().UTC(), fixed: false}
}

// Now returns the current instant and monotonic sequence.
func (c *Clock) Now() (time.Time, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	if c.fixed {
		return c.base, c.seq
	}
	return c.base.Add(time.Since(c.base)), c.seq
}

// Stamp returns an RFC3339 string plus sequence.
func (c *Clock) Stamp() (string, int) {
	t, seq := c.Now()
	return t.UTC().Format(time.RFC3339Nano), seq
}

// Advance moves a fixed clock by a duration.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base = c.base.Add(d)
}

// SetTo sets a fixed clock to an RFC3339 string.
func (c *Clock) SetTo(s string) error {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("clock set: %w", err)
	}
	c.mu.Lock()
	c.base = t
	c.fixed = true
	c.mu.Unlock()
	return nil
}
