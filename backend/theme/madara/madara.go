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
package madara

import (
	"context"
	"fmt"
	"net/url"
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

	// The plugin's own asset path. Only the plugin puts files here.
	add(p.Contains("/wp-content/plugins/madara/"), 40)

	// The custom post type, which appears in body classes, search forms and
	// REST links alike.
	add(p.Contains("wp-manga"), 25)

	// The AJAX action name, present in inline script on series pages of both
	// the current and the legacy shape.
	add(p.Contains("manga_get_chapters"), 20)

	// Markup landmarks. Individually weak, collectively decisive.
	add(p.Has("#manga-chapters-holder"), 15)
	add(p.Has("li.wp-manga-chapter"), 15)
	add(p.Has(".c-tabs-item__content"), 10)
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

	doc, err := t.doc(ctx, s, path+"?"+qs.Encode())
	if err != nil {
		return nil, err
	}

	var out []theme.SeriesStub
	// Search results render as tab items; a few skins use the same inner
	// markup inside a plain .row, so match on the inner .post-title anchor
	// rather than on the container.
	doc.Find(".c-tabs-item__content, .page-listing-item .page-item-detail").Each(func(_ int, sel *goquery.Selection) {
		link := sel.Find(".post-title h3 a, .post-title h4 a, .post-title a, h3.h4 a").First()
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		id := t.seriesID(s, href)
		if id == "" {
			return
		}
		out = append(out, theme.SeriesStub{
			ID:       id,
			Title:    theme.Text(link),
			CoverURL: theme.ImageURL(sel.Find("img").First()),
		})
	})
	return out, nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	doc, err := t.doc(ctx, s, t.seriesPath(s, id))
	if err != nil {
		return nil, err
	}

	out := &theme.Series{ID: id}
	out.Title = theme.Text(doc.Find(".post-title h1, .post-title h3, .post-title h2").First())
	out.CoverURL = theme.ImageURL(doc.Find(".summary_image img, .tab-summary img").First())
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
