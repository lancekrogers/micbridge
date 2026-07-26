//go:build linux

package hollyland35470007_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	// Keep the wrong generic collection lexically before the semantic one so
	// this regression fails if discovery ever returns to first-child guessing.
	addInputFixture(t, inputRoot, parent, "event1", "Wireless microphone", "9", "0")
	addInputFixture(t, inputRoot, parent, "event12", "Wireless microphone Consumer Control", "13", volumeUpBitmap())

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

func TestDiscoverLinuxRejectsAmbiguousConsumerControls(t *testing.T) {
	root := t.TempDir()
	usbRoot := filepath.Join(root, "sys", "bus", "usb", "devices")
	inputRoot := filepath.Join(root, "sys", "class", "input")
	parent := filepath.Join(usbRoot, "1-2")
	mustWrite(t, filepath.Join(parent, "idVendor"), "3547\n")
	mustWrite(t, filepath.Join(parent, "idProduct"), "0007\n")
	mustWrite(t, filepath.Join(parent, "manufacturer"), hollyland35470007.DefaultProfile().Manufacturer+"\n")
	mustWrite(t, filepath.Join(parent, "product"), hollyland35470007.DefaultProfile().Product+"\n")
	classes := map[int]string{0: "03", 1: "01", 3: "01", 7: "ff"}
	for number, class := range classes {
		dir := filepath.Join(usbRoot, "1-2:1."+string(rune('0'+number)))
		mustWrite(t, filepath.Join(dir, "bInterfaceNumber"), twoHex(number)+"\n")
		mustWrite(t, filepath.Join(dir, "bInterfaceClass"), class+"\n")
	}
	addInputFixture(t, inputRoot, parent, "event12", "Wireless microphone Consumer Control", "13", volumeUpBitmap())
	addInputFixture(t, inputRoot, parent, "event13", "Wireless microphone Consumer Control", "13", volumeUpBitmap())

	_, err := hollyland35470007.DiscoverLinux(context.Background(), usbRoot, inputRoot, filepath.Join(root, "dev"))
	if err == nil || !strings.Contains(err.Error(), "2 Consumer Control inputs") {
		t.Fatalf("ambiguous controls were accepted: %v", err)
	}
}

func addInputFixture(t *testing.T, inputRoot, parent, event, name, ev, key string) {
	t.Helper()
	inputDevice := filepath.Join(parent, filepath.Base(parent)+":1.0", "input", event+"-device")
	mustWrite(t, filepath.Join(inputDevice, "name"), name+"\n")
	mustWrite(t, filepath.Join(inputDevice, "capabilities", "ev"), ev+"\n")
	mustWrite(t, filepath.Join(inputDevice, "capabilities", "key"), key+"\n")
	eventDir := filepath.Join(inputRoot, event)
	if err := os.MkdirAll(eventDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inputDevice, filepath.Join(eventDir, "device")); err != nil {
		t.Fatal(err)
	}
}

func volumeUpBitmap() string {
	// KEY_VOLUMEUP=115 is bit 51 of the second 64-bit word.
	return "8000000000000 0"
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
