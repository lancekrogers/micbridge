package storage

import "testing"

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
