package weebcentral

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/rickl/quire/backend/theme"
)

// Package theme.Lister for this site's own /search/data fragment endpoint.
// Every parameter below was read out of GET /search's advanced-search form
// and confirmed live against weebcentral.com on 2026-09-24 by checking the
// resulting result sets actually differ:
//
//	Popular   sort=Popularity           (the fragment's own enum, already
//	                                     KeyBrowseSort's default)
//	New       sort=Recently+Added
//	Completed included_status=Complete
//	Genre     included_tag={name}, read from GET /search's hidden
//	          "tag-*-value" inputs
//
// # What is not offered, and why
//
// The sort enum also has "Latest Updates" and "Alphabet". "Latest Updates" is
// not offered under any well-known ID: it is not ListingNew (that means
// "added to the site", which "Recently Added" already answers honestly), and
// it cannot be ListingLatest, because theme.Lister requires
// List(ListingLatest) to equal Search(q="") exactly, and Search's own default
// sort for an empty query is KeyBrowseSort's ("Popularity" unless the user
// changed it) — not "Latest Updates". Offering it under either name would
// answer a listing with a page that is not what its label promises.
//
// There is no "Rating" entry in the site's own sort enum at all — confirmed
// against the live advanced-search form, which offers exactly Popularity,
// Latest Updates, Recently Added, Alphabet and Best Match — so
// theme.ListingRating is not offered: the site has nothing to answer it with.
// "Alphabet" has no well-known ID of its own and is left out for the same
// reason.
const (
	sortPopularity    = "Popularity"
	sortRecentlyAdded = "Recently Added"

	// statusComplete is the value the site's own <option> sends for a
	// finished series (confirmed live: distinct from "Ongoing", "Hiatus" and
	// "Canceled").
	statusComplete = "Complete"
)

var _ theme.Lister = (*Theme)(nil)

// Listings implements theme.Lister.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	out := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}

	genres, err := t.genreListings(ctx, s)
	if err != nil {
		return nil, err
	}
	return append(out, genres...), nil
}

// genreListings reads the site's own tag list off GET /search.
//
// Each tag in the advanced-search form is a visible checkbox plus a *hidden*
// sibling input whose id ends "-value" and whose value is the tag's display
// name — the exact string the fragment endpoint's included_tag parameter
// wants back verbatim, spaces and all ("Gender Bender"). That hidden input,
// not the checkbox's own slugified id, is what this reads.
func (t *Theme) genreListings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	doc, err := t.doc(ctx, s, "/search")
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: genres: %w", ID, s.ID, err)
	}

	var names []string
	seen := make(map[string]bool)
	doc.Find(`input[id$="-value"]`).Each(func(_ int, sel *goquery.Selection) {
		name := strings.TrimSpace(sel.AttrOr("value", ""))
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	})
	sort.Strings(names)

	out := make([]theme.Listing, 0, len(names))
	for _, name := range names {
		out = append(out, theme.GenreListing(name, name))
	}
	return out, nil
}

// List implements theme.Lister.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	if listingID == "" || listingID == theme.ListingLatest {
		// theme.Lister's doc: List(ListingLatest) must equal Search(q="")
		// exactly, implemented by delegating.
		return t.Search(ctx, s, "", page)
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	switch listingID {
	case theme.ListingPopular:
		return t.searchFragment(ctx, s, o, page, sortPopularity, "", "", "")
	case theme.ListingNew:
		return t.searchFragment(ctx, s, o, page, sortRecentlyAdded, "", "", "")
	case theme.ListingCompleted:
		return t.searchFragment(ctx, s, o, page, o.String(KeyBrowseSort), "", statusComplete, "")
	}
	if name, ok := theme.GenreID(listingID); ok && name != "" {
		return t.searchFragment(ctx, s, o, page, o.String(KeyBrowseSort), "", "", name)
	}
	return nil, fmt.Errorf("%s: source %q: unknown listing %q", ID, s.ID, listingID)
}
