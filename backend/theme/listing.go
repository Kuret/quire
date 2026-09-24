package theme

import (
	"context"
	"strings"
)

// Lister is implemented by a theme whose site can be browsed in more ways than
// its default Browse (a Search with an empty query, PLAN §7.5): by popularity,
// by what was added most recently, by rating, finished series only, or by
// genre.
//
// It is a side interface for the same reason FirstPageProber and FileTheme
// are: plenty of sites offer none of this, a book source like Shelfmark needs a
// query to answer anything, and a theme with nothing to add is not making a
// false claim by omission — the browse screen then offers the one listing it
// always had.
//
// A theme lists only what its site actually serves. Offering "Top rated" and
// then answering it with the latest-updates page would be the screen saying
// something that is not true (PLAN §2).
type Lister interface {
	// Listings names the ways this source can be browsed, in the order the
	// screen should offer them within each group. Sort listings use the
	// well-known IDs below so every source words them the same way;
	// GenreListing builds the genre ones. It may fetch (a site's genre list
	// lives on a page) — the caller caches the answer per source.
	//
	// ListingLatest may be omitted: the default Browse already is that
	// listing, and the caller offers it first whether or not it is named.
	Listings(ctx context.Context, s *Source) ([]Listing, error)

	// List returns one page (1-based) of a listing Listings named, through the
	// source's policy exactly as Search does. An ID Listings did not name is
	// an error, never a silent fall back to another listing.
	List(ctx context.Context, s *Source, listingID string, page int) ([]SeriesStub, error)
}

// Listing is one way to browse a source.
type Listing struct {
	// ID is ListingLatest and friends for a sort or status listing, or
	// "genre:" plus the theme's own identifier for a genre (GenreListing).
	ID string `json:"id"`

	// Label is what the screen shows. For the well-known IDs the caller
	// replaces it with ListingLabel's wording, so a theme need not set it;
	// for a genre it is the site's own name for the genre.
	Label string `json:"label"`

	// Group is ListingGroupSort, ListingGroupStatus or ListingGroupGenre.
	Group string `json:"group"`
}

// The well-known listing IDs.
const (
	ListingLatest    = "latest"    // most recently updated first: today's Browse
	ListingPopular   = "popular"   // most read / most viewed first
	ListingNew       = "new"       // most recently added to the site first
	ListingRating    = "rating"    // highest rated first
	ListingCompleted = "completed" // finished series only
)

// The listing groups, in the order the screen shows them.
const (
	ListingGroupSort   = "sort"
	ListingGroupStatus = "status"
	ListingGroupGenre  = "genre"
)

// listingLabels is the one wording for each well-known listing (PLAN §2): a
// source does not get to call its popular page "Hot" and another "Trending".
var listingLabels = map[string]string{
	ListingLatest:    "Latest updates",
	ListingPopular:   "Popular",
	ListingNew:       "Newly added",
	ListingRating:    "Top rated",
	ListingCompleted: "Completed",
}

// ListingLabel is the wording for a well-known listing ID, and "" for any other.
func ListingLabel(id string) string { return listingLabels[id] }

// genrePrefix marks a genre listing's ID.
const genrePrefix = "genre:"

// GenreListing is the Listing for one of a site's genres. id is the theme's
// own handle for it (a slug, a tag id); label is the site's name for it.
func GenreListing(id, label string) Listing {
	return Listing{ID: genrePrefix + id, Label: strings.TrimSpace(label), Group: ListingGroupGenre}
}

// GenreID returns the theme's own handle from a genre listing's ID, and
// whether id was a genre listing at all.
func GenreID(listingID string) (string, bool) {
	if !strings.HasPrefix(listingID, genrePrefix) {
		return "", false
	}
	g := strings.TrimPrefix(listingID, genrePrefix)
	return g, g != ""
}
