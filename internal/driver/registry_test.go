package driver_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/lancekrogers/micbridge/internal/driver"
	"github.com/lancekrogers/micbridge/internal/driver/fake"
)

func TestSelectAmbiguousWithTwoMatchingDrivers(t *testing.T) {
	// Two drivers with different IDs but same match criteria require wrappers.
	reg := driver.NewRegistry()
	a := &aliasDriver{Driver: fake.New(fake.WithScenario(fake.Scenario{DeviceID: "dev_a"})), id: "fake-a"}
	b := &aliasDriver{Driver: fake.New(fake.WithScenario(fake.Scenario{DeviceID: "dev_b"})), id: "fake-b"}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(b); err != nil {
		t.Fatal(err)
	}
	_, _, err := reg.Select(context.Background(), driver.Candidate{
		Attributes: map[string]string{"profile": "fake"},
	})
	if !errors.Is(err, driver.ErrAmbiguous) {
		t.Fatalf("want ambiguous, got %v", err)
	}
}

type aliasDriver struct {
	*fake.Driver
	id string
}

func (a *aliasDriver) ID() string { return a.id }

type wrappedNoMatchDriver struct {
	*fake.Driver
}

func (d *wrappedNoMatchDriver) ID() string { return "wrapped-no-match" }
func (d *wrappedNoMatchDriver) Match(context.Context, driver.Candidate) (driver.Match, error) {
	return driver.Match{}, fmt.Errorf("candidate rejected: %w", driver.ErrNoMatch)
}

func TestWrappedNoMatchDoesNotAbortSelection(t *testing.T) {
	reg := driver.NewRegistry()
	if err := reg.Register(&wrappedNoMatchDriver{Driver: fake.New()}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fake.New()); err != nil {
		t.Fatal(err)
	}
	_, selected, err := reg.Select(context.Background(), driver.Candidate{Attributes: map[string]string{"profile": "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID() != fake.DriverID {
		t.Fatalf("selected=%s", selected.ID())
	}
}
