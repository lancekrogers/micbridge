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
		defer func() {
			_ = ac.Uninit()
			ac.Free()
		}()
	}
	infos, err := ac.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("audio: list capture devices: %w", err)
	}
	out := make([]Source, 0, len(infos))
	for _, info := range infos {
		if full, infoErr := ac.DeviceInfo(malgo.Capture, info.ID, malgo.Shared); infoErr == nil {
			info = full
		}
		name := info.Name()
		id := stableSourceID(info.ID.String())
		var nativeFormats, requestedFormats []Format
		for _, native := range info.Formats {
			bits := bitsForMalgo(native.Format)
			if native.SampleRate == 0 || native.Channels == 0 || bits == 0 {
				continue
			}
			nativeFormat := Format{
				SampleRate: native.SampleRate, Channels: native.Channels, BitsPerSample: bits,
			}
			nativeFormats = appendFormat(nativeFormats, nativeFormat)
			// Miniaudio performs an explicit conversion when the backend can
			// open the native shape. Keep requested and measured formats
			// separate instead of claiming every source is natively S16.
			requestedFormats = appendFormat(requestedFormats, nativeFormat)
			requestedFormats = appendFormat(requestedFormats, Format{
				SampleRate: native.SampleRate, Channels: native.Channels, BitsPerSample: 16,
			})
		}
		out = append(out, Source{
			ID:            id,
			Name:          name,
			Backend:       "malgo",
			TransientHint: info.ID.String(),
			Formats:       requestedFormats,
			NativeFormats: nativeFormats,
			IdentityScope: "connection",
			Available:     true,
		})
	}
	return DedupeNames(out), nil
}

func stableSourceID(nativeID string) SourceID {
	h := fnv.New64a()
	_, _ = h.Write([]byte(nativeID))
	return SourceID("src_" + strconv.FormatUint(h.Sum64(), 16))
}

func appendFormat(formats []Format, candidate Format) []Format {
	for _, format := range formats {
		if format == candidate {
			return formats
		}
	}
	return append(formats, candidate)
}
