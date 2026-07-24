//go:build !cgo

package audio

import (
	"context"
	"fmt"
)

// MalgoEnumerator is unavailable without cgo.
type MalgoEnumerator struct{}

// List returns an explicit error when cgo is disabled.
func (m *MalgoEnumerator) List(ctx context.Context) ([]Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("audio: malgo enumeration requires cgo")
}
