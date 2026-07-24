package event

import (
	"time"

	"github.com/lancekrogers/micbridge/internal/core"
)

// Sequence is a daemon-monotonic event order assigned by the bus.
type Sequence uint64

// Type identifies an event kind in the micbridge.event/v1 envelope.
type Type string

const (
	TypeStreamGap Type = "stream.gap"
	// Other types are defined as strings by publishers (device.*, control.*, …).
)

// Envelope is a versioned, ordered notification.
type Envelope struct {
	Schema            string         `json:"schema"`
	EventID           string         `json:"event_id"`
	Sequence          Sequence       `json:"sequence"`
	Time              time.Time      `json:"time"`
	MonotonicOffsetNs int64          `json:"monotonic_offset_ns,omitempty"`
	Type              Type           `json:"type"`
	DeviceID          core.DeviceID  `json:"device_id,omitempty"`
	SessionID         string         `json:"session_id,omitempty"`
	Data              map[string]any `json:"data,omitempty"`
}

// Cursor is a subscriber resume position.
type Cursor struct {
	Sequence Sequence `json:"sequence"`
}

// GapData is payload for stream.gap events.
type GapData struct {
	FromSequence Sequence `json:"from_sequence"`
	ToSequence   Sequence `json:"to_sequence"`
	Reason       string   `json:"reason"`
}
