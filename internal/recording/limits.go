package recording

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Policy configures resource and disconnect behavior.
type Policy struct {
	MaxDuration     time.Duration
	MinFreeBytes    uint64
	OutputRoot      string
	DisconnectGrace time.Duration
	// AllowReconnectSameID permits resume only for the same SourceID.
	AllowReconnectSameID bool
	// FreeBytes overrides filesystem-space measurement for unusual filesystems
	// and deterministic fault tests. Nil selects the platform implementation.
	FreeBytes func(path string) (uint64, error)
}

// DefaultPolicy returns conservative defaults.
func DefaultPolicy(outputRoot string) Policy {
	return Policy{
		MaxDuration:          2 * time.Hour,
		MinFreeBytes:         64 * 1024 * 1024,
		OutputRoot:           outputRoot,
		DisconnectGrace:      5 * time.Second,
		AllowReconnectSameID: true,
	}
}

// LimitError is an actionable policy failure.
type LimitError struct {
	Code    string
	Message string
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("recording limit %s: %s", e.Code, e.Message)
}

// CheckArm validates preconditions before arm/start.
func (p Policy) CheckArm() error {
	if p.OutputRoot == "" {
		return &LimitError{Code: "output_root", Message: "output root is required"}
	}
	clean := filepath.Clean(p.OutputRoot)
	if err := os.MkdirAll(clean, 0o700); err != nil {
		return &LimitError{Code: "output_root", Message: err.Error()}
	}
	if p.MinFreeBytes > 0 {
		measure := p.FreeBytes
		if measure == nil {
			measure = freeBytes
		}
		free, err := measure(clean)
		if err != nil {
			// best-effort; do not fail arm if FS stats unavailable
			return nil
		}
		if free < p.MinFreeBytes {
			return &LimitError{
				Code:    "disk_space",
				Message: fmt.Sprintf("need %d free bytes, have %d", p.MinFreeBytes, free),
			}
		}
	}
	return nil
}

// CheckDuration fails if elapsed exceeds max.
func (p Policy) CheckDuration(elapsed time.Duration) error {
	if p.MaxDuration > 0 && elapsed > p.MaxDuration {
		return &LimitError{Code: "max_duration", Message: fmt.Sprintf("elapsed %s exceeds max %s", elapsed, p.MaxDuration)}
	}
	return nil
}

// SourceLoss decides disconnect handling.
type SourceLoss struct {
	SourceID      string
	LostAt        time.Time
	Grace         time.Duration
	ReconnectedID string
}

// Resolve returns failed reason or empty if still in grace / reconnected same ID.
func (p Policy) ResolveSourceLoss(loss SourceLoss, now time.Time) (fail bool, reason string) {
	if loss.ReconnectedID != "" {
		if p.AllowReconnectSameID && loss.ReconnectedID == loss.SourceID {
			return false, ""
		}
		return true, "refusing substitution of a different capture source"
	}
	grace := p.DisconnectGrace
	if loss.Grace > 0 {
		grace = loss.Grace
	}
	if now.Sub(loss.LostAt) <= grace {
		return false, "within_disconnect_grace"
	}
	return true, "source lost after grace; partial preserved"
}

// freeBytes is OS-specific; Linux uses Statfs via syscall in free_linux.go.
var freeBytes = freeBytesFallback

func freeBytesFallback(path string) (uint64, error) {
	// Portable fallback: assume enough space if directory is writable.
	f, err := os.CreateTemp(path, ".space-check-*")
	if err != nil {
		return 0, err
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return 1 << 40, nil // unknown large
}
