package driver

// Name identifies a compiled-in driver implementation.
type Name string

// Registry is a compile-time placeholder for driver discovery.
// Phase 002 will replace this with production registration.
type Registry struct {
	names []Name
}

// NewRegistry returns an empty driver registry.
func NewRegistry() *Registry {
	return &Registry{names: nil}
}

// Names returns registered driver names in registration order.
func (r *Registry) Names() []Name {
	if r == nil || len(r.names) == 0 {
		return nil
	}
	out := make([]Name, len(r.names))
	copy(out, r.names)
	return out
}
