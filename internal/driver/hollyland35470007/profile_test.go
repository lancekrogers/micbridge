package hollyland35470007_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/lancekrogers/micbridge/internal/binding"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	hl "github.com/lancekrogers/micbridge/internal/driver/hollyland35470007"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

func exactCandidate() driver.Candidate {
	profile := hl.DefaultProfile()
	descriptors, err := json.Marshal(profile.Descriptors)
	if err != nil {
		panic(err)
	}
	return driver.Candidate{
		Attributes: map[string]string{
			"usb_vid": "3547", "usb_pid": "0007",
			"manufacturer": "Shenzhen Hollyland Technology Co.,Ltd",
			"product":      "Wireless microphone",
			"has_hid":      "1", "has_audio": "1", "has_vendor": "1",
			"interfaces": "0,1,3,7", "descriptor_digest": profile.EvidenceDigest,
			"interface_descriptors": string(descriptors),
		},
		TransientPaths: map[string]string{"audio": "pcmC3D0c", "control": "event12"},
	}
}

func TestExactMatchAndNearMisses(t *testing.T) {
	p := hl.DefaultProfile()
	res, _ := hl.EvaluateMatch(p, hl.CandidateUSB{
		VID: "3547", PID: "0007", Manufacturer: p.Manufacturer, Product: p.Product,
		HasHID: true, HasAudio: true, HasVendorIF: true, InterfaceNums: []int{0, 1, 3, 7},
		Descriptors: p.Descriptors, DescriptorDigest: p.EvidenceDigest,
	})
	if res != hl.MatchExact {
		t.Fatal(res)
	}
	wrongDigest := hl.CandidateUSB{
		VID: "3547", PID: "0007", Manufacturer: p.Manufacturer, Product: p.Product,
		HasHID: true, HasAudio: true, HasVendorIF: true, InterfaceNums: []int{0, 1, 3, 7},
		Descriptors: p.Descriptors, DescriptorDigest: strings.Repeat("0", 64),
	}
	if result, _ := hl.EvaluateMatch(p, wrongDigest); result != hl.MatchFuture {
		t.Fatalf("wrong descriptor digest classified %s", result)
	}
	encodedShapes, _ := json.Marshal(p.Descriptors)
	var wrongEndpoint []hl.InterfaceDescriptor
	_ = json.Unmarshal(encodedShapes, &wrongEndpoint)
	wrongEndpoint[len(wrongEndpoint)-1].Endpoints[0].MaxPacketBytes = 32
	if result, _ := hl.EvaluateMatch(p, hl.CandidateUSB{
		VID: "3547", PID: "0007", Manufacturer: p.Manufacturer, Product: p.Product,
		HasHID: true, HasAudio: true, HasVendorIF: true, InterfaceNums: []int{0, 1, 3, 7},
		Descriptors: wrongEndpoint, DescriptorDigest: p.EvidenceDigest,
	}); result != hl.MatchFuture {
		t.Fatalf("wrong endpoint shape classified %s", result)
	}
	res, why := hl.EvaluateMatch(p, hl.CandidateUSB{VID: "3547", PID: "0008", Manufacturer: "Shenzhen Hollyland Technology Co.,Ltd"})
	if res != hl.MatchNearMiss {
		t.Fatal(res, why)
	}
	res, _ = hl.EvaluateMatch(p, hl.CandidateUSB{
		VID: "3547", PID: "0007", Manufacturer: p.Manufacturer, Product: p.Product,
		HasHID: true, HasAudio: true, HasVendorIF: true, InterfaceNums: []int{0, 1, 2},
		Descriptors: p.Descriptors, DescriptorDigest: p.EvidenceDigest,
	})
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
	c1 := exactCandidate()
	c1.Attributes["serial"] = "stable-test-serial"
	m, err := d.Match(context.Background(), c1)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := d.Open(context.Background(), m, c1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dev.Close() }()
	if len(dev.Surfaces()) < 2 {
		t.Fatal(dev.Surfaces())
	}
	// same identity after path churn
	c2 := exactCandidate()
	c2.Attributes["serial"] = "stable-test-serial"
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

func TestDriverRejectsMalformedStructuredDescriptor(t *testing.T) {
	candidate := exactCandidate()
	candidate.Attributes["interface_descriptors"] = `[{"number":0,"unknown":true}]`
	if _, err := hl.New(nil).Match(context.Background(), candidate); err == nil ||
		!strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed descriptor accepted: %v", err)
	}
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
	nearMiss := exactCandidate()
	nearMiss.Attributes["usb_pid"] = "0008"
	_, err := d.Match(context.Background(), nearMiss)
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

func TestIdentityNeverEmbedsSerialAndNoSerialCandidatesStayDistinct(t *testing.T) {
	const serial = "PERSONAL-SERIAL-123"
	identity := hl.IdentityFor(serial)
	if strings.Contains(string(identity.DeviceID), serial) ||
		strings.Contains(strings.ToLower(string(identity.DeviceID)), strings.ToLower(serial)) {
		t.Fatalf("serial leaked in device id %q", identity.DeviceID)
	}
	withSerial := exactCandidate()
	withSerial.Attributes["serial"] = serial
	installA, err := hl.NewWithIdentitySalt(idgen.NewSeeded("a"), []byte("installation-a")).Match(context.Background(), withSerial)
	if err != nil {
		t.Fatal(err)
	}
	installB, err := hl.NewWithIdentitySalt(idgen.NewSeeded("b"), []byte("installation-b")).Match(context.Background(), withSerial)
	if err != nil {
		t.Fatal(err)
	}
	if installA.Identity.DeviceID == installB.Identity.DeviceID {
		t.Fatal("serial identity is linkable across installation salts")
	}
	d := hl.New(idgen.NewSeeded("privacy"))
	candidate := exactCandidate()
	delete(candidate.Attributes, "serial")
	first, err := d.Match(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.Match(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity.DeviceID == second.Identity.DeviceID {
		t.Fatalf("ambiguous receivers collapsed to %q", first.Identity.DeviceID)
	}
}

func TestControlInjectionCancellationAndCloseAreRaceSafe(t *testing.T) {
	d := hl.New(idgen.NewSeeded("lifecycle"))
	candidate := exactCandidate()
	m, err := d.Match(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := d.Open(context.Background(), m, candidate)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := dev.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = hl.Inject(dev, driver.ControlEvent{Control: "primary", Type: "pressed"})
		}()
	}
	cancel()
	if err := dev.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for range events {
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

func FuzzEvaluateMatchClassification(f *testing.F) {
	f.Add("3547", "0007", "Shenzhen Hollyland Technology Co.,Ltd", "Wireless microphone", []byte{0, 1, 3, 7}, true, true, true)
	f.Add("0x3547", "0007junk", "Hollyland", "other", []byte{0, 1}, false, false, false)
	f.Fuzz(func(t *testing.T, vid, pid, manufacturer, product string, rawInterfaces []byte, hid, audio, vendor bool) {
		if len(rawInterfaces) > 32 {
			rawInterfaces = rawInterfaces[:32]
		}
		interfaces := make([]int, len(rawInterfaces))
		for i, value := range rawInterfaces {
			interfaces[i] = int(value)
		}
		candidate := hl.CandidateUSB{
			VID: vid, PID: pid, Manufacturer: manufacturer, Product: product,
			InterfaceNums: interfaces, HasHID: hid, HasAudio: audio, HasVendorIF: vendor,
			Descriptors: hl.DefaultProfile().Descriptors, DescriptorDigest: hl.DefaultProfile().EvidenceDigest,
		}
		result, _ := hl.EvaluateMatch(hl.DefaultProfile(), candidate)
		if result == hl.MatchExact {
			normalizedVID := strings.ToLower(strings.TrimPrefix(vid, "0x"))
			normalizedPID := strings.ToLower(strings.TrimPrefix(pid, "0x"))
			if normalizedVID != "3547" || normalizedPID != "0007" ||
				strings.TrimSpace(manufacturer) != hl.DefaultProfile().Manufacturer ||
				strings.TrimSpace(product) != hl.DefaultProfile().Product ||
				!hid || !audio || !vendor {
				t.Fatalf("false exact classification: %+v", candidate)
			}
		}
	})
}
