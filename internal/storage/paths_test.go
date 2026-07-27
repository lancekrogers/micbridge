package storage

import (
	"path/filepath"
	"testing"
)

func TestNewLayoutRequiresRoot(t *testing.T) {
	if _, err := NewLayout(""); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestLayoutSubpaths(t *testing.T) {
	l, err := NewLayout("/tmp/micbridge-state")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := l.RecordingsDir(), "/tmp/micbridge-state/recordings"; got != want {
		t.Fatalf("RecordingsDir = %q, want %q", got, want)
	}
	if got, want := l.JournalsDir(), "/tmp/micbridge-state/journals"; got != want {
		t.Fatalf("JournalsDir = %q, want %q", got, want)
	}
}

func TestDefaultStateRootUsesXDGStateHome(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	root, err := DefaultStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "micbridge"); root != want {
		t.Fatalf("default state root = %q, want %q", root, want)
	}
}

func TestNewLayoutMakesRelativeRootAbsolute(t *testing.T) {
	layout, err := NewLayout("relative-state")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(layout.Root) {
		t.Fatalf("state root is not absolute: %q", layout.Root)
	}
}
