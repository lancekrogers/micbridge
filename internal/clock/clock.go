// Package clock provides injectable time for production and deterministic tests.
package clock

import "time"

// Clock yields wall and monotonic-like instants without forcing wall time.
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
	// Since returns the duration since t using this clock's Now.
	Since(t time.Time) time.Duration
}

// System is the real wall clock.
type System struct{}

// Now returns time.Now().
func (System) Now() time.Time { return time.Now() }

// Since returns time.Since(t).
func (System) Since(t time.Time) time.Duration { return time.Since(t) }

// Fake is a manually advanced clock for deterministic tests.
type Fake struct {
	now time.Time
}

// NewFake returns a fake clock starting at start (UTC if location is nil).
func NewFake(start time.Time) *Fake {
	if start.IsZero() {
		start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &Fake{now: start.UTC()}
}

// Now returns the current fake instant.
func (f *Fake) Now() time.Time {
	return f.now
}

// Since returns f.Now().Sub(t).
func (f *Fake) Since(t time.Time) time.Duration {
	return f.now.Sub(t)
}

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.now = f.now.Add(d)
}

// Set replaces the current fake instant.
func (f *Fake) Set(t time.Time) {
	f.now = t.UTC()
}
