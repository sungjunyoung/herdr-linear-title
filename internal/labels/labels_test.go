package labels

import (
	"os"
	"path/filepath"
	"testing"
)

// herdr reports the same checkout through different paths (e.g. /tmp and
// /private/tmp on macOS); both must resolve to one entry.
func TestStoreKeysBySymlinkResolvedPath(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "real", "homeco-1")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "real"), link); err != nil {
		t.Fatal(err)
	}

	s := Store{Dir: t.TempDir()}
	if err := s.Set(filepath.Join(link, "homeco-1"), "HOMECO-1 Title"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(checkout)
	if err != nil || !ok || got != "HOMECO-1 Title" {
		t.Fatalf("Get(checkout) = %q, %v, %v", got, ok, err)
	}
	if _, ok, _ := s.Get(filepath.Join(root, "real", "other")); ok {
		t.Error("unrelated checkout has a label")
	}
}
