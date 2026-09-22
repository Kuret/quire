package tryreader_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/tryreader"
)

// stubTheme is the least a theme.Theme can be; tryreader.Session only reaches
// into it through theme.PolicyFor's optional side interfaces, and this
// implements neither, so PolicyFor returns exactly src.Policy() unchanged.
type stubTheme struct{}

func (stubTheme) ID() string                  { return "stub" }
func (stubTheme) Fingerprint(*probe.Page) int { return 0 }
func (stubTheme) Search(context.Context, *theme.Source, string, int) ([]theme.SeriesStub, error) {
	return nil, nil
}
func (stubTheme) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return nil, nil
}
func (stubTheme) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return nil, nil
}
func (stubTheme) Pages(context.Context, *theme.Source, string) ([]string, error) { return nil, nil }
func (stubTheme) AllowedHosts() []string                                         { return nil }
func (stubTheme) SuggestedName() string                                          { return "" }

// countingFetcher serves a fixed JPEG body to every GetRetrievalFrom call,
// and counts how many times each URL was actually fetched — the thing that
// proves a cached page is served from disk rather than fetched twice.
type countingFetcher struct {
	body []byte

	mu    sync.Mutex
	hits  map[string]int
	delay time.Duration
}

func newCountingFetcher(body []byte) *countingFetcher {
	return &countingFetcher{body: body, hits: map[string]int{}}
}

func (f *countingFetcher) Get(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	return f.GetRetrievalFrom(ctx, p, rawurl, fetch.Referrer{})
}
func (f *countingFetcher) GetFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error) {
	return f.GetRetrievalFrom(ctx, p, rawurl, from)
}
func (f *countingFetcher) GetRetrieval(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	return f.GetRetrievalFrom(ctx, p, rawurl, fetch.Referrer{})
}
func (f *countingFetcher) GetRetrievalFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error) {
	f.mu.Lock()
	f.hits[rawurl]++
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return &fetch.Response{StatusCode: 200, Body: f.body}, nil
}
func (f *countingFetcher) PostForm(ctx context.Context, p *fetch.Policy, rawurl string, form url.Values) (*fetch.Response, error) {
	return nil, nil
}

func (f *countingFetcher) hitsFor(rawurl string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[rawurl]
}

// pageJPEG is a real, decodable JPEG — a fake that handed back HTML or empty
// bytes would prove nothing about the transcode path.
func pageJPEG(t *testing.T) []byte {
	t.Helper()
	// Larger than the panel grid on both axes, so a correctly-running
	// transcode has to actually downscale it rather than pass it through.
	const w, h = 2480, 3508
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 256)})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testSource() *theme.Source {
	return &theme.Source{ID: "src", BaseURL: "https://example.invalid"}
}

func TestPageIsFetchedTranscodedAndCached(t *testing.T) {
	f := newCountingFetcher(pageJPEG(t))
	cache := tryreader.New(t.TempDir(), f)
	sess := cache.NewSession(stubTheme{}, testSource(),
		[]string{"https://example.invalid/1.jpg", "https://example.invalid/2.jpg"}, fetch.Referrer{})

	path, err := sess.Page(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the cached page is not a decodable JPEG: %v", err)
	}
	// Downscaled to fit the panel grid — proof the imageproc pipeline
	// actually ran rather than the 2480x3508 source being copied verbatim.
	if cfg.Width > tryreader.PageWidth || cfg.Height > tryreader.PageHeight {
		t.Errorf("page is %dx%d, want it to fit within the %dx%d panel grid",
			cfg.Width, cfg.Height, tryreader.PageWidth, tryreader.PageHeight)
	}
	if cfg.Width == 2480 || cfg.Height == 3508 {
		t.Errorf("page is %dx%d, the same as the source — it was not downscaled", cfg.Width, cfg.Height)
	}

	if hits := f.hitsFor("https://example.invalid/1.jpg"); hits != 1 {
		t.Fatalf("fetched page 0 %d times, want 1", hits)
	}

	// A second call must be served from disk, not fetched again.
	if _, err := sess.Page(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if hits := f.hitsFor("https://example.invalid/1.jpg"); hits != 1 {
		t.Fatalf("page 0 was fetched again on a cache hit: %d requests", hits)
	}
}

func TestPageOutOfRangeIsRefused(t *testing.T) {
	f := newCountingFetcher(pageJPEG(t))
	cache := tryreader.New(t.TempDir(), f)
	sess := cache.NewSession(stubTheme{}, testSource(), []string{"https://example.invalid/1.jpg"}, fetch.Referrer{})

	if _, err := sess.Page(context.Background(), 5); err == nil {
		t.Fatal("expected an error for a page beyond the known list")
	}
}

func TestPrefetchWritesTheFileInTheBackground(t *testing.T) {
	f := newCountingFetcher(pageJPEG(t))
	cache := tryreader.New(t.TempDir(), f)
	sess := cache.NewSession(stubTheme{}, testSource(),
		[]string{"https://example.invalid/1.jpg", "https://example.invalid/2.jpg"}, fetch.Referrer{})

	sess.Prefetch(context.Background(), 1)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.hitsFor("https://example.invalid/2.jpg") > 0 {
			path, err := sess.Page(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("prefetch never fetched page 1")
}

func TestExtendAcceptsAnAgreeingPrefix(t *testing.T) {
	f := newCountingFetcher(pageJPEG(t))
	cache := tryreader.New(t.TempDir(), f)
	sess := cache.NewSession(stubTheme{}, testSource(), []string{"https://example.invalid/1.jpg"}, fetch.Referrer{})

	if sess.PageCount() != 1 {
		t.Fatalf("PageCount() = %d, want 1", sess.PageCount())
	}
	ok := sess.Extend([]string{"https://example.invalid/1.jpg", "https://example.invalid/2.jpg"})
	if !ok {
		t.Fatal("Extend refused an agreeing, longer list")
	}
	if sess.PageCount() != 2 {
		t.Fatalf("PageCount() = %d, want 2 after Extend", sess.PageCount())
	}
}

func TestExtendRefusesADisagreeingList(t *testing.T) {
	f := newCountingFetcher(pageJPEG(t))
	cache := tryreader.New(t.TempDir(), f)
	sess := cache.NewSession(stubTheme{}, testSource(), []string{"https://example.invalid/1.jpg"}, fetch.Referrer{})

	ok := sess.Extend([]string{"https://example.invalid/DIFFERENT.jpg", "https://example.invalid/2.jpg"})
	if ok {
		t.Fatal("Extend accepted a list that disagreed with what was already known")
	}
	if sess.PageCount() != 1 {
		t.Fatalf("PageCount() = %d, want 1 (unchanged)", sess.PageCount())
	}
}

// TestEndRemovesTheSessionDirectory is the mutation-testing anchor for "skip
// the cleanup of a finished session": if End is ever made a no-op, or the
// directory it removes is the wrong one, this fails.
func TestEndRemovesTheSessionDirectory(t *testing.T) {
	f := newCountingFetcher(pageJPEG(t))
	root := t.TempDir()
	cache := tryreader.New(root, f)
	sess := cache.NewSession(stubTheme{}, testSource(), []string{"https://example.invalid/1.jpg"}, fetch.Referrer{})

	path, err := sess.Page(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the page should exist before End: %v", err)
	}

	if err := sess.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the page still exists after End: err=%v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("the cache root still holds %d entries after the only session ended", len(entries))
	}
}

// TestNewSweepsAnyExistingCache is the mutation-testing anchor for "must not
// accumulate if the app dies mid-session": New is the one moment a fresh
// start can tell a stale Try cache from a live one, and it has to remove it
// rather than build on top of what a crash left behind.
func TestNewSweepsAnyExistingCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "try")
	stray := filepath.Join(dir, "leftover-session", "0000.jpg")
	if err := os.MkdirAll(filepath.Dir(stray), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}

	tryreader.New(dir, newCountingFetcher(pageJPEG(t)))

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("a crash-orphaned Try session survived New: err=%v", err)
	}
}

// TestConcurrentRequestsForOnePageFetchItOnce proves the inflight collapsing:
// a prefetch racing the reader's own request for the same page must not cost
// two fetches of the same page.
func TestConcurrentRequestsForOnePageFetchItOnce(t *testing.T) {
	f := newCountingFetcher(pageJPEG(t))
	f.delay = 50 * time.Millisecond
	cache := tryreader.New(t.TempDir(), f)
	sess := cache.NewSession(stubTheme{}, testSource(), []string{"https://example.invalid/1.jpg"}, fetch.Referrer{})

	var wg sync.WaitGroup
	var errs int32
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sess.Page(context.Background(), 0); err != nil {
				atomic.AddInt32(&errs, 1)
			}
		}()
	}
	wg.Wait()
	if errs != 0 {
		t.Fatalf("%d of 5 concurrent requests for the same page failed", errs)
	}
	if hits := f.hitsFor("https://example.invalid/1.jpg"); hits != 1 {
		t.Fatalf("page 0 was fetched %d times concurrently, want 1", hits)
	}
}
