// Package globalcomix implements GlobalComix's JSON API as a Quire theme.
//
// # API, not markup — and why
//
// PLAN §7.3 asks a theme's author to check for a usable API before scraping
// markup. GlobalComix has one: its own web client is a single-page app that
// server-renders almost nothing (a batch probe against markup-based themes
// scored it 0 across the board), and everything the page shows — search
// results, a series' releases, a chapter's pages — is fetched by the client
// from a versioned JSON API at api.globalcomix.com after the shell loads. That
// API is not publicly documented, but it is observable: every request and
// response shape below was read out of the site's own shipped JavaScript
// bundle and confirmed against live responses on 2026-09-21/22 (PLAN §1.4:
// endpoint shapes are facts, not expression — no bundle source is transcribed
// here, only the request/response shapes it revealed).
//
// robots.txt permits it. api.globalcomix.com's robots.txt (identical to the
// main site's) disallows only /admin/, /my/, /forums/subscribe/ and the
// parameterised forms of /downloads and /browse; nothing under /v1/ is
// mentioned, and there is no challenge or bot-detection response anywhere in
// this flow — every 4xx encountered while mapping it out was an ordinary,
// stable, machine-readable application error (see api.go's clientHeaderName
// comment), not a refusal to be worked around. PLAN §7.6 is not in tension
// with anything here.
//
// # This is one site, not a family — like mangadex
//
// See mangadex's package doc for the fuller version of this argument; it
// applies here without much change. GlobalComix is one deployment with one
// API, so Fingerprint gates on the registrable domain rather than reading
// markup class names, and Validate refuses any baseUrl outside that same
// registrable domain, for the same reason mangadex's does: a theme with one
// true home should not accept an arbitrary host that happens to answer
// similar JSON. Unlike mangadex, Validate accepts either host on that
// domain — the browser-facing site as well as the API — because endpoint()
// (see normaliseAPIHost) rewrites every request to the API host regardless
// of which one a source names.
//
// # The client header and the reading-grant cookie
//
// Every request this theme sends needs a header naming the client
// application, and the one call that reads a chapter's pages needs, in
// addition, a short-lived cookie the API itself issues for exactly that
// purpose. Both are opt-in side interfaces on theme.Theme built for exactly
// this shape — theme.SourceHeaders and theme.CookieUser — and Theme below
// implements both: SourceHeaders attaches the client header to every request
// theme.PolicyFor builds a Policy for, and UsesCookies gives the source a
// fetch.CookieJar so the cookie the reading grant sets is presented back on
// the page-image fetches that follow. See clientHeaderName and
// defaultClientHeaderValue in api.go for the header's value and what happens
// if it ever rotates, and readerCDNAccess's comment for the cookie.
package globalcomix

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "globalcomix"

// The site, as constants rather than configuration — a single-site theme has
// nothing to configure here, and PLAN §1.3's ban on a shipped catalogue is
// about a list of sources, not the one host a single-site theme necessarily
// names to drive itself (mangadex does the same for api.mangadex.org).
const (
	apiHost    = "api.globalcomix.com"
	siteDomain = "globalcomix.com"
)

// searchPerPage is how many results one page of Search asks for. GlobalComix's
// own web client asks for its default page size; there is no documented
// maximum, so this is a conservative, unremarkable number rather than a
// measured ceiling.
const searchPerPage = 20

// Override keys.
const (
	// KeyPageQuality selects which rendition readerCDNAccess.URLTemplate's
	// {quality} placeholder is filled with. GlobalComix's reader offers
	// "desktop" and "mobile"; "desktop" is the default for the same reason
	// mangadex takes full-quality pages over its data-saver set — M4 resizes
	// and re-encodes for the device anyway, so starting from an
	// already-downscaled image only compounds the loss.
	KeyPageQuality = "pageQuality"
)

var pageQualities = []string{"desktop", "mobile"}

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key:     KeyPageQuality,
			Kind:    theme.KindString,
			Default: "desktop",
			Enum:    pageQualities,
			Why: "GlobalComix's reader serves each page at a desktop or mobile rendition. " +
				"The default matches what the site's own reader shows by default; " +
				"\"mobile\" is here for a metered connection, not because it reads better.",
		},
		{
			Key:     clientHeaderOverride,
			Kind:    theme.KindString,
			Default: "",
			Why: "Every request needs a header naming the client application " +
				"(X-Gc-Client); the built-in value is read from the site's own " +
				"public, unauthenticated bootstrap config and is not a secret. " +
				"It can rotate on GlobalComix's own schedule, independent of any " +
				"Quire release; when it does, every request starts failing with " +
				"the site's own CLIENT_HEADER_MISSING error, and this override " +
				"lets a user paste in the replacement value without waiting.",
		},
	},
}

// Theme is the globalcomix theme.
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
// This is the theme that interface exists for, alongside mangadex: Pages()
// asks for one release's reading grant as retrieval, because the user opened
// that chapter. A watch check never opens a chapter, but "never reaches it
// today" is not a property worth betting the exception's narrowness on.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// SuggestedName implements theme.Theme.
//
// One site, and one that knows its own name — without this a source added at
// the API host would default to whatever that host's own page <title> says,
// exactly the "MangaDex API documentation" problem mangadex's SuggestedName
// exists to avoid.
func (t *Theme) SuggestedName() string { return "GlobalComix" }

// SourceHeaders implements theme.SourceHeaders: the client-identifier header
// every endpoint on this API requires (CLIENT_HEADER_MISSING otherwise). See
// clientHeaderName and defaultClientHeaderValue in api.go for what the value
// is and how a rotated one is handled.
//
// It never returns an error itself; a malformed override was already rejected
// by ValidateOverrides before a source could reach this call, so
// clientHeaderFor's error return is not expected to trigger here. Failing
// open to the built-in default rather than sending no header at all would
// turn a validation bug into every request being refused instead, which is
// the worse of the two failures to have chosen silently.
func (t *Theme) SourceHeaders(s *theme.Source) http.Header {
	v, err := clientHeaderFor(s)
	if err != nil {
		v = defaultClientHeaderValue
	}
	h := http.Header{}
	h.Set(clientHeaderName, v)
	return h
}

// UsesCookies implements theme.CookieUser.
//
// Every source of this theme needs the jar: readV3 (Pages' request) is the
// call that receives the reading-grant's Set-Cookie, and the reader-CDN
// requests that follow need it presented back. There is no source for which
// this theme's cookie need is optional, unlike CookieUser's doc comment
// example of a theme that might want one per source — s is accepted only to
// match that interface shape.
func (t *Theme) UsesCookies(s *theme.Source) bool { return true }

// AllowedHosts implements theme.Theme.
//
// Unlike mangadex, this is nil. Every host this theme's responses point at —
// the API, the reader-CDN, the cover-image path — is under globalcomix.com,
// the API's own registrable domain, so PLAN §7.4's guard already permits all
// of it without widening. Checked, not assumed: see the reader_cdn_access and
// image_url comments in api.go and globalcomix.go for where each URL comes
// from.
func (t *Theme) AllowedHosts() []string { return nil }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// Fingerprint scores a probed page for "is this GlobalComix?".
//
// Like mangadex, the first signal is a gate rather than a weight: GlobalComix
// is one deployment, so the question is "is this host GlobalComix", not
// "which theme's markup does this page carry". A page from any other
// registrable domain scores exactly 0, whatever else it contains.
//
// Given the gate, the remaining signals are read from the SPA shell every
// route serves — there is no server-rendered markup to speak of, which is
// exactly why the earlier batch probe found nothing here. Measured live
// 2026-09-22 against the real prober's registry: the browser-facing home page
// (https://globalcomix.com/) scores 90, a series page on the same host scores
// 90, and api.globalcomix.com's own root — which answers the identical shell —
// scores 100. Every other registered theme scores 0 on all three, since none
// of their signals appear anywhere in this page.
func (t *Theme) Fingerprint(p *probe.Page) int {
	if p == nil || p.URL == nil {
		return 0
	}
	u := p.FinalURL
	if u == nil {
		u = p.URL
	}
	if !strings.EqualFold(fetch.RegistrableDomain(u.Hostname()), siteDomain) {
		return 0
	}

	score := 30
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	// The API host itself, which is what a source must actually point at for
	// this theme to work at all (see Validate).
	add(strings.EqualFold(u.Hostname(), apiHost), 25)

	// window.gc.global is the inline script every route's shell carries: the
	// client's own config, including the api_url and api_key this theme's
	// requests would need. It is GlobalComix-specific in a way no other site's
	// bootstrap script would coincidentally match.
	add(p.Contains("window.gc.global"), 25)

	// The mobile app's package ID, in a <meta name="google-play-app"> tag on
	// every route.
	add(p.Contains("com.globalcomix.mobileapp"), 15)

	// The static-asset host named in window.gc.global and in every image `src`
	// the shell's own chrome uses (icons, fonts) before any content loads.
	add(p.Contains("assets.globalcomix.com"), 10)

	// The <title> on every route ends with "on GlobalComix" or is simply
	// "GlobalComix - …"; weak alone, useful as a tie-breaker.
	add(strings.Contains(strings.ToLower(p.Title()), "globalcomix"), 10)

	if score > 100 {
		score = 100
	}
	return score
}

// Search implements theme.Theme.
//
// Discovery: the user is asking what exists. GlobalComix's own search
// endpoint doubles as its browse listing — an empty q with context=series is
// exactly what the site's own "Explore" page sends — so PLAN §12.1's "search
// with an empty query is browse" costs nothing extra to support here.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	v := url.Values{}
	if q = strings.TrimSpace(q); q != "" {
		v.Set("q", q)
	}
	v.Set("context", "series")
	v.Set("p", strconv.Itoa(page))
	v.Set("perpage", strconv.Itoa(searchPerPage))

	res, err := getJSON[searchResults](ctx, t, t.f, s, t.endpoint(s, "/v1/search/query", v))
	if err != nil {
		return nil, err
	}

	out := make([]theme.SeriesStub, 0, len(res.Series.Items))
	for _, it := range res.Series.Items {
		if it.ID == 0 || strings.TrimSpace(it.Name) == "" {
			continue
		}
		out = append(out, theme.SeriesStub{
			ID:       seriesID(it.ID),
			Title:    strings.TrimSpace(it.Name),
			CoverURL: it.CoverImageURL,
			Authors:  authorsOf(it.ArtistName),
		})
	}
	return out, nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	numID, err := parseSeriesID(id)
	if err != nil {
		return nil, err
	}
	res, err := getJSON[seriesResult](ctx, t, t.f, s, t.endpoint(s, "/v1/comics/"+strconv.Itoa(numID), nil))
	if err != nil {
		return nil, err
	}
	names := authorsOf(res.Artist.displayName())
	return &theme.Series{
		ID:          seriesID(res.ID),
		Title:       strings.TrimSpace(res.Name),
		CoverURL:    res.ImageURL,
		Description: strings.TrimSpace(res.Description),
		Authors:     names,
		Artists:     names,
		Genres:      genresOf(res.CategoryName),
		Status:      status(res.StatusName),
	}, nil
}

// Chapters implements theme.Theme.
//
// "all=true" is what the site's own release list asks for — every release, one
// request, rather than the paginated form the reader screen uses for a series
// with hundreds of issues. That trade is the right one for Quire: a chapter
// list is read once and cached, where the app re-fetches it on every visit.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	numID, err := parseSeriesID(id)
	if err != nil {
		return nil, err
	}
	v := url.Values{}
	v.Set("all", "true")
	releases, err := getJSON[[]release](ctx, t, t.f, s, t.endpoint(s, fmt.Sprintf("/v1/comics/%d/releases", numID), v))
	if err != nil {
		return nil, err
	}

	out := make([]theme.Chapter, 0, len(releases))
	for _, rel := range releases {
		if rel.Key == "" {
			continue
		}
		out = append(out, theme.Chapter{
			ID:        releaseID(rel.Key),
			Title:     chapterTitle(rel),
			Number:    chapterNumber(rel),
			Published: parsePublished(rel.PublishedTime),
			Scanlator: rel.Artist.displayName(),
		})
	}
	// PLAN §7.2: ascending reading order. GlobalComix's own "order" field is
	// already ascending on every release list seen live, so this is very
	// nearly a no-op — it runs anyway because SortAndMark is the theme's
	// promise, not an observation about one API response, and because Number
	// here comes from parsing "chapter" as text rather than trusting Order
	// directly, which is worth a real check rather than an assumption.
	return theme.SortAndMark(out), nil
}

// Pages implements theme.Theme.
//
// This is the retrieval call: the user opened a specific chapter and asked to
// read it, which /v1/readV3/{key} answers with a short-lived reading grant
// (page count, per-page metadata, and a template for the reader-CDN's image
// URLs). PLAN §7.4 classifies it as retrieval for the same reason mangadex's
// /at-home/server call is: it is not crawling, it is one page a person is
// reading right now, and robots.txt does not gate it either way (nothing
// under /v1/ is disallowed).
//
// The reader-CDN also requires a session cookie that only this readV3
// response itself can set (see readerCDNAccess's doc comment in api.go), so
// this request carries the source's theme.CookieUser jar exactly like every
// other call getJSONRetrieval makes: the Set-Cookie it receives here is what
// the page-image request that follows must present.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	key, err := parseReleaseID(chapterID)
	if err != nil {
		return nil, err
	}
	res, err := getJSONRetrieval[readResult](ctx, t, t.f, s, t.endpoint(s, "/v1/readV3/"+key, nil))
	if err != nil {
		return nil, err
	}
	grant := res.ReaderCDNAccess
	if grant == nil || grant.BaseURL == "" || grant.URLTemplate == "" {
		return nil, fmt.Errorf("globalcomix: release %s: readV3 carried no reading grant", key)
	}
	n := len(res.PageObjects)
	if n == 0 {
		n = grant.PageCount
	}
	if n <= 0 {
		return nil, fmt.Errorf("globalcomix: release %s: no pages", key)
	}

	ov, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	quality := ov.String(KeyPageQuality)
	if quality == "" {
		quality = "desktop"
	}

	out := make([]string, 0, n)
	for order := 1; order <= n; order++ {
		out = append(out, fillTemplate(grant.URLTemplate, grant.BaseURL, grant.ReleaseKey, order, quality))
	}
	return out, nil
}

// fillTemplate substitutes readerCDNAccess.URLTemplate's placeholders.
// Observed live shape: "{base_url}/r/{release_key}/p/{order}/{quality}.webp".
// Substitution rather than a fixed format string, so a template the API
// reorders or extends in future still resolves correctly as long as its
// placeholder names do not change.
func fillTemplate(tmpl, baseURL, releaseKey string, order int, quality string) string {
	r := strings.NewReplacer(
		"{base_url}", strings.TrimRight(baseURL, "/"),
		"{release_key}", releaseKey,
		"{order}", strconv.Itoa(order),
		"{quality}", quality,
	)
	return r.Replace(tmpl)
}

// endpoint builds an absolute API URL from the source's base.
//
// The base is the source's, not the apiHost constant, for the same reason
// mangadex's endpoint helper works this way: PLAN §6 M2's offline tests run
// against example.invalid, and a theme that hard-coded its host could only be
// tested by going online. Validate below is what keeps a real source on the
// site's registrable domain in the first place; normaliseAPIHost, next, is
// what keeps every request actually reaching the API host regardless of
// which host on that domain the source names.
//
// normaliseAPIHost is applied to the base first. This is the fix for a source
// added by pasting the site's own, natural URL — https://globalcomix.com —
// rather than the API host the theme actually drives, https://api.globalcomix.com:
// the fingerprint correctly recognises the site on either host (Fingerprint's
// gate is the registrable domain), but until now only a source already
// pointed at the API host actually worked, leaving the natural URL fingerprinted
// yet functionally broken. Doing the correction here, rather than requiring
// the API host at Validate time, means every call this theme makes —
// including the PLAN §7.5 stage 5 capability check that runs before a source
// is ever stored — reaches the right host regardless of which of the two the
// source names, and a source may be stored under either name.
func (t *Theme) endpoint(s *theme.Source, path string, v url.Values) string {
	base := normaliseAPIHost(strings.TrimRight(strings.TrimSpace(s.BaseURL), "/"))
	u := base + path
	if len(v) > 0 {
		u += "?" + v.Encode()
	}
	return u
}

// normaliseAPIHost rewrites base to apiHost when its host is on the site's
// registrable domain but is not already apiHost. A base whose host is not on
// siteDomain at all — in particular *.invalid, which PLAN §6 M2's offline
// tests use as their base — is returned unchanged: this only resolves the one
// confusion this site invites (its own domain vs. its API subdomain), not a
// general host substitution that would make an offline test's fixed host
// silently drift to something else.
func normaliseAPIHost(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	host := u.Hostname()
	if strings.EqualFold(host, apiHost) || !strings.EqualFold(fetch.RegistrableDomain(host), siteDomain) {
		return base
	}
	u.Host = apiHost
	return strings.TrimRight(u.String(), "/")
}

// authorsOf wraps a single display name as the []string theme.SeriesStub and
// theme.Series expect, or nil for an empty one — GlobalComix names one artist
// per series/release rather than a byline list, so there is never more than
// one entry.
func authorsOf(name string) []string {
	if name = strings.TrimSpace(name); name == "" {
		return nil
	}
	return []string{name}
}

// genresOf wraps GlobalComix's single category as theme.Series.Genres. The API
// also carries comic_facets, but a facet's own label is only present when the
// request asked for it to be enriched, and category_name is the one genre-like
// field every series response already carries.
func genresOf(category string) []string {
	if category = strings.TrimSpace(category); category == "" {
		return nil
	}
	return []string{category}
}

// status normalises GlobalComix's status_name to theme's enum. An unrecognised
// value becomes theme.StatusUnknown rather than being passed through, per
// theme.Series.Status's contract.
func status(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ongoing":
		return theme.StatusOngoing
	case "completed", "complete":
		return theme.StatusCompleted
	case "hiatus":
		return theme.StatusHiatus
	case "cancelled", "canceled":
		return theme.StatusCancelled
	default:
		return theme.StatusUnknown
	}
}

// chapterTitle prefers the release's own title, falling back to its chapter
// number when the release was never given one — GlobalComix does not require
// a title, and many one-shots and episodic releases have none.
func chapterTitle(rel release) string {
	if t := strings.TrimSpace(rel.Title); t != "" {
		return t
	}
	if c := strings.TrimSpace(rel.Chapter); c != "" {
		return "Chapter " + c
	}
	return "Untitled"
}

// chapterNumber reads the release's own "chapter" field, which is already the
// plain number GlobalComix displays — no decorated title to parse a number out
// of, unlike madara or mangathemesia. theme.ChapterNumber is reused rather than
// strconv directly so a value the site ever decorates ("1.5b") degrades to its
// leading number instead of failing outright.
func chapterNumber(rel release) float64 {
	if strings.TrimSpace(rel.Chapter) == "" {
		return theme.ChapterNumber(rel.Title)
	}
	return theme.ChapterNumber(rel.Chapter)
}

// publishedLayout is GlobalComix's own timestamp shape, observed live on every
// release: "2026-08-07 10:43:32", UTC, space-separated rather than RFC 3339.
const publishedLayout = "2006-01-02 15:04:05"

// parsePublished parses GlobalComix's timestamp, or returns the zero time for
// one it cannot read — an unreadable date leaves a chapter with an unset
// Published rather than failing the whole list (PLAN §7.2).
func parsePublished(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	tm, err := time.Parse(publishedLayout, raw)
	if err != nil {
		return time.Time{}
	}
	return tm.UTC()
}

// Validate implements theme.SourceValidator.
//
// One site has to say which host it is. Without this, "theme": "globalcomix"
// with any baseUrl would be accepted, and an imported sources file could point
// Quire's GlobalComix support at a host that merely answers similarly-shaped
// JSON.
//
// Any host on siteDomain's registrable domain is accepted, including the
// site domain itself (https://globalcomix.com), not only apiHost. This used
// to be narrower — a non-API host on the same domain was rejected — but
// endpoint() (see normaliseAPIHost) now rewrites every request to apiHost
// regardless of which of the two a source names, so a stored baseUrl that
// names the site rather than the API is a cosmetic difference, not a
// functional one: every request still reaches the real API. What still has
// to be rejected is a host outside this domain entirely, which
// normaliseAPIHost leaves untouched and which would otherwise let a source
// point this theme's requests at an unrelated host that merely answers
// similarly-shaped JSON.
func (t *Theme) Validate(s *theme.Source) error {
	u, err := url.Parse(strings.TrimSpace(s.BaseURL))
	if err != nil {
		return fmt.Errorf("theme %s: source %q: parse baseUrl: %w", ID, s.ID, err)
	}
	host := u.Hostname()
	if strings.EqualFold(fetch.RegistrableDomain(host), siteDomain) {
		return nil
	}
	// Allowed only for the offline tests (PLAN §6 M2); .invalid can never
	// resolve (RFC 2606).
	if strings.HasSuffix(strings.ToLower(host), ".invalid") {
		return nil
	}
	return fmt.Errorf(
		"theme %s: source %q: baseUrl is %q; this theme drives one site, so it must be on %s",
		ID, s.ID, s.BaseURL, siteDomain)
}
