// Package mangadex implements MangaDex's public JSON API as a Quire theme.
//
// # This is a different animal from the other themes
//
// PLAN §7.2 defines a theme as "a Go implementation of one site *family's*
// shape". madara and mangathemesia are exactly that: one WordPress plugin or
// theme, deployed across hundreds of independent sites, recognised from its
// markup and configured per site with `overrides` because every deployment
// renames something.
//
// MangaDex is one site. There is no family, no second deployment, and nothing
// to vary. What it has instead is a documented, versioned, unauthenticated
// JSON API with published rate limits, which makes it the *easiest* target in
// the catalogue and the one worth doing first when a real live test is needed.
//
// It still implements theme.Theme, because a theme with exactly one instance
// costs nothing and keeps one interface for the probe, the browse UI and the
// download queue to speak to. The consequences of being a single site rather
// than a family show up in three places, all of them noted where they happen:
//
//   - Fingerprint keys on the API host and its response shape rather than on
//     page markup, and scores 0 for everything else (there is nothing else it
//     could be).
//   - `overrides` are few and are about *what to show*, not about where the
//     site put something.
//   - The language handling is real work rather than a formality. MangaDex is
//     multilingual by design: a series carries titles in a dozen languages and
//     chapters in as many, and a source configured for "en" that returns
//     Portuguese chapters is broken.
//
// # Provenance
//
// Written from the official documentation at https://api.mangadex.org/docs/
// and from responses observed live on 2026-09-15. PLAN §1.4: endpoint shapes
// are facts, expression is not copied. No fixture here is a recording (§1.3).
package mangadex

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "mangadex"

// The site, as constants rather than as configuration. A single-site theme has
// nothing to configure here, and a `baseUrl` that could be pointed at an
// arbitrary host would be a way to aim Quire's MangaDex support at something
// that is not MangaDex.
const (
	// apiHost is where the API lives.
	apiHost = "api.mangadex.org"

	// siteDomain is the registrable domain the fingerprint accepts. The
	// browser-facing site and the API share it.
	siteDomain = "mangadex.org"
)

// Paging limits, from the documented maxima.
//
// The API caps `limit` at 100 on /manga and at 500 on a manga feed, and
// refuses an offset past 10000. Asking for the maximum is the polite choice
// here rather than the greedy one: one 500-chapter request is one request,
// where five 100-chapter ones are five, and §7.4's floor spaces them 2s apart.
const (
	searchLimit = 20
	feedLimit   = 500
	maxOffset   = 10000
)

// maxFeedRequests bounds a chapter list. 20 × 500 is 10 000 chapters, which is
// past the API's own offset ceiling; the bound exists so a paging bug becomes
// a wrong answer rather than an unbounded loop on a device with no swap.
const maxFeedRequests = 20

// Override keys. Both are about what the user wants to see, which is the only
// kind of variation a single site has.
const (
	// KeyMaxContentRating is the highest content rating to include. MangaDex
	// tags every series, and its own default is safe+suggestive; Quire makes
	// the choice explicit rather than inheriting a default that could change.
	KeyMaxContentRating = "maxContentRating"

	// KeyIncludeExternal controls whether chapters hosted on a publisher's own
	// site are listed. They appear in the feed with an externalUrl and no
	// pages, and Quire cannot read them — listing them by default would offer
	// the user chapters that fail at download. On by request only, for someone
	// who wants the list to match the website's.
	KeyIncludeExternal = "includeExternal"
)

// contentRatings is the API's rating scale, weakest first. The override names
// a maximum and this slice turns it into the set to send.
var contentRatings = []string{"safe", "suggestive", "erotica", "pornographic"}

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key:     KeyMaxContentRating,
			Kind:    theme.KindString,
			Default: "suggestive",
			Enum:    contentRatings,
			Why: "MangaDex rates every series and filters on request. This names the " +
				"highest rating to include; the default matches the site's own.",
		},
		{
			Key:     KeyIncludeExternal,
			Kind:    theme.KindBool,
			Default: false,
			Why: "Some chapters are hosted on the publisher's site and MangaDex serves no " +
				"images for them. They are hidden by default because Quire cannot download " +
				"one, and offering a chapter that cannot be read is worse than omitting it.",
		},
	},
}

// Theme is the mangadex theme.
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

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// imageCDN is where MangaDex serves page images from.
//
// /at-home/server/{id} returns a base URL on a host like
// "cmdxd98sb0x3yprd.mangadex.network" — a generated label on a *different
// registrable domain* from the API. Without this declaration, §7.4's redirect
// boundary refuses every page image, so M4 could list a chapter and download
// none of it, and the user's only clue would be an SSRF rejection naming a
// host they have never heard of.
//
// The wildcard form is the honest one. The bare domain serves nothing; only
// the generated subdomains do, and saying so keeps the widening as narrow as
// the facts are.
//
// Covers are not here on purpose: uploads.mangadex.org is under the same
// registrable domain as the API, so the guard already permits it and naming it
// would imply a widening that is not happening.
const imageCDN = "*.mangadex.network"

// SuggestedName implements theme.Theme.
//
// The one theme that can answer this, because it drives exactly one site.
// Without it a new source is named from the API root's <title> and comes out
// as "MangaDex API documentation".
func (t *Theme) SuggestedName() string { return "MangaDex" }

// AllowedHosts implements theme.Theme.
func (t *Theme) AllowedHosts() []string { return []string{imageCDN} }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// Fingerprint scores a probed page for "is this MangaDex?".
//
// Every other theme fingerprints markup, because every other theme is a family
// of sites that share a plugin. This one has no markup to read: the thing it
// drives is an API that answers JSON, and the question is not "which theme is
// this site running" but "is this host MangaDex".
//
// So the first signal is a gate rather than a weight. Unless the page came
// from mangadex.org, the answer is 0 — not a low score, zero — and no
// combination of the remaining signals can lift it. That is what stops a
// perfectly ordinary JSON API elsewhere, or a site that sets `Server:
// MangaDex` out of mischief, from scoring at all. A single-site theme that
// matched on shape alone would be a liability, since "returns JSON with a
// result field" describes half the web.
//
// Given the gate, the rest are confirmation, and they are chosen to be cheap
// and structural in §7.5's sense: the host label, a response header the API
// sets on every response, and the envelope every endpoint shares.
//
// Scores land where §7.5's threshold of 60 expects them: the API root reaches
// 100, the browser-facing host 80, and everything else exactly 0.
func (t *Theme) Fingerprint(p *probe.Page) int {
	if p == nil || p.URL == nil {
		return 0
	}
	// The final URL is what matters: api.mangadex.org/ answers 308 to /docs/,
	// and a redirect that *left* mangadex.org would mean this is not MangaDex
	// however the user typed it.
	u := p.FinalURL
	if u == nil {
		u = p.URL
	}
	if !strings.EqualFold(fetch.RegistrableDomain(u.Hostname()), siteDomain) {
		return 0
	}

	score := 40
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	// The API host itself, which is what a source must point at.
	add(strings.EqualFold(u.Hostname(), apiHost), 25)

	// MangaDex sets this on every response from its edge, including the 308 at
	// the root and the four-byte /ping. It is not proof on its own — hence the
	// gate above — but paired with the host it is decisive.
	add(strings.EqualFold(strings.TrimSpace(p.Header.Get("Server")), "MangaDex"), 20)

	// The shared envelope, or the two non-JSON endpoints that stand in for it
	// when the probe lands on the root.
	body := strings.TrimSpace(p.HTML())
	add(strings.Contains(body, `"result":"ok"`) ||
		strings.Contains(body, `"result": "ok"`) ||
		body == "pong" ||
		strings.Contains(strings.ToLower(u.Path), "/docs"), 15)

	if score > 100 {
		score = 100
	}
	return score
}

// Search implements theme.Theme.
//
// Discovery, unambiguously: the user is asking what exists, not for a thing
// they named. robots.txt allows /manga, and would be honoured here if it did
// not.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * searchLimit
	if offset >= maxOffset {
		// Past the API's own ceiling. An empty page is the truthful answer;
		// a request would earn a 400 and say the same thing more slowly.
		return nil, nil
	}

	v := url.Values{}
	if q = strings.TrimSpace(q); q != "" {
		v.Set("title", q)
	}
	v.Set("limit", strconv.Itoa(searchLimit))
	v.Set("offset", strconv.Itoa(offset))
	// Without this the cover is a bare UUID relationship and every result
	// would need its own request to render a thumbnail.
	v.Add("includes[]", "cover_art")
	t.addContentRating(s, v)
	if lang := s.Lang; lang != "" {
		// Not the same filter as the chapter one: this asks for series that
		// have *any* chapter in the user's language, so a search does not
		// offer them a series they will find empty when they open it.
		v.Add("availableTranslatedLanguage[]", lang)
	}

	var col collection
	if err := t.getJSON(ctx, s, t.endpoint(s, "/manga", v), &col); err != nil {
		return nil, err
	}

	out := make([]theme.SeriesStub, 0, len(col.Data))
	for _, e := range col.Data {
		var a mangaAttributes
		if unmarshal(e.Attributes, &a) != nil {
			continue
		}
		title := bestTitle(a, s.Lang)
		if e.ID == "" || title == "" {
			continue
		}
		out = append(out, theme.SeriesStub{
			ID:       seriesID(e.ID),
			Title:    title,
			CoverURL: t.coverURL(s, e),
		})
	}
	return out, nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	uuid, err := parseID(id, seriesPrefix)
	if err != nil {
		return nil, err
	}

	v := url.Values{}
	for _, inc := range []string{"cover_art", "author", "artist"} {
		v.Add("includes[]", inc)
	}

	var res single
	if err := t.getJSON(ctx, s, t.endpoint(s, "/manga/"+uuid, v), &res); err != nil {
		return nil, err
	}
	var a mangaAttributes
	if err := unmarshal(res.Data.Attributes, &a); err != nil {
		return nil, fmt.Errorf("mangadex: series %s: %w", uuid, err)
	}

	prefer := []string{s.Lang, "en", romanised(a.OriginalLanguage), a.OriginalLanguage}
	title := bestTitle(a, s.Lang)

	return &theme.Series{
		ID:          seriesID(res.Data.ID),
		Title:       title,
		AltTitles:   altTitles(a.AltTitles, title),
		CoverURL:    t.coverURL(s, res.Data),
		Description: pick(a.Description, prefer...),
		Authors:     res.Data.names("author"),
		Artists:     res.Data.names("artist"),
		Genres:      genres(a.Tags, prefer),
		Status:      status(a.Status),
	}, nil
}

// Chapters implements theme.Theme.
//
// The language filter is the point of this method. MangaDex's feed returns
// every translation of every chapter unless told otherwise, so a source
// configured for "en" that did not send translatedLanguage[] would hand the
// user a list in a dozen languages with duplicate numbers — which is not a
// cosmetic problem, because the download queue would then fetch whichever one
// happened to sort first.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	uuid, err := parseID(id, seriesPrefix)
	if err != nil {
		return nil, err
	}
	ov, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	includeExternal := ov.Bool(KeyIncludeExternal)

	var out []theme.Chapter
	for req, offset := 0, 0; req < maxFeedRequests; req++ {
		v := url.Values{}
		v.Set("limit", strconv.Itoa(feedLimit))
		v.Set("offset", strconv.Itoa(offset))
		// Ascending by chapter, so the order the user sees is reading order
		// and not upload order. MangaDex sorts numerically here; Quire does
		// not re-sort, because a site's own view of "what comes next" is more
		// reliable than parsing decorated chapter strings.
		v.Set("order[chapter]", "asc")
		v.Add("includes[]", "scanlation_group")
		t.addContentRating(s, v)
		if s.Lang != "" {
			v.Add("translatedLanguage[]", s.Lang)
		}

		var col collection
		if err := t.getJSON(ctx, s, t.endpoint(s, "/manga/"+uuid+"/feed", v), &col); err != nil {
			return nil, err
		}
		if len(col.Data) == 0 {
			break
		}
		for _, e := range col.Data {
			var a chapterAttributes
			if unmarshal(e.Attributes, &a) != nil {
				continue
			}
			// A second, local check of the filter we asked the server for. If
			// the parameter is ever dropped in a refactor the list goes empty
			// rather than silently multilingual, and an empty list is a bug
			// someone notices.
			if s.Lang != "" && !strings.EqualFold(a.TranslatedLanguage, s.Lang) {
				continue
			}
			if a.IsUnavailable {
				continue
			}
			if a.ExternalURL != "" && !includeExternal {
				continue
			}
			out = append(out, theme.Chapter{
				ID:        chapterID(e.ID),
				Title:     chapterTitle(a),
				Volume:    strings.TrimSpace(a.Volume),
				Number:    theme.ChapterNumber(a.Chapter),
				Published: parseTime(a.PublishAt),
				Scanlator: firstName(e, "scanlation_group"),
			})
		}
		offset += len(col.Data)
		if offset >= col.Total || offset >= maxOffset {
			break
		}
	}
	// PLAN §7.2: ascending reading order — and normalised here rather than
	// trusted from the server.
	//
	// The request above asks for order[chapter]=asc and MangaDex honours it,
	// so this is very nearly a no-op. It runs anyway, for three reasons that
	// are not hypothetical: the pages are concatenated in *request* order and
	// a retry could reorder them, MangaDex sorts "12.5" between 12 and 13 but
	// is entitled to change its mind about that, and a chapter with no number
	// has no defined position in its sort at all. The cost is one pass over a
	// slice we already own; the failure it prevents is a volume PDF that reads
	// backwards, which nothing downstream would notice.
	return theme.SortAndMark(out), nil
}

// Pages implements theme.Theme.
//
// This is the retrieval call, and the one that forced PLAN §7.4's
// discovery/retrieval distinction into existence.
//
// api.mangadex.org publishes exactly two lines of robots.txt:
//
//	User-agent: *
//	Disallow: /at-home/
//
// Everything used to *find* a chapter — /manga, /manga/{id}, the feeds — is
// allowed, and Quire fetches all of it as discovery above. The single
// disallowed prefix is the endpoint that hands out the page images, which is
// only ever reached because the user opened a chapter and asked for it. Under
// a blanket robots rule Quire could search MangaDex and list its chapters and
// then refuse to read one, which is not a reading of that file any operator
// intended; RFC 9309 scopes robots to crawlers, and this is not crawling.
//
// Nothing else is relaxed. This request is rate limited, guarded, capped and
// accounted exactly like every other, and carries the same honest User-Agent.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	uuid, err := parseID(chapterID, chapterPrefix)
	if err != nil {
		return nil, err
	}

	var res atHomeResponse
	if err := t.getJSONRetrieval(ctx, s, t.endpoint(s, "/at-home/server/"+uuid, nil), &res); err != nil {
		return nil, err
	}
	if res.BaseURL == "" || res.Chapter.Hash == "" {
		return nil, fmt.Errorf("mangadex: chapter %s: at-home response carried no image host", uuid)
	}

	// Full quality. MangaDex also publishes a "data-saver" set of smaller,
	// recompressed JPEGs at /data-saver/; Quire takes the originals because M4
	// resizes and re-encodes for the device anyway, and starting from an
	// already-degraded JPEG would compound the loss. See docs/THEME-NOTES.md —
	// data-saver is a plausible future option for a metered connection, not a
	// better default.
	names := res.Chapter.Data
	if len(names) == 0 {
		return nil, fmt.Errorf("mangadex: chapter %s: no pages", uuid)
	}

	base := strings.TrimRight(res.BaseURL, "/")
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		out = append(out, base+"/data/"+res.Chapter.Hash+"/"+n)
	}
	return out, nil
}

// endpoint builds an absolute API URL from the source's base.
//
// The base is the source's, not a constant, for one reason: the offline tests
// run against example.invalid (PLAN §6 M2), and a theme that hard-coded its
// host could only be tested by going online. Validate below is what keeps a
// real source pointed at the real API.
func (t *Theme) endpoint(s *theme.Source, path string, v url.Values) string {
	base := strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	u := base + path
	if len(v) > 0 {
		u += "?" + v.Encode()
	}
	return u
}

// addContentRating turns the maxContentRating override into the set of
// contentRating[] values the API expects. An unreadable override is not worth
// failing a search over, so it falls back to the documented default.
func (t *Theme) addContentRating(s *theme.Source, v url.Values) {
	max := "suggestive"
	if ov, err := spec.Resolve(s.Overrides); err == nil {
		if got := ov.String(KeyMaxContentRating); got != "" {
			max = got
		}
	}
	for _, r := range contentRatings {
		v.Add("contentRating[]", r)
		if r == max {
			return
		}
	}
}

// coverURL builds the cover image URL from the cover_art relationship.
//
// The filename alone is useless: covers live on a separate uploads host, under
// the manga's own UUID. The host is derived from the API host by swapping the
// leading label, which is both what MangaDex does (api. -> uploads.) and what
// keeps the offline fixtures working on a host that has no such label.
func (t *Theme) coverURL(s *theme.Source, e entity) string {
	rel, ok := e.find("cover_art")
	if !ok || len(rel.Attributes) == 0 {
		return ""
	}
	var a coverAttributes
	if unmarshal(rel.Attributes, &a) != nil || a.FileName == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(s.BaseURL))
	if err != nil {
		return ""
	}
	if host, ok := strings.CutPrefix(u.Hostname(), "api."); ok {
		u.Host = "uploads." + host
	}
	u.Path = "/covers/" + e.ID + "/" + a.FileName
	u.RawQuery = ""
	return u.String()
}

// Validate implements theme.SourceValidator.
//
// A single-site theme has to say which site. Without this, `"theme":
// "mangadex"` with any baseUrl at all would be accepted, and an imported
// sources file could point Quire's MangaDex support at a host that answers
// MangaDex-shaped JSON and is not MangaDex — which is a far more interesting
// thing to be wrong about than a typo.
func (t *Theme) Validate(s *theme.Source) error {
	u, err := url.Parse(strings.TrimSpace(s.BaseURL))
	if err != nil {
		return fmt.Errorf("theme %s: source %q: parse baseUrl: %w", ID, s.ID, err)
	}
	host := u.Hostname()
	if strings.EqualFold(fetch.RegistrableDomain(host), siteDomain) {
		if !strings.EqualFold(host, apiHost) {
			return fmt.Errorf(
				"theme %s: source %q: baseUrl is %q, but this theme drives the API; use https://%s",
				ID, s.ID, s.BaseURL, apiHost)
		}
		return nil
	}
	// Anything else is allowed only because the offline tests need it, and is
	// spelled out rather than silently tolerated: a real source must be the
	// real host, and .invalid can never resolve (RFC 2606).
	if strings.HasSuffix(strings.ToLower(host), ".invalid") {
		return nil
	}
	return fmt.Errorf(
		"theme %s: source %q: baseUrl is %q; this theme drives one site, so it must be https://%s",
		ID, s.ID, s.BaseURL, apiHost)
}
