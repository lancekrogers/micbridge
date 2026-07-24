package audio

import "context"

// Capture is a compile-time placeholder for native capture ownership.
// Phase 003 attaches malgo and the callback-safe handoff queue.
type Capture struct {
	Source Source
	Format Format
}

// NewCapture returns an inactive capture shell.
func NewCapture() *Capture {
	return &Capture{}
}

// OpenResult is returned when a source is selected for capture setup.
type OpenResult struct {
	Source Source
	Format Format
}

// ResolveSource selects and negotiates without silent fallback.
func ResolveSource(ctx context.Context, enum Enumerator, id SourceID, want Format) (OpenResult, error) {
	sources, err := enum.List(ctx)
	if err != nil {
		return OpenResult{}, err
	}
	src, err := SelectByID(sources, id)
	if err != nil {
		return OpenResult{}, err
	}
	f, err := Negotiate(src, want)
	if err != nil {
		return OpenResult{}, err
	}
	return OpenResult{Source: src, Format: f}, nil
}
