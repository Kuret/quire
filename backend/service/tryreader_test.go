package service_test

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/tryreader"
)

// newTryService is newDownloadService with a Try cache wired in, over its own
// temp directory so a test can check what is left in it — and with the
// library store and fake library handed back too, so a test can assert Try
// left *those* untouched.
func newTryService(t *testing.T) (*service.Service, *tryreader.Cache, string,
	*library.Store, *fakeLibrary, *recorder) {
	t.Helper()
	dir := t.TempDir()
	var cache *tryreader.Cache
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, downloadRoutes(t), func(o *service.Options) {
		cache = tryreader.New(dir, o.Fetcher)
		o.TryCache = cache
	})
	addSource(t, store)
	return svc, cache, dir, libStore, fake, rec
}

func tryReq(sourceID, seriesID, chapterID string) string {
	b, _ := json.Marshal(map[string]string{
		"sourceId": sourceID, "seriesId": seriesID, "chapterId": chapterID,
	})
	return string(b)
}

// The M1 acceptance path: tapping Try shows the first page without touching
// the reMarkable library at all.
func TestTryChapterShowsAPageAndTouchesNoLibrary(t *testing.T) {
	svc, _, _, libStore, fake, rec := newTryService(t)
	seriesID, chapterID := firstChapter(t, svc, rec)

	handle(t, svc, rec, appload.MessageTryChapter, tryReq("example-reader", seriesID, chapterID))

	var ready struct {
		SourceID  string `json:"sourceId"`
		SeriesID  string `json:"seriesId"`
		ChapterID string `json:"chapterId"`
		Index     int    `json:"index"`
		Path      string `json:"path"`
		PageCount int    `json:"pageCount"`
		Complete  bool   `json:"complete"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageTryReady), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Index != 0 {
		t.Errorf("first message is for page %d, want 0", ready.Index)
	}
	if ready.PageCount < 1 {
		t.Errorf("pageCount = %d, want at least 1", ready.PageCount)
	}
	if ready.Path == "" {
		t.Fatal("no path for the first page")
	}
	b, err := os.ReadFile(ready.Path)
	if err != nil {
		t.Fatalf("the reported page path does not exist: %v", err)
	}
	if _, err := jpeg.DecodeConfig(bytes.NewReader(b)); err != nil {
		t.Fatalf("the served page is not a JPEG: %v", err)
	}

	// The whole point of Try: nothing was uploaded, and no library.Record
	// exists for it. This is the mutation-testing anchor for "leave a
	// library entry behind" — a startTry that ever called s.library.Upload
	// or s.libStore.Put would be caught here.
	if len(libStore.List()) != 0 {
		t.Errorf("libStore has %d records after a Try; want 0", len(libStore.List()))
	}
	fake.mu.Lock()
	uploaded := len(fake.uploaded)
	fake.mu.Unlock()
	if uploaded != 0 {
		t.Errorf("%d documents were uploaded during a Try; want 0", uploaded)
	}
}

// TestEndTryRemovesTheCache is the mutation-testing anchor for "skip the
// cleanup of a finished session": if endTry stops calling Session.End, or
// calls it against the wrong session, this is what notices.
func TestEndTryRemovesTheCache(t *testing.T) {
	svc, _, dir, _, _, rec := newTryService(t)
	seriesID, chapterID := firstChapter(t, svc, rec)

	handle(t, svc, rec, appload.MessageTryChapter, tryReq("example-reader", seriesID, chapterID))
	var ready struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageTryReady), &ready); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ready.Path); err != nil {
		t.Fatalf("the page should exist before EndTry: %v", err)
	}

	handle(t, svc, rec, appload.MessageEndTry, tryReq("example-reader", seriesID, chapterID))

	deadlineOK := waitForRemoval(t, ready.Path)
	if !deadlineOK {
		t.Fatalf("the page still exists after EndTry: %s", ready.Path)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("the try cache directory still holds %d entries after the only session ended", len(entries))
	}
}

func waitForRemoval(t *testing.T, path string) bool {
	t.Helper()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return true
		}
	}
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// TestStartingAnotherTrySessionEndsThePrevious proves a session cannot
// accumulate: opening a second chapter's Try must remove the first one's
// cache, since only one session is ever open at a time.
func TestStartingAnotherTrySessionEndsThePrevious(t *testing.T) {
	svc, _, dir, _, _, rec := newTryService(t)
	seriesID, chapterID := firstChapter(t, svc, rec)

	handle(t, svc, rec, appload.MessageTryChapter, tryReq("example-reader", seriesID, chapterID))
	rec.wait(t, appload.MessageTryReady)

	countAfterFirst := countFiles(t, dir)
	if countAfterFirst == 0 {
		t.Fatal("the first session left nothing to compare against")
	}

	// Starting Try on the same chapter again still opens a fresh session and
	// must not leave the previous one's directory behind.
	handle(t, svc, rec, appload.MessageTryChapter, tryReq("example-reader", seriesID, chapterID))
	rec.wait(t, appload.MessageTryReady)

	deadlineOK := false
	for i := 0; i < 200 && !deadlineOK; i++ {
		if countDirs(t, dir) <= 1 {
			deadlineOK = true
		}
	}
	if !deadlineOK {
		t.Fatalf("more than one Try session directory survives: %d", countDirs(t, dir))
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func countDirs(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// TestTryPageRequestServesTheNextPage exercises the per-page request path a
// page turn drives, separate from the first page MessageTryChapter serves.
func TestTryPageRequestServesTheNextPage(t *testing.T) {
	svc, _, _, _, _, rec := newTryService(t)
	seriesID, chapterID := firstChapter(t, svc, rec)

	handle(t, svc, rec, appload.MessageTryChapter, tryReq("example-reader", seriesID, chapterID))
	var ready struct {
		PageCount int `json:"pageCount"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageTryReady), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.PageCount < 2 {
		t.Skip("this chapter has only one page; nothing to turn to")
	}

	b, _ := json.Marshal(map[string]any{
		"sourceId": "example-reader", "seriesId": seriesID, "chapterId": chapterID, "index": 1,
	})
	handle(t, svc, rec, appload.MessageTryPageRequest, string(b))

	var page struct {
		Index int    `json:"index"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageTryPage), &page); err != nil {
		t.Fatal(err)
	}
	if page.Index != 1 {
		t.Errorf("index = %d, want 1", page.Index)
	}
	if _, err := os.Stat(page.Path); err != nil {
		t.Fatalf("page 1's path does not exist: %v", err)
	}
}

// TestTryIsRefusedForABook is the mutation-testing anchor for "offer Try for
// a FileTheme": a book has no page images, and this must be refused before
// any of Pages() runs — even with a working Try cache behind it, which is
// why this wires its own service and source rather than reusing
// newBookService (which does not configure one).
func TestTryIsRefusedForABook(t *testing.T) {
	th := &bookTheme{}
	dir := t.TempDir()
	routes := downloadRoutes(t)

	svc, store, _, _, rec := newDownloadServiceWith(t, routes, func(o *service.Options) {
		o.Registry.MustRegister(th)
		o.TryCache = tryreader.New(dir, o.Fetcher)
	})
	if _, err := store.Add(&theme.Source{
		Name: "Example Books", Lang: "en", Theme: bookThemeID,
		BaseURL: "https://books.example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}

	handle(t, svc, rec, appload.MessageTryChapter,
		tryReq("example-books", "/book/openlibrary/OL1W", bookReleaseID))

	var errMsg struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &errMsg); err != nil {
		t.Fatal(err)
	}
	if errMsg.Code != "try_unavailable" {
		t.Errorf("code = %q, want try_unavailable", errMsg.Code)
	}
	if th.asked() != 0 {
		t.Errorf("Pages() was called %d times for a book; it must never be", th.asked())
	}
}

// TestTryPageRequestForAnotherChapterIsRefused makes sure a stale page
// request — the reader having since moved on, or never having opened a
// session at all — cannot be served against whatever session happens to be
// open.
func TestTryPageRequestForAnotherChapterIsRefused(t *testing.T) {
	svc, _, _, _, _, rec := newTryService(t)
	seriesID, chapterID := firstChapter(t, svc, rec)

	handle(t, svc, rec, appload.MessageTryChapter, tryReq("example-reader", seriesID, chapterID))
	rec.wait(t, appload.MessageTryReady)

	b, _ := json.Marshal(map[string]any{
		"sourceId": "example-reader", "seriesId": seriesID, "chapterId": "not-the-open-chapter", "index": 0,
	})
	handle(t, svc, rec, appload.MessageTryPageRequest, string(b))

	var errMsg struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &errMsg); err != nil {
		t.Fatal(err)
	}
	if errMsg.Code != "try_unavailable" {
		t.Errorf("code = %q, want try_unavailable", errMsg.Code)
	}
}
