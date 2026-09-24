package globalcomix_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/globalcomix"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestListingsNamesSortStatusAndActiveGenres(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/app/init": {File: "app_init.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}
	want := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
		theme.GenreListing("20", "Abstract"),
		theme.GenreListing("1", "Action"),
		// "Retired Genre" (is_active: 0) must not appear.
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Listings() = %+v, want %+v", got, want)
	}
}

func TestListingLatestDelegatesToEmptyQuerySearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

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
	// Neither call may have sent a sort or filter parameter: this is the same
	// bare query Search("") already sends.
	q := lastQuery(t, f)
	for _, k := range []string{"sort", "comic_status", "comic_genre_id"} {
		if _, ok := q[k]; ok {
			t.Errorf("request carried %q, want the bare empty-query search", k)
		}
	}
}

func TestListingPopularSendsThePopularSort(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	if _, err := th.List(context.Background(), site(), theme.ListingPopular, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastQuery(t, f).Get("sort"); got != "popular" {
		t.Errorf("sort = %q, want %q", got, "popular")
	}
}

func TestListingNewSendsTheNewSort(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	if _, err := th.List(context.Background(), site(), theme.ListingNew, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastQuery(t, f).Get("sort"); got != "new" {
		t.Errorf("sort = %q, want %q", got, "new")
	}
}

func TestListingCompletedSendsTheFinishedStatusFilter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	if _, err := th.List(context.Background(), site(), theme.ListingCompleted, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastQuery(t, f).Get("comic_status"); got != "3" {
		t.Errorf("comic_status = %q, want %q", got, "3")
	}
}

func TestListingGenreSendsTheGenreFilter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	if _, err := th.List(context.Background(), site(), "genre:20", 2); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q.Get("comic_genre_id"); got != "20" {
		t.Errorf("comic_genre_id = %q, want %q", got, "20")
	}
	if got := q.Get("p"); got != "2" {
		t.Errorf("p = %q, want %q", got, "2")
	}
}

func TestListingUnknownIDErrors(t *testing.T) {
	th := globalcomix.NewWithClock(themetest.New(t, nil), clock)
	if _, err := th.List(context.Background(), site(), "not-a-real-listing", 1); err == nil {
		t.Fatal("List returned no error for an unknown listing id")
	}
}
