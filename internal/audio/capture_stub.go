//go:build !cgo

package audio

import (
	"context"
	"fmt"
)

// OpenNativeCapture reports the explicit build constraint without falling back
// to another microphone.
func OpenNativeCapture(ctx context.Context, source Source, format Format, handle func([]byte) error) (*Capture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("audio: native capture requires cgo")
}
