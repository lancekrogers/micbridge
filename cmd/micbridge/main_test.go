package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/storage"
	"github.com/lancekrogers/micbridge/pkg/client"
)

func TestVersionAndHelp(t *testing.T) {
	if got := defaultArgs(nil); len(got) != 1 || got[0] != "ui" {
		t.Fatalf("bare command routes to %v, want ui", got)
	}
	if code := run([]string{"version"}); code != client.ExitOK {
		t.Fatal(code)
	}
	if code := run([]string{"help"}); code != client.ExitOK {
		t.Fatal(code)
	}
	if code := run([]string{"nope"}); code != client.ExitUsage {
		t.Fatal(code)
	}
}

func TestCLIStatusJSONAgainstDaemon(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "micbridge.sock")
	layout, err := storage.NewLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{
		Layout:   layout,
		Bus:      event.NewBus(event.WithIDs(idgen.NewSeeded("cli"))),
		Bindings: binding.New(nil, filepath.Join(dir, "b.json")),
		Machine:  recording.NewMachine(nil, idgen.NewSeeded("m"), &recording.FakeSink{}, recording.Limits{}),
	}
	if err := srv.Start(sock); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Close(context.Background())
		srv.Machine.Close()
		srv.Bus.Close()
	})

	t.Setenv("MICBRIDGE_STATE_DIR", dir)
	t.Setenv("MICBRIDGE_SOCK", sock)
	t.Setenv("MICBRIDGE_JSON", "1")

	if code := run([]string{"status", "--json"}); code != client.ExitOK {
		t.Fatalf("status exit %d", code)
	}
	if code := run([]string{"doctor", "--json"}); code != client.ExitOK {
		t.Fatalf("doctor exit %d", code)
	}
}

func TestCLIDaemonUnavailable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MICBRIDGE_STATE_DIR", dir)
	t.Setenv("MICBRIDGE_SOCK", filepath.Join(dir, "missing.sock"))
	t.Setenv("MICBRIDGE_JSON", "")
	code := run([]string{"devices"})
	if code != client.ExitUnavailable {
		t.Fatalf("exit %d", code)
	}
}

func TestDefaultStateLayoutIsStableAndAbsolute(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("MICBRIDGE_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", xdg)
	layout, err := stateLayout()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "micbridge"); layout.Root != want {
		t.Fatalf("state root = %q, want %q", layout.Root, want)
	}
}

func TestServeOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MICBRIDGE_STATE_DIR", dir)
	t.Setenv("MICBRIDGE_SOCK", filepath.Join(dir, "s.sock"))
	t.Setenv("MICBRIDGE_SERVE_ONCE", "1")
	t.Setenv("MICBRIDGE_FAKE", "1")
	code := run([]string{"serve"})
	if code != client.ExitOK {
		t.Fatal(code)
	}
	time.Sleep(10 * time.Millisecond)
	_ = os.Remove(filepath.Join(dir, "s.sock"))
}

func TestControlRemainsReadyAcrossRecordingsAfterOneArm(t *testing.T) {
	root := t.TempDir()
	recDir := filepath.Join(root, "recordings")
	journalDir := filepath.Join(root, "journals")
	format := recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
	sink := recording.NewWAVSink(
		recDir, journalDir, format, recording.DefaultPolicy(recDir),
		recording.SyntheticPCMStarter(format, 5*time.Millisecond),
	)
	machine := recording.NewMachine(nil, idgen.NewSeeded("press"), sink, recording.Limits{})
	defer machine.Close()
	engine := binding.NewEngine()
	deviceID := core.DeviceID("dev_hollyland_test")
	_, err := engine.Upsert(binding.Binding{
		Schema: binding.Schema, ID: "primary", DeviceID: deviceID,
		Control: "primary", On: "pressed", Action: binding.ActionRecordingToggle, Enabled: true,
	}, []core.Capability{{
		Schema: core.CapabilitySchema, Key: core.CapControlPress,
		Availability: core.Available, Evidence: core.Evidence{Level: core.EvidenceMeasured},
	}})
	if err != nil {
		t.Fatal(err)
	}
	armed, err := machine.Submit(context.Background(), recording.Command{Name: recording.CmdArm, Actor: recording.ActorCLI})
	if err != nil {
		t.Fatal(err)
	}
	bus := event.NewBus()
	defer bus.Close()
	controls := make(chan driver.ControlEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumeControlEvents(ctx, bus, engine, machine, deviceID, controls)
	controls <- driver.ControlEvent{Type: "pressed", Control: "primary", PressID: "p1"}
	waitForState(t, machine, recording.StateRecording)
	controls <- driver.ControlEvent{Type: "pressed", Control: "primary", PressID: "p2"}
	waitForState(t, machine, recording.StateCompleted)
	if err := recording.ValidateWAV(filepath.Join(recDir, armed.SessionID+".wav")); err != nil {
		t.Fatal(err)
	}
	controls <- driver.ControlEvent{Type: "pressed", Control: "primary", PressID: "p3"}
	waitForState(t, machine, recording.StateRecording)
	secondID := machine.Snapshot().SessionID
	if secondID == armed.SessionID {
		t.Fatalf("second recording reused session %q", secondID)
	}
	controls <- driver.ControlEvent{Type: "pressed", Control: "primary", PressID: "p4"}
	waitForState(t, machine, recording.StateCompleted)
	if err := recording.ValidateWAV(filepath.Join(recDir, secondID+".wav")); err != nil {
		t.Fatal(err)
	}
}

func waitForState(t *testing.T, machine *recording.Machine, want recording.State) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if machine.Snapshot().State == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state=%s want=%s", machine.Snapshot().State, want)
}
