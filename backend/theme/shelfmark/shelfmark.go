// Package shelfmark implements a self-hosted Shelfmark instance as a Quire
// theme: a book search-and-download service, driven through its JSON API.
//
// # This one is not a comic site
//
// Every other theme in backend/theme drives a site that serves *page images*,
// and the shape of the Theme interface follows from that: a series has
// chapters, a chapter has pages, and M4 turns the pages into a PDF. Shelfmark
// has none of those things. It searches book metadata, finds *releases* —
// a particular format of a particular book from a particular source — and,
// when asked, fetches one and hands back a finished epub or pdf.
//
// The mapping is therefore a deliberate one rather than an obvious one, and it
// is worth stating plainly because the words in the interface will otherwise
// mislead whoever reads this next:
//
//   - A **book is a series** with no chapters in the ordinary sense. There are
//     no volumes, no instalments, nothing to read in order.
//   - A **chapter is a release**. Choosing a "chapter" here means choosing a
//     format and a source, which is why the titles are built to be read as
//     exactly that: "EPUB · 1.7MB · Direct Download — Dune".
//   - **Pages does not exist**, and says so. It returns an error rather than
//     an empty slice, because an empty slice reads as "this chapter has no
//     pages", which is a different and false claim. The file is fetched
//     through theme.FileTheme.Retrieve instead.
//
// # Only what the device can open
//
// Shelfmark's own `supported_formats` on the instance measured on 2026-09-20
// was epub, mobi, azw3, fb2, djvu, cbz, cbr. The reMarkable opens two of those
// — and pdf, which is not in the instance's list but is what several sources
// actually offer. Everything else is filtered out (see keepFormat): offering a
// user an azw3 they cannot read, on a device with no way to convert it, is
// worse than telling them the file exists somewhere else. On the Dune capture
// that keeps 34 of 50 releases, and ReleaseList reports the other 16 rather
// than hiding them — see its Dropped field.
//
// # Provenance
//
// Written against a live self-hosted instance on 2026-09-20; every endpoint,
// field and number in this package was observed there, and the fixtures under
// testdata are that instance's own responses with personal data removed. PLAN
// §1.4: endpoint shapes are facts, expression is not copied.
package shelfmark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "shelfmark"

// The API, as verified on 2026-09-20. Nothing here is guessed: an endpoint
// that is not on this list is not called.
const (
	pathHealth        = "/api/health"
	pathConfig        = "/api/config"
	pathSearch        = "/api/metadata/search"
	pathBook          = "/api/metadata/book/"
	pathReleases      = "/api/releases"
	pathStartDownload = "/api/releases/download"
	pathStatus        = "/api/status"
	pathLocalDownload = "/api/localdownload"
)

// ConfigPath is where the instance's config lives, exported so the PLAN §7.5
// stage-5 capability probe knows what to fetch before calling CheckConfig.
const ConfigPath = pathConfig

// Theme is the shelfmark theme.
type Theme struct {
	f theme.Fetcher

	// now and sleep are injectable so Retrieve's polling loop can be tested
	// without a test that actually waits minutes. See NewWithClock.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// New builds the theme over a fetcher.
func New(f theme.Fetcher) *Theme {
	return &Theme{f: f, now: time.Now, sleep: realSleep}
}

// NewWithClock is New with an injectable clock, for the retrieval tests.
func NewWithClock(f theme.Fetcher, now func() time.Time, sleep func(ctx context.Context, d time.Duration) error) *Theme {
	t := New(f)
	if now != nil {
		t.now = now
	}
	if sleep != nil {
		t.sleep = sleep
	}
	return t
}

func realSleep(ctx context.Context, d time.Duration) error {
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tm.C:
		return nil
	}
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// SuggestedName implements theme.Theme.
//
// The instance's <title> is already "Shelfmark", so this changes nothing
// today. It is answered rather than left to the title because the title is
// the *web UI's*, and a future build is free to put an instance name in it —
// at which point a source called "Rick's Books — Shelfmark" would be named
// after someone else's deployment detail.
func (t *Theme) SuggestedName() string { return "Shelfmark" }

// ProbeQuery implements theme.ProbeQuerier.
//
// Measured against a live instance (2026-09-20): the empty-query listing
// stage 5 tries first answers HTTP 400 here — Shelfmark's search requires a
// query, by design, not by fault — and the package default fallback, "one",
// came back with zero results from that instance's metadata provider
// (openlibrary). "dune" is a well-known title any book index answers, so it
// is what actually tests whether the site works rather than manufacturing a
// false refusal out of a query chosen for a different family of themes.
func (t *Theme) ProbeQuery() string { return "dune" }

// AllowedHosts implements theme.Theme.
//
// Nil, and it stays nil. Everything this theme fetches is on the instance
// itself: covers come back as instance-relative paths ("/api/covers/…"), and
// so does the finished file ("/api/localdownload?id=…"). The release's own
// `download_url` points at whatever third-party site the instance found it on
// — annas-archive.gl in the capture — and Quire never fetches that: the
// instance does, and Quire collects the result. Naming those hosts here would
// widen PLAN §7.4's redirect boundary for a request that is never made.
func (t *Theme) AllowedHosts() []string { return nil }

// DiscoveryOnly implements theme.DiscoveryClassifier.
//
// It is answered because PLAN §12.2 requires callers to *require* the
// interface rather than taking silence for "this theme retrieves nothing" —
// and this theme does retrieve: Retrieve posts a download request, which is
// the most consequential thing any theme here does. A watch check must never
// reach it, and a copy whose every request is downgraded to discovery is the
// cheap way of saying so.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// Fingerprint scores a probed page for "is this a Shelfmark instance?".
//
// Shelfmark is self-hosted, so unlike mangadex there is no host to gate on:
// the instance lives wherever its owner put it, and the only evidence is what
// the page says about itself. The probe lands on the web UI, which is a
// single-page app — a near-empty shell whose <head> is all there is to read.
//
// Two signals carry the weight, and they are weighted the way they are because
// one is much harder to hit by accident than the other:
//
//   - The meta description, "Shelfmark - Book search and download", is a
//     complete sentence shipped by the application. 55.
//   - The <title>, "Shelfmark". 35 — a title is one word, and one word is
//     something any site can have.
//   - The iOS PWA title, also "Shelfmark". 10, as corroboration only.
//
// The arithmetic is the point. The real shell scores 100; a page that is
// merely *called* Shelfmark scores 45, which is below PLAN §7.5's threshold of
// 60, so it is reported as unrecognised rather than claimed. Nothing short of
// the application's own description gets over the line on its own.
//
// **This is a first guess and is not sufficient.** /api/health answering
// {"status":"ok"} is the most generic response on the internet and scores
// nothing here at all. The confirmation is CheckConfig, against /api/config —
// see its comment for why that key set is the strong check and this is not.
func (t *Theme) Fingerprint(p *probe.Page) int {
	if p == nil {
		return 0
	}
	doc, err := p.Document()
	if err != nil || doc == nil {
		return 0
	}

	score := 0
	if meta, ok := doc.Find(`meta[name="description"]`).First().Attr("content"); ok &&
		strings.EqualFold(strings.TrimSpace(meta), appDescription) {
		score += 55
	}
	if strings.EqualFold(p.Title(), appName) {
		score += 35
	}
	if meta, ok := doc.Find(`meta[name="apple-mobile-web-app-title"]`).First().Attr("content"); ok &&
		strings.EqualFold(strings.TrimSpace(meta), appName) {
		score += 10
	}
	return score
}

// What the application calls itself, observed 2026-09-20.
const (
	appName        = "Shelfmark"
	appDescription = "Shelfmark - Book search and download"
)

// configKeys are the keys /api/config carries that, *together*, identify a
// Shelfmark instance. See CheckConfig.
var configKeys = []string{
	"supported_formats",
	"release_search_timeout",
	"metadata_search_fields",
	"books_output_mode",
}

// CheckConfig reports whether body is a Shelfmark instance's /api/config.
//
// This is the strong check, and it is deliberately not the fingerprint.
// Fingerprint reads a page's <head>, which anyone can write; /api/health
// answering {"status":"ok"} is worth nothing at all. What is hard to produce
// by accident is the *co-occurrence* of this particular set of keys in one
// config object: `supported_formats` alongside `release_search_timeout`
// alongside `metadata_search_fields` alongside `books_output_mode`. Any one of
// them could belong to some other application; all four together describe
// Shelfmark's model — formats it can hand over, how long it will search
// sources for a release, what a metadata search may be keyed on, and where
// finished books are put.
//
// # Where this belongs, and why it is exported
//
// It cannot be theme.SourceValidator: that interface is handed a *Source and
// nothing else, so it cannot fetch anything, and this check is about a
// response. It belongs in PLAN §7.5's stage-5 capability probe, which does
// have a fetcher — that wiring is not in this change (see the package's entry
// in the report), so this is left as the named, exported half the prober
// calls, with ConfigPath saying what to GET. Confirm below is the same check
// with the fetch attached, for a caller that has a theme.Fetcher to hand.
//
// The "Resource not found" body is rejected here too, and has to be: an
// unknown path on this API answers it with HTTP 200, so a prober that took a
// successful status for a successful check would confirm any server at all.
func CheckConfig(body []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("shelfmark: %s did not answer a JSON object: %w", ConfigPath, err)
	}
	if msg, ok := raw["error"]; ok {
		var s string
		if json.Unmarshal(msg, &s) == nil && strings.TrimSpace(s) != "" {
			return fmt.Errorf("shelfmark: %s answered an error: %s", ConfigPath, s)
		}
	}
	var missing []string
	for _, k := range configKeys {
		if _, ok := raw[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("shelfmark: %s is missing %s; these four keys together are what "+
			"identifies a Shelfmark instance, so this is not one", ConfigPath, strings.Join(missing, ", "))
	}
	return nil
}

// Confirm fetches /api/config and runs CheckConfig over it.
//
// This is the call the capability probe wants: it is the difference between
// "a page said Shelfmark" and "this host is a Shelfmark instance". It is a
// separate method rather than part of Fingerprint because Fingerprint is given
// a page and no way to make a second request.
func (t *Theme) Confirm(ctx context.Context, s *theme.Source) error {
	body, err := t.getRaw(ctx, s, t.endpoint(s, ConfigPath, nil))
	if err != nil {
		return err
	}
	return CheckConfig(body)
}

// searchPageSize is what the instance returns per page. It is not a parameter:
// /api/metadata/search takes `query` and `page` and nothing else that was
// observed to change the page size, so paging is by page number only.
const searchPageSize = 40

// SearchResult is a page of search results, with the paging the instance
// reported alongside it.
//
// theme.Theme.Search returns only a slice, which cannot express "there is
// another page" — and this API answers that question directly, with has_more,
// rather than leaving a caller to infer it from a short page. So the full
// answer is available here and Search is the interface-shaped view of it.
type SearchResult struct {
	Books   []theme.SeriesStub
	HasMore bool
	Page    int

	// TotalFound is the instance's own count. It was **0** on a response that
	// carried 40 books (2026-09-20), so it is reported for completeness and
	// must not be used to decide anything. HasMore is the trustworthy field.
	TotalFound int
}

// Search implements theme.Theme. It is SearchPage without the paging.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	res, err := t.SearchPage(ctx, s, q, page)
	if err != nil {
		return nil, err
	}
	return res.Books, nil
}

// SearchPage searches book metadata.
//
// Discovery, unambiguously (PLAN §7.4): the user is asking what exists.
func (t *Theme) SearchPage(ctx context.Context, s *theme.Source, q string, page int) (*SearchResult, error) {
	if page < 1 {
		page = 1
	}
	v := url.Values{}
	v.Set("query", strings.TrimSpace(q))
	v.Set("page", strconv.Itoa(page))

	var res searchResponse
	if err := t.getJSON(ctx, s, t.endpoint(s, pathSearch, v), &res); err != nil {
		return nil, err
	}

	out := make([]theme.SeriesStub, 0, len(res.Books))
	for _, b := range res.Books {
		stub, ok := t.stub(s, b)
		if !ok {
			continue
		}
		out = append(out, stub)
	}
	got := &SearchResult{Books: out, HasMore: res.HasMore, Page: res.Page, TotalFound: res.TotalFound}
	if got.Page == 0 {
		got.Page = page
	}
	return got, nil
}

// stub turns a book into a search row, or reports that it cannot be addressed.
//
// A book with no provider or no provider_id has no id Quire could build, so it
// is dropped rather than given a half-formed one that fails later, out of
// sight of the search that produced it.
func (t *Theme) stub(s *theme.Source, b book) (theme.SeriesStub, bool) {
	if b.Provider == "" || b.ProviderID == "" || strings.TrimSpace(b.Title) == "" {
		return theme.SeriesStub{}, false
	}
	return theme.SeriesStub{
		ID:       bookID(b.Provider, b.ProviderID),
		Title:    fullTitle(b),
		CoverURL: t.coverURL(s, b),
		Authors:  trimAll(b.Authors),
		// CoverReferrer is left empty on purpose. The instance serves its own
		// covers through its own /api/covers/ proxy and asks for no Referer;
		// naming a page we fetched would send a header nobody wants, and PLAN
		// §7.6 is clear that "no header" is the right answer when there is
		// nothing the host requires.
	}, true
}

// Series implements theme.Theme: one book.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	ref, err := parseBookID(id)
	if err != nil {
		return nil, err
	}
	b, err := t.book(ctx, s, ref)
	if err != nil {
		return nil, err
	}
	return &theme.Series{
		ID:          ref.String(),
		Title:       fullTitle(b),
		CoverURL:    t.coverURL(s, b),
		Description: strings.TrimSpace(b.Description),
		Authors:     trimAll(b.Authors),
		Genres:      trimAll(b.Genres),
		// Status is left unknown, which is the honest answer: a book is not
		// serialised, so "ongoing" and "completed" describe nothing about it,
		// and theme.Series documents an unrecognised value as StatusUnknown
		// rather than something invented.
		Status: theme.StatusUnknown,
	}, nil
}

// book fetches one book's metadata.
func (t *Theme) book(ctx context.Context, s *theme.Source, ref bookRef) (book, error) {
	path := pathBook + url.PathEscape(ref.Provider) + "/" + url.PathEscape(ref.ProviderID)
	var res bookResponse
	if err := t.getJSON(ctx, s, t.endpoint(s, path, nil), &res); err != nil {
		return book{}, err
	}
	if res.ProviderID == "" {
		return book{}, fmt.Errorf("shelfmark: %s: the instance returned no book", path)
	}
	return res.book, nil
}

// ReleaseList is the answer to "what can I actually download for this book?",
// with the filtering made visible.
//
// theme.Chapters returns a bare slice, so there is nowhere in it to say "and
// 16 more exist that your device cannot open". That count is not a detail to
// swallow: a user who can see 34 epubs and knows there were 50 results
// understands what happened, where one who sees 34 and no explanation is left
// wondering whether the search worked. Chapters is the interface-shaped view
// of this; anything that wants to tell the user calls this directly.
type ReleaseList struct {
	// Chapters are the releases the device can open, in the order described on
	// Chapters.
	Chapters []theme.Chapter

	// Book is the instance's own metadata for the book, which /api/releases
	// returns alongside the releases and is therefore free.
	Book theme.Series

	// Dropped is how many releases were filtered out for being in a format the
	// reMarkable cannot open.
	Dropped int

	// DroppedFormats names those formats, deduplicated and sorted, so a
	// message can say "16 hidden (azw3, fb2, mobi)" rather than just a number.
	DroppedFormats []string

	// SourcesSearched is what the instance says it searched. Worth surfacing
	// when the list comes back short: "no releases found" and "no releases
	// found, and only one source was reachable" are different problems.
	SourcesSearched []string
}

// Chapters implements theme.Theme: the book's releases, one Chapter each.
//
// Choosing a "chapter" here is choosing a format and a source, so the titles
// are built to be read that way — see releaseTitle.
//
// # The order
//
// There is none to discover. Releases are alternatives to each other, not a
// sequence, so no ordering of them is "the reading order" and the list is
// marked OrderUnknown through theme.SortAndMark — which is exactly the case
// that mechanism exists for, and is more honest than assigning positions 1..n
// to a set of interchangeable files.
//
// The list is still *deterministically* ordered, because a list that shuffles
// between two calls is a bad list whether or not it means anything: epub
// before pdf (reflowable text is what this device is for, and a pdf's fixed
// page size usually is not), then smallest first, then by title, source and
// source id. Smallest-first is a device decision rather than a quality
// judgement — storage is scarce, the connection is not fast, and the smaller
// of two copies of the same book is the one to try first.
//
// Every release carries Number -1: none of them has a chapter number, and
// inventing one would make theme.SortAscending reorder them on the strength of
// a number this theme made up.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	list, err := t.ReleaseList(ctx, s, id)
	if err != nil {
		return nil, err
	}
	return list.Chapters, nil
}

// ReleaseList fetches and filters a book's releases.
//
// This is the slow call — 36 seconds, measured 2026-09-20, because the
// instance asks each of its sources in turn. It imposes no deadline of its
// own: the caller's context governs, and the instance's own
// `release_search_timeout` (300s in its config) already bounds it at the far
// end. A shorter timeout here would abandon a search that was working.
func (t *Theme) ReleaseList(ctx context.Context, s *theme.Source, id string) (*ReleaseList, error) {
	ref, err := parseBookID(id)
	if err != nil {
		return nil, err
	}
	res, err := t.releases(ctx, s, ref)
	if err != nil {
		return nil, err
	}

	out := &ReleaseList{SourcesSearched: trimAll(res.SourcesSearched)}
	out.Book = theme.Series{
		ID:          ref.String(),
		Title:       fullTitle(res.Book),
		CoverURL:    t.coverURL(s, res.Book),
		Description: strings.TrimSpace(res.Book.Description),
		Authors:     trimAll(res.Book.Authors),
		Genres:      trimAll(res.Book.Genres),
	}

	type row struct {
		ch   theme.Chapter
		rank int
		size int64
		// name is the release's *own* title, not the composed chapter title.
		// The composed one begins with the format and the size, so sorting on
		// it would sort by size a second time and quietly stand in for the
		// size comparison above if that ever broke.
		name string
		src  string
		sid  string
	}
	var rows []row
	droppedFormats := map[string]bool{}

	for _, raw := range res.Releases {
		r, err := decodeRelease(raw)
		if err != nil {
			// One malformed release is not a reason to fail the list; the
			// other 49 are still usable. It is counted as dropped so the total
			// still adds up to what the instance sent.
			out.Dropped++
			continue
		}
		f := normaliseFormat(r.Format)
		rank, ok := keepFormat(f)
		if !ok {
			out.Dropped++
			if f != "" {
				droppedFormats[f] = true
			}
			continue
		}
		if r.Source == "" || r.SourceID == "" {
			// Nothing to build an id from, so nothing that could be retrieved
			// later. Counted, for the same reason as above.
			out.Dropped++
			continue
		}
		rows = append(rows, row{
			ch: theme.Chapter{
				ID:     releaseID(ref.Provider, ref.ProviderID, r.Source, r.SourceID),
				Title:  releaseTitle(r, f),
				Number: -1,
			},
			rank: rank,
			size: releaseSize(r),
			name: r.Title,
			src:  r.Source,
			sid:  r.SourceID,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		// An unknown size (-1) sorts last rather than first, so a release
		// whose size string we could not read does not lead the list.
		ai, bi := a.size, b.size
		if ai < 0 {
			ai = 1<<62 - 1
		}
		if bi < 0 {
			bi = 1<<62 - 1
		}
		if ai != bi {
			return ai < bi
		}
		if a.name != b.name {
			return a.name < b.name
		}
		if a.src != b.src {
			return a.src < b.src
		}
		return a.sid < b.sid
	})

	chs := make([]theme.Chapter, 0, len(rows))
	for _, r := range rows {
		chs = append(chs, r.ch)
	}
	// Marks the whole list OrderUnknown, which is the point: there is no
	// reading order among alternatives. It is called rather than open-coded so
	// that this theme cannot report an order it did not establish.
	out.Chapters = theme.SortAndMark(chs)

	for f := range droppedFormats {
		out.DroppedFormats = append(out.DroppedFormats, f)
	}
	sort.Strings(out.DroppedFormats)
	return out, nil
}

// releases fetches /api/releases for a book.
//
// **This is the slow one, and it asks for the slow client.** The instance asks
// each of its sources in turn before answering: measured at 36 seconds against
// an instance searching one source, and its own `release_search_timeout` is 300
// seconds (2026-09-20). The client's ordinary bound is 60 seconds overall and 30
// seconds to the first response byte — and 30 is the one that matters, because a
// server that is searching sends no header until it has finished. So this call
// would have died at 30 seconds on a real instance, in the middle of adding a
// source or starting a download. See fetch.Client.GetSlow.
func (t *Theme) releases(ctx context.Context, s *theme.Source, ref bookRef) (*releasesResponse, error) {
	v := url.Values{}
	v.Set("provider", ref.Provider)
	v.Set("book_id", ref.ProviderID)

	var res releasesResponse
	if err := t.getJSONSlow(ctx, s, t.endpoint(s, pathReleases, v), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// slowGetter is the one capability the release search needs that theme.Fetcher
// does not offer. fetch.Client implements it, and so does themetest.Fetcher.
//
// It is asserted rather than added to theme.Fetcher for the reason jsonPoster
// gives: that interface is implemented by the real client and half a dozen test
// doubles, and only this theme has a call a server spends minutes on.
type slowGetter interface {
	GetSlow(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error)
}

// getJSONSlow is getJSON with the longer bound, falling back to the ordinary
// request for a fetcher that has none.
//
// The fallback is deliberate and is not a safety relaxation: it is exactly the
// behaviour this call had until 2026-09-20, so the worst it can do is time out
// where it used to. It is needed because theme.DiscoveryFetcher — what
// DiscoveryOnly wraps this theme's fetcher in for PLAN §12.2's watch check —
// exposes only theme.Fetcher's methods. Refusing there would break the watch
// check on a source that works, which is a real failure traded for a
// hypothetical one.
func (t *Theme) getJSONSlow(ctx context.Context, s *theme.Source, rawurl string, out errorCarrier) error {
	g, ok := t.f.(slowGetter)
	if !ok {
		return t.getJSON(ctx, s, rawurl, out)
	}
	p, err := s.Policy()
	if err != nil {
		return err
	}
	resp, err := g.GetSlow(ctx, p, rawurl)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("shelfmark: %s: HTTP %d", rawurl, resp.StatusCode)
	}
	return decodeInto(rawurl, resp.Body, out)
}

// Pages implements theme.Theme, and this theme has none.
//
// A Shelfmark "chapter" is a finished epub or pdf, not a run of page images,
// so there is nothing for the M4 image pipeline to do with one. The error is
// deliberate and an empty slice would be wrong: nil, nil reads as "this
// chapter has no pages", which a caller would report to the user as a broken
// chapter on a broken site, when in fact the file is right there and is
// fetched by Retrieve.
//
// Callers that can handle a file should type-assert theme.FileTheme rather
// than calling this and interpreting the failure.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	return nil, fmt.Errorf("%w: %q is a finished file, not a set of page images; "+
		"fetch it with theme.FileTheme.Retrieve", ErrNotPageBased, chapterID)
}

// ErrNotPageBased is what Pages returns. It is a sentinel so a caller can tell
// "this theme does not do pages" from "this chapter's pages could not be
// found", which are the same sentence and completely different problems.
var ErrNotPageBased = fmt.Errorf("shelfmark: this theme has no page images")

// keepFormat reports whether a format is one the reMarkable opens natively,
// and its rank in the release ordering.
//
// The device reads epub and pdf. It does not read mobi, azw3, fb2, djvu, cbz
// or cbr, and Quire has no converter — on the 2026-09-20 capture that is 16 of
// 50 releases filtered out, all of them formats the instance is happy to fetch
// and the device cannot open. The list is explicit rather than a subtraction
// from the instance's `supported_formats`, because that config key describes
// what *Shelfmark* supports, which is a different question from what the
// reading device does.
func keepFormat(f string) (rank int, ok bool) {
	switch f {
	case "epub":
		return 0, true
	case "pdf":
		return 1, true
	default:
		return 0, false
	}
}

// normaliseFormat lowercases and trims a format, and drops a leading dot.
func normaliseFormat(f string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(f)), ".")
}

// releaseSize is the release's size in bytes, or -1.
func releaseSize(r release) int64 {
	if r.SizeBytes != nil && *r.SizeBytes >= 0 {
		return *r.SizeBytes
	}
	return sizeInBytes(r.Size)
}

// releaseTitle composes what the user picks from.
//
// A release list is a list of *choices*, and the three things that decide the
// choice are the format, the size and who it comes from — in that order, since
// the format is the one that can make the file useless. The release's own
// title comes last and is still needed: the sources return loosely matched
// results, and the first release for "Dune" in the capture was in fact
// "O'Reilly, Timothy - The Maker of Dune", which a user must be able to see
// before downloading it.
//
//	EPUB · 1.7MB · Direct Download — Dune
//	PDF · Direct Download — Dune                (no size given)
func releaseTitle(r release, format string) string {
	parts := []string{strings.ToUpper(format)}
	if sz := strings.TrimSpace(r.Size); sz != "" {
		parts = append(parts, sz)
	}
	if src := sourceLabel(r); src != "" {
		parts = append(parts, src)
	}
	if q := contentQualifier(r); q != "" {
		parts = append(parts, q)
	}
	title := strings.Join(parts, " · ")
	if name := strings.TrimSpace(r.Title); name != "" {
		title += " — " + name
	}
	return title
}

// sourceLabel is the human-readable name of where a release came from.
//
// `indexer` is the display name ("Direct Download") and `source` is the
// machine one ("direct_download"); the first is preferred and the second is
// tidied into title case when it is all there is, because a user picking
// between sources should not have to read snake_case.
func sourceLabel(r release) string {
	if v := strings.TrimSpace(r.Indexer); v != "" {
		return v
	}
	v := strings.TrimSpace(r.Source)
	if v == "" {
		return ""
	}
	words := strings.FieldsFunc(v, func(r rune) bool { return r == '_' || r == '-' })
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// contentQualifier is the informative part of a release's content_type, or "".
//
// The field arrives as "📕 book (fiction)" — every value in the capture was
// "book" with a parenthesised qualifier, so the word "book" says nothing
// (everything here is one) and the qualifier sometimes does. "unknown", which
// was 13 of the 50 releases, says nothing either and is dropped rather than
// shown as a label that means "we did not look".
//
// The emoji never reaches a title: see CleanContentType.
func contentQualifier(r release) string {
	v := CleanContentType(r.ContentType)
	open := strings.Index(v, "(")
	closing := strings.LastIndex(v, ")")
	if open < 0 || closing < open {
		return ""
	}
	q := strings.ToLower(strings.TrimSpace(v[open+1 : closing]))
	if q == "" || q == "unknown" {
		return ""
	}
	return q
}

// fullTitle joins a book's title and subtitle.
func fullTitle(b book) string {
	title := strings.TrimSpace(b.Title)
	if sub := strings.TrimSpace(b.Subtitle); sub != "" && title != "" {
		return title + ": " + sub
	}
	return title
}

// coverURL resolves the instance-relative cover path against the source base.
//
// Covers come back as "/api/covers/…?url=<base64>" — the instance proxies the
// provider's image, so the only host involved is the instance's own. Resolving
// against s.BaseURL rather than storing an absolute URL is what keeps a source
// re-pointable, and is why AllowedHosts is nil.
func (t *Theme) coverURL(s *theme.Source, b book) string {
	raw := strings.TrimSpace(b.CoverURL)
	if raw == "" {
		return ""
	}
	abs, err := s.Resolve(raw)
	if err != nil {
		return ""
	}
	return abs
}

// endpoint builds an absolute URL from the source's base.
//
// The base is the source's because this theme drives a *self-hosted* service:
// there is no canonical host, every instance is somewhere different, and
// unlike mangadex there is nothing to validate it against. That is also why
// CheckConfig exists — with no host to recognise, the only way to know the
// user pointed Quire at a real Shelfmark is to ask the server.
func (t *Theme) endpoint(s *theme.Source, path string, v url.Values) string {
	u := strings.TrimRight(strings.TrimSpace(s.BaseURL), "/") + path
	if len(v) > 0 {
		u += "?" + v.Encode()
	}
	return u
}

// getRaw performs a discovery GET and returns the body.
func (t *Theme) getRaw(ctx context.Context, s *theme.Source, rawurl string) ([]byte, error) {
	if t.f == nil {
		return nil, fmt.Errorf("shelfmark: no fetcher")
	}
	p, err := s.Policy()
	if err != nil {
		return nil, err
	}
	resp, err := t.f.Get(ctx, p, rawurl)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("shelfmark: %s: HTTP %d", rawurl, resp.StatusCode)
	}
	return resp.Body, nil
}

// getJSON performs a discovery GET and decodes the body into out.
//
// out must embed apiError, and the check is not optional: this API answers an
// unknown path with {"error":"Resource not found"} and **HTTP 200**, so a
// decode that ignored the error member would hand back a zero-valued struct
// that looks exactly like an empty but successful result. Every response in
// this package goes through here for that reason.
func (t *Theme) getJSON(ctx context.Context, s *theme.Source, rawurl string, out errorCarrier) error {
	body, err := t.getRaw(ctx, s, rawurl)
	if err != nil {
		return err
	}
	return decodeInto(rawurl, body, out)
}

// decodeInto is the body half of getJSON, shared with the POST in Retrieve.
func decodeInto(rawurl string, body []byte, out errorCarrier) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("shelfmark: %s: %w", rawurl, err)
	}
	if msg := strings.TrimSpace(out.apiErrorMessage()); msg != "" {
		return fmt.Errorf("shelfmark: %s: the instance answered %q; "+
			"an unknown path answers this with HTTP 200, so check the base URL", rawurl, msg)
	}
	return nil
}

// errorCarrier is what apiError gives every response type: the ability to be
// asked whether it is in fact an error.
type errorCarrier interface {
	apiErrorMessage() string
}

func (e apiError) apiErrorMessage() string { return e.Error }

// trimAll trims each entry and drops the empties, returning nil for nothing —
// so an absent list and a list of blanks are the same thing downstream.
func trimAll(in []string) []string {
	var out []string
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
