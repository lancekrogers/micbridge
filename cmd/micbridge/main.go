package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/audio"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/driver/fake"
	hollyland "github.com/lancekrogers/micbridge/internal/driver/hollyland35470007"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/storage"
	"github.com/lancekrogers/micbridge/internal/tui"
	"github.com/lancekrogers/micbridge/internal/ux"
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
	case "sources":
		return cmdSources(jsonOut)
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
	case "setup":
		return cmdSetup(jsonOut)
	case "ui":
		return cmdUI(args[1:], jsonOut)
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
	_, _ = fmt.Fprintln(os.Stdout, "  sources    List capture sources (no capture; uses malgo when cgo enabled)")
	_, _ = fmt.Fprintln(os.Stdout, "  bindings   List or set bindings")
	_, _ = fmt.Fprintln(os.Stdout, "  record     Arm/start recording")
	_, _ = fmt.Fprintln(os.Stdout, "  stop       Stop recording")
	_, _ = fmt.Fprintln(os.Stdout, "  events     Stream events")
	_, _ = fmt.Fprintln(os.Stdout, "  recover    Request recovery transition")
	_, _ = fmt.Fprintln(os.Stdout, "  setup      Guided device setup plan (JSON-friendly)")
	_, _ = fmt.Fprintln(os.Stdout, "  ui         Interactive responsive recorder TUI")
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

func mapClientErr(err error, jsonOut bool) int {
	if err == nil {
		return client.ExitOK
	}
	code := "unavailable"
	status := client.ExitUnavailable
	if err == context.Canceled {
		code = "canceled"
		status = client.ExitCanceled
	}
	if ae, ok := err.(*client.APIError); ok {
		code = ae.Code
		switch {
		case ae.Status == 409:
			status = client.ExitConflict
		case ae.Status == 400:
			status = client.ExitSchema
		}
	}
	if jsonOut {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error(), "code": code})
	} else {
		fmt.Fprintf(os.Stderr, "micbridge: %v\n", err)
	}
	return status
}

func cmdServe(args []string, jsonOut bool) int {
	_ = args
	layout, err := stateLayout()
	if err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: %v\n", err)
		return client.ExitUsage
	}
	for _, dir := range []string{layout.Root, layout.RecordingsDir(), layout.JournalsDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "micbridge: initialize %s: %v\n", dir, err)
			return client.ExitUnavailable
		}
	}
	runCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	bus := event.NewBus(event.WithIDs(&idgen.Atomic{}))
	defer bus.Close()
	eng := binding.New(nil, filepath.Join(layout.Root, "bindings.json"))
	if err := eng.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: load bindings: %v\n", err)
		return client.ExitUnavailable
	}

	format := recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
	policy := recording.DefaultPolicy(layout.RecordingsDir())
	var (
		devices       []core.Device
		openedDevices []driver.Device
		startPCM      recording.PCMStarter
		healthReasons []string
	)
	reg := driver.NewRegistry()
	if envBool("MICBRIDGE_FAKE") {
		if err := reg.Register(fake.New(fake.WithIDs(&idgen.Atomic{}), fake.WithScenario(fake.Scenario{DeviceID: "dev_fake_1"}))); err != nil {
			fmt.Fprintf(os.Stderr, "micbridge: register fake driver: %v\n", err)
			return client.ExitUnavailable
		}
		candidate := driver.Candidate{
			Attributes:     map[string]string{"profile": "fake"},
			TransientPaths: map[string]string{"audio": "fake-pcm", "control": "fake-evt"},
		}
		match, selected, err := reg.Select(runCtx, candidate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "micbridge: select fake driver: %v\n", err)
			return client.ExitUnavailable
		}
		dev, err := selected.Open(runCtx, match, candidate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "micbridge: open fake driver: %v\n", err)
			return client.ExitUnavailable
		}
		openedDevices = append(openedDevices, dev)
		devices = append(devices, coreDevice(dev))
		startPCM = recording.SyntheticPCMStarter(format, 20*time.Millisecond)
	} else {
		identitySalt, saltErr := loadIdentitySalt(layout.Root)
		if saltErr != nil {
			fmt.Fprintf(os.Stderr, "micbridge: identity salt: %v\n", saltErr)
			return client.ExitUnavailable
		}
		hardwareDriver := hollyland.NewWithIdentitySalt(&idgen.Atomic{}, identitySalt)
		if err := reg.Register(hardwareDriver); err != nil {
			fmt.Fprintf(os.Stderr, "micbridge: register Hollyland driver: %v\n", err)
			return client.ExitUnavailable
		}
		candidates, discoverErr := hollyland.DiscoverLinux(runCtx, "", "", "")
		if discoverErr != nil {
			healthReasons = append(healthReasons, "USB discovery failed: "+discoverErr.Error())
		}
		sources, sourceErr := (&audio.MalgoEnumerator{}).List(runCtx)
		if sourceErr != nil {
			healthReasons = append(healthReasons, "audio enumeration failed: "+sourceErr.Error())
		}
		var selectedSources []audio.Source
		for _, candidate := range candidates {
			if source, ok := correlateCaptureSource(candidate, sources); ok {
				candidate.TransientPaths["audio"] = string(source.ID)
				selectedSources = append(selectedSources, source)
			}
			match, selected, selectErr := reg.Select(runCtx, candidate)
			if selectErr != nil {
				healthReasons = append(healthReasons, "receiver near-match: "+selectErr.Error())
				continue
			}
			dev, openErr := selected.Open(runCtx, match, candidate)
			if openErr != nil {
				healthReasons = append(healthReasons, "receiver open failed: "+openErr.Error())
				continue
			}
			openedDevices = append(openedDevices, dev)
			snapshot := coreDevice(dev)
			devices = append(devices, snapshot)
			for _, capability := range snapshot.Capabilities {
				if (capability.Key == core.CapAudioCapture || capability.Key == core.CapControlPress) &&
					capability.Availability != core.Available {
					healthReasons = append(healthReasons, string(capability.Key)+": "+capability.UnavailableReason)
				}
			}
		}
		switch {
		case len(openedDevices) == 0:
			healthReasons = append(healthReasons, "no exact Hollyland 3547:0007 receiver found")
		case len(openedDevices) > 1:
			healthReasons = append(healthReasons, "multiple receivers found; select one before recording")
		case len(selectedSources) != 1:
			healthReasons = append(healthReasons, "receiver audio source is missing or ambiguous")
		default:
			source := selectedSources[0]
			audioFormat := audio.Format{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
			startPCM = func(ctx context.Context, write func([]byte) error) (func() error, error) {
				capture, err := audio.OpenNativeCapture(ctx, source, audioFormat, write)
				if err != nil {
					return nil, err
				}
				if err := capture.Start(); err != nil {
					_ = capture.Close()
					return nil, err
				}
				return capture.Close, nil
			}
		}
	}
	for _, dev := range openedDevices {
		defer func(device driver.Device) { _ = device.Close() }(dev)
	}
	if startPCM == nil {
		startPCM = func(context.Context, func([]byte) error) (func() error, error) {
			return nil, fmt.Errorf("recording: no unambiguous capture source is available")
		}
	}
	sink := recording.NewWAVSink(layout.RecordingsDir(), layout.JournalsDir(), format, policy, startPCM)
	mach := recording.NewMachine(nil, &idgen.Atomic{}, sink, recording.Limits{MaxDuration: policy.MaxDuration})
	defer mach.Close()

	for i, dev := range openedDevices {
		snapshot := devices[i]
		if len(eng.List()) == 0 {
			if _, err := eng.Upsert(binding.Binding{
				Schema: binding.Schema, ID: binding.ID("default_primary"),
				DeviceID: snapshot.Identity.DeviceID, Control: "primary", On: "pressed",
				Action: binding.ActionRecordingToggle, Enabled: true,
			}, snapshot.Capabilities); err != nil {
				healthReasons = append(healthReasons, "default binding failed: "+err.Error())
			}
		}
		events, eventErr := dev.Events(runCtx)
		if eventErr != nil {
			healthReasons = append(healthReasons, "control access failed: "+eventErr.Error())
			continue
		}
		go consumeControlEvents(runCtx, bus, eng, mach, snapshot.Identity.DeviceID, events)
	}

	srv := &api.Server{
		Layout:   layout,
		Bus:      bus,
		Bindings: eng,
		Machine:  mach,
		Devices:  func() []core.Device { return devices },
		Health: func() (bool, []string) {
			return len(healthReasons) == 0, append([]string(nil), healthReasons...)
		},
	}
	sock := socketPath(layout)
	if err := srv.Start(sock); err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: serve: %v\n", err)
		return client.ExitUnavailable
	}
	defer func() { _ = srv.Close(context.Background()) }()
	for _, device := range devices {
		_, _ = bus.Publish("device.connected", device.Identity.DeviceID, nil)
	}

	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"socket": sock, "state_dir": layout.Root})
	} else {
		fmt.Printf("micbridge serving on %s (state %s)\n", sock, layout.Root)
	}
	if os.Getenv("MICBRIDGE_SERVE_ONCE") == "1" {
		time.Sleep(50 * time.Millisecond)
		return client.ExitOK
	}
	<-runCtx.Done()
	return client.ExitOK
}

func loadIdentitySalt(stateRoot string) ([]byte, error) {
	path := filepath.Join(stateRoot, ".identity-salt")
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("identity salt has invalid length")
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	data = make([]byte, 32)
	if _, readErr := rand.Read(data); readErr != nil {
		return nil, readErr
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return loadIdentitySalt(stateRoot)
		}
		return nil, err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return data, nil
}

func coreDevice(dev driver.Device) core.Device {
	return core.Device{
		Schema:       core.DeviceSchema,
		Identity:     dev.Identity(),
		ConnectionID: dev.ConnectionID(),
		Surfaces:     dev.Surfaces(),
		Capabilities: dev.Capabilities(),
	}
}

func correlateCaptureSource(candidate driver.Candidate, sources []audio.Source) (audio.Source, bool) {
	product := strings.ToLower(candidate.Attributes["product"])
	var matches []audio.Source
	for _, source := range sources {
		name := strings.ToLower(source.Name)
		if (product != "" && strings.Contains(name, product)) ||
			strings.Contains(name, "hollyland") ||
			strings.Contains(name, "wireless microphone") {
			matches = append(matches, source)
		}
	}
	if len(matches) != 1 {
		return audio.Source{}, false
	}
	return matches[0], true
}

func consumeControlEvents(
	ctx context.Context,
	bus *event.Bus,
	engine *binding.Engine,
	machine *recording.Machine,
	deviceID core.DeviceID,
	events <-chan driver.ControlEvent,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case control, ok := <-events:
			if !ok {
				if ctx.Err() == nil {
					_, _ = bus.Publish("device.disconnected", deviceID, nil)
					current := machine.Snapshot()
					if current.State == recording.StateRecording || current.State == recording.StateArmed {
						_, _ = machine.Submit(ctx, recording.Command{
							Name: recording.CmdFail, Actor: recording.ActorSystem,
							SessionID: current.SessionID, Reason: "receiver control source disconnected; partial preserved",
						})
					}
				}
				return
			}
			_, _ = bus.Publish(event.Type("control."+control.Type), deviceID, map[string]any{
				"control": control.Control, "press_id": control.PressID,
				"side_effects": control.SideEffects,
			})
			_, command := engine.Handle(binding.ControlInput{
				DeviceID: deviceID, Type: control.Type, Control: control.Control,
				PressID: control.PressID, SideEffects: control.SideEffects,
			})
			if command == nil {
				continue
			}
			current := machine.Snapshot()
			var name recording.CommandName
			switch command.Action {
			case binding.ActionRecordingToggle:
				switch current.State {
				case recording.StateArmed:
					name = recording.CmdStart
				case recording.StateRecording:
					name = recording.CmdStop
				default:
					continue
				}
			case binding.ActionRecordingStart:
				if current.State != recording.StateArmed {
					continue
				}
				name = recording.CmdStart
			case binding.ActionRecordingStop:
				if current.State != recording.StateRecording {
					continue
				}
				name = recording.CmdStop
			default:
				continue
			}
			snap, err := machine.Submit(ctx, recording.Command{
				Name: name, Actor: recording.ActorBinding, SessionID: current.SessionID,
				IdempotencyKey: string(command.BindingID) + ":" + command.PressID,
				Reason:         "receiver " + control.Type,
			})
			data := map[string]any{"state": snap.State, "session_id": snap.SessionID}
			if err != nil {
				data["error"] = err.Error()
			}
			_, _ = bus.Publish(event.Type("recording."+string(snap.State)), deviceID, data)
		}
	}
}

func cmdStatus(jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err, jsonOut)
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
		return mapClientErr(err, jsonOut)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	devs, err := cl.Devices(ctx)
	if err != nil {
		return mapClientErr(err, jsonOut)
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

func cmdSources(jsonOut bool) int {
	// Diagnostic only: enumerate, never open a capture stream.
	var enum audio.Enumerator = &audio.MalgoEnumerator{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srcs, err := enum.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: sources: %v\n", err)
		return client.ExitUnavailable
	}
	if jsonOut {
		return printResult(true, "", map[string]any{"sources": srcs})
	}
	if len(srcs) == 0 {
		fmt.Println("no capture sources")
		return client.ExitOK
	}
	for _, s := range srcs {
		fmt.Printf("%s\t%q\tavailable=%v\n", s.ID, s.Name, s.Available)
	}
	return client.ExitOK
}

func cmdDoctor(jsonOut bool) int {
	layout, err := stateLayout()
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	sock := socketPath(layout)
	report := map[string]any{
		"state_dir": layout.Root,
		"socket":    sock,
		"version":   core.Version,
	}
	candidates, discoverErr := hollyland.DiscoverLinux(context.Background(), "", "", "")
	if discoverErr != nil {
		report["receiver_discovery"] = "unavailable"
		report["receiver_error"] = discoverErr.Error()
	} else {
		sources, _ := (&audio.MalgoEnumerator{}).List(context.Background())
		var receiverReports []hollyland.DoctorReport
		for _, candidate := range candidates {
			audioAccess := ""
			audioOpenFailed := false
			if source, ok := correlateCaptureSource(candidate, sources); ok {
				if len(source.Formats) == 0 {
					audioOpenFailed = true
				} else {
					probeCtx, probeCancel := context.WithTimeout(context.Background(), 2*time.Second)
					capture, openErr := audio.OpenNativeCapture(probeCtx, source, source.Formats[0], func([]byte) error {
						return nil
					})
					probeCancel()
					if openErr == nil {
						closeErr := capture.Close()
						audioOpenFailed = closeErr != nil
						if closeErr == nil {
							audioAccess = string(source.ID)
						}
					} else {
						audioOpenFailed = true
					}
				}
			}
			receiverReport := hollyland.DoctorAccess(audioAccess, candidate.TransientPaths["control"])
			if audioOpenFailed {
				receiverReport.OK = false
				receiverReport.Messages = append(receiverReport.Messages, "audio: native ordinary-user open failed")
				receiverReport.Hints = append(receiverReport.Hints, "close applications holding the microphone, then re-run doctor")
			}
			receiverReports = append(receiverReports, receiverReport)
		}
		report["receivers"] = receiverReports
		report["udev_rule"] = hollyland.UdevHint()
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
	if discovery, ok := report["receiver_discovery"]; ok {
		fmt.Printf("receiver_discovery=%v\n", discovery)
	}
	if receivers, ok := report["receivers"].([]hollyland.DoctorReport); ok {
		if len(receivers) == 0 {
			fmt.Println("receivers=none")
		}
		for index, receiver := range receivers {
			fmt.Printf("receiver[%d] access_ok=%v\n", index, receiver.OK)
			for _, message := range receiver.Messages {
				fmt.Printf("  - %s\n", message)
			}
			for _, hint := range receiver.Hints {
				fmt.Printf("  hint: %s\n", hint)
			}
		}
	}
	if rule, ok := report["udev_rule"]; ok {
		fmt.Printf("device-scoped udev example:\n%v\n", rule)
	}
	return client.ExitOK
}

func cmdBindings(args []string, jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if len(args) >= 1 && args[0] == "set" {
		// bindings set <id> <device> <control> <action>
		if len(args) < 5 {
			fmt.Fprintln(os.Stderr, "usage: micbridge bindings set <id> <device_id> <control> <action>")
			return client.ExitUsage
		}
		b := client.Binding{
			Schema: client.BindingSchema, ID: client.BindingID(args[1]), DeviceID: client.DeviceID(args[2]),
			Control: args[3], On: "pressed", Action: client.Action(args[4]), Enabled: true,
		}
		out, putErr := cl.PutBinding(ctx, b, "")
		if putErr != nil {
			return mapClientErr(putErr, jsonOut)
		}
		return printResult(jsonOut, fmt.Sprintf("binding %s ok", out.ID), out)
	}
	list, err := cl.ListBindings(ctx)
	if err != nil {
		return mapClientErr(err, jsonOut)
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
		return mapClientErr(err, jsonOut)
	}
	action := "start"
	if len(args) > 0 && args[0] == "arm" {
		action = "arm"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snap, err := cl.CreateRecording(ctx, action, "")
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	return printResult(jsonOut, fmt.Sprintf("recording %s state=%s", snap.SessionID, snap.State), snap)
}

func cmdStop(jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Use current session placeholder id.
	snap, err := cl.RecordingAction(ctx, "current", "stop", "")
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	return printResult(jsonOut, fmt.Sprintf("state=%s", snap.State), snap)
}

func cmdEvents(args []string, jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var cursor client.Sequence
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: micbridge events [cursor]")
		return client.ExitUsage
	}
	if len(args) == 1 {
		value, parseErr := strconv.ParseUint(args[0], 10, 64)
		if parseErr != nil {
			fmt.Fprintln(os.Stderr, "micbridge: event cursor must be an unsigned integer")
			return client.ExitUsage
		}
		cursor = client.Sequence(value)
	}
	ch, errs := cl.Events(ctx, cursor)
	for ch != nil || errs != nil {
		select {
		case env, ok := <-ch:
			if !ok {
				ch = nil
				continue
			}
			if jsonOut {
				_ = json.NewEncoder(os.Stdout).Encode(env)
			} else {
				fmt.Printf("%d %s %s\n", env.Sequence, env.Type, env.DeviceID)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil && err != context.Canceled {
				return mapClientErr(err, jsonOut)
			}
			if err == context.Canceled {
				return client.ExitCanceled
			}
			return client.ExitOK
		case <-ctx.Done():
			return client.ExitCanceled
		}
	}
	return client.ExitOK
}

func cmdSetup(jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	devs, err := cl.Devices(ctx)
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	if len(devs) == 0 {
		fmt.Fprintln(os.Stderr, "micbridge: no devices — start the daemon or connect a receiver")
		return client.ExitUnavailable
	}
	plan := ux.BuildSetupPlan(devs[0])
	bindings, bindingErr := cl.ListBindings(ctx)
	bindingReady := false
	if bindingErr == nil {
		for _, configured := range bindings {
			if configured.DeviceID == devs[0].Identity.DeviceID &&
				configured.Control == "primary" && configured.On == "pressed" &&
				configured.Action == client.ActionRecordingToggle && configured.Enabled {
				bindingReady = true
				plan.Binding = &configured
				break
			}
		}
	}
	plan.Complete = true
	for i := range plan.Steps {
		switch plan.Steps[i].ID {
		case "confirm_device":
			plan.Steps[i].Done = true
		case "review_capabilities", "permissions":
			plan.Steps[i].Done = !plan.Steps[i].Blocked
		case "bind_toggle":
			plan.Steps[i].Done = bindingReady
		}
		if !plan.Steps[i].Done || plan.Steps[i].Blocked {
			plan.Complete = false
		}
	}
	if jsonOut {
		return printResult(true, "", plan)
	}
	for i, s := range plan.Steps {
		fmt.Printf("%d. %s\n   %s\n   → %s\n", i+1, s.Title, s.Body, s.Recommended)
	}
	return client.ExitOK
}

func cmdUI(args []string, jsonOut bool) int {
	cl, err := newClient()
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	if !jsonOut && len(args) == 0 {
		if runErr := tui.Run(context.Background(), cl); runErr != nil {
			fmt.Fprintf(os.Stderr, "micbridge: ui: %v\n", runErr)
			return client.ExitUnavailable
		}
		return client.ExitOK
	}
	mode := "wide"
	if len(args) > 0 {
		mode = args[0]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := cl.Status(ctx)
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	snap := recording.Snapshot{State: recording.StateIdle}
	if rec, ok := st["recording"].(map[string]any); ok {
		if s, ok := rec["state"].(string); ok {
			snap.State = recording.State(s)
		}
		if id, ok := rec["session_id"].(string); ok {
			snap.SessionID = id
		}
	}
	h := audio.Snapshot{Level: audio.HealthOK}
	var view ux.RecorderView
	if mode == "narrow" {
		view = ux.NarrowView(snap, h)
	} else {
		view = ux.WideView(snap, h, "device")
	}
	return printResult(jsonOut, view.Message, view)
}

func cmdRecover(jsonOut bool) int {
	layout, err := stateLayout()
	if err != nil {
		return mapClientErr(err, jsonOut)
	}
	ids, err := recording.ScanRecoverable(layout.RecordingsDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: recover: %v\n", err)
		return client.ExitUnavailable
	}
	type result struct {
		SessionID string                    `json:"session_id"`
		Outcome   recording.RecoveryOutcome `json:"outcome"`
		Path      string                    `json:"path,omitempty"`
		Error     string                    `json:"error,omitempty"`
	}
	results := make([]result, 0, len(ids))
	failed := false
	for _, id := range ids {
		outcome, path, recoverErr := recording.RecoverSession(layout.RecordingsDir(), layout.JournalsDir(), id)
		item := result{SessionID: id, Outcome: outcome, Path: path}
		if recoverErr != nil {
			item.Error = recoverErr.Error()
			failed = true
		}
		results = append(results, item)
	}
	if jsonOut {
		code := printResult(true, "", map[string]any{"recoveries": results})
		if failed {
			return client.ExitConflict
		}
		return code
	}
	if len(results) == 0 {
		fmt.Println("no recoverable recordings")
		return client.ExitOK
	}
	for _, item := range results {
		fmt.Printf("%s\t%s\t%s\n", item.SessionID, item.Outcome, item.Path)
		if item.Error != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", item.Error)
		}
	}
	if failed {
		return client.ExitConflict
	}
	return client.ExitOK
}
