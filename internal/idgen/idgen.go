// Package idgen provides injectable, seedable identifier generation.
package idgen

import (
	"fmt"
	"sync/atomic"
)

// Generator produces stable, opaque identifiers.
type Generator interface {
	// Next returns the next identifier with the given prefix (e.g. "dev", "evt").
	Next(prefix string) string
}

// Atomic is a process-local counter generator suitable for production defaults.
type Atomic struct {
	n atomic.Uint64
}

// Next returns prefix_ followed by a monotonic counter.
func (a *Atomic) Next(prefix string) string {
	v := a.n.Add(1)
	if prefix == "" {
		prefix = "id"
	}
	return fmt.Sprintf("%s_%d", prefix, v)
}

// Seeded produces fully deterministic identifiers from a fixed seed and counter.
// Safe for single-goroutine tests; use one instance per test.
type Seeded struct {
	seed    string
	counter uint64
}

// NewSeeded returns a generator that always yields the same sequence for seed.
func NewSeeded(seed string) *Seeded {
	if seed == "" {
		seed = "test"
	}
	return &Seeded{seed: seed}
}

// Next returns prefix_seed_NNNN with a zero-padded counter.
func (s *Seeded) Next(prefix string) string {
	s.counter++
	if prefix == "" {
		prefix = "id"
	}
	return fmt.Sprintf("%s_%s_%04d", prefix, s.seed, s.counter)
}

// Reset sets the counter back to zero (tests only).
func (s *Seeded) Reset() {
	s.counter = 0
}
