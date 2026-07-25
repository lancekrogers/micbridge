// Package hollyland35470007 is the exact measured Linux driver for USB 3547:0007.
// Exact retail model remains unresolved; matching requires VID:PID + descriptor shape.
package hollyland35470007

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
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
	Schema          string                `json:"schema"`
	VID             string                `json:"vid"`
	PID             string                `json:"pid"`
	Manufacturer    string                `json:"manufacturer"`
	Product         string                `json:"product"`
	ModelExact      string                `json:"model_exact"` // empty = unresolved
	Interfaces      []int                 `json:"interface_numbers"`
	NonContiguousIF bool                  `json:"interfaces_are_non_contiguous"`
	RequireHID      bool                  `json:"require_hid"`
	RequireAudio    bool                  `json:"require_audio"`
	VendorIF        int                   `json:"vendor_interface"`
	EvidenceDigest  string                `json:"evidence_digest,omitempty"`
	Descriptors     []InterfaceDescriptor `json:"interface_descriptors"`
	ProfileVersion  string                `json:"profile_version"`
}

// InterfaceDescriptor is the sanitized, exact USB interface/alternate shape.
type InterfaceDescriptor struct {
	Number    int                  `json:"number"`
	Alternate int                  `json:"alternate"`
	Class     string               `json:"class"`
	Subclass  string               `json:"subclass"`
	Protocol  string               `json:"protocol"`
	Endpoints []EndpointDescriptor `json:"endpoints,omitempty"`
}

// EndpointDescriptor is the bounded endpoint shape used for exact matching.
type EndpointDescriptor struct {
	Address        string `json:"address"`
	Transfer       string `json:"transfer"`
	MaxPacketBytes int    `json:"max_packet_bytes"`
	Interval       int    `json:"interval"`
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
		EvidenceDigest:  "cbf97d1b4ccd3b2aee5fe52fc78efca9e8a2d19daf93aab1acbfa4b08801d536",
		Descriptors: []InterfaceDescriptor{
			{Number: 0, Alternate: 0, Class: "03", Subclass: "00", Protocol: "00", Endpoints: []EndpointDescriptor{
				{Address: "82", Transfer: "interrupt", MaxPacketBytes: 64, Interval: 1},
				{Address: "03", Transfer: "interrupt", MaxPacketBytes: 64, Interval: 1},
			}},
			{Number: 1, Alternate: 0, Class: "01", Subclass: "01", Protocol: "00"},
			{Number: 3, Alternate: 0, Class: "01", Subclass: "02", Protocol: "00"},
			{Number: 3, Alternate: 1, Class: "01", Subclass: "02", Protocol: "00", Endpoints: []EndpointDescriptor{
				{Address: "81", Transfer: "isochronous", MaxPacketBytes: 288, Interval: 1},
			}},
			{Number: 7, Alternate: 0, Class: "ff", Subclass: "f0", Protocol: "00", Endpoints: []EndpointDescriptor{
				{Address: "83", Transfer: "bulk", MaxPacketBytes: 64, Interval: 9},
				{Address: "02", Transfer: "bulk", MaxPacketBytes: 64, Interval: 9},
			}},
		},
		ProfileVersion: "1.0.0",
	}
}

// CandidateUSB is a sanitized discovery observation.
type CandidateUSB struct {
	VID              string
	PID              string
	Manufacturer     string
	Product          string
	InterfaceNums    []int
	HasHID           bool
	HasAudio         bool
	HasVendorIF      bool
	Descriptors      []InterfaceDescriptor
	DescriptorDigest string
	// Transient topology (never identity)
	BusDevice       string
	MalformedReason string
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
	if c.MalformedReason != "" {
		return MatchMalformed, c.MalformedReason
	}
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
	if strings.TrimSpace(c.Manufacturer) != p.Manufacturer ||
		strings.TrimSpace(c.Product) != p.Product {
		return MatchNearMiss, "3547:0007 manufacturer/product differs from measured profile"
	}
	if c.DescriptorDigest == "" || !strings.EqualFold(c.DescriptorDigest, p.EvidenceDigest) {
		return MatchFuture, "3547:0007 descriptor digest differs from measured profile"
	}
	if !sameDescriptors(p.Descriptors, c.Descriptors) {
		return MatchFuture, "3547:0007 interface/alternate/endpoint shape differs from measured profile"
	}
	// VID:PID match — require topology
	if p.RequireHID && !c.HasHID {
		return MatchNearMiss, "3547:0007 without HID surface"
	}
	if p.RequireAudio && !c.HasAudio {
		return MatchNearMiss, "3547:0007 without audio surface"
	}
	if p.VendorIF >= 0 && !c.HasVendorIF {
		return MatchNearMiss, "3547:0007 without measured vendor interface"
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

func sameDescriptors(a, b []InterfaceDescriptor) bool {
	if len(a) != len(b) {
		return false
	}
	canonical := func(values []InterfaceDescriptor) []InterfaceDescriptor {
		out := append([]InterfaceDescriptor(nil), values...)
		for i := range out {
			out[i].Class = strings.ToLower(out[i].Class)
			out[i].Subclass = strings.ToLower(out[i].Subclass)
			out[i].Protocol = strings.ToLower(out[i].Protocol)
			out[i].Endpoints = append([]EndpointDescriptor(nil), out[i].Endpoints...)
			sort.Slice(out[i].Endpoints, func(left, right int) bool {
				return out[i].Endpoints[left].Address < out[i].Endpoints[right].Address
			})
		}
		sort.Slice(out, func(left, right int) bool {
			if out[left].Number != out[right].Number {
				return out[left].Number < out[right].Number
			}
			return out[left].Alternate < out[right].Alternate
		})
		return out
	}
	left, right := canonical(a), canonical(b)
	return reflect.DeepEqual(left, right)
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
	return identityFor(serialHint, []byte(DriverID))
}

func identityFor(serialHint string, installationSalt []byte) core.Identity {
	attrs := map[string]string{
		"usb_vid_pid":  USBVendorID + ":" + USBProductID,
		"model_exact":  "unresolved",
		"manufacturer": DefaultProfile().Manufacturer,
	}
	id := core.DeviceID("dev_hl_" + USBVendorID + USBProductID)
	if serialHint != "" && serialHint != "present-redacted" {
		// The serial is evidence for correlation, never an identifier payload.
		// A per-installation HMAC makes IDs stable locally but unlinkable across
		// Micbridge installations.
		mac := hmac.New(sha256.New, installationSalt)
		_, _ = mac.Write([]byte(DriverID + "\x00" + serialHint))
		id = core.DeviceID(fmt.Sprintf("dev_hl_%x", mac.Sum(nil)[:12]))
		attrs["serial_present"] = "true"
		attrs["identity_stability"] = "hardware_correlated"
	} else {
		attrs["identity_stability"] = "ambiguous_without_serial"
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
