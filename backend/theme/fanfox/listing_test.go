package fanfox_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/fanfox"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestListingsNamesTheSortStatusAndGenreEntries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
		theme.GenreListing("action", "Action"),
		theme.GenreListing("slice-of-life", "Slice of Life"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Listings() = %+v, want %+v", got, want)
	}
}

// TestDefaultListingIsPopular pins this file's own doc comment:
// KeyBrowseOrder's own default is "popular", so Search("") — absent a source
// override — is the same request as ListingPopular, not ListingLatest's
// "most recently updated" meaning.
func TestDefaultListingIsPopular(t *testing.T) {
	th := fanfox.NewWithClock(nil, clock)
	if got := th.DefaultListing(); got != theme.ListingPopular {
		t.Fatalf("DefaultListing() = %q, want %q", got, theme.ListingPopular)
	}
}

func TestListingLatestDelegatesToEmptyQuerySearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.List(context.Background(), site(), theme.ListingLatest, 1)
	if err != nil {
		t.Fatal(err)
	}
	want, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List(ListingLatest) = %+v, want the same as Search(\"\") = %+v", got, want)
	}
}

func TestListingPopularReadsTheBareDirectory(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.List(context.Background(), site(), theme.ListingPopular, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
}

func TestListingNewUsesTheValuelessNewsParameter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/2.html?news": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	if _, err := th.List(context.Background(), site(), theme.ListingNew, 2); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if !strings.HasSuffix(calls[0].URL, "/directory/2.html?news") {
		t.Errorf("asked for %q; want the page as a file and the sort with no value", calls[0].URL)
	}
}

func TestListingRatingUsesTheValuelessRatingParameter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/?rating": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	if _, err := th.List(context.Background(), site(), theme.ListingRating, 1); err != nil {
		t.Fatal(err)
	}
}

func TestListingCompletedReadsTheStatusFilterPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/completed/": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	if _, err := th.List(context.Background(), site(), theme.ListingCompleted, 1); err != nil {
		t.Fatal(err)
	}
}

func TestListingGenreReadsTheGenreDirectoryPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/action/3.html": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.List(context.Background(), site(), "genre:action", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
}

func TestListingUnknownIDErrors(t *testing.T) {
	th := fanfox.NewWithClock(themetest.New(t, nil), clock)

	if _, err := th.List(context.Background(), site(), "not-a-real-listing", 1); err == nil {
		t.Fatal("List returned no error for an unknown listing id")
	}
}
