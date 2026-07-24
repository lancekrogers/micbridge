// Package hollyland35470007 is the exact measured Linux driver for USB 3547:0007.
// Exact retail model remains unresolved; matching requires VID:PID + descriptor shape.
package hollyland35470007

import (
	"fmt"
	"strings"

	"github.com/lancekrogers/micbridge/internal/core"
)

const (
	DriverID      = "hollyland-3547-0007"
	DriverVersion = "1.0.0"
	USBVendorID   = "3547"
	USBProductID  = "0007"
)

// Profile is sanitized, versioned match evidence (no private paths/audio).
type Profile struct {
	Schema          string `json:"schema"`
	VID             string `json:"vid"`
	PID             string `json:"pid"`
	Manufacturer    string `json:"manufacturer"`
	Product         string `json:"product"`
	ModelExact      string `json:"model_exact"` // empty = unresolved
	Interfaces      []int  `json:"interface_numbers"`
	NonContiguousIF bool   `json:"interfaces_are_non_contiguous"`
	RequireHID      bool   `json:"require_hid"`
	RequireAudio    bool   `json:"require_audio"`
	VendorIF        int    `json:"vendor_interface"`
	EvidenceDigest  string `json:"evidence_digest,omitempty"`
	ProfileVersion  string `json:"profile_version"`
}

// DefaultProfile returns the measured v1 profile shape.
func DefaultProfile() Profile {
	return Profile{
		Schema:          "micbridge.driver.profile/v1",
		VID:             USBVendorID,
		PID:             USBProductID,
		Manufacturer:    "Shenzhen Hollyland Technology Co.,Ltd",
		Product:         "Wireless microphone",
		ModelExact:      "", // unresolved by design
		Interfaces:      []int{0, 1, 3, 7},
		NonContiguousIF: true,
		RequireHID:      true,
		RequireAudio:    true,
		VendorIF:        7,
		EvidenceDigest:  "explore/hollyland-3547-0007-usb",
		ProfileVersion:  "1.0.0",
	}
}

// CandidateUSB is a sanitized discovery observation.
type CandidateUSB struct {
	VID           string
	PID           string
	Manufacturer  string
	Product       string
	InterfaceNums []int
	HasHID        bool
	HasAudio      bool
	HasVendorIF   bool
	// Transient topology (never identity)
	BusDevice string
}

// MatchResult classifies profile matching.
type MatchResult string

const (
	MatchExact     MatchResult = "exact"
	MatchNearMiss  MatchResult = "near_miss"
	MatchMalformed MatchResult = "malformed"
	MatchFuture    MatchResult = "future_revision"
	MatchNo        MatchResult = "no_match"
)

// EvaluateMatch returns exact only for VID:PID + measured shape.
func EvaluateMatch(p Profile, c CandidateUSB) (MatchResult, string) {
	if c.VID == "" || c.PID == "" {
		return MatchMalformed, "missing vid/pid"
	}
	vid := strings.ToLower(strings.TrimPrefix(c.VID, "0x"))
	pid := strings.ToLower(strings.TrimPrefix(c.PID, "0x"))
	if vid != strings.ToLower(p.VID) || pid != strings.ToLower(p.PID) {
		// brand-only substring must never match
		if strings.Contains(strings.ToLower(c.Manufacturer), "hollyland") {
			return MatchNearMiss, "hollyland brand without exact 3547:0007"
		}
		return MatchNo, "vid/pid mismatch"
	}
	// VID:PID match — require topology
	if p.RequireHID && !c.HasHID {
		return MatchNearMiss, "3547:0007 without HID surface"
	}
	if p.RequireAudio && !c.HasAudio {
		return MatchNearMiss, "3547:0007 without audio surface"
	}
	if !sameInterfaceSet(p.Interfaces, c.InterfaceNums) {
		// possible future revision
		if len(c.InterfaceNums) > 0 {
			return MatchFuture, "vid/pid match but interface set differs from measured profile"
		}
		return MatchNearMiss, "vid/pid match without interface evidence"
	}
	return MatchExact, "measured descriptor shape"
}

func sameInterfaceSet(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[int]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
		if m[x] < 0 {
			return false
		}
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

// IdentityFor builds a stable core identity; serial redacted/optional.
func IdentityFor(serialHint string) core.Identity {
	attrs := map[string]string{
		"usb_vid_pid":  USBVendorID + ":" + USBProductID,
		"model_exact":  "unresolved",
		"manufacturer": DefaultProfile().Manufacturer,
	}
	id := core.DeviceID("dev_hl_" + USBVendorID + USBProductID)
	if serialHint != "" && serialHint != "present-redacted" {
		// only use opaque hash-like serials; never raw personal paths
		id = core.DeviceID("dev_hl_" + sanitizeID(serialHint))
		attrs["serial_present"] = "true"
	}
	return core.Identity{
		Schema:        core.IdentitySchema,
		DeviceID:      id,
		DriverID:      DriverID,
		DriverVersion: DriverVersion,
		DisplayName:   "Hollyland wireless mic receiver (3547:0007)",
		Attributes:    attrs,
	}
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "unknown"
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

// Capabilities returns evidence-backed capability list.
func Capabilities() []core.Capability {
	return []core.Capability{
		{
			Schema: core.CapabilitySchema, Key: core.CapAudioCapture,
			Availability:  core.Available,
			Evidence:      core.Evidence{Level: core.EvidenceMeasured, Summary: "2ch 48kHz USB Audio", ProfileVersion: DriverVersion},
			DriverVersion: DriverVersion,
		},
		{
			Schema: core.CapabilitySchema, Key: core.CapControlPress,
			Availability:  core.Available,
			Evidence:      core.Evidence{Level: core.EvidenceMeasured, Summary: "HID 06 01 00 → KEY_VOLUMEUP", ProfileVersion: DriverVersion},
			SideEffects:   []core.SideEffect{{ID: "host.volume_increment", Passive: true, Description: "Consumer Control Volume Increment"}},
			DriverVersion: DriverVersion,
		},
		{
			Schema: core.CapabilitySchema, Key: core.CapControlRelease,
			Availability:  core.Available,
			Evidence:      core.Evidence{Level: core.EvidenceMeasured, Summary: "HID 06 00 00", ProfileVersion: DriverVersion},
			DriverVersion: DriverVersion,
		},
		{
			Schema: core.CapabilitySchema, Key: core.CapSettingNoiseCancellation,
			Availability:      core.Unsupported,
			UnavailableReason: "transmitter NC not host-visible in bounded trials",
			Evidence:          core.Evidence{Level: core.EvidenceMeasuredNegative, Summary: "no HID/evdev/usbmon event"},
			DriverVersion:     DriverVersion,
		},
	}
}

// ErrNearMiss is returned when match is close but not exact.
var ErrNearMiss = fmt.Errorf("hollyland: near-miss match")
