package event_test

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

func TestTotalOrderAndTimestamps(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bus := event.NewBus(event.WithClock(clk), event.WithIDs(idgen.NewSeeded("e")), event.WithLogCapacity(32))
	defer bus.Close()

	ch, unsub, err := bus.Subscribe(16, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	var seqs []event.Sequence
	for i := 0; i < 5; i++ {
		clk.Advance(time.Millisecond)
		env, err := bus.Publish("device.connected", "dev_1", nil)
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, env.Sequence)
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("not monotonic: %v", seqs)
		}
	}
	// Drain live events.
	got := 0
	deadline := time.After(time.Second)
	for got < 5 {
		select {
		case env := <-ch:
			if env.Type == event.TypeStreamGap {
				continue
			}
			got++
		case <-deadline:
			t.Fatalf("only got %d events", got)
		}
	}
}

func TestSlowSubscriberGetsGap(t *testing.T) {
	bus := event.NewBus(event.WithIDs(idgen.NewSeeded("g")), event.WithLogCapacity(8))
	defer bus.Close()
	ch, unsub, err := bus.Subscribe(1, 0) // tiny buffer
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	for i := 0; i < 20; i++ {
		if _, err := bus.Publish("control.pressed", "dev_1", map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	// Read some events; at least one should be a gap under overflow.
	sawGap := false
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				goto done
			}
			if env.Type == event.TypeStreamGap {
				sawGap = true
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:
	if !sawGap {
		t.Fatal("expected stream.gap for slow subscriber")
	}
}

func TestSlowSubscriberGapCoversEveryDroppedSequence(t *testing.T) {
	bus := event.NewBus(event.WithIDs(idgen.NewSeeded("exact")), event.WithLogCapacity(8))
	defer bus.Close()
	ch, unsub, err := bus.Subscribe(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	_, _ = bus.Publish("control.pressed", "dev_1", nil)
	_, _ = bus.Publish("control.pressed", "dev_1", nil)
	gap := <-ch
	if gap.Type != event.TypeStreamGap {
		t.Fatalf("want gap, got %s", gap.Type)
	}
	if got := gap.Data["from_sequence"]; got != event.Sequence(1) {
		t.Fatalf("from_sequence=%v", got)
	}
	if got := gap.Data["to_sequence"]; got != event.Sequence(2) {
		t.Fatalf("to_sequence=%v", got)
	}
}

func TestReplayOverflowAlwaysDeliversExactGap(t *testing.T) {
	bus := event.NewBus(event.WithIDs(idgen.NewSeeded("replay")), event.WithLogCapacity(8))
	defer bus.Close()
	for i := 0; i < 5; i++ {
		_, _ = bus.Publish("device.ready", "dev_1", nil)
	}
	ch, unsub, err := bus.Subscribe(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	gap := <-ch
	if gap.Type != event.TypeStreamGap {
		t.Fatalf("want replay gap, got %s", gap.Type)
	}
	if got := gap.Data["from_sequence"]; got != event.Sequence(2) {
		t.Fatalf("from_sequence=%v", got)
	}
	if got := gap.Data["to_sequence"]; got != event.Sequence(5) {
		t.Fatalf("to_sequence=%v", got)
	}
}

func TestReplayCursorAndHistoryGap(t *testing.T) {
	bus := event.NewBus(event.WithIDs(idgen.NewSeeded("r")), event.WithLogCapacity(3))
	defer bus.Close()
	for i := 0; i < 5; i++ {
		if _, err := bus.Publish("device.ready", "dev_x", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Resume from sequence 1; earliest retained is > 2 → history_expired gap.
	ch, unsub, err := bus.Subscribe(8, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	env := <-ch
	if env.Type != event.TypeStreamGap {
		t.Fatalf("want gap, got %s seq=%d", env.Type, env.Sequence)
	}
}

func TestSubscriberRemovalNoLeak(t *testing.T) {
	bus := event.NewBus(event.WithLogCapacity(4))
	before := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		_, unsub, err := bus.Subscribe(4, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = bus.Publish("health.degraded", "", nil)
		unsub()
	}
	bus.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+5 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine growth before=%d after=%d", before, runtime.NumGoroutine())
}

func TestConcurrentPublishRace(t *testing.T) {
	bus := event.NewBus(event.WithIDs(idgen.NewSeeded("c")), event.WithLogCapacity(128))
	defer bus.Close()
	ch, unsub, _ := bus.Subscribe(256, 0)
	defer unsub()

	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = bus.Publish("control.pressed", "dev", nil)
			}
		}()
	}
	wg.Wait()
	// Drain without deadlock.
	timeout := time.After(2 * time.Second)
	count := 0
	for count < 10 {
		select {
		case <-ch:
			count++
		case <-timeout:
			return
		}
	}
}
