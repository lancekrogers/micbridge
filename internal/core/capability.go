package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CapabilityKey is a versioned capability name (e.g. audio.capture).
type CapabilityKey string

// Well-known capability keys from the design capability model.
const (
	CapAudioCapture             CapabilityKey = "audio.capture"
	CapControlPress             CapabilityKey = "control.press"
	CapControlRelease           CapabilityKey = "control.release"
	CapControlHold              CapabilityKey = "control.hold"
	CapRecordingTrigger         CapabilityKey = "recording.trigger"
	CapStatusBattery            CapabilityKey = "status.battery"
	CapStatusLink               CapabilityKey = "status.link"
	CapSettingNoiseCancellation CapabilityKey = "setting.noise_cancellation"
)

// Availability describes whether a capability can be used now.
type Availability string

const (
	// Available means the capability is usable under current policy.
	Available Availability = "available"
	// Unavailable means recognized but not usable (permissions, offline, …).
	Unavailable Availability = "unavailable"
	// Unsupported means the driver knows the device lacks this capability.
	Unsupported Availability = "unsupported"
	// Unverified means plausible but evidence is insufficient to expose.
	Unverified Availability = "unverified"
)

// EvidenceLevel follows the driver evidence taxonomy.
type EvidenceLevel string

const (
	EvidenceSpecified        EvidenceLevel = "specified"
	EvidenceMeasured         EvidenceLevel = "measured"
	EvidenceCorrelated       EvidenceLevel = "correlated"
	EvidenceCandidate        EvidenceLevel = "candidate"
	EvidenceMeasuredNegative EvidenceLevel = "measured_negative"
	EvidenceUnknown          EvidenceLevel = "unknown"
)

// Evidence is a reviewable claim backing a capability.
type Evidence struct {
	Level          EvidenceLevel `json:"level"`
	Summary        string        `json:"summary,omitempty"`
	ProfileVersion string        `json:"profile_version,omitempty"`
	// ArtifactDigest is an opaque content hash of an evidence artifact.
	// Raw private audio or personal identifiers must never be embedded.
	ArtifactDigest string         `json:"artifact_digest,omitempty"`
	Extra          map[string]any `json:"-"`
}

// SideEffect declares a known host or device side effect of using a capability.
type SideEffect struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Passive     bool   `json:"passive,omitempty"`
}

// Capability is a versioned, evidence-backed device feature.
type Capability struct {
	Schema            string         `json:"schema"`
	Key               CapabilityKey  `json:"key"`
	Availability      Availability   `json:"availability"`
	Mode              string         `json:"mode,omitempty"`
	UnavailableReason string         `json:"unavailable_reason,omitempty"`
	Evidence          Evidence       `json:"evidence"`
	SideEffects       []SideEffect   `json:"side_effects,omitempty"`
	DriverVersion     string         `json:"driver_version,omitempty"`
	Extra             map[string]any `json:"-"`
}

// Validate checks capability structure.
func (c Capability) Validate() error {
	if c.Schema == "" {
		return fmt.Errorf("core: capability schema is required")
	}
	if c.Schema != CapabilitySchema {
		return fmt.Errorf("core: unsupported capability schema %q", c.Schema)
	}
	if strings.TrimSpace(string(c.Key)) == "" {
		return fmt.Errorf("core: capability key is required")
	}
	switch c.Availability {
	case Available, Unavailable, Unsupported, Unverified:
	default:
		return fmt.Errorf("core: invalid availability %q", c.Availability)
	}
	switch c.Evidence.Level {
	case EvidenceSpecified, EvidenceMeasured, EvidenceCorrelated,
		EvidenceCandidate, EvidenceMeasuredNegative, EvidenceUnknown:
	default:
		return fmt.Errorf("core: invalid evidence level %q", c.Evidence.Level)
	}
	if c.Availability == Available {
		switch c.Evidence.Level {
		case EvidenceCandidate, EvidenceMeasuredNegative, EvidenceUnknown:
			return fmt.Errorf("core: capability %q cannot be available with evidence %q", c.Key, c.Evidence.Level)
		}
	}
	if c.Availability != Available && c.UnavailableReason == "" &&
		(c.Availability == Unavailable || c.Availability == Unsupported || c.Availability == Unverified) {
		return fmt.Errorf("core: capability %q requires unavailable_reason when not available", c.Key)
	}
	seen := map[string]struct{}{}
	for _, se := range c.SideEffects {
		if se.ID == "" {
			return fmt.Errorf("core: side effect id is required")
		}
		if _, ok := seen[se.ID]; ok {
			return fmt.Errorf("core: duplicate side effect %q", se.ID)
		}
		seen[se.ID] = struct{}{}
	}
	return nil
}

// Callable reports whether the capability may be invoked.
func (c Capability) Callable() bool {
	if c.Availability != Available {
		return false
	}
	switch c.Evidence.Level {
	case EvidenceSpecified, EvidenceMeasured:
		return true
	case EvidenceCorrelated:
		// Read-only by default; callers must check Mode.
		return c.Mode == "read" || c.Mode == "read_write" || c.Mode == ""
	default:
		return false
	}
}

// MarshalJSON preserves unknown fields.
func (c Capability) MarshalJSON() ([]byte, error) {
	type alias Capability
	b, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, c.Extra)
}

// UnmarshalJSON captures unknown fields.
func (c *Capability) UnmarshalJSON(data []byte) error {
	type alias Capability
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = Capability(a)
	extra, err := extractExtra(data, []string{
		"schema", "key", "availability", "mode", "unavailable_reason",
		"evidence", "side_effects", "driver_version",
	})
	if err != nil {
		return err
	}
	c.Extra = extra
	return nil
}
