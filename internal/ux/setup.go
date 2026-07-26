// Package ux implements guided reference-recorder flows over public contracts.
package ux

import (
	"fmt"
	"strings"

	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/pkg/client"
)

// SetupStep is one guided decision.
type SetupStep struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Recommended  string   `json:"recommended"`
	Consequences string   `json:"consequences"`
	ExpertNote   string   `json:"expert_note,omitempty"`
	Done         bool     `json:"done"`
	Blocked      bool     `json:"blocked"`
	Hints        []string `json:"hints,omitempty"`
}

// SetupPlan is the guided path from discovery to binding.
type SetupPlan struct {
	Device   client.Device   `json:"device"`
	Steps    []SetupStep     `json:"steps"`
	Binding  *client.Binding `json:"binding,omitempty"`
	Complete bool            `json:"complete"`
}

// BuildSetupPlan creates a one-decision-at-a-time plan for a device.
func BuildSetupPlan(dev client.Device) SetupPlan {
	p := SetupPlan{Device: dev}
	isLARKM2 := dev.Identity.DriverID == "hollyland-3547-0007"
	deviceTitle := "Confirm receiver"
	controlRequirement := "Audio capture and control press must be available"
	bindTitle := "Enable press-to-toggle recording"
	bindBody := "Map the primary press control to recording.toggle."
	bindConsequences := "Each press starts/stops an armed session."
	if isLARKM2 {
		deviceTitle = "Confirm LARK M2 receiver"
		controlRequirement = "Audio capture and the LARK M2 transmitter double-press control must be available"
		bindTitle = "Enable LARK M2 recording shortcut"
		bindBody = "Map a double press on either LARK M2 microphone to recording.toggle. A single press only toggles noise cancellation."
		bindConsequences = "Each transmitter double press starts/stops an armed session."
	}
	// Step 1: confirm identity
	p.Steps = append(p.Steps, SetupStep{
		ID: "confirm_device", Title: deviceTitle,
		Body: fmt.Sprintf("Found %s (%s)\nDriver %s %s",
			dev.Identity.DisplayName, dev.Identity.DeviceID, dev.Identity.DriverID, dev.Identity.DriverVersion),
		Recommended:  "Continue with this device",
		Consequences: "Micbridge will group audio and control under this stable ID",
		ExpertNote:   "Raw ALSA/evdev paths are not required; surfaces listed by kind only",
	})
	// Step 2: capabilities
	var capLines []string
	audioOK, pressOK := false, false
	for _, c := range dev.Capabilities {
		line := fmt.Sprintf("%s: %s", c.Key, c.Availability)
		if c.UnavailableReason != "" {
			line += " (" + c.UnavailableReason + ")"
		}
		capLines = append(capLines, line)
		if c.Key == client.CapAudioCapture && c.Callable() {
			audioOK = true
		}
		if c.Key == client.CapControlPress && c.Callable() {
			pressOK = true
		}
	}
	stepCap := SetupStep{
		ID: "review_capabilities", Title: "Review capabilities",
		Body:         strings.Join(capLines, "\n"),
		Recommended:  "Continue",
		Consequences: "Unavailable controls stay hidden from bindings",
	}
	if !audioOK || !pressOK {
		stepCap.Blocked = true
		stepCap.Hints = append(stepCap.Hints, controlRequirement)
	}
	p.Steps = append(p.Steps, stepCap)
	// Step 3: permissions
	p.Steps = append(p.Steps, SetupStep{
		ID: "permissions", Title: "Permissions",
		Body:         "Micbridge needs ordinary-user access to this receiver's audio and control surfaces only.",
		Recommended:  "Run: micbridge doctor",
		Consequences: "No root daemon; no broad input group membership",
		ExpertNote:   "Optional udev TAG+=uaccess for 3547:0007 only",
		Hints:        []string{"After fixing access, re-run setup"},
	})
	// Step 4: binding ack
	side := "host.volume_increment (passive Volume-Up)"
	p.Steps = append(p.Steps, SetupStep{
		ID: "bind_toggle", Title: bindTitle,
		Body:         bindBody,
		Recommended:  "Enable binding",
		Consequences: bindConsequences + " Side effect: " + side,
		ExpertNote:   "Bindings API: PUT /v1/bindings",
	})
	return p
}

// ApplyRecommendedBinding creates the default press→toggle binding when safe.
func ApplyRecommendedBinding(eng *binding.Engine, dev core.Device) (binding.Binding, error) {
	b := binding.Binding{
		Schema:   binding.Schema,
		ID:       binding.ID("bind_" + string(dev.Identity.DeviceID) + "_toggle"),
		DeviceID: dev.Identity.DeviceID,
		Control:  "primary",
		On:       "pressed",
		Action:   binding.ActionRecordingToggle,
		Enabled:  true,
	}
	return eng.Upsert(b, dev.Capabilities)
}
