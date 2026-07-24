//go:build linux

package hollyland35470007_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lancekrogers/micbridge/internal/driver/hollyland35470007"
)

func TestDiscoverLinuxGroupsMeasuredUSBParent(t *testing.T) {
	root := t.TempDir()
	usbRoot := filepath.Join(root, "sys", "bus", "usb", "devices")
	inputRoot := filepath.Join(root, "sys", "class", "input")
	parent := filepath.Join(usbRoot, "1-2")
	mustWrite(t, filepath.Join(parent, "idVendor"), "3547\n")
	mustWrite(t, filepath.Join(parent, "idProduct"), "0007\n")
	mustWrite(t, filepath.Join(parent, "manufacturer"), hollyland35470007.DefaultProfile().Manufacturer+"\n")
	mustWrite(t, filepath.Join(parent, "product"), hollyland35470007.DefaultProfile().Product+"\n")
	mustWrite(t, filepath.Join(parent, "serial"), "private-test-serial\n")
	classes := map[int]string{0: "03", 1: "01", 3: "01", 7: "ff"}
	for number, class := range classes {
		dir := filepath.Join(usbRoot, "1-2:1."+string(rune('0'+number)))
		mustWrite(t, filepath.Join(dir, "bInterfaceNumber"), twoHex(number)+"\n")
		mustWrite(t, filepath.Join(dir, "bInterfaceClass"), class+"\n")
	}
	eventDir := filepath.Join(inputRoot, "event12")
	if err := os.MkdirAll(eventDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(parent, filepath.Join(eventDir, "device")); err != nil {
		t.Fatal(err)
	}

	candidates, err := hollyland35470007.DiscoverLinux(context.Background(), usbRoot, inputRoot, filepath.Join(root, "dev"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%v", candidates)
	}
	candidate := candidates[0]
	if got := candidate.Attributes["interfaces"]; got != "0,1,3,7" {
		t.Fatalf("interfaces=%q", got)
	}
	if got := candidate.TransientPaths["control"]; got != filepath.Join(root, "dev", "input", "event12") {
		t.Fatalf("control=%q", got)
	}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func twoHex(value int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(value>>4)&0xf], digits[value&0xf]})
}
