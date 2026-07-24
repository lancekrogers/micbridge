package audio

// SourceID is a stable capture-source identifier assigned by Micbridge.
// Volatile OS indexes must not be persisted as identity.
type SourceID string

// Capture is a compile-time placeholder for native capture ownership.
// Phase 003 will attach malgo and the callback-safe handoff queue.
type Capture struct{}

// NewCapture returns an inactive capture shell.
func NewCapture() *Capture {
	return &Capture{}
}
