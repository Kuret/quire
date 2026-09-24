package weebcentral_test

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
	"github.com/rickl/quire/backend/theme/weebcentral"
)

func lastListingQuery(t *testing.T, f *themetest.Fetcher) url.Values {
	t.Helper()
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("no requests were made")
	}
	u, err := url.Parse(calls[len(calls)-1].URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestListings(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search": {File: "genres.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
		// "Action" appears twice in the fixture (mobile and desktop pickers)
		// and must be de-duplicated; "Gender Bender" keeps its embedded space.
		theme.GenreListing("Action", "Action"),
		theme.GenreListing("Gender Bender", "Gender Bender"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Listings() =\n %+v\nwant\n %+v", got, want)
	}
}

func TestListPopularSendsSort(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingPopular, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastListingQuery(t, f).Get("sort"); got != "Popularity" {
		t.Errorf("sort = %q, want %q", got, "Popularity")
	}
}

func TestListNewSendsSort(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingNew, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastListingQuery(t, f).Get("sort"); got != "Recently Added" {
		t.Errorf("sort = %q, want %q", got, "Recently Added")
	}
}

func TestListCompletedSendsIncludedStatus(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingCompleted, 1); err != nil {
		t.Fatal(err)
	}
	q := lastListingQuery(t, f)
	if got := q.Get("included_status"); got != "Complete" {
		t.Errorf("included_status = %q, want %q", got, "Complete")
	}
	// Completed still browses, and Browse's own sort preference applies —
	// it is a filter on top of a listing, not a listing with no order at all.
	if got := q.Get("sort"); got != "Popularity" {
		t.Errorf("sort = %q, want the browseSort default %q", got, "Popularity")
	}
}

func TestListGenreSendsIncludedTag(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), "genre:Gender Bender", 1); err != nil {
		t.Fatal(err)
	}
	q := lastListingQuery(t, f)
	if got := q.Get("included_tag"); got != "Gender Bender" {
		t.Errorf("included_tag = %q, want %q", got, "Gender Bender")
	}
}

// theme.Lister's doc: List(ListingLatest) must equal what Search(q="")
// returns today. It is implemented by delegating, and this pins that.
func TestListLatestMatchesSearchEmptyQuery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

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
	th := weebcentral.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), "bogus", 1); err == nil {
		t.Error("List(\"bogus\") = nil error, want one — an unknown listing must never fall back to another")
	}
}
