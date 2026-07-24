//go:build !linux

package hollyland35470007

import (
	"context"
	"fmt"

	"github.com/lancekrogers/micbridge/internal/driver"
)

func DiscoverLinux(ctx context.Context, usbRoot, inputClassRoot, devRoot string) ([]driver.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("hollyland: Linux discovery is unavailable on this platform")
}
