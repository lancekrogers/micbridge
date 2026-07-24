package ux

import (
	"fmt"

	"github.com/lancekrogers/micbridge/internal/audio"
	"github.com/lancekrogers/micbridge/internal/recording"
)

// RecorderView is a compact reference UI model (wide/narrow).
type RecorderView struct {
	Mode        string            `json:"mode"` // wide|narrow
	State       recording.State   `json:"state"`
	SessionID   string            `json:"session_id,omitempty"`
	DeviceName  string            `json:"device_name,omitempty"`
	Health      audio.HealthLevel `json:"health"`
	HealthNote  string            `json:"health_note,omitempty"`
	SideEffect  string            `json:"side_effect,omitempty"`
	Message     string            `json:"message"`
	Recoverable bool              `json:"recoverable"`
}

// WideView shows full status; NarrowView is a single-line summary.
func WideView(snap recording.Snapshot, health audio.Snapshot, device string) RecorderView {
	v := RecorderView{
		Mode: "wide", State: snap.State, SessionID: snap.SessionID,
		DeviceName: device, Health: health.Level, HealthNote: health.Reason,
		SideEffect:  "host.volume_increment (passive)",
		Message:     fmt.Sprintf("Recording %s — health %s", snap.State, health.Level),
		Recoverable: snap.State == recording.StateFailed || snap.State == recording.StateRecovering,
	}
	return v
}

// NarrowView is a compact one-line UI.
func NarrowView(snap recording.Snapshot, health audio.Snapshot) RecorderView {
	return RecorderView{
		Mode: "narrow", State: snap.State, SessionID: snap.SessionID,
		Health:  health.Level,
		Message: fmt.Sprintf("%s|%s", snap.State, health.Level),
	}
}

// FailureView presents recovery guidance.
func FailureView(reason string, partialPath string) RecorderView {
	msg := reason
	if partialPath != "" {
		msg += " — partial at " + partialPath + " (recoverable)"
	}
	return RecorderView{
		Mode: "wide", State: recording.StateFailed, Health: audio.HealthDisconnected,
		Message: msg, Recoverable: partialPath != "",
	}
}
