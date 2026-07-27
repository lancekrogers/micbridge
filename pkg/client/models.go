package client

import (
	"encoding/json"
	"time"
)

const (
	IdentitySchema   = "micbridge.identity/v1"
	CapabilitySchema = "micbridge.capability/v1"
	DeviceSchema     = "micbridge.device/v1"
	BindingSchema    = "micbridge.binding/v1"
)

type DeviceID string
type ConnectionID string
type SurfaceKind string

const (
	SurfaceAudioCapture SurfaceKind = "audio.capture"
	SurfaceControl      SurfaceKind = "control"
	SurfaceVendor       SurfaceKind = "vendor"
	SurfaceUnknown      SurfaceKind = "unknown"
)

type Surface struct {
	ID            string         `json:"id"`
	Kind          SurfaceKind    `json:"kind"`
	Label         string         `json:"label,omitempty"`
	TransientPath string         `json:"transient_path,omitempty"`
	Extra         map[string]any `json:"-"`
}

type Identity struct {
	Schema            string            `json:"schema"`
	DeviceID          DeviceID          `json:"device_id"`
	DriverID          string            `json:"driver_id"`
	DriverVersion     string            `json:"driver_version"`
	DisplayName       string            `json:"display_name,omitempty"`
	Serial            string            `json:"serial,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
	InstallationAlias string            `json:"installation_alias,omitempty"`
	Extra             map[string]any    `json:"-"`
}

type CapabilityKey string

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

type Availability string

const (
	Available   Availability = "available"
	Unavailable Availability = "unavailable"
	Unsupported Availability = "unsupported"
	Unverified  Availability = "unverified"
)

type EvidenceLevel string

const (
	EvidenceSpecified        EvidenceLevel = "specified"
	EvidenceMeasured         EvidenceLevel = "measured"
	EvidenceCorrelated       EvidenceLevel = "correlated"
	EvidenceCandidate        EvidenceLevel = "candidate"
	EvidenceMeasuredNegative EvidenceLevel = "measured_negative"
	EvidenceUnknown          EvidenceLevel = "unknown"
)

type Evidence struct {
	Level          EvidenceLevel  `json:"level"`
	Summary        string         `json:"summary,omitempty"`
	ProfileVersion string         `json:"profile_version,omitempty"`
	ArtifactDigest string         `json:"artifact_digest,omitempty"`
	Extra          map[string]any `json:"-"`
}

type SideEffect struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Passive     bool   `json:"passive,omitempty"`
}

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

func (c Capability) Callable() bool {
	if c.Availability != Available {
		return false
	}
	switch c.Evidence.Level {
	case EvidenceSpecified, EvidenceMeasured:
		return true
	case EvidenceCorrelated:
		return c.Mode == "" || c.Mode == "read" || c.Mode == "read_write"
	default:
		return false
	}
}

type Device struct {
	Schema       string         `json:"schema"`
	Identity     Identity       `json:"identity"`
	ConnectionID ConnectionID   `json:"connection_id,omitempty"`
	Surfaces     []Surface      `json:"surfaces,omitempty"`
	Capabilities []Capability   `json:"capabilities,omitempty"`
	Extra        map[string]any `json:"-"`
}

type BindingID string
type Action string

const (
	ActionRecordingToggle Action = "recording.toggle"
	ActionRecordingStart  Action = "recording.start"
	ActionRecordingStop   Action = "recording.stop"
	ActionNone            Action = "none"
)

type Binding struct {
	Schema      string    `json:"schema"`
	ID          BindingID `json:"id"`
	DeviceID    DeviceID  `json:"device_id"`
	Control     string    `json:"control"`
	On          string    `json:"on"`
	Action      Action    `json:"action"`
	DebounceMs  int       `json:"debounce_ms,omitempty"`
	Enabled     bool      `json:"enabled"`
	SideEffects []string  `json:"side_effects,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

type RecordingState string
type RecordingActor string

const (
	StateIdle       RecordingState = "idle"
	StateArmed      RecordingState = "armed"
	StateStarting   RecordingState = "starting"
	StateRecording  RecordingState = "recording"
	StateStopping   RecordingState = "stopping"
	StateRecovering RecordingState = "recovering"
	StateCompleted  RecordingState = "completed"
	StateFailed     RecordingState = "failed"
)

type RecordingSnapshot struct {
	SessionID  string         `json:"session_id"`
	State      RecordingState `json:"state"`
	Actor      RecordingActor `json:"actor"`
	Reason     string         `json:"reason,omitempty"`
	OutputPath string         `json:"output_path,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
	Title      string         `json:"title,omitempty"`
}

type Sequence uint64
type EventType string

type Event struct {
	Schema            string         `json:"schema"`
	EventID           string         `json:"event_id"`
	Sequence          Sequence       `json:"sequence"`
	Time              time.Time      `json:"time"`
	MonotonicOffsetNs int64          `json:"monotonic_offset_ns,omitempty"`
	Type              EventType      `json:"type"`
	DeviceID          DeviceID       `json:"device_id,omitempty"`
	SessionID         string         `json:"session_id,omitempty"`
	Data              map[string]any `json:"data,omitempty"`
}

func marshalExtra(value any, extra map[string]any) ([]byte, error) {
	base, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return base, nil
	}
	var object map[string]any
	if err := json.Unmarshal(base, &object); err != nil {
		return nil, err
	}
	for key, value := range extra {
		if _, known := object[key]; !known {
			object[key] = value
		}
	}
	return json.Marshal(object)
}

func captureExtra(data []byte, known ...string) (map[string]any, error) {
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	for _, key := range known {
		delete(object, key)
	}
	if len(object) == 0 {
		return nil, nil
	}
	return object, nil
}

func (s Surface) MarshalJSON() ([]byte, error) {
	type wire Surface
	return marshalExtra(wire(s), s.Extra)
}

func (s *Surface) UnmarshalJSON(data []byte) error {
	type wire Surface
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = Surface(decoded)
	extra, err := captureExtra(data, "id", "kind", "label", "transient_path")
	s.Extra = extra
	return err
}

func (i Identity) MarshalJSON() ([]byte, error) {
	type wire Identity
	return marshalExtra(wire(i), i.Extra)
}

func (i *Identity) UnmarshalJSON(data []byte) error {
	type wire Identity
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*i = Identity(decoded)
	extra, err := captureExtra(data, "schema", "device_id", "driver_id", "driver_version",
		"display_name", "serial", "attributes", "installation_alias")
	i.Extra = extra
	return err
}

func (e Evidence) MarshalJSON() ([]byte, error) {
	type wire Evidence
	return marshalExtra(wire(e), e.Extra)
}

func (e *Evidence) UnmarshalJSON(data []byte) error {
	type wire Evidence
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*e = Evidence(decoded)
	extra, err := captureExtra(data, "level", "summary", "profile_version", "artifact_digest")
	e.Extra = extra
	return err
}

func (c Capability) MarshalJSON() ([]byte, error) {
	type wire Capability
	return marshalExtra(wire(c), c.Extra)
}

func (c *Capability) UnmarshalJSON(data []byte) error {
	type wire Capability
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Capability(decoded)
	extra, err := captureExtra(data, "schema", "key", "availability", "mode",
		"unavailable_reason", "evidence", "side_effects", "driver_version")
	c.Extra = extra
	return err
}

func (d Device) MarshalJSON() ([]byte, error) {
	type wire Device
	return marshalExtra(wire(d), d.Extra)
}

func (d *Device) UnmarshalJSON(data []byte) error {
	type wire Device
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*d = Device(decoded)
	extra, err := captureExtra(data, "schema", "identity", "connection_id", "surfaces", "capabilities")
	d.Extra = extra
	return err
}
