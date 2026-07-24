package audio

import "context"

// Enumerator lists capture sources without starting capture.
type Enumerator interface {
	List(ctx context.Context) ([]Source, error)
}

// StaticEnumerator is an injectable fixture enumerator for tests.
type StaticEnumerator struct {
	Sources []Source
	Err     error
}

// List returns configured sources or error.
func (s StaticEnumerator) List(ctx context.Context) ([]Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Err != nil {
		return nil, s.Err
	}
	out := make([]Source, len(s.Sources))
	copy(out, s.Sources)
	return out, nil
}
