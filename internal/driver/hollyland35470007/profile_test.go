package hollyland35470007_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	hl "github.com/lancekrogers/micbridge/internal/driver/hollyland35470007"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

func exactCandidate() driver.Candidate {
	return driver.Candidate{
		Attributes: map[string]string{
			"usb_vid": "3547", "usb_pid": "0007",
			"manufacturer": "Shenzhen Hollyland Technology Co.,Ltd",
			"product":      "Wireless microphone",
			"has_hid":      "1", "has_audio": "1", "has_vendor": "1",
			"interfaces": "0,1,3,7",
		},
		TransientPaths: map[string]string{"audio": "pcmC3D0c", "control": "event12"},
	}
}

func TestExactMatchAndNearMisses(t *testing.T) {
	p := hl.DefaultProfile()
	res, _ := hl.EvaluateMatch(p, hl.CandidateUSB{
		VID: "3547", PID: "0007", HasHID: true, HasAudio: true, InterfaceNums: []int{0, 1, 3, 7},
	})
	if res != hl.MatchExact {
		t.Fatal(res)
	}
	res, why := hl.EvaluateMatch(p, hl.CandidateUSB{VID: "3547", PID: "0008", Manufacturer: "Shenzhen Hollyland Technology Co.,Ltd"})
	if res != hl.MatchNearMiss {
		t.Fatal(res, why)
	}
	res, _ = hl.EvaluateMatch(p, hl.CandidateUSB{VID: "3547", PID: "0007", HasHID: true, HasAudio: true, InterfaceNums: []int{0, 1, 2}})
	if res != hl.MatchFuture {
		t.Fatal(res)
	}
	res, _ = hl.EvaluateMatch(p, hl.CandidateUSB{})
	if res != hl.MatchMalformed {
		t.Fatal(res)
	}
}

func TestDriverMatchOpenGroupSurfaces(t *testing.T) {
	d := hl.New(idgen.NewSeeded("hl"))
	m, err := d.Match(context.Background(), exactCandidate())
	if err != nil {
		t.Fatal(err)
	}
	dev, err := d.Open(context.Background(), m, exactCandidate())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dev.Close() }()
	if len(dev.Surfaces()) < 2 {
		t.Fatal(dev.Surfaces())
	}
	// same identity after path churn
	c2 := exactCandidate()
	c2.TransientPaths["audio"] = "pcmC9D0c"
	m2, err := d.Match(context.Background(), c2)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Identity.EqualStable(m2.Identity) {
		t.Fatal("identity not stable")
	}
	dev2, _ := d.Open(context.Background(), m2, c2)
	if dev.ConnectionID() == dev2.ConnectionID() {
		t.Fatal("connection should change")
	}
	_ = dev2.Close()
}

func TestControlParserAndPassiveSideEffect(t *testing.T) {
	ev, err := hl.ParseHIDReport(hl.HIDPress, "p1")
	if err != nil || ev.Type != "pressed" {
		t.Fatal(err, ev)
	}
	if len(ev.SideEffects) != 1 || ev.SideEffects[0] != "host.volume_increment" {
		t.Fatal(ev.SideEffects)
	}
	rel, err := hl.ParseHIDReport(hl.HIDRelease, "p1")
	if err != nil || rel.Type != "released" {
		t.Fatal(err, rel)
	}
	evk, err := hl.ParseEvdevKey(115, 1, "p2")
	if err != nil || evk.Type != "pressed" {
		t.Fatal(err, evk)
	}
	// bind with side effect disclosure
	eng := binding.New(nil, "")
	b, err := eng.Upsert(binding.Binding{
		Schema: binding.Schema, ID: "hl_toggle", DeviceID: "dev_hl_35470007",
		Control: "primary", On: "pressed", Action: binding.ActionRecordingToggle, Enabled: true,
	}, hl.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	if !b.Enabled || len(b.SideEffects) == 0 {
		t.Fatal(b)
	}
}

func TestDoctorAndNearMissDriver(t *testing.T) {
	r := hl.DoctorAccess("", "")
	if r.OK {
		t.Fatal("expected fail without paths")
	}
	_ = hl.UdevHint()
	d := hl.New(nil)
	_, err := d.Match(context.Background(), driver.Candidate{Attributes: map[string]string{
		"usb_vid": "3547", "usb_pid": "0007", "has_hid": "1", // missing audio + interfaces
	}})
	if !errors.Is(err, hl.ErrNearMiss) {
		t.Fatalf("got %v", err)
	}
}

func TestUnsupportedNC(t *testing.T) {
	for _, c := range hl.Capabilities() {
		if c.Key == core.CapSettingNoiseCancellation && c.Availability != core.Unsupported {
			t.Fatal(c)
		}
	}
}

func TestTwoPressLivePathSimulated(t *testing.T) {
	d := hl.New(idgen.NewSeeded("live"))
	m, _ := d.Match(context.Background(), exactCandidate())
	dev, _ := d.Open(context.Background(), m, exactCandidate())
	defer func() { _ = dev.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := dev.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	press, _ := hl.ParseHIDReport(hl.HIDPress, "press_1")
	rel, _ := hl.ParseHIDReport(hl.HIDRelease, "press_1")
	if err := hl.Inject(dev, press); err != nil {
		t.Fatal(err)
	}
	if err := hl.Inject(dev, rel); err != nil {
		t.Fatal(err)
	}
	ev1 := <-ch
	ev2 := <-ch
	if ev1.Type != "pressed" || ev2.Type != "released" {
		t.Fatalf("%+v %+v", ev1, ev2)
	}
	// Audio path present
	srcs, err := dev.AudioSources(ctx)
	if err != nil || len(srcs) != 1 || srcs[0].RateHz != 48000 {
		t.Fatal(err, srcs)
	}
}
