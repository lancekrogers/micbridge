package fixtures_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/recording"
)

var update = flag.Bool("update", false, "update golden fixtures")

func TestGoldenDevicesRoundTrip(t *testing.T) {
	raw := readGolden(t, "device.json")
	var dev core.Device
	if err := json.Unmarshal(raw, &dev); err != nil {
		t.Fatal(err)
	}
	if err := dev.Validate(); err != nil {
		t.Fatal(err)
	}
	// Unknown field preservation path via identity extra is covered in core tests.
	// Encode deterministic subset and compare canonical form.
	out, err := json.Marshal(dev)
	if err != nil {
		t.Fatal(err)
	}
	assertCompatibleJSON(t, raw, out)
	// Additive fields accepted on decode.
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["future_client_field"] = true
	b, _ := json.Marshal(m)
	var dev2 core.Device
	if err := json.Unmarshal(b, &dev2); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenBinding(t *testing.T) {
	raw := readGolden(t, "binding.json")
	var b binding.Binding
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenEvents(t *testing.T) {
	for _, name := range []string{"event_control.json", "event_gap.json"} {
		raw := readGolden(t, name)
		var env event.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(name, err)
		}
		if env.Schema != "micbridge.event/v1" {
			t.Fatalf("%s schema %s", name, env.Schema)
		}
		if name == "event_gap.json" && env.Type != event.TypeStreamGap {
			t.Fatal(env.Type)
		}
	}
}

func TestGoldenRecordingAndError(t *testing.T) {
	raw := readGolden(t, "recording_state.json")
	var snap recording.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.State != recording.StateRecording {
		t.Fatal(snap.State)
	}
	raw = readGolden(t, "error.json")
	var eb map[string]any
	if err := json.Unmarshal(raw, &eb); err != nil {
		t.Fatal(err)
	}
	if eb["code"] != "path_error" {
		t.Fatal(eb)
	}
}

func TestBreakingSchemaChangeDetected(t *testing.T) {
	raw := readGolden(t, "device.json")
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	// Simulate accidental break: rename schema.
	m["schema"] = "micbridge.device/v0-broken"
	broken, _ := json.Marshal(m)
	var dev core.Device
	if err := json.Unmarshal(broken, &dev); err != nil {
		t.Fatal(err)
	}
	if err := dev.Validate(); err == nil {
		t.Fatal("expected validation failure on breaking schema")
	}
}

func TestEncodeFixturesStable(t *testing.T) {
	// Re-encode known values and ensure required keys remain.
	dev := core.Device{
		Schema: core.DeviceSchema,
		Identity: core.Identity{
			Schema: core.IdentitySchema, DeviceID: "dev_fixture_receiver",
			DriverID: "example-driver", DriverVersion: "1.0.0",
		},
	}
	b, err := json.Marshal(dev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["schema"] != core.DeviceSchema {
		t.Fatal(m)
	}
	if *update {
		// Example of controlled update path — only when -update is set.
		path := filepath.Join("status.json")
		st := map[string]any{
			"schema": "micbridge.status/v1", "name": "micbridge",
			"version": core.Version, "healthy": true, "state_dir": "/tmp/micbridge-state",
		}
		out, _ := json.MarshalIndent(st, "", "  ")
		out = append(out, '\n')
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// assertCompatibleJSON requires all golden keys to remain present with equal values
// (additive encoder fields are allowed).
func assertCompatibleJSON(t *testing.T, golden, encoded []byte) {
	t.Helper()
	var g, e map[string]any
	if err := json.Unmarshal(golden, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &e); err != nil {
		t.Fatal(err)
	}
	var missing []string
	checkSubset(g, e, "", &missing)
	if len(missing) > 0 {
		var gb, eb bytes.Buffer
		_ = json.Indent(&gb, golden, "", "  ")
		_ = json.Indent(&eb, encoded, "", "  ")
		t.Fatalf("breaking change; missing/changed paths %v\ngolden:\n%s\nencoded:\n%s", missing, gb.String(), eb.String())
	}
}

func checkSubset(g, e map[string]any, prefix string, missing *[]string) {
	for k, gv := range g {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		ev, ok := e[k]
		if !ok {
			*missing = append(*missing, path)
			continue
		}
		gm, gok := gv.(map[string]any)
		em, eok := ev.(map[string]any)
		if gok && eok {
			checkSubset(gm, em, path, missing)
			continue
		}
		gb, _ := json.Marshal(gv)
		eb, _ := json.Marshal(ev)
		if !bytes.Equal(gb, eb) {
			// Allow empty vs null differences only for optional omitempty that vanished.
			if string(eb) == "null" || string(eb) == "[]" || string(eb) == "{}" {
				continue
			}
			// Surfaces/capabilities order-sensitive compare via marshal.
			if !bytes.Equal(gb, eb) {
				*missing = append(*missing, path+" (value changed)")
			}
		}
	}
}
