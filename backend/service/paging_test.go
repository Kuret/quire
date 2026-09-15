package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rickl/quire/backend/theme"
)

// stubSource is a fake site: it hands back `perPage` stubs per source page for
// `pages` pages, then nothing. It counts calls, which is how the tests hold
// PLAN §12.1's "one tap must not equal one HTTP request" to account.
type stubSource struct {
	perPage int
	pages   int
	calls   int

	// repeat makes every source page return the same items, which some sites
	// genuinely do for an out-of-range page number.
	repeat bool

	// failFrom makes the source page of that number (and every later one) fail.
	failFrom int
}

var errSource = errors.New("the site did not answer")

func (s *stubSource) fetch(_ context.Context, sourcePage int) ([]theme.SeriesStub, error) {
	s.calls++
	if s.failFrom > 0 && sourcePage >= s.failFrom {
		return nil, errSource
	}
	if !s.repeat && sourcePage > s.pages {
		return nil, nil
	}
	base := 0
	if !s.repeat {
		base = (sourcePage - 1) * s.perPage
	}
	out := make([]theme.SeriesStub, 0, s.perPage)
	for i := 0; i < s.perPage; i++ {
		n := base + i
		out = append(out, theme.SeriesStub{ID: fmt.Sprintf("s%d", n), Title: fmt.Sprintf("Series %d", n)})
	}
	return out, nil
}

func ids(items []theme.SeriesStub) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func sameIDs(got []theme.SeriesStub, want []string) bool {
	g := ids(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

// TestSeriesPagerServesDisplayPages is the core of PLAN §12.1: the display page
// size has nothing to do with the source page size, and the total is only ever
// reported once the source has actually run out.
func TestSeriesPagerServesDisplayPages(t *testing.T) {
	tests := []struct {
		name string

		src  stubSource
		page int
		size int

		wantIDs     []string
		wantPage    int
		wantTotal   int
		wantHasMore bool

		// wantCalls is the number of source requests the whole sequence of
		// asks below cost. Zero means "do not check".
		wantCalls int
	}{
		{
			// The display page is smaller than the source page: one request
			// serves several taps.
			name:        "display page smaller than source page",
			src:         stubSource{perPage: 20, pages: 3},
			page:        1,
			size:        9,
			wantIDs:     []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"},
			wantPage:    1,
			wantTotal:   0, // the source has not run out, so no total is claimed
			wantHasMore: true,
			wantCalls:   1,
		},
		{
			// The source hands back fewer than a display page holds, so one
			// tap costs several requests — but still fills the screen. A page
			// that is half empty with more available is the thing PLAN §12.1
			// calls the scrolling problem in miniature.
			name:        "source page smaller than display page",
			src:         stubSource{perPage: 3, pages: 10},
			page:        1,
			size:        9,
			wantIDs:     []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"},
			wantPage:    1,
			wantTotal:   0,
			wantHasMore: true,
			wantCalls:   4,
		},
		{
			// The source has more cached than this page shows but has not yet
			// said it is finished, so there is no honest total to give — and
			// "Page 2" with a working Next is the whole of what is known.
			name:        "runs out mid page",
			src:         stubSource{perPage: 5, pages: 2},
			page:        2,
			size:        4,
			wantIDs:     []string{"s4", "s5", "s6", "s7"},
			wantPage:    2,
			wantTotal:   0,
			wantHasMore: true,
		},
		{
			name:        "last page is partial and is a hard stop",
			src:         stubSource{perPage: 5, pages: 2},
			page:        3,
			size:        4,
			wantIDs:     []string{"s8", "s9"},
			wantPage:    3,
			wantTotal:   3,
			wantHasMore: false,
		},
		{
			// Asking past the end clamps rather than showing a blank screen,
			// and reports the page it actually served.
			name:        "past the end clamps",
			src:         stubSource{perPage: 5, pages: 2},
			page:        99,
			size:        4,
			wantIDs:     []string{"s8", "s9"},
			wantPage:    3,
			wantTotal:   3,
			wantHasMore: false,
		},
		{
			// A source with nothing in it is one empty page, not zero pages.
			name:        "empty source is one empty page",
			src:         stubSource{perPage: 0, pages: 0},
			page:        1,
			size:        9,
			wantIDs:     nil,
			wantPage:    1,
			wantTotal:   1,
			wantHasMore: false,
		},
		{
			// A source that serves the same page for every page number would
			// otherwise be an infinite loop.
			name:        "a repeating source is exhausted, not looped",
			src:         stubSource{perPage: 4, repeat: true},
			page:        1,
			size:        9,
			wantIDs:     []string{"s0", "s1", "s2", "s3"},
			wantPage:    1,
			wantTotal:   1,
			wantHasMore: false,
			wantCalls:   2,
		},
		{
			// Exactly a whole number of pages: the boundary that most easily
			// grows a phantom empty page on the end.
			name:        "exactly full last page",
			src:         stubSource{perPage: 4, pages: 2},
			page:        2,
			size:        4,
			wantIDs:     []string{"s4", "s5", "s6", "s7"},
			wantPage:    2,
			wantTotal:   2,
			wantHasMore: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src
			p := newSeriesPager()
			res, err := p.Page(context.Background(), tc.page, tc.size, src.fetch)
			if err != nil {
				t.Fatalf("Page: %v", err)
			}
			if !sameIDs(res.Items, tc.wantIDs) {
				t.Errorf("items = %v, want %v", ids(res.Items), tc.wantIDs)
			}
			if res.Page != tc.wantPage {
				t.Errorf("page = %d, want %d", res.Page, tc.wantPage)
			}
			if res.TotalPages != tc.wantTotal {
				t.Errorf("totalPages = %d, want %d", res.TotalPages, tc.wantTotal)
			}
			if res.HasMore != tc.wantHasMore {
				t.Errorf("hasMore = %v, want %v", res.HasMore, tc.wantHasMore)
			}
			if tc.wantCalls > 0 && src.calls != tc.wantCalls {
				t.Errorf("source requests = %d, want %d", src.calls, tc.wantCalls)
			}
		})
	}
}

// TestSeriesPagerTurnsPagesWithoutRefetching is the requirement stated as a
// number: paging forward through what one request returned, and back again,
// must not touch the network.
func TestSeriesPagerTurnsPagesWithoutRefetching(t *testing.T) {
	src := &stubSource{perPage: 20, pages: 5}
	p := newSeriesPager()
	ctx := context.Background()

	for _, page := range []int{1, 2, 1, 2, 1} {
		if _, err := p.Page(ctx, page, 9, src.fetch); err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
	}
	if src.calls != 1 {
		t.Fatalf("five page turns cost %d source requests, want 1", src.calls)
	}

	// Page 3 needs 27 items and only 20 are cached, so exactly one more.
	if _, err := p.Page(ctx, 3, 9, src.fetch); err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if src.calls != 2 {
		t.Fatalf("crossing the cached boundary cost %d source requests in total, want 2", src.calls)
	}

	// And coming back is free again.
	if _, err := p.Page(ctx, 1, 9, src.fetch); err != nil {
		t.Fatalf("back to page 1: %v", err)
	}
	if src.calls != 2 {
		t.Fatalf("going back refetched: %d source requests, want 2", src.calls)
	}
}

// TestSeriesPagerKeepsWhatItHasWhenTheNextFetchFails: losing a screenful the
// user can already see because the *next* request failed is the worse answer.
func TestSeriesPagerKeepsWhatItHasWhenTheNextFetchFails(t *testing.T) {
	ctx := context.Background()

	t.Run("cached page survives a failure", func(t *testing.T) {
		src := &stubSource{perPage: 5, pages: 10, failFrom: 2}
		p := newSeriesPager()
		res, err := p.Page(ctx, 1, 9, src.fetch)
		if !errors.Is(err, errSource) {
			t.Fatalf("err = %v, want the source error", err)
		}
		if !sameIDs(res.Items, []string{"s0", "s1", "s2", "s3", "s4"}) {
			t.Errorf("items = %v, want the five that did arrive", ids(res.Items))
		}
		// A failure is not an ending: the source was not exhausted, so no
		// total is claimed and Next stays available to retry.
		if res.TotalPages != 0 {
			t.Errorf("totalPages = %d, want 0 — a failure is not a total", res.TotalPages)
		}
		if !res.HasMore {
			t.Error("hasMore = false after a failure, want true so the user can retry")
		}
	})

	t.Run("nothing cached means nothing to show", func(t *testing.T) {
		src := &stubSource{perPage: 5, pages: 10, failFrom: 1}
		p := newSeriesPager()
		res, err := p.Page(ctx, 1, 9, src.fetch)
		if !errors.Is(err, errSource) {
			t.Fatalf("err = %v, want the source error", err)
		}
		if len(res.Items) != 0 {
			t.Errorf("items = %v, want none", ids(res.Items))
		}
	})

	t.Run("a retry after a failure resumes rather than restarting", func(t *testing.T) {
		src := &stubSource{perPage: 5, pages: 10, failFrom: 2}
		p := newSeriesPager()
		if _, err := p.Page(ctx, 1, 9, src.fetch); err == nil {
			t.Fatal("expected the first extension to fail")
		}
		src.failFrom = 0
		res, err := p.Page(ctx, 1, 9, src.fetch)
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if !sameIDs(res.Items, []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"}) {
			t.Errorf("items = %v, want a full page with no duplicates", ids(res.Items))
		}
	})
}

// TestSeriesPagerDedupes: a source that overlaps its pages must not put the
// same tile on screen twice, and must not make the page count wrong.
func TestSeriesPagerDedupes(t *testing.T) {
	overlapping := func(_ context.Context, sourcePage int) ([]theme.SeriesStub, error) {
		if sourcePage > 3 {
			return nil, nil
		}
		// Pages of 4 that step by 2, so half of each page is a repeat.
		out := make([]theme.SeriesStub, 0, 4)
		for i := 0; i < 4; i++ {
			n := (sourcePage-1)*2 + i
			out = append(out, theme.SeriesStub{ID: fmt.Sprintf("s%d", n)})
		}
		return out, nil
	}

	p := newSeriesPager()
	res, err := p.Page(context.Background(), 1, 9, overlapping)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !sameIDs(res.Items, []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7"}) {
		t.Fatalf("items = %v, want each series once", ids(res.Items))
	}
	if res.TotalPages != 1 || res.HasMore {
		t.Fatalf("totalPages = %d hasMore = %v, want 1 and false", res.TotalPages, res.HasMore)
	}
}

// TestPagerForReplacesADifferentListing: one listing is cached, not every
// listing ever opened.
func TestPagerForReplacesADifferentListing(t *testing.T) {
	s := &Service{}

	browse := s.pagerFor(pagerKey{sourceID: "a"})
	if again := s.pagerFor(pagerKey{sourceID: "a"}); again != browse {
		t.Fatal("asking for the same listing twice built a second pager")
	}
	search := s.pagerFor(pagerKey{sourceID: "a", query: "lantern"})
	if search == browse {
		t.Fatal("a different query reused the listing's cache")
	}
	if back := s.pagerFor(pagerKey{sourceID: "a"}); back == search {
		t.Fatal("returning to the listing reused the search's cache")
	}

	s.dropPagers()
	if s.pager != nil {
		t.Fatal("dropPagers left a pager behind")
	}
}
