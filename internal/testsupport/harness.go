package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
	"github.com/lancekrogers/micbridge/internal/idgen"
	"github.com/lancekrogers/micbridge/internal/storage"
)

// Harness bundles deterministic seams for a single test.
type Harness struct {
	Clock  *clock.Fake
	IDs    *idgen.Seeded
	Layout *storage.Layout
}

// NewHarness builds a fully deterministic test environment.
// seed controls identifier sequences; start fixes the fake clock.
func NewHarness(t *testing.T, seed string, start time.Time) *Harness {
	t.Helper()
	return &Harness{
		Clock:  clock.NewFake(start),
		IDs:    idgen.NewSeeded(seed),
		Layout: TempRoot(t),
	}
}

// Context returns a context cancelled when the test ends.
func (h *Harness) Context(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
