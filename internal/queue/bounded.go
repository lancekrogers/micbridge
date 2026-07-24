// Package queue provides bounded handoff queues used between capture, workers,
// and event subscribers. Full queues never block producers when DropNewest or
// DropOldest policy is selected.
package queue

import "fmt"

// Policy controls overflow behavior when Push finds a full buffer.
type Policy int

const (
	// DropNewest rejects the incoming item and reports overflow.
	DropNewest Policy = iota
	// DropOldest discards the head item to make room for the incoming item.
	DropOldest
)

// OverflowError is returned when a push cannot retain the item under policy.
type OverflowError struct {
	Capacity int
	Policy   Policy
}

func (e *OverflowError) Error() string {
	return fmt.Sprintf("queue: overflow capacity=%d policy=%d", e.Capacity, e.Policy)
}

// Bounded is a fixed-capacity FIFO of values of type T.
type Bounded[T any] struct {
	buf      []T
	head     int
	len      int
	policy   Policy
	overflow uint64
}

// NewBounded returns a queue with fixed capacity and overflow policy.
func NewBounded[T any](capacity int, policy Policy) (*Bounded[T], error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("queue: capacity must be positive")
	}
	return &Bounded[T]{
		buf:    make([]T, capacity),
		policy: policy,
	}, nil
}

// Cap returns the fixed capacity.
func (q *Bounded[T]) Cap() int { return len(q.buf) }

// Len returns the number of queued items.
func (q *Bounded[T]) Len() int { return q.len }

// OverflowCount returns how many times an overflow decision occurred.
func (q *Bounded[T]) OverflowCount() uint64 { return q.overflow }

// Push appends v, applying the overflow policy if full.
func (q *Bounded[T]) Push(v T) error {
	if q.len < len(q.buf) {
		idx := (q.head + q.len) % len(q.buf)
		q.buf[idx] = v
		q.len++
		return nil
	}
	q.overflow++
	switch q.policy {
	case DropOldest:
		// Drop head, then append.
		var zero T
		q.buf[q.head] = zero
		q.head = (q.head + 1) % len(q.buf)
		q.len--
		idx := (q.head + q.len) % len(q.buf)
		q.buf[idx] = v
		q.len++
		return &OverflowError{Capacity: len(q.buf), Policy: q.policy}
	default: // DropNewest
		return &OverflowError{Capacity: len(q.buf), Policy: q.policy}
	}
}

// Pop removes and returns the head item.
func (q *Bounded[T]) Pop() (T, bool) {
	var zero T
	if q.len == 0 {
		return zero, false
	}
	v := q.buf[q.head]
	q.buf[q.head] = zero
	q.head = (q.head + 1) % len(q.buf)
	q.len--
	return v, true
}

// Snapshot returns queued items in FIFO order without removing them.
func (q *Bounded[T]) Snapshot() []T {
	out := make([]T, q.len)
	for i := 0; i < q.len; i++ {
		out[i] = q.buf[(q.head+i)%len(q.buf)]
	}
	return out
}
