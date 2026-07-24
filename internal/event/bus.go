package event

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

// Bus is the single ordering authority for daemon events.
// Publish never blocks on slow subscribers; they receive gap events instead.
type Bus struct {
	mu     sync.Mutex
	clk    clock.Clock
	ids    idgen.Generator
	next   Sequence
	origin time.Time
	log    []Envelope
	logCap int
	// head is index of oldest event; logLen is count retained.
	head     int
	logLen   int
	retStart Sequence // sequence of oldest retained event; 0 if empty
	subs     map[uint64]*subscriber
	subSeq   uint64
	closed   bool
}

type subscriber struct {
	id     uint64
	ch     chan Envelope
	cursor Sequence // last delivered sequence
}

// Option configures a Bus.
type Option func(*Bus)

// WithClock sets the bus clock.
func WithClock(c clock.Clock) Option { return func(b *Bus) { b.clk = c } }

// WithIDs sets event id generation.
func WithIDs(g idgen.Generator) Option { return func(b *Bus) { b.ids = g } }

// WithLogCapacity sets how many events are retained for replay (default 256).
func WithLogCapacity(n int) Option { return func(b *Bus) { b.logCap = n } }

// NewBus returns an idle event bus.
func NewBus(opts ...Option) *Bus {
	b := &Bus{
		clk:    clock.System{},
		ids:    &idgen.Atomic{},
		next:   1,
		logCap: 256,
		subs:   make(map[uint64]*subscriber),
	}
	for _, o := range opts {
		o(b)
	}
	if b.logCap < 1 {
		b.logCap = 1
	}
	b.log = make([]Envelope, b.logCap)
	b.origin = b.clk.Now()
	return b
}

// NextSequence returns the next sequence value without publishing.
func (b *Bus) NextSequence() Sequence {
	b.mu.Lock()
	defer b.mu.Unlock()
	seq := b.next
	b.next++
	return seq
}

// Publish assigns sequence/time and fans out without blocking on subscribers.
func (b *Bus) Publish(typ Type, deviceID core.DeviceID, data map[string]any) (Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return Envelope{}, fmt.Errorf("event: bus closed")
	}
	now := b.clk.Now()
	env := Envelope{
		Schema:            "micbridge.event/v1",
		EventID:           b.ids.Next("evt"),
		Sequence:          b.next,
		Time:              now,
		MonotonicOffsetNs: now.Sub(b.origin).Nanoseconds(),
		Type:              typ,
		DeviceID:          deviceID,
		Data:              data,
	}
	b.next++
	b.appendLocked(env)
	b.fanoutLocked(env)
	return env, nil
}

func (b *Bus) appendLocked(env Envelope) {
	if b.logLen == b.logCap {
		// Drop oldest from replay log.
		b.head = (b.head + 1) % b.logCap
		b.logLen--
		if b.logLen > 0 {
			b.retStart = b.log[b.head].Sequence
		} else {
			b.retStart = 0
		}
	}
	idx := (b.head + b.logLen) % b.logCap
	b.log[idx] = env
	if b.logLen == 0 {
		b.retStart = env.Sequence
	}
	b.logLen++
}

func (b *Bus) fanoutLocked(env Envelope) {
	for _, sub := range b.subs {
		// Never block on slow subscribers; surface overflow as stream.gap.
		select {
		case sub.ch <- env:
			sub.cursor = env.Sequence
		default:
			// Drop one pending message (if any) to make room for an explicit gap.
			select {
			case <-sub.ch:
			default:
			}
			gap := Envelope{
				Schema:   "micbridge.event/v1",
				EventID:  b.ids.Next("evt"),
				Sequence: env.Sequence,
				Time:     env.Time,
				Type:     TypeStreamGap,
				Data: map[string]any{
					"from_sequence": env.Sequence,
					"to_sequence":   env.Sequence,
					"reason":        "subscriber_overflow",
				},
			}
			select {
			case sub.ch <- gap:
				sub.cursor = env.Sequence
			default:
				// Still blocked: advance cursor so reconnect can request replay.
				sub.cursor = env.Sequence
			}
		}
	}
}

// Subscribe attaches a subscriber with bounded buffer.
// from is the exclusive resume cursor (0 means from the next live event only,
// with optional replay of retained log when from < current).
// If from > 0 and history was dropped, a gap event is delivered first.
func (b *Bus) Subscribe(buffer int, from Sequence) (<-chan Envelope, func(), error) {
	if buffer < 1 {
		buffer = 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, nil, fmt.Errorf("event: bus closed")
	}
	b.subSeq++
	id := b.subSeq
	ch := make(chan Envelope, buffer)
	sub := &subscriber{id: id, ch: ch, cursor: from}
	b.subs[id] = sub

	// from == 0 means live-only (no replay). from > 0 resumes after that sequence.
	if from > 0 {
		if b.logLen > 0 && b.retStart > 0 && from+1 < b.retStart {
			// Cursor refers to history that fell out of the log.
			gap := Envelope{
				Schema:   "micbridge.event/v1",
				EventID:  b.ids.Next("evt"),
				Sequence: b.retStart - 1,
				Time:     b.clk.Now(),
				Type:     TypeStreamGap,
				Data: map[string]any{
					"from_sequence": from + 1,
					"to_sequence":   b.retStart - 1,
					"reason":        "history_expired",
				},
			}
			ch <- gap
			sub.cursor = b.retStart - 1
		}
	replay:
		for i := 0; i < b.logLen; i++ {
			env := b.log[(b.head+i)%b.logCap]
			if env.Sequence <= from {
				continue
			}
			select {
			case ch <- env:
				sub.cursor = env.Sequence
			default:
				// Buffer full during replay: emit one gap and stop replay.
				gap := Envelope{
					Schema:   "micbridge.event/v1",
					EventID:  b.ids.Next("evt"),
					Sequence: env.Sequence,
					Time:     env.Time,
					Type:     TypeStreamGap,
					Data: map[string]any{
						"from_sequence": env.Sequence,
						"to_sequence":   b.log[(b.head+b.logLen-1)%b.logCap].Sequence,
						"reason":        "replay_overflow",
					},
				}
				select {
				case ch <- gap:
					sub.cursor = gap.Sequence
				default:
				}
				break replay
			}
		}
	}

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if s, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(s.ch)
		}
	}
	return ch, unsub, nil
}

// Close rejects further publishes and removes all subscribers.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, s := range b.subs {
		close(s.ch)
		delete(b.subs, id)
	}
}

// Wait is a helper for tests: wait until ctx is done.
func Wait(ctx context.Context) { <-ctx.Done() }
