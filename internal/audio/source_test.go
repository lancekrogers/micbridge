package audio_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/lancekrogers/micbridge/internal/audio"
)

func TestNegotiateAndNoDefaultFallback(t *testing.T) {
	src := audio.Source{
		ID: "src_a", Name: "Mic", Available: true,
		Formats: []audio.Format{{SampleRate: 48000, Channels: 2, BitsPerSample: 16}},
	}
	if _, err := audio.Negotiate(src, audio.Format{}); err == nil {
		t.Fatal("expected explicit format requirement")
	}
	f, err := audio.Negotiate(src, audio.Format{SampleRate: 48000, Channels: 2, BitsPerSample: 16})
	if err != nil || f.SampleRate != 48000 {
		t.Fatal(err, f)
	}
	if _, err := audio.Negotiate(src, audio.Format{SampleRate: 44100, Channels: 2, BitsPerSample: 16}); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestMissingSourceAndDuplicates(t *testing.T) {
	sources := []audio.Source{
		{ID: "src_1", Name: "USB Mic", Available: true, Formats: []audio.Format{{48000, 2, 16}}},
		{ID: "src_2", Name: "USB Mic", Available: true, Formats: []audio.Format{{48000, 2, 16}}},
	}
	named := audio.DedupeNames(sources)
	if named[0].Name == named[1].Name {
		t.Fatalf("names not deduped: %v %v", named[0].Name, named[1].Name)
	}
	if _, err := audio.SelectByID(sources, ""); err == nil || !strings.Contains(err.Error(), "explicit") {
		t.Fatal(err)
	}
	if _, err := audio.SelectByID(sources, "missing"); err == nil {
		t.Fatal("expected missing")
	}
	got, err := audio.SelectByID(sources, "src_1")
	if err != nil || got.ID != "src_1" {
		t.Fatal(err, got)
	}
}

func TestPathChurnSameID(t *testing.T) {
	enum := audio.StaticEnumerator{Sources: []audio.Source{{
		ID: "src_stable", Name: "Recv", TransientHint: "pcmC1", Available: true,
		Formats: []audio.Format{{48000, 2, 16}},
	}}}
	// After churn, same ID different transient path.
	enum.Sources[0].TransientHint = "pcmC9"
	res, err := audio.ResolveSource(context.Background(), enum, "src_stable", audio.Format{48000, 2, 16})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source.TransientHint != "pcmC9" {
		t.Fatal(res.Source.TransientHint)
	}
}

func TestUnavailableSource(t *testing.T) {
	src := audio.Source{ID: "x", Name: "n", Available: false, UnavailableReason: "busy"}
	if _, err := audio.Negotiate(src, audio.Format{48000, 2, 16}); err == nil {
		t.Fatal("expected unavailable")
	}
}

func TestOptInMalgoList(t *testing.T) {
	// Opt-in only: never run in default CI to avoid host audio stack flakes.
	if os.Getenv("MICBRIDGE_NATIVE_AUDIO") != "1" {
		t.Skip("set MICBRIDGE_NATIVE_AUDIO=1 to list real capture devices")
	}
	var enum audio.Enumerator = &audio.MalgoEnumerator{}
	srcs, err := enum.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("found %d capture sources", len(srcs))
	for _, s := range srcs {
		t.Logf("  %s %q hint=%s", s.ID, s.Name, s.TransientHint)
	}
}
