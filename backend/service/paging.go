package service

import (
	"context"
	"sync"

	"github.com/rickl/quire/backend/theme"
)

// Display paging — PLAN §12.1.
//
// The panel does not scroll (a scroll is a stream of partial refreshes and so a
// column of ghosts); it turns pages. A *display* page is however many tiles fit
// the viewport, which the frontend computes from its own geometry because it is
// the only layer that knows any pixels. A *source* page is whatever the site
// hands back per request — MangaDex gives 20, a Madara listing gives 36.
//
// The two numbers have nothing to do with each other, and PLAN §12.1 is explicit
// that one tap must not equal one HTTP request. So the pager accumulates source
// pages into one list and serves display-sized slices out of it: turning the
// page is free until the slice runs past what has been fetched, and only then
// does it reach for the network, and then for as many source pages as it takes
// to fill the display page — never a half-empty screen with more available.
//
// Everything here is deliberately outside Service so it can be tested without a
// socket, a store or a theme (PLAN §2: the logic belongs where it is testable).

// pageResult is one display page, plus enough context for the frontend to say
// where the user is without inventing anything.
type pageResult struct {
	// Items is the slice for this display page. It is shorter than the page
	// size only on the last page.
	Items []theme.SeriesStub

	// Page is the 1-based display page actually served. It differs from the
	// page asked for when the request ran past the end and was clamped.
	Page int

	// TotalPages is 0 when the source has not yet told us how much there is.
	// The frontend shows "Page 3" in that case and "Page 3 of 12" otherwise.
	// It is never guessed: it is set only once the source has run dry.
	TotalPages int

	// HasMore says a next page exists — either already cached or still out
	// there. It is what disables the Next control at a hard stop; PLAN §12.1
	// wants a hard stop rather than a wrap, because wrapping lies about where
	// the end is.
	HasMore bool
}

// sourceFetch fetches one source page, 1-based. It is the only thing in this
// file that touches the network.
type sourceFetch func(ctx context.Context, sourcePage int) ([]theme.SeriesStub, error)

// seriesPager caches the source pages fetched for one (source, query) pair and
// serves display pages out of the cache.
//
// A pager is single-purpose and cheap: when the user searches for something
// else, the Service throws it away rather than growing a cache of every listing
// ever browsed (PLAN §6 M3: never hold more than a screenful in memory — the
// stubs are titles and URLs, but the principle is the same).
type seriesPager struct {
	mu sync.Mutex

	items []theme.SeriesStub
	seen  map[string]bool

	// nextSourcePage is the source page to ask for next. 1-based.
	nextSourcePage int

	// exhausted is set once the source has nothing further. It is the only
	// thing that makes a total honest, so it is never set on a guess.
	exhausted bool
}

func newSeriesPager() *seriesPager {
	return &seriesPager{seen: map[string]bool{}, nextSourcePage: 1}
}

// Page serves display page `page` of `size` items, fetching source pages until
// it can fill it.
//
// A fetch error is only fatal when it leaves nothing to show: if the cache
// already holds the requested page, or part of it, the caller gets that page
// and the error, and decides. Losing a screenful of results the user can see
// because the *next* request failed would be the worse answer.
func (p *seriesPager) Page(ctx context.Context, page, size int, fetch sourceFetch) (pageResult, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 1
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Fill until the requested page is whole *and one item past it*, or the
	// source runs out. One display page can span several source pages (9 tiles
	// from a source that hands back 3) and that must still cost one tap.
	//
	// The "one item past" is the last-cached-page problem: sitting on a page
	// that exactly consumes the cache, nothing here knows whether a next page
	// exists, so Next would either be offered and then turn up empty, or be
	// withheld when there is more. Reading one item beyond the page is what
	// makes the control truthful, and it costs a request only at the boundary
	// — which the next tap would have spent anyway.
	var fetchErr error
	want := page * size
	for len(p.items) <= want && !p.exhausted {
		stubs, err := fetch(ctx, p.nextSourcePage)
		if err != nil {
			fetchErr = err
			break
		}
		p.nextSourcePage++

		added := 0
		for _, st := range stubs {
			if st.ID == "" || p.seen[st.ID] {
				continue
			}
			p.seen[st.ID] = true
			p.items = append(p.items, st)
			added++
		}
		// Nothing new came back. Either the source is out, or it is serving
		// the same page for every page number — which some do, and which would
		// otherwise be an infinite loop with a growing "next page" counter.
		if added == 0 {
			p.exhausted = true
		}
	}

	return p.slice(page, size), fetchErr
}

// slice cuts the cached items into the requested display page. Caller holds mu.
func (p *seriesPager) slice(page, size int) pageResult {
	total := len(p.items)

	res := pageResult{Page: page}
	if p.exhausted {
		// Now, and only now, is a total honest. An empty result is still one
		// page — of nothing — rather than zero pages.
		res.TotalPages = (total + size - 1) / size
		if res.TotalPages < 1 {
			res.TotalPages = 1
		}
		if res.Page > res.TotalPages {
			res.Page = res.TotalPages
		}
	}

	start := (res.Page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	res.Items = append([]theme.SeriesStub(nil), p.items[start:end]...)
	res.HasMore = end < total || !p.exhausted
	return res
}

// pagerKey identifies the listing a pager holds. The empty query is the
// site's own default listing, which is what MessageBrowse asks for.
//
// listing is the theme.Listing ID the empty-query case is paging — "" and
// "latest" both mean the default and are normalised to "" (runSearch does
// that), so Browse and a Search naming "latest" share the one cache. It is
// part of the key, not a detail alongside it, precisely so that switching
// listings on the same source never serves the old listing's cached page: a
// pager keyed only on (source, query) would hand "Popular" page 2 the tail
// end of "Top rated"'s cache because both are empty-query listings on the
// same source.
type pagerKey struct {
	sourceID string
	query    string
	listing  string
}

// pagerFor returns the pager for this listing, replacing the one held if the
// user has moved to a different listing. Exactly one is kept: paging back and
// forth through a listing must be free, but every listing ever opened is a
// cache nobody asked for.
func (s *Service) pagerFor(key pagerKey) *seriesPager {
	s.pagerMu.Lock()
	defer s.pagerMu.Unlock()
	if s.pager == nil || s.pagerKey != key {
		s.pagerKey = key
		s.pager = newSeriesPager()
	}
	return s.pager
}

// dropPagers forgets the cached listing. Called when a source is removed or
// re-probed, because its results are no longer trustworthy.
func (s *Service) dropPagers() {
	s.pagerMu.Lock()
	defer s.pagerMu.Unlock()
	s.pager, s.pagerKey = nil, pagerKey{}
	s.searchAllPagerCur, s.searchAllQuery = nil, searchAllKey{}
}
