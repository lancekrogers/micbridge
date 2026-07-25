package core_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lancekrogers/micbridge/internal/core"
)

func TestDeviceIDRejectsVolatilePaths(t *testing.T) {
	cases := []struct {
		id    core.DeviceID
		valid bool
	}{
		{"dev_abc123", true},
		{"", false},
		{"/dev/input/event12", false},
		{"hw:1,0", false},
		{"card2", false},
		{"hidraw0", false},
		{"has space", false},
	}
	for _, tc := range cases {
		err := tc.id.Validate()
		if tc.valid && err != nil {
			t.Fatalf("%q: unexpected err %v", tc.id, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("%q: expected error", tc.id)
		}
	}
}

func TestIdentityStableAcrossPathChanges(t *testing.T) {
	base := core.Identity{
		Schema:        core.IdentitySchema,
		DeviceID:      "dev_receiver_1",
		DriverID:      "example-driver",
		DriverVersion: "1.0.0",
		Serial:        "SN-1",
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}

	// Connection A vs B: different volatile surfaces, same stable identity.
	devA := core.Device{
		Schema:       core.DeviceSchema,
		Identity:     base,
		ConnectionID: "conn_a",
		Surfaces: []core.Surface{{
			ID: "audio0", Kind: core.SurfaceAudioCapture, TransientPath: "/dev/snd/pcmC1D0c",
		}},
	}
	devB := core.Device{
		Schema:       core.DeviceSchema,
		Identity:     base,
		ConnectionID: "conn_b",
		Surfaces: []core.Surface{{
			ID: "audio0", Kind: core.SurfaceAudioCapture, TransientPath: "/dev/snd/pcmC5D0c",
		}},
	}
	if err := devA.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := devB.Validate(); err != nil {
		t.Fatal(err)
	}
	if !devA.Identity.EqualStable(devB.Identity) {
		t.Fatal("stable identity should match across path changes")
	}
	devB.Identity.InstallationAlias = "renamed at installation"
	if !devA.Identity.EqualStable(devB.Identity) {
		t.Fatal("display alias changed stable physical identity")
	}
	if devA.ConnectionID == devB.ConnectionID {
		t.Fatal("connection ids should differ")
	}
	if devA.Surfaces[0].TransientPath == devB.Surfaces[0].TransientPath {
		t.Fatal("transient paths should differ")
	}
}

func TestNestedUnknownFieldsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"schema":"micbridge.device/v1",
		"identity":{"schema":"micbridge.identity/v1","device_id":"dev_nested","driver_id":"d","driver_version":"1"},
		"surfaces":[{"id":"control","kind":"control","future_surface":{"x":1}}],
		"capabilities":[{
			"schema":"micbridge.capability/v1","key":"control.press","availability":"available",
			"evidence":{"level":"measured","future_evidence":["proof"]}
		}]
	}`)
	var device core.Device
	if err := json.Unmarshal(raw, &device); err != nil {
		t.Fatal(err)
	}
	if device.Surfaces[0].Extra["future_surface"] == nil ||
		device.Capabilities[0].Evidence.Extra["future_evidence"] == nil {
		t.Fatalf("nested extra missing: %+v", device)
	}
	out, err := json.Marshal(device)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "future_surface") ||
		!strings.Contains(string(out), "future_evidence") {
		t.Fatalf("nested extra lost: %s", out)
	}
}

func FuzzDeviceJSONRoundTrip(f *testing.F) {
	f.Add([]byte(`{"schema":"micbridge.device/v1","identity":{"schema":"micbridge.identity/v1","device_id":"dev_fuzz","driver_id":"fuzz","driver_version":"1"}}`))
	f.Add([]byte(`{"future":true,"surfaces":[{"future_nested":[1,2,3]}]}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		var first core.Device
		if err := json.Unmarshal(input, &first); err != nil {
			return
		}
		encoded, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal accepted device: %v", err)
		}
		var second core.Device
		if err := json.Unmarshal(encoded, &second); err != nil {
			t.Fatalf("redecode encoded device: %v\n%s", err, encoded)
		}
	})
}

func TestCapabilityAvailabilityDistinctions(t *testing.T) {
	table := []struct {
		name      string
		cap       core.Capability
		wantCall  bool
		wantValid bool
	}{
		{
			name: "measured available press",
			cap: core.Capability{
				Schema:       core.CapabilitySchema,
				Key:          core.CapControlPress,
				Availability: core.Available,
				Evidence:     core.Evidence{Level: core.EvidenceMeasured, Summary: "repeated host event"},
				SideEffects:  []core.SideEffect{{ID: "host.volume_increment", Passive: true}},
			},
			wantCall:  true,
			wantValid: true,
		},
		{
			name: "unsupported transmitter",
			cap: core.Capability{
				Schema:            core.CapabilitySchema,
				Key:               core.CapControlPress,
				Availability:      core.Unsupported,
				UnavailableReason: "no host-visible control on this device class",
				Evidence:          core.Evidence{Level: core.EvidenceMeasuredNegative, Summary: "bounded trial found no signal"},
			},
			wantCall:  false,
			wantValid: true,
		},
		{
			name: "unavailable permission",
			cap: core.Capability{
				Schema:            core.CapabilitySchema,
				Key:               core.CapAudioCapture,
				Availability:      core.Unavailable,
				UnavailableReason: "capture permission not granted",
				Evidence:          core.Evidence{Level: core.EvidenceMeasured, Summary: "source enumerated"},
			},
			wantCall:  false,
			wantValid: true,
		},
		{
			name: "unverified candidate",
			cap: core.Capability{
				Schema:            core.CapabilitySchema,
				Key:               core.CapSettingNoiseCancellation,
				Availability:      core.Unverified,
				UnavailableReason: "candidate only; not isolated",
				Evidence:          core.Evidence{Level: core.EvidenceCandidate, Summary: "plausible vendor field"},
			},
			wantCall:  false,
			wantValid: true,
		},
		{
			name: "invalid available+candidate",
			cap: core.Capability{
				Schema:       core.CapabilitySchema,
				Key:          core.CapControlPress,
				Availability: core.Available,
				Evidence:     core.Evidence{Level: core.EvidenceCandidate},
			},
			wantCall:  false,
			wantValid: false,
		},
		{
			name: "missing reason when unavailable",
			cap: core.Capability{
				Schema:       core.CapabilitySchema,
				Key:          core.CapControlRelease,
				Availability: core.Unavailable,
				Evidence:     core.Evidence{Level: core.EvidenceMeasured},
			},
			wantValid: false,
		},
	}
	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cap.Validate()
			if tc.wantValid && err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !tc.wantValid && err == nil {
				t.Fatal("expected validation error")
			}
			if tc.wantValid && tc.cap.Callable() != tc.wantCall {
				t.Fatalf("Callable = %v, want %v", tc.cap.Callable(), tc.wantCall)
			}
		})
	}
}

func TestDeviceRejectsDuplicateCapabilityKeys(t *testing.T) {
	cap := core.Capability{
		Schema:       core.CapabilitySchema,
		Key:          core.CapAudioCapture,
		Availability: core.Available,
		Evidence:     core.Evidence{Level: core.EvidenceMeasured, Summary: "ok"},
	}
	dev := core.Device{
		Schema: core.DeviceSchema,
		Identity: core.Identity{
			Schema: core.IdentitySchema, DeviceID: "dev_1",
			DriverID: "d", DriverVersion: "1",
		},
		Capabilities: []core.Capability{cap, cap},
	}
	if err := dev.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate capability") {
		t.Fatalf("expected duplicate capability error, got %v", err)
	}
}

func TestIdentityRoundTripPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"schema":"micbridge.identity/v1",
		"device_id":"dev_rt",
		"driver_id":"example",
		"driver_version":"1.0.0",
		"future_field":{"nested":true}
	}`)
	var id core.Identity
	if err := json.Unmarshal(raw, &id); err != nil {
		t.Fatal(err)
	}
	if err := id.Validate(); err != nil {
		t.Fatal(err)
	}
	if id.Extra["future_field"] == nil {
		t.Fatal("expected unknown field preserved")
	}
	out, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	var again map[string]any
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if again["future_field"] == nil {
		t.Fatalf("round-trip dropped future_field: %s", out)
	}
	// Change only transient path on a device view; stable id unchanged.
	dev := core.Device{
		Schema:   core.DeviceSchema,
		Identity: id,
		Surfaces: []core.Surface{{ID: "s1", Kind: core.SurfaceControl, TransientPath: "event3"}},
	}
	b1, _ := json.Marshal(dev)
	dev.Surfaces[0].TransientPath = "event9"
	b2, _ := json.Marshal(dev)
	var d1, d2 core.Device
	_ = json.Unmarshal(b1, &d1)
	_ = json.Unmarshal(b2, &d2)
	if !d1.Identity.EqualStable(d2.Identity) {
		t.Fatal("identity changed with path")
	}
}

func TestDeviceRoundTripFixture(t *testing.T) {
	dev := core.Device{
		Schema: core.DeviceSchema,
		Identity: core.Identity{
			Schema:        core.IdentitySchema,
			DeviceID:      "dev_fixture_1",
			DriverID:      "example-driver",
			DriverVersion: "1.0.0",
			DisplayName:   "Example Receiver",
		},
		ConnectionID: "conn_1",
		Surfaces: []core.Surface{
			{ID: "audio", Kind: core.SurfaceAudioCapture, TransientPath: "pcmC2D0c"},
			{ID: "ctrl", Kind: core.SurfaceControl, TransientPath: "event7"},
		},
		Capabilities: []core.Capability{
			{
				Schema:       core.CapabilitySchema,
				Key:          core.CapAudioCapture,
				Availability: core.Available,
				Evidence:     core.Evidence{Level: core.EvidenceMeasured, Summary: "enumerated"},
			},
			{
				Schema:            core.CapabilitySchema,
				Key:               core.CapSettingNoiseCancellation,
				Availability:      core.Unsupported,
				UnavailableReason: "no host-visible control",
				Evidence:          core.Evidence{Level: core.EvidenceMeasuredNegative, Summary: "bounded trial"},
			},
		},
	}
	if err := dev.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(dev)
	if err != nil {
		t.Fatal(err)
	}
	var out core.Device
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if err := out.Validate(); err != nil {
		t.Fatal(err)
	}
	if out.Identity.DeviceID != "dev_fixture_1" {
		t.Fatal(out.Identity.DeviceID)
	}
	if len(out.Capabilities) != 2 {
		t.Fatal(len(out.Capabilities))
	}
}
