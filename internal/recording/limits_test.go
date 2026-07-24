package recording_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/recording"
)

func TestCheckArmOutputRootAndDuration(t *testing.T) {
	dir := t.TempDir()
	p := recording.DefaultPolicy(dir)
	if err := p.CheckArm(); err != nil {
		t.Fatal(err)
	}
	p2 := recording.DefaultPolicy("")
	if err := p2.CheckArm(); err == nil {
		t.Fatal("expected output root error")
	}
	p.MaxDuration = time.Second
	if err := p.CheckDuration(2 * time.Second); err == nil {
		t.Fatal("expected max duration")
	}
}

func TestSourceLossNoSubstitution(t *testing.T) {
	p := recording.DefaultPolicy(t.TempDir())
	p.DisconnectGrace = time.Second
	loss := recording.SourceLoss{SourceID: "src_a", LostAt: time.Now()}
	fail, reason := p.ResolveSourceLoss(loss, time.Now())
	if fail || reason != "within_disconnect_grace" {
		t.Fatal(fail, reason)
	}
	fail, reason = p.ResolveSourceLoss(loss, time.Now().Add(2*time.Second))
	if !fail {
		t.Fatal(reason)
	}
	loss.ReconnectedID = "src_b"
	fail, reason = p.ResolveSourceLoss(loss, time.Now())
	if !fail || reason == "" {
		t.Fatal("should refuse other mic")
	}
	loss.ReconnectedID = "src_a"
	fail, _ = p.ResolveSourceLoss(loss, time.Now())
	if fail {
		t.Fatal("same id should reconnect")
	}
}

func TestDiskFullInjection(t *testing.T) {
	dir := t.TempDir()
	p := recording.Policy{
		OutputRoot: dir, MinFreeBytes: 1024,
		FreeBytes: func(string) (uint64, error) { return 512, nil },
	}
	err := p.CheckArm()
	if err == nil {
		t.Fatal("expected deterministic disk-space failure")
	}
	var le *recording.LimitError
	if !errors.As(err, &le) || le.Code != "disk_space" {
		t.Fatalf("want disk_space LimitError, got %T: %v", err, err)
	}
}

func TestFreeSpaceProbeFailureDoesNotInventDiskFull(t *testing.T) {
	dir := t.TempDir()
	p := recording.Policy{
		OutputRoot: dir, MinFreeBytes: 1024,
		FreeBytes: func(string) (uint64, error) { return 0, fmt.Errorf("unsupported filesystem") },
	}
	if err := p.CheckArm(); err != nil {
		t.Fatalf("best-effort probe should not invent disk full: %v", err)
	}
}

func TestOutputRootFailureIsActionable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := recording.DefaultPolicy(filepath.Join(root, "recordings"))
	err := p.CheckArm()
	var limitErr *recording.LimitError
	if !errors.As(err, &limitErr) || limitErr.Code != "output_root" {
		t.Fatalf("want output_root LimitError, got %T: %v", err, err)
	}
}
