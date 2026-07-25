package client_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/testsupport"
	"github.com/lancekrogers/micbridge/pkg/client"
)

func startDaemon(t *testing.T) (string, *api.Server) {
	t.Helper()
	layout := testsupport.TempRoot(t)
	bus := event.NewBus(event.WithIDs(idgen.NewSeeded("sdk")))
	t.Cleanup(bus.Close)
	mach := recording.NewMachine(nil, idgen.NewSeeded("m"), &recording.FakeSink{}, recording.Limits{})
	t.Cleanup(mach.Close)
	srv := &api.Server{
		Layout:   layout,
		Bus:      bus,
		Bindings: binding.New(nil, filepath.Join(layout.Root, "b.json")),
		Machine:  mach,
		Devices: func() []core.Device {
			return []core.Device{{
				Schema: core.DeviceSchema,
				Identity: core.Identity{
					Schema: core.IdentitySchema, DeviceID: "dev_1",
					DriverID: "fake", DriverVersion: "1",
				},
			}}
		},
	}
	sock := filepath.Join(layout.Root, "micbridge.sock")
	if err := srv.Start(sock); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	return sock, srv
}

func TestSDKStatusDevicesCancel(t *testing.T) {
	sock, _ := startDaemon(t)
	cl := client.New(sock)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	st, err := cl.Status(ctx)
	if err != nil || st["name"] != "micbridge" {
		t.Fatalf("%v %v", st, err)
	}
	devs, err := cl.Devices(ctx)
	if err != nil || len(devs) != 1 {
		t.Fatal(err, devs)
	}
	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	_, err = cl.Status(cctx)
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func TestDaemonUnavailable(t *testing.T) {
	cl := client.New(filepath.Join(t.TempDir(), "no.sock"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := cl.Status(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEventStream(t *testing.T) {
	sock, srv := startDaemon(t)
	cl := client.New(sock)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, errs := cl.Events(ctx, 0)
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _ = srv.Bus.Publish("control.pressed", "dev_1", nil)
	}()
	select {
	case env := <-ch:
		if env.Type != "control.pressed" {
			t.Fatalf("%+v", env)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestEventStreamSurfacesMalformedSSE(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "malformed.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {not-json}\n\n"))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, errs := client.New(socket).Events(ctx, 0)
	select {
	case streamErr := <-errs:
		if streamErr == nil {
			t.Fatal("malformed event was silently discarded")
		}
	case <-ctx.Done():
		t.Fatal("malformed event did not surface an error")
	}
}

func TestPublicModelsPreserveUnknownFields(t *testing.T) {
	raw := []byte(`{
		"schema":"micbridge.device/v1",
		"identity":{"schema":"micbridge.identity/v1","device_id":"dev_1","driver_id":"fake","driver_version":"1","future_identity":true},
		"surfaces":[{"id":"audio","kind":"audio.capture","future_surface":{"x":1}}],
		"capabilities":[{"schema":"micbridge.capability/v1","key":"audio.capture","availability":"available","evidence":{"level":"measured","future_evidence":7},"future_capability":"x"}],
		"future_device":[1,2,3]
	}`)
	var device client.Device
	if err := json.Unmarshal(raw, &device); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(device)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if _, ok := roundTrip["future_device"]; !ok {
		t.Fatalf("lost public-model extension: %s", encoded)
	}
	identity := roundTrip["identity"].(map[string]any)
	if _, ok := identity["future_identity"]; !ok {
		t.Fatalf("lost identity extension: %s", encoded)
	}
	surface := roundTrip["surfaces"].([]any)[0].(map[string]any)
	if _, ok := surface["future_surface"]; !ok {
		t.Fatalf("lost surface extension: %s", encoded)
	}
	capability := roundTrip["capabilities"].([]any)[0].(map[string]any)
	if _, ok := capability["future_capability"]; !ok {
		t.Fatalf("lost capability extension: %s", encoded)
	}
	evidence := capability["evidence"].(map[string]any)
	if _, ok := evidence["future_evidence"]; !ok {
		t.Fatalf("lost evidence extension: %s", encoded)
	}
}
