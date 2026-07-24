package recording_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lancekrogers/micbridge/internal/recording"
	"github.com/lancekrogers/micbridge/internal/testsupport"
)

func TestWAVFinalizeAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	fmt := recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
	w, err := recording.CreateWAV(dir, "sess1", fmt)
	if err != nil {
		t.Fatal(err)
	}
	pcm := testsupport.Tone(testsupport.DefaultCaptureFormat(), 100, 440, 0.2)
	if err := w.WritePCM(pcm); err != nil {
		t.Fatal(err)
	}
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
	meta, err := w.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if err := recording.ValidateWAV(meta.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := recording.CreateWAV(dir, "sess1", fmt); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func TestKillPointsRecovery(t *testing.T) {
	dir := t.TempDir()
	recDir := filepath.Join(dir, "recordings")
	jouDir := filepath.Join(dir, "journals")
	fmt := recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}

	// Kill after create (header only)
	w, err := recording.CreateWAV(recDir, "k1", fmt)
	if err != nil {
		t.Fatal(err)
	}
	j, _ := recording.OpenJournal(jouDir, "k1")
	_ = j.Append(recording.JournalRecord{SessionID: "k1", Step: "created", Partial: w.PartialPath()})
	_ = w.Abort()
	out, path, err := recording.RecoverSession(recDir, jouDir, "k1")
	if out != recording.RecoveryPartialOK {
		t.Fatalf("got %s %s %v", out, path, err)
	}

	// Kill after write+sync before finalize
	w2, err := recording.CreateWAV(recDir, "k2", fmt)
	if err != nil {
		t.Fatal(err)
	}
	_ = w2.WritePCM(make([]byte, 256))
	_ = w2.Sync()
	j2, _ := recording.OpenJournal(jouDir, "k2")
	_ = j2.Append(recording.JournalRecord{SessionID: "k2", Step: "synced", Partial: w2.PartialPath(), DataBytes: 256})
	_ = w2.Abort()
	out, _, _ = recording.RecoverSession(recDir, jouDir, "k2")
	if out != recording.RecoveryPartialOK {
		t.Fatal(out)
	}

	// Happy finalize
	w3, _ := recording.CreateWAV(recDir, "k3", fmt)
	_ = w3.WritePCM(make([]byte, 128))
	meta, err := w3.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	j3, _ := recording.OpenJournal(jouDir, "k3")
	_ = j3.Append(recording.JournalRecord{SessionID: "k3", Step: "finalized", Final: meta.Path, DataBytes: meta.DataBytes, CRC32: meta.CRC32})
	out, path, err = recording.RecoverSession(recDir, jouDir, "k3")
	if out != recording.RecoveryFinalized || err != nil {
		t.Fatal(out, err)
	}
	if err := recording.ValidateWAV(path); err != nil {
		t.Fatal(err)
	}

	ids, err := recording.ScanRecoverable(recDir)
	if err != nil {
		t.Fatal(err)
	}
	// k1 and k2 partials remain
	if len(ids) < 2 {
		t.Fatalf("ids=%v", ids)
	}
}

func TestCorruptFinalQuarantine(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "recordings")
	_ = os.MkdirAll(rec, 0o700)
	p := filepath.Join(rec, "bad.wav")
	if err := os.WriteFile(p, []byte("not a wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, path, err := recording.RecoverSession(rec, filepath.Join(dir, "journals"), "bad")
	if out != recording.RecoveryQuarantine {
		t.Fatalf("%s %s %v", out, path, err)
	}
}
