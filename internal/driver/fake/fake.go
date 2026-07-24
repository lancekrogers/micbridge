// Package fake implements a scriptable deterministic driver for tests and demos.
package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

const (
	DriverID      = "fake"
	DriverVersion = "1.0.0"
)

// Scenario controls scripted behavior for a fake device.
type Scenario struct {
	// MatchAttribute key/value required to match (default profile=fake).
	MatchKey   string
	MatchValue string
	// PermissionDenied makes Open succeed but AudioSources fail.
	PermissionDenied bool
	// Events is the scripted control sequence delivered after Open.
	Events []driver.ControlEvent
	// MalformedDrop drops events with empty Type when injecting the script.
	// DisconnectAfter, if >0, closes the event stream after that many events.
	DisconnectAfter int
	// DeviceID overrides generated stable id.
	DeviceID core.DeviceID
}

// Driver is a compiled-in fake driver.
type Driver struct {
	clk  clock.Clock
	ids  idgen.Generator
	scen Scenario
}

// Option configures the fake driver.
type Option func(*Driver)

// WithClock injects a clock (defaults to system).
func WithClock(c clock.Clock) Option {
	return func(d *Driver) { d.clk = c }
}

// WithIDs injects an id generator.
func WithIDs(g idgen.Generator) Option {
	return func(d *Driver) { d.ids = g }
}

// WithScenario sets scripted behavior.
func WithScenario(s Scenario) Option {
	return func(d *Driver) { d.scen = s }
}

// New returns a fake driver.
func New(opts ...Option) *Driver {
	d := &Driver{
		clk: clock.System{},
		ids: &idgen.Atomic{},
		scen: Scenario{
			MatchKey:   "profile",
			MatchValue: "fake",
		},
	}
	for _, o := range opts {
		o(d)
	}
	if d.scen.MatchKey == "" {
		d.scen.MatchKey = "profile"
	}
	if d.scen.MatchValue == "" {
		d.scen.MatchValue = "fake"
	}
	return d
}

func (d *Driver) ID() string      { return DriverID }
func (d *Driver) Version() string { return DriverVersion }

// Match recognizes candidates with the configured attribute pair.
func (d *Driver) Match(ctx context.Context, c driver.Candidate) (driver.Match, error) {
	if err := ctx.Err(); err != nil {
		return driver.Match{}, err
	}
	if c.Attributes == nil || c.Attributes[d.scen.MatchKey] != d.scen.MatchValue {
		return driver.Match{}, driver.ErrNoMatch
	}
	devID := d.scen.DeviceID
	if devID == "" {
		devID = core.DeviceID(d.ids.Next("dev"))
	}
	id := core.Identity{
		Schema:        core.IdentitySchema,
		DeviceID:      devID,
		DriverID:      DriverID,
		DriverVersion: DriverVersion,
		DisplayName:   "Fake Microphone",
		Attributes:    map[string]string{d.scen.MatchKey: d.scen.MatchValue},
	}
	return driver.Match{
		DriverID:      DriverID,
		DriverVersion: DriverVersion,
		Confidence:    "exact",
		Identity:      id,
	}, nil
}

// Open creates a scriptable device instance.
func (d *Driver) Open(ctx context.Context, m driver.Match, c driver.Candidate) (driver.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.DriverID != DriverID {
		return nil, fmt.Errorf("fake: unexpected driver id %q", m.DriverID)
	}
	conn := core.ConnectionID(d.ids.Next("conn"))
	pathAudio := ""
	pathCtrl := ""
	if c.TransientPaths != nil {
		pathAudio = c.TransientPaths["audio"]
		pathCtrl = c.TransientPaths["control"]
	}
	dev := &device{
		identity: m.Identity,
		conn:     conn,
		scen:     d.scen,
		clk:      d.clk,
		surfaces: []core.Surface{
			{ID: "audio0", Kind: core.SurfaceAudioCapture, Label: "Fake Capture", TransientPath: pathAudio},
			{ID: "ctrl0", Kind: core.SurfaceControl, Label: "Fake Control", TransientPath: pathCtrl},
		},
		caps: defaultCaps(d.scen),
	}
	return dev, nil
}

func defaultCaps(s Scenario) []core.Capability {
	audioReason := ""
	audioAvail := core.Available
	if s.PermissionDenied {
		audioAvail = core.Unavailable
		audioReason = "capture permission denied (fake scenario)"
	}
	return []core.Capability{
		{
			Schema:            core.CapabilitySchema,
			Key:               core.CapAudioCapture,
			Availability:      audioAvail,
			UnavailableReason: audioReason,
			Evidence:          core.Evidence{Level: core.EvidenceMeasured, Summary: "fake source"},
			DriverVersion:     DriverVersion,
		},
		{
			Schema:        core.CapabilitySchema,
			Key:           core.CapControlPress,
			Availability:  core.Available,
			Evidence:      core.Evidence{Level: core.EvidenceMeasured, Summary: "scripted"},
			SideEffects:   []core.SideEffect{{ID: "host.volume_increment", Passive: true, Description: "fake side effect"}},
			DriverVersion: DriverVersion,
		},
		{
			Schema:            core.CapabilitySchema,
			Key:               core.CapSettingNoiseCancellation,
			Availability:      core.Unsupported,
			UnavailableReason: "fake device has no NC control",
			Evidence:          core.Evidence{Level: core.EvidenceMeasuredNegative, Summary: "not simulated"},
			DriverVersion:     DriverVersion,
		},
	}
}

type device struct {
	identity core.Identity
	conn     core.ConnectionID
	scen     Scenario
	clk      clock.Clock
	surfaces []core.Surface
	caps     []core.Capability

	mu     sync.Mutex
	closed bool
	events chan driver.ControlEvent
}

func (d *device) Identity() core.Identity         { return d.identity }
func (d *device) Capabilities() []core.Capability { return append([]core.Capability(nil), d.caps...) }
func (d *device) ConnectionID() core.ConnectionID { return d.conn }
func (d *device) Surfaces() []core.Surface        { return append([]core.Surface(nil), d.surfaces...) }

func (d *device) AudioSources(ctx context.Context) ([]driver.AudioSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, driver.ErrClosed
	}
	if d.scen.PermissionDenied {
		return nil, fmt.Errorf("fake: permission denied")
	}
	return []driver.AudioSource{{
		ID:       "src_fake_0",
		Label:    "Fake Capture",
		Channels: 2,
		RateHz:   48000,
	}}, nil
}

func (d *device) Events(ctx context.Context) (<-chan driver.ControlEvent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, driver.ErrClosed
	}
	if d.events != nil {
		return nil, driver.ErrAlreadyOpen
	}
	ch := make(chan driver.ControlEvent, 16)
	d.events = ch
	go d.runScript(ctx, ch)
	return ch, nil
}

func (d *device) runScript(ctx context.Context, ch chan driver.ControlEvent) {
	defer close(ch)
	sent := 0
	for _, ev := range d.scen.Events {
		if err := ctx.Err(); err != nil {
			return
		}
		if ev.Type == "" {
			// Malformed event: skip (observation dropped), do not panic.
			continue
		}
		select {
		case <-ctx.Done():
			return
		case ch <- ev:
			sent++
		}
		if d.scen.DisconnectAfter > 0 && sent >= d.scen.DisconnectAfter {
			return
		}
		// Deterministic spacing using injected clock observation only (no sleep on fake clock).
		_ = d.clk.Now()
	}
	// Keep stream open until cancel/close unless disconnect-after exhausted the script.
	if d.scen.DisconnectAfter > 0 {
		return
	}
	<-ctx.Done()
}

func (d *device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return driver.ErrClosed
	}
	d.closed = true
	return nil
}

// ReopenCandidate builds a candidate for reconnect tests with new transient paths.
func ReopenCandidate(profile string, audioPath, ctrlPath string) driver.Candidate {
	if profile == "" {
		profile = "fake"
	}
	return driver.Candidate{
		Attributes:     map[string]string{"profile": profile},
		TransientPaths: map[string]string{"audio": audioPath, "control": ctrlPath},
	}
}

// Idle pause helper for tests that use real time sparingly.
func Pause(d time.Duration) { time.Sleep(d) }
