// Package clock provides a controllable clock so tests run on virtual time.
package clock

import (
	"sync"
	"time"
)

// Clock is the time source used by the whole application.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// New returns a Clock set to the given start time.
func New(start time.Time) *Clock { return &Clock{now: start} }

// Now returns the current (possibly virtual) time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves virtual time forward.
func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

// Set assigns an absolute virtual time.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
