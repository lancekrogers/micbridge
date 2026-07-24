package recording

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

// State is a session lifecycle state.
type State string

const (
	StateIdle       State = "idle"
	StateArmed      State = "armed"
	StateStarting   State = "starting"
	StateRecording  State = "recording"
	StateStopping   State = "stopping"
	StateRecovering State = "recovering"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
)

// Actor identifies who requested a transition.
type Actor string

const (
	ActorAPI     Actor = "api"
	ActorCLI     Actor = "cli"
	ActorBinding Actor = "binding"
	ActorSystem  Actor = "system"
)

// CommandName is a transition request.
type CommandName string

const (
	CmdArm      CommandName = "arm"
	CmdStart    CommandName = "start"
	CmdStop     CommandName = "stop"
	CmdFail     CommandName = "fail"
	CmdRecover  CommandName = "recover"
	CmdComplete CommandName = "complete"
	CmdReset    CommandName = "reset"
)

// Command is an idempotent request to the session owner.
type Command struct {
	Name          CommandName
	Actor         Actor
	Reason        string
	IdempotencyKey string
	SessionID     string // optional; empty means current/new
}

// TransitionError is a typed illegal or failed transition.
type TransitionError struct {
	From   State
	To     State
	Cmd    CommandName
	Reason string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("recording: illegal transition %s -[%s]-> %s: %s", e.From, e.Cmd, e.To, e.Reason)
}

// Snapshot is an immutable view of session state.
type Snapshot struct {
	SessionID string
	State     State
	Actor     Actor
	Reason    string
	UpdatedAt time.Time
	Title     string
}

// Sink is the audio/session effect boundary (fake in this phase).
type Sink interface {
	// Prepare arms capture resources.
	Prepare(ctx context.Context, sessionID string) error
	// Start begins writing.
	Start(ctx context.Context, sessionID string) error
	// Stop finalizes output.
	Stop(ctx context.Context, sessionID string) error
	// Abort fails without finalizing as completed.
	Abort(ctx context.Context, sessionID string, reason string) error
}

// FakeSink is an in-memory sink for tests.
type FakeSink struct {
	mu      sync.Mutex
	Calls   []string
	FailOn  string
	PrepareFn func(ctx context.Context, id string) error
}

func (f *FakeSink) record(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, name)
	if f.FailOn == name {
		return fmt.Errorf("fake sink fail on %s", name)
	}
	return nil
}

func (f *FakeSink) Prepare(ctx context.Context, sessionID string) error {
	if f.PrepareFn != nil {
		return f.PrepareFn(ctx, sessionID)
	}
	return f.record("prepare")
}
func (f *FakeSink) Start(ctx context.Context, sessionID string) error {
	return f.record("start")
}
func (f *FakeSink) Stop(ctx context.Context, sessionID string) error {
	return f.record("stop")
}
func (f *FakeSink) Abort(ctx context.Context, sessionID string, reason string) error {
	return f.record("abort:" + reason)
}

// Limits configures session constraints.
type Limits struct {
	MaxDuration time.Duration
}

// Machine is the single owner of recording transitions.
type Machine struct {
	clk    clock.Clock
	ids    idgen.Generator
	sink   Sink
	limits Limits

	cmdCh  chan cmdReq
	done   chan struct{}
	closed chan struct{}

	// read via Snapshot under mu for tests; owner loop is authoritative.
	mu        sync.RWMutex
	snap      Snapshot
	idem      map[string]Snapshot
	listeners []chan Snapshot
}

type cmdReq struct {
	cmd  Command
	resp chan cmdResp
}

type cmdResp struct {
	snap Snapshot
	err  error
}

// NewMachine starts the owner loop.
func NewMachine(clk clock.Clock, ids idgen.Generator, sink Sink, limits Limits) *Machine {
	if clk == nil {
		clk = clock.System{}
	}
	if ids == nil {
		ids = &idgen.Atomic{}
	}
	if sink == nil {
		sink = &FakeSink{}
	}
	m := &Machine{
		clk:    clk,
		ids:    ids,
		sink:   sink,
		limits: limits,
		cmdCh:  make(chan cmdReq),
		done:   make(chan struct{}),
		closed: make(chan struct{}),
		snap: Snapshot{
			State:     StateIdle,
			UpdatedAt: clk.Now(),
		},
		idem: make(map[string]Snapshot),
	}
	go m.loop()
	return m
}

func (m *Machine) loop() {
	defer close(m.closed)
	for {
		select {
		case <-m.done:
			return
		case req := <-m.cmdCh:
			snap, err := m.apply(context.Background(), req.cmd)
			req.resp <- cmdResp{snap: snap, err: err}
		}
	}
}

// Close stops the owner loop.
func (m *Machine) Close() {
	select {
	case <-m.done:
		return
	default:
		close(m.done)
	}
	<-m.closed
}

// Snapshot returns the current state.
func (m *Machine) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snap
}

// Submit runs a command on the owner loop (serialized).
func (m *Machine) Submit(ctx context.Context, cmd Command) (Snapshot, error) {
	req := cmdReq{cmd: cmd, resp: make(chan cmdResp, 1)}
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-m.done:
		return Snapshot{}, fmt.Errorf("recording: machine closed")
	case m.cmdCh <- req:
	}
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-m.done:
		return Snapshot{}, fmt.Errorf("recording: machine closed")
	case resp := <-req.resp:
		return resp.snap, resp.err
	}
}

func (m *Machine) apply(ctx context.Context, cmd Command) (Snapshot, error) {
	if cmd.IdempotencyKey != "" {
		if prev, ok := m.idem[cmd.IdempotencyKey]; ok {
			return prev, nil
		}
	}
	cur := m.snap
	now := m.clk.Now()

	var (
		next State
		err  error
		sid  = cur.SessionID
	)

	switch cmd.Name {
	case CmdArm:
		if cur.State != StateIdle && cur.State != StateCompleted && cur.State != StateFailed {
			return cur, &TransitionError{From: cur.State, Cmd: cmd.Name, Reason: "arm only from idle/completed/failed"}
		}
		sid = m.ids.Next("rec")
		if err = m.sink.Prepare(ctx, sid); err != nil {
			next = StateFailed
			err = fmt.Errorf("prepare: %w", err)
		} else {
			next = StateArmed
		}
	case CmdStart:
		switch cur.State {
		case StateArmed:
			next = StateStarting
		case StateIdle:
			// arm+start
			sid = m.ids.Next("rec")
			if err = m.sink.Prepare(ctx, sid); err != nil {
				next = StateFailed
				break
			}
			next = StateStarting
		case StateRecording, StateStarting:
			// idempotent start
			return cur, nil
		default:
			return cur, &TransitionError{From: cur.State, Cmd: cmd.Name, Reason: "cannot start"}
		}
		if next == StateStarting {
			if err = m.sink.Start(ctx, sid); err != nil {
				_ = m.sink.Abort(ctx, sid, err.Error())
				next = StateFailed
			} else {
				next = StateRecording
			}
		}
	case CmdStop:
		switch cur.State {
		case StateRecording, StateStarting:
			next = StateStopping
		case StateStopping, StateCompleted:
			return cur, nil
		case StateArmed:
			// cancel arm without recording
			_ = m.sink.Abort(ctx, sid, "disarmed")
			next = StateIdle
			sid = ""
		default:
			return cur, &TransitionError{From: cur.State, Cmd: cmd.Name, Reason: "cannot stop"}
		}
		if next == StateStopping {
			if err = m.sink.Stop(ctx, sid); err != nil {
				next = StateFailed
			} else {
				next = StateCompleted
			}
		}
	case CmdFail:
		_ = m.sink.Abort(ctx, sid, cmd.Reason)
		next = StateFailed
	case CmdRecover:
		if cur.State != StateFailed && cur.State != StateRecovering {
			return cur, &TransitionError{From: cur.State, Cmd: cmd.Name, Reason: "recover only from failed"}
		}
		next = StateRecovering
		// recovery shell: mark completed recoverable → completed
		next = StateCompleted
	case CmdComplete:
		if cur.State != StateStopping && cur.State != StateRecovering {
			return cur, &TransitionError{From: cur.State, Cmd: cmd.Name, Reason: "complete not allowed"}
		}
		next = StateCompleted
	case CmdReset:
		next = StateIdle
		sid = ""
	default:
		return cur, &TransitionError{From: cur.State, Cmd: cmd.Name, Reason: "unknown command"}
	}

	snap := Snapshot{
		SessionID: sid,
		State:     next,
		Actor:     cmd.Actor,
		Reason:    cmd.Reason,
		UpdatedAt: now,
		Title:     cur.Title,
	}
	m.mu.Lock()
	m.snap = snap
	if cmd.IdempotencyKey != "" {
		m.idem[cmd.IdempotencyKey] = snap
	}
	listeners := append([]chan Snapshot(nil), m.listeners...)
	m.mu.Unlock()
	for _, ch := range listeners {
		select {
		case ch <- snap:
		default:
		}
	}
	if err != nil && next == StateFailed {
		return snap, err
	}
	return snap, nil
}

// Watch returns a non-blocking snapshot stream; cancel by calling the stop func.
func (m *Machine) Watch(buffer int) (<-chan Snapshot, func()) {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan Snapshot, buffer)
	m.mu.Lock()
	m.listeners = append(m.listeners, ch)
	m.mu.Unlock()
	stop := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for i, c := range m.listeners {
			if c == ch {
				m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, stop
}

// NewSession returns an idle session shell (legacy bootstrap helper).
func NewSession() *Session {
	return &Session{State: StateIdle}
}

// Session is a compile-time placeholder retained for package compatibility.
type Session struct {
	State State
}
