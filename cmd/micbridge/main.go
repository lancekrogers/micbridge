package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/driver/fake"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/storage"
	"github.com/lancekrogers/micbridge/pkg/client"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return client.ExitOK
	}
	jsonOut := envBool("MICBRIDGE_JSON") || flagJSON(&args)

	switch args[0] {
	case "version", "--version", "-V":
		fmt.Printf("%s %s\n", core.Name, core.Version)
		return client.ExitOK
	case "help", "-h", "--help":
		printUsage()
		return client.ExitOK
	case "serve":
		return cmdServe(args[1:], jsonOut)
	case "status":
		return cmdStatus(jsonOut)
	case "devices":
		return cmdDevices(jsonOut)
	case "doctor":
		return cmdDoctor(jsonOut)
	case "bindings":
		return cmdBindings(args[1:], jsonOut)
	case "record":
		return cmdRecord(args[1:], jsonOut)
	case "stop":
		return cmdStop(jsonOut)
	case "events":
		return cmdEvents(args[1:], jsonOut)
	case "recover":
		return cmdRecover(jsonOut)
	default:
		fmt.Fprintf(os.Stderr, "micbridge: unknown command %q (try 'micbridge help')\n", args[0])
		return client.ExitUsage
	}
}

func flagJSON(args *[]string) bool {
	out := false
	var rest []string
	for _, a := range *args {
		if a == "--json" {
			out = true
			continue
		}
		rest = append(rest, a)
	}
	*args = rest
	return out
}

func envBool(k string) bool {
	v := os.Getenv(k)
	return v == "1" || v == "true" || v == "yes"
}

func printUsage() {
	_, _ = fmt.Fprintf(os.Stdout, "Usage: %s <command> [flags]\n\n", core.Name)
	_, _ = fmt.Fprintln(os.Stdout, "Commands:")
	_, _ = fmt.Fprintln(os.Stdout, "  version    Print version")
	_, _ = fmt.Fprintln(os.Stdout, "  serve      Run local daemon (fake driver by default)")
	_, _ = fmt.Fprintln(os.Stdout, "  status     Daemon status")
	_, _ = fmt.Fprintln(os.Stdout, "  devices    List devices")
	_, _ = fmt.Fprintln(os.Stdout, "  doctor     Local diagnostics")
	_, _ = fmt.Fprintln(os.Stdout, "  bindings   List or set bindings")
	_, _ = fmt.Fprintln(os.Stdout, "  record     Arm/start recording")
	_, _ = fmt.Fprintln(os.Stdout, "  stop       Stop recording")
	_, _ = fmt.Fprintln(os.Stdout, "  events     Stream events")
	_, _ = fmt.Fprintln(os.Stdout, "  recover    Request recovery transition")
	_, _ = fmt.Fprintln(os.Stdout, "  help       Show this help")
	_, _ = fmt.Fprintln(os.Stdout, "\nGlobal: --json or MICBRIDGE_JSON=1 for JSON output")
	_, _ = fmt.Fprintln(os.Stdout, "Socket: MICBRIDGE_SOCK or state dir default")
}

func stateLayout() (*storage.Layout, error) {
	root := os.Getenv("MICBRIDGE_STATE_DIR")
	if root == "" {
		root = filepath.Join(".", ".micbridge")
	}
	return storage.NewLayout(root)
}

func socketPath(layout *storage.Layout) string {
	if s := os.Getenv("MICBRIDGE_SOCK"); s != "" {
		return s
	}
	return api.DefaultSocketPath(layout)
}

func newClient() (*client.Client, error) {
	layout, err := stateLayout()
	if err != nil {
		return nil, err
	}
	return client.New(socketPath(layout)), nil
}

func printResult(jsonOut bool, human string, v any) int {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			fmt.Fprintf(os.Stderr, "micbridge: %v\n", err)
			return client.ExitUsage
		}
		return client.ExitOK
	}
	fmt.Println(human)
	return client.ExitOK
}

func mapClientErr(err error) int {
	if err == nil {
		return client.ExitOK
	}
	if err == context.Canceled {
		return client.ExitCanceled
	}
	if ae, ok := err.(*client.APIError); ok {
		switch {
		case ae.Status == 409:
			return client.ExitConflict
		case ae.Status == 400:
			return client.ExitSchema
		default:
			return client.ExitUnavailable
		}
	}
	fmt.Fprintf(os.Stderr, "micbridge: %v\n", err)
	return client.ExitUnavailable
}

func cmdServe(args []string, jsonOut bool) int {
	layout, err := stateLayout()
	if err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: %v\n", err)
		return client.ExitUsage
	}
	_ = os.MkdirAll(layout.Root, 0o700)
	_ = os.MkdirAll(layout.RecordingsDir(), 0o700)
	_ = os.MkdirAll(layout.JournalsDir(), 0o700)

	bus := event.NewBus(event.WithIDs(&idgen.Atomic{}))
	defer bus.Close()
	eng := binding.New(nil, filepath.Join(layout.Root, "bindings.json"))
	_ = eng.Load()
	mach := recording.NewMachine(nil, &idgen.Atomic{}, &recording.FakeSink{}, recording.Limits{})
	defer mach.Close()

	reg := driver.NewRegistry()
	_ = reg.Register(fake.New(fake.WithIDs(&idgen.Atomic{}), fake.WithScenario(fake.Scenario{DeviceID: "dev_fake_1"})))
	c := driver.Candidate{Attributes: map[string]string{"profile": "fake"}, TransientPaths: map[string]string{"audio": "fake-pcm", "control": "fake-evt"}}
	var devices []core.Device
	if m, d, err := reg.Select(context.Background(), c); err == nil {
		if dev, err := d.Open(context.Background(), m, c); err == nil {
			devices = []core.Device{{
				Schema:       core.DeviceSchema,
				Identity:     dev.Identity(),
				ConnectionID: dev.ConnectionID(),
				Surfaces:     dev.Surfaces(),
				Capabilities: dev.Capabilities(),
			}}
			_ = dev.Close()
		}
	}

	srv := &api.Server{
		Layout:   layout,
		Bus:      bus,
		Bindings: eng,
		Machine:  mach,
		Devices:  func() []core.Device { return devices },
	}
	sock := socketPath(layout)
	if err := srv.Start(sock); err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: serve: %v\n", err)
		return client.ExitUnavailable
	}
	defer srv.Close(context.Background())
	_, _ = bus.Publish("device.connected", "dev_fake_1", nil)

	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"socket": sock, "state_dir": layout.Root})
	} else {
		fmt.Printf("micbridge serving on %s (state %s)\n", sock, layout.Root)
	}
	// Block until signal would be handled; for demo, sleep forever unless MICBRIDGE_SERVE_ONCE.
	if os.Getenv("MICBRIDGE_SERVE_ONCE") == "1" {
		time.Sleep(50 * time.Millisecond)
		return client.ExitOK
	}
	select {}
}

func cmdStatus(jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := cl.Status(ctx)
	if err != nil {
		// Offline fallback for bootstrap without daemon.
		layout, _ := stateLayout()
		st = map[string]any{
			"name": core.Name, "version": core.Version, "healthy": false,
			"state_dir": layout.Root, "daemon": "unavailable",
		}
		if jsonOut {
			return printResult(true, "", st)
		}
		fmt.Fprintf(os.Stderr, "micbridge: daemon unavailable (%v)\n", err)
		return client.ExitUnavailable
	}
	if jsonOut {
		return printResult(true, "", st)
	}
	fmt.Printf("%s %v healthy=%v\n", st["name"], st["version"], st["healthy"])
	return client.ExitOK
}

func cmdDevices(jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	devs, err := cl.Devices(ctx)
	if err != nil {
		return mapClientErr(err)
	}
	if jsonOut {
		return printResult(true, "", map[string]any{"devices": devs})
	}
	if len(devs) == 0 {
		fmt.Println("no devices")
		return client.ExitOK
	}
	for _, d := range devs {
		fmt.Printf("%s\t%s\t%s\n", d.Identity.DeviceID, d.Identity.DriverID, d.Identity.DisplayName)
	}
	return client.ExitOK
}

func cmdDoctor(jsonOut bool) int {
	layout, err := stateLayout()
	if err != nil {
		return mapClientErr(err)
	}
	sock := socketPath(layout)
	report := map[string]any{
		"state_dir": layout.Root,
		"socket":    sock,
		"version":   core.Version,
	}
	if fi, err := os.Stat(sock); err == nil {
		report["socket_exists"] = true
		report["socket_mode"] = fi.Mode().String()
	} else {
		report["socket_exists"] = false
	}
	cl := client.New(sock)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := cl.Status(ctx); err != nil {
		report["daemon"] = "unavailable"
		report["daemon_error"] = err.Error()
	} else {
		report["daemon"] = "ok"
	}
	if jsonOut {
		return printResult(true, "", report)
	}
	fmt.Printf("state_dir=%s\nsocket=%s\ndaemon=%v\n", report["state_dir"], report["socket"], report["daemon"])
	return client.ExitOK
}

func cmdBindings(args []string, jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if len(args) >= 1 && args[0] == "set" {
		// bindings set <id> <device> <control> <action>
		if len(args) < 5 {
			fmt.Fprintln(os.Stderr, "usage: micbridge bindings set <id> <device_id> <control> <action>")
			return client.ExitUsage
		}
		b := binding.Binding{
			Schema: binding.Schema, ID: binding.ID(args[1]), DeviceID: core.DeviceID(args[2]),
			Control: args[3], On: "pressed", Action: binding.Action(args[4]), Enabled: true,
		}
		out, err := cl.PutBinding(ctx, b, "")
		if err != nil {
			return mapClientErr(err)
		}
		return printResult(jsonOut, fmt.Sprintf("binding %s ok", out.ID), out)
	}
	list, err := cl.ListBindings(ctx)
	if err != nil {
		return mapClientErr(err)
	}
	if jsonOut {
		return printResult(true, "", map[string]any{"bindings": list})
	}
	for _, b := range list {
		fmt.Printf("%s\t%s\t%s\t%s\tenabled=%v\n", b.ID, b.DeviceID, b.Control, b.Action, b.Enabled)
	}
	return client.ExitOK
}

func cmdRecord(args []string, jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err)
	}
	action := "start"
	if len(args) > 0 && args[0] == "arm" {
		action = "arm"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snap, err := cl.CreateRecording(ctx, action, "")
	if err != nil {
		return mapClientErr(err)
	}
	return printResult(jsonOut, fmt.Sprintf("recording %s state=%s", snap.SessionID, snap.State), snap)
}

func cmdStop(jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Use current session placeholder id.
	snap, err := cl.RecordingAction(ctx, "current", "stop", "")
	if err != nil {
		return mapClientErr(err)
	}
	return printResult(jsonOut, fmt.Sprintf("state=%s", snap.State), snap)
}

func cmdEvents(args []string, jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, errs := cl.Events(ctx, 0)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				return client.ExitOK
			}
			if jsonOut {
				_ = json.NewEncoder(os.Stdout).Encode(env)
			} else {
				fmt.Printf("%d %s %s\n", env.Sequence, env.Type, env.DeviceID)
			}
		case err := <-errs:
			if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
				return mapClientErr(err)
			}
			return client.ExitOK
		case <-ctx.Done():
			return client.ExitOK
		}
	}
}

func cmdRecover(jsonOut bool) int {
	// Recovery is modeled via recording machine; expose as doctor-adjacent status for now.
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := cl.Status(ctx)
	if err != nil {
		return mapClientErr(err)
	}
	return printResult(jsonOut, "recovery: inspect status.recording and journals under state dir", st)
}
