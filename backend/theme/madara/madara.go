// Package madara implements the WordPress `wp-manga` family — the Madara
// plugin — which PLAN §7.3 names as tier 1 and "the largest family by a wide
// margin".
//
// What this package encodes is the *shape of the markup the plugin generates*.
// That shape is an observable fact about a commercially distributed WordPress
// plugin, not anyone's expression: the plugin emits a `wp-manga` custom post
// type, renders chapter lists into `li.wp-manga-chapter`, and exposes a
// `manga_get_chapters` admin-ajax action. PLAN §1.4 governs — the selectors,
// the fingerprint weights and the parsing below are written from scratch
// against that shape; nothing is transcribed from another project.
//
// Two facts from PLAN §7.3 drive the design:
//
//   - "Chapter list often behind an admin-ajax.php POST rather than in the
//     initial HTML." Hence `useAjaxChapters`, and a fallback that reads the
//     initial HTML when the POST yields nothing.
//   - "Per-site variation in path segments (manga / series / comics) — hence
//     overrides." Hence `mangaSubPath`.
//
// The current/legacy split is the third. Upstream ecosystems have forked this
// family into two themes (see docs/THEME-NOTES.md); we keep one theme and an
// `ajaxStyle` override, because the two differ in exactly one endpoint and
// splitting them would double the fingerprinting surface for no gain.
//
// # Listings
//
// This theme implements theme.Lister. Popular (m_orderby=views), Newly added
// (m_orderby=new-manga), Top rated (m_orderby=rating) and Completed
// (status=end) are offered unconditionally: they are the plugin's own CPT
// archive query parameters, a fact about the software rather than about any
// one install, confirmed live 2026-09-24 against mangaread.org and the adult
// family member hentaixcomic.com. Genres are offered only when discoverGenres
// can find the site's own genre-taxonomy links on its home page — the
// taxonomy's URL base is renamed per site (observed "/genres/" and
// "/manga-genre/" on the two sites above) and is not assumed. A site whose
// genre navigation is not statically discoverable (also observed: it can sit
// behind an interaction this theme does not simulate) gets the sort and
// status listings with no genres, which is not an error.
package madara

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "madara"

// Override keys.
const (
	// KeyMangaSubPath is the path segment under which series live. Sites ship
	// this as "manga" but rename it freely; "series" and "comics" are both
	// common. PLAN §7.3 names this variation as the reason overrides exist.
	KeyMangaSubPath = "mangaSubPath"

	// KeyUseAjaxChapters selects the AJAX chapter list. Default true because
	// the plugin's recent default is to lazy-load it; when it is false, or the
	// POST comes back empty, we read the initial HTML instead.
	KeyUseAjaxChapters = "useAjaxChapters"

	// KeyAjaxStyle is the current/legacy switch. "current" POSTs to
	// {series}ajax/chapters/; "legacy" POSTs manga_get_chapters to
	// /wp-admin/admin-ajax.php with the series' WordPress post ID.
	KeyAjaxStyle = "ajaxStyle"

	// KeyDateFormat is the site's chapter date format, in the CLDR-ish
	// spelling PLAN §7.2's example uses. A WordPress date setting is closer to
	// this than to a Go layout, so a user can copy theirs across.
	KeyDateFormat = "dateFormat"

	// KeySearchPostType is the post_type the search form filters on. Sites
	// that renamed the post type need this; sites that only renamed the
	// *permalink* need KeyMangaSubPath instead, which is why they are separate
	// keys rather than one.
	KeySearchPostType = "searchPostType"
)

// AJAX styles for KeyAjaxStyle.
const (
	AjaxCurrent = "current"
	AjaxLegacy  = "legacy"
)

// spec is the theme's complete override declaration. PLAN §7.2: anything not
// listed here is a validation error, not a silently ignored key.
var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key: KeyMangaSubPath, Kind: theme.KindString, Default: "manga",
			Why: "series live under /{mangaSubPath}/{slug}/; sites rename this segment freely (manga, series, comics, ...)",
		},
		{
			Key: KeyUseAjaxChapters, Kind: theme.KindBool, Default: true,
			Why: "the chapter list is usually lazy-loaded over POST rather than present in the series HTML",
		},
		{
			Key: KeyAjaxStyle, Kind: theme.KindString, Default: AjaxCurrent,
			Enum: []string{AjaxCurrent, AjaxLegacy},
			Why:  "the family forked: current builds POST to {series}ajax/chapters/, older ones POST manga_get_chapters to admin-ajax.php",
		},
		{
			Key: KeyDateFormat, Kind: theme.KindString, Default: "MMMM d, yyyy",
			Why: "chapter dates follow the site's WordPress date setting; relative dates are handled without it",
		},
		{
			Key: KeySearchPostType, Kind: theme.KindString, Default: "wp-manga",
			Why: "the search form filters on post_type; a site that renamed the custom post type needs this",
		},
	},
}

// Theme is the madara theme. It is stateless apart from its fetcher and clock,
// so one instance serves every configured madara source.
type Theme struct {
	f   theme.Fetcher
	now func() time.Time
}

// New builds the theme over a fetcher.
func New(f theme.Fetcher) *Theme { return &Theme{f: f, now: time.Now} }

// NewWithClock is New with an injectable clock, for tests that assert on
// relative chapter dates.
func NewWithClock(f theme.Fetcher, now func() time.Time) *Theme {
	return &Theme{f: f, now: now}
}

// DiscoveryOnly implements theme.DiscoveryClassifier: a copy of this theme
// whose every request is classified as discovery (PLAN §7.4), for the unattended
// watched-series check of PLAN §12.2.
//
// madara makes no retrieval-classified request today, so this changes nothing
// about what it sends. It is implemented anyway because the interface is how a
// caller *knows* that, and because a future call site that reaches for
// GetRetrieval must not silently escape the downgrade.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// SuggestedName implements theme.Theme.
//
// Empty: madara is a plugin running on hundreds of independently branded
// sites, so there is no name this theme could offer that would be right for
// more than one of them. Stage 6 falls back to the page title, which for a
// site family is genuinely the best available answer.
func (t *Theme) SuggestedName() string { return "" }

// AllowedHosts implements theme.Theme.
//
// Nil, and confirmed rather than assumed: this is a family of hundreds of
// independently hosted WordPress installs, and they have no CDN in common.
// Each site's images come from its own /wp-content/uploads/, on its own
// registrable domain, which the guard already permits. Naming a host here
// would widen the boundary for every madara site at once on the strength of
// what one of them happens to do.
//
// A site that does front its uploads with a third-party CDN is a per-source
// allowedHosts entry, which is exactly what that field is for.
func (t *Theme) AllowedHosts() []string { return nil }

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// Fingerprint scores a page for "is this Madara?".
//
// The signals are weighted by how hard they are to produce by accident. The
// `wp-manga` post type and the plugin's own asset path are near-conclusive:
// nothing else generates them. The class names are strong but individually
// imitable, so none of them alone reaches the "this is the theme" threshold —
// which is what keeps a MangaThemesia page, also WordPress and also full of
// wp- prefixed classes, from scoring here (see the near-miss test).
func (t *Theme) Fingerprint(p *probe.Page) int {
	score := 0
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	// The family's own asset paths.
	//
	// CORRECTED 2026-09-16, and this was the bug: the only string checked here
	// used to be "/wp-content/plugins/madara/", worth 40 — and **no real site
	// serves that path.** Madara is distributed as a WordPress *theme*, so its
	// assets come from /wp-content/themes/madara/, and its companion plugin is
	// registered as `madara-core`. A live site of the largest family we support
	// scored 43 against a threshold of 60, which made stage 4 return
	// `unrecognised` for it. The string had been invented in our own fixtures
	// and then verified against those fixtures, which is how it survived.
	//
	// All three spellings are accepted: two are measured on live installs, and
	// the original is kept because an install that renames its plugin
	// directory back is cheap to allow and costs nothing.
	add(p.Contains("/wp-content/themes/madara/") ||
		p.Contains("/wp-content/plugins/madara-core/") ||
		p.Contains("/wp-content/plugins/madara/"), 35)

	// The custom post type, which appears in body classes, search forms and
	// REST links alike. Measured on every live page we looked at.
	add(p.Contains("wp-manga"), 25)

	// Listing markup, which is what a *home* page is made of. Measured: the
	// home page of a live install carries none of the series, reader or search
	// markup below, and the home page is what the probe fingerprints — so this
	// group has to be able to carry a page on its own, the same problem
	// mangathemesia's listing signals solve.
	add(p.Has(".page-listing-item .page-item-detail, .page-item-detail.manga"), 15)
	add(p.Has(".page-content-listing .list-chapter .chapter-item, .list-chapter .chapter-item"), 10)
	add(p.Has(".item-thumb.c-image-hover, .tab-thumb.c-image-hover"), 8)

	// The chapter list, in both spellings this family has shipped: the current
	// wrapper and the older holder with its data-id.
	add(p.Has(".listing-chapters_wrap ul.main, .listing-chapters_wrap"), 12)
	add(p.Has("#manga-chapters-holder"), 12)
	add(p.Contains("manga_get_chapters"), 12)
	add(p.Has("li.wp-manga-chapter"), 12)

	// Search results, which are a different rendering again from the home
	// page's — measured, after a home fixture of ours had put this one on the
	// wrong page for a year.
	add(p.Has(".c-tabs-item__content"), 10)

	// Remaining landmarks. Individually weak, and all of them measured present
	// on at least one live page.
	add(p.Has(".site-content .c-blog__heading, .c-blog__heading"), 8)
	add(p.Has(".reading-content .page-break"), 10)
	add(p.Has("input.rating-post-id"), 8)
	add(p.Has(".post-title h1, .post-title h3, .post-title h4"), 5)
	add(p.Has(".manga-title-badges, .summary_image"), 5)

	return score
}

// Search implements theme.Theme. Madara sites answer the stock WordPress
// search form with a post_type filter; page 1 is the bare /?s=, later pages
// use WordPress's /page/N/ pagination.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	o, err := t.overrides(s)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	path := "/"
	if page > 1 {
		path = fmt.Sprintf("/page/%d/", page)
	}
	qs := url.Values{}
	qs.Set("s", q)
	qs.Set("post_type", o.String(KeySearchPostType))

	reqPath := path + "?" + qs.Encode()
	doc, err := t.doc(ctx, s, reqPath)
	if err != nil {
		return nil, err
	}
	// The search results page just fetched above — the page these covers are
	// actually parsed from, resolved the same way t.doc resolved it. PLAN
	// §7.6: truthful, per-request, never a constant.
	pageURL, err := s.Resolve(reqPath)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve %s: %w", ID, reqPath, err)
	}

	return parseListingPage(t, s, doc, pageURL), nil
}

// parseListingPage reads the series cards off a page rendering the family's
// "listing" markup — the WordPress search-results tabs and the CPT archive's
// card grid share the same two shapes, so one parser reads a search results
// page, the plain /{mangaSubPath}/ archive, an m_orderby-sorted or
// status-filtered archive, and a genre archive alike (List uses it for the
// latter three).
//
// A listing page also links to tags, genres and authors from inside the very
// same markup this loop matches — .post-title is not unique to a series link.
// mangaSubPath used to be trusted to say which segment is the real one, but
// that default ("manga") is only ever right for a site that never renamed it,
// and two real installs (toonily.com's "/serie/", allporncomic.com's
// "/porncomic/") rename it to something a fixed default or a per-host guess
// could never anticipate, discarding every result and making a working search
// read as "this site has nothing" (found live, 2026-09-21).
//
// The fix asks the page itself rather than the config: whichever first path
// segment the majority of candidates share *is* the site's series segment,
// evidence found fresh on every request rather than assumed once. A tag or
// author link mixed into the same batch is outvoted, because a real result
// page has many more series links than decoys.
func parseListingPage(t *Theme, s *theme.Source, doc *goquery.Document, pageURL string) []theme.SeriesStub {
	var candidates []searchCandidate
	// Search results render as tab items; a few skins use the same inner
	// markup inside a plain .row, so match on the inner .post-title anchor
	// rather than on the container.
	doc.Find(".c-tabs-item__content, .page-listing-item .page-item-detail").Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find(".post-title h3 a, .post-title h4 a, .post-title a, h3.h4 a").First()
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		id := t.relativeID(s, href)
		if id == "" {
			return
		}
		candidates = append(candidates, searchCandidate{
			id:    id,
			title: theme.Text(link),
			cover: theme.ImageURL(sel.Find("img").First()),
		})
	})

	seg := dominantSegment(candidates)
	var out []theme.SeriesStub
	for _, c := range candidates {
		if seg != "" && firstSegment(c.id) != seg {
			continue
		}
		stub := theme.SeriesStub{ID: c.id, Title: c.title, CoverURL: c.cover}
		if stub.CoverURL != "" {
			stub.CoverReferrer = pageURL
		}
		out = append(out, stub)
	}
	return out
}

// Sort values for the plugin's own m_orderby query parameter (its "Sort by"
// dropdown), sent verbatim — this is the site's own vocabulary, not ours.
// Confirmed live on 2026-09-24 against mangaread.org and the adult member
// hentaixcomic.com: "views", "new-manga", "rating" and "trending" all answer
// with the archive's card grid, distinct from the default ordering. "trending"
// has no well-known Quire meaning (a hybrid of new and popular) and is not
// offered; "latest" is what the *default* Browse already returns via Search,
// and the Lister doc says ListingLatest may be omitted for exactly that
// reason.
const (
	orderByPopular = "views"     // "most read / most viewed first" — ListingPopular's own wording
	orderByNew     = "new-manga" // "most recently added to the site" — ListingNew's own wording
	orderByRating  = "rating"
)

// minGenreLinks is the smallest number of distinct genre-shaped links
// discoverGenres requires before trusting a segment is the site's genre
// taxonomy rather than a handful of unrelated two-segment links (a single
// "Ongoing" page link, an author archive with two authors). Both live sites
// this theme was verified against clear this by a wide margin (45 on
// mangaread.org); a site that does not is one whose genre navigation this
// theme cannot find statically, and Listings simply offers none rather than
// guessing.
const minGenreLinks = 3

// reservedListingSegments are first path segments that are never a genre
// taxonomy on a WordPress site: core rewrite bases, and the plugin's own
// non-series archives. Excluding them by name, rather than trusting the count
// alone, keeps a small site's paginator ("/page/2/") or author archive from
// outvoting a genuine but modest genre list.
var reservedListingSegments = map[string]bool{
	"page": true, "tag": true, "author": true, "category": true,
	"comments": true, "feed": true, "wp-content": true, "wp-admin": true,
	"wp-json": true, "cdn-cgi": true,
}

// pagedPath appends the plugin's WordPress-standard pagination segment to a
// site-relative directory path (which must already end in "/"). Confirmed
// live on 2026-09-24: /manga/page/2/?m_orderby=... and
// /genres/action/page/2/ both return the next page's cards, distinct from
// page 1's.
func pagedPath(base string, page int) string {
	if page <= 1 {
		return base
	}
	return strings.TrimSuffix(base, "/") + fmt.Sprintf("/page/%d/", page)
}

// Listings implements theme.Lister.
//
// The sort and status listings are a fact about the plugin itself (PLAN
// §7.3), so they are offered without a request: m_orderby and status are
// documented query parameters of every Madara install's CPT archive,
// confirmed live 2026-09-24 against mangaread.org and its adult member
// hentaixcomic.com. Genres are different — the taxonomy's URL base is
// per-site (mangaread.org uses "/genres/", hentaixcomic.com renamed it to
// "/manga-genre/", both confirmed live) — so they are discovered from
// whatever genre-shaped links the home page actually carries rather than
// assumed at a fixed path. A site whose genre navigation cannot be found
// statically (also observed live: hentaixcomic.com's is loaded behind an
// interaction this theme does not simulate) simply gets no genre listings;
// that is not an error; see discoverGenres.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	o, err := t.overrides(s)
	if err != nil {
		return nil, err
	}
	listings := []theme.Listing{
		{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
		{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		{ID: theme.ListingRating, Group: theme.ListingGroupSort},
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
	}

	doc, err := t.doc(ctx, s, "/")
	if err != nil {
		// The sort and status listings above need no page fetch — they are the
		// plugin's own query parameters, not something read off this page — so a
		// home page that fails to load (a challenge, a timeout) still leaves a
		// working, if genre-less, Browse rather than an empty list.
		return listings, nil
	}
	listings = append(listings, discoverGenres(t, s, doc, o.PathSegment(KeyMangaSubPath))...)
	return listings, nil
}

// discoverGenres finds the site's genre taxonomy archive links on a page —
// the home page, in practice — without assuming a path. Every Madara install
// links its series under {mangaSubPath}; the same page also carries a handful
// of other two-segment link families (genres, sometimes tags or authors), and
// the genre one is reliably the largest after the series links are excluded
// by name. Confirmed live 2026-09-24: mangaread.org's home page carries 104
// links under "/manga/" and 45 under "/genres/", with nothing else close.
//
// The discovered handle — "{segment}/{slug}" — becomes the genre listing's ID
// via theme.GenreListing, so List can turn it back into a request path
// without having to know the site's taxonomy base name a second time.
func discoverGenres(t *Theme, s *theme.Source, doc *goquery.Document, seriesSubPath string) []theme.Listing {
	type slugLabel struct{ slug, label string }
	bySegment := map[string][]slugLabel{}
	seen := map[string]bool{} // "segment/slug" already recorded, first label wins
	var order []string

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		id := t.relativeID(s, href)
		if id == "" {
			return
		}
		if i := strings.IndexByte(id, '?'); i >= 0 {
			id = id[:i]
		}
		trimmed := strings.Trim(id, "/")
		segs := strings.Split(trimmed, "/")
		if len(segs) != 2 || segs[0] == "" || segs[1] == "" {
			return
		}
		seg, slug := segs[0], segs[1]
		if seg == seriesSubPath || reservedListingSegments[seg] {
			return
		}
		if _, err := strconv.Atoi(slug); err == nil {
			// A numeric second segment under an unrecognised first one is
			// almost always a paginator this site named differently, not a
			// genre slug.
			return
		}
		key := seg + "/" + slug
		if seen[key] {
			return
		}
		seen[key] = true
		if _, ok := bySegment[seg]; !ok {
			order = append(order, seg)
		}
		label := theme.Text(a)
		if label == "" {
			label = slug
		}
		bySegment[seg] = append(bySegment[seg], slugLabel{slug: slug, label: label})
	})

	best, bestN := "", 0
	for _, seg := range order {
		if n := len(bySegment[seg]); n > bestN {
			best, bestN = seg, n
		}
	}
	if best == "" || bestN < minGenreLinks {
		return nil
	}
	entries := bySegment[best]
	sort.Slice(entries, func(i, j int) bool { return entries[i].slug < entries[j].slug })
	out := make([]theme.Listing, 0, len(entries))
	for _, e := range entries {
		out = append(out, theme.GenreListing(best+"/"+e.slug, e.label))
	}
	return out
}

// List implements theme.Lister.
//
// ListingLatest delegates to Search with an empty query — today's Browse,
// unchanged. Every other sort or status listing is the same CPT archive
// Search's own results share their markup with, read through
// parseListingPage; a genre listing reuses the handle Listings discovered,
// with no second guess at the site's taxonomy base.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	if listingID == "" || listingID == theme.ListingLatest {
		return t.Search(ctx, s, "", page)
	}
	o, err := t.overrides(s)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}

	var base, query string
	switch {
	case listingID == theme.ListingPopular:
		base = "/" + o.PathSegment(KeyMangaSubPath) + "/"
		query = "m_orderby=" + orderByPopular
	case listingID == theme.ListingNew:
		base = "/" + o.PathSegment(KeyMangaSubPath) + "/"
		query = "m_orderby=" + orderByNew
	case listingID == theme.ListingRating:
		base = "/" + o.PathSegment(KeyMangaSubPath) + "/"
		query = "m_orderby=" + orderByRating
	case listingID == theme.ListingCompleted:
		base = "/" + o.PathSegment(KeyMangaSubPath) + "/"
		query = "status=end"
	default:
		handle, ok := theme.GenreID(listingID)
		if !ok || strings.Trim(handle, "/") == "" {
			return nil, fmt.Errorf("%s: unknown listing %q", ID, listingID)
		}
		base = "/" + strings.Trim(handle, "/") + "/"
	}

	reqPath := pagedPath(base, page)
	if query != "" {
		reqPath += "?" + query
	}
	doc, err := t.doc(ctx, s, reqPath)
	if err != nil {
		return nil, err
	}
	pageURL, err := s.Resolve(reqPath)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve %s: %w", ID, reqPath, err)
	}
	return parseListingPage(t, s, doc, pageURL), nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	seriesPath := t.seriesPath(s, id)
	doc, err := t.doc(ctx, s, seriesPath)
	if err != nil {
		return nil, err
	}

	out := &theme.Series{ID: id}
	out.Title = theme.Text(doc.Find(".post-title h1, .post-title h3, .post-title h2").First())
	out.CoverURL = theme.ImageURL(doc.Find(".summary_image img, .tab-summary img").First())
	if out.CoverURL != "" {
		// The series page just fetched above, the page the cover markup was
		// actually parsed from — not a value assembled by hand.
		if pageURL, err := s.Resolve(seriesPath); err == nil {
			out.CoverReferrer = pageURL
		}
	}
	out.Description = theme.Text(doc.Find(".description-summary .summary__content, .summary__content, .manga-excerpt").First())

	doc.Find(".genres-content a, .summary-content .genres-content a").Each(func(_ int, a *goquery.Selection) {
		if g := theme.Text(a); g != "" {
			out.Genres = append(out.Genres, g)
		}
	})

	// The metadata block is a list of heading/content pairs whose headings are
	// the site's own words ("Author(s)", "Artist(s)", "Status", and their
	// translations). Matching on the heading text rather than on position is
	// what lets a translated site still work.
	doc.Find(".post-content_item, .post-status .post-content_item").Each(func(_ int, row *goquery.Selection) {
		heading := strings.ToLower(theme.Text(row.Find(".summary-heading").First()))
		content := row.Find(".summary-content").First()
		switch {
		case strings.Contains(heading, "author"):
			out.Authors = append(out.Authors, splitNames(content)...)
		case strings.Contains(heading, "artist"):
			out.Artists = append(out.Artists, splitNames(content)...)
		case strings.Contains(heading, "status"):
			out.Status = normaliseStatus(theme.Text(content))
		case strings.Contains(heading, "alternative"):
			for _, alt := range strings.Split(theme.Text(content), ",") {
				if alt = strings.TrimSpace(alt); alt != "" {
					out.AltTitles = append(out.AltTitles, alt)
				}
			}
		}
	})
	return out, nil
}

// Chapters implements theme.Theme.
//
// The order of attempts is deliberate: try the configured AJAX endpoint, and
// fall back to the series HTML if it yields nothing. A site that has the list
// inline works even with the default useAjaxChapters=true, so the common case
// needs no override at all — which is the point of PLAN §6 M2's "adding a
// third site requires only a config entry".
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	o, err := t.overrides(s)
	if err != nil {
		return nil, err
	}

	if o.Bool(KeyUseAjaxChapters) {
		chapters, err := t.ajaxChapters(ctx, s, id, o)
		if err != nil {
			return nil, err
		}
		if len(chapters) > 0 {
			return chapters, nil
		}
	}

	doc, err := t.doc(ctx, s, t.seriesPath(s, id))
	if err != nil {
		return nil, err
	}
	return t.parseChapters(s, o, doc), nil
}

func (t *Theme) ajaxChapters(ctx context.Context, s *theme.Source, id string, o theme.Overrides) ([]theme.Chapter, error) {
	switch o.String(KeyAjaxStyle) {
	case AjaxLegacy:
		// The legacy endpoint needs the series' WordPress post ID, which only
		// the series page carries — so this shape costs an extra round trip.
		doc, err := t.doc(ctx, s, t.seriesPath(s, id))
		if err != nil {
			return nil, err
		}
		postID := seriesPostID(doc)
		if postID == "" {
			// No post ID means no legacy call is possible; let the caller fall
			// back to the HTML it would have to fetch anyway.
			return t.parseChapters(s, o, doc), nil
		}
		form := url.Values{}
		form.Set("action", "manga_get_chapters")
		form.Set("manga", postID)
		frag, err := t.postDoc(ctx, s, "/wp-admin/admin-ajax.php", form)
		if err != nil {
			return nil, err
		}
		return t.parseChapters(s, o, frag), nil

	default: // AjaxCurrent
		frag, err := t.postDoc(ctx, s, strings.TrimSuffix(t.seriesPath(s, id), "/")+"/ajax/chapters/", nil)
		if err != nil {
			return nil, err
		}
		return t.parseChapters(s, o, frag), nil
	}
}

// parseChapters reads a chapter list from either a full series page or an AJAX
// fragment — the markup is identical, which is why one parser serves both.
//
// It is also the single exit for every path through Chapters, which is why the
// PLAN §7.2 ascending-order normalisation happens here rather than at each of
// the three call sites above. madara's markup lists chapters **newest first**;
// testdata/chapters-ajax.html is deliberately in that order, and the test
// asserts we hand back the reverse.
func (t *Theme) parseChapters(s *theme.Source, o theme.Overrides, doc *goquery.Document) []theme.Chapter {
	format := o.String(KeyDateFormat)
	now := t.now()

	var out []theme.Chapter
	doc.Find("li.wp-manga-chapter").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		id := t.relativeID(s, href)
		if id == "" {
			return
		}
		title := theme.Text(a)
		ch := theme.Chapter{
			ID:     id,
			Title:  title,
			Number: theme.ChapterNumber(title),
		}
		// The date is either a plain <i> (absolute) or an <a title="..."> that
		// the plugin uses for "N days ago" — the title attribute then holds
		// the absolute date, which is the more useful of the two.
		rel := li.Find(".chapter-release-date").First()
		if abs, ok := rel.Find("a").Attr("title"); ok && strings.TrimSpace(abs) != "" {
			ch.Published = theme.ParseDate(abs, format, now)
		}
		if ch.Published.IsZero() {
			ch.Published = theme.ParseDate(theme.Text(rel), format, now)
		}
		out = append(out, ch)
	})
	return theme.SortAndMark(out)
}

// Pages implements theme.Theme. The reader renders one .page-break per image;
// the images are lazy-loaded, so ImageURL's attribute priority matters here
// more than anywhere else.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	doc, err := t.doc(ctx, s, chapterID)
	if err != nil {
		return nil, err
	}
	var out []string
	doc.Find(".reading-content .page-break img, .reading-content img.wp-manga-chapter-img").Each(func(_ int, img *goquery.Selection) {
		raw := theme.ImageURL(img)
		if raw == "" {
			return
		}
		abs, err := s.Resolve(raw)
		if err != nil {
			return
		}
		out = append(out, abs)
	})
	return out, nil
}

// seriesPostID reads the WordPress post ID the legacy AJAX call needs. The
// plugin leaves it in two places; either will do.
func seriesPostID(doc *goquery.Document) string {
	if v, ok := doc.Find("input.rating-post-id").First().Attr("value"); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := doc.Find("#manga-chapters-holder").First().Attr("data-id"); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

// splitNames turns a comma-separated or multi-anchor name cell into a slice.
func splitNames(sel *goquery.Selection) []string {
	var out []string
	sel.Find("a").Each(func(_ int, a *goquery.Selection) {
		if n := theme.Text(a); n != "" && !strings.EqualFold(n, "updating") {
			out = append(out, n)
		}
	})
	if len(out) > 0 {
		return out
	}
	for _, n := range strings.Split(theme.Text(sel), ",") {
		if n = strings.TrimSpace(n); n != "" && !strings.EqualFold(n, "updating") {
			out = append(out, n)
		}
	}
	return out
}

// normaliseStatus maps the site's own status words onto our small enum, so the
// UI never has to render a site's vocabulary. Unrecognised words become
// StatusUnknown rather than being passed through.
func normaliseStatus(s string) string {
	switch l := strings.ToLower(strings.TrimSpace(s)); {
	case strings.Contains(l, "ongoing"), strings.Contains(l, "publishing"), strings.Contains(l, "releasing"):
		return theme.StatusOngoing
	case strings.Contains(l, "completed"), strings.Contains(l, "finished"), strings.Contains(l, "end"):
		return theme.StatusCompleted
	case strings.Contains(l, "hiatus"), strings.Contains(l, "paused"):
		return theme.StatusHiatus
	case strings.Contains(l, "cancel"), strings.Contains(l, "dropped"):
		return theme.StatusCancelled
	default:
		return theme.StatusUnknown
	}
}
