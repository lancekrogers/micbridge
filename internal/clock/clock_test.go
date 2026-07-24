package clock_test

import (
	"testing"
	"time"

	"github.com/lancekrogers/micbridge/internal/clock"
)

func TestFakeIsDeterministicAndAdvanceable(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	a := clock.NewFake(start)
	b := clock.NewFake(start)
	if !a.Now().Equal(b.Now()) {
		t.Fatalf("seeds diverged: %v vs %v", a.Now(), b.Now())
	}
	a.Advance(250 * time.Millisecond)
	if got := a.Since(start); got != 250*time.Millisecond {
		t.Fatalf("Since = %v", got)
	}
	if a.Now().Equal(b.Now()) {
		t.Fatal("expected advance to move clock")
	}
	b.Advance(250 * time.Millisecond)
	if !a.Now().Equal(b.Now()) {
		t.Fatal("parallel fakes with same advances should match")
	}
}
