package event

// Sequence is a daemon-monotonic event order assigned by the bus.
type Sequence uint64

// Bus is a compile-time placeholder for ordered event distribution.
// Phase 002 will add bounded subscriber buffers and gap events.
type Bus struct {
	next Sequence
}

// NewBus returns an idle event bus.
func NewBus() *Bus {
	return &Bus{next: 1}
}

// NextSequence returns the next sequence value without publishing.
func (b *Bus) NextSequence() Sequence {
	if b == nil {
		return 0
	}
	seq := b.next
	b.next++
	return seq
}
