package recording_test

import (
	"encoding/binary"
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
	_ = j2.Append(recording.JournalRecord{SessionID: "k2", Step: "created", Partial: w2.PartialPath()})
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
	_ = j3.Append(recording.JournalRecord{SessionID: "k3", Step: "created", Partial: filepath.Join(recDir, "k3.wav.partial")})
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

func TestWAVRejectsTraversalInvalidFormatsAndUnalignedPCM(t *testing.T) {
	dir := t.TempDir()
	valid := recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
	for _, base := range []string{"", "..", "../escape", "a/b", `a\b`, "spaces bad"} {
		if _, err := recording.CreateWAV(dir, base, valid); err == nil {
			t.Fatalf("accepted base %q", base)
		}
	}
	for _, format := range []recording.WAVFormat{
		{}, {SampleRate: 48000, Channels: 0, BitsPerSample: 16},
		{SampleRate: 48000, Channels: 2, BitsPerSample: 12},
	} {
		if _, err := recording.CreateWAV(dir, "bad_format", format); err == nil {
			t.Fatalf("accepted format %+v", format)
		}
	}
	w, err := recording.CreateWAV(dir, "unaligned", valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WritePCM([]byte{1, 2, 3}); err == nil {
		t.Fatal("accepted unaligned PCM")
	}
	_ = w.Abort()
}

func TestWAVFinalizeDoesNotReplaceRacingFinal(t *testing.T) {
	dir := t.TempDir()
	format := recording.WAVFormat{SampleRate: 48000, Channels: 1, BitsPerSample: 16}
	w, err := recording.CreateWAV(dir, "race", format)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WritePCM(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "race.wav")
	const sentinel = "existing"
	if err := os.WriteFile(final, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Finalize(); err == nil {
		t.Fatal("expected no-replace failure")
	}
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Fatalf("racing final overwritten: %q", got)
	}
}

func TestValidateWAVRejectsInconsistentHeaderAndTrailingBytes(t *testing.T) {
	dir := t.TempDir()
	format := recording.WAVFormat{SampleRate: 48000, Channels: 1, BitsPerSample: 16}
	w, err := recording.CreateWAV(dir, "strict", format)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WritePCM(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	meta, err := w.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(meta.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, 0)
	if err := os.WriteFile(meta.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recording.ValidateWAV(meta.Path); err == nil {
		t.Fatal("accepted trailing byte")
	}
	binary.LittleEndian.PutUint32(raw[4:8], 0)
	if err := os.WriteFile(meta.Path, raw[:len(raw)-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recording.ValidateWAV(meta.Path); err == nil {
		t.Fatal("accepted inconsistent RIFF size")
	}
}

func TestRecoveryRejectsMalformedJournalWithoutTouchingAudio(t *testing.T) {
	root := t.TempDir()
	recDir := filepath.Join(root, "recordings")
	journalDir := filepath.Join(root, "journals")
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	format := recording.WAVFormat{SampleRate: 48000, Channels: 1, BitsPerSample: 16}
	writer, err := recording.CreateWAV(recDir, "journal_bad", format)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WritePCM(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(journalDir, "journal_bad.journal")
	if err := os.WriteFile(journalPath, []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome, path, err := recording.RecoverSession(recDir, journalDir, "journal_bad")
	if outcome != recording.RecoveryJournalInvalid || path != journalPath || err == nil {
		t.Fatalf("outcome=%s path=%s err=%v", outcome, path, err)
	}
	if _, statErr := os.Stat(filepath.Join(recDir, "journal_bad.wav.partial")); statErr != nil {
		t.Fatalf("partial was touched: %v", statErr)
	}
}
