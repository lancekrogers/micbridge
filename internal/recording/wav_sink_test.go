package recording_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
)

func TestWAVSinkMachineCreatesFinalizedSyntheticRecording(t *testing.T) {
	root := t.TempDir()
	recDir := filepath.Join(root, "recordings")
	journalDir := filepath.Join(root, "journals")
	format := recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
	sink := recording.NewWAVSink(
		recDir,
		journalDir,
		format,
		recording.DefaultPolicy(recDir),
		recording.SyntheticPCMStarter(format, 0),
	)
	machine := recording.NewMachine(nil, idgen.NewSeeded("wav"), sink, recording.Limits{})
	defer machine.Close()
	ctx := context.Background()
	armed, err := machine.Submit(ctx, recording.Command{Name: recording.CmdArm, Actor: recording.ActorCLI})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Submit(ctx, recording.Command{Name: recording.CmdStart, Actor: recording.ActorBinding}); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Submit(ctx, recording.Command{Name: recording.CmdStop, Actor: recording.ActorBinding}); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(recDir, armed.SessionID+".wav")
	if err := recording.ValidateWAV(final); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(final); err != nil || st.Size() <= 44 {
		t.Fatalf("final recording size: %v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(journalDir, armed.SessionID+".journal")); err != nil {
		t.Fatal(err)
	}
}

func TestWAVSinkMachineHonorsCreateOnlySelectedOutput(t *testing.T) {
	root := t.TempDir()
	recDir := filepath.Join(root, "recordings")
	journalDir := filepath.Join(root, "journals")
	target := filepath.Join(recDir, "meeting_custom.wav")
	format := recording.WAVFormat{SampleRate: 48000, Channels: 1, BitsPerSample: 16}
	sink := recording.NewWAVSink(
		recDir,
		journalDir,
		format,
		recording.DefaultPolicy(recDir),
		recording.SyntheticPCMStarter(format, 0),
	)
	machine := recording.NewMachine(nil, idgen.NewSeeded("selected"), sink, recording.Limits{})
	defer machine.Close()
	ctx := context.Background()
	started, err := machine.Submit(ctx, recording.Command{
		Name: recording.CmdStart, Actor: recording.ActorAPI, OutputPath: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.Submit(ctx, recording.Command{
		Name: recording.CmdStop, Actor: recording.ActorAPI, SessionID: started.SessionID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recording.ValidateWAV(target); err != nil {
		t.Fatalf("selected target was not finalized: %v", err)
	}
	outcome, recoveredPath, err := recording.RecoverSession(recDir, journalDir, started.SessionID)
	if err != nil || outcome != recording.RecoveryFinalized || recoveredPath != target {
		t.Fatalf("selected output recovery=%s path=%s err=%v", outcome, recoveredPath, err)
	}
	if _, err := os.Stat(filepath.Join(recDir, started.SessionID+".wav")); !os.IsNotExist(err) {
		t.Fatalf("unexpected default artifact: %v", err)
	}
	if _, err := recording.CreateWAV(recDir, "meeting_custom", format); err == nil {
		t.Fatal("selected output was not create-only")
	}
}

func TestWAVSinkPropagatesDeterministicWriterFailure(t *testing.T) {
	root := t.TempDir()
	recDir := filepath.Join(root, "recordings")
	journalDir := filepath.Join(root, "journals")
	format := recording.WAVFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
	badStarter := func(_ context.Context, write func([]byte) error) (func() error, error) {
		err := write([]byte{0x01})
		return func() error { return nil }, err
	}
	sink := recording.NewWAVSink(
		recDir, journalDir, format, recording.DefaultPolicy(recDir), badStarter,
	)
	machine := recording.NewMachine(nil, idgen.NewSeeded("write-failure"), sink, recording.Limits{})
	defer machine.Close()
	snapshot, err := machine.Submit(context.Background(), recording.Command{
		Name: recording.CmdStart, Actor: recording.ActorSystem,
	})
	if err == nil || snapshot.State != recording.StateFailed {
		t.Fatalf("writer failure was not propagated: snapshot=%+v err=%v", snapshot, err)
	}
	partial := filepath.Join(recDir, snapshot.SessionID+".wav.partial")
	if _, statErr := os.Stat(partial); statErr != nil {
		t.Fatalf("failed recording did not preserve partial: %v", statErr)
	}
}
