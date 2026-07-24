package binding

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/core"
)

const Schema = "micbridge.binding/v1"

// ID is a stable binding identifier.
type ID string

// Action is a product action a control can invoke.
type Action string

const (
	ActionRecordingToggle Action = "recording.toggle"
	ActionRecordingStart  Action = "recording.start"
	ActionRecordingStop   Action = "recording.stop"
	ActionNone            Action = "none"
)

// Binding maps a device control to an action.
type Binding struct {
	Schema      string        `json:"schema"`
	ID          ID            `json:"id"`
	DeviceID    core.DeviceID `json:"device_id"`
	Control     string        `json:"control"`
	On          string        `json:"on"` // pressed, released
	Action      Action        `json:"action"`
	DebounceMs  int           `json:"debounce_ms,omitempty"`
	Enabled     bool          `json:"enabled"`
	SideEffects []string      `json:"side_effects,omitempty"`
	Reason      string        `json:"reason,omitempty"` // why inactive/invalid
}

// Validate checks structural binding rules.
func (b Binding) Validate() error {
	if b.Schema != Schema {
		return fmt.Errorf("binding: unsupported schema %q", b.Schema)
	}
	if b.ID == "" {
		return fmt.Errorf("binding: id is required")
	}
	if err := b.DeviceID.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(b.Control) == "" {
		return fmt.Errorf("binding: control is required")
	}
	switch b.On {
	case "pressed", "released":
	default:
		return fmt.Errorf("binding: on must be pressed or released")
	}
	switch b.Action {
	case ActionRecordingToggle, ActionRecordingStart, ActionRecordingStop, ActionNone:
	default:
		return fmt.Errorf("binding: unknown action %q", b.Action)
	}
	return nil
}

// Decision is an auditable match result for one control event.
type Decision struct {
	BindingID   ID
	Action      Action
	Accepted    bool
	Reason      string
	SideEffects []string
}

// Command is submitted to the recording owner when a binding activates.
type Command struct {
	BindingID ID
	Action    Action
	DeviceID  core.DeviceID
	PressID   string
	At        time.Time
}

// ControlInput is a normalized control observation for matching.
type ControlInput struct {
	DeviceID    core.DeviceID
	Type        string // pressed, released, held
	Control     string
	PressID     string
	At          time.Time
	SideEffects []string
}

// Engine evaluates bindings and persists them under a state root.
type Engine struct {
	mu       sync.Mutex
	clk      clock.Clock
	path     string
	bindings map[ID]Binding
	// lastFire[pressKey] = last fire time for debounce
	lastFire map[string]time.Time
	// seenPress actions by press_id+binding for idempotency
	seen map[string]struct{}
}

// NewEngine returns an empty binding engine (in-memory only).
func NewEngine() *Engine {
	return New(nil, "")
}

// New constructs an engine with optional clock and persistence path.
func New(clk clock.Clock, storePath string) *Engine {
	if clk == nil {
		clk = clock.System{}
	}
	return &Engine{
		clk:      clk,
		path:     storePath,
		bindings: make(map[ID]Binding),
		lastFire: make(map[string]time.Time),
		seen:     make(map[string]struct{}),
	}
}

// Upsert validates, checks capabilities when provided, and stores a binding.
func (e *Engine) Upsert(b Binding, caps []core.Capability) (Binding, error) {
	if err := b.Validate(); err != nil {
		return Binding{}, err
	}
	b, err := e.activatePolicy(b, caps)
	if err != nil {
		return Binding{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bindings[b.ID] = b
	if err := e.persistLocked(); err != nil {
		return Binding{}, err
	}
	return b, nil
}

func (e *Engine) activatePolicy(b Binding, caps []core.Capability) (Binding, error) {
	if !b.Enabled {
		if b.Reason == "" {
			b.Reason = "disabled"
		}
		return b, nil
	}
	if caps == nil {
		return b, nil
	}
	// Require a callable control.press (or release) capability for activation.
	need := core.CapControlPress
	if b.On == "released" {
		need = core.CapControlRelease
	}
	var found *core.Capability
	for i := range caps {
		if caps[i].Key == need {
			found = &caps[i]
			break
		}
	}
	if found == nil {
		b.Enabled = false
		b.Reason = fmt.Sprintf("device lacks capability %s", need)
		return b, nil
	}
	if !found.Callable() {
		b.Enabled = false
		b.Reason = fmt.Sprintf("capability %s not callable: %s", need, found.UnavailableReason)
		if b.Reason == fmt.Sprintf("capability %s not callable: ", need) {
			b.Reason = fmt.Sprintf("capability %s not callable (%s)", need, found.Availability)
		}
		return b, nil
	}
	// Disclose side effects from capability.
	for _, se := range found.SideEffects {
		b.SideEffects = appendUnique(b.SideEffects, se.ID)
	}
	b.Reason = ""
	return b, nil
}

func appendUnique(ss []string, v string) []string {
	for _, s := range ss {
		if s == v {
			return ss
		}
	}
	return append(ss, v)
}

// Delete removes a binding.
func (e *Engine) Delete(id ID) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.bindings, id)
	return e.persistLocked()
}

// List returns all bindings including inactive ones.
func (e *Engine) List() []Binding {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Binding, 0, len(e.bindings))
	for _, b := range e.bindings {
		out = append(out, b)
	}
	return out
}

// Get returns a binding by id.
func (e *Engine) Get(id ID) (Binding, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, ok := e.bindings[id]
	return b, ok
}

// Handle evaluates input against bindings and returns at most one command.
func (e *Engine) Handle(in ControlInput) (Decision, *Command) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := in.At
	if now.IsZero() {
		now = e.clk.Now()
	}
	for _, b := range e.bindings {
		if b.DeviceID != in.DeviceID || b.Control != in.Control || b.On != in.Type {
			continue
		}
		if !b.Enabled {
			return Decision{BindingID: b.ID, Action: b.Action, Accepted: false, Reason: b.Reason}, nil
		}
		if b.Action == ActionNone {
			return Decision{BindingID: b.ID, Action: b.Action, Accepted: false, Reason: "action is none"}, nil
		}
		// Debounce
		key := string(b.ID) + "|" + b.Control + "|" + b.On
		if b.DebounceMs > 0 {
			if last, ok := e.lastFire[key]; ok && now.Sub(last) < time.Duration(b.DebounceMs)*time.Millisecond {
				return Decision{BindingID: b.ID, Action: b.Action, Accepted: false, Reason: "debounced"}, nil
			}
		}
		// Idempotency per press_id
		if in.PressID != "" {
			idem := string(b.ID) + "|" + in.PressID + "|" + b.On
			if _, ok := e.seen[idem]; ok {
				return Decision{BindingID: b.ID, Action: b.Action, Accepted: false, Reason: "duplicate_press_id"}, nil
			}
			e.seen[idem] = struct{}{}
		}
		e.lastFire[key] = now
		cmd := &Command{
			BindingID: b.ID,
			Action:    b.Action,
			DeviceID:  b.DeviceID,
			PressID:   in.PressID,
			At:        now,
		}
		return Decision{
			BindingID:   b.ID,
			Action:      b.Action,
			Accepted:    true,
			Reason:      "matched",
			SideEffects: append([]string(nil), b.SideEffects...),
		}, cmd
	}
	return Decision{Accepted: false, Reason: "no_binding"}, nil
}

func (e *Engine) persistLocked() error {
	if e.path == "" {
		return nil
	}
	type file struct {
		Schema   string    `json:"schema"`
		Bindings []Binding `json:"bindings"`
	}
	f := file{Schema: Schema}
	for _, b := range e.bindings {
		f.Bindings = append(f.Bindings, b)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
		return err
	}
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.path)
}

// Load reads bindings from the store path.
func (e *Engine) Load() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.path == "" {
		return nil
	}
	data, err := os.ReadFile(e.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f struct {
		Bindings []Binding `json:"bindings"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	e.bindings = make(map[ID]Binding)
	for _, b := range f.Bindings {
		if err := b.Validate(); err != nil {
			continue
		}
		e.bindings[b.ID] = b
	}
	return nil
}
