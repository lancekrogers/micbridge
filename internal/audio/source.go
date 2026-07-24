package audio

import (
	"fmt"
	"strings"
)

// SourceID is a stable capture-source identifier assigned by Micbridge.
// Volatile OS indexes must not be persisted as identity.
type SourceID string

// Format describes a PCM capture format the source can negotiate.
type Format struct {
	SampleRate    uint32 `json:"sample_rate"`
	Channels      uint32 `json:"channels"`
	BitsPerSample uint32 `json:"bits_per_sample"`
}

// Source is a stable-ish capture descriptor (never an ALSA index alone).
type Source struct {
	ID      SourceID `json:"id"`
	Name    string   `json:"name"`
	Backend string   `json:"backend,omitempty"`
	// TransientHint is connection-scoped (card path); never used as identity.
	TransientHint     string   `json:"transient_hint,omitempty"`
	Formats           []Format `json:"formats,omitempty"`
	Available         bool     `json:"available"`
	UnavailableReason string   `json:"unavailable_reason,omitempty"`
}

// Validate checks source structure.
func (s Source) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("audio: source id required")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("audio: source name required")
	}
	return nil
}

// Negotiate picks an exact format match; never silently substitutes.
func Negotiate(src Source, want Format) (Format, error) {
	if !src.Available {
		return Format{}, fmt.Errorf("audio: source %s unavailable: %s", src.ID, src.UnavailableReason)
	}
	if want.SampleRate == 0 || want.Channels == 0 || want.BitsPerSample == 0 {
		return Format{}, fmt.Errorf("audio: explicit format required (no default fallback)")
	}
	for _, f := range src.Formats {
		if f.SampleRate == want.SampleRate && f.Channels == want.Channels && f.BitsPerSample == want.BitsPerSample {
			return f, nil
		}
	}
	return Format{}, fmt.Errorf("audio: source %s does not support %dHz %dch %dbit", src.ID, want.SampleRate, want.Channels, want.BitsPerSample)
}

// SelectByID finds an exact source; missing sources fail visibly.
func SelectByID(sources []Source, id SourceID) (Source, error) {
	if id == "" {
		return Source{}, fmt.Errorf("audio: explicit source id required (no default microphone fallback)")
	}
	var matches []Source
	for _, s := range sources {
		if s.ID == id {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		return Source{}, fmt.Errorf("audio: source %q not found", id)
	case 1:
		return matches[0], nil
	default:
		return Source{}, fmt.Errorf("audio: ambiguous source id %q (%d matches)", id, len(matches))
	}
}

// DedupeNames annotates duplicate friendly names without changing IDs.
func DedupeNames(sources []Source) []Source {
	counts := map[string]int{}
	for _, s := range sources {
		counts[s.Name]++
	}
	seen := map[string]int{}
	out := make([]Source, len(sources))
	for i, s := range sources {
		out[i] = s
		if counts[s.Name] > 1 {
			seen[s.Name]++
			out[i].Name = fmt.Sprintf("%s (%d)", s.Name, seen[s.Name])
		}
	}
	return out
}
