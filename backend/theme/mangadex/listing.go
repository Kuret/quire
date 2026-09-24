package mangadex

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/rickl/quire/backend/theme"
)

// Package theme.Lister for MangaDex. Every listing below is a fact checked
// live against https://api.mangadex.org on 2026-09-24, not a guess at the
// API's shape:
//
//	Popular   order[followedCount]=desc
//	New       order[createdAt]=desc
//	Rating    order[rating]=desc
//	Completed status[]=completed
//	Genre     includedTags[]={tag uuid}, tags whose group is "genre"
//
// ListingLatest is not named: PLAN's Lister doc says it may be omitted
// because the default Browse already is that listing, and List still answers
// it correctly below by delegating to Search(q="") exactly as today.
//
// What is missing, and why: MangaDex has no separate "most viewed this week"
// or similar facet beyond followedCount/rating/createdAt, so there is nothing
// else honest to offer. The 38 "theme", 12 "format" and 2 "content" tag
// groups are real facets on the site too, but ListingGroupGenre is offered
// for the group the site itself calls "genre" — the other groups are what
// Series.Genres already surfaces per-series, and inventing three more browse
// facets nobody asked the site for would be adding a listing "genre" was
// never meant to describe.

// tagAttrsGroup is the group name MangaDex uses for what Quire calls a genre.
const tagGroupGenre = "genre"

var _ theme.Lister = (*Theme)(nil)

// Listings implements theme.Lister.
//
// It fetches /manga/tag, which is the only listing here that needs a request:
// the four order-based listings and Completed are facts about the API's query
// parameters, not about anything a page could disagree with. The caller
// caches the result per source for 24h (theme.Lister's doc), so tags are not
// re-fetched on every browse-screen open.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	out := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}

	var col collection
	if err := t.getJSON(ctx, s, t.endpoint(s, "/manga/tag", nil), &col); err != nil {
		return nil, fmt.Errorf("%s: source %q: genres: %w", ID, s.ID, err)
	}

	type genre struct{ id, name string }
	var genres []genre
	for _, e := range col.Data {
		var a tagAttributes
		if unmarshal(e.Attributes, &a) != nil {
			continue
		}
		if !strings.EqualFold(a.Group, tagGroupGenre) {
			continue
		}
		name := pickAny(a.Name)
		if e.ID == "" || name == "" {
			continue
		}
		genres = append(genres, genre{id: e.ID, name: name})
	}
	// The API does not document an order for /manga/tag and none was observed
	// live; sorting by name is what makes the picker usable and the test
	// result independent of whatever order the server happens to answer in.
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
		// exactly, implemented by delegating rather than by guessing which
		// order parameter would reproduce it.
		return t.Search(ctx, s, "", page)
	case theme.ListingPopular:
		return t.searchManga(ctx, s, page, func(v url.Values) {
			v.Set("order[followedCount]", "desc")
		})
	case theme.ListingNew:
		return t.searchManga(ctx, s, page, func(v url.Values) {
			v.Set("order[createdAt]", "desc")
		})
	case theme.ListingRating:
		return t.searchManga(ctx, s, page, func(v url.Values) {
			v.Set("order[rating]", "desc")
		})
	case theme.ListingCompleted:
		return t.searchManga(ctx, s, page, func(v url.Values) {
			v.Add("status[]", "completed")
		})
	}
	if tagID, ok := theme.GenreID(listingID); ok && tagID != "" {
		return t.searchManga(ctx, s, page, func(v url.Values) {
			v.Add("includedTags[]", tagID)
		})
	}
	return nil, fmt.Errorf("%s: source %q: unknown listing %q", ID, s.ID, listingID)
}
