package client_test

import (
	"context"
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
