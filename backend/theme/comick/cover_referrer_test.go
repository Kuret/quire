package comick_test

// The `Referer` this site's cover host requires.
//
// Measured 2026-09-16: the cover CDN answers 403 with no `Referer` and 200
// `image/webp` with one, across cdn1 and cdn2. PLAN §7.6 permits a truthful
// one and forbids every other kind, so these assert the *exact* URL the theme
// fetched rather than merely something plausible.

import (
	"context"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/comick"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.6: the referrer must be the page the cover URL was genuinely
// extracted from. For a listing that is the listing request itself, and these
// assert the exact URL the theme fetched rather than merely "something
// plausible" — a constant or a guessed page is what §7.6 forbids.
func TestSearchCoversNameTheListingTheyCameFrom(t *testing.T) {
	const listing = "/api/search?q=lantern&type=comic"
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + listing: jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "lantern", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.invalid" + listing
	for _, s := range got {
		if s.CoverReferrer != want {
			t.Errorf("%s: cover referrer = %q, want the listing actually fetched, %q", s.ID, s.CoverReferrer, want)
		}
	}
}

func TestSeriesCoverNamesTheSeriesPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.invalid" + seriesID
	if got.CoverReferrer != want {
		t.Errorf("cover referrer = %q, want the series page actually fetched, %q", got.CoverReferrer, want)
	}
}

// A stub with no cover names no page. A referrer attached to nothing is not
// useful and would be a claim about a fetch that never happens.
func TestNoCoverMeansNoReferrer(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.CoverURL == "" && s.CoverReferrer != "" {
			t.Errorf("%s: no cover but a referrer %q", s.ID, s.CoverReferrer)
		}
	}
}

// The referrer must be a URL fetch.PageReferrer accepts, or it is a theme bug
// that sends no header at all rather than a broken one.
func TestCoverReferrerIsUsable(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
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
