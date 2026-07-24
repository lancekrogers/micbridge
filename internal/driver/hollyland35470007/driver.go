package hollyland35470007

import (
	"context"
	"fmt"
	"sync"

	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

// Driver is the compiled-in exact receiver driver.
type Driver struct {
	profile Profile
	ids     idgen.Generator
}

// New returns a Hollyland 3547:0007 driver.
func New(ids idgen.Generator) *Driver {
	if ids == nil {
		ids = &idgen.Atomic{}
	}
	return &Driver{profile: DefaultProfile(), ids: ids}
}

func (d *Driver) ID() string      { return DriverID }
func (d *Driver) Version() string { return DriverVersion }

// Match parses candidate attributes into USB shape and evaluates the profile.
func (d *Driver) Match(ctx context.Context, c driver.Candidate) (driver.Match, error) {
	if err := ctx.Err(); err != nil {
		return driver.Match{}, err
	}
	usb := candidateFromAttrs(c)
	res, why := EvaluateMatch(d.profile, usb)
	switch res {
	case MatchExact:
		id := IdentityFor(c.Attributes["serial"])
		return driver.Match{
			DriverID: DriverID, DriverVersion: DriverVersion,
			Confidence: "exact", Identity: id,
		}, nil
	case MatchNearMiss, MatchFuture:
		return driver.Match{}, fmt.Errorf("%w: %s", ErrNearMiss, why)
	case MatchMalformed:
		return driver.Match{}, fmt.Errorf("hollyland: malformed: %s", why)
	default:
		return driver.Match{}, driver.ErrNoMatch
	}
}

func candidateFromAttrs(c driver.Candidate) CandidateUSB {
	a := c.Attributes
	if a == nil {
		return CandidateUSB{}
	}
	usb := CandidateUSB{
		VID: a["usb_vid"], PID: a["usb_pid"],
		Manufacturer: a["manufacturer"], Product: a["product"],
		HasHID:      a["has_hid"] == "1" || a["has_hid"] == "true",
		HasAudio:    a["has_audio"] == "1" || a["has_audio"] == "true",
		HasVendorIF: a["has_vendor"] == "1" || a["has_vendor"] == "true",
		BusDevice:   a["bus_device"],
	}
	// interfaces as comma list optional
	if s := a["interfaces"]; s != "" {
		usb.InterfaceNums = append(usb.InterfaceNums, splitInts(s)...)
	}
	return usb
}

func splitInts(s string) []int {
	var out []int
	cur := 0
	has := false
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if has {
				out = append(out, cur)
			}
			cur = 0
			has = false
			continue
		}
		if s[i] >= '0' && s[i] <= '9' {
			cur = cur*10 + int(s[i]-'0')
			has = true
		}
	}
	return out
}

// Open groups audio + control surfaces under one connection.
func (d *Driver) Open(ctx context.Context, m driver.Match, c driver.Candidate) (driver.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	audioPath := ""
	ctrlPath := ""
	if c.TransientPaths != nil {
		audioPath = c.TransientPaths["audio"]
		ctrlPath = c.TransientPaths["control"]
	}
	dev := &device{
		identity: m.Identity,
		conn:     core.ConnectionID(d.ids.Next("conn")),
		surfaces: []core.Surface{
			{ID: "audio0", Kind: core.SurfaceAudioCapture, Label: "USB Audio", TransientPath: audioPath},
			{ID: "ctrl0", Kind: core.SurfaceControl, Label: "Receiver button", TransientPath: ctrlPath},
			{ID: "vendor0", Kind: core.SurfaceVendor, Label: "Vendor bulk (metadata only)", TransientPath: c.Attributes["vendor"]},
		},
		caps: Capabilities(),
	}
	return dev, nil
}

type device struct {
	identity core.Identity
	conn     core.ConnectionID
	surfaces []core.Surface
	caps     []core.Capability
	mu       sync.Mutex
	closed   bool
	events   chan driver.ControlEvent
}

func (d *device) Identity() core.Identity         { return d.identity }
func (d *device) Capabilities() []core.Capability { return append([]core.Capability(nil), d.caps...) }
func (d *device) ConnectionID() core.ConnectionID { return d.conn }
func (d *device) Surfaces() []core.Surface        { return append([]core.Surface(nil), d.surfaces...) }

func (d *device) AudioSources(ctx context.Context) ([]driver.AudioSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []driver.AudioSource{{
		ID: "src_" + string(d.identity.DeviceID), Label: "Hollyland capture",
		Channels: 2, RateHz: 48000,
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
	ch := make(chan driver.ControlEvent, 8)
	d.events = ch
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

// InjectControl publishes a parsed control event (tests / platform adapter).
func (d *device) InjectControl(ev driver.ControlEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.events == nil {
		return driver.ErrClosed
	}
	select {
	case d.events <- ev:
		return nil
	default:
		return fmt.Errorf("hollyland: event buffer full")
	}
}

// Inject publishes a control event into an opened Hollyland device.
func Inject(dev driver.Device, ev driver.ControlEvent) error {
	d, ok := dev.(*device)
	if !ok {
		return fmt.Errorf("hollyland: not a hollyland device")
	}
	return d.InjectControl(ev)
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
