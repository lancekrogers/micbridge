package driver

import (
	"context"
	"fmt"

	"github.com/lancekrogers/micbridge/internal/core"
)

// Candidate is a platform observation that may match a driver.
type Candidate struct {
	// Attributes are stable-ish descriptors (VID:PID, serial hints, etc.).
	Attributes map[string]string
	// TransientPaths are connection-scoped OS paths (never stored as identity).
	TransientPaths map[string]string
}

// Match is a successful driver recognition of a candidate.
type Match struct {
	DriverID      string
	DriverVersion string
	Confidence    string // e.g. exact, partial
	Identity      core.Identity
}

// ControlEvent is a normalized control observation from a device.
type ControlEvent struct {
	Type        string // pressed, released, held
	Control     string
	PressID     string
	SideEffects []string
}

// AudioSource describes a capture surface without opening OS handles for clients.
type AudioSource struct {
	ID       string
	Label    string
	Channels int
	RateHz   int
}

// Device is an opened driver instance for one physical attachment.
type Device interface {
	Identity() core.Identity
	Capabilities() []core.Capability
	ConnectionID() core.ConnectionID
	Surfaces() []core.Surface
	Events(ctx context.Context) (<-chan ControlEvent, error)
	AudioSources(ctx context.Context) ([]AudioSource, error)
	// Inject is optional for scriptable drivers; production drivers return ErrNotSupported.
	Close() error
}

// Driver recognizes and opens devices of one reviewed profile family.
type Driver interface {
	ID() string
	Version() string
	Match(ctx context.Context, c Candidate) (Match, error)
	Open(ctx context.Context, m Match, c Candidate) (Device, error)
}

// Sentinel errors for registry selection.
var (
	ErrNoMatch      = fmt.Errorf("driver: no match")
	ErrAmbiguous    = fmt.Errorf("driver: ambiguous match")
	ErrUnsupported  = fmt.Errorf("driver: unsupported candidate")
	ErrNotSupported = fmt.Errorf("driver: operation not supported")
	ErrAlreadyOpen  = fmt.Errorf("driver: already open")
	ErrClosed       = fmt.Errorf("driver: closed")
)
