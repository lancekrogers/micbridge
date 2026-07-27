package audio

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
)

// Frame is a fixed-size PCM chunk handed from the callback path.
// Ownership: after Push, the ingress owns the bytes until Pop copies them out.
type Frame struct {
	Seq       uint64
	Timestamp int64
	PCM       []byte
}

// IngressPolicy controls overflow on the callback-safe ring.
type IngressPolicy int

const (
	// PolicyDropNewest rejects frames when full (default for capture health).
	PolicyDropNewest IngressPolicy = iota
	// PolicyDropOldest is reserved. It is rejected by NewIngress because a
	// lock-free SPSC producer cannot safely advance the consumer-owned head.
	PolicyDropOldest
)

// Ingress is a single-producer / single-consumer preallocated ring.
//
// Callback budget:
//   - no heap allocation in steady state
//   - no mutexes (atomics only)
//   - no I/O, logging, JSON, or subscriber fan-out
//   - return immediately after PushFromCallback
type Ingress struct {
	slots   [][]byte
	metaSeq []uint64
	metaTs  []int64
	metaN   []int32
	cap     uint64
	frameB  int
	policy  IngressPolicy

	// head = next read index (consumer), tail = next write index (producer)
	head atomic.Uint64
	tail atomic.Uint64
	seq  atomic.Uint64
	drop atomic.Uint64
	// closed: 0 open, 1 closed
	closed atomic.Uint32
}

// NewIngress preallocates capacity slots of maxFrameBytes each. Callback sizes
// may vary by backend even when a period size was requested, so any non-empty
// chunk up to the configured bound is accepted without allocation.
func NewIngress(capacity, maxFrameBytes int, policy IngressPolicy) (*Ingress, error) {
	if capacity < 1 || maxFrameBytes < 1 {
		return nil, fmt.Errorf("audio: ingress capacity and maxFrameBytes must be positive")
	}
	if policy != PolicyDropNewest {
		return nil, fmt.Errorf("audio: ingress policy %d is not safe for SPSC callback ownership", policy)
	}
	in := &Ingress{
		slots:   make([][]byte, capacity),
		metaSeq: make([]uint64, capacity),
		metaTs:  make([]int64, capacity),
		metaN:   make([]int32, capacity),
		cap:     uint64(capacity),
		frameB:  maxFrameBytes,
		policy:  policy,
	}
	for i := range in.slots {
		in.slots[i] = make([]byte, maxFrameBytes)
	}
	return in, nil
}

// PushFromCallback copies pcm into a preallocated slot. Single producer only.
func (in *Ingress) PushFromCallback(pcm []byte, ts int64) bool {
	if in.closed.Load() == 1 {
		return false
	}
	if len(pcm) == 0 || len(pcm) > in.frameB {
		in.drop.Add(1)
		return false
	}
	head := in.head.Load()
	tail := in.tail.Load()
	size := tail - head
	if size >= in.cap {
		in.drop.Add(1)
		return false
	}
	idx := tail % in.cap
	n := copy(in.slots[idx], pcm)
	seq := in.seq.Add(1)
	in.metaSeq[idx] = seq
	in.metaTs[idx] = ts
	in.metaN[idx] = int32(n)
	// Publish slot.
	in.tail.Store(tail + 1)
	return true
}

// OverflowCount is the number of overflow decisions.
func (in *Ingress) OverflowCount() uint64 { return in.drop.Load() }

// Len returns approximate queued frames.
func (in *Ingress) Len() int {
	h := in.head.Load()
	t := in.tail.Load()
	if t < h {
		return 0
	}
	n := t - h
	if n > in.cap {
		n = in.cap
	}
	return int(n)
}

// Pop removes one frame for the single consumer. PCM is a copy.
func (in *Ingress) Pop() (Frame, bool) {
	head := in.head.Load()
	tail := in.tail.Load()
	if head >= tail {
		return Frame{}, false
	}
	idx := head % in.cap
	n := int(in.metaN[idx])
	pcm := make([]byte, n)
	copy(pcm, in.slots[idx][:n])
	fr := Frame{Seq: in.metaSeq[idx], Timestamp: in.metaTs[idx], PCM: pcm}
	in.head.Store(head + 1)
	return fr, true
}

// Close marks ingress closed; further pushes fail.
func (in *Ingress) Close() { in.closed.Store(1) }

// DrainWorker pulls frames until ctx done and ring drained after close.
func DrainWorker(ctx context.Context, in *Ingress, handle func(Frame)) {
	for {
		fr, ok := in.Pop()
		if !ok {
			if in.closed.Load() == 1 {
				return
			}
			if err := ctx.Err(); err != nil {
				return
			}
			runtime.Gosched()
			continue
		}
		handle(fr)
	}
}
