package testsupport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lancekrogers/micbridge/internal/storage"
)

// TempRoot creates a temporary Micbridge state root under the test's TempDir
// and returns a storage.Layout. All artifacts must stay under this root.
func TempRoot(t *testing.T) *storage.Layout {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}
	layout, err := storage.NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{layout.RecordingsDir(), layout.JournalsDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return layout
}

// WriteArtifact writes data under the layout root and returns the absolute path.
// Paths never escape the state root.
func WriteArtifact(t *testing.T, layout *storage.Layout, rel string, data []byte) string {
	t.Helper()
	path := filepath.Join(layout.Root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure the path is still under root after clean.
	clean := filepath.Clean(path)
	root := filepath.Clean(layout.Root)
	if clean != root && !isSubpath(root, clean) {
		t.Fatalf("artifact escaped state root: %s", clean)
	}
	return clean
}

func isSubpath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && (rel == "." || (len(rel) > 0 && rel[0] != '.'))
}
