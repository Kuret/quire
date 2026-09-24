package webtoons_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
	"github.com/rickl/quire/backend/theme/webtoons"
)

func TestListings(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /en/genres": {File: "genres.html"},
	})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
		// The fixture repeats "drama" and must be de-duplicated; the label is
		// read verbatim from the tab's text, embedded space and all.
		theme.GenreListing("drama", "DRAMA"),
		theme.GenreListing("slice_of_life", "SLICE OF LIFE"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Listings() =\n %+v\nwant\n %+v", got, want)
	}
}

func TestListPopularAsksTheGenresListingSortedByPopularity(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /en/genres?sortOrder=MANA": {File: "search.html"},
	})
	th := webtoons.NewWithClock(f, clock)
	got, err := th.List(context.Background(), site(), theme.ListingPopular, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("got no results")
	}
}

func TestListCompletedAsksTheCompletedOriginalsListing(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /en/originals/complete?sortOrder=MANA": {File: "search.html"},
	})
	th := webtoons.NewWithClock(f, clock)
	got, err := th.List(context.Background(), site(), theme.ListingCompleted, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("got no results")
	}
}

func TestListGenreAsksTheGenreListing(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /en/genres/drama?sortOrder=MANA": {File: "search.html"},
	})
	th := webtoons.NewWithClock(f, clock)
	got, err := th.List(context.Background(), site(), "genre:drama", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("got no results")
	}
}

// Confirmed live 2026-09-24: neither the genre listing nor the completed
// listing honours a page parameter, so page 2 is an empty page rather than a
// request that would silently repeat page 1.
func TestListPageTwoIsEmptyWithoutARequest(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := webtoons.NewWithClock(f, clock)
	got, err := th.List(context.Background(), site(), theme.ListingPopular, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d results on page 2, want 0 and no request at all", len(got))
	}
}

// theme.Lister's doc: List(ListingLatest) must equal what Search(q="")
// returns today. It is implemented by delegating, and this pins that.
func TestListLatestMatchesSearchEmptyQuery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /en/search?keyword=": {File: "search.html"},
	})
	th := webtoons.NewWithClock(f, clock)

	viaListing, err := th.List(context.Background(), site(), theme.ListingLatest, 1)
	if err != nil {
		t.Fatal(err)
	}
	viaSearch, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viaListing, viaSearch) {
		t.Errorf("List(ListingLatest) =\n %+v\nSearch(\"\") =\n %+v", viaListing, viaSearch)
	}
}

func TestListUnknownListingErrors(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := webtoons.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), "bogus", 1); err == nil {
		t.Error("List(\"bogus\") = nil error, want one — an unknown listing must never fall back to another")
	}
}
