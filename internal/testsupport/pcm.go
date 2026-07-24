package testsupport

import (
	"encoding/binary"
	"math"
)

// PCMFormat describes interleaved signed PCM used in synthetic fixtures.
type PCMFormat struct {
	SampleRate    int
	Channels      int
	BitsPerSample int // 16 only for generators below
}

// DefaultCaptureFormat matches measured Hollyland evidence: 48 kHz stereo.
func DefaultCaptureFormat() PCMFormat {
	return PCMFormat{SampleRate: 48000, Channels: 2, BitsPerSample: 16}
}

// Silence returns frames of interleaved zero samples.
func Silence(format PCMFormat, frames int) []byte {
	if frames < 0 {
		frames = 0
	}
	n := frames * format.Channels * (format.BitsPerSample / 8)
	return make([]byte, n)
}

// Tone returns frames of a sine wave at freqHz (all channels identical).
// Amplitude is in 0..1 relative to full scale int16.
func Tone(format PCMFormat, frames int, freqHz float64, amplitude float64) []byte {
	if frames < 0 {
		frames = 0
	}
	if amplitude < 0 {
		amplitude = 0
	}
	if amplitude > 1 {
		amplitude = 1
	}
	out := make([]byte, frames*format.Channels*2)
	for i := 0; i < frames; i++ {
		t := float64(i) / float64(format.SampleRate)
		sample := int16(amplitude * 32767 * math.Sin(2*math.Pi*freqHz*t))
		for ch := 0; ch < format.Channels; ch++ {
			off := (i*format.Channels + ch) * 2
			binary.LittleEndian.PutUint16(out[off:], uint16(sample))
		}
	}
	return out
}

// Discontinuity marks a mid-buffer discontinuity for recovery tests by zeroing
// a gap and returning metadata about the gap frame range [start, end).
func Discontinuity(pcm []byte, format PCMFormat, startFrame, gapFrames int) (out []byte, gapStart, gapEnd int) {
	out = make([]byte, len(pcm))
	copy(out, pcm)
	frameBytes := format.Channels * (format.BitsPerSample / 8)
	if frameBytes == 0 {
		return out, 0, 0
	}
	totalFrames := len(out) / frameBytes
	if startFrame < 0 {
		startFrame = 0
	}
	if startFrame > totalFrames {
		startFrame = totalFrames
	}
	end := startFrame + gapFrames
	if end > totalFrames {
		end = totalFrames
	}
	for i := startFrame; i < end; i++ {
		off := i * frameBytes
		for j := 0; j < frameBytes; j++ {
			out[off+j] = 0
		}
	}
	return out, startFrame, end
}

// FrameCount returns the number of complete frames in pcm for format.
func FrameCount(pcm []byte, format PCMFormat) int {
	fb := format.Channels * (format.BitsPerSample / 8)
	if fb == 0 {
		return 0
	}
	return len(pcm) / fb
}
