package audio_test

import (
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/audio"
)

func TestHealthOKToDegradedOnDrops(t *testing.T) {
	m := audio.NewMonitor(audio.DefaultThresholds())
	m.OnCallback(1, 1, false)
	if m.Snapshot(1).Level != audio.HealthOK {
		t.Fatal(m.Snapshot(1))
	}
	m.OnCallback(2, 2, true)
	s := m.Snapshot(2)
	if s.Level != audio.HealthDegraded || s.Drops != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestHealthDiscontinuityAndDisconnect(t *testing.T) {
	m := audio.NewMonitor(audio.DefaultThresholds())
	m.OnCallback(1, 0, false)
	m.OnCallback(5, 0, false) // gap
	s := m.Snapshot(0)
	if s.Level != audio.HealthDiscontinuous || s.Discontinuities == 0 {
		t.Fatalf("%+v", s)
	}
	m.NoteDisconnect("unplugged")
	s = m.Snapshot(0)
	if s.Level != audio.HealthDisconnected || s.Reason != "unplugged" {
		t.Fatalf("%+v", s)
	}
}

func TestHealthInjectedStall(t *testing.T) {
	th := audio.DefaultThresholds()
	th.MaxCallbackIntervalWarn = time.Millisecond
	m := audio.NewMonitor(th)
	m.OnCallback(1, 0, false)
	time.Sleep(3 * time.Millisecond)
	m.OnCallback(2, 0, false)
	s := m.Snapshot(0)
	if s.Level != audio.HealthDegraded {
		t.Fatalf("want degraded after stall, got %+v", s)
	}
	if s.MaxCallbackGapNs == 0 {
		t.Fatal("expected gap timing")
	}
}
