package webtoons

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/rickl/quire/backend/theme"
)

// Package theme.Lister for this publisher. Every endpoint below and its
// sortOrder values were read out of the live nav and each listing page's own
// sort tabs, and confirmed against webtoons.com on 2026-09-24 by checking the
// resulting card grids actually differ:
//
//	Popular   GET /{lang}/genres?sortOrder=MANA          the tab labelled
//	                                                      "by Popularity"
//	                                                      (also the default
//	                                                      when the parameter
//	                                                      is omitted)
//	Completed GET /{lang}/originals/complete?sortOrder=MANA
//	Genre     GET /{lang}/genres/{slug}?sortOrder=MANA, slugs read from
//	          /{lang}/genres' own genre tab bar
//
// Every one of these is the same ".webtoon_list" card grid Search's own
// fixture already pins, so List reuses cardList.
//
// Like Search's own unscoped query, none of these pages honour a page
// parameter — verified live: appending &page=2 to the genre listing and to
// the completed listing returns the identical set of titles, not a second
// page of it. List therefore answers page 1 and an empty page beyond it,
// exactly as Search already does for the case its own doc comment names.
//
// # What is not offered, and why
//
// Both listing pages also offer sortOrder=LIKEIT ("by Likes") and
// sortOrder=UPDATE ("by Date", confirmed by `data-sort-log-code="update"`).
// Neither has a well-known home here. "by Likes" is a second popularity
// metric alongside "by Popularity" and offering both under one facet would
// be inventing a distinction Quire's own vocabulary does not have. "by Date"
// is not ListingNew — its own log code says "update", not "created" or
// "added", so it orders by last activity, not by when a series joined the
// site — and it cannot be ListingLatest either, because theme.Lister requires
// List(ListingLatest) to equal Search(q="") exactly, and Search's own
// unscoped query answers a fixed set of "best matches" that is not ordered by
// date at all. There is no rating-based sort on either page, so
// theme.ListingRating is not offered.
const (
	genresPath          = "genres"
	originalsCompleted  = "originals/complete"
	sortOrderPopularity = "MANA"
)

var _ theme.Lister = (*Theme)(nil)

// Listings implements theme.Lister.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	out := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}
	genres, err := t.genreListings(ctx, s)
	if err != nil {
		return nil, err
	}
	return append(out, genres...), nil
}

// genreListings reads the genre tab bar off /{lang}/genres.
//
// Each tab is an <a class="snb_tab"> whose href names the slug and whose text
// is the site's own name for it — kept verbatim, including its own casing,
// per theme.Lister's doc: the label is the site's name, not Quire's opinion
// of how to style it.
func (t *Theme) genreListings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	path := "/" + t.lang(s) + "/" + genresPath
	doc, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: genres: %w", ID, s.ID, err)
	}

	var out []theme.Listing
	seen := make(map[string]bool)
	doc.Find("a.snb_tab[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		slug := genreSlug(href)
		label := theme.Collapse(theme.Text(a))
		if slug == "" || label == "" || seen[slug] {
			return
		}
		seen[slug] = true
		out = append(out, theme.GenreListing(slug, label))
	})
	return out, nil
}

// genreSlug pulls the path segment after ".../genres/" out of a tab's href,
// ignoring its sortOrder query string.
func genreSlug(href string) string {
	href = strings.TrimSpace(href)
	i := strings.Index(href, "/"+genresPath+"/")
	if i < 0 {
		return ""
	}
	rest := href[i+len(genresPath)+2:]
	if q := strings.IndexAny(rest, "?#"); q >= 0 {
		rest = rest[:q]
	}
	return strings.Trim(rest, "/")
}

// List implements theme.Lister.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	if listingID == "" || listingID == theme.ListingLatest {
		// theme.Lister's doc: List(ListingLatest) must equal Search(q="")
		// exactly, implemented by delegating.
		return t.Search(ctx, s, "", page)
	}
	if page > 1 {
		// Neither listing page below honours a page parameter — verified
		// live, see the package comment — so an empty page is the truthful
		// answer to "page 2", exactly as Search's own unscoped query already
		// does for the case its doc comment names.
		return nil, nil
	}

	lang := t.lang(s)
	switch listingID {
	case theme.ListingPopular:
		return t.cardList(ctx, s, "/"+lang+"/"+genresPath+"?sortOrder="+sortOrderPopularity)
	case theme.ListingCompleted:
		return t.cardList(ctx, s, "/"+lang+"/"+originalsCompleted+"?sortOrder="+sortOrderPopularity)
	}
	if slug, ok := theme.GenreID(listingID); ok && slug != "" {
		return t.cardList(ctx, s, "/"+lang+"/"+genresPath+"/"+slug+"?sortOrder="+sortOrderPopularity)
	}
	return nil, fmt.Errorf("%s: source %q: unknown listing %q", ID, s.ID, listingID)
}
