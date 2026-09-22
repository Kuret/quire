package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// Searching every source at once — MessageSearchAll.
//
// The single-source search (runSearch) is a list; this is a *merge*. Three
// things follow from that and none of them are shared with runSearch, which is
// why this lives in its own file rather than growing an `if sourceID == ""`
// branch over there:
//
//   - The same series is on four sites under four spellings, so the rows are
//     grouped by title and a group names the sources that have it.
//   - The paging is across sources: one display page is cut out of everything
//     fetched so far, not out of one site's page 3.
//   - A source that fails is a missing row, not a failed search. Four sites
//     answered; emptying the screen because the fifth timed out would be the
//     worst possible reading of the evidence.

// searchAllMatch is one source's copy of a series. The lowercase fields are the
// sort keys and never reach the frontend: the UI orders nothing (PLAN §2).
type searchAllMatch struct {
	SourceID   string `json:"sourceId"`
	SourceName string `json:"sourceName"`
	SeriesID   string `json:"seriesId"`
	CoverURL   string `json:"coverUrl,omitempty"`

	// rank is 1-based within the source: the position the site itself put this
	// result in. It is the only relevance signal any of these sites give us,
	// and it is not comparable between sources — which is why ties fall back
	// to the user's own source order rather than to anything numeric.
	rank int

	// sourceOrder is the source's index in the user's configured order.
	sourceOrder int

	title string

	// kind is "book" or "manga" for the source this match came from (see
	// kind.go). Lowercase like the other sort-and-merge keys: it is what the
	// group's own kind is derived from, and the frontend is given the group's.
	kind string

	// authors is this match's copy of theme.SeriesStub.Authors, lowercase like
	// title and kind: the frontend is given the group's authors, chosen from
	// among the matches the same way the group's cover is (see group()), not
	// any one source's own.
	authors []string
}

// searchAllGroup is one series as far as the user is concerned.
type searchAllGroup struct {
	Key      string           `json:"key"`
	Title    string           `json:"title"`
	CoverURL string           `json:"coverUrl,omitempty"`
	Matches  []searchAllMatch `json:"matches"`

	// CoverSourceID and CoverSeriesID name the match the cover above actually
	// came from — which is not necessarily Matches[0]. The cover is the
	// best-ranked match that *has* one (see group()), so a request for it must
	// be attributed to that match's own source and series, never the group's
	// primary: a cover fetched under the wrong source is a cross-domain
	// request the SSRF guard correctly refuses, because the URL genuinely does
	// not belong to that source.
	CoverSourceID string `json:"coverSourceId,omitempty"`
	CoverSeriesID string `json:"coverSeriesId,omitempty"`

	// Kind is "book" or "manga" when every source in this group agrees, and
	// absent when they do not.
	//
	// Absent rather than "the best match's kind": a combined search merges on
	// the normalised title, so a book service and a comic site can legitimately
	// land under one heading — Dune the novel and Dune the manga are the same
	// six letters. Calling that group a book would be a claim about rows that
	// are not books, and the group is the wrong place to make a per-row claim.
	// Empty means "this group is not all one thing", which is the truth, and the
	// only honest thing a single field can say about it.
	Kind string `json:"kind,omitempty"`

	// Authors mirrors seriesRow.Authors, chosen the way CoverURL above is: the
	// best-ranked match that actually names one, not necessarily the group's
	// best match — a source that lists results with no author metadata would
	// otherwise blank out a group another source did name.
	Authors []string `json:"authors,omitempty"`
}

// searchAllError is one source that did not answer. It carries the source's
// name because the frontend has no other way to say *which* site is missing,
// and "one source failed" is not an actionable sentence.
type searchAllError struct {
	SourceID   string `json:"sourceId"`
	SourceName string `json:"sourceName"`
	Message    string `json:"message"`
}

// normaliseTitle is the whole of the grouping rule: lowercase, every rune that
// is not a letter or a digit becomes a space, runs of spaces collapse, trim.
// Two titles group when the results are *equal*. Nothing fuzzy, and no
// stripping of "vol. 2", "season 3", "(colour)" or "uncensored".
//
// The asymmetry is the argument. Under-merging costs the user one extra row,
// which they can see and understand. Over-merging hides a series behind
// another series' name, and there is no screen on which that is recoverable —
// edit distance would have merged "Vagabond" with "Vagabond (colour)", and a
// season-marker stripper would have merged a series with its sequel. Both were
// tried on paper against the sources in the store and both lost rows the user
// had asked for, so exact equality it is.
func normaliseTitle(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	pendingSpace := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteRune(r)
			continue
		}
		pendingSpace = true
	}
	return b.String()
}

// searchAllSourceState is one source's share of a cross-source search: what it
// has handed over so far, where to ask next, and whether it is out or broken.
type searchAllSourceState struct {
	id    string
	name  string
	order int

	// kind is what this source deals in, resolved once when the pager is built
	// rather than per stub: it is a property of the theme and cannot change
	// under a query.
	kind string

	// fetch asks this source for one source page, 1-based. It is the only
	// thing in this file that touches the network.
	fetch sourceFetch

	// items are the stubs in the order the source returned them, across every
	// round so far. The index is the rank, so this is append-only: a stub's
	// rank must not change under the user between two page turns.
	items []theme.SeriesStub
	seen  map[string]bool

	nextSourcePage int
	exhausted      bool

	// failure is the plain-language reason this source contributes nothing,
	// remembered for the life of the query. A site that just refused is not
	// asked again on the next page turn: it would cost the user a stall per
	// tap to re-learn the same answer.
	failure string
}

func (st *searchAllSourceState) done() bool { return st.exhausted || st.failure != "" }

// searchAllPager accumulates source pages from every source and serves display
// pages out of the accumulation, the way seriesPager does for one source. See
// the long comment at the top of paging.go for display pages vs source pages;
// the rule it exists to keep — one tap must not equal one HTTP request (PLAN
// §12.1) — is the same here, only now a "round" is one page from every source
// that still has some.
//
// It is deliberately outside Service, like seriesPager, so the merge can be
// tested without a socket, a store or a theme.
type searchAllPager struct {
	mu      sync.Mutex
	sources []*searchAllSourceState
}

// searchAllResult is one display page of groups, plus what the frontend needs
// to say where the user is. It mirrors pageResult, including the rule that a
// total is never guessed.
type searchAllResult struct {
	Groups     []searchAllGroup
	Page       int
	TotalPages int
	HasMore    bool
	Errors     []searchAllError

	// AllFailed says every source errored — the one case in which a search
	// across sources really is a failed search rather than a thin answer.
	AllFailed bool
}

// coverNoter is called for every stub seen, so the Service can record PLAN
// §7.6's truthful `Referer` per cover URL. The pager takes it as a function
// because it otherwise knows nothing about a Service.
type coverNoter func(sourceID string, st theme.SeriesStub)

// Page serves display page `page` of `size` groups, running fetch rounds until
// it can fill it or every source has run dry.
//
// Cancellation stops the loop between rounds rather than mid-round: the sources
// already asked are in flight and their answers are cheap to keep, and tearing
// out of the wait would leak the goroutines that are still writing into them.
func (p *searchAllPager) Page(ctx context.Context, page, size int, note coverNoter) searchAllResult {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 1
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Fill until the requested page is whole *and one group past it*, or every
	// source is out. The "one past" is paging.go's last-cached-page problem: a
	// page that exactly consumes what is cached leaves nothing here able to say
	// whether Next leads anywhere, so the control would either be offered and
	// then turn up empty or be withheld while there is more.
	want := page * size
	groups := p.group()
	for len(groups) <= want && !p.allDone() {
		if ctx.Err() != nil {
			break
		}
		p.fetchRound(ctx, note)
		groups = p.group()
	}
	return p.slice(groups, page, size)
}

// fetchRound asks every source that still has more for its next source page,
// all at once. Sequentially this would cost the sum of five sites' latencies
// for one screen; the sites are independent and the rate limiter is per-host
// (PLAN §7.4), so there is nothing to serialise them for.
func (p *searchAllPager) fetchRound(ctx context.Context, note coverNoter) {
	type answer struct {
		stubs []theme.SeriesStub
		err   error
	}
	answers := make([]answer, len(p.sources))

	var wg sync.WaitGroup
	for i, st := range p.sources {
		if st.done() {
			continue
		}
		wg.Add(1)
		go func(i int, st *searchAllSourceState) {
			defer wg.Done()
			stubs, err := st.fetch(ctx, st.nextSourcePage)
			answers[i] = answer{stubs, err}
		}(i, st)
	}
	wg.Wait()

	// Merged in source order, after the wait, so the accumulation does not
	// depend on which site answered first. Ordering that varied with network
	// timing would make the result of a page turn unreproducible.
	for i, st := range p.sources {
		if st.done() {
			continue
		}
		a := answers[i]
		if a.err != nil {
			st.failure = plain(a.err)
			continue
		}
		st.nextSourcePage++
		added := 0
		for _, stub := range a.stubs {
			if stub.ID == "" || st.seen[stub.ID] {
				continue
			}
			st.seen[stub.ID] = true
			st.items = append(st.items, stub)
			added++
			if note != nil {
				note(st.id, stub)
			}
		}
		// Nothing new came back: the source is either out, or serving the same
		// page for every page number — which some do, and which would be an
		// endless loop with a growing page counter.
		if added == 0 {
			st.exhausted = true
		}
	}
}

func (p *searchAllPager) allDone() bool {
	for _, st := range p.sources {
		if !st.done() {
			return false
		}
	}
	return true
}

// group rebuilds the groups from everything accumulated so far.
//
// It is a rebuild rather than an incremental merge because a later round can
// legitimately improve a group's rank — source B's page 2 can hold the result
// source A buried at 30 — and an incrementally maintained order would have to
// undo that anyway. The inputs are a few hundred stubs, so the sort is not
// worth the bug surface of caching it.
func (p *searchAllPager) group() []searchAllGroup {
	type bucket struct {
		matches []searchAllMatch
	}
	byKey := map[string]*bucket{}
	keys := make([]string, 0, 32)

	for _, st := range p.sources {
		for i, stub := range st.items {
			key := normaliseTitle(stub.Title)
			if key == "" {
				// A title of nothing but punctuation, or none at all. Grouping
				// those together would merge unrelated rows on the strength of
				// having no evidence, which is exactly what this rule refuses
				// to do elsewhere, so each keeps its own identity.
				key = "\x00" + st.id + "\x00" + stub.ID
			}
			b := byKey[key]
			if b == nil {
				b = &bucket{}
				byKey[key] = b
				keys = append(keys, key)
			}
			b.matches = append(b.matches, searchAllMatch{
				SourceID:    st.id,
				SourceName:  st.name,
				SeriesID:    stub.ID,
				CoverURL:    stub.CoverURL,
				rank:        i + 1,
				sourceOrder: st.order,
				title:       stub.Title,
				kind:        st.kind,
				authors:     stub.Authors,
			})
		}
	}

	groups := make([]searchAllGroup, 0, len(keys))
	for _, key := range keys {
		matches := byKey[key].matches

		// Best = the lowest rank, and between equal ranks the source the user
		// put first. It decides the group's title, its cover and its place in
		// the list.
		best := 0
		for i := range matches {
			if betterMatch(matches[i], matches[best]) {
				best = i
			}
		}

		// The cover is the best-ranked match that *has* one, not the best
		// match's cover: a source that lists results without thumbnails would
		// otherwise leave the group blank while four others had a picture.
		cover, coverSourceID, coverSeriesID := "", "", ""
		for _, m := range byRelevance(matches) {
			if m.CoverURL != "" {
				cover = m.CoverURL
				coverSourceID = m.SourceID
				coverSeriesID = m.SeriesID
				break
			}
		}

		var authors []string
		for _, m := range byRelevance(matches) {
			if len(m.authors) > 0 {
				authors = m.authors
				break
			}
		}

		g := searchAllGroup{
			Key:           key,
			Title:         matches[best].title,
			CoverURL:      cover,
			CoverSourceID: coverSourceID,
			CoverSeriesID: coverSeriesID,
			Authors:       authors,
			Kind:          agreedKind(matches),
			// Within a group the order is the user's source order: the list of
			// sources under a title should read the same way every time, and
			// the same way as the source list itself.
			Matches: matches,
		}
		sort.SliceStable(g.Matches, func(i, j int) bool {
			a, b := g.Matches[i], g.Matches[j]
			if a.sourceOrder != b.sourceOrder {
				return a.sourceOrder < b.sourceOrder
			}
			return a.rank < b.rank
		})
		groups = append(groups, g)
	}

	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		ar, ao := groupRank(a)
		br, bo := groupRank(b)
		if ar != br {
			return ar < br
		}
		if ao != bo {
			return ao < bo
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		// The key is the last resort so that the order is total: two groups
		// with the same rank, source and title would otherwise be settled by
		// map iteration, and a test that pins the order would be flaky.
		return a.Key < b.Key
	})
	return groups
}

// agreedKind is the kind every match in a group shares, or "" when they differ.
// See searchAllGroup.Kind for why disagreement is reported as absence rather
// than settled by a vote.
func agreedKind(matches []searchAllMatch) string {
	kind := ""
	for i, m := range matches {
		if i == 0 {
			kind = m.kind
			continue
		}
		if m.kind != kind {
			return ""
		}
	}
	return kind
}

// betterMatch is the relevance comparison: the site's own rank first, the
// user's source order to settle a tie.
func betterMatch(a, b searchAllMatch) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	return a.sourceOrder < b.sourceOrder
}

// byRelevance is the group's matches in relevance order, which is not the
// order they are shown in.
func byRelevance(matches []searchAllMatch) []searchAllMatch {
	out := append([]searchAllMatch(nil), matches...)
	sort.SliceStable(out, func(i, j int) bool { return betterMatch(out[i], out[j]) })
	return out
}

// groupRank is the group's place in the list: its best match's rank, and that
// match's source order for ties.
func groupRank(g searchAllGroup) (rank, order int) {
	rank, order = 0, 0
	for i, m := range g.Matches {
		if i == 0 || betterMatch(m, searchAllMatch{rank: rank, sourceOrder: order}) {
			rank, order = m.rank, m.sourceOrder
		}
	}
	return rank, order
}

// slice cuts the groups into the requested display page. Caller holds mu.
func (p *searchAllPager) slice(groups []searchAllGroup, page, size int) searchAllResult {
	total := len(groups)
	res := searchAllResult{Page: page}

	failed := 0
	for _, st := range p.sources {
		if st.failure != "" {
			res.Errors = append(res.Errors, searchAllError{SourceID: st.id, SourceName: st.name, Message: st.failure})
			failed++
		}
	}
	res.AllFailed = failed > 0 && failed == len(p.sources)

	if p.allDone() {
		// Only now is a total honest — the same rule as pageResult. An empty
		// result is one page of nothing, not zero pages.
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
	res.Groups = append([]searchAllGroup(nil), groups[start:end]...)
	res.HasMore = end < total || !p.allDone()
	return res
}

// searchAllKey identifies which combined listing a cached pager answers for:
// the query text, and which scope it was asked on. The scope is part of the
// key — not a separate flag beside it — so that switching between the normal
// combined search and the private one is exactly like typing a different
// query: a cache miss that rebuilds the source list from scratch, never a
// stale pager built for the other scope answering under the wrong one.
type searchAllKey struct {
	query   string
	private bool
}

// searchAllPagerFor returns the pager for this query and scope, building it —
// and with it the list of sources to ask — when either has changed.
//
// One pager is kept, replaced when the key changes, for the same reason
// pagerFor keeps one: paging back and forth must be free, but every search ever
// typed is a cache nobody asked for. It shares pagerMu with the single-source
// pager so that dropPagers retires both, which is what makes a removed or
// re-probed source invalidate a combined listing too.
func (s *Service) searchAllPagerFor(query string, private bool) *searchAllPager {
	key := searchAllKey{query: query, private: private}
	s.pagerMu.Lock()
	defer s.pagerMu.Unlock()
	if s.searchAllPagerCur != nil && s.searchAllQuery == key {
		return s.searchAllPagerCur
	}
	p := s.newSearchAllPager(query, private)
	s.searchAllPagerCur, s.searchAllQuery = p, key
	return p
}

// newSearchAllPager snapshots the enabled sources whose Private flag matches
// wantPrivate, in the user's order, and binds each to its theme. A source
// whose theme is gone is recorded as a failure rather than dropped: a silently
// absent site is indistinguishable from a site with no results for this query.
//
// **This is the combined search's partition point**, and it is the one that
// matters most: a source excluded here is never bound to a theme and never
// asked anything, so the mixed-scope leak this feature exists to prevent — a
// private source's site receiving a request because a combined search touched
// it — cannot happen no matter what the reply to the frontend does or does
// not filter. See buildSourceViews (service.go) for the enumeration's other
// half, driven by the same theme.Source.IsPrivate.
func (s *Service) newSearchAllPager(query string, wantPrivate bool) *searchAllPager {
	p := &searchAllPager{}
	for _, src := range s.store.List() {
		if !src.IsEnabled() || src.IsPrivate() != wantPrivate {
			continue
		}
		st := &searchAllSourceState{
			id:             src.ID,
			name:           src.Name,
			order:          len(p.sources),
			seen:           map[string]bool{},
			nextSourcePage: 1,
		}
		th, bound, err := s.themeFor(src.ID)
		if err != nil {
			st.failure = err.Error()
		} else {
			st.kind = kindOf(th)
			st.fetch = func(ctx context.Context, sourcePage int) ([]theme.SeriesStub, error) {
				return th.Search(ctx, bound, query, sourcePage)
			}
		}
		p.sources = append(p.sources, st)
	}
	return p
}

// runSearchAll answers MessageSearchAll (private=false) and MessageSearchAllPrivate
// (private=true): one query, every source in that scope, one grouped and
// paged reply. See newSearchAllPager for where the scope actually excludes a
// source's site from being asked anything at all.
func (s *Service) runSearchAll(ctx context.Context, out Sender, query string, page, pageSize int, private bool) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	// Nothing is sent for a cancelled request. The frontend cancels when the
	// user has moved on, so the reply would land on a screen showing something
	// else — and the error path would turn a deliberate cancel into a banner.
	if ctx.Err() != nil {
		return
	}

	pager := s.searchAllPagerFor(query, private)
	// Collected here and written once per source below, rather than a write per
	// stub: a combined search touches every enabled source and this callback
	// runs for every row of every one of them.
	refs := map[string][]state.CoverRef{}
	res := pager.Page(ctx, page, pageSize, func(sourceID string, st theme.SeriesStub) {
		// Without this the group's cover 403s at the CDN (PLAN §7.6): the URL
		// round-trips through the frontend and arrives back with no memory of
		// the page it was parsed out of.
		s.rememberCoverReferrer(sourceID, st.CoverURL, st.CoverReferrer)
		refs[sourceID] = append(refs[sourceID], state.CoverRef{SeriesID: st.ID, URL: st.CoverURL})
	})
	for sourceID, bySource := range refs {
		s.rememberCoverURLs(sourceID, bySource)
	}
	if ctx.Err() != nil {
		return
	}

	for _, e := range res.Errors {
		s.log.Warn("a source did not answer the combined search", "source", e.SourceID, "err", e.Message)
	}

	// Every source failed and nothing at all came back: there is no partial
	// answer to show, so this is a failed search rather than an empty one.
	if res.AllFailed && len(res.Groups) == 0 {
		_ = s.sendError(out, "search_failed", res.Errors[0].Message)
		return
	}

	groups := res.Groups
	if groups == nil {
		groups = []searchAllGroup{}
	}
	errs := res.Errors
	if errs == nil {
		errs = []searchAllError{}
	}
	_ = send(out, appload.MessageSearchAllResults, map[string]any{
		"query":    query,
		"page":     res.Page,
		"pageSize": pageSize,
		// 0 means "not every source has said how much it has" — the frontend
		// shows "Page 3" rather than inventing a denominator.
		"totalPages":   res.TotalPages,
		"hasMore":      res.HasMore,
		"groups":       groups,
		"sourceErrors": errs,
	})
}
