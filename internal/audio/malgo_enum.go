//go:build cgo

package audio

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/gen2brain/malgo"
)

// MalgoEnumerator lists capture devices via miniaudio/malgo.
type MalgoEnumerator struct {
	// Context is optional; created on demand when nil.
	Context *malgo.AllocatedContext
}

// List returns capture devices with normalized descriptors.
func (m *MalgoEnumerator) List(ctx context.Context) ([]Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ac := m.Context
	var err error
	if ac == nil {
		ac, err = malgo.InitContext(nil, malgo.ContextConfig{}, nil)
		if err != nil {
			return nil, fmt.Errorf("audio: malgo init: %w", err)
		}
		// Free releases the allocated context; Uninit is invoked by Free paths
		// on this malgo version — only Free to avoid double-teardown crashes.
		defer ac.Free()
	}
	infos, err := ac.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("audio: list capture devices: %w", err)
	}
	out := make([]Source, 0, len(infos))
	for _, info := range infos {
		name := info.Name()
		id := stableSourceID(name, info.ID.String())
		// miniaudio may not expose full format lists on all backends; advertise
		// the measured Hollyland-compatible format as negotiable default probe.
		formats := []Format{{SampleRate: 48000, Channels: 2, BitsPerSample: 16}}
		out = append(out, Source{
			ID:            id,
			Name:          name,
			Backend:       "malgo",
			TransientHint: info.ID.String(),
			Formats:       formats,
			Available:     true,
		})
	}
	return DedupeNames(out), nil
}

func stableSourceID(name, nativeID string) SourceID {
	h := fnv.New64a()
	_, _ = h.Write([]byte(nativeID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(name))
	return SourceID("src_" + strconv.FormatUint(h.Sum64(), 16))
}
