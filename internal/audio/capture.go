package audio

import (
	"context"
	"fmt"
	"sync"
)

// CaptureStats reports loss and throughput at the real-time callback boundary.
// A finalized recording must have zero dropped frames.
type CaptureStats struct {
	Callbacks      uint64
	FramesAccepted uint64
	FramesDropped  uint64
	BytesAccepted  uint64
}

// Capture owns one native capture stream and its callback-to-worker handoff.
type Capture struct {
	Source       Source
	Format       Format
	NativeFormat Format

	mu       sync.Mutex
	started  bool
	closed   bool
	startFn  func() error
	closeFn  func() error
	overflow func() uint64
	stats    func() CaptureStats
}

// NewCapture returns an inactive capture value for compatibility.
func NewCapture() *Capture {
	return &Capture{}
}

// Start begins native capture.
func (c *Capture) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("audio: capture closed")
	}
	if c.started {
		return nil
	}
	if c.startFn == nil {
		return fmt.Errorf("audio: capture is not initialized")
	}
	if err := c.startFn(); err != nil {
		return err
	}
	c.started = true
	return nil
}

// Close stops callbacks, drains accepted frames, and releases native handles.
func (c *Capture) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	closeFn := c.closeFn
	c.mu.Unlock()
	if closeFn != nil {
		return closeFn()
	}
	return nil
}

// OverflowCount reports callback frames rejected by the bounded ingress.
func (c *Capture) OverflowCount() uint64 {
	if c.overflow == nil {
		return 0
	}
	return c.overflow()
}

// Stats returns a point-in-time capture-health snapshot.
func (c *Capture) Stats() CaptureStats {
	if c.stats == nil {
		return CaptureStats{}
	}
	return c.stats()
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
