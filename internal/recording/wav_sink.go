package recording

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PCMStarter opens one explicitly selected PCM source. The returned stop
// function must stop callbacks before it returns.
type PCMStarter func(context.Context, func([]byte) error) (stop func() error, err error)

// WAVSink connects the recording state machine to create-only WAV persistence.
// It owns at most one prepared session and serializes writer access.
type WAVSink struct {
	mu          sync.Mutex
	recordings  string
	journals    string
	format      WAVFormat
	policy      Policy
	startPCM    PCMStarter
	writer      *Writer
	journal     *Journal
	sessionID   string
	startedAt   time.Time
	captureStop func() error
	writeErr    error
	lastFinal   FinalMeta
}

// NewWAVSink returns a durable sink for one explicitly configured source.
func NewWAVSink(recordingsDir, journalsDir string, format WAVFormat, policy Policy, start PCMStarter) *WAVSink {
	if policy.OutputRoot == "" {
		policy.OutputRoot = recordingsDir
	}
	return &WAVSink{
		recordings: recordingsDir,
		journals:   journalsDir,
		format:     format,
		policy:     policy,
		startPCM:   start,
	}
}

func (s *WAVSink) Prepare(ctx context.Context, sessionID string) error {
	return s.prepare(ctx, sessionID, s.recordings, sessionID)
}

// PrepareAt creates a recording at an API-validated absolute .wav target.
func (s *WAVSink) PrepareAt(ctx context.Context, sessionID, outputPath string) error {
	if !filepath.IsAbs(outputPath) || !strings.EqualFold(filepath.Ext(outputPath), ".wav") {
		return fmt.Errorf("recording: output path must be an absolute .wav path")
	}
	base := strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))
	if err := validateArtifactID(base); err != nil {
		return err
	}
	return s.prepare(ctx, sessionID, filepath.Dir(outputPath), base)
}

func (s *WAVSink) prepare(ctx context.Context, sessionID, recordingsDir, artifactBase string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.policy.CheckArm(); err != nil {
		return err
	}
	writer, err := CreateWAV(recordingsDir, artifactBase, s.format)
	if err != nil {
		return err
	}
	journal, err := OpenJournal(s.journals, sessionID)
	if err != nil {
		_ = writer.Abort()
		return err
	}
	if err := journal.Append(JournalRecord{
		SessionID: sessionID,
		Step:      "created",
		Partial:   writer.PartialPath(),
	}); err != nil {
		_ = writer.Abort()
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != nil {
		_ = writer.Abort()
		return fmt.Errorf("recording: another session is already prepared")
	}
	s.writer = writer
	s.journal = journal
	s.sessionID = sessionID
	s.writeErr = nil
	s.captureStop = nil
	return nil
}

func (s *WAVSink) Start(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.writer == nil || s.sessionID != sessionID {
		s.mu.Unlock()
		return fmt.Errorf("recording: session %s is not prepared", sessionID)
	}
	if s.captureStop != nil {
		s.mu.Unlock()
		return nil
	}
	s.startedAt = time.Now()
	s.mu.Unlock()

	sessionCtx, cancel := context.WithCancel(context.Background())
	starter := s.startPCM
	if starter == nil {
		starter = SyntheticPCMStarter(s.format, 20*time.Millisecond)
	}
	type startResult struct {
		stop func() error
		err  error
	}
	result := make(chan startResult, 1)
	go func() {
		stop, err := starter(sessionCtx, s.writePCM)
		result <- startResult{stop: stop, err: err}
	}()
	var started startResult
	select {
	case started = <-result:
	case <-ctx.Done():
		cancel()
		started = <-result
		if started.stop != nil {
			_ = started.stop()
		}
		return ctx.Err()
	}
	stop, err := started.stop, started.err
	if err != nil {
		cancel()
		return err
	}
	s.mu.Lock()
	s.captureStop = func() error {
		cancel()
		return stop()
	}
	journal := s.journal
	s.mu.Unlock()
	if journal != nil {
		return journal.Append(JournalRecord{SessionID: sessionID, Step: "recording"})
	}
	return nil
}

func (s *WAVSink) writePCM(pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return fmt.Errorf("recording: no active writer")
	}
	if s.writeErr != nil {
		return s.writeErr
	}
	if err := s.policy.CheckDuration(time.Since(s.startedAt)); err != nil {
		s.writeErr = err
		return err
	}
	if err := s.writer.WritePCM(pcm); err != nil {
		s.writeErr = err
		return err
	}
	return nil
}

func (s *WAVSink) Stop(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.writer == nil || s.sessionID != sessionID {
		s.mu.Unlock()
		return fmt.Errorf("recording: session %s is not active", sessionID)
	}
	stop := s.captureStop
	s.captureStop = nil
	s.mu.Unlock()
	if stop != nil {
		if err := stop(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	if err := s.writer.Sync(); err != nil {
		return err
	}
	if s.journal != nil {
		if err := s.journal.Append(JournalRecord{
			SessionID: sessionID,
			Step:      "synced",
			Partial:   s.writer.PartialPath(),
			DataBytes: s.writer.DataBytes(),
		}); err != nil {
			return err
		}
	}
	meta, err := s.writer.Finalize()
	if err != nil {
		return err
	}
	if s.journal != nil {
		if err := s.journal.Append(JournalRecord{
			SessionID: sessionID,
			Step:      "finalized",
			Final:     meta.Path,
			DataBytes: meta.DataBytes,
			CRC32:     meta.CRC32,
		}); err != nil {
			return err
		}
	}
	s.lastFinal = meta
	s.writer = nil
	s.journal = nil
	s.sessionID = ""
	return nil
}

func (s *WAVSink) Abort(ctx context.Context, sessionID string, reason string) error {
	s.mu.Lock()
	stop := s.captureStop
	s.captureStop = nil
	s.mu.Unlock()
	if stop != nil {
		_ = stop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return nil
	}
	if s.sessionID != sessionID {
		return fmt.Errorf("recording: session %s is not active", sessionID)
	}
	if s.journal != nil {
		_ = s.journal.Append(JournalRecord{
			SessionID: sessionID,
			Step:      "aborted",
			Partial:   s.writer.PartialPath(),
			Note:      reason,
		})
	}
	err := s.writer.Abort()
	s.writer = nil
	s.journal = nil
	s.sessionID = ""
	return err
}

// LastFinal returns the most recently published artifact metadata.
func (s *WAVSink) LastFinal() FinalMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastFinal
}

// SyntheticPCMStarter provides deterministic hardware-free PCM for integration
// tests, demos, and VHS. It is never selected for a physical device.
func SyntheticPCMStarter(format WAVFormat, period time.Duration) PCMStarter {
	if period <= 0 {
		period = 20 * time.Millisecond
	}
	frames := uint64(format.SampleRate) * uint64(period) / uint64(time.Second)
	frameBytes := int(frames * uint64(format.Channels) * uint64(format.BitsPerSample/8))
	if frameBytes < int(format.Channels*(format.BitsPerSample/8)) {
		frameBytes = int(format.Channels * (format.BitsPerSample / 8))
	}
	pcm := make([]byte, frameBytes)
	return func(ctx context.Context, write func([]byte) error) (func() error, error) {
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		var (
			mu        sync.Mutex
			streamErr error
		)
		go func() {
			defer close(done)
			ticker := time.NewTicker(period)
			defer ticker.Stop()
			if err := write(pcm); err != nil {
				mu.Lock()
				streamErr = err
				mu.Unlock()
				return
			}
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					if err := write(pcm); err != nil {
						mu.Lock()
						streamErr = err
						mu.Unlock()
						return
					}
				}
			}
		}()
		var once sync.Once
		stop := func() error {
			once.Do(func() {
				cancel()
				<-done
			})
			mu.Lock()
			defer mu.Unlock()
			return streamErr
		}
		return stop, nil
	}
}
