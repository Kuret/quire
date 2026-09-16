// Package mangakakalot implements the shape of a comic-site family that is
// neither of the WordPress families PLAN §7.3 lists as tier 1.
//
// It is its own lineage, and the check that it is was structural rather than
// nominal: none of madara's markers are present (no `wp-manga` asset path, no
// `admin-ajax.php` chapter POST) and none of mangathemesia's are either (no
// `ts_reader.run(`, no `#chapterlist`, no `.bixbox`). It is a family rather
// than one site — the same markup is served by a long tail of mirrors — so it
// is a theme like the others and not a one-off.
//
// What makes it structurally different from both, which is the part worth
// knowing before reading the code:
//
//   - The **chapter list is a JSON API**, not markup. The series page ships an
//     empty `#chapter-list-container` carrying the endpoint template in
//     `data-api-url`, and the list arrives from
//     `/api/manga/{slug}/chapters?limit=-1` as
//     `{success, data:{chapters:[{chapter_name, chapter_slug, chapter_num,
//     updated_at}]}}`. Parsing the series HTML for chapters finds nothing at
//     all — not a partial list, nothing.
//   - That API returns **newest first**, so Chapters reverses it. See
//     theme.SortAndMark and PLAN §7.2's ordering contract.
//   - The **reader is server-rendered** into `.container-chapter-reader`, with
//     absolute image URLs, *and* carries the same list in inline
//     `cdns = [...]` / `chapterImages = [...]` arrays. Either is usable; the
//     DOM is authoritative here because it is what the browser displays, and
//     the arrays are the fallback for a mirror that lazy-loads.
//   - Page images come from **separate image hosts**, on a different
//     registrable domain than the site. AllowedHosts declares them; without
//     that the SSRF guard refuses every download (PLAN §7.2, 2026-09-15).
//
// Provenance (PLAN §1.4): the endpoint shapes above were read as *facts* —
// from an existing Apache-2.0 extension for the family, and confirmed against
// live responses while developing. Nothing here is transcribed from anyone's
// source, the parsing is written from scratch against observed HTML and JSON,
// and no site of the family is named anywhere in this repository (PLAN §1.3).
// The fixtures are synthetic and hosted at example.invalid.
package mangakakalot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "mangakakalot"

// Override keys.
const (
	// KeySeriesSubPath is the path segment series and their readers live
	// under: /{seriesSubPath}/{slug} and /{seriesSubPath}/{slug}/{chapter}.
	// The family ships "manga"; it is a key rather than a constant because
	// this family is a long tail of mirrors and a mirror renaming a segment is
	// exactly the kind of drift a source should survive without a new build.
	KeySeriesSubPath = "seriesSubPath"

	// KeySearchPath is the prefix the site's own text search lives under. The
	// query is appended as a single normalised path segment, not as a query
	// parameter.
	KeySearchPath = "searchPath"

	// KeyBrowsePath is the listing used when the query is empty — the UI's
	// "Browse" is a search with no query (PLAN §7.5, M3 correction 1), and
	// this family's search endpoint has nothing sensible to say about an empty
	// string.
	KeyBrowsePath = "browsePath"

	// KeyPageSource selects where Pages() looks first. Both are present on the
	// sites we have seen; see the package comment.
	KeyPageSource = "pageSource"
)

// Values for KeyPageSource.
const (
	// PageSourceDOM reads the server-rendered reader images.
	PageSourceDOM = "dom"
	// PageSourceScript reads the inline cdns/chapterImages arrays.
	PageSourceScript = "script"
)

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key: KeySeriesSubPath, Kind: theme.KindString, Default: "manga",
			Why: "series live under /{seriesSubPath}/{slug} and chapters under /{seriesSubPath}/{slug}/{chapterSlug}; a family of mirrors is where a renamed segment shows up",
		},
		{
			Key: KeySearchPath, Kind: theme.KindString, Default: "search/story",
			Why: "text search is a path, not a query parameter: /{searchPath}/{normalised query}",
		},
		{
			Key: KeyBrowsePath, Kind: theme.KindString, Default: "manga-list/latest-manga",
			Why: "the listing used for an empty query, since the UI's Browse is a search with no query",
		},
		{
			Key: KeyPageSource, Kind: theme.KindString, Default: PageSourceDOM,
			Enum: []string{PageSourceDOM, PageSourceScript},
			Why:  "the reader is server-rendered into .container-chapter-reader and *also* carries an inline chapterImages array; whichever is chosen, the other is tried if it yields nothing",
		},
	},
}

// Theme is the mangakakalot theme.
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

// DiscoveryOnly implements theme.DiscoveryClassifier: a copy whose every
// request is classified as discovery (PLAN §7.4), for the unattended
// watched-series check of PLAN §12.2.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// SuggestedName implements theme.Theme.
//
// Empty. This family is a set of mirrors with different names and no shared
// branding, so there is no one name to lend them; the page title is the better
// default and the user can rename the source anyway (PLAN §7.5 stage 6).
func (t *Theme) SuggestedName() string { return "" }

// AllowedHosts implements theme.Theme.
//
// Unlike the WordPress families, this one does not serve its own images: both
// covers and page images come from dedicated image hosts on a different
// registrable domain than the site, so without this declaration the SSRF guard
// refuses every download and the user is left to guess a CDN's name from a
// rejection (PLAN §7.2, 2026-09-15).
//
// Narrow on purpose, in the two ways available. The image domain is named
// exactly rather than left to a redirect allowance, and the one that needs a
// wildcard gets `*.` — subdomains only, never the bare domain — because the
// hosts under it are rotated, numbered labels rather than one stable name.
func (t *Theme) AllowedHosts() []string {
	return []string{"*.2xstorage.com", "storage.waitst.com"}
}

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// Fingerprint scores a page for "is this the mangakakalot family?".
//
// The strongest signal is the one that is also the theme's defining structural
// difference: a series page whose chapter list is an empty container carrying
// the API endpoint in `data-api-url`. Nothing else we have seen ships a
// chapter list that way.
//
// The rest are grouped by the page a probe might land on. The probe scores the
// *home* page, which carries none of the series, reader or listing markup, so
// the home-page signals have to reach the threshold on their own — the same
// problem mangathemesia's listing signals solve, and solved the same way: a
// set of class names that is distinctive together while no single one of them
// decides anything.
func (t *Theme) Fingerprint(p *probe.Page) int {
	score := 0
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	// Series page.
	add(p.Has("#chapter-list-container[data-api-url]"), 35)
	add(p.Has("[data-chapter-url-template]"), 10)
	add(p.Has(".manga-info-top, .panel-story-info"), 20)
	add(p.Has("ul.manga-info-text"), 10)
	add(p.Has(".manga-info-pic, span.info-image"), 10)

	// Reader page.
	add(p.Has(".container-chapter-reader"), 35)
	add(p.Contains("chapterImages"), 20)
	add(p.Contains("cdns"), 5)

	// Listing and search pages.
	add(p.Has(".list-comic-item-wrap, .list-truyen-item-wrap"), 25)
	add(p.Has("a.list-story-item"), 20)
	add(p.Has("a.list-story-item-wrap-chapter"), 10)
	add(p.Has(".panel_story_list .story_item"), 35)
	add(p.Has(".story_item .story_name, .story_item_right"), 25)
	add(p.Has(".group_page, .group-page"), 5)

	// Home page. None of the above appears on it.
	add(p.Has("#contentstory .doreamon"), 35)
	add(p.Has(".itemupdate a.bookmark_check, a.cover.bookmark_check"), 25)
	add(p.Has(".daily-update"), 10)

	// Negative signals. A page carrying another family's own marker is that
	// family's page however many generic class names it shares with this one;
	// without these, a site could in principle be claimed by two themes at
	// once and PLAN §7.5 stage 4 would have a near-tie to put to the user over
	// a question that is not actually close.
	add(p.Contains("wp-manga"), -40)
	add(p.Contains("ts_reader.run("), -40)

	if score < 0 {
		return 0
	}
	return score
}

// Search implements theme.Theme.
//
// Two endpoints, because the family has two. A real query goes to the site's
// text search, whose query is a *path segment* rather than a parameter and is
// normalised to the site's own spelling first. An empty query — which is what
// the UI's Browse sends (PLAN §7.5, M3 correction 1) — goes to the listing
// instead, because "search for nothing" is not a thing this family answers.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}

	var path string
	if q = strings.TrimSpace(q); q == "" {
		path = fmt.Sprintf("/%s?page=%d", strings.Trim(o.String(KeyBrowsePath), "/"), page)
	} else {
		path = fmt.Sprintf("/%s/%s?page=%d",
			strings.Trim(o.String(KeySearchPath), "/"), url.PathEscape(normaliseQuery(q)), page)
	}

	doc, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var out []theme.SeriesStub
	// Three renderings of one list: the grid used by the listing pages, the
	// older row-per-result panel, and the bare anchor for a skin that carries
	// neither wrapper.
	doc.Find(".list-comic-item-wrap, .list-truyen-item-wrap, .panel_story_list .story_item").Each(func(_ int, item *goquery.Selection) {
		a := item.Find("h3 a").First()
		if a.Length() == 0 {
			a = item.Find("a").First()
		}
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		id := t.seriesID(s, href)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true

		title := strings.TrimSpace(a.AttrOr("title", ""))
		if title == "" {
			title = theme.Text(a)
		}
		out = append(out, theme.SeriesStub{
			ID:       id,
			Title:    theme.Collapse(title),
			CoverURL: theme.ImageURL(item.Find("img").First()),
		})
	})
	return out, nil
}

// Series implements theme.Theme.
//
// Everything here is in one flat <ul class="manga-info-text"> of labelled
// rows — "Author(s) : ...", "Status : ..." — so the rows are matched on their
// label rather than on their position, which is what lets a mirror that
// reorders or translates them still work.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	doc, err := t.doc(ctx, s, t.seriesPath(s, id))
	if err != nil {
		return nil, err
	}

	out := &theme.Series{ID: id}
	out.Title = theme.Text(doc.Find(".manga-info-text h1, .manga-info-top h1, .story-info-right h1").First())
	out.CoverURL = theme.ImageURL(doc.Find(".manga-info-pic img, span.info-image img").First())

	doc.Find(".manga-info-text li, .variations-tableInfo tr").Each(func(_ int, row *goquery.Selection) {
		label, value := splitLabelled(row)
		switch {
		case label == "":
			return
		case strings.HasPrefix(label, "author"):
			out.Authors = appendNames(out.Authors, value, row)
		case strings.HasPrefix(label, "artist"):
			out.Artists = appendNames(out.Artists, value, row)
		case strings.HasPrefix(label, "status"):
			out.Status = normaliseStatus(value)
		case strings.HasPrefix(label, "genre"):
			row.Find("a").Each(func(_ int, a *goquery.Selection) {
				if g := theme.Text(a); g != "" {
					out.Genres = append(out.Genres, g)
				}
			})
		case strings.HasPrefix(label, "alternative"):
			out.AltTitles = appendNames(out.AltTitles, value, nil)
		}
	})

	// The summary sits in its own box under a decorative heading; the heading
	// is dropped so the description does not begin with "<title> summary:".
	desc := doc.Find("#contentBox, #panel-story-info-description, #noidungm").First().Clone()
	desc.Find("h2, h3, .story-alternative, script, style").Remove()
	out.Description = theme.Text(desc)

	if len(out.AltTitles) == 0 {
		alt := theme.Text(doc.Find(".story-alternative").First())
		alt = strings.TrimPrefix(alt, "Alternative :")
		out.AltTitles = appendNames(out.AltTitles, alt, nil)
	}
	return out, nil
}

// chapterListResponse is the subset of the chapter API we need. The endpoint
// also returns view counts and a pagination block; neither is ours to care
// about, and `limit=-1` asks for the whole list in one response so the
// pagination never has to be walked.
type chapterListResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Chapters []apiChapter `json:"chapters"`
	} `json:"data"`
}

type apiChapter struct {
	Name      string   `json:"chapter_name"`
	Slug      string   `json:"chapter_slug"`
	Number    *float64 `json:"chapter_num"`
	UpdatedAt string   `json:"updated_at"`
}

// Chapters implements theme.Theme.
//
// One JSON request, no HTML: the series page ships an empty container and the
// list arrives from the API. The API answers newest-first, so the result is
// reversed — PLAN §7.2's ordering contract, and the reason a volume assembled
// from this family would otherwise read backwards.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	body, err := t.get(ctx, s, t.chapterAPIPath(s, id))
	if err != nil {
		return nil, err
	}

	var resp chapterListResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("%s: chapter list for %q is not the JSON this API returns: %w", ID, id, err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s: chapter list for %q: the API reported failure", ID, id)
	}

	slug := t.seriesSlug(s, id)
	out := make([]theme.Chapter, 0, len(resp.Data.Chapters))
	for _, ch := range resp.Data.Chapters {
		if ch.Slug == "" {
			continue
		}
		title := strings.TrimSpace(ch.Name)
		if title == "" {
			title = ch.Slug
		}
		c := theme.Chapter{
			ID:    t.chapterID(s, slug, ch.Slug),
			Title: theme.Collapse(title),
			// The API's own number is authoritative: a chapter named
			// "Chapter 453: Night 453" has two numbers in its title and only
			// one of them is the chapter's.
			Number: chapterNumber(ch),
		}
		if ts, err := time.Parse(time.RFC3339, ch.UpdatedAt); err == nil {
			c.Published = ts.UTC()
		}
		out = append(out, c)
	}
	return theme.SortAndMark(out), nil
}

// chapterNumber prefers the API's number and falls back to the title, so a
// mirror that omits the field is degraded rather than unordered.
func chapterNumber(ch apiChapter) float64 {
	if ch.Number != nil {
		return *ch.Number
	}
	if n := theme.ChapterNumber(ch.Name); n >= 0 {
		return n
	}
	return theme.ChapterNumber(ch.Slug)
}

// chapterImagesRE and cdnsRE find the reader's inline arrays. They are matched
// as whole assignments rather than by looking for the bare name, because the
// names also appear in the reader's own scripts as ordinary reads.
var (
	chapterImagesRE = regexp.MustCompile(`chapterImages\s*=\s*\[([^\]]*)\]`)
	cdnsRE          = regexp.MustCompile(`cdns\s*=\s*\[([^\]]*)\]`)
)

// Pages implements theme.Theme.
//
// The reader is server-rendered, so the DOM is the default and the inline
// arrays are the fallback. Whichever the source asks for, the other is tried
// when the first yields nothing: a skin that lazy-loads its images has the
// arrays and a skin that ships no script has the DOM, and returning no pages
// at all when the other half of the page had them would be a failure we could
// see and chose not to look at.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	body, doc, err := t.page(ctx, s, t.chapterPath(s, chapterID))
	if err != nil {
		return nil, err
	}

	first, second := domPages, scriptPages
	if o.String(KeyPageSource) == PageSourceScript {
		first, second = scriptPages, domPages
	}
	if urls := t.absolutise(s, first(body, doc)); len(urls) > 0 {
		return urls, nil
	}
	return t.absolutise(s, second(body, doc)), nil
}

// domPages reads the reader's own <img> tags.
func domPages(_ string, doc *goquery.Document) []string {
	var out []string
	doc.Find(".container-chapter-reader img, .container-chapter-reader-img img").Each(func(_ int, img *goquery.Selection) {
		if u := theme.ImageURL(img); u != "" {
			out = append(out, u)
		}
	})
	return out
}

// scriptPages joins the inline arrays: chapterImages holds paths, cdns holds
// the hosts to hang them on. The first CDN is the one the reader itself uses;
// the others are its own fallbacks and are not ours to round-robin.
func scriptPages(body string, _ *goquery.Document) []string {
	paths := jsStringArray(chapterImagesRE, body)
	if len(paths) == 0 {
		return nil
	}
	cdns := jsStringArray(cdnsRE, body)
	if len(cdns) == 0 {
		// Paths with no CDN are still relative URLs against the site; Pages
		// absolutises them, which is the honest reading of "no host given".
		return paths
	}
	base := strings.TrimSuffix(cdns[0], "/")
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, base+"/"+strings.TrimPrefix(p, "/"))
	}
	return out
}

// jsStringArray pulls the quoted entries out of an array literal.
//
// Every occurrence is tried rather than only the first, and only *quoted*
// entries count. Both rules earn their keep: the array names also appear in
// prose — a comment, a documentation block, this repository's own fixture
// headers — and a bracket pair in prose has no quoted entries in it, so the
// first textual match is not reliably the real one. The first match that
// yields strings is.
//
// The site escapes its slashes (`https:\/\/...`), which is valid JavaScript
// and not valid in a Go URL, so they are unescaped here.
func jsStringArray(re *regexp.Regexp, body string) []string {
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		var out []string
		for _, raw := range strings.Split(m[1], ",") {
			raw = strings.TrimSpace(raw)
			if len(raw) < 2 || !isQuote(raw[0]) || raw[len(raw)-1] != raw[0] {
				out = nil
				break
			}
			entry := strings.ReplaceAll(raw[1:len(raw)-1], `\/`, "/")
			if entry == "" {
				out = nil
				break
			}
			out = append(out, entry)
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func isQuote(b byte) bool { return b == '"' || b == '\'' }

func (t *Theme) absolutise(s *theme.Source, raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		abs, err := s.Resolve(r)
		if err != nil {
			continue
		}
		out = append(out, abs)
	}
	return out
}

// splitLabelled takes "Author(s) : Someone" apart into a lower-cased label and
// its value. Rows are matched on the label because the family's mirrors
// reorder them freely, and a row with no label at all — the title row — yields
// nothing rather than being mistaken for one.
func splitLabelled(row *goquery.Selection) (label, value string) {
	text := theme.Text(row)
	i := strings.Index(text, ":")
	if i < 0 {
		return "", ""
	}
	// "Author(s)", "Authors" and "Author" are the same label wearing different
	// hats, and the switch on it matches by prefix for that reason. The "(s)"
	// is dropped because it sits in the middle of the word rather than at the
	// end of it.
	label = strings.ToLower(strings.TrimSpace(text[:i]))
	label = strings.ReplaceAll(label, "(s)", "")
	return label, strings.TrimSpace(text[i+1:])
}

// appendNames splits a comma-separated value, preferring the row's own links
// when it has them: a linked name is delimited by markup and cannot be split
// wrongly by a comma inside it.
func appendNames(dst []string, text string, row *goquery.Selection) []string {
	if row != nil && row.Find("a").Length() > 0 {
		row.Find("a").Each(func(_ int, a *goquery.Selection) {
			if n := theme.Text(a); n != "" && !isPlaceholder(n) {
				dst = append(dst, n)
			}
		})
		return dst
	}
	for _, n := range strings.Split(text, ",") {
		if n = strings.TrimSpace(n); n != "" && !isPlaceholder(n) {
			dst = append(dst, n)
		}
	}
	return dst
}

// isPlaceholder catches the fillers these sites leave in an empty field, so a
// series with no artist does not acquire one called "Updating".
func isPlaceholder(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "-", "n/a", "na", "updating", "unknown", "none":
		return true
	}
	return false
}

// normaliseStatus maps the site's own status words onto our small enum.
func normaliseStatus(s string) string {
	switch l := strings.ToLower(strings.TrimSpace(s)); {
	case strings.Contains(l, "ongoing"), strings.Contains(l, "publishing"), strings.Contains(l, "releasing"):
		return theme.StatusOngoing
	case strings.Contains(l, "completed"), strings.Contains(l, "finished"):
		return theme.StatusCompleted
	case strings.Contains(l, "hiatus"), strings.Contains(l, "paused"):
		return theme.StatusHiatus
	case strings.Contains(l, "cancel"), strings.Contains(l, "dropped"):
		return theme.StatusCancelled
	default:
		return theme.StatusUnknown
	}
}

// queryUnsafe is every character the family's search folds into "_". The site
// does this itself before looking the query up, so a query that keeps its
// punctuation finds nothing.
var queryUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// normaliseQuery spells a query the way the site's search expects it.
func normaliseQuery(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	q = strings.Map(foldDiacritic, q)
	return strings.Trim(queryUnsafe.ReplaceAllString(q, "_"), "_")
}

// foldDiacritic reduces the Latin letters this family's own search folds. It
// is deliberately a small table rather than a Unicode normalisation pass: the
// site folds a specific set, and folding more than it does would produce a
// query it cannot answer.
func foldDiacritic(r rune) rune {
	switch {
	case strings.ContainsRune("àáạảãâầấậẩẫăằắặẳẵ", r):
		return 'a'
	case strings.ContainsRune("èéẹẻẽêềếệểễ", r):
		return 'e'
	case strings.ContainsRune("ìíịỉĩ", r):
		return 'i'
	case strings.ContainsRune("òóọỏõôồốộổỗơờớợởỡ", r):
		return 'o'
	case strings.ContainsRune("ùúụủũưừứựửữ", r):
		return 'u'
	case strings.ContainsRune("ỳýỵỷỹ", r):
		return 'y'
	case r == 'đ':
		return 'd'
	default:
		return r
	}
}
