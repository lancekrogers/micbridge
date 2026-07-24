package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Schema versions for core identity/capability envelopes.
const (
	IdentitySchema    = "micbridge.identity/v1"
	CapabilitySchema  = "micbridge.capability/v1"
	DeviceSchema      = "micbridge.device/v1"
)

// DeviceID is a Micbridge-stable device identifier. It must never encode
// volatile OS paths (ALSA indexes, eventN nodes, USB bus/device numbers).
type DeviceID string

// Validate reports whether id is well-formed.
func (id DeviceID) Validate() error {
	s := string(id)
	if s == "" {
		return fmt.Errorf("core: device_id is required")
	}
	if strings.ContainsAny(s, "/\\ \t\n") {
		return fmt.Errorf("core: device_id must not contain path or whitespace characters")
	}
	if looksLikeVolatilePath(s) {
		return fmt.Errorf("core: device_id must not be a volatile OS path")
	}
	return nil
}

func looksLikeVolatilePath(s string) bool {
	lower := strings.ToLower(s)
	prefixes := []string{
		"/dev/", "hw:", "plughw:", "card", "event", "hidraw", "bus/",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return strings.Contains(lower, "/dev/input/") ||
		strings.Contains(lower, "alsa:") ||
		strings.Contains(lower, "usb-") && strings.Contains(lower, ":")
}

// ConnectionID identifies one live attachment instance. It may change across
// reconnects and must not be used as stable identity.
type ConnectionID string

// SurfaceKind classifies a physical or logical surface on a device.
type SurfaceKind string

const (
	SurfaceAudioCapture SurfaceKind = "audio.capture"
	SurfaceControl      SurfaceKind = "control"
	SurfaceVendor       SurfaceKind = "vendor"
	SurfaceUnknown      SurfaceKind = "unknown"
)

// Surface is a grouped physical surface under a stable device identity.
// TransientPath is connection-scoped and never part of stable equality.
type Surface struct {
	ID            string         `json:"id"`
	Kind          SurfaceKind    `json:"kind"`
	Label         string         `json:"label,omitempty"`
	TransientPath string         `json:"transient_path,omitempty"`
	Extra         map[string]any `json:"-"`
}

// Identity is the stable, vendor-neutral device identity.
type Identity struct {
	Schema         string            `json:"schema"`
	DeviceID       DeviceID          `json:"device_id"`
	DriverID       string            `json:"driver_id"`
	DriverVersion  string            `json:"driver_version"`
	DisplayName    string            `json:"display_name,omitempty"`
	Serial         string            `json:"serial,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	InstallationAlias string         `json:"installation_alias,omitempty"`
	// Extra preserves unknown JSON fields for forward compatibility.
	Extra map[string]any `json:"-"`
}

// Validate checks required identity fields.
func (id Identity) Validate() error {
	if id.Schema == "" {
		return fmt.Errorf("core: identity schema is required")
	}
	if id.Schema != IdentitySchema {
		return fmt.Errorf("core: unsupported identity schema %q", id.Schema)
	}
	if err := id.DeviceID.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(id.DriverID) == "" {
		return fmt.Errorf("core: driver_id is required")
	}
	if strings.TrimSpace(id.DriverVersion) == "" {
		return fmt.Errorf("core: driver_version is required")
	}
	return nil
}

// EqualStable reports whether two identities share stable fields (ignoring
// display-only and connection-scoped data).
func (id Identity) EqualStable(other Identity) bool {
	return id.DeviceID == other.DeviceID &&
		id.DriverID == other.DriverID &&
		id.Serial == other.Serial &&
		id.InstallationAlias == other.InstallationAlias
}

// MarshalJSON preserves unknown fields.
func (id Identity) MarshalJSON() ([]byte, error) {
	type alias Identity
	b, err := json.Marshal(alias(id))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, id.Extra)
}

// UnmarshalJSON captures unknown fields into Extra.
func (id *Identity) UnmarshalJSON(data []byte) error {
	type alias Identity
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*id = Identity(a)
	extra, err := extractExtra(data, []string{
		"schema", "device_id", "driver_id", "driver_version", "display_name",
		"serial", "attributes", "installation_alias",
	})
	if err != nil {
		return err
	}
	id.Extra = extra
	return nil
}

// Device is a stable identity plus current connection-scoped view.
type Device struct {
	Schema       string       `json:"schema"`
	Identity     Identity     `json:"identity"`
	ConnectionID ConnectionID `json:"connection_id,omitempty"`
	Surfaces     []Surface    `json:"surfaces,omitempty"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Extra        map[string]any `json:"-"`
}

// Validate checks the device graph for structural errors.
func (d Device) Validate() error {
	if d.Schema == "" {
		return fmt.Errorf("core: device schema is required")
	}
	if d.Schema != DeviceSchema {
		return fmt.Errorf("core: unsupported device schema %q", d.Schema)
	}
	if err := d.Identity.Validate(); err != nil {
		return err
	}
	seenSurf := map[string]struct{}{}
	for _, s := range d.Surfaces {
		if s.ID == "" {
			return fmt.Errorf("core: surface id is required")
		}
		if _, ok := seenSurf[s.ID]; ok {
			return fmt.Errorf("core: duplicate surface id %q", s.ID)
		}
		seenSurf[s.ID] = struct{}{}
	}
	seenCap := map[CapabilityKey]struct{}{}
	for _, c := range d.Capabilities {
		if err := c.Validate(); err != nil {
			return err
		}
		if _, ok := seenCap[c.Key]; ok {
			return fmt.Errorf("core: duplicate capability key %q", c.Key)
		}
		seenCap[c.Key] = struct{}{}
	}
	return nil
}

// MarshalJSON preserves unknown fields.
func (d Device) MarshalJSON() ([]byte, error) {
	type alias Device
	b, err := json.Marshal(alias(d))
	if err != nil {
		return nil, err
	}
	return mergeExtra(b, d.Extra)
}

// UnmarshalJSON captures unknown fields.
func (d *Device) UnmarshalJSON(data []byte) error {
	type alias Device
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*d = Device(a)
	extra, err := extractExtra(data, []string{
		"schema", "identity", "connection_id", "surfaces", "capabilities",
	})
	if err != nil {
		return err
	}
	d.Extra = extra
	return nil
}
