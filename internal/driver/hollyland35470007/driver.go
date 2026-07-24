package hollyland35470007

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/idgen"
)

// Driver is the compiled-in exact receiver driver.
type Driver struct {
	profile      Profile
	ids          idgen.Generator
	identitySalt []byte
}

// New returns a Hollyland 3547:0007 driver.
func New(ids idgen.Generator) *Driver {
	return NewWithIdentitySalt(ids, []byte(DriverID))
}

// NewWithIdentitySalt creates a driver whose hardware-correlated IDs are
// unlinkable to IDs from a different installation salt.
func NewWithIdentitySalt(ids idgen.Generator, salt []byte) *Driver {
	if ids == nil {
		ids = &idgen.Atomic{}
	}
	if len(salt) == 0 {
		salt = []byte(DriverID)
	}
	return &Driver{profile: DefaultProfile(), ids: ids, identitySalt: append([]byte(nil), salt...)}
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
		id := identityFor(c.Attributes["serial"], d.identitySalt)
		if c.Attributes["serial"] == "" || c.Attributes["serial"] == "present-redacted" {
			// Without a correlatable serial, candidates must remain distinct
			// and explicitly connection-scoped instead of silently collapsing.
			id.DeviceID = core.DeviceID("dev_hl_" + d.ids.Next("candidate"))
		}
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
		BusDevice: a["bus_device"], DescriptorDigest: strings.ToLower(a["descriptor_digest"]),
	}
	if len(usb.DescriptorDigest) != sha256.Size*2 {
		usb.MalformedReason = "missing or malformed descriptor digest"
		return usb
	}
	if _, err := hex.DecodeString(usb.DescriptorDigest); err != nil {
		usb.MalformedReason = "malformed descriptor digest"
		return usb
	}
	rawShape := a["interface_descriptors"]
	if rawShape == "" {
		usb.MalformedReason = "missing structured interface descriptors"
		return usb
	}
	decoder := json.NewDecoder(bytes.NewBufferString(rawShape))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&usb.Descriptors); err != nil {
		usb.MalformedReason = "malformed structured interface descriptors"
		return usb
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		usb.MalformedReason = "multiple values in interface descriptors"
		return usb
	}
	usb.InterfaceNums, usb.HasAudio, usb.HasHID, usb.HasVendorIF = summarizeDescriptors(usb.Descriptors)
	// The legacy summary must agree with the structured source if present.
	if s := a["interfaces"]; s != "" {
		values, err := splitInts(s)
		if err != nil {
			usb.MalformedReason = err.Error()
			return usb
		}
		if !sameInterfaceSet(values, usb.InterfaceNums) {
			usb.MalformedReason = "interface summary disagrees with structured descriptors"
			return usb
		}
	}
	return usb
}

func splitInts(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			return nil, fmt.Errorf("malformed interface list")
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			return nil, fmt.Errorf("malformed interface number %q", part)
		}
		out = append(out, value)
	}
	return out, nil
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
	access := DoctorAccess(audioPath, ctrlPath)
	for i := range dev.caps {
		switch dev.caps[i].Key {
		case core.CapAudioCapture:
			if audioPath == "" || !access.OK {
				dev.caps[i].Availability = core.Unavailable
				dev.caps[i].UnavailableReason = "capture/control surfaces were not correlated or accessible"
			}
		case core.CapControlPress, core.CapControlRelease:
			if ctrlPath == "" || !access.OK {
				dev.caps[i].Availability = core.Unavailable
				dev.caps[i].UnavailableReason = "evdev control node was not correlated or accessible"
			}
		}
	}
	return dev, nil
}

type device struct {
	identity    core.Identity
	conn        core.ConnectionID
	surfaces    []core.Surface
	caps        []core.Capability
	mu          sync.Mutex
	closed      bool
	events      chan driver.ControlEvent
	eventCancel context.CancelFunc
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
	eventCtx, cancel := context.WithCancel(ctx)
	d.events = ch
	d.eventCancel = cancel
	go func() {
		<-eventCtx.Done()
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.events == ch {
			close(ch)
			d.events = nil
			d.eventCancel = nil
		}
	}()
	controlPath := ""
	for _, surface := range d.surfaces {
		if surface.Kind == core.SurfaceControl {
			controlPath = surface.TransientPath
			break
		}
	}
	if controlPath != "" {
		go func() {
			_ = readEvdev(eventCtx, controlPath, d.InjectControl)
			cancel()
		}()
	}
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
		return nil
	}
	d.closed = true
	if d.eventCancel != nil {
		d.eventCancel()
		d.eventCancel = nil
	}
	if d.events != nil {
		close(d.events)
		d.events = nil
	}
	return nil
}
