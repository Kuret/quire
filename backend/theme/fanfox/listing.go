package fanfox

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/theme"
)

// This file implements theme.Lister. The site's own "MANGA BROWSE" bar
// (verified live on fanfox.net and mangahere.cc on 2026-09-24) offers, beyond
// the popularity order Search("") already reads from the directory:
//
//   - a "News" sort (?news), which orders by when a series was *added* to the
//     site — confirmed by its results being unfamiliar low-numbered titles,
//     unlike ?latest's well-known series with a fresh chapter. That is
//     ListingNew, not ListingLatest: the two well-known IDs mean different
//     things and the site happens to have both.
//   - a "Rating" sort (?rating), highest rated first: ListingRating.
//   - a status filter for finished series (/directory/completed/):
//     ListingCompleted.
//   - a genre filter, one path segment per genre, listed on the directory
//     page itself (the "GENRES" block) rather than a separate page.
//
// Not offered:
//   - "Alphabetical" (?az) and the separate "Chapters" order some directory
//     revisions have shown: neither maps to a well-known Lister ID and PLAN
//     §2 forbids inventing one that would put words in the site's mouth.
//   - The site's own /ranking/ page: its TOTAL and MONTHLY tabs are loaded by
//     a script call this theme does not make (only WEEKLY ships in the
//     initial HTML), and its WEEKLY tab is the same ordering /directory/
//     already reads through Search's default. Reading it would mean either
//     fabricating a listing behind a request never sent, or duplicating one
//     already offered — both are the false claim PLAN §2 rules out.
//   - /directory/updated/, the status filter behind "New Updated": it answers
//     the same question ListingLatest (today's Browse) already does, so
//     naming it separately would offer the same page twice under different
//     labels.

// listSort is a directory sort's query string, "" for the default (no query,
// popularity order — which is also what an unadorned genre or status page
// serves).
type listSort string

const (
	sortPopular listSort = ""
	sortNews    listSort = "news"
	sortRating  listSort = "rating"
)

// Listings implements theme.Lister.
//
// The genre list lives on the directory page itself — there is no separate
// genre-list endpoint on this site — so this fetches the same page
// Search("") already reads. The caller caches the result (theme.Lister's
// doc comment), so this is one extra request per source per 24h, not one per
// screen open.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	out := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}

	doc, err := t.doc(ctx, s, browsePath)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	doc.Find(".update-bar-filter-list li a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		slug := genreSlugFromHref(href)
		if slug == "" || seen[slug] {
			return
		}
		label := theme.Collapse(a.AttrOr("title", ""))
		if label == "" {
			label = theme.Text(a)
		}
		if label == "" || strings.EqualFold(label, "all") {
			// The "All" entry clears the filter; it is not a genre.
			return
		}
		seen[slug] = true
		out = append(out, theme.GenreListing(slug, label))
	})
	return out, nil
}

// genreSlugFromHref reads the genre slug out of a directory genre link
// (/directory/{slug}/), and "" for anything else — including the bare
// /directory/ the "All" entry points at.
func genreSlugFromHref(href string) string {
	href = strings.TrimSpace(href)
	if !strings.HasPrefix(href, browsePath) {
		return ""
	}
	rest := strings.Trim(strings.TrimPrefix(href, browsePath), "/")
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// List implements theme.Lister.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}

	switch listingID {
	case theme.ListingLatest:
		// Today's Browse, delegated exactly (theme.Lister's doc comment):
		// Search("") already reads the directory in whatever order the
		// browseOrder override configures.
		return t.Search(ctx, s, "", page)
	case theme.ListingPopular:
		return t.directoryList(ctx, s, "", sortPopular, page)
	case theme.ListingNew:
		return t.directoryList(ctx, s, "", sortNews, page)
	case theme.ListingRating:
		return t.directoryList(ctx, s, "", sortRating, page)
	case theme.ListingCompleted:
		return t.directoryList(ctx, s, "completed", sortPopular, page)
	}
	if slug, ok := theme.GenreID(listingID); ok {
		return t.directoryList(ctx, s, slug, sortPopular, page)
	}
	return nil, fmt.Errorf("%s: source %q: unknown listing %q", ID, s.ID, listingID)
}

// directoryList fetches one page of the directory, optionally filtered to one
// status/genre path segment and sorted by one of the site's query params, and
// scrapes it exactly as Search's directory branch does.
//
// segment and sort are mutually exclusive in what the site itself offers
// (its filter bar has one slot, not two: a genre page and a status page are
// different pages), so this does not try to combine e.g. a genre with
// "completed" — nothing here claims that combination works.
func (t *Theme) directoryList(ctx context.Context, s *theme.Source, segment string, sort listSort, page int) ([]theme.SeriesStub, error) {
	path := browsePath
	if segment != "" {
		path += segment + "/"
	}
	if page > 1 {
		// Pages are files, not parameters: /directory/3.html.
		path += strconv.Itoa(page) + ".html"
	}
	if sort != "" {
		// A valueless parameter, as Search's directory branch already notes:
		// url.Values would render "news=" and the server would ignore it.
		path += "?" + string(sort)
	}

	doc, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}
	pageURL, err := s.Resolve(path)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve %s: %w", ID, path, err)
	}
	return t.scrapeStubs(s, doc, pageURL), nil
}
