// Package asurascans implements a single Astro-generated comic site as a
// Quire theme, driven entirely by the site's own JSON API rather than by
// scraping its rendered markup.
//
// # Why an API theme rather than an HTML one
//
// The site is built with Astro: its comic and chapter pages are genuinely
// server-rendered (a real page of chapter anchors is present in the initial
// response, confirmed live against a 117-chapter series), but everything a
// series page shows beyond that — cover, genres, status, description — lives
// only inside client-hydration data, either an "Astro island" prop blob using
// a devalue-style encoding or a JSON-LD block, neither of which is a set of
// elements a selector can read cleanly.
//
// Reading the site's own script chunks (found by fetching the linked
// `/_astro/*.js` files a page already loads, not by any bypass) turned up
// something better than either: the search box and the browse filters both
// call a plain, unauthenticated JSON API on a sibling subdomain of the site
// itself, and that same API answers a series' full metadata, its complete
// chapter list in one request, and a chapter's page image URLs — the same
// data the hydrated page eventually renders, as structured JSON instead of
// markup to unpick. That makes this theme's shape much closer to `mangadex`'s
// than to `madara`'s or `mangathemesia`'s: one deployment, recognised and
// driven by its API rather than by a family of shared markup.
//
// # Why no host is written down here
//
// PLAN §1.3 forbids this repository from naming a real site's domain in
// product code. Unlike `mangadex`, which is the *documented* single API this
// exception was written for, this theme earns no such exception, so it takes
// none: every request is built from the source's own configured `baseUrl` —
// see apiURL — and no file in this package spells out a domain. The
// consequence is that AllowedHosts is nil rather than a wildcard: the API and
// the image host are both observed to be plain subdomains of the site's own
// host, so fetch.Guard's ordinary same-registrable-domain rule already
// permits them without this theme naming anything.
//
// # Search, and what "solved" means here
//
// The obvious guesses (`?search=`, `?q=` against the human-facing `/browse`
// page) do not work — that page is client-hydrated and answers nothing to a
// plain GET's query string. The API does: `/api/series?search={q}` is the
// exact call the site's own browse-filter UI makes, it paginates with a
// genuine `offset`, and an empty `search` is the site's own browse listing —
// which is what makes Search also serve as PLAN §7.5's "Browse is a search
// with an empty query" case, with no separate listing method to write.
//
// # Provenance
//
// Written from HTTP responses and JavaScript observed live on 2026-09-21/22,
// fetched at a deliberately gentle pace with the honest User-Agent. PLAN
// §1.4: endpoint shapes and response field names are facts about the site,
// recorded here in our own words; no selector, script or prose is
// transcribed from anywhere. The fixtures under testdata/ are synthetic —
// written from scratch to reproduce the *shape* of a real response, on
// "example.invalid" hosts that can never resolve, per PLAN §1.3.
package asurascans

import (
	"context"
	"fmt"
	gohtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "asurascans"

// searchLimit mirrors the site's own page size for both the search box and
// the browse listing, so paging through Search matches what the site itself
// considers a page.
const searchLimit = 20

// Override keys.
const (
	// KeyBrowseSort is the sort order used when the query is empty.
	//
	// PLAN §7.5's M3 correction makes "Browse" a search with an empty query,
	// and this site's relevance ordering — its default when a `search` term is
	// present — means nothing when there is no term to rank against. Naming
	// the sort is the fix, and it is a preference rather than a constant for
	// the same reason `weebcentral`'s is: what Browse should show differs
	// between someone catching up and someone looking for something new.
	KeyBrowseSort = "browseSort"
)

// browseSorts is the site's own sort vocabulary, sent verbatim (observed in
// its browse-filter script), so a value here does not need a translation
// table to keep in step with someone else's naming.
var browseSorts = []string{"popular", "newest", "rating", "name", "update"}

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key:     KeyBrowseSort,
			Kind:    theme.KindString,
			Default: "popular",
			Enum:    browseSorts,
			Why: "Browse is a search with an empty query (PLAN §7.5), and this site's " +
				"default ranking is relevance, which orders nothing without a term to rank " +
				"against. This names the order the browse listing comes back in instead.",
		},
	},
}

// Theme is the asurascans theme.
type Theme struct {
	f   theme.Fetcher
	now func() time.Time
}

// New builds the theme over a fetcher.
func New(f theme.Fetcher) *Theme { return &Theme{f: f, now: time.Now} }

// NewWithClock is New with an injectable clock.
func NewWithClock(f theme.Fetcher, now func() time.Time) *Theme {
	return &Theme{f: f, now: now}
}

// DiscoveryOnly implements theme.DiscoveryClassifier: a copy of this theme
// whose every request is classified as discovery (PLAN §7.4), for the
// unattended watched-series check of PLAN §12.2.
//
// Every call this theme makes — search, series lookup, chapter list, and the
// chapter-page manifest — is a listing or a link being followed rather than a
// thing the user individually asked to retrieve, so the copy is equivalent to
// the receiver. It implements the interface anyway, for the reason theme.go
// gives: a caller that needs the guarantee should be able to require it
// rather than take a theme's silence for an answer.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// AllowedHosts implements theme.Theme.
//
// nil, and checked rather than assumed: the API and the image host are both
// plain subdomains of the source's own host (see apiURL and the package
// comment on why neither is named here), so fetch.Guard's ordinary
// same-registrable-domain rule already covers both without widening anything.
func (t *Theme) AllowedHosts() []string { return nil }

// SuggestedName implements theme.Theme.
//
// A real name, because this theme drives one site rather than a family of
// independently branded ones — the same reasoning as weebcentral's and
// mangadex's. It is a display name, not a URL, so it names nothing PLAN §1.3
// forbids.
func (t *Theme) SuggestedName() string { return "Asura Scans" }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// disqualifying markers put the score at zero outright: each is another
// theme's own unmistakable marker, and this site shares no lineage with any
// of them, so there is no legitimate page of this theme's on which one could
// appear.
var disqualifying = []string{
	"/wp-content/",              // madara, mangathemesia, and WordPress at large
	"ts_reader.run(",            // mangathemesia's reader bootstrap
	"#chapter-images",           // weebcentral's reader container
	"data-chapter-url-template", // mangakakalot's client-rendered chapter list
}

// Fingerprint scores a probed page for "is this the site this theme drives?".
//
// The site is an Astro build with utility-CSS classes, so — as with
// weebcentral — styling carries no signal. What is distinctive is the
// application's own plumbing: the named component chunks its "Astro island"
// hydration loads (stable, human-chosen names; only the cache-busting hash
// suffix on each changes between builds, and these signals do not depend on
// it), a schema.org type this site's structured data uses that is rare
// outside comic publishers, and the site's own path convention.
//
// Several of these — the subscription-chargeback and per-series Discord-guild
// features — are combined in a way no other family in this registry has any
// reason to share, which is what lets a handful of weak, individually common
// signals add up to a confident score without leaning on any one of them, or
// on the site's own brand text.
func (t *Theme) Fingerprint(p *probe.Page) int {
	if p == nil {
		return 0
	}
	for _, m := range disqualifying {
		if p.Contains(m) {
			return 0
		}
	}

	score := 0
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	// Present on every page: the astro-island component chunks the header and
	// footer chrome load everywhere, and the framework's own generator tag.
	add(p.Contains("GuildInviteModal"), 20)
	add(p.Contains("ChargebackBanner"), 20)
	add(p.Contains("MonthlySubBanner"), 15)
	add(p.Contains("RelinkBanner"), 15)
	add(p.Contains("MigrationBanner"), 10)
	add(p.Contains("OnboardingModal"), 10)
	add(p.Has(`meta[name="generator"][content^="Astro"]`), 10)
	add(p.Has(`a[href*="/comics/"]`), 15)

	// Series-page-only markers, strong enough on their own that a probe
	// landing directly on a series page is not left "unrecognised".
	add(p.Contains(`"@type":"ComicSeries"`), 25)
	add(p.Contains("SeriesDownloadModal"), 15)
	add(p.Contains("ReportSeriesDataButton"), 15)

	if score > 100 {
		score = 100
	}
	return score
}

// Search implements theme.Theme.
//
// Discovery in PLAN §7.4's sense. An empty query is PLAN §7.5's Browse case:
// the site's own relevance ranking (its default with a search term) orders
// nothing without one, so KeyBrowseSort's ranking is asked for explicitly
// instead — the same shape as weebcentral's KeyBrowseSort, chosen for the
// same reason.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	q = strings.TrimSpace(q)
	v := url.Values{}
	if q != "" {
		v.Set("search", q)
	} else {
		v.Set("sort", o.String(KeyBrowseSort))
		v.Set("order", "desc")
	}
	v.Set("limit", strconv.Itoa(searchLimit))
	v.Set("offset", strconv.Itoa((page-1)*searchLimit))

	var res seriesListResponse
	if err := t.apiGet(ctx, s, "/api/series", v, &res); err != nil {
		return nil, err
	}
	return seriesStubs(res), nil
}

// seriesStubs maps a /api/series response's entries onto the stubs the theme
// hands back. Search and List's listing pages read the same envelope, so this
// is the one place that decides which entries are worth keeping (a slug- or
// title-less entry is not a series this theme can resolve later).
func seriesStubs(res seriesListResponse) []theme.SeriesStub {
	out := make([]theme.SeriesStub, 0, len(res.Data))
	for _, e := range res.Data {
		if e.Slug == "" || e.Title == "" {
			continue
		}
		out = append(out, theme.SeriesStub{
			ID:       seriesID(e.Slug),
			Title:    e.Title,
			CoverURL: e.Cover,
		})
	}
	return out
}

// Listings implements theme.Lister.
//
// The site's own browse-filter script (see the package comment) sends five
// sort values, of which three have a well-known Quire meaning: "popular",
// "newest" and "rating". "name" (alphabetical) and "update" have no
// well-known counterpart and are not offered as their own listing — "update"
// is what the *default* Browse already uses when browseSort is left at its
// default, and the Lister doc says ListingLatest may be omitted for exactly
// that reason.
//
// The site also filters its listing by status; only "completed" has a
// well-known ID (ListingCompleted covers "finished series only", the one
// status filter Quire's browse screen offers).
//
// Genres come from the API's own /api/genres, the same list the site's own
// filter UI is built from — fetched fresh here because the caller (the
// service layer) caches the answer per source for 24h.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	listings := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}

	var res genreListResponse
	if err := t.apiGet(ctx, s, "/api/genres", nil, &res); err != nil {
		return nil, err
	}
	for _, g := range res.Data {
		name := strings.TrimSpace(g.Name)
		slug := strings.TrimSpace(g.Slug)
		if name == "" || slug == "" {
			continue
		}
		listings = append(listings, theme.GenreListing(slug, name))
	}
	return listings, nil
}

// List implements theme.Lister.
//
// ListingLatest delegates to Search with an empty query, which is what today's
// Browse already does ("List(ListingLatest) must equal what Search(q="")
// returns today"). Every other listing goes to the same /api/series endpoint
// Search uses, with an explicit sort or status/genre filter instead of the
// source's configured browseSort — a named listing's meaning does not depend
// on a source's override.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	if listingID == "" || listingID == theme.ListingLatest {
		return t.Search(ctx, s, "", page)
	}
	if page < 1 {
		page = 1
	}

	v := url.Values{}
	switch {
	case listingID == theme.ListingPopular:
		v.Set("sort", "popular")
		v.Set("order", "desc")
	case listingID == theme.ListingNew:
		v.Set("sort", "newest")
		v.Set("order", "desc")
	case listingID == theme.ListingRating:
		v.Set("sort", "rating")
		v.Set("order", "desc")
	case listingID == theme.ListingCompleted:
		v.Set("status", "completed")
		v.Set("sort", "update")
		v.Set("order", "desc")
	default:
		genre, ok := theme.GenreID(listingID)
		if !ok || genre == "" {
			return nil, fmt.Errorf("%s: unknown listing %q", ID, listingID)
		}
		v.Set("genre", genre)
		v.Set("sort", "update")
		v.Set("order", "desc")
	}
	v.Set("limit", strconv.Itoa(searchLimit))
	v.Set("offset", strconv.Itoa((page-1)*searchLimit))

	var res seriesListResponse
	if err := t.apiGet(ctx, s, "/api/series", v, &res); err != nil {
		return nil, err
	}
	return seriesStubs(res), nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	slug, err := seriesSlug(id)
	if err != nil {
		return nil, err
	}

	var res seriesDetailResponse
	if err := t.apiGet(ctx, s, "/api/series/"+url.PathEscape(slug), nil, &res); err != nil {
		return nil, err
	}
	e := res.Series
	if e.Slug == "" {
		return nil, fmt.Errorf("%s: series %q: not found", ID, slug)
	}

	out := &theme.Series{
		ID:          seriesID(e.Slug),
		Title:       e.Title,
		AltTitles:   append([]string(nil), e.AltTitles...),
		CoverURL:    e.Cover,
		Description: stripDescriptionHTML(e.Description),
		Status:      parseStatus(e.Status),
	}
	if a := placeholderTrim(e.Author); a != "" {
		out.Authors = []string{a}
	}
	if a := placeholderTrim(e.Artist); a != "" {
		out.Artists = []string{a}
	}
	for _, g := range e.Genres {
		if name := strings.TrimSpace(g.Name); name != "" {
			out.Genres = append(out.Genres, name)
		}
	}
	return out, nil
}

// Chapters implements theme.Theme.
//
// One request returns the whole list — confirmed live against a 117-chapter
// series, all 117 present — so there is no paging loop to bound here, unlike
// mangadex's feed.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	slug, err := seriesSlug(id)
	if err != nil {
		return nil, err
	}

	var res chapterListResponse
	if err := t.apiGet(ctx, s, "/api/series/"+url.PathEscape(slug)+"/chapters", nil, &res); err != nil {
		return nil, err
	}

	out := make([]theme.Chapter, 0, len(res.Data))
	for _, c := range res.Data {
		out = append(out, theme.Chapter{
			ID:        chapterID(slug, c.Number),
			Title:     chapterTitle(c.Number),
			Number:    c.Number,
			Published: parseAPITime(c.PublishedAt),
		})
	}
	// PLAN §7.2's ordering contract. The API lists chapters newest first (the
	// same direction the site's own server-rendered chapter list uses), so
	// this reverses rather than sorts — SortAndMark decides which, and records
	// the answer either way.
	return theme.SortAndMark(out), nil
}

// Pages implements theme.Theme.
//
// Discovery: this fetches the manifest naming a chapter's page images, not
// the images themselves — the download queue fetches those separately,
// through the ordinary guarded client. No robots.txt was found on the API
// host at all (a 404, which fetch's robots handling treats as "no rules");
// see the package comment.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	slug, number, err := parseChapterID(chapterID)
	if err != nil {
		return nil, err
	}

	var res chapterPageResponse
	if err := t.apiGet(ctx, s, "/api/series/"+url.PathEscape(slug)+"/chapters/"+url.PathEscape(number), nil, &res); err != nil {
		return nil, err
	}
	if res.Data.AccessGate != "" {
		return nil, fmt.Errorf("%s: chapter %s: not readable: %s", ID, chapterID, res.Data.AccessGate)
	}

	out := make([]string, 0, len(res.Data.Chapter.Pages))
	for _, pg := range res.Data.Chapter.Pages {
		if u := strings.TrimSpace(pg.URL); u != "" {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: chapter %s: no page images", ID, chapterID)
	}
	return out, nil
}

// parseStatus normalises the site's status vocabulary (observed in its
// browse-filter script: "ongoing", "completed", "hiatus", "dropped", "axed")
// to Quire's. PLAN §7.2: an unrecognised value becomes StatusUnknown rather
// than being passed through, so the UI never has to render a site's own
// words.
func parseStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "completed":
		return theme.StatusCompleted
	case "ongoing":
		return theme.StatusOngoing
	case "hiatus":
		return theme.StatusHiatus
	case "dropped", "axed", "cancelled", "canceled":
		return theme.StatusCancelled
	default:
		return theme.StatusUnknown
	}
}

// placeholderTrim trims a metadata field and treats a bare "-" — this site's
// way of rendering an unset author or artist — as empty, the same quirk
// mangathemesia's placeholder fields have under a different site's markup.
func placeholderTrim(s string) string {
	s = strings.TrimSpace(s)
	if s == "-" {
		return ""
	}
	return s
}

var tagRE = regexp.MustCompile(`<[^>]+>`)

// stripDescriptionHTML converts the API's HTML-formatted description to plain
// text. Paragraph and line breaks become blank lines and single newlines
// respectively before the remaining tags are dropped, so a multi-paragraph
// synopsis does not collapse into one run-on line; theme.Series.Description
// is shown as plain text and every other theme's is already that.
func stripDescriptionHTML(s string) string {
	s = strings.ReplaceAll(s, "</p>", "\n\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = tagRE.ReplaceAllString(s, "")
	s = gohtml.UnescapeString(s)
	return strings.TrimSpace(s)
}

// parseAPITime parses the API's RFC3339 timestamps. Its "unset" sentinel,
// "0001-01-01T00:00:00Z", parses to exactly Go's zero time.Time — confirmed
// rather than assumed, since it is the one date value this function must not
// let through as a real publish date — so no separate check is needed for it.
// An unparseable value also yields the zero time, which PLAN §7.2 says is
// fine: a chapter with no date is still a chapter worth reading.
func parseAPITime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
