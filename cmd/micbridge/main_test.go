package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionSmoke(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("version: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "micbridge") {
		t.Fatalf("version output missing name: %q", out)
	}
}

func TestStatusSmokeWithTempState(t *testing.T) {
	bin := buildBinary(t)
	state := t.TempDir()
	cmd := exec.Command(bin, "status")
	cmd.Env = append(os.Environ(), "MICBRIDGE_STATE_DIR="+state)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, `"healthy": true`) {
		t.Fatalf("expected healthy status, got: %s", s)
	}
	if !strings.Contains(s, state) {
		t.Fatalf("expected state dir %q in output: %s", state, s)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "micbridge")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v\n%s", err, stderr.String())
	}
	return bin
}
