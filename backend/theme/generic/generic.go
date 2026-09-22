// Package generic is the escape hatch: a source that matches no theme,
// driven by raw CSS selectors from its own config and, if that is not enough,
// a small goja script.
//
// PLAN §6 M2 is explicit that this is an "escape hatch, not the main path",
// so this package is deliberately small and deliberately unambitious. It does
// not try to be a scraping framework. If a site needs more than a dozen
// selectors and one function, the right answer is a new theme in its own
// package and an afternoon with docs/THEME-NOTES.md — not more machinery here.
//
// Two rules are enforced rather than documented:
//
//   - `selectors` and `script` are legal only on a source whose theme is
//     "generic". schema/source.schema.json says so with an allOf clause and
//     theme.Registry.Validate says so again in Go, because an imported source
//     never met the schema.
//   - The script runs with no host bindings at all: no fetch, no file access,
//     no timers, and a wall-clock interrupt. It transforms a string we already
//     fetched into a list of strings. Anything more would put an arbitrary
//     program from a config file between Quire and the network, which is
//     exactly what the §7.4 fetch invariants exist to prevent.
//
// # The pageReferrer override
//
// A CSS-selector site can still fail for a reason that has nothing to do with
// selectors: its image host answers 403 to a request with no `Referer` and 200
// to the identical one with `Referer` naming one of the site's own pages
// (PLAN §7.6). comick, fanfox and webtoons answer this by implementing
// theme.PageReferrer; the escape hatch has no fixed page shape to hang that on
// automatically, so it is a config knob instead: KeyPageReferrer, off by
// default.
//
// It defaults to off, and not merely to a safe default, because sending a
// Referer nobody asked for is sending a header the site did not require —
// exactly what PLAN §7.6 says not to do. A generic source configured against
// an ordinary site should behave byte-for-byte as it always has; a user turns
// this on only once they have measured, as the finding behind this feature
// did, that the image host actually wants one.
//
// When it is on, the value sent is never invented: it is the exact absolute
// URL Pages() itself requested for that chapter — the same one-line
// s.Resolve(chapterID) call t.doc already makes — so the header states a fact
// about a request Quire actually made, not a fabrication. See PageReferer
// below.
//
// # searchPath and browsePath
//
// Search() is used for both a text query and a browse (empty query), and one
// measured site could not answer both from the same URL: its search endpoint
// returned correctly filtered results for a query and zero results for an
// empty one, while its browse listing lived at a different path and ignored
// any query it was given. selectors.browsePath exists for exactly that split:
// it is the template used when the query is empty, falling back to
// searchPath when absent so a source that never needed the distinction is
// unaffected. See BrowsePath below.
package generic

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID. It must match theme.GenericID, which is what
// the selectors/script restriction keys off.
const ID = theme.GenericID

// Selector keys. These are the complete vocabulary; anything else in a
// source's `selectors` map is a validation error, for the same reason unknown
// override keys are (PLAN §7.2): a typo that silently does nothing is the
// worst possible outcome for a hand-configured source.
const (
	// SearchPath is a URL template, not a selector. {query} and {page} are
	// substituted; {query} is URL-escaped.
	SearchPath = "searchPath"

	// BrowsePath is a URL template, not a selector, used in place of
	// SearchPath when the query is empty. It exists because a browse listing
	// and a text search do not always live at the same URL: one measured site
	// answers a query at /search/?s={query} but returns zero results there
	// for an empty one, while its 24-item browse listing lives at / and
	// ignores any query entirely. Absent BrowsePath, Search behaves exactly
	// as it always has: SearchPath serves both jobs. {page} is substituted
	// the same way as in SearchPath; {query} is substituted with the empty
	// string rather than rejected at validation, because it is always empty
	// by construction here (this template is only ever used for the
	// no-query case) and a literal "{query}" left in the URL would be a
	// worse failure than a silently empty one.
	BrowsePath = "browsePath"

	SearchItem  = "searchItem"
	SearchLink  = "searchLink"
	SearchTitle = "searchTitle"
	SearchCover = "searchCover"

	// SeriesPath is a URL template with {id}. When absent, the stored series
	// ID is used as the path directly.
	SeriesPath = "seriesPath"

	SeriesTitle       = "seriesTitle"
	SeriesCover       = "seriesCover"
	SeriesDescription = "seriesDescription"
	SeriesGenres      = "seriesGenres"
	SeriesStatus      = "seriesStatus"

	ChapterItem  = "chapterItem"
	ChapterLink  = "chapterLink"
	ChapterTitle = "chapterTitle"
	ChapterDate  = "chapterDate"

	PageImage = "pageImage"
)

// knownSelectors is the closed vocabulary above, sorted for error messages.
var knownSelectors = []string{
	BrowsePath,
	ChapterDate, ChapterItem, ChapterLink, ChapterTitle,
	PageImage,
	SearchCover, SearchItem, SearchLink, SearchPath, SearchTitle,
	SeriesCover, SeriesDescription, SeriesGenres, SeriesPath, SeriesStatus, SeriesTitle,
}

// spec declares the one override the generic theme has. Everything else about
// a generic source is a selector, which is the point.
var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key: KeyDateFormat, Kind: theme.KindString, Default: "MMMM d, yyyy",
			Why: "chapter dates, in the same CLDR-ish spelling the themed sources use",
		},
		{
			Key: KeyScriptTimeoutMs, Kind: theme.KindInt, Default: 2000,
			Why: "wall-clock limit on the goja hook; a script that overruns is interrupted rather than allowed to hang the backend",
		},
		{
			Key: KeyPageReferrer, Kind: theme.KindBool, Default: false,
			Why: "some image hosts 403 a page-image request with no Referer and 200 the identical one naming the chapter page (PLAN §7.6); off by default because most sites need no such header, and this sends one only for the page Pages() actually fetched",
		},
	},
}

// Override keys.
const (
	KeyDateFormat      = "dateFormat"
	KeyScriptTimeoutMs = "scriptTimeoutMs"

	// KeyPageReferrer opts a source into sending a `Referer` header with its
	// page images, naming the chapter page Pages() fetched. See PageReferer.
	KeyPageReferrer = "pageReferrer"
)

// ErrNotConfigured is returned when a generic source is missing the selectors
// an operation needs. It is a distinct error so the UI can say "this source
// needs a chapterItem selector" rather than "no chapters found".
var ErrNotConfigured = errors.New("generic source is missing a required selector")

// Theme is the generic theme.
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
// whose every request is classified as discovery (PLAN §7.4), for the unattended
// watched-series check of PLAN §12.2. The escape hatch issues no retrieval-
// classified request today; see madara's for why it implements this regardless.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// SuggestedName implements theme.Theme.
//
// Empty. The escape hatch is pointed at a site nobody has written a theme for,
// so it knows nothing about that site — least of all what it is called.
func (t *Theme) SuggestedName() string { return "" }

// AllowedHosts implements theme.Theme.
//
// Nil. The generic theme is the escape hatch for a site nobody has written a
// theme for, so there is nothing it could know about that site's hosting. A
// user who needs one sets allowedHosts on the source itself.
func (t *Theme) AllowedHosts() []string { return nil }

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// Fingerprint implements theme.Theme and always returns zero.
//
// The generic theme has no shape to recognise: it *is* whatever its config
// says. Scoring anything above zero would let it win pages that belong to a
// real theme, and would cost the probe its ability to answer "unrecognised"
// (PLAN §7.5). theme.Registry.Fingerprint excludes it from the fan-out for the
// same reason; this is the second lock on that door.
func (t *Theme) Fingerprint(*probe.Page) int { return 0 }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// ValidateSelectors checks a source's selectors map against the closed
// vocabulary. theme.Registry.Validate does not know about selectors beyond
// "only generic may have them", so this is called from Validate below.
func ValidateSelectors(sel map[string]string) error {
	keys := make([]string, 0, len(sel))
	for k := range sel {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !slices.Contains(knownSelectors, k) {
			return fmt.Errorf("theme %s: unknown selector %q; accepted selectors are %s",
				ID, k, strings.Join(knownSelectors, ", "))
		}
		if strings.TrimSpace(sel[k]) == "" {
			return fmt.Errorf("theme %s: selector %q is empty", ID, k)
		}
	}
	return nil
}

// Validate checks a generic source beyond what the registry can: the selector
// vocabulary, and that the script compiles. A script that fails to compile
// must be rejected when the source is added, not when a chapter is opened.
func (t *Theme) Validate(s *theme.Source) error {
	if err := ValidateSelectors(s.Selectors); err != nil {
		return err
	}
	if s.Script != "" {
		if _, err := compile(s.Script); err != nil {
			return fmt.Errorf("theme %s: script: %w", ID, err)
		}
	}
	return nil
}

// Search implements theme.Theme.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	sel := s.Selectors
	// An empty query is a browse, not a search: use BrowsePath when the
	// source configured one, and fall back to SearchPath otherwise so a
	// source that never set BrowsePath behaves exactly as it always has.
	var path string
	if q == "" && sel[BrowsePath] != "" {
		path = sel[BrowsePath]
	} else {
		path = sel[SearchPath]
	}
	if path == "" {
		path = "/?s={query}"
	}
	if sel[SearchItem] == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotConfigured, SearchItem)
	}
	if page < 1 {
		page = 1
	}
	path = strings.ReplaceAll(path, "{query}", url.QueryEscape(q))
	path = strings.ReplaceAll(path, "{page}", strconv.Itoa(page))

	doc, _, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}
	// The search/browse page just fetched above — the page these covers are
	// actually parsed from, resolved the same way t.doc resolved it. PLAN
	// §7.6: truthful, per-request, never a constant.
	pageURL, _ := s.Resolve(path)

	var out []theme.SeriesStub
	doc.Find(sel[SearchItem]).Each(func(_ int, item *goquery.Selection) {
		link := item
		if sel[SearchLink] != "" {
			link = item.Find(sel[SearchLink]).First()
		}
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		id := relative(s, href)
		if id == "" {
			return
		}
		title := theme.Text(link)
		if sel[SearchTitle] != "" {
			title = theme.Text(item.Find(sel[SearchTitle]).First())
		}
		var cover string
		if sel[SearchCover] != "" {
			cover = theme.ImageURL(item.Find(sel[SearchCover]).First())
		}
		stub := theme.SeriesStub{ID: id, Title: title, CoverURL: cover}
		if stub.CoverURL != "" && pageURL != "" {
			stub.CoverReferrer = pageURL
		}
		out = append(out, stub)
	})
	return out, nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	path := seriesPath(s, id)
	doc, _, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}
	sel := s.Selectors
	out := &theme.Series{ID: id}
	if sel[SeriesTitle] != "" {
		out.Title = theme.Text(doc.Find(sel[SeriesTitle]).First())
	}
	if sel[SeriesCover] != "" {
		out.CoverURL = theme.ImageURL(doc.Find(sel[SeriesCover]).First())
		if out.CoverURL != "" {
			// The series page just fetched above, the page the cover markup
			// was actually parsed from — not a value assembled by hand.
			if abs, err := s.Resolve(path); err == nil {
				out.CoverReferrer = abs
			}
		}
	}
	if sel[SeriesDescription] != "" {
		out.Description = theme.Text(doc.Find(sel[SeriesDescription]).First())
	}
	if sel[SeriesGenres] != "" {
		doc.Find(sel[SeriesGenres]).Each(func(_ int, g *goquery.Selection) {
			if v := theme.Text(g); v != "" {
				out.Genres = append(out.Genres, v)
			}
		})
	}
	if sel[SeriesStatus] != "" {
		out.Status = normaliseStatus(theme.Text(doc.Find(sel[SeriesStatus]).First()))
	}
	return out, nil
}

// Chapters implements theme.Theme. A script hook may replace the selector
// path entirely by exporting a `chapters` function.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	path := seriesPath(s, id)
	doc, body, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}

	if s.Script != "" {
		abs, _ := s.Resolve(path)
		vals, ok, err := runHook(s.Script, "chapters", body, abs, o.Int(KeyScriptTimeoutMs))
		if err != nil {
			return nil, fmt.Errorf("theme %s: script chapters(): %w", ID, err)
		}
		if ok {
			return t.chaptersFromScript(s, vals), nil
		}
	}

	sel := s.Selectors
	if sel[ChapterItem] == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotConfigured, ChapterItem)
	}
	format := o.String(KeyDateFormat)
	now := t.now()

	var out []theme.Chapter
	doc.Find(sel[ChapterItem]).Each(func(_ int, item *goquery.Selection) {
		link := item
		if sel[ChapterLink] != "" {
			link = item.Find(sel[ChapterLink]).First()
		}
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		rel := relative(s, href)
		if rel == "" {
			return
		}
		title := theme.Text(link)
		if sel[ChapterTitle] != "" {
			title = theme.Text(item.Find(sel[ChapterTitle]).First())
		}
		ch := theme.Chapter{ID: rel, Title: title, Number: theme.ChapterNumber(title)}
		if sel[ChapterDate] != "" {
			ch.Published = theme.ParseDate(theme.Text(item.Find(sel[ChapterDate]).First()), format, now)
		}
		out = append(out, ch)
	})
	// PLAN §7.2: ascending reading order. The escape hatch knows nothing about
	// the site it is pointed at — including which way round its chapter list
	// runs — so it normalises like everyone else and admits it when the
	// configured selectors yield nothing orderable.
	return theme.SortAndMark(out), nil
}

// chaptersFromScript turns the script's return value into chapters. The script
// returns an array of hrefs or of {id,title} objects; both are accepted
// because a one-off site is exactly where a user should not have to learn a
// schema.
func (t *Theme) chaptersFromScript(s *theme.Source, vals []scriptItem) []theme.Chapter {
	out := make([]theme.Chapter, 0, len(vals))
	for _, v := range vals {
		rel := relative(s, v.ID)
		if rel == "" {
			continue
		}
		title := v.Title
		if title == "" {
			title = rel
		}
		out = append(out, theme.Chapter{ID: rel, Title: title, Number: theme.ChapterNumber(title)})
	}
	// The script hook gets the same treatment as the selector path. A user's
	// script is not trusted to have got the direction right, and it has no way
	// to tell us that it did.
	return theme.SortAndMark(out)
}

// Pages implements theme.Theme. This is the operation the script hook exists
// for: a one-off site whose page list is behind an encoding or an obfuscated
// blob that no selector can reach.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	doc, body, err := t.doc(ctx, s, chapterID)
	if err != nil {
		return nil, err
	}

	if s.Script != "" {
		abs, _ := s.Resolve(chapterID)
		vals, ok, err := runHook(s.Script, "pages", body, abs, o.Int(KeyScriptTimeoutMs))
		if err != nil {
			return nil, fmt.Errorf("theme %s: script pages(): %w", ID, err)
		}
		if ok {
			return absolutise(s, itemIDs(vals)), nil
		}
	}

	sel := s.Selectors
	if sel[PageImage] == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotConfigured, PageImage)
	}
	var raw []string
	doc.Find(sel[PageImage]).Each(func(_ int, img *goquery.Selection) {
		if u := theme.ImageURL(img); u != "" {
			raw = append(raw, u)
		}
	})
	return absolutise(s, raw), nil
}

// PageReferer implements theme.PageReferrer (PLAN §7.6). It answers "" unless
// the source has set the pageReferrer override, which defaults to off — a
// generic source that never asked for this behaves exactly as it always has.
//
// When it is on, the page named is the exact URL Pages() fetches for
// chapterID: the same s.Resolve(chapterID) call t.doc makes to read that
// chapter's markup. That makes the header a true statement about a request
// Quire actually made, never one invented for the occasion. An unresolvable
// chapterID yields "" and no header, the same as an unconfigured source.
func (t *Theme) PageReferer(s *theme.Source, chapterID string) string {
	o, err := spec.Resolve(s.Overrides)
	if err != nil || !o.Bool(KeyPageReferrer) {
		return ""
	}
	abs, err := s.Resolve(chapterID)
	if err != nil {
		return ""
	}
	return abs
}

func seriesPath(s *theme.Source, id string) string {
	if tmpl := s.Selectors[SeriesPath]; tmpl != "" {
		return strings.ReplaceAll(tmpl, "{id}", strings.Trim(id, "/"))
	}
	return id
}

func relative(s *theme.Source, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	base, err := url.Parse(s.BaseURL)
	if err != nil {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	abs := base.ResolveReference(u)
	// theme.SameSite rather than an exact host comparison: a site that
	// redirects its apex to www emits absolute links there, and comparing
	// exactly discarded every one of them. See site.go — this was worth a
	// live search returning zero results.
	if !theme.SameSite(abs.Hostname(), base.Hostname()) {
		return ""
	}
	out := abs.EscapedPath()
	if abs.RawQuery != "" {
		out += "?" + abs.RawQuery
	}
	return out
}

func absolutise(s *theme.Source, raw []string) []string {
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

func itemIDs(items []scriptItem) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.ID)
	}
	return out
}

func (t *Theme) doc(ctx context.Context, s *theme.Source, path string) (*goquery.Document, string, error) {
	p, err := s.Policy()
	if err != nil {
		return nil, "", err
	}
	abs, err := s.Resolve(path)
	if err != nil {
		return nil, "", err
	}
	resp, err := t.f.Get(ctx, p, abs)
	if err != nil {
		return nil, "", fmt.Errorf("%s: get %s: %w", ID, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("%s: %s: HTTP %d", ID, path, resp.StatusCode)
	}
	body := string(resp.Body)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %s: parse HTML: %w", ID, path, err)
	}
	return doc, body, nil
}

func normaliseStatus(s string) string {
	switch l := strings.ToLower(strings.TrimSpace(s)); {
	case strings.Contains(l, "ongoing"), strings.Contains(l, "publishing"):
		return theme.StatusOngoing
	case strings.Contains(l, "completed"), strings.Contains(l, "finished"):
		return theme.StatusCompleted
	case strings.Contains(l, "hiatus"):
		return theme.StatusHiatus
	case strings.Contains(l, "cancel"), strings.Contains(l, "dropped"):
		return theme.StatusCancelled
	default:
		return theme.StatusUnknown
	}
}
