package ux_test

import (
	"testing"

	"github.com/lancekrogers/micbridge/internal/audio"
	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver/fake"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/ux"
	"github.com/lancekrogers/micbridge/pkg/client"
)

func TestGuidedSetupFakeDevice(t *testing.T) {
	dev := core.Device{
		Schema: core.DeviceSchema,
		Identity: core.Identity{
			Schema: core.IdentitySchema, DeviceID: "dev_fake_1",
			DriverID: fake.DriverID, DriverVersion: fake.DriverVersion,
			DisplayName: "Fake Microphone",
		},
		Capabilities: []core.Capability{
			{Schema: core.CapabilitySchema, Key: core.CapAudioCapture, Availability: core.Available,
				Evidence: core.Evidence{Level: core.EvidenceMeasured, Summary: "fake"}},
			{Schema: core.CapabilitySchema, Key: core.CapControlPress, Availability: core.Available,
				Evidence:    core.Evidence{Level: core.EvidenceMeasured, Summary: "fake"},
				SideEffects: []core.SideEffect{{ID: "host.volume_increment", Passive: true}}},
		},
	}
	plan := ux.BuildSetupPlan(client.Device{
		Schema: client.DeviceSchema,
		Identity: client.Identity{
			Schema: client.IdentitySchema, DeviceID: "dev_fake_1",
			DriverID: fake.DriverID, DriverVersion: fake.DriverVersion,
			DisplayName: "Fake Microphone",
		},
		Capabilities: []client.Capability{
			{Schema: client.CapabilitySchema, Key: client.CapAudioCapture, Availability: client.Available,
				Evidence: client.Evidence{Level: client.EvidenceMeasured, Summary: "fake"}},
			{Schema: client.CapabilitySchema, Key: client.CapControlPress, Availability: client.Available,
				Evidence:    client.Evidence{Level: client.EvidenceMeasured, Summary: "fake"},
				SideEffects: []client.SideEffect{{ID: "host.volume_increment", Passive: true}}},
		},
	})
	if len(plan.Steps) < 4 {
		t.Fatal(plan.Steps)
	}
	for _, s := range plan.Steps {
		if s.Recommended == "" || s.Consequences == "" {
			t.Fatalf("%+v", s)
		}
	}
	eng := binding.New(nil, "")
	b, err := ux.ApplyRecommendedBinding(eng, dev)
	if err != nil || !b.Enabled {
		t.Fatal(err, b)
	}
}

func TestSetupBlockedWhenUnsupported(t *testing.T) {
	dev := client.Device{
		Schema:   client.DeviceSchema,
		Identity: client.Identity{Schema: client.IdentitySchema, DeviceID: "dev_x", DriverID: "x", DriverVersion: "1"},
		Capabilities: []client.Capability{{
			Schema: client.CapabilitySchema, Key: client.CapAudioCapture, Availability: client.Unavailable,
			UnavailableReason: "denied", Evidence: client.Evidence{Level: client.EvidenceMeasured},
		}},
	}
	plan := ux.BuildSetupPlan(dev)
	blocked := false
	for _, s := range plan.Steps {
		if s.Blocked {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("expected blocked capability step")
	}
}

func TestRecorderViews(t *testing.T) {
	snap := recording.Snapshot{State: recording.StateRecording, SessionID: "rec_1"}
	h := audio.Snapshot{Level: audio.HealthOK}
	w := ux.WideView(snap, h, "Fake")
	n := ux.NarrowView(snap, h)
	if w.Mode != "wide" || n.Mode != "narrow" {
		t.Fatal(w, n)
	}
	f := ux.FailureView("disk full", "/tmp/x.wav.partial")
	if !f.Recoverable {
		t.Fatal(f)
	}
}
