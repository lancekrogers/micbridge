package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Journal records durable session progress without embedding audio.
type Journal struct {
	path string
}

// JournalRecord is one checkpoint.
type JournalRecord struct {
	SessionID string    `json:"session_id"`
	Step      string    `json:"step"` // created|wrote|synced|finalized|aborted
	Partial   string    `json:"partial,omitempty"`
	Final     string    `json:"final,omitempty"`
	DataBytes uint32    `json:"data_bytes,omitempty"`
	CRC32     uint32    `json:"crc32,omitempty"`
	At        time.Time `json:"at"`
	Note      string    `json:"note,omitempty"`
}

// OpenJournal creates or appends a journal under journalsDir.
func OpenJournal(journalsDir, sessionID string) (*Journal, error) {
	if err := os.MkdirAll(journalsDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(journalsDir, sessionID+".journal")
	return &Journal{path: path}, nil
}

// Append writes one JSON line and syncs the journal file.
func (j *Journal) Append(rec JournalRecord) error {
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Path returns the journal path.
func (j *Journal) Path() string { return j.path }

// RecoveryOutcome is deterministic startup recovery result.
type RecoveryOutcome string

const (
	RecoveryNone       RecoveryOutcome = "none"
	RecoveryFinalized  RecoveryOutcome = "finalized"
	RecoveryQuarantine RecoveryOutcome = "quarantined"
	RecoveryPartialOK  RecoveryOutcome = "recoverable_partial"
)

// RecoverSession inspects partial/final artifacts and journal.
func RecoverSession(recordingsDir, journalsDir, sessionID string) (RecoveryOutcome, string, error) {
	final := filepath.Join(recordingsDir, sessionID+".wav")
	partial := filepath.Join(recordingsDir, sessionID+".wav.partial")
	journal := filepath.Join(journalsDir, sessionID+".journal")

	if _, err := os.Stat(final); err == nil {
		if err := ValidateWAV(final); err != nil {
			q := final + ".quarantine"
			_ = os.Rename(final, q)
			return RecoveryQuarantine, q, err
		}
		return RecoveryFinalized, final, nil
	}
	if _, err := os.Stat(partial); err == nil {
		// Try to finalize header sizes from file length.
		st, err := os.Stat(partial)
		if err != nil {
			return RecoveryQuarantine, partial, err
		}
		if st.Size() < 44 {
			q := partial + ".quarantine"
			_ = os.Rename(partial, q)
			return RecoveryQuarantine, q, fmt.Errorf("partial too short")
		}
		// Leave as recoverable partial; do not claim success.
		_ = journal // journal present for operators
		return RecoveryPartialOK, partial, nil
	}
	if _, err := os.Stat(journal); err == nil {
		// journal without media
		return RecoveryNone, journal, nil
	}
	return RecoveryNone, "", nil
}

// ScanRecoverable lists session IDs with partials under recordingsDir.
func ScanRecoverable(recordingsDir string) ([]string, error) {
	ents, err := os.ReadDir(recordingsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range ents {
		name := e.Name()
		if strings.HasSuffix(name, ".wav.partial") {
			ids = append(ids, strings.TrimSuffix(name, ".wav.partial"))
		}
	}
	return ids, nil
}
