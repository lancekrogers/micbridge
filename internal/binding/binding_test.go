package binding_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/core"
)

func capsCallable() []core.Capability {
	return []core.Capability{{
		Schema:       core.CapabilitySchema,
		Key:          core.CapControlPress,
		Availability: core.Available,
		Evidence:     core.Evidence{Level: core.EvidenceMeasured, Summary: "ok"},
		SideEffects:  []core.SideEffect{{ID: "host.volume_increment", Passive: true}},
	}}
}

func TestOneGestureOneAction(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	path := filepath.Join(t.TempDir(), "bindings.json")
	eng := binding.New(clk, path)
	b, err := eng.Upsert(binding.Binding{
		Schema:   binding.Schema,
		ID:       "bind_toggle",
		DeviceID: "dev_1",
		Control:  "primary",
		On:       "pressed",
		Action:   binding.ActionRecordingToggle,
		Enabled:  true,
	}, capsCallable())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.SideEffects) != 1 || b.SideEffects[0] != "host.volume_increment" {
		t.Fatalf("side effects = %v", b.SideEffects)
	}

	in := binding.ControlInput{
		DeviceID: "dev_1", Type: "pressed", Control: "primary", PressID: "press_1", At: clk.Now(),
	}
	d1, cmd1 := eng.Handle(in)
	if !d1.Accepted || cmd1 == nil || cmd1.Action != binding.ActionRecordingToggle {
		t.Fatalf("first: %+v %+v", d1, cmd1)
	}
	// Duplicate same press_id must not re-fire.
	d2, cmd2 := eng.Handle(in)
	if d2.Accepted || cmd2 != nil || d2.Reason != "duplicate_press_id" {
		t.Fatalf("dup: %+v %+v", d2, cmd2)
	}
	// New press fires once.
	clk.Advance(time.Second)
	in.PressID = "press_2"
	in.At = clk.Now()
	d3, cmd3 := eng.Handle(in)
	if !d3.Accepted || cmd3 == nil {
		t.Fatalf("second gesture: %+v %+v", d3, cmd3)
	}
}

func TestDebounce(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	eng := binding.New(clk, "")
	_, err := eng.Upsert(binding.Binding{
		Schema: binding.Schema, ID: "b1", DeviceID: "dev_1",
		Control: "primary", On: "pressed", Action: binding.ActionRecordingStart,
		DebounceMs: 100, Enabled: true,
	}, capsCallable())
	if err != nil {
		t.Fatal(err)
	}
	_, cmd := eng.Handle(binding.ControlInput{
		DeviceID: "dev_1", Type: "pressed", Control: "primary", PressID: "a", At: clk.Now(),
	})
	if cmd == nil {
		t.Fatal("expected first fire")
	}
	clk.Advance(50 * time.Millisecond)
	d, cmd := eng.Handle(binding.ControlInput{
		DeviceID: "dev_1", Type: "pressed", Control: "primary", PressID: "b", At: clk.Now(),
	})
	if cmd != nil || d.Reason != "debounced" {
		t.Fatalf("debounce failed: %+v %+v", d, cmd)
	}
}

func TestUnsafeBindingCannotActivate(t *testing.T) {
	eng := binding.New(nil, "")
	b, err := eng.Upsert(binding.Binding{
		Schema: binding.Schema, ID: "unsafe", DeviceID: "dev_1",
		Control: "primary", On: "pressed", Action: binding.ActionRecordingToggle, Enabled: true,
	}, []core.Capability{{
		Schema: core.CapabilitySchema, Key: core.CapControlPress,
		Availability: core.Unsupported, UnavailableReason: "not present",
		Evidence: core.Evidence{Level: core.EvidenceMeasuredNegative},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Enabled {
		t.Fatal("should not stay enabled")
	}
	d, cmd := eng.Handle(binding.ControlInput{
		DeviceID: "dev_1", Type: "pressed", Control: "primary", PressID: "p",
	})
	if d.Accepted || cmd != nil {
		t.Fatalf("unsafe activated: %+v %+v", d, cmd)
	}
	// Binding remains inspectable.
	got, ok := eng.Get("unsafe")
	if !ok || got.Reason == "" {
		t.Fatalf("expected inspectable reason: %+v", got)
	}
}

func TestPersistAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.json")
	eng := binding.New(nil, path)
	_, err := eng.Upsert(binding.Binding{
		Schema: binding.Schema, ID: "p1", DeviceID: "dev_1",
		Control: "primary", On: "pressed", Action: binding.ActionRecordingStop, Enabled: true,
	}, capsCallable())
	if err != nil {
		t.Fatal(err)
	}
	eng2 := binding.New(nil, path)
	if err := eng2.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := eng2.Get("p1"); !ok {
		t.Fatal("missing after load")
	}
}

func TestUnknownActionRejected(t *testing.T) {
	eng := binding.New(nil, "")
	_, err := eng.Upsert(binding.Binding{
		Schema: binding.Schema, ID: "x", DeviceID: "dev_1",
		Control: "primary", On: "pressed", Action: "explode", Enabled: true,
	}, nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestOverlappingBindingsChooseStableLowestID(t *testing.T) {
	eng := binding.New(nil, "")
	for _, id := range []binding.ID{"z_last", "a_first"} {
		_, err := eng.Upsert(binding.Binding{
			Schema: binding.Schema, ID: id, DeviceID: "dev_1",
			Control: "primary", On: "pressed", Action: binding.ActionRecordingToggle, Enabled: true,
		}, capsCallable())
		if err != nil {
			t.Fatal(err)
		}
	}
	decision, command := eng.Handle(binding.ControlInput{
		DeviceID: "dev_1", Type: "pressed", Control: "primary", PressID: "stable",
	})
	if command == nil || decision.BindingID != "a_first" {
		t.Fatalf("decision=%+v command=%+v", decision, command)
	}
}

func TestPressDedupeRetentionIsBounded(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	eng := binding.New(clk, "")
	_, err := eng.Upsert(binding.Binding{
		Schema: binding.Schema, ID: "bounded", DeviceID: "dev_1",
		Control: "primary", On: "pressed", Action: binding.ActionRecordingToggle, Enabled: true,
	}, capsCallable())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5000; i++ {
		_, _ = eng.Handle(binding.ControlInput{
			DeviceID: "dev_1", Type: "pressed", Control: "primary",
			PressID: fmt.Sprintf("press-%d", i), At: clk.Now(),
		})
		clk.Advance(time.Millisecond)
	}
	if got := eng.DedupeEntries(); got > 4096 {
		t.Fatalf("dedupe entries=%d", got)
	}
}
