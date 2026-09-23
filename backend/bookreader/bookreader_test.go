package bookreader_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/bookreader"
	"github.com/rickl/quire/backend/bookrender"
	"github.com/rickl/quire/backend/state"
)

// fakeRenderer implements bookreader.Renderer over an in-memory "book": a
// slice of page texts that reflows differently depending on how many
// characters-per-page the current layout allows, so a settings change
// genuinely moves content between pages the way a real reflow would.
type fakeRenderer struct {
	mu     sync.Mutex
	closed bool

	// fullText is the whole "book", a single string; charsPerPage (set by
	// Layout) decides how it is paginated.
	fullText     string
	charsPerPage int

	pages []string // current pagination, computed by Layout

	openErr   error
	renderErr error
}

func (f *fakeRenderer) Open(ctx context.Context, path string) (bookrender.OpenResult, error) {
	if f.openErr != nil {
		return bookrender.OpenResult{}, f.openErr
	}
	return bookrender.OpenResult{FixedLayout: false, Title: "Fake Book"}, nil
}

func (f *fakeRenderer) Layout(ctx context.Context, p bookrender.LayoutParams) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The em size stands in for "how much fits on a page": a bigger font
	// fits less text per page, exactly like a real reflow.
	perPage := int(400 / p.Em * 10)
	if perPage < 10 {
		perPage = 10
	}
	f.charsPerPage = perPage
	f.pages = nil
	for i := 0; i < len(f.fullText); i += perPage {
		end := i + perPage
		if end > len(f.fullText) {
			end = len(f.fullText)
		}
		f.pages = append(f.pages, f.fullText[i:end])
	}
	return len(f.pages), nil
}

func (f *fakeRenderer) Render(ctx context.Context, page int, out string, scale float64) error {
	if f.renderErr != nil {
		return f.renderErr
	}
	return os.WriteFile(out, []byte("fake-png"), 0o644)
}

func (f *fakeRenderer) Text(ctx context.Context, page int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if page < 0 || page >= len(f.pages) {
		return "", fmt.Errorf("page %d out of range", page)
	}
	return f.pages[page], nil
}

func (f *fakeRenderer) Outline(ctx context.Context) ([]bookrender.TocEntry, error) {
	return []bookrender.TocEntry{{Title: "Chapter 1", Page: 0, Level: 0}}, nil
}

func (f *fakeRenderer) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// A book long enough to reflow into a meaningfully different number of pages
// at different font sizes, built from unique tokens so any short run of its
// text identifies exactly where in the book it came from — unlike prose with
// repeated phrasing, which would let a search match the wrong page for the
// wrong reason.
func longBookText() string {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "TOKEN%05d_", i)
	}
	return b.String()
}

func openTestSession(t *testing.T, dir string, f *fakeRenderer) (*bookreader.Cache, *bookreader.Session) {
	t.Helper()
	c := bookreader.New(dir, func() bookreader.Renderer { return f })
	sess, err := c.Open(context.Background(), "book.epub", state.DefaultReaderSettings())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	return c, sess
}

func TestSessionOpenAndPage(t *testing.T) {
	f := &fakeRenderer{fullText: longBookText()}
	_, sess := openTestSession(t, t.TempDir(), f)

	if sess.Title() != "Fake Book" {
		t.Errorf("Title() = %q", sess.Title())
	}
	if sess.FixedLayout() {
		t.Error("FixedLayout() = true, want false")
	}
	if sess.PageCount() <= 0 {
		t.Fatal("PageCount() <= 0")
	}
	toc := sess.TOC()
	if len(toc) != 1 || toc[0].Title != "Chapter 1" {
		t.Errorf("TOC() = %+v", toc)
	}

	path, err := sess.Page(context.Background(), 0)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("page file missing: %v", err)
	}

	if _, err := sess.Page(context.Background(), sess.PageCount()+10); err == nil {
		t.Error("expected an error for an out-of-range page")
	}
}

// TestSetSettingsReanchorsToTheSnippet is the mutation check for "position
// anchoring finds the snippet page after a settings change": a font-size
// change reflows the book into a different number of pages, and the reader
// must land back on the page holding the same text, not on whatever page
// number it used to be.
func TestSetSettingsReanchorsToTheSnippet(t *testing.T) {
	f := &fakeRenderer{fullText: longBookText()}
	dir := t.TempDir()
	_, sess := openTestSession(t, dir, f)

	ctx := context.Background()
	oldPageCount := sess.PageCount()
	oldPage := oldPageCount / 3 // wherever the reader happens to be

	oldText, err := f.Text(ctx, oldPage)
	if err != nil {
		t.Fatal(err)
	}
	// The unique token nearest the start of the old page — what a real
	// render.js snippet (the first ~200 characters) would also capture.
	needle := oldText
	if len(needle) > 20 {
		needle = needle[:20]
	}

	// A much bigger font: fewer characters fit per page, so the book grows
	// into more, shorter pages, and the same text lands on a different one.
	bigFont := state.ReaderSettings{Font: "book", Size: 9, Margins: "normal", Spacing: "book", Align: "book"}
	newPageCount, newPage, toc, err := sess.SetSettings(ctx, bigFont, oldPage)
	if err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if newPageCount <= oldPageCount {
		t.Fatalf("expected a bigger font to need more pages: old=%d new=%d", oldPageCount, newPageCount)
	}
	if len(toc) != 1 {
		t.Errorf("TOC not refreshed: %+v", toc)
	}

	newText, err := f.Text(ctx, newPage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(newText, needle) {
		t.Errorf("after the settings change, page %d (%q) does not hold the old page's text (%q)",
			newPage, newText, needle)
	}
}

func TestInitialPageUsesRecordedPositionUnderTheSameLayout(t *testing.T) {
	f := &fakeRenderer{fullText: longBookText()}
	_, sess := openTestSession(t, t.TempDir(), f)
	ctx := context.Background()

	fraction, snippet, hash, err := sess.PositionFor(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	got := sess.InitialPage(ctx, 3, fraction, snippet, hash)
	if got != 3 {
		t.Errorf("InitialPage under the same layout = %d, want 3", got)
	}
}

func TestInitialPageReanchorsUnderADifferentLayout(t *testing.T) {
	f := &fakeRenderer{fullText: longBookText()}
	_, sess := openTestSession(t, t.TempDir(), f)
	ctx := context.Background()

	oldPage := sess.PageCount() / 3
	fraction, snippet, _, err := sess.PositionFor(ctx, oldPage)
	if err != nil {
		t.Fatal(err)
	}
	needle := snippet
	if len(needle) > 20 {
		needle = needle[:20]
	}

	// A stale layout hash — as if the settings changed since this position
	// was recorded, requiring a re-anchor rather than trusting the page.
	got := sess.InitialPage(ctx, oldPage, fraction, snippet, "stale-hash-from-another-layout")
	text, err := f.Text(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, needle) {
		t.Errorf("InitialPage under a different layout landed on page %d (%q), which does not hold %q", got, text, needle)
	}
}

// TestCloseRemovesTheCacheDirectory is the mutation check for "render cache
// removed on close".
func TestCloseRemovesTheCacheDirectory(t *testing.T) {
	f := &fakeRenderer{fullText: longBookText()}
	dir := t.TempDir()
	c := bookreader.New(dir, func() bookreader.Renderer { return f })
	sess, err := c.Open(context.Background(), "book.epub", state.DefaultReaderSettings())
	if err != nil {
		t.Fatal(err)
	}

	path, err := sess.Page(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("page file missing before Close: %v", err)
	}
	sessionDir := filepath.Dir(path)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Errorf("the session's cache directory still exists after Close: %v", err)
	}
	if !f.closed {
		t.Error("Close did not close the underlying renderer")
	}
}

func TestNewSweepsAPreviousCrashedSession(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "leftover-session", "0000.png")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bookreader.New(dir, func() bookreader.Renderer { return &fakeRenderer{} })
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("New did not sweep a leftover session directory")
	}
}
