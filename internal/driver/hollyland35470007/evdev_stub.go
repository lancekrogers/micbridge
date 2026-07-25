//go:build !linux

package hollyland35470007

import (
	"context"
	"fmt"

	"github.com/lancekrogers/micbridge/internal/driver"
)

func readEvdev(ctx context.Context, path string, emit func(driver.ControlEvent) error) error {
	return fmt.Errorf("hollyland: evdev is unavailable on this platform")
}
