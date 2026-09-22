package generic_test

// PLAN §7.6: the referrer must be the page the cover URL was genuinely
// extracted from. TestSearchFromSelectorsAlone and
// TestSeriesAndChaptersFromSelectorsAlone already assert the *exact* URL
// fetched; this file adds the two guarantees those do not check: that a
// result with no cover acquires no referrer, and that the header is one
// fetch.PageReferrer will actually accept.

import (
	"context"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/themetest"
)

// A source with no searchCover selector configured extracts no cover at all,
// and must not acquire a referrer naming a page nothing was fetched for.
func TestNoCoverMeansNoReferrer(t *testing.T) {
	src := selectorSource()
	delete(src.Selectors, generic.SearchCover)

	f := themetest.New(t, map[string]themetest.Route{
		"GET /search": {File: "listing.html"},
	})
	th := generic.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), src, "lantern keeper", 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.CoverURL != "" {
			t.Fatalf("test setup broken: result %+v still has a cover", s)
		}
		if s.CoverReferrer != "" {
			t.Errorf("%s: no cover but a referrer %q", s.ID, s.CoverReferrer)
		}
	}
}

// The referrer must be a URL fetch.PageReferrer accepts, or it is a theme bug
// that sends no header at all rather than a broken one.
func TestCoverReferrerIsUsable(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /read/the-lantern-keeper": {File: "listing.html"},
	})
	th := generic.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), selectorSource(), "the-lantern-keeper")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := theme.CoverRefererFrom(got.CoverReferrer)
	if err != nil {
		t.Fatalf("the theme named a referrer the fetch layer refuses: %v", err)
	}
	if ref.IsZero() {
		t.Fatal("a cover with a page behind it must yield a referrer, not the zero value")
	}
}
