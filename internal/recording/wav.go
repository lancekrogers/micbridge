package recording

import (
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// WAVFormat is PCM format for create-only WAV files.
type WAVFormat struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
}

// Writer writes a create-only WAV via a .partial path then atomic rename.
type Writer struct {
	finalPath   string
	partialPath string
	f           *os.File
	format      WAVFormat
	dataBytes   uint64
	crc         hash.Hash32
	headerFixed bool
}

// CreateWAV creates a new recording file; fails if final or partial exists.
func CreateWAV(dir, base string, format WAVFormat) (*Writer, error) {
	if err := validateArtifactID(base); err != nil {
		return nil, err
	}
	if err := validateWAVFormat(format); err != nil {
		return nil, err
	}
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

func validateArtifactID(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id ||
		strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("recording: invalid artifact id")
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("recording: invalid artifact id")
		}
	}
	return nil
}

func validateWAVFormat(format WAVFormat) error {
	if format.SampleRate == 0 || format.Channels == 0 || format.Channels > 64 {
		return fmt.Errorf("recording: invalid PCM rate/channels")
	}
	switch format.BitsPerSample {
	case 8, 16, 24, 32:
	default:
		return fmt.Errorf("recording: unsupported PCM bits per sample %d", format.BitsPerSample)
	}
	bytesPerSample := uint64(format.BitsPerSample / 8)
	byteRate := uint64(format.SampleRate) * uint64(format.Channels) * bytesPerSample
	if byteRate > math.MaxUint32 {
		return fmt.Errorf("recording: PCM byte rate exceeds WAV limit")
	}
	return nil
}

func (w *Writer) writeHeader(dataBytes uint64) error {
	if dataBytes > math.MaxUint32-36 {
		return fmt.Errorf("recording: WAV exceeds RIFF 4 GiB limit")
	}
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	byteRate := w.format.SampleRate * uint32(w.format.Channels) * uint32(w.format.BitsPerSample/8)
	blockAlign := w.format.Channels * (w.format.BitsPerSample / 8)
	// RIFF chunk size = 36 + dataBytes
	riffSize := uint32(36 + dataBytes)
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
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataBytes))
	n, err := w.f.Write(buf)
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	return err
}

// WritePCM appends interleaved PCM samples.
func (w *Writer) WritePCM(p []byte) error {
	if w.f == nil {
		return fmt.Errorf("recording: writer closed")
	}
	blockAlign := int(w.format.Channels * (w.format.BitsPerSample / 8))
	if len(p) == 0 || len(p)%blockAlign != 0 {
		return fmt.Errorf("recording: PCM payload is not block aligned")
	}
	if uint64(len(p)) > math.MaxUint32-36-w.dataBytes {
		return fmt.Errorf("recording: WAV exceeds RIFF 4 GiB limit")
	}
	n, err := w.f.Write(p)
	if n > 0 {
		w.dataBytes += uint64(n)
		_, _ = w.crc.Write(p[:n])
	}
	if err == nil && n != len(p) {
		return io.ErrShortWrite
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
	// Link is an atomic no-replace publication on the same filesystem. Rename
	// would overwrite a final created after CreateWAV's initial check.
	if err = os.Link(w.partialPath, w.finalPath); err != nil {
		return meta, err
	}
	if err = syncDir(filepath.Dir(w.finalPath)); err != nil {
		return meta, err
	}
	if err = os.Remove(w.partialPath); err != nil {
		return meta, err
	}
	if err = syncDir(filepath.Dir(w.finalPath)); err != nil {
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

// DataBytes returns the accepted aligned PCM byte count.
func (w *Writer) DataBytes() uint64 { return w.dataBytes }

// FinalMeta describes a published WAV.
type FinalMeta struct {
	Path      string
	DataBytes uint64
	CRC32     uint32
	Format    WAVFormat
}

// ValidateWAV checks RIFF/WAVE header basics.
func ValidateWAV(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	hdr := make([]byte, 44)
	if _, readErr := io.ReadFull(f, hdr); readErr != nil {
		return fmt.Errorf("recording: short wav header: %w", readErr)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" || string(hdr[36:40]) != "data" {
		return fmt.Errorf("recording: invalid wav magic")
	}
	if binary.LittleEndian.Uint32(hdr[16:20]) != 16 ||
		binary.LittleEndian.Uint16(hdr[20:22]) != 1 {
		return fmt.Errorf("recording: unsupported wav format")
	}
	format := WAVFormat{
		Channels:      binary.LittleEndian.Uint16(hdr[22:24]),
		SampleRate:    binary.LittleEndian.Uint32(hdr[24:28]),
		BitsPerSample: binary.LittleEndian.Uint16(hdr[34:36]),
	}
	if formatErr := validateWAVFormat(format); formatErr != nil {
		return formatErr
	}
	blockAlign := format.Channels * (format.BitsPerSample / 8)
	byteRate := format.SampleRate * uint32(blockAlign)
	if binary.LittleEndian.Uint16(hdr[32:34]) != blockAlign ||
		binary.LittleEndian.Uint32(hdr[28:32]) != byteRate {
		return fmt.Errorf("recording: inconsistent wav format fields")
	}
	dataBytes := binary.LittleEndian.Uint32(hdr[40:])
	if dataBytes%uint32(blockAlign) != 0 {
		return fmt.Errorf("recording: wav data is not block aligned")
	}
	if binary.LittleEndian.Uint32(hdr[4:8]) != 36+dataBytes {
		return fmt.Errorf("recording: inconsistent RIFF size")
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() != int64(44)+int64(dataBytes) {
		return fmt.Errorf("recording: wav size mismatch (header claims %d, file is %d)", dataBytes, st.Size())
	}
	return nil
}

func validatePartialWAV(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, 44)
	if _, readErr := io.ReadFull(file, header); readErr != nil {
		return fmt.Errorf("recording: short partial header: %w", readErr)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" ||
		string(header[12:16]) != "fmt " || string(header[36:40]) != "data" {
		return fmt.Errorf("recording: invalid partial WAV magic")
	}
	format := WAVFormat{
		Channels:      binary.LittleEndian.Uint16(header[22:24]),
		SampleRate:    binary.LittleEndian.Uint32(header[24:28]),
		BitsPerSample: binary.LittleEndian.Uint16(header[34:36]),
	}
	if formatErr := validateWAVFormat(format); formatErr != nil {
		return formatErr
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	blockAlign := int64(format.Channels * (format.BitsPerSample / 8))
	if (info.Size()-44)%blockAlign != 0 {
		return fmt.Errorf("recording: partial PCM is not block aligned")
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
