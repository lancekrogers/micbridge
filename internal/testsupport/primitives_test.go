package testsupport_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/queue"
	"github.com/lancekrogers/micbridge/internal/testsupport"
)

func TestHarnessRepeatabilityAndCleanup(t *testing.T) {
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	h1 := testsupport.NewHarness(t, "alpha", start)
	h2 := testsupport.NewHarness(t, "alpha", start)

	if h1.IDs.Next("dev") != h2.IDs.Next("dev") {
		t.Fatal("seeded ids diverged across harnesses")
	}
	h1.Clock.Advance(time.Second)
	h2.Clock.Advance(time.Second)
	if !h1.Clock.Now().Equal(h2.Clock.Now()) {
		t.Fatal("clocks diverged")
	}

	path := testsupport.WriteArtifact(t, h1.Layout, "recordings/sample.pcm", []byte{1, 2, 3})
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	// Path must stay under temp root (cleaned by testing.T).
	if filepath.Dir(filepath.Dir(path)) != h1.Layout.Root && filepath.Dir(path) != h1.Layout.RecordingsDir() {
		// RecordingsDir is root/recordings — accept either check via prefix.
		if rel, err := filepath.Rel(h1.Layout.Root, path); err != nil || rel == ".." || len(rel) >= 2 && rel[:2] == ".." {
			t.Fatalf("artifact not under root: %s", path)
		}
	}

	ctx := h1.Context(t)
	if err := ctx.Err(); err != nil {
		t.Fatalf("fresh context cancelled: %v", err)
	}
}

func TestSyntheticPCMDeterministic(t *testing.T) {
	format := testsupport.DefaultCaptureFormat()
	if format.SampleRate != 48000 || format.Channels != 2 {
		t.Fatalf("format = %+v", format)
	}
	a := testsupport.Tone(format, 480, 440, 0.5)
	b := testsupport.Tone(format, 480, 440, 0.5)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("tone lengths %d %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("tone byte %d diverged", i)
		}
	}
	sil := testsupport.Silence(format, 100)
	if testsupport.FrameCount(sil, format) != 100 {
		t.Fatal("silence frame count")
	}
	for _, v := range sil {
		if v != 0 {
			t.Fatal("silence not zero")
		}
	}
	marked, gs, ge := testsupport.Discontinuity(a, format, 10, 5)
	if gs != 10 || ge != 15 {
		t.Fatalf("gap = [%d,%d)", gs, ge)
	}
	if testsupport.FrameCount(marked, format) != testsupport.FrameCount(a, format) {
		t.Fatal("discontinuity changed frame count")
	}
}

func TestQueueCancellationDoesNotLeakOrdering(t *testing.T) {
	q, err := queue.NewBounded[int](8, queue.DropNewest)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 5; i++ {
		select {
		case <-ctx.Done():
			t.Fatal("unexpected cancel")
		default:
			if err := q.Push(i); err != nil {
				t.Fatal(err)
			}
		}
	}
	cancel()
	if ctx.Err() == nil {
		t.Fatal("expected cancelled context")
	}
	// Logical queue content remains ordered after cancel.
	for want := 0; want < 5; want++ {
		got, ok := q.Pop()
		if !ok || got != want {
			t.Fatalf("pop = %v %v, want %d", got, ok, want)
		}
	}
}

func TestRepeatedRunsIdenticalLogicalResults(t *testing.T) {
	run := func() []string {
		h := testsupport.NewHarness(t, "repeat", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		format := testsupport.DefaultCaptureFormat()
		pcm := testsupport.Tone(format, 32, 1000, 0.25)
		_ = testsupport.WriteArtifact(t, h.Layout, "journals/meta.bin", pcm[:16])
		var ids []string
		for i := 0; i < 3; i++ {
			ids = append(ids, h.IDs.Next("rec"))
			h.Clock.Advance(10 * time.Millisecond)
		}
		ids = append(ids, h.Clock.Now().UTC().Format(time.RFC3339Nano))
		return ids
	}
	a := run()
	b := run()
	if len(a) != len(b) {
		t.Fatalf("length %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("index %d: %q != %q", i, a[i], b[i])
		}
	}
}
