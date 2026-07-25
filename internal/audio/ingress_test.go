package audio_test

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/audio"
)

func TestIngressBoundedOverflow(t *testing.T) {
	in, err := audio.NewIngress(4, 64, audio.PolicyDropNewest)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, 64)
	for i := 0; i < 4; i++ {
		if !in.PushFromCallback(pcm, int64(i)) {
			t.Fatalf("push %d failed", i)
		}
	}
	if in.PushFromCallback(pcm, 99) {
		t.Fatal("expected overflow")
	}
	if in.OverflowCount() == 0 {
		t.Fatal("expected drop counter")
	}
	// Drain
	n := 0
	for {
		if _, ok := in.Pop(); !ok {
			break
		}
		n++
	}
	if n != 4 {
		t.Fatalf("drained %d", n)
	}
}

func TestIngressSlowWriterStress(t *testing.T) {
	in, err := audio.NewIngress(8, 32, audio.PolicyDropNewest)
	if err != nil {
		t.Fatal(err)
	}
	var handled atomic.Uint64
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		audio.DrainWorker(ctx, in, func(fr audio.Frame) {
			time.Sleep(time.Millisecond) // intentionally slow
			handled.Add(1)
			_ = fr
		})
	}()
	pcm := make([]byte, 32)
	for i := 0; i < 200; i++ {
		in.PushFromCallback(pcm, int64(i))
	}
	// allow some drain
	time.Sleep(20 * time.Millisecond)
	in.Close()
	cancel()
	wg.Wait()
	if in.OverflowCount() == 0 {
		t.Fatal("expected overflow under slow writer")
	}
	// Memory bounded: queue capacity fixed
	if in.Len() > 8 {
		t.Fatalf("unbounded queue? %d", in.Len())
	}
}

func TestIngressCleanShutdown(t *testing.T) {
	in, _ := audio.NewIngress(2, 16, audio.PolicyDropNewest)
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		audio.DrainWorker(ctx, in, func(audio.Frame) {})
		close(done)
	}()
	in.PushFromCallback(make([]byte, 16), 1)
	in.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drain worker leak")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestIngressRejectsUnsafeDropOldestAndWrongFrameSizes(t *testing.T) {
	if _, err := audio.NewIngress(2, 16, audio.PolicyDropOldest); err == nil {
		t.Fatal("expected unsafe drop-oldest policy rejection")
	}
	in, err := audio.NewIngress(2, 16, audio.PolicyDropNewest)
	if err != nil {
		t.Fatal(err)
	}
	if in.PushFromCallback(make([]byte, 15), 1) {
		t.Fatal("accepted short frame")
	}
	if in.PushFromCallback(make([]byte, 17), 2) {
		t.Fatal("accepted oversized frame")
	}
	if got := in.OverflowCount(); got != 2 {
		t.Fatalf("integrity rejects=%d", got)
	}
}

func TestDrainWorkerDrainsEveryAcceptedFrameAfterCloseAndCancel(t *testing.T) {
	in, err := audio.NewIngress(8, 16, audio.PolicyDropNewest)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if !in.PushFromCallback(make([]byte, 16), int64(i)) {
			t.Fatalf("push %d", i)
		}
	}
	in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var drained atomic.Uint64
	audio.DrainWorker(ctx, in, func(audio.Frame) { drained.Add(1) })
	if got := drained.Load(); got != 8 {
		t.Fatalf("drained %d accepted frames", got)
	}
}
