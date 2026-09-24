package generic_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/themetest"
)

// listingSource is selectorSource with every browse-listing key set, so a
// test can exercise sort, status and genre listings all at once.
func listingSource() *theme.Source {
	src := selectorSource()
	src.Selectors[generic.ListingPopular] = "/popular?page={page}"
	src.Selectors[generic.ListingNew] = "/new?page={page}"
	src.Selectors[generic.ListingRating] = "/rating?page={page}"
	src.Selectors[generic.ListingCompleted] = "/completed?page={page}"
	src.Selectors[generic.GenreListPath] = "/genres"
	src.Selectors[generic.GenreLinkSelector] = ".genre-index a.genre-link"
	src.Selectors[generic.GenrePath] = "/genre/{genre}?page={page}"
	return src
}

func listingRoutes() map[string]themetest.Route {
	return map[string]themetest.Route{
		"GET /popular":             {File: "listing.html"},
		"GET /new":                 {File: "listing.html"},
		"GET /rating":              {File: "listing.html"},
		"GET /completed":           {File: "listing.html"},
		"GET /genres":              {File: "genres.html"},
		"GET /genre/fantasy":       {File: "listing.html"},
		"GET /genre/slice-of-life": {File: "listing.html"},
		"GET /genre/romance":       {File: "listing.html"},
	}
}

// TestListingsOffersOnlyWhatIsConfigured pins the closed-vocabulary rule at
// the Lister level: a source with none of these selectors set offers nothing
// of its own (the caller adds "latest" itself — that is service package
// territory, not this theme's).
func TestListingsOffersOnlyWhatIsConfigured(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := generic.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), selectorSource())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Listings() = %+v, want none — this source configured no listing selectors", got)
	}
}

// TestListingsOrdersSortStatusGenre pins the ordering this theme owes the
// caller: sort listings, in the order they were checked, then status, then
// genres in the site's own page order (not resorted).
func TestListingsOrdersSortStatusGenre(t *testing.T) {
	f := themetest.New(t, listingRoutes())
	th := generic.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), listingSource())
	if err != nil {
		t.Fatal(err)
	}

	var ids, groups []string
	for _, l := range got {
		ids = append(ids, l.ID)
		groups = append(groups, l.Group)
	}
	wantIDs := []string{
		theme.ListingPopular, theme.ListingNew, theme.ListingRating,
		theme.ListingCompleted,
		"genre:fantasy", "genre:slice-of-life", "genre:romance",
	}
	if strings.Join(ids, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("ids = %v, want %v", ids, wantIDs)
	}
	wantGroups := []string{
		theme.ListingGroupSort, theme.ListingGroupSort, theme.ListingGroupSort,
		theme.ListingGroupStatus,
		theme.ListingGroupGenre, theme.ListingGroupGenre, theme.ListingGroupGenre,
	}
	if strings.Join(groups, ",") != strings.Join(wantGroups, ",") {
		t.Fatalf("groups = %v, want %v", groups, wantGroups)
	}

	for _, l := range got {
		switch l.ID {
		case "genre:fantasy":
			if l.Label != "Fantasy" {
				t.Errorf("fantasy label = %q", l.Label)
			}
		case "genre:slice-of-life":
			if l.Label != "Slice of Life" {
				t.Errorf("slice-of-life label = %q", l.Label)
			}
		case "genre:romance":
			// The href carried "?ref=nav" — proof the query string never
			// leaks into the slug used as the genre's ID.
			if l.Label != "Romance" {
				t.Errorf("romance label = %q", l.Label)
			}
		}
	}
}

// TestListingsSurvivesAGenrePageFailure is generic's half of "a failed
// Listings() call degrades" — except here the theme itself absorbs a broken
// genre page rather than failing the whole call, because the sort and status
// listings above it are still true.
func TestListingsSurvivesAGenrePageFailure(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /popular": {File: "listing.html"},
		"GET /genres":  {Status: 403, Body: "blocked"},
	})
	th := generic.NewWithClock(f, clock)

	src := selectorSource()
	src.Selectors[generic.ListingPopular] = "/popular?page={page}"
	src.Selectors[generic.GenreListPath] = "/genres"
	src.Selectors[generic.GenreLinkSelector] = ".genre-index a.genre-link"
	src.Selectors[generic.GenrePath] = "/genre/{genre}?page={page}"

	got, err := th.Listings(context.Background(), src)
	if err != nil {
		t.Fatalf("Listings() = %v, want nil error — the genre page's failure must not sink the sort listings", err)
	}
	if len(got) != 1 || got[0].ID != theme.ListingPopular {
		t.Fatalf("Listings() = %+v, want just popular", got)
	}
}

// TestListPagesEachSortListing pins that List() templates {page} into the
// right selector for each well-known ID, and parses it exactly like Search.
func TestListPagesEachSortListing(t *testing.T) {
	f := themetest.New(t, listingRoutes())
	th := generic.NewWithClock(f, clock)
	src := listingSource()

	for _, id := range []string{theme.ListingPopular, theme.ListingNew, theme.ListingRating, theme.ListingCompleted} {
		got, err := th.List(context.Background(), src, id, 1)
		if err != nil {
			t.Fatalf("List(%q) = %v", id, err)
		}
		if len(got) != 2 {
			t.Fatalf("List(%q) = %d results, want 2", id, len(got))
		}
	}
}

// TestListGenreUsesGenrePath pins genre listing paging: {genre} is the slug
// read off the genre index, {page} is templated the same way every other
// listing's is.
func TestListGenreUsesGenrePath(t *testing.T) {
	f := themetest.New(t, listingRoutes())
	th := generic.NewWithClock(f, clock)
	src := listingSource()

	got, err := th.List(context.Background(), src, "genre:fantasy", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("List(genre:fantasy) = %d results, want 2", len(got))
	}
	calls := f.Calls()
	found := false
	for _, c := range calls {
		if strings.Contains(c.URL, "/genre/fantasy") {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls = %+v, want a request to /genre/fantasy", calls)
	}
}

// TestListLatestDelegatesToSearch pins §7.5/theme.Lister's rule that
// List(ListingLatest) is exactly what Search("") returns — implemented here
// by delegating, not by a second code path that could drift from it.
func TestListLatestDelegatesToSearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search": {File: "listing.html"},
	})
	th := generic.NewWithClock(f, clock)
	src := selectorSource()

	viaList, err := th.List(context.Background(), src, theme.ListingLatest, 1)
	if err != nil {
		t.Fatal(err)
	}
	viaSearch, err := th.Search(context.Background(), src, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(viaList) != len(viaSearch) || len(viaList) == 0 {
		t.Fatalf("List(latest) = %+v, Search(\"\") = %+v — must match", viaList, viaSearch)
	}
	for i := range viaList {
		if !reflect.DeepEqual(viaList[i], viaSearch[i]) {
			t.Errorf("result %d differs: List=%+v Search=%+v", i, viaList[i], viaSearch[i])
		}
	}
}

// TestListUnknownOrUnconfiguredListingErrors is the "never a silent fall
// back" mutation check at this theme's boundary: an ID this source never
// named, and a well-known ID it did not configure, are both errors — never
// the default listing standing in for either.
func TestListUnknownOrUnconfiguredListingErrors(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := generic.NewWithClock(f, clock)

	t.Run("unknown listing id", func(t *testing.T) {
		_, err := th.List(context.Background(), selectorSource(), "genre:nonexistent", 1)
		if err == nil {
			t.Fatal("want an error: this source configured no genrePath at all")
		}
	})
	t.Run("well-known id, not configured", func(t *testing.T) {
		_, err := th.List(context.Background(), selectorSource(), theme.ListingPopular, 1)
		if err == nil {
			t.Fatal("want an error: this source never set listingPopular")
		}
	})
	t.Run("not a recognised id at all", func(t *testing.T) {
		_, err := th.List(context.Background(), listingSource(), "made-up-listing", 1)
		if err == nil {
			t.Fatal("want an error for a listing id this theme has never heard of")
		}
	})
}
