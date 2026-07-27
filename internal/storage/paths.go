package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// Layout names well-known subpaths under a Micbridge state root.
type Layout struct {
	Root string
}

// DefaultStateRoot returns a stable per-user state directory. Runtime state
// must not move just because micbridge was launched from a different working
// directory.
func DefaultStateRoot() (string, error) {
	if root := os.Getenv("XDG_STATE_HOME"); root != "" {
		return filepath.Join(root, "micbridge"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("storage: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "micbridge"), nil
}

// NewLayout validates and returns a layout rooted at root.
func NewLayout(root string) (*Layout, error) {
	if root == "" {
		return nil, fmt.Errorf("storage: state root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve state root: %w", err)
	}
	return &Layout{Root: filepath.Clean(abs)}, nil
}

// RecordingsDir is where create-only WAV artifacts live.
func (l *Layout) RecordingsDir() string {
	return filepath.Join(l.Root, "recordings")
}

// JournalsDir is where session journals live.
func (l *Layout) JournalsDir() string {
	return filepath.Join(l.Root, "journals")
}
