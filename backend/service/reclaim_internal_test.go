package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/library"
)

// cacheDir writes a chapter's page files where a download would leave them.
func cacheDir(t *testing.T, seriesDir, chapterID string) string {
	t.Helper()
	dir := download.ChapterDir(seriesDir, chapterID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0000.jpg"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A delete that lands mid-download must leave that download's pages where they
// are. Taking them away leaves a writer filling a directory that is no longer
// anywhere, and a volume assembled out of whatever survived.
//
// Only a *running* download is protected. A queued one has written nothing, and
// finding its pages gone simply costs it the fetch it would have skipped.
func TestReclaimLeavesPagesADownloadIsWriting(t *testing.T) {
	root := t.TempDir()
	s := New(Options{DownloadDir: root})

	seriesDir := filepath.Join(root, "example-reader", "the-lantern-keeper")
	busy := "/manga/the-lantern-keeper/chapter-1/"
	idle := "/manga/the-lantern-keeper/chapter-2/"
	busyDir, idleDir := cacheDir(t, seriesDir, busy), cacheDir(t, seriesDir, idle)

	rec := library.Record{
		Key:      library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v1"},
		Chapters: []string{busy, idle},
	}

	release := s.claimChapters([]download.Chapter{{ID: busy}})

	freed := s.reclaimPages(rec, nil)
	if _, err := os.Stat(busyDir); err != nil {
		t.Errorf("pages a download was writing were removed: %v", err)
	}
	if _, err := os.Stat(idleDir); !os.IsNotExist(err) {
		t.Errorf("pages nothing was writing survived: %v", err)
	}
	if freed == 0 {
		t.Error("nothing was reported reclaimed, though a chapter went")
	}

	// Once the download is done, the same delete reclaims the rest.
	release()
	if s.chapterIsDownloading(busy) {
		t.Fatal("the claim outlived the download")
	}
	if freed := s.reclaimPages(rec, nil); freed == 0 {
		t.Error("the released chapter was still not reclaimed")
	}
	if _, err := os.Stat(busyDir); !os.IsNotExist(err) {
		t.Errorf("the released chapter's pages survived: %v", err)
	}
}

// The claim is counted, not a flag: the same chapter can be claimed by the
// volume that holds it and by itself, and the first release must not unlock
// pages the second download is still writing.
func TestChapterClaimsAreCounted(t *testing.T) {
	s := New(Options{})
	id := "/manga/the-lantern-keeper/chapter-1/"

	first := s.claimChapters([]download.Chapter{{ID: id}})
	second := s.claimChapters([]download.Chapter{{ID: id}})

	first()
	if !s.chapterIsDownloading(id) {
		t.Fatal("one release freed a chapter two downloads had claimed")
	}
	second()
	if s.chapterIsDownloading(id) {
		t.Fatal("the chapter stayed claimed after both releases")
	}
}
