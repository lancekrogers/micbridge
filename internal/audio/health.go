package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// HealthLevel is an operator-facing capture health state.
type HealthLevel string

const (
	HealthOK            HealthLevel = "ok"
	HealthDegraded      HealthLevel = "degraded"
	HealthDiscontinuous HealthLevel = "discontinuous"
	HealthDisconnected  HealthLevel = "disconnected"
)

// Thresholds define warning/failure units for ingress health.
type Thresholds struct {
	// MaxQueueDepthWarn is frames; above this → degraded.
	MaxQueueDepthWarn int
	// MaxDropsWarn is total overflow drops before degraded.
	MaxDropsWarn uint64
	// MaxDiscontinuitiesFail forces discontinuous level.
	MaxDiscontinuitiesFail uint64
	// MaxCallbackIntervalWarn is the max gap between callbacks before degraded.
	MaxCallbackIntervalWarn time.Duration
}

// DefaultThresholds returns conservative v1 thresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		MaxQueueDepthWarn:       6,
		MaxDropsWarn:            1,
		MaxDiscontinuitiesFail:  1,
		MaxCallbackIntervalWarn: 50 * time.Millisecond,
	}
}

// Snapshot is a structured health view (never contains PCM).
type Snapshot struct {
	Level             HealthLevel `json:"level"`
	FramesIn          uint64      `json:"frames_in"`
	FramesOut         uint64      `json:"frames_out"`
	Drops             uint64      `json:"drops"`
	Discontinuities   uint64      `json:"discontinuities"`
	QueueDepth        int         `json:"queue_depth"`
	QueueHighWater    int         `json:"queue_high_water"`
	LastSeq           uint64      `json:"last_seq"`
	LastCallbackGapNs int64       `json:"last_callback_gap_ns"`
	MaxCallbackGapNs  int64       `json:"max_callback_gap_ns"`
	Disconnected      bool        `json:"disconnected"`
	Reason            string      `json:"reason,omitempty"`
}

// Monitor aggregates ingress health outside the callback.
type Monitor struct {
	th Thresholds

	framesIn  atomic.Uint64
	framesOut atomic.Uint64
	drops     atomic.Uint64
	disc      atomic.Uint64
	lastSeq   atomic.Uint64
	lastCB    atomic.Int64 // unix nano monotonic-ish via time.Now
	maxGap    atomic.Int64
	lastGap   atomic.Int64
	hwm       atomic.Int64
	discFlag  atomic.Uint32
	disconn   atomic.Uint32

	mu     sync.Mutex
	reason string
}

// NewMonitor returns a health monitor.
func NewMonitor(th Thresholds) *Monitor {
	return &Monitor{th: th}
}

// OnCallback observes a successful or attempted callback handoff (hot path: atomics only).
func (m *Monitor) OnCallback(seq uint64, queueDepth int, dropped bool) {
	m.framesIn.Add(1)
	if dropped {
		m.drops.Add(1)
	}
	now := time.Now().UnixNano()
	prev := m.lastCB.Swap(now)
	if prev != 0 {
		gap := now - prev
		m.lastGap.Store(gap)
		for {
			max := m.maxGap.Load()
			if gap <= max || m.maxGap.CompareAndSwap(max, gap) {
				break
			}
		}
	}
	prevSeq := m.lastSeq.Swap(seq)
	if prevSeq != 0 && seq > prevSeq+1 {
		m.disc.Add(1)
		m.discFlag.Store(1)
	}
	for {
		hw := m.hwm.Load()
		if int64(queueDepth) <= hw || m.hwm.CompareAndSwap(hw, int64(queueDepth)) {
			break
		}
	}
}

// OnDrain marks a frame consumed by the worker.
func (m *Monitor) OnDrain() { m.framesOut.Add(1) }

// NoteDiscontinuity records an explicit gap (injected stalls/tests).
func (m *Monitor) NoteDiscontinuity(reason string) {
	m.disc.Add(1)
	m.discFlag.Store(1)
	m.mu.Lock()
	m.reason = reason
	m.mu.Unlock()
}

// NoteDisconnect marks device loss.
func (m *Monitor) NoteDisconnect(reason string) {
	m.disconn.Store(1)
	m.mu.Lock()
	m.reason = reason
	m.mu.Unlock()
}

// Snapshot computes the current health level.
func (m *Monitor) Snapshot(queueDepth int) Snapshot {
	m.mu.Lock()
	reason := m.reason
	m.mu.Unlock()
	s := Snapshot{
		FramesIn:          m.framesIn.Load(),
		FramesOut:         m.framesOut.Load(),
		Drops:             m.drops.Load(),
		Discontinuities:   m.disc.Load(),
		QueueDepth:        queueDepth,
		QueueHighWater:    int(m.hwm.Load()),
		LastSeq:           m.lastSeq.Load(),
		LastCallbackGapNs: m.lastGap.Load(),
		MaxCallbackGapNs:  m.maxGap.Load(),
		Disconnected:      m.disconn.Load() == 1,
		Reason:            reason,
	}
	switch {
	case s.Disconnected:
		s.Level = HealthDisconnected
		if s.Reason == "" {
			s.Reason = "device_disconnected"
		}
	case s.Discontinuities >= m.th.MaxDiscontinuitiesFail || m.discFlag.Load() == 1:
		s.Level = HealthDiscontinuous
		if s.Reason == "" {
			s.Reason = "frame_sequence_gap"
		}
	case s.Drops >= m.th.MaxDropsWarn ||
		queueDepth >= m.th.MaxQueueDepthWarn ||
		(m.th.MaxCallbackIntervalWarn > 0 && time.Duration(s.MaxCallbackGapNs) > m.th.MaxCallbackIntervalWarn):
		s.Level = HealthDegraded
		if s.Reason == "" {
			s.Reason = "ingress_pressure"
		}
	default:
		s.Level = HealthOK
	}
	return s
}
