package mangadex_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestListings(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/tag": {File: "tags.json"},
	})
	th := mangadex.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
		// Only the two group=="genre" tags survive, sorted by name — "theme",
		// "format" and "content" tags in the fixture are dropped.
		theme.GenreListing("aaaaaaaa-0000-4000-8000-000000000002", "Comedy"),
		theme.GenreListing("aaaaaaaa-0000-4000-8000-000000000001", "Sci-Fi"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Listings() =\n %+v\nwant\n %+v", got, want)
	}
}

func TestListPopularSendsFollowedCountOrder(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingPopular, 1); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q.Get("order[followedCount]"); got != "desc" {
		t.Errorf("order[followedCount] = %q, want %q", got, "desc")
	}
}

func TestListNewSendsCreatedAtOrder(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingNew, 1); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q.Get("order[createdAt]"); got != "desc" {
		t.Errorf("order[createdAt] = %q, want %q", got, "desc")
	}
}

func TestListRatingSendsRatingOrder(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingRating, 1); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q.Get("order[rating]"); got != "desc" {
		t.Errorf("order[rating] = %q, want %q", got, "desc")
	}
}

func TestListCompletedSendsStatusFilter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingCompleted, 1); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q["status[]"]; len(got) != 1 || got[0] != "completed" {
		t.Errorf("status[] = %v, want [completed]", got)
	}
}

func TestListGenreSendsIncludedTags(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	const tagUUID = "aaaaaaaa-0000-4000-8000-000000000001"
	if _, err := th.List(context.Background(), site(), "genre:"+tagUUID, 1); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q["includedTags[]"]; len(got) != 1 || got[0] != tagUUID {
		t.Errorf("includedTags[] = %v, want [%s]", got, tagUUID)
	}
}

// theme.Lister's doc: List(ListingLatest) must equal what Search(q="")
// returns today. It is implemented by delegating, and this pins that.
func TestListLatestMatchesSearchEmptyQuery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)

	viaListing, err := th.List(context.Background(), site(), theme.ListingLatest, 2)
	if err != nil {
		t.Fatal(err)
	}
	viaSearch, err := th.Search(context.Background(), site(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viaListing, viaSearch) {
		t.Errorf("List(ListingLatest) =\n %+v\nSearch(\"\") =\n %+v", viaListing, viaSearch)
	}
}

func TestListUnknownListingErrors(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), "bogus", 1); err == nil {
		t.Error("List(\"bogus\") = nil error, want one — an unknown listing must never fall back to another")
	}
}
