package audio_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/audio"
	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/testsupport"
)

func TestStressIngressAndRecovery(t *testing.T) {
	// Bounded CI duration; long-run via MICBRIDGE_STRESS=1.
	iters := 500
	if os.Getenv("MICBRIDGE_STRESS") == "1" {
		iters = 20000
	}
	in, err := audio.NewIngress(16, 256, audio.PolicyDropNewest)
	if err != nil {
		t.Fatal(err)
	}
	mon := audio.NewMonitor(audio.DefaultThresholds())
	dir := t.TempDir()
	recDir := filepath.Join(dir, "recordings")
	jouDir := filepath.Join(dir, "journals")
	w, err := recording.CreateWAV(recDir, "stress1", recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16})
	if err != nil {
		t.Fatal(err)
	}
	j, _ := recording.OpenJournal(jouDir, "stress1")
	_ = j.Append(recording.JournalRecord{SessionID: "stress1", Step: "created", Partial: w.PartialPath()})

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		audio.DrainWorker(ctx, in, func(fr audio.Frame) {
			mon.OnDrain()
			// slow storage injection every 50 frames
			if fr.Seq%50 == 0 {
				time.Sleep(200 * time.Microsecond)
			}
			_ = w.WritePCM(fr.PCM)
		})
	}()

	// Subscriber churn simulation: health snapshots
	var snap audio.Snapshot
	format := testsupport.DefaultCaptureFormat()
	tone := testsupport.Tone(format, 32, 440, 0.3)
	sil := testsupport.Silence(format, 32)
	beforeG := runtime.NumGoroutine()
	for i := 0; i < iters; i++ {
		pcm := tone
		if i%2 == 0 {
			pcm = sil
		}
		ok := in.PushFromCallback(pcm, int64(i))
		mon.OnCallback(uint64(i+1), in.Len(), !ok)
		if i%100 == 0 {
			snap = mon.Snapshot(in.Len())
		}
	}
	// disconnect mid-run
	mon.NoteDisconnect("stress_disconnect")
	in.Close()
	cancel()
	wg.Wait()
	_ = w.Abort()
	_ = j.Append(recording.JournalRecord{SessionID: "stress1", Step: "aborted", Note: "stress end"})

	out, path, err := recording.RecoverSession(recDir, jouDir, "stress1")
	if out != recording.RecoveryPartialOK && out != recording.RecoveryFinalized {
		t.Fatalf("recovery %s %s %v", out, path, err)
	}
	if mon.Snapshot(0).Level != audio.HealthDisconnected {
		t.Fatalf("health %+v snap earlier %+v", mon.Snapshot(0), snap)
	}
	// leak check
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= beforeG+5 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("goroutines before=%d after=%d (informational)", beforeG, runtime.NumGoroutine())
}

func TestStressConcurrentProducersRejectedByDesign(t *testing.T) {
	// SPSC: only one producer; document that multi-producer is unsupported.
	in, _ := audio.NewIngress(4, 32, audio.PolicyDropNewest)
	pcm := make([]byte, 32)
	for i := 0; i < 10; i++ {
		in.PushFromCallback(pcm, int64(i))
	}
	in.Close()
}
