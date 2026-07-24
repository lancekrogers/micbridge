package storage

import (
	"fmt"
	"path/filepath"
)

// Layout names well-known subpaths under a Micbridge state root.
type Layout struct {
	Root string
}

// NewLayout validates and returns a layout rooted at root.
func NewLayout(root string) (*Layout, error) {
	if root == "" {
		return nil, fmt.Errorf("storage: state root is required")
	}
	return &Layout{Root: filepath.Clean(root)}, nil
}

// RecordingsDir is where create-only WAV artifacts live.
func (l *Layout) RecordingsDir() string {
	return filepath.Join(l.Root, "recordings")
}

// JournalsDir is where session journals live.
func (l *Layout) JournalsDir() string {
	return filepath.Join(l.Root, "journals")
}
