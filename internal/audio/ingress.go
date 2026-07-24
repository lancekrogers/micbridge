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
	// PolicyDropOldest discards the oldest queued frame to accept the new one.
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

// NewIngress preallocates capacity frames of frameBytes each.
func NewIngress(capacity, frameBytes int, policy IngressPolicy) (*Ingress, error) {
	if capacity < 1 || frameBytes < 1 {
		return nil, fmt.Errorf("audio: ingress capacity and frameBytes must be positive")
	}
	in := &Ingress{
		slots:   make([][]byte, capacity),
		metaSeq: make([]uint64, capacity),
		metaTs:  make([]int64, capacity),
		metaN:   make([]int32, capacity),
		cap:     uint64(capacity),
		frameB:  frameBytes,
		policy:  policy,
	}
	for i := range in.slots {
		in.slots[i] = make([]byte, frameBytes)
	}
	return in, nil
}

// PushFromCallback copies pcm into a preallocated slot. Single producer only.
func (in *Ingress) PushFromCallback(pcm []byte, ts int64) bool {
	if in.closed.Load() == 1 {
		return false
	}
	if len(pcm) > in.frameB {
		pcm = pcm[:in.frameB]
	}
	for {
		head := in.head.Load()
		tail := in.tail.Load()
		size := tail - head
		if size >= in.cap {
			in.drop.Add(1)
			if in.policy == PolicyDropOldest {
				// Advance head once to drop oldest (SPSC: producer may help drop).
				in.head.CompareAndSwap(head, head+1)
				continue
			}
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
		if err := ctx.Err(); err != nil {
			return
		}
		fr, ok := in.Pop()
		if !ok {
			if in.closed.Load() == 1 {
				return
			}
			runtime.Gosched()
			continue
		}
		handle(fr)
	}
}
