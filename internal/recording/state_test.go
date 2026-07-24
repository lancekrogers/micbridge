package recording_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/recording"
)

func TestArmStartStopHappyPath(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	sink := &recording.FakeSink{}
	m := recording.NewMachine(clk, idgen.NewSeeded("rec"), sink, recording.Limits{})
	defer m.Close()

	ctx := context.Background()
	s, err := m.Submit(ctx, recording.Command{Name: recording.CmdArm, Actor: recording.ActorCLI})
	if err != nil || s.State != recording.StateArmed {
		t.Fatalf("arm: %+v %v", s, err)
	}
	s, err = m.Submit(ctx, recording.Command{Name: recording.CmdStart, Actor: recording.ActorBinding, Reason: "press"})
	if err != nil || s.State != recording.StateRecording {
		t.Fatalf("start: %+v %v", s, err)
	}
	s, err = m.Submit(ctx, recording.Command{Name: recording.CmdStop, Actor: recording.ActorBinding})
	if err != nil || s.State != recording.StateCompleted {
		t.Fatalf("stop: %+v %v", s, err)
	}
	if len(sink.Calls) < 3 {
		t.Fatalf("sink calls %v", sink.Calls)
	}
}

func TestIdempotentStartStop(t *testing.T) {
	m := recording.NewMachine(nil, idgen.NewSeeded("i"), &recording.FakeSink{}, recording.Limits{})
	defer m.Close()
	ctx := context.Background()
	_, _ = m.Submit(ctx, recording.Command{Name: recording.CmdStart, Actor: recording.ActorAPI, IdempotencyKey: "k1"})
	s1, err := m.Submit(ctx, recording.Command{Name: recording.CmdStart, Actor: recording.ActorAPI, IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := m.Submit(ctx, recording.Command{Name: recording.CmdStart, Actor: recording.ActorAPI})
	if err != nil || s2.State != recording.StateRecording {
		t.Fatalf("dup start without key should be idempotent on state: %+v %v", s2, err)
	}
	_ = s1
	s3, err := m.Submit(ctx, recording.Command{Name: recording.CmdStop, Actor: recording.ActorAPI, IdempotencyKey: "k2"})
	if err != nil || s3.State != recording.StateCompleted {
		t.Fatal(err, s3)
	}
	s4, err := m.Submit(ctx, recording.Command{Name: recording.CmdStop, Actor: recording.ActorAPI, IdempotencyKey: "k2"})
	if err != nil || s4.State != recording.StateCompleted {
		t.Fatal(err, s4)
	}
}

func TestIllegalTransition(t *testing.T) {
	m := recording.NewMachine(nil, nil, &recording.FakeSink{}, recording.Limits{})
	defer m.Close()
	_, err := m.Submit(context.Background(), recording.Command{Name: recording.CmdStop, Actor: recording.ActorCLI})
	var te *recording.TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("want TransitionError, got %v", err)
	}
}

func TestConcurrentCommandsSerialized(t *testing.T) {
	m := recording.NewMachine(nil, idgen.NewSeeded("c"), &recording.FakeSink{}, recording.Limits{})
	defer m.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Submit(ctx, recording.Command{Name: recording.CmdStart, Actor: recording.ActorAPI})
			_, _ = m.Submit(ctx, recording.Command{Name: recording.CmdStop, Actor: recording.ActorAPI})
		}()
	}
	wg.Wait()
	// Deterministic terminal: completed, recording, or idle after races of start/stop.
	st := m.Snapshot().State
	switch st {
	case recording.StateCompleted, recording.StateRecording, recording.StateIdle, recording.StateArmed, recording.StateFailed:
	default:
		t.Fatalf("unexpected state %s", st)
	}
}

func TestCancelledContext(t *testing.T) {
	m := recording.NewMachine(nil, nil, &recording.FakeSink{}, recording.Limits{})
	defer m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Submit(ctx, recording.Command{Name: recording.CmdArm, Actor: recording.ActorCLI})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestRecoveryTransition(t *testing.T) {
	sink := &recording.FakeSink{FailOn: "start"}
	m := recording.NewMachine(nil, idgen.NewSeeded("r"), sink, recording.Limits{})
	defer m.Close()
	ctx := context.Background()
	s, err := m.Submit(ctx, recording.Command{Name: recording.CmdStart, Actor: recording.ActorSystem})
	if s.State != recording.StateFailed {
		t.Fatalf("want failed, got %+v err=%v", s, err)
	}
	s, err = m.Submit(ctx, recording.Command{Name: recording.CmdRecover, Actor: recording.ActorSystem})
	if err != nil || s.State != recording.StateCompleted {
		t.Fatalf("recover: %+v %v", s, err)
	}
}
