package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/event"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/storage"
	"github.com/lancekrogers/micbridge/pkg/client"
)

func TestVersionAndHelp(t *testing.T) {
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

func TestServeOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MICBRIDGE_STATE_DIR", dir)
	t.Setenv("MICBRIDGE_SOCK", filepath.Join(dir, "s.sock"))
	t.Setenv("MICBRIDGE_SERVE_ONCE", "1")
	code := run([]string{"serve"})
	if code != client.ExitOK {
		t.Fatal(code)
	}
	time.Sleep(10 * time.Millisecond)
	_ = os.Remove(filepath.Join(dir, "s.sock"))
}
