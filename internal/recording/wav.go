package recording

import (
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

// WAVFormat is PCM format for create-only WAV files.
type WAVFormat struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
}

// Writer writes a create-only WAV via a .partial path then atomic rename.
type Writer struct {
	finalPath    string
	partialPath  string
	f            *os.File
	format       WAVFormat
	dataBytes    uint32
	crc          hash.Hash32
	headerFixed  bool
}

// CreateWAV creates a new recording file; fails if final or partial exists.
func CreateWAV(dir, base string, format WAVFormat) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	final := filepath.Join(dir, base+".wav")
	partial := filepath.Join(dir, base+".wav.partial")
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("recording: refuse overwrite %s", final)
	}
	if _, err := os.Stat(partial); err == nil {
		return nil, fmt.Errorf("recording: partial already exists %s", partial)
	}
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	w := &Writer{finalPath: final, partialPath: partial, f: f, format: format, crc: crc32.NewIEEE()}
	if err := w.writeHeader(0); err != nil {
		_ = f.Close()
		_ = os.Remove(partial)
		return nil, err
	}
	w.headerFixed = true
	return w, nil
}

func (w *Writer) writeHeader(dataBytes uint32) error {
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	byteRate := w.format.SampleRate * uint32(w.format.Channels) * uint32(w.format.BitsPerSample/8)
	blockAlign := w.format.Channels * (w.format.BitsPerSample / 8)
	// RIFF chunk size = 36 + dataBytes
	riffSize := 36 + dataBytes
	buf := make([]byte, 44)
	copy(buf[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:], riffSize)
	copy(buf[8:], []byte("WAVE"))
	copy(buf[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:], 16) // PCM fmt chunk
	binary.LittleEndian.PutUint16(buf[20:], 1)  // PCM
	binary.LittleEndian.PutUint16(buf[22:], w.format.Channels)
	binary.LittleEndian.PutUint32(buf[24:], w.format.SampleRate)
	binary.LittleEndian.PutUint32(buf[28:], byteRate)
	binary.LittleEndian.PutUint16(buf[32:], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:], w.format.BitsPerSample)
	copy(buf[36:], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:], dataBytes)
	_, err := w.f.Write(buf)
	return err
}

// WritePCM appends interleaved PCM samples.
func (w *Writer) WritePCM(p []byte) error {
	n, err := w.f.Write(p)
	if n > 0 {
		w.dataBytes += uint32(n)
		_, _ = w.crc.Write(p[:n])
	}
	return err
}

// Sync flushes file data to stable storage.
func (w *Writer) Sync() error {
	return w.f.Sync()
}

// Finalize patches sizes, syncs, and renames partial → final.
func (w *Writer) Finalize() (meta FinalMeta, err error) {
	if err = w.writeHeader(w.dataBytes); err != nil {
		return meta, err
	}
	if err = w.f.Sync(); err != nil {
		return meta, err
	}
	if err = w.f.Close(); err != nil {
		return meta, err
	}
	w.f = nil
	if err = os.Rename(w.partialPath, w.finalPath); err != nil {
		return meta, err
	}
	meta = FinalMeta{
		Path:      w.finalPath,
		DataBytes: w.dataBytes,
		CRC32:     w.crc.Sum32(),
		Format:    w.format,
	}
	return meta, nil
}

// Abort closes and leaves the partial for recovery (does not delete).
func (w *Writer) Abort() error {
	if w.f != nil {
		_ = w.f.Sync()
		err := w.f.Close()
		w.f = nil
		return err
	}
	return nil
}

// PartialPath returns the in-progress path.
func (w *Writer) PartialPath() string { return w.partialPath }

// FinalMeta describes a published WAV.
type FinalMeta struct {
	Path      string
	DataBytes uint32
	CRC32     uint32
	Format    WAVFormat
}

// ValidateWAV checks RIFF/WAVE header basics.
func ValidateWAV(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := make([]byte, 44)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return fmt.Errorf("recording: short wav header: %w", err)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" || string(hdr[36:40]) != "data" {
		return fmt.Errorf("recording: invalid wav magic")
	}
	dataBytes := binary.LittleEndian.Uint32(hdr[40:])
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < int64(44+dataBytes) {
		return fmt.Errorf("recording: truncated wav data (header claims %d)", dataBytes)
	}
	return nil
}
