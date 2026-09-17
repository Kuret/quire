package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
)

// serviceWithDownloadRoot is a download service whose page cache is somewhere
// the test can see, which is what these tests are about.
func serviceWithDownloadRoot(t *testing.T) (*service.Service, *state.Store, *library.Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "downloads")
	svc, store, libStore, _, _ := newDownloadServiceWith(t, downloadRoutes(t),
		func(o *service.Options) { o.DownloadDir = root })
	return svc, store, libStore, root
}

// pageCache builds the on-disk cache for a set of chapters, the way a download
// leaves it, and returns the series directory.
//
// The directories come from download.ChapterDir rather than from a name spelled
// out here: a fixture that invents the layout would agree with a reclaim that
// invents it the same wrong way, and the test would pass over a bug.
func pageCache(t *testing.T, root, sourceID, seriesID string, chapters ...string) string {
	t.Helper()
	seriesDir := filepath.Join(root, sourceID, seriesID)
	for _, id := range chapters {
		dir := download.ChapterDir(seriesDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, page := range []string{"0000.jpg", "0001.jpg"} {
			if err := os.WriteFile(filepath.Join(dir, page), make([]byte, 1024), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return seriesDir
}

// exists is "is this path still on disk".
func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// deleteDocument runs the delete the frontend would, for a document already in
// the store.
func deleteDocument(t *testing.T, svc *service.Service, uuid string) *recorder {
	t.Helper()
	rec := &recorder{}
	handle(t, svc, rec, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuid+`","confirmed":true,"trashed":true,"emptied":true}`)
	rec.wait(t, appload.MessageDownloadDeleted)
	return rec
}

// The reason the space did not come back: the pages behind a deleted document
// stayed, and the next download of the same chapter found them and skipped the
// network entirely.
func TestDeletingADownloadRemovesItsCachedPages(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)

	stored := recordFor(t, libStore, uuid)
	if len(stored.Chapters) == 0 {
		t.Fatal("the record lists no chapters, so this test proves nothing")
	}
	seriesDir := filepath.Dir(stored.PDF)
	dirs := make([]string, 0, len(stored.Chapters))
	for _, id := range stored.Chapters {
		dir := download.ChapterDir(seriesDir, id)
		if !exists(t, dir) {
			t.Fatalf("the download left no pages at %s", dir)
		}
		dirs = append(dirs, dir)
	}

	deleteDocument(t, svc, uuid)

	for _, dir := range dirs {
		if exists(t, dir) {
			t.Errorf("%s survived the delete; the space does not come back", dir)
		}
	}
	if exists(t, stored.PDF) {
		t.Errorf("%s survived the delete", stored.PDF)
	}
}

// A chapter two records cover is not this record's to remove. The case is built
// from scratch here rather than carried over from another test: a shared
// chapter that survives because an *earlier* assertion left it behind would
// pass with the check missing.
func TestPagesAnotherRecordStillNeedsSurvive(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	shared, own := "/manga/the-lantern-keeper/chapter-1/", "/manga/the-lantern-keeper/chapter-2/"
	seriesDir := pageCache(t, root, "example-reader", "the-lantern-keeper", shared, own)

	going := library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v1"},
		DocumentUUID: "doc-going",
		Chapters:     []string{shared, own},
	}
	staying := library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v2"},
		DocumentUUID: "doc-staying",
		Chapters:     []string{shared},
	}
	for _, r := range []library.Record{going, staying} {
		if err := libStore.Put(r); err != nil {
			t.Fatal(err)
		}
	}

	deleteDocument(t, svc, "doc-going")

	if !exists(t, download.ChapterDir(seriesDir, shared)) {
		t.Error("a chapter another record still lists was removed with this one")
	}
	if exists(t, download.ChapterDir(seriesDir, own)) {
		t.Error("a chapter no record lists any more was left behind")
	}
}

// The mirror of the case above, with nothing else in the store: the same
// chapter, and this time it goes. Its own fixture, so neither case can pass on
// the state the other left.
func TestPagesNoRecordNeedsAreRemoved(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	only := "/manga/the-lantern-keeper/chapter-1/"
	seriesDir := pageCache(t, root, "example-reader", "the-lantern-keeper", only)

	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v1"},
		DocumentUUID: "doc-only",
		Chapters:     []string{only},
	}); err != nil {
		t.Fatal(err)
	}

	deleteDocument(t, svc, "doc-only")

	if exists(t, download.ChapterDir(seriesDir, only)) {
		t.Error("the only record naming this chapter went, and its pages stayed")
	}
	// And the tree it was alone in goes with it.
	if exists(t, seriesDir) {
		t.Error("the series directory was left behind empty")
	}
	if exists(t, filepath.Dir(seriesDir)) {
		t.Error("the source directory was left behind empty")
	}
}

// A chapter id is the source's to choose, so the path built from it is the one
// place a source could reach outside the downloads directory. It must not.
func TestReclaimRefusesToEscapeTheDownloadsDirectory(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	outside := filepath.Join(filepath.Dir(root), "not-quires")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	keepMe := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keepMe, []byte("not Quire's"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "../../..", Volume: "v1"},
		DocumentUUID: "doc-escape",
		Chapters:     []string{"../../../../not-quires", "/etc/passwd"},
		PDF:          filepath.Join(outside, "keep.txt"),
	}); err != nil {
		t.Fatal(err)
	}

	deleteDocument(t, svc, "doc-escape")

	if !exists(t, keepMe) {
		t.Fatal("a delete removed a file outside the downloads directory")
	}
	if !exists(t, outside) {
		t.Fatal("a delete removed a directory outside the downloads directory")
	}
}

// The reply is unchanged by any of this: a delete that reclaims pages is still
// a delete, and the row still has to stop saying Read.
func TestReclaimingDoesNotChangeWhatTheUserIsTold(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)

	fresh := deleteDocument(t, svc, uuid)

	fresh.mu.Lock()
	defer fresh.mu.Unlock()
	for _, f := range fresh.sent {
		if f.Type == appload.MessageError {
			var e struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(f.Payload, &e)
			t.Errorf("the delete reported %q", e.Code)
		}
	}
}

// A re-stitched chapter keeps its re-cut pages in a subdirectory beside the
// sources (PLAN §12.3). Deleting the download has to take both, and it does so
// for free — reclaim removes the chapter directory whole — but "for free" is
// exactly the kind of claim that stops being true silently, so it is asserted
// against the real reclaim path rather than reasoned about.
func TestDeletingAlsoRemovesTheRestitchedPages(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	only := "/manga/the-lantern-keeper/chapter-1/"
	seriesDir := pageCache(t, root, "example-reader", "the-lantern-keeper", only)

	// The re-cut pages, where the download package puts them.
	recut := filepath.Join(download.ChapterDir(seriesDir, only), download.RestitchDirName)
	if err := os.MkdirAll(recut, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0000.jpg", "0001.jpg", "quire-restitch.json"} {
		if err := os.WriteFile(filepath.Join(recut, name), make([]byte, 2048), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v1"},
		DocumentUUID: "doc-restitched",
		Chapters:     []string{only},
	}); err != nil {
		t.Fatal(err)
	}

	deleteDocument(t, svc, "doc-restitched")

	if exists(t, recut) {
		t.Error("the re-cut pages survived the delete")
	}
	if exists(t, download.ChapterDir(seriesDir, only)) {
		t.Error("the chapter directory survived the delete")
	}
	if exists(t, seriesDir) {
		t.Error("the series directory was left behind")
	}
}

// A document the user deleted on the tablet is noticed when they tap Read, and
// its pages must go with its record. Before this, the record was dropped and
// the pages were orphaned for good — no later delete could name them, because
// the record naming them was gone.
func TestOpeningAMissingDocumentAlsoReclaimsItsPages(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	only := "/manga/the-lantern-keeper/chapter-1/"
	seriesDir := pageCache(t, root, "example-reader", "the-lantern-keeper", only)
	pages := download.ChapterDir(seriesDir, only)

	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v1"},
		DocumentUUID: "doc-deleted-on-tablet",
		Chapters:     []string{only},
	}); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageOpenInReader,
		`{"documentUuid":"doc-deleted-on-tablet","missing":true}`)
	rec.wait(t, appload.MessageError)

	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records left; the dead UUID must be forgotten", n)
	}
	if exists(t, pages) {
		t.Error("the cached pages survived; they are now orphaned for good")
	}
}

// And a chapter another record still holds is left alone, exactly as on the
// delete path: the reclaim is asked of the records that remain.
func TestOpeningAMissingDocumentKeepsPagesAnotherRecordNeeds(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	shared := "/manga/the-lantern-keeper/chapter-1/"
	seriesDir := pageCache(t, root, "example-reader", "the-lantern-keeper", shared)

	for _, r := range []library.Record{
		{
			Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v1"},
			DocumentUUID: "doc-gone",
			Chapters:     []string{shared},
		},
		{
			Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "v2"},
			DocumentUUID: "doc-staying",
			Chapters:     []string{shared},
		},
	} {
		if err := libStore.Put(r); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageOpenInReader, `{"documentUuid":"doc-gone","missing":true}`)
	rec.wait(t, appload.MessageError)

	if !exists(t, download.ChapterDir(seriesDir, shared)) {
		t.Error("pages another record still lists were reclaimed")
	}
}
