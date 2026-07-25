package hollyland35470007

import (
	"os"
	"path/filepath"
	"strings"
)

// DoctorReport is actionable ordinary-user access diagnostics (no root required).
type DoctorReport struct {
	OK       bool              `json:"ok"`
	Messages []string          `json:"messages"`
	Hints    []string          `json:"hints"`
	Paths    map[string]string `json:"paths,omitempty"`
}

// DoctorAccess checks device-scoped paths without requiring broad input group.
func DoctorAccess(audioPath, controlPath string) DoctorReport {
	r := DoctorReport{OK: true, Paths: map[string]string{}}
	check := func(label, path string) {
		if path == "" {
			r.Messages = append(r.Messages, label+": path not configured")
			r.OK = false
			r.Hints = append(r.Hints, "connect the 3547:0007 receiver and re-run doctor")
			return
		}
		if strings.HasPrefix(path, string(os.PathSeparator)) {
			r.Paths[label] = filepath.Base(path)
		} else {
			r.Paths[label] = path
		}
		if label == "audio" && !strings.HasPrefix(path, string(os.PathSeparator)) {
			r.Messages = append(r.Messages, label+": correlated native source")
			return
		}
		fi, err := os.Stat(path)
		if err != nil {
			r.OK = false
			r.Messages = append(r.Messages, label+": device node is missing or inaccessible")
			r.Hints = append(r.Hints, "install udev rule that grants the active seat access only to this device node")
			return
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			r.OK = false
			r.Messages = append(r.Messages, label+": ordinary-user open failed")
			r.Hints = append(r.Hints, "install the device-scoped udev uaccess rule shown by doctor")
			return
		}
		_ = file.Close()
		// world-writable is a security smell
		if fi.Mode().Perm()&0o002 != 0 {
			r.OK = false
			r.Messages = append(r.Messages, label+": world-writable device node is not acceptable")
		} else {
			r.Messages = append(r.Messages, label+": present")
		}
	}
	check("audio", audioPath)
	check("control", controlPath)
	r.Hints = append(r.Hints, "Micbridge runs as the ordinary user; do not add a root daemon or broad input group membership")
	r.Hints = append(r.Hints, "Receiver Volume-Up is a passive side effect (host.volume_increment); exclusive grab is off by default")
	return r
}

// UdevHint returns a sample device-scoped rule text (not installed automatically).
func UdevHint() string {
	return strings.TrimSpace(`
# Example only — review before installing. Scope to 3547:0007.
SUBSYSTEM=="sound", ATTRS{idVendor}=="3547", ATTRS{idProduct}=="0007", TAG+="uaccess"
SUBSYSTEM=="input", ATTRS{idVendor}=="3547", ATTRS{idProduct}=="0007", TAG+="uaccess"
`)
}
