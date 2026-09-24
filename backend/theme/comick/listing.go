package comick

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/rickl/quire/backend/theme"
)

// Package theme.Lister for this application's own frontend backend. Every
// query parameter below is a fact read out of its own bundled JS
// (search-EyXSzVzj.js, meta_data-DpHBHWQp.js) and confirmed live against
// comick.art on 2026-09-24 — never guessed, because the endpoint is
// undocumented and guessing at a parameter's semantics is exactly the mistake
// KeyMaxContentRating's own doc comment already warns against.
//
//	Popular   order_by=user_follow_count
//	Rating    order_by=rating
//	New       order_by=created_at        (the site's own "Latest" tab)
//	Completed status=2                   (the site's own numeric vocabulary)
//	Genre     genres[]={slug}, from GET /api/metadata's genres whose
//	          group is "Genre"
//
// Verified live: each order_by and status value was checked against
// /api/search and produces a visibly different, correctly-ordered or
// correctly-filtered result set (e.g. status=2 drops every series whose
// status is not 2; order_by=rating surfaces 10/10-rated series that do not
// appear near the top of the default order).
//
// # What is not offered, and why
//
// The site also has an order_by=uploaded, labelled "Last updated" in its own
// UI — genuinely a different, real ordering (by last chapter upload, not by
// series creation date). It has no home in theme.Lister's well-known IDs
// here: it is not ListingNew (that is "added to the site", which
// order_by=created_at already answers honestly), and it cannot be
// ListingLatest, because theme.Lister requires List(ListingLatest) to equal
// Search(q="") exactly, and Search sends no order_by at all — landing on the
// API's own default, which is order_by=created_at, not uploaded (confirmed
// live: an unordered /api/search and one with order_by=created_at return the
// same series in the same order). Exposing "Last updated" under either name
// would answer a listing with a page that is not what its label promises, so
// it is left out rather than misrepresented.
//
// ListingNew and the default Browse therefore render the same series in the
// same order today — both are, honestly, order_by=created_at. That is a
// coincidence of what this site's default happens to be, not two listings
// pretending to be different; see above for why "New" is still worth naming
// even though it currently agrees with Browse.
const (
	orderByFollowCount = "user_follow_count"
	orderByRating      = "rating"
	orderByCreatedAt   = "created_at"

	// statusCompleted is comick's own status vocabulary: 1 ongoing, 2
	// completed, 3 hiatus, 4 cancelled (docs/THEME-NOTES.md, confirmed live).
	statusCompleted = "2"

	// genreGroup is the /api/metadata group name for what Quire calls a
	// genre, as opposed to "Theme", "Format" or "Content".
	genreGroup = "Genre"
)

// metadataGenre is one entry of GET /api/metadata's genres array.
type metadataGenre struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Group string `json:"group"`
}

// metadataResponse is the shape GET /api/metadata answers. Only genres is
// read; tags, demographics and the rest are the site's own advanced-search
// facets and have no well-known theme.Listing to become.
type metadataResponse struct {
	// A to-many relation on an undocumented backend that has already been
	// seen sending one as a keyed object instead of an array (shapes.go) —
	// there is no reason to trust this one arrives any more disciplined.
	Genres list[metadataGenre] `json:"genres"`
}

var _ theme.Lister = (*Theme)(nil)
var _ theme.DefaultLister = (*Theme)(nil)

// DefaultListing implements theme.DefaultLister: Search("") sends no order_by
// at all and lands on the API's own default, order_by=created_at — the same
// ordering as ListingNew, not ListingLatest's "most recently updated"
// meaning (see this file's doc comment) — so the browse screen's
// always-first entry should say "Newly added", not "Latest updates".
func (t *Theme) DefaultListing() string { return theme.ListingNew }

// Listings implements theme.Lister.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	out := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}

	raw, err := t.get(ctx, s, "/api/metadata")
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: genres: %w", ID, s.ID, err)
	}
	var meta metadataResponse
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("%s: source %q: genres: parse JSON: %w", ID, s.ID, err)
	}

	type genre struct{ id, name string }
	var genres []genre
	for _, g := range meta.Genres {
		if !strings.EqualFold(g.Group, genreGroup) {
			continue
		}
		slug := strings.TrimSpace(g.Slug)
		name := strings.TrimSpace(g.Name)
		if slug == "" || name == "" {
			continue
		}
		genres = append(genres, genre{id: slug, name: name})
	}
	// The API is observed to answer genres already sorted by name, but
	// sorting here rather than trusting that keeps the result — and the test
	// pinning it — independent of a fact about someone else's server that
	// could change without notice.
	sort.Slice(genres, func(i, j int) bool {
		return strings.ToLower(genres[i].name) < strings.ToLower(genres[j].name)
	})
	for _, g := range genres {
		out = append(out, theme.GenreListing(g.id, g.name))
	}
	return out, nil
}

// List implements theme.Lister.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	switch listingID {
	case "", theme.ListingLatest:
		// theme.Lister's doc: List(ListingLatest) must equal Search(q="")
		// exactly, implemented by delegating.
		return t.Search(ctx, s, "", page)
	case theme.ListingPopular:
		return t.searchComics(ctx, s, page, func(qs url.Values) {
			qs.Set("order_by", orderByFollowCount)
		})
	case theme.ListingNew:
		return t.searchComics(ctx, s, page, func(qs url.Values) {
			qs.Set("order_by", orderByCreatedAt)
		})
	case theme.ListingRating:
		return t.searchComics(ctx, s, page, func(qs url.Values) {
			qs.Set("order_by", orderByRating)
		})
	case theme.ListingCompleted:
		return t.searchComics(ctx, s, page, func(qs url.Values) {
			qs.Set("status", statusCompleted)
		})
	}
	if slug, ok := theme.GenreID(listingID); ok && slug != "" {
		return t.searchComics(ctx, s, page, func(qs url.Values) {
			qs.Add("genres[]", slug)
		})
	}
	return nil, fmt.Errorf("%s: source %q: unknown listing %q", ID, s.ID, listingID)
}
