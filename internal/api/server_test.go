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
	raw := []byte(`{"schema":"micbridge.binding/v1","id":"b1","device_id":"dev_1","control":"primary","on":"pressed","action":"recording.toggle","enabled":true}`)
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
	_ = srv
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
