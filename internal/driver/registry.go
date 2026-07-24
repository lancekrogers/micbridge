package driver

import (
	"context"
	"fmt"
	"sync"
)

// Name identifies a compiled-in driver implementation.
type Name string

// Registry holds compiled-in drivers and selects matches for candidates.
type Registry struct {
	mu      sync.RWMutex
	drivers []Driver
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a driver. Duplicate IDs are rejected.
func (r *Registry) Register(d Driver) error {
	if d == nil {
		return fmt.Errorf("driver: nil driver")
	}
	id := d.ID()
	if id == "" {
		return fmt.Errorf("driver: empty id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.drivers {
		if existing.ID() == id {
			return fmt.Errorf("driver: duplicate id %q", id)
		}
	}
	r.drivers = append(r.drivers, d)
	return nil
}

// Names returns registered driver IDs in registration order.
func (r *Registry) Names() []Name {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Name, 0, len(r.drivers))
	for _, d := range r.drivers {
		out = append(out, Name(d.ID()))
	}
	return out
}

// Drivers returns a snapshot of registered drivers.
func (r *Registry) Drivers() []Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Driver, len(r.drivers))
	copy(out, r.drivers)
	return out
}

// Select finds exactly one matching driver for c.
// Returns ErrNoMatch, ErrAmbiguous, or a single Match.
func (r *Registry) Select(ctx context.Context, c Candidate) (Match, Driver, error) {
	r.mu.RLock()
	drivers := make([]Driver, len(r.drivers))
	copy(drivers, r.drivers)
	r.mu.RUnlock()

	var matches []struct {
		m Match
		d Driver
	}
	for _, d := range drivers {
		if err := ctx.Err(); err != nil {
			return Match{}, nil, err
		}
		m, err := d.Match(ctx, c)
		if err != nil {
			if err == ErrNoMatch || err == ErrUnsupported {
				continue
			}
			return Match{}, nil, err
		}
		matches = append(matches, struct {
			m Match
			d Driver
		}{m, d})
	}
	switch len(matches) {
	case 0:
		return Match{}, nil, ErrNoMatch
	case 1:
		return matches[0].m, matches[0].d, nil
	default:
		return Match{}, nil, fmt.Errorf("%w: %d drivers matched", ErrAmbiguous, len(matches))
	}
}
