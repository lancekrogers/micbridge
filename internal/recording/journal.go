package recording

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
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
	DataBytes uint64    `json:"data_bytes,omitempty"`
	CRC32     uint32    `json:"crc32,omitempty"`
	At        time.Time `json:"at"`
	Note      string    `json:"note,omitempty"`
}

// OpenJournal creates or appends a journal under journalsDir.
func OpenJournal(journalsDir, sessionID string) (*Journal, error) {
	if err := validateArtifactID(sessionID); err != nil {
		return nil, err
	}
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
	RecoveryNone           RecoveryOutcome = "none"
	RecoveryFinalized      RecoveryOutcome = "finalized"
	RecoveryQuarantine     RecoveryOutcome = "quarantined"
	RecoveryPartialOK      RecoveryOutcome = "recoverable_partial"
	RecoveryJournalInvalid RecoveryOutcome = "journal_invalid"
)

// ReadJournal validates every checkpoint and returns it in append order.
func ReadJournal(path, sessionID string) ([]JournalRecord, error) {
	if err := validateArtifactID(sessionID); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var (
		records        []JournalRecord
		lastData       uint64
		currentPartial string
	)
	validSteps := map[string]bool{
		"created": true, "recording": true, "wrote": true, "synced": true,
		"finalized": true, "aborted": true,
	}
	for scanner.Scan() {
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		var record JournalRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("recording: corrupt journal: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("recording: journal checkpoint must contain one JSON object")
		}
		if record.SessionID != sessionID || !validSteps[record.Step] {
			return nil, fmt.Errorf("recording: invalid journal checkpoint")
		}
		if len(records) == 0 && record.Step != "created" {
			return nil, fmt.Errorf("recording: journal must begin with created")
		}
		if len(records) > 0 {
			previous := records[len(records)-1].Step
			if previous == "finalized" || previous == "aborted" || record.Step == "created" {
				return nil, fmt.Errorf("recording: invalid journal step order")
			}
		}
		switch record.Step {
		case "created":
			if record.Partial == "" {
				return nil, fmt.Errorf("recording: %s checkpoint lacks partial path", record.Step)
			}
			currentPartial = record.Partial
		case "wrote", "synced", "aborted":
			if record.Partial == "" {
				record.Partial = currentPartial
			}
			if record.Partial == "" {
				return nil, fmt.Errorf("recording: %s checkpoint lacks partial path", record.Step)
			}
			currentPartial = record.Partial
		case "finalized":
			if record.Final == "" {
				return nil, fmt.Errorf("recording: finalized checkpoint lacks final path")
			}
		}
		if record.DataBytes < lastData {
			return nil, fmt.Errorf("recording: journal byte count moved backwards")
		}
		lastData = record.DataBytes
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("recording: empty journal")
	}
	return records, nil
}

// RecoverSession inspects partial/final artifacts and journal.
func RecoverSession(recordingsDir, journalsDir, sessionID string) (RecoveryOutcome, string, error) {
	if err := validateArtifactID(sessionID); err != nil {
		return RecoveryNone, "", err
	}
	final := filepath.Join(recordingsDir, sessionID+".wav")
	partial := filepath.Join(recordingsDir, sessionID+".wav.partial")
	journal := filepath.Join(journalsDir, sessionID+".journal")
	var records []JournalRecord
	if _, err := os.Stat(journal); err == nil {
		records, err = ReadJournal(journal, sessionID)
		if err != nil {
			return RecoveryJournalInvalid, journal, err
		}
		for _, record := range records {
			if record.Partial != "" {
				if err := validateJournalPath(recordingsDir, record.Partial, ".wav.partial"); err != nil {
					return RecoveryJournalInvalid, journal, err
				}
				partial = record.Partial
			}
			if record.Final != "" {
				if err := validateJournalPath(recordingsDir, record.Final, ".wav"); err != nil {
					return RecoveryJournalInvalid, journal, err
				}
				final = record.Final
			}
		}
	}

	if _, err := os.Stat(final); err == nil {
		if err := ValidateWAV(final); err != nil {
			q := final + ".quarantine"
			if renameErr := quarantineNoReplace(final, q); renameErr != nil {
				return RecoveryQuarantine, final, fmt.Errorf("%w; quarantine: %v", err, renameErr)
			}
			return RecoveryQuarantine, q, err
		}
		if len(records) > 0 {
			last := records[len(records)-1]
			if last.Step == "finalized" {
				if err := validateFinalCheckpoint(final, last); err != nil {
					return RecoveryJournalInvalid, journal, err
				}
			}
		}
		return RecoveryFinalized, final, nil
	}
	if _, err := os.Stat(partial); err == nil {
		if err := validatePartialWAV(partial); err != nil {
			q := partial + ".quarantine"
			if renameErr := quarantineNoReplace(partial, q); renameErr != nil {
				return RecoveryQuarantine, partial, fmt.Errorf("%w; quarantine: %v", err, renameErr)
			}
			return RecoveryQuarantine, q, err
		}
		if len(records) > 0 {
			last := records[len(records)-1]
			info, statErr := os.Stat(partial)
			if statErr != nil {
				return RecoveryJournalInvalid, journal, statErr
			}
			actual := uint64(info.Size() - 44)
			if actual < last.DataBytes || (last.Step == "synced" && actual != last.DataBytes) {
				return RecoveryJournalInvalid, journal,
					fmt.Errorf("recording: journal data bytes=%d, partial has=%d", last.DataBytes, actual)
			}
		}
		return RecoveryPartialOK, partial, nil
	}
	if _, err := os.Stat(journal); err == nil {
		// journal without media
		return RecoveryNone, journal, nil
	}
	return RecoveryNone, "", nil
}

func validateJournalPath(recordingsDir, path, suffix string) error {
	if !filepath.IsAbs(path) || !strings.HasSuffix(path, suffix) {
		return fmt.Errorf("recording: journal artifact path is invalid")
	}
	root, err := filepath.Abs(recordingsDir)
	if err != nil {
		return err
	}
	candidate, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("recording: journal artifact escapes recordings root")
	}
	return nil
}

func validateFinalCheckpoint(path string, record JournalRecord) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(contents) < 44 {
		return fmt.Errorf("recording: final artifact is too short")
	}
	data := contents[44:]
	if uint64(len(data)) != record.DataBytes {
		return fmt.Errorf("recording: journal data bytes=%d, final has=%d", record.DataBytes, len(data))
	}
	if crc32.ChecksumIEEE(data) != record.CRC32 {
		return fmt.Errorf("recording: journal CRC32 does not match final artifact")
	}
	return nil
}

func quarantineNoReplace(source, target string) error {
	if err := os.Link(source, target); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	return syncDir(filepath.Dir(target))
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
