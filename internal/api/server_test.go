package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/storage"
	"github.com/lancekrogers/micbridge/internal/testsupport"
)

func startTestServer(t *testing.T) (*api.Server, string) {
	t.Helper()
	layout := testsupport.TempRoot(t)
	bus := event.NewBus(event.WithIDs(idgen.NewSeeded("api")))
	t.Cleanup(bus.Close)
	eng := binding.New(nil, filepath.Join(layout.Root, "bindings.json"))
	mach := recording.NewMachine(nil, idgen.NewSeeded("m"), &recording.FakeSink{}, recording.Limits{})
	t.Cleanup(mach.Close)
	srv := &api.Server{
		Layout:   layout,
		Bus:      bus,
		Bindings: eng,
		Machine:  mach,
		Devices: func() []core.Device {
			return []core.Device{{
				Schema: core.DeviceSchema,
				Identity: core.Identity{
					Schema: core.IdentitySchema, DeviceID: "dev_test",
					DriverID: "fake", DriverVersion: "1.0.0",
				},
				Capabilities: []core.Capability{{
					Schema: core.CapabilitySchema, Key: core.CapControlPress,
					Availability: core.Available,
					Evidence:     core.Evidence{Level: core.EvidenceMeasured},
				}},
			}}
		},
	}
	sock := filepath.Join(layout.Root, "micbridge.sock")
	if err := srv.Start(sock); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	// permission check
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", fi.Mode().Perm())
	}
	return srv, sock
}

func TestStatusAndDevices(t *testing.T) {
	_, sock := startTestServer(t)
	client := api.DialHTTP(sock)
	resp, err := client.Get("http://unix/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	resp, err = client.Get("http://unix/v1/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	devs := body["devices"].([]any)
	if len(devs) != 1 {
		t.Fatalf("%v", body)
	}
}

func TestIdempotencyAndSchemaError(t *testing.T) {
	_, sock := startTestServer(t)
	client := api.DialHTTP(sock)
	raw := []byte(`{"schema":"micbridge.binding/v1","id":"b1","device_id":"dev_test","control":"primary","on":"pressed","action":"recording.toggle","enabled":true}`)
	req, _ := http.NewRequest(http.MethodPut, "http://unix/v1/bindings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body1, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	req2, _ := http.NewRequest(http.MethodPut, "http://unix/v1/bindings", bytes.NewReader(raw))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", "idem-1")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if string(body1) != string(body2) {
		t.Fatalf("idem mismatch %s vs %s", body1, body2)
	}

	bad, _ := http.NewRequest(http.MethodPut, "http://unix/v1/bindings", bytes.NewReader([]byte(`{"id":`)))
	resp3, err := client.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp3.Body.Close() }()
	if resp3.StatusCode != 400 {
		t.Fatalf("status %d", resp3.StatusCode)
	}
}

func TestPathContainment(t *testing.T) {
	srv, sock := startTestServer(t)
	client := api.DialHTTP(sock)
	raw := []byte(`{"action":"arm","output_path":"../../etc/passwd"}`)
	req, _ := http.NewRequest(http.MethodPost, "http://unix/v1/recordings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Fatalf("want path error, got %d", resp.StatusCode)
	}
	outside := t.TempDir()
	link := filepath.Join(srv.Layout.RecordingsDir(), "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	raw = []byte(`{"action":"arm","output_path":"linked/escape.wav"}`)
	req, _ = http.NewRequest(http.MethodPost, "http://unix/v1/recordings", bytes.NewReader(raw))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("symlink escape status=%d", resp.StatusCode)
	}
}

func TestSelectedOutputFlowsFromAPIToWAVSink(t *testing.T) {
	layout := testsupport.TempRoot(t)
	bus := event.NewBus()
	t.Cleanup(bus.Close)
	format := recording.WAVFormat{SampleRate: 48000, Channels: 1, BitsPerSample: 16}
	sink := recording.NewWAVSink(
		layout.RecordingsDir(), layout.JournalsDir(), format,
		recording.DefaultPolicy(layout.RecordingsDir()),
		recording.SyntheticPCMStarter(format, 0),
	)
	machine := recording.NewMachine(nil, idgen.NewSeeded("api-output"), sink, recording.Limits{})
	t.Cleanup(machine.Close)
	server := &api.Server{
		Layout: layout, Bus: bus, Bindings: binding.New(nil, ""), Machine: machine,
		Devices: func() []core.Device { return []core.Device{} },
	}
	socket := filepath.Join(layout.Root, "output.sock")
	if err := server.Start(socket); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })
	httpClient := api.DialHTTP(socket)
	resp, err := httpClient.Post("http://unix/v1/recordings", "application/json",
		bytes.NewBufferString(`{"action":"start","output_path":"nested/my_recording.wav"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started recording.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || started.State != recording.StateRecording {
		t.Fatalf("start status=%d snapshot=%+v", resp.StatusCode, started)
	}
	resp, err = httpClient.Post("http://unix/v1/recordings/"+started.SessionID+"/actions",
		"application/json", bytes.NewBufferString(`{"action":"stop"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop status=%d", resp.StatusCode)
	}
	target := filepath.Join(layout.RecordingsDir(), "nested", "my_recording.wav")
	if err := recording.ValidateWAV(target); err != nil {
		t.Fatalf("API-selected output missing/invalid: %v", err)
	}
}

func TestEventResume(t *testing.T) {
	srv, sock := startTestServer(t)
	_, _ = srv.Bus.Publish("device.connected", "dev_test", nil)
	_, _ = srv.Bus.Publish("device.ready", "dev_test", nil)

	client := api.DialHTTP(sock)
	// Cursor 0 live-only; publish after subscribe.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/events?cursor=0", nil)
	// Use a client without short timeout for streaming.
	streamClient := &http.Client{Transport: client.Transport}
	resp, err := streamClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = srv.Bus.Publish("control.pressed", "dev_test", map[string]any{"control": "primary"})
	}()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	if n == 0 {
		t.Fatal("expected SSE data")
	}
	cancel()
}

func TestStaleSocketReplace(t *testing.T) {
	layout, err := storage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(layout.Root, "s.sock")
	// First server
	s1 := &api.Server{Layout: layout, Bus: event.NewBus()}
	if err := s1.Start(sock); err != nil {
		t.Fatal(err)
	}
	_ = s1.Close(context.Background())
	// Second server replaces stale socket
	s2 := &api.Server{Layout: layout, Bus: event.NewBus()}
	if err := s2.Start(sock); err != nil {
		t.Fatal(err)
	}
	_ = s2.Close(context.Background())
}

func TestLiveSocketCannotBeTakenOver(t *testing.T) {
	layout, err := storage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(layout.Root, "live.sock")
	s1 := &api.Server{Layout: layout, Bus: event.NewBus()}
	if err := s1.Start(sock); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s1.Close(context.Background()) }()
	s2 := &api.Server{Layout: layout, Bus: event.NewBus()}
	if err := s2.Start(sock); err == nil {
		_ = s2.Close(context.Background())
		t.Fatal("second server took over live socket")
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("first socket removed: %v", err)
	}
}

func TestRecordingMutationsAreStrictAndSessionScoped(t *testing.T) {
	_, sock := startTestServer(t)
	client := api.DialHTTP(sock)
	for _, raw := range []string{`{not-json`, `{}`, `{"action":"arm","unknown":true}`, `{"action":"arm"} {}`} {
		resp, err := client.Post("http://unix/v1/recordings", "application/json", bytes.NewBufferString(raw))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q: status=%d", raw, resp.StatusCode)
		}
	}
	resp, err := client.Post("http://unix/v1/recordings", "application/json", bytes.NewBufferString(`{"action":"arm"}`))
	if err != nil {
		t.Fatal(err)
	}
	var armed recording.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&armed); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if armed.SessionID == "" {
		t.Fatal("missing session id")
	}
	resp, err = client.Post("http://unix/v1/recordings/not-current/actions", "application/json", bytes.NewBufferString(`{"action":"stop"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("wrong session status=%d", resp.StatusCode)
	}
}

func TestIdempotencyIsRequestScopedAndSnapshotUsesStableJSON(t *testing.T) {
	_, sock := startTestServer(t)
	client := api.DialHTTP(sock)
	rawBinding := []byte(`{"schema":"micbridge.binding/v1","id":"b1","device_id":"dev_test","control":"primary","on":"pressed","action":"recording.toggle","enabled":true}`)
	req, _ := http.NewRequest(http.MethodPut, "http://unix/v1/bindings", bytes.NewReader(rawBinding))
	req.Header.Set("Idempotency-Key", "shared")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	req, _ = http.NewRequest(http.MethodPost, "http://unix/v1/recordings", bytes.NewBufferString(`{"action":"arm"}`))
	req.Header.Set("Idempotency-Key", "shared")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("mismatched key status=%d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, "http://unix/v1/recordings", bytes.NewBufferString(`{"action":"arm"}`))
	req.Header.Set("Idempotency-Key", "recording")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if _, ok := raw["session_id"]; !ok {
		t.Fatalf("missing snake_case schema: %v", raw)
	}
	if _, exists := raw["SessionID"]; exists {
		t.Fatalf("leaked Go field name: %v", raw)
	}
}
