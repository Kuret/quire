package service

import (
	"context"
	"sync"
	"time"

	"github.com/rickl/quire/backend/theme"
)

// Browse listings — MessageListListings/MessageListings, and PLAN's browse
// screen generalised from "the site's own recent page" to "however many ways
// the site actually offers to browse it" (theme.Lister).
//
// Everything here is deliberately outside Service, like paging.go, so it can
// be tested without a socket, a store or a real theme.

// listingsTTL is how long a source's answer is trusted before it is fetched
// again. A day, because a site's set of genres and sort orders changes about
// as often as its theme does — nowhere near often enough to justify asking on
// every visit to the browse screen.
const listingsTTL = 24 * time.Hour

// listingView is one entry of a MessageListings reply.
type listingView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"`
}

// latestOnly is what a source with nothing else to offer answers: today's
// Browse, named explicitly, exactly as MessageListings' doc comment promises.
func latestOnly() []listingView {
	return []listingView{{ID: theme.ListingLatest, Label: theme.ListingLabel(theme.ListingLatest), Group: theme.ListingGroupSort}}
}

// fetchListings asks the theme, if it is a theme.Lister, and orders the
// result: theme.ListingLatest first (never duplicated even if the theme also
// named it), then the rest of the sort group, then status, then genre — in
// the order the theme gave within each group, which for genres is the order
// the site's own genre list page had them (PLAN §2: not ours to re-sort).
func fetchListings(ctx context.Context, th theme.Theme, src *theme.Source) ([]listingView, error) {
	result := latestOnly()

	lister, ok := th.(theme.Lister)
	if !ok {
		return result, nil
	}
	named, err := lister.Listings(ctx, src)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{theme.ListingLatest: true}
	var sortG, statusG, genreG []listingView
	for _, l := range named {
		if seen[l.ID] {
			continue
		}
		seen[l.ID] = true

		label := l.Label
		// Well-known listings get the one wording every source uses, whatever
		// the theme actually sent — a source does not get to call its
		// popular page "Hot" (PLAN §2, theme.ListingLabel's own doc comment).
		if wk := theme.ListingLabel(l.ID); wk != "" {
			label = wk
		}
		lv := listingView{ID: l.ID, Label: label, Group: l.Group}
		switch l.Group {
		case theme.ListingGroupSort:
			sortG = append(sortG, lv)
		case theme.ListingGroupStatus:
			statusG = append(statusG, lv)
		case theme.ListingGroupGenre:
			genreG = append(genreG, lv)
			// A group this caller does not recognise is a theme bug, not a
			// listing to hide the user's way into by silently dropping it —
			// but there is nowhere honest to place it, so it is skipped
			// rather than guessed at.
		}
	}
	result = append(result, sortG...)
	result = append(result, statusG...)
	result = append(result, genreG...)
	return result, nil
}

// listingsCacheEntry is one source's last successful answer.
type listingsCacheEntry struct {
	listings  []listingView
	fetchedAt time.Time
}

// listingsCache holds each source's MessageListings answer for listingsTTL,
// and is what makes a failed Listings() call degrade rather than blank the
// browse screen's picker: the caller never sees an error for this, only a
// shorter (or unchanged) list, plus a log line.
type listingsCache struct {
	mu      sync.Mutex
	entries map[string]listingsCacheEntry
}

func newListingsCache() *listingsCache {
	return &listingsCache{entries: map[string]listingsCacheEntry{}}
}

// listingsFor answers a MessageListListings request for sourceID, using the
// cache when it is fresh (within listingsTTL of `now`).
//
// A fresh fetch that fails degrades to whatever was cached before — stale
// rather than nothing — and, failing that, to latestOnly: the one listing
// every source has always had. Either way `onErr` is called so the failure is
// logged; there is deliberately no error reply and no error banner (PLAN §2:
// a browse screen that stopped offering "Top rated" because one fetch of the
// picker's own menu failed is a worse answer than a screen that just does not
// offer it this time).
func (c *listingsCache) listingsFor(ctx context.Context, sourceID string, th theme.Theme, src *theme.Source, now func() time.Time, onErr func(error)) []listingView {
	if now == nil {
		now = time.Now
	}

	c.mu.Lock()
	entry, ok := c.entries[sourceID]
	fresh := ok && now().Sub(entry.fetchedAt) < listingsTTL
	c.mu.Unlock()
	if fresh {
		return entry.listings
	}

	fetched, err := fetchListings(ctx, th, src)
	if err != nil {
		if onErr != nil {
			onErr(err)
		}
		if ok {
			// Degrade to the last thing that worked rather than to nothing —
			// "the sort/status entries it can know" (and whatever genres came
			// with them) — even though it may itself now be stale.
			return entry.listings
		}
		return latestOnly()
	}

	c.mu.Lock()
	c.entries[sourceID] = listingsCacheEntry{listings: fetched, fetchedAt: now()}
	c.mu.Unlock()
	return fetched
}

// forget drops a source's cached listings. Called when the source itself is
// removed, so a re-added source of the same id starts clean.
func (c *listingsCache) forget(sourceID string) {
	c.mu.Lock()
	delete(c.entries, sourceID)
	c.mu.Unlock()
}
