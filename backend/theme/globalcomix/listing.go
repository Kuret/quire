package globalcomix

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/rickl/quire/backend/theme"
)

// This file implements theme.Lister over the same /v1/search/query endpoint
// Search uses — GlobalComix has no separate browse endpoint (see
// globalcomix.go's package doc) — plus one more call, /v1/app/init, which is
// where the site publishes its own sort-order and genre vocabularies as data.
//
// # What was tried and what actually works, confirmed live 2026-09-24
//
// /v1/search/query with an empty q ignores most parameter names a client
// might reasonably guess: order, order_by, sort_by, sort_field, status,
// comic_status_id, genre, genre_id, category, category_id and comic_genre_id's
// near-misses all left the 24,553-series result set and its first page
// (headed by "Absolute Batman (2024-)") completely unchanged. Two parameter
// names do work, verified by both the result set and its reported total
// changing:
//
//   - sort — takes one of the ids /v1/app/init's comic_browse_sort_orders
//     names (confirmed live: "popular", "featured", "recent", "new", "views"
//     each produced a different top page). Of these, "popular" is
//     ListingPopular and "new" is ListingNew. "featured" and "views" have no
//     well-known Lister ID to answer to — "featured" is an editorial pick,
//     not one of PLAN §2's sort meanings, and "views" would only duplicate
//     ListingPopular under another name — so neither is offered. "recent" is
//     also not offered: it is not the empty-query default (see below) and
//     PLAN §2 forbids a listing whose label promises "Latest updates" but
//     whose page is a different request than what Browse already reads.
//   - comic_genre_id — confirmed live: filtering by id 20 ("Abstract")
//     dropped the total from 24,553 to 113 and returned a plausible,
//     abstract-tagged set. This backs every genre listing.
//   - comic_status — confirmed live: filtering by id 3, /v1/app/init's
//     comic_statuses id for "Finished", dropped the total to 948. This backs
//     ListingCompleted. (comic_status_id, the name that would match the other
//     two filters' "_id" pattern, does nothing; comic_status, without it,
//     does — apparently which parameter names are registered as filters on
//     this API is not fully predictable from the field's own name, which is
//     exactly why this comment records what was actually tried rather than
//     asserting a pattern.)
//
// No sort id answers "highest rated first", so ListingRating is not offered:
// GlobalComix has ratings (Series could read one if this theme wanted to
// display it) but no way to browse by them.
//
// ListingLatest is not offered either. Search("") — today's default Browse —
// sends no sort parameter at all, and its result already matches sort=popular
// rather than sort=recent or sort=new; adding an explicit "Latest updates"
// entry pointed at a different query than what Browse already returns would
// be exactly the false claim PLAN §2 rules out. Lister's own doc comment
// allows the well-known ID to be omitted, and List(ListingLatest) still
// delegates to Search("") below for API callers that ask for it directly.
//
// # Genres are the one thing fetched, not hard-coded
//
// Unlike the sort ids and the "Finished" status id — a small, stable
// vocabulary the API defines once, verified above — the genre catalogue is
// content GlobalComix can add to, so Listings fetches /v1/app/init and reads
// comic_genres from it live rather than shipping a list that goes stale. The
// numeric id is what actually filters (see comicGenre's doc comment in
// api.go), so it is what GenreListing's id names; there is no separate
// human-readable slug this theme trusts more than the field the filter itself
// uses.

// The sort ids /v1/app/init's comic_browse_sort_orders table names, confirmed
// live to change /v1/search/query's results (see this file's doc comment).
const (
	sortPopular = "popular"
	sortNew     = "new"
)

// completedStatusID is /v1/app/init's comic_statuses id for "Finished",
// confirmed live on 2026-09-24 to filter /v1/search/query's comic_status
// parameter down from 24,553 to 948 series. A fixed id rather than looked up
// per call: GlobalComix's status vocabulary is a small, stable enum, unlike
// the genre catalogue Listings fetches fresh below — re-resolving this one
// value on every List(ListingCompleted) call would cost a request for
// something not expected to change.
const completedStatusID = "3"

// Listings implements theme.Lister.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	out := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}

	init, err := getJSON[appInit](ctx, t, t.f, s, t.endpoint(s, "/v1/app/init", nil))
	if err != nil {
		return nil, err
	}
	for _, g := range init.ComicGenres {
		if g.IsActive == 0 || g.ID == 0 || g.Name == "" {
			continue
		}
		out = append(out, theme.GenreListing(strconv.Itoa(g.ID), g.Name))
	}
	return out, nil
}

// List implements theme.Lister.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	switch listingID {
	case theme.ListingLatest:
		// Today's Browse, delegated exactly (theme.Lister's doc comment): no
		// sort parameter, exactly what Search("") already sends.
		return t.Search(ctx, s, "", page)
	case theme.ListingPopular:
		return t.searchSeries(ctx, s, "", page, url.Values{"sort": {sortPopular}})
	case theme.ListingNew:
		return t.searchSeries(ctx, s, "", page, url.Values{"sort": {sortNew}})
	case theme.ListingCompleted:
		return t.searchSeries(ctx, s, "", page, url.Values{"comic_status": {completedStatusID}})
	}
	if genreID, ok := theme.GenreID(listingID); ok {
		return t.searchSeries(ctx, s, "", page, url.Values{"comic_genre_id": {genreID}})
	}
	return nil, fmt.Errorf("%s: source %q: unknown listing %q", ID, s.ID, listingID)
}
