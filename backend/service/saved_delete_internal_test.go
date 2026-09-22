package service

import (
	"os"
	"path/filepath"
	"testing"
)

// removeAllUnderRoot is the one guard standing between a chapter id a source
// chose and the rest of the filesystem. It must refuse anything that resolves
// outside root, exactly as reclaim.go's removeUnderRoot does for the download
// cache.
func TestRemoveAllUnderRootRefusesToEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "not-quires-saved")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })

	keepMe := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keepMe, []byte("not Quire's"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removeAllUnderRoot(root, keepMe); err == nil {
		t.Fatal("removeAllUnderRoot did not refuse a path outside root")
	}
	if _, err := os.Stat(keepMe); err != nil {
		t.Fatal("a file outside root was removed anyway")
	}

	// And the ordinary case: a path that does resolve inside root is removed.
	inside := filepath.Join(root, "chapter-dir")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeAllUnderRoot(root, inside); err != nil {
		t.Fatalf("removeAllUnderRoot refused a path inside root: %v", err)
	}
	if _, err := os.Stat(inside); err == nil {
		t.Error("the directory inside root was not removed")
	}
}
