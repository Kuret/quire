package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/download"
)

// A clear must not take pages out from under a running download, and it must
// not pretend it cleared them either: "cleared 592 MB" with 40 MB quietly left
// behind is the small lie that becomes a bug report.
func TestClearingKeepsWhatADownloadIsWritingAndSaysSo(t *testing.T) {
	root := t.TempDir()
	s := New(Options{DownloadDir: root})

	seriesDir := filepath.Join(root, "example-reader", "the-lantern-keeper")
	busy := "/manga/the-lantern-keeper/chapter-1/"
	idle := "/manga/the-lantern-keeper/chapter-2/"
	busyDir, idleDir := cacheDir(t, seriesDir, busy), cacheDir(t, seriesDir, idle)

	release := s.claimChapters(seriesDir, []download.Chapter{{ID: busy}})
	defer release()

	swept := s.sweepCache()

	if _, err := os.Stat(busyDir); err != nil {
		t.Errorf("pages a download was writing were cleared: %v", err)
	}
	if _, err := os.Stat(idleDir); !os.IsNotExist(err) {
		t.Errorf("pages nothing was writing survived the clear: %v", err)
	}
	if swept.KeptDirs != 1 {
		t.Errorf("reported %d chapters kept, want 1", swept.KeptDirs)
	}
	if swept.Kept != treeSize(busyDir) {
		t.Errorf("reported %d bytes kept, want %d", swept.Kept, treeSize(busyDir))
	}
	if swept.Freed == 0 {
		t.Error("nothing was reported cleared, though a chapter went")
	}

	// And the sentence accounts for it rather than reporting the smaller number
	// on its own.
	said := clearCacheOutcome(swept)
	if !strings.Contains(said, "was left") {
		t.Errorf("outcome %q does not mention what was kept", said)
	}
}

// The figure reported is the figure removed: measured against the cache before
// and after, not against what the sweep hoped to do.
func TestTheClearedFigureMatchesWhatWentAway(t *testing.T) {
	root := t.TempDir()
	s := New(Options{DownloadDir: root})

	seriesDir := filepath.Join(root, "example-reader", "the-lantern-keeper")
	for _, id := range []string{
		"/manga/the-lantern-keeper/chapter-1/",
		"/manga/the-lantern-keeper/chapter-2/",
		"/manga/the-lantern-keeper/chapter-3/",
	} {
		cacheDir(t, seriesDir, id)
	}
	before := s.cacheSize()
	if before == 0 {
		t.Fatal("the fixture wrote nothing")
	}

	swept := s.sweepCache()

	if after := s.cacheSize(); after != 0 {
		t.Errorf("%d bytes left in a cache with nothing claimed", after)
	}
	if swept.Freed != before {
		t.Errorf("reported %d bytes cleared, but the cache was holding %d", swept.Freed, before)
	}
	if swept.KeptDirs != 0 || swept.Kept != 0 {
		t.Errorf("reported %d bytes kept across %d chapters, want none", swept.Kept, swept.KeptDirs)
	}
}

// A claim held over a whole series still leaves the tree standing, because the
// directories it is in are not empty. Pruning must not remove a directory a
// kept chapter lives in.
func TestClearingLeavesTheTreeAKeptChapterNeeds(t *testing.T) {
	root := t.TempDir()
	s := New(Options{DownloadDir: root})

	seriesDir := filepath.Join(root, "example-reader", "the-lantern-keeper")
	busy := "/manga/the-lantern-keeper/chapter-1/"
	busyDir := cacheDir(t, seriesDir, busy)

	release := s.claimChapters(seriesDir, []download.Chapter{{ID: busy}})
	defer release()

	s.sweepCache()

	for _, dir := range []string{busyDir, seriesDir, filepath.Dir(seriesDir)} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s was pruned though a download is writing inside it: %v", dir, err)
		}
	}
}
