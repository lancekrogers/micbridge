package core

import "testing"

func TestVersionIsNonEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	if Name != "micbridge" {
		t.Fatalf("Name = %q, want micbridge", Name)
	}
}
