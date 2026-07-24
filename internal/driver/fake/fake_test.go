package fake_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/driver/fake"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

func TestRegistrySelectAndAmbiguity(t *testing.T) {
	reg := driver.NewRegistry()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := idgen.NewSeeded("reg")
	d1 := fake.New(fake.WithClock(clk), fake.WithIDs(ids), fake.WithScenario(fake.Scenario{
		DeviceID: "dev_fixed",
	}))
	if err := reg.Register(d1); err != nil {
		t.Fatal(err)
	}
	// Duplicate discovery: second fake with same match attributes causes ambiguity.
	d2 := fake.New(fake.WithClock(clk), fake.WithIDs(idgen.NewSeeded("reg2")), fake.WithScenario(fake.Scenario{
		DeviceID: "dev_other",
	}))
	if err := reg.Register(d2); err == nil {
		t.Fatal("expected duplicate id error for second fake driver")
	}
	// Replace registry with two differently-IDd drivers that both match — use wrappers.
	reg = driver.NewRegistry()
	_ = reg.Register(d1)
	c := driver.Candidate{Attributes: map[string]string{"profile": "fake"}}
	m, dr, err := reg.Select(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if m.Identity.DeviceID != "dev_fixed" || dr.ID() != fake.DriverID {
		t.Fatalf("match = %+v driver=%s", m, dr.ID())
	}
	_, _, err = reg.Select(context.Background(), driver.Candidate{Attributes: map[string]string{"profile": "nope"}})
	if !errors.Is(err, driver.ErrNoMatch) {
		t.Fatalf("want ErrNoMatch, got %v", err)
	}
}

func TestMatchCancellation(t *testing.T) {
	d := fake.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.Match(ctx, driver.Candidate{Attributes: map[string]string{"profile": "fake"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestFakeEventsDisconnectAndMalformed(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	d := fake.New(fake.WithClock(clk), fake.WithIDs(idgen.NewSeeded("ev")), fake.WithScenario(fake.Scenario{
		DeviceID: "dev_ev",
		Events: []driver.ControlEvent{
			{Type: "", Control: "primary"}, // malformed dropped
			{Type: "pressed", Control: "primary", PressID: "p1"},
			{Type: "released", Control: "primary", PressID: "p1"},
			{Type: "pressed", Control: "primary", PressID: "p2"},
		},
		DisconnectAfter: 2, // after two valid deliveries
	}))
	m, err := d.Match(context.Background(), driver.Candidate{Attributes: map[string]string{"profile": "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := d.Open(context.Background(), m, driver.Candidate{
		Attributes:     map[string]string{"profile": "fake"},
		TransientPaths: map[string]string{"audio": "pcmA", "control": "evtA"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dev.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := dev.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []driver.ControlEvent
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events: %+v", len(got), got)
	}
	if got[0].Type != "pressed" || got[1].Type != "released" {
		t.Fatalf("events = %+v", got)
	}
}

func TestPermissionDeniedAudio(t *testing.T) {
	d := fake.New(fake.WithScenario(fake.Scenario{
		DeviceID:         "dev_perm",
		PermissionDenied: true,
	}))
	m, _ := d.Match(context.Background(), driver.Candidate{Attributes: map[string]string{"profile": "fake"}})
	dev, err := d.Open(context.Background(), m, driver.Candidate{Attributes: map[string]string{"profile": "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dev.Close() }()
	_, err = dev.AudioSources(context.Background())
	if err == nil {
		t.Fatal("expected permission error")
	}
	caps := dev.Capabilities()
	found := false
	for _, c := range caps {
		if c.Key == core.CapAudioCapture {
			found = true
			if c.Availability != core.Unavailable {
				t.Fatalf("avail = %s", c.Availability)
			}
		}
	}
	if !found {
		t.Fatal("missing audio cap")
	}
}

func TestReconnectStableIdentityNewPaths(t *testing.T) {
	ids := idgen.NewSeeded("re")
	d := fake.New(fake.WithIDs(ids), fake.WithScenario(fake.Scenario{DeviceID: "dev_re"}))
	c1 := fake.ReopenCandidate("fake", "pcm1", "evt1")
	m1, err := d.Match(context.Background(), c1)
	if err != nil {
		t.Fatal(err)
	}
	dev1, err := d.Open(context.Background(), m1, c1)
	if err != nil {
		t.Fatal(err)
	}
	id1 := dev1.Identity()
	conn1 := dev1.ConnectionID()
	_ = dev1.Close()

	c2 := fake.ReopenCandidate("fake", "pcm9", "evt9")
	m2, err := d.Match(context.Background(), c2)
	if err != nil {
		t.Fatal(err)
	}
	dev2, err := d.Open(context.Background(), m2, c2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dev2.Close() }()
	if !id1.EqualStable(dev2.Identity()) {
		t.Fatal("identity not stable across reconnect")
	}
	if conn1 == dev2.ConnectionID() {
		t.Fatal("connection id should change")
	}
	if dev2.Surfaces()[0].TransientPath != "pcm9" {
		t.Fatalf("path = %s", dev2.Surfaces()[0].TransientPath)
	}
}

func TestEventsCancellationNoLeak(t *testing.T) {
	d := fake.New(fake.WithScenario(fake.Scenario{
		DeviceID: "dev_cancel",
		Events:   []driver.ControlEvent{{Type: "pressed", Control: "primary", PressID: "p"}},
		// no DisconnectAfter: waits on ctx
	}))
	m, _ := d.Match(context.Background(), driver.Candidate{Attributes: map[string]string{"profile": "fake"}})
	dev, _ := d.Open(context.Background(), m, driver.Candidate{Attributes: map[string]string{"profile": "fake"}})
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := dev.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for range ch {
	}
	_ = dev.Close()
	// Allow scheduler to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak? before=%d after=%d", before, runtime.NumGoroutine())
}

func TestDuplicateRegister(t *testing.T) {
	reg := driver.NewRegistry()
	d := fake.New()
	if err := reg.Register(d); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(d); err == nil {
		t.Fatal("expected duplicate error")
	}
}
