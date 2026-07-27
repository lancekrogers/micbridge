//go:build cgo

package audio

// #include <stdlib.h>
import "C"

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"
)

const (
	// Recording favors continuity over interactive-monitoring latency.
	capturePeriodFrames      = 960 // 20 ms at 48 kHz
	capturePeriods           = 4
	maxCaptureCallbackFrames = 8192
)

// OpenNativeCapture initializes an explicitly selected malgo capture device.
// The native callback only copies into a bounded preallocated SPSC ingress;
// handle runs on a Go worker.
func OpenNativeCapture(ctx context.Context, source Source, format Format, handle func([]byte) error) (*Capture, error) {
	if handle == nil {
		return nil, fmt.Errorf("audio: PCM handler is required")
	}
	if _, err := Negotiate(source, format); err != nil {
		return nil, err
	}
	ac, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: malgo init: %w", err)
	}
	cleanupContext := func() {
		_ = ac.Uninit()
		ac.Free()
	}
	infos, err := ac.Devices(malgo.Capture)
	if err != nil {
		cleanupContext()
		return nil, fmt.Errorf("audio: enumerate selected source: %w", err)
	}
	var selected *malgo.DeviceInfo
	for i := range infos {
		if infos[i].ID.String() == source.TransientHint {
			selected = &infos[i]
			break
		}
	}
	if selected == nil {
		cleanupContext()
		return nil, fmt.Errorf("audio: selected native source is no longer attached")
	}
	nativeFormat, err := malgoFormat(format.BitsPerSample)
	if err != nil {
		cleanupContext()
		return nil, err
	}
	bytesPerFrame := int(format.Channels * (format.BitsPerSample / 8))
	maxFrameBytes := maxCaptureCallbackFrames * bytesPerFrame
	ingress, err := NewIngress(64, maxFrameBytes, PolicyDropNewest)
	if err != nil {
		cleanupContext()
		return nil, err
	}
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	var (
		errMu          sync.Mutex
		streamErr      error
		callbacksCount atomic.Uint64
		acceptedFrames atomic.Uint64
		droppedFrames  atomic.Uint64
		acceptedBytes  atomic.Uint64
	)
	go func() {
		defer close(workerDone)
		DrainWorker(workerCtx, ingress, func(frame Frame) {
			if handleErr := handle(frame.PCM); handleErr != nil {
				errMu.Lock()
				if streamErr == nil {
					streamErr = handleErr
				}
				errMu.Unlock()
			}
		})
	}()

	config := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceIDPointer := selected.ID.Pointer()
	config.Capture.DeviceID = deviceIDPointer
	config.Capture.Format = nativeFormat
	config.Capture.Channels = format.Channels
	config.SampleRate = format.SampleRate
	config.PeriodSizeInFrames = capturePeriodFrames
	config.Periods = capturePeriods
	config.PerformanceProfile = malgo.Conservative
	config.NoFixedSizedCallback = 0
	callbacks := malgo.DeviceCallbacks{Data: func(_, input []byte, frameCount uint32) {
		callbacksCount.Add(1)
		expectedBytes := int(frameCount) * bytesPerFrame
		if frameCount == 0 || len(input) != expectedBytes || !ingress.PushFromCallback(input, time.Now().UnixNano()) {
			droppedFrames.Add(uint64(frameCount))
			return
		}
		acceptedFrames.Add(uint64(frameCount))
		acceptedBytes.Add(uint64(len(input)))
	}}
	device, err := malgo.InitDevice(ac.Context, config, callbacks)
	C.free(deviceIDPointer)
	if err != nil {
		ingress.Close()
		cancelWorker()
		<-workerDone
		cleanupContext()
		return nil, fmt.Errorf("audio: open selected capture source: %w", err)
	}
	capture := &Capture{
		Source: source,
		Format: format,
		NativeFormat: Format{
			SampleRate:    device.CaptureInternalSampleRate(),
			Channels:      device.CaptureInternalChannels(),
			BitsPerSample: bitsForMalgo(device.CaptureInternalFormat()),
		},
		startFn:  device.Start,
		overflow: ingress.OverflowCount,
		stats: func() CaptureStats {
			return CaptureStats{
				Callbacks:      callbacksCount.Load(),
				FramesAccepted: acceptedFrames.Load(),
				FramesDropped:  droppedFrames.Load(),
				BytesAccepted:  acceptedBytes.Load(),
			}
		},
	}
	var closeOnce sync.Once
	capture.closeFn = func() error {
		var closeErr error
		closeOnce.Do(func() {
			if device.IsStarted() {
				closeErr = device.Stop()
			}
			ingress.Close()
			cancelWorker()
			<-workerDone
			device.Uninit()
			cleanupContext()
			errMu.Lock()
			if closeErr == nil {
				closeErr = streamErr
			}
			errMu.Unlock()
			if lost := droppedFrames.Load(); closeErr == nil && lost > 0 {
				closeErr = fmt.Errorf("audio: capture dropped %d frames; partial recording preserved", lost)
			}
		})
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		_ = capture.Close()
		return nil, err
	}
	return capture, nil
}

func malgoFormat(bits uint32) (malgo.FormatType, error) {
	switch bits {
	case 16:
		return malgo.FormatS16, nil
	case 24:
		return malgo.FormatS24, nil
	case 32:
		return malgo.FormatS32, nil
	default:
		return malgo.FormatUnknown, fmt.Errorf("audio: unsupported native PCM width %d", bits)
	}
}

func bitsForMalgo(format malgo.FormatType) uint32 {
	switch format {
	case malgo.FormatS16:
		return 16
	case malgo.FormatS24:
		return 24
	case malgo.FormatS32, malgo.FormatF32:
		return 32
	default:
		return 0
	}
}
