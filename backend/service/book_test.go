package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/bookreader"
	"github.com/rickl/quire/backend/bookrender"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/shelf"
	"github.com/rickl/quire/backend/state"
)

// fakeBookRenderer is a minimal, in-memory bookreader.Renderer for the
// service layer's own tests — it never touches mutool, so these tests stay
// offline like the rest of the suite (books-contract.md §B: "Tests offline
// on the Mac").
type fakeBookRenderer struct {
	pages   int
	title   string
	openErr error
}

func (f *fakeBookRenderer) Open(ctx context.Context, path string) (bookrender.OpenResult, error) {
	if f.openErr != nil {
		return bookrender.OpenResult{}, f.openErr
	}
	return bookrender.OpenResult{FixedLayout: false, Title: f.title}, nil
}
func (f *fakeBookRenderer) Layout(ctx context.Context, p bookrender.LayoutParams) (int, error) {
	return f.pages, nil
}
func (f *fakeBookRenderer) Render(ctx context.Context, page int, out string, scale float64) error {
	return os.WriteFile(out, []byte("fake-png"), 0o644)
}
func (f *fakeBookRenderer) Text(ctx context.Context, page int) (string, error) {
	return "page text", nil
}
func (f *fakeBookRenderer) Outline(ctx context.Context) ([]bookrender.TocEntry, error) {
	return []bookrender.TocEntry{{Title: "Chapter 1", Page: 0, Level: 0}}, nil
}
func (f *fakeBookRenderer) Close() error { return nil }

// withBookCache wires a fake book reader into the download harness's
// Options, so a test can open a saved or a tried book without a real
// mutool.
func withBookCache(t *testing.T, cacheDir string) func(*service.Options) {
	t.Helper()
	return func(o *service.Options) {
		o.BookCache = bookreader.New(cacheDir, func() bookreader.Renderer {
			return &fakeBookRenderer{pages: 12, title: "Fake Book"}
		})
	}
}

func TestOpenSavedBookAnswersBookOpened(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t), withBookCache(t, filepath.Join(t.TempDir(), "bookcache")))
	addSource(t, h.store)

	bookFile := filepath.Join(h.savedDir, "books", "example-reader", "series-1", "vol-1.epub")
	if err := os.MkdirAll(filepath.Dir(bookFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bookFile, []byte("epub bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := shelf.Record{
		Key:          shelf.Key{Source: "example-reader", Series: "series-1", Chapter: "vol-1"},
		Kind:         shelf.KindBook,
		SeriesTitle:  "A Series",
		ChapterTitle: "Volume 1",
		File:         "books/example-reader/series-1/vol-1.epub",
		Format:       "epub",
	}
	if err := h.shelfStore.Put(rec); err != nil {
		t.Fatal(err)
	}

	rc := &recorder{}
	handle(t, h.svc, rc, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"series-1","chapterId":"vol-1"}`)

	var got struct {
		SourceID       string                     `json:"sourceId"`
		ChapterID      string                     `json:"chapterId"`
		Title          string                     `json:"title"`
		Mode           string                     `json:"mode"`
		FixedLayout    bool                       `json:"fixedLayout"`
		PageCount      int                        `json:"pageCount"`
		Page           int                        `json:"page"`
		SettingsNote   string                     `json:"settingsNote"`
		SettingsFields []bookrender.SettingsField `json:"settingsFields"`
	}
	if err := json.Unmarshal(rc.wait(t, appload.MessageBookOpened), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "saved" {
		t.Errorf("mode = %q, want saved", got.Mode)
	}
	if got.Title != "Fake Book" {
		t.Errorf("title = %q", got.Title)
	}
	if got.PageCount != 12 {
		t.Errorf("pageCount = %d, want 12", got.PageCount)
	}
	if got.SettingsNote != bookrender.SettingsNote {
		t.Errorf("settingsNote = %q, want %q", got.SettingsNote, bookrender.SettingsNote)
	}
	if !reflect.DeepEqual(got.SettingsFields, bookrender.SettingsFields()) {
		t.Errorf("settingsFields = %+v, want the backend's own %+v", got.SettingsFields, bookrender.SettingsFields())
	}

	// Every field names a size step by its point size, taken straight from
	// bookrender's own em table (EmForSize) rather than a second copy that
	// could drift from it.
	sizeField := findSettingsField(t, got.SettingsFields, "size")
	if len(sizeField.Steps) != state.ReaderSizeMax-state.ReaderSizeMin+1 {
		t.Fatalf("size steps = %d, want %d", len(sizeField.Steps), state.ReaderSizeMax-state.ReaderSizeMin+1)
	}
	for _, step := range sizeField.Steps {
		want := fmt.Sprintf("%d pt", int(bookrender.EmForSize(step.ID)))
		if step.Label != want {
			t.Errorf("size step %d label = %q, want %q (from EmForSize)", step.ID, step.Label, want)
		}
	}
}

func findSettingsField(t *testing.T, fields []bookrender.SettingsField, key string) bookrender.SettingsField {
	t.Helper()
	for _, f := range fields {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("no settings field named %q among %+v", key, fields)
	return bookrender.SettingsField{}
}

func TestBookPageRequestServesAPage(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t), withBookCache(t, filepath.Join(t.TempDir(), "bookcache")))
	addSource(t, h.store)
	bookFile := filepath.Join(h.savedDir, "books", "example-reader", "series-1", "vol-1.epub")
	if err := os.MkdirAll(filepath.Dir(bookFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bookFile, []byte("epub bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.shelfStore.Put(shelf.Record{
		Key:  shelf.Key{Source: "example-reader", Series: "series-1", Chapter: "vol-1"},
		Kind: shelf.KindBook, File: "books/example-reader/series-1/vol-1.epub", Format: "epub",
	}); err != nil {
		t.Fatal(err)
	}

	rc := &recorder{}
	handle(t, h.svc, rc, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"series-1","chapterId":"vol-1"}`)
	rc.wait(t, appload.MessageBookOpened)

	handle(t, h.svc, rc, appload.MessageBookPageRequest,
		`{"sourceId":"example-reader","seriesId":"series-1","chapterId":"vol-1","index":2}`)

	var page struct {
		Index int    `json:"index"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(rc.wait(t, appload.MessageBookPage), &page); err != nil {
		t.Fatal(err)
	}
	if page.Index != 2 {
		t.Errorf("index = %d, want 2", page.Index)
	}
	if _, err := os.Stat(page.Path); err != nil {
		t.Errorf("page file missing: %v", err)
	}
}

func TestSetReaderSettingsRelaysAndPersists(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t), withBookCache(t, filepath.Join(t.TempDir(), "bookcache")))
	addSource(t, h.store)
	bookFile := filepath.Join(h.savedDir, "books", "example-reader", "series-1", "vol-1.epub")
	if err := os.MkdirAll(filepath.Dir(bookFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bookFile, []byte("epub bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.shelfStore.Put(shelf.Record{
		Key:  shelf.Key{Source: "example-reader", Series: "series-1", Chapter: "vol-1"},
		Kind: shelf.KindBook, File: "books/example-reader/series-1/vol-1.epub", Format: "epub",
	}); err != nil {
		t.Fatal(err)
	}

	rc := &recorder{}
	handle(t, h.svc, rc, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"series-1","chapterId":"vol-1"}`)
	rc.wait(t, appload.MessageBookOpened)

	handle(t, h.svc, rc, appload.MessageSetReaderSettings,
		`{"sourceId":"example-reader","seriesId":"series-1","chapterId":"vol-1","page":0,`+
			`"settings":{"font":"garamond","size":7,"margins":"wide","spacing":"relaxed","align":"left"}}`)

	var relaid struct {
		PageCount      int                        `json:"pageCount"`
		Page           int                        `json:"page"`
		SettingsNote   string                     `json:"settingsNote"`
		SettingsFields []bookrender.SettingsField `json:"settingsFields"`
	}
	if err := json.Unmarshal(rc.wait(t, appload.MessageBookRelaid), &relaid); err != nil {
		t.Fatal(err)
	}
	if relaid.PageCount != 12 {
		t.Errorf("pageCount = %d, want 12", relaid.PageCount)
	}
	if relaid.SettingsNote != bookrender.SettingsNote {
		t.Errorf("settingsNote = %q, want %q", relaid.SettingsNote, bookrender.SettingsNote)
	}
	if !reflect.DeepEqual(relaid.SettingsFields, bookrender.SettingsFields()) {
		t.Errorf("settingsFields = %+v, want the backend's own %+v", relaid.SettingsFields, bookrender.SettingsFields())
	}

	got := h.store.Settings().Reader()
	if got.Font != "garamond" || got.Size != 7 || got.Margins != "wide" || got.Spacing != "relaxed" || got.Align != "left" {
		t.Errorf("settings were not persisted: %+v", got)
	}
}

// TestTryChapterOnABookFetchesAndOpens is books-contract.md §B's Try-on-a-book
// path: TryChapter on a theme.FileTheme source fetches the file through the
// guarded client and opens it in a "try" book session, rather than the old
// try_unavailable refusal.
func TestTryChapterOnABookFetchesAndOpens(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th, withBookCache(t, filepath.Join(t.TempDir(), "bookcache")))

	handle(t, env.svc, env.rec, appload.MessageTryChapter,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","chapterId":"`+bookReleaseID+`"}`)

	var got struct {
		Mode      string `json:"mode"`
		Title     string `json:"title"`
		PageCount int    `json:"pageCount"`
	}
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageBookOpened), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "try" {
		t.Errorf("mode = %q, want try", got.Mode)
	}
	if got.Title != "Fake Book" {
		t.Errorf("title = %q", got.Title)
	}

	// Nothing was remembered — Try leaves no shelf record, book or not.
	if _, ok := env.shelfStore.Get(shelf.Key{
		Source: "example-books", Series: "/book/openlibrary/OL1W", Chapter: bookReleaseID,
	}); ok {
		t.Error("trying a book should not have saved it")
	}
}

// TestCloseBookInTryModeDeletesTheFetchedFile: try mode has nothing to
// remember, only a fetched temp file to clean up (books-contract.md §B).
func TestCloseBookInTryModeDeletesTheFetchedFile(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th, withBookCache(t, filepath.Join(t.TempDir(), "bookcache")))

	handle(t, env.svc, env.rec, appload.MessageTryChapter,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","chapterId":"`+bookReleaseID+`"}`)
	env.rec.wait(t, appload.MessageBookOpened)

	handle(t, env.svc, env.rec, appload.MessageBookPageRequest,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","chapterId":"`+bookReleaseID+`","index":0}`)
	var page struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageBookPage), &page); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Dir(page.Path)

	handle(t, env.svc, env.rec, appload.MessageCloseBook,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","chapterId":"`+bookReleaseID+`","page":0}`)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Errorf("the try session's cache directory still exists after closing: %v", err)
	}
}

func TestCloseBookSavesPositionInSavedMode(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t), withBookCache(t, filepath.Join(t.TempDir(), "bookcache")))
	addSource(t, h.store)
	bookFile := filepath.Join(h.savedDir, "books", "example-reader", "series-1", "vol-1.epub")
	if err := os.MkdirAll(filepath.Dir(bookFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bookFile, []byte("epub bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := shelf.Key{Source: "example-reader", Series: "series-1", Chapter: "vol-1"}
	if err := h.shelfStore.Put(shelf.Record{
		Key: key, Kind: shelf.KindBook, File: "books/example-reader/series-1/vol-1.epub", Format: "epub",
	}); err != nil {
		t.Fatal(err)
	}

	rc := &recorder{}
	handle(t, h.svc, rc, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"series-1","chapterId":"vol-1"}`)
	rc.wait(t, appload.MessageBookOpened)

	handle(t, h.svc, rc, appload.MessageCloseBook,
		`{"sourceId":"example-reader","seriesId":"series-1","chapterId":"vol-1","page":5}`)

	// closeBook has no reply; poll the shelf record directly.
	deadline := time.Now().Add(2 * time.Second)
	var final shelf.Record
	for time.Now().Before(deadline) {
		rec, ok := h.shelfStore.Get(key)
		if ok && rec.Position == 5 {
			final = rec
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if final.Position != 5 {
		t.Fatalf("the book's position was never saved as 5; got %+v", final)
	}
	if final.PositionLayout == "" {
		t.Error("PositionLayout was not recorded")
	}
}
