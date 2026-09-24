package comick_test

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/comick"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestListings(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/metadata": jsonRoute("metadata.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
		// Only the two group=="Genre" entries survive, sorted by name — the
		// fixture's Theme and Format entries are dropped.
		theme.GenreListing("romance", "Romance"),
		theme.GenreListing("sci-fi", "Sci-fi"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Listings() =\n %+v\nwant\n %+v", got, want)
	}
}

func lastCallQuery(t *testing.T, f *themetest.Fetcher) url.Values {
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

func TestListPopularSendsOrderBy(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingPopular, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastCallQuery(t, f).Get("order_by"); got != "user_follow_count" {
		t.Errorf("order_by = %q, want %q", got, "user_follow_count")
	}
}

func TestListNewSendsOrderBy(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingNew, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastCallQuery(t, f).Get("order_by"); got != "created_at" {
		t.Errorf("order_by = %q, want %q", got, "created_at")
	}
}

func TestListRatingSendsOrderBy(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingRating, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastCallQuery(t, f).Get("order_by"); got != "rating" {
		t.Errorf("order_by = %q, want %q", got, "rating")
	}
}

func TestListCompletedSendsStatus(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), theme.ListingCompleted, 1); err != nil {
		t.Fatal(err)
	}
	if got := lastCallQuery(t, f).Get("status"); got != "2" {
		t.Errorf("status = %q, want %q (comick's own vocabulary: 2 == completed)", got, "2")
	}
}

func TestListGenreSendsGenresParam(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), "genre:romance", 1); err != nil {
		t.Fatal(err)
	}
	q := lastCallQuery(t, f)
	if got := q["genres[]"]; len(got) != 1 || got[0] != "romance" {
		t.Errorf("genres[] = %v, want [romance]", got)
	}
}

// theme.Lister's doc: List(ListingLatest) must equal what Search(q="")
// returns today. It is implemented by delegating, and this pins that.
func TestListLatestMatchesSearchEmptyQuery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)

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

// TestListingsGenresParseEitherCardinality covers PLAN §9's durable half of
// the shapes.go fix (see its own doc comment): /api/metadata's genres is a
// to-many relation on the same undocumented backend that has already been
// seen sending one as a keyed object instead of an array, and there is no
// reason to trust this field is any more disciplined.
func TestListingsGenresParseEitherCardinality(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/metadata": jsonRoute("metadata-single.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatalf("a genres object keyed by position must parse: %v", err)
	}
	want := append([]theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}, theme.GenreListing("romance", "Romance"))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Listings() =\n %+v\nwant\n %+v", got, want)
	}
}

func TestListUnknownListingErrors(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := comick.NewWithClock(f, clock)
	if _, err := th.List(context.Background(), site(), "bogus", 1); err == nil {
		t.Error("List(\"bogus\") = nil error, want one — an unknown listing must never fall back to another")
	}
}
