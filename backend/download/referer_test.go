package download_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/download"
)

// PLAN §7.6: the Referer sent with a page image must be the page that image
// address was really extracted from. One queue serves a whole volume, so the
// only way for that to be true is for the value to travel *with the page* —
// resolved once per chapter where the chapter was still in hand, never
// reconstructed inside the fetch.
//
// The bug this pins is the plausible one: every page of a forty-chapter volume
// carrying chapter one's Referer, which would be a header naming a page that
// image did not come from.

// refererFetcher records the referer each URL was fetched with.
type refererFetcher struct {
	body []byte

	mu   sync.Mutex
	seen map[string]string
}

func (f *refererFetcher) Get(_ context.Context, url, referer string) (io.ReadCloser, error) {
	f.mu.Lock()
	if f.seen == nil {
		f.seen = map[string]string{}
	}
	f.seen[url] = referer
	f.mu.Unlock()
	return io.NopCloser(bytes.NewReader(f.body)), nil
}

func (f *refererFetcher) refererFor(url string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[url]
}

func TestEachPageCarriesItsOwnChaptersReferer(t *testing.T) {
	dir := t.TempDir()
	f := &refererFetcher{body: synthJPEG(t, 800, 1200)}

	chs := make([]download.Chapter, 3)
	for i := range chs {
		chs[i] = download.Chapter{
			ID:      fmt.Sprintf("ch-%d", i+1),
			Title:   fmt.Sprintf("Chapter %d", i+1),
			Volume:  "1",
			Referer: fmt.Sprintf("https://example.invalid/read/%d/", i+1),
		}
		for p := range 4 {
			chs[i].PageURLs = append(chs[i].PageURLs,
				fmt.Sprintf("https://cdn.example.invalid/%d/%d.jpg", i+1, p))
		}
	}

	q := download.New(f, download.Options{MinFreeBytes: -1, Concurrency: 3})
	if _, stats, err := q.Run(t.Context(), dir, chs); err != nil {
		t.Fatalf("Run: %v (%+v)", err, stats.Progress)
	}

	for _, ch := range chs {
		for _, u := range ch.PageURLs {
			if got := f.refererFor(u); got != ch.Referer {
				t.Errorf("%s was fetched with Referer %q, want its own chapter's %q", u, got, ch.Referer)
			}
		}
	}
}

// A chapter whose theme had no page to name must produce no Referer at all,
// and it must not inherit one from a sibling chapter that did.
func TestAChapterWithNoRefererSendsNoneAndBorrowsNone(t *testing.T) {
	dir := t.TempDir()
	f := &refererFetcher{body: synthJPEG(t, 800, 1200)}

	chs := []download.Chapter{
		{
			ID: "ch-1", Title: "Chapter 1", Volume: "1",
			Referer:  "https://example.invalid/read/1/",
			PageURLs: []string{"https://cdn.example.invalid/1/0.jpg"},
		},
		{
			ID: "ch-2", Title: "Chapter 2", Volume: "1",
			PageURLs: []string{"https://cdn.example.invalid/2/0.jpg"},
		},
	}

	q := download.New(f, download.Options{MinFreeBytes: -1, Concurrency: 1})
	if _, stats, err := q.Run(t.Context(), dir, chs); err != nil {
		t.Fatalf("Run: %v (%+v)", err, stats.Progress)
	}

	if got := f.refererFor("https://cdn.example.invalid/1/0.jpg"); got != chs[0].Referer {
		t.Errorf("chapter 1's page carried %q, want %q", got, chs[0].Referer)
	}
	if got := f.refererFor("https://cdn.example.invalid/2/0.jpg"); got != "" {
		t.Errorf("chapter 2 names no page, so its page must carry no Referer; got %q", got)
	}
}
