// Package webtoons implements a vertical-scroll publisher platform as a Quire
// theme.
//
// # What makes it different from everything else here
//
// It is the first **first-party publisher** in the registry rather than an
// aggregator or a CMS family: the site hosts work it licenses or publishes
// itself, and it has an app, a mobile web surface and a desktop one that do
// not agree about anything.
//
//   - **Search and series detail are desktop HTML; the chapter list is a JSON
//     API on the mobile host.** Not a different path — a different hostname,
//     `m.` instead of `www.`, under the same registrable domain. The desktop
//     series page paginates its episode list ten at a time; the mobile API
//     returns all of them in one response.
//   - **There are two kinds of series** — the publisher's own and a
//     user-submitted tier — and they are the same shape everywhere except in
//     the chapter-list endpoint, which takes a different path segment for
//     each. The kind is readable from the series URL, which is why it is not
//     an override.
//   - **The chapter number is not in the title.** Episodes are titled things
//     like "[Season 1] Ep. 0", where a number-finding heuristic reads the
//     season as the chapter. The API's own episode number is authoritative and
//     monotonic across seasons, so that is what is used, and the season
//     becomes the volume label.
//
// # The strip problem, which this theme does not solve
//
// The pages here are webtoon *strips*: single images several thousand pixels
// tall, meant to be scrolled. PLAN §6 M4 fits a page image into a 3:4 PDF
// page, so a strip arrives as a small centred sliver with white space either
// side — output that is technically correct and unreadable. M4's memory guard
// bounds the damage and does not change the result.
//
// Fixing it needs real strip-splitting — cutting a tall image into
// screen-shaped pages at sensible boundaries — which is a design decision of
// its own and deliberately not attempted here. See docs/THEME-NOTES.md. A
// theme that quietly half-solved it would be worse than one that does not try.
//
// # Provenance
//
// Written from HTML and JSON observed live on 2026-09-16, and from endpoint
// shapes read as *facts* from the prior art PLAN §1.4 permits reading. No
// selector block and no line of anyone's code is transcribed. Fixtures are
// synthetic pages on example.invalid (PLAN §1.3).
package webtoons

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "webtoons"

// The path vocabulary. Constants rather than overrides: this theme drives one
// publisher, and a knob nobody can usefully set is one that only gets typed
// wrong.
const (
	// canvasSegment is the path segment marking the user-submitted tier. It is
	// also, confusingly, *not* what the chapter-list API calls it — see
	// apiKind.
	canvasSegment = "canvas"

	// apiPrefix is the mobile host's chapter-list API.
	apiPrefix = "/api/v1"

	// titleNoParam identifies a series. The site spells it two ways depending
	// on which surface generated the link, and both are accepted.
	titleNoParam    = "title_no"
	titleNoParamAlt = "titleNo"

	// episodePageSize asks for every episode in one response. The API honours
	// an arbitrarily large value and there is no cursor to walk when it does.
	episodePageSize = 5000
)

// Override keys.
const (
	// KeySearchScope narrows search to one of the two tiers.
	//
	// It exists because the user-submitted tier is very large and its entries
	// dominate a text search — a query for a licensed title returns the
	// publisher's own edition somewhere below a page of fan work with similar
	// names. Which of those a reader wants is genuinely a preference.
	KeySearchScope = "searchScope"

	// KeyFullQualityImages requests the unresized originals.
	//
	// Page URLs carry a quality parameter; dropping it yields the full-size
	// image. Off by default because M4 downscales to a 1620x2160 screen
	// anyway, so the extra bytes are spent and then thrown away — and these
	// are strips, which are already the largest images Quire fetches.
	KeyFullQualityImages = "fullQualityImages"
)

// searchScopes are the values KeySearchScope accepts. "all" is the site's own
// unscoped search; the other two are path segments it understands.
var searchScopes = []string{"all", "originals", canvasSegment}

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key:     KeySearchScope,
			Kind:    theme.KindString,
			Default: "all",
			Enum:    searchScopes,
			Why: "The user-submitted tier is large and dominates a text search, so a " +
				"query for a licensed title can return a page of fan work before the " +
				"publisher's own edition. This narrows the search to one tier.",
		},
		{
			Key:     KeyFullQualityImages,
			Kind:    theme.KindBool,
			Default: false,
			Why: "Page URLs carry a quality parameter and dropping it yields the " +
				"unresized original. Off by default: M4 downscales to the device's " +
				"screen, so the extra bytes are fetched and then discarded, and these " +
				"pages are already the largest images Quire handles.",
		},
	},
}

// Theme is the webtoons theme.
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

// DiscoveryOnly implements theme.DiscoveryClassifier (PLAN §12.2). Every
// request this theme makes is already discovery; it implements the interface
// so that a caller needing the guarantee can require it rather than infer it
// from silence.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// imageHosts are where the publisher serves page images and thumbnails from.
//
// A different registrable domain from the site — the platform's parent company
// runs its own image infrastructure — so without this declaration PLAN §7.4's
// boundary refuses every page and §7.5 stage 5 fails the probe.
//
// Named exactly rather than with a wildcard, which is as narrow as this can
// be: the parent domain hosts a great deal that has nothing to do with comics,
// so `*.` there would widen the boundary far past anything this theme needs.
// Both labels are single hosts and nothing exists beneath them.
//
// PLAN §1.3's exception for image hosts is what permits naming them at all.
var imageHosts = []string{
	"webtoon-phinf.pstatic.net",
	"swebtoon-phinf.pstatic.net",
}

// AllowedHosts implements theme.Theme.
func (t *Theme) AllowedHosts() []string {
	return append([]string(nil), imageHosts...)
}

// SuggestedName implements theme.Theme. One publisher, one name — and a
// <title> that is a sentence of marketing copy, which is exactly the case
// PLAN §7.5's stage-6 correction was written for.
func (t *Theme) SuggestedName() string { return "WEBTOON" }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// disqualifying markers put the score at zero outright: each belongs to
// software this platform has nothing to do with, so no legitimate page of
// this theme can carry one.
var disqualifying = []string{
	"/wp-content/",              // madara, mangathemesia
	"ts_reader.run(",            // mangathemesia
	"data-chapter-url-template", // mangakakalot
	"checkNewChapter(",          // weebcentral
}

// Fingerprint scores a page for "is this the shape this theme reads?".
//
// The signals are the platform's own card grid and its episode identifiers.
// The strongest is the viewer's image list, which is a single element ID
// nothing else emits; the home and search pages have to reach the threshold
// without it, which the card markup does.
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

	// The viewer. One ID, the images hang directly off it, and nothing else in
	// the registry emits it — so it is weighted to clear the threshold with
	// only the episode-navigation link beside it, which is all a viewer page
	// has.
	add(p.Has("#_imageList"), 50)
	add(p.Contains("/viewer?"+titleNoParam+"="), 15)

	// The card grid, on the home page and on every listing.
	add(p.Has(".webtoon_list"), 25)
	add(p.Contains("data-title-no="), 25)
	add(p.Has(".info_text .title"), 15)
	add(p.Has(".image_wrap"), 10)

	// The series page: the publisher's own and the user-submitted tier use
	// different heading levels for the same class, so both are named.
	add(p.Has("h1.subj, h3.subj"), 20)
	add(p.Has(".detail_header"), 15)
	add(p.Has("#_listUl, ._episodeItem"), 20)

	// The series link shape, which every surface agrees on.
	add(p.Contains("list?"+titleNoParam+"="), 15)

	if score > 100 {
		score = 100
	}
	return score
}

// Search implements theme.Theme.
//
// The site's own search, with the scope override folded in as a path segment.
// An empty query is PLAN §7.5's Browse; the search endpoint answers it with
// the unfiltered listing, so there is no separate browse path here.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	path := "/" + t.lang(s) + "/search"
	scope := o.String(KeySearchScope)
	if scope != "" && scope != "all" {
		path += "/" + scope
	}

	qs := url.Values{}
	qs.Set("keyword", strings.TrimSpace(q))
	if page > 1 {
		// Paging only exists on the scoped searches; the unscoped one answers
		// a fixed set of "best matches" per tier and ignores the parameter.
		// Sending it anyway would invent results that are not there.
		if scope == "all" {
			return nil, nil
		}
		qs.Set("page", strconv.Itoa(page))
	}

	reqPath := path + "?" + qs.Encode()
	doc, err := t.doc(ctx, s, reqPath)
	if err != nil {
		return nil, err
	}
	// The search page just fetched above — the page these covers are
	// actually parsed from, resolved the same way t.doc resolved it. PLAN
	// §7.6: truthful, per-request, never a constant.
	pageURL := t.absolute(s, reqPath)

	var out []theme.SeriesStub
	seen := make(map[string]bool)
	doc.Find(".webtoon_list li a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		id := t.seriesID(s, href)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true

		title := theme.Text(a.Find(".info_text .title, .title").First())
		if title == "" {
			title = theme.Text(a)
		}
		stub := theme.SeriesStub{
			ID:       id,
			Title:    title,
			CoverURL: t.absolute(s, strings.TrimSpace(a.Find("img[src]").First().AttrOr("src", ""))),
		}
		if stub.CoverURL != "" {
			stub.CoverReferrer = pageURL
		}
		out = append(out, stub)
	})
	return out, nil
}

// Series implements theme.Theme.
//
// One page, and the same selectors for both tiers: they differ in heading
// level and in which element carries the author, not in class names.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	seriesPath := t.seriesPath(id)
	doc, err := t.doc(ctx, s, seriesPath)
	if err != nil {
		return nil, err
	}

	out := &theme.Series{
		ID:          id,
		Title:       theme.Text(doc.Find("h1.subj, h3.subj").First()),
		Description: theme.Text(doc.Find("p.summary").First()),
		CoverURL:    t.absolute(s, strings.TrimSpace(doc.Find(".thmb img[src]").First().AttrOr("src", ""))),
	}
	if out.CoverURL != "" {
		// The series page just fetched above, the page the cover markup was
		// actually parsed from — not a value assembled by hand.
		out.CoverReferrer = t.absolute(s, seriesPath)
	}

	// The author block holds a button on one tier and a profile link on the
	// other. Removing the button rather than reading only the link is what
	// keeps both working: taking the link alone loses the publisher's own
	// credits, which are a bare text node.
	if area := doc.Find(".author_area").First(); area.Length() > 0 {
		clone := area.Clone()
		clone.Find("button").Remove()
		if a := theme.Text(clone); a != "" {
			out.Authors = append(out.Authors, a)
		}
	}

	seenGenre := make(map[string]bool)
	doc.Find("h2.genre, p.genre").Each(func(_ int, g *goquery.Selection) {
		v := theme.Text(g)
		if v == "" || seenGenre[v] {
			return
		}
		seenGenre[v] = true
		out.Genres = append(out.Genres, v)
	})

	// The platform publishes an update schedule rather than a status, and
	// "EVERY MONDAY" is not one of PLAN §7.2's five values. A completed series
	// says so in the same place, so that is the one case that maps.
	sched := strings.ToLower(theme.Text(doc.Find(".day_info, .detail_body .day_info").First()))
	switch {
	case strings.Contains(sched, "completed"):
		out.Status = theme.StatusCompleted
	case sched != "":
		out.Status = theme.StatusOngoing
	}

	return out, nil
}

// episodeList is the chapter-list API's envelope.
type episodeList struct {
	Result struct {
		EpisodeList []episode `json:"episodeList"`
	} `json:"result"`
}

type episode struct {
	EpisodeNo          int    `json:"episodeNo"`
	EpisodeTitle       string `json:"episodeTitle"`
	ViewerLink         string `json:"viewerLink"`
	ExposureDateMillis int64  `json:"exposureDateMillis"`
}

// Chapters implements theme.Theme.
//
// It goes to the mobile host's JSON API, not to the series page. The series
// page paginates its episode list ten at a time, so reading it would mean
// walking pages to assemble a list the API hands over in one response — and
// would give a *short* list to anyone who forgot to walk them, which is the
// failure mode this project has now met twice.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	titleNo, kind, err := t.seriesRef(id)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	qs := url.Values{}
	qs.Set("pageSize", strconv.Itoa(episodePageSize))
	if kind == apiKindCanvas {
		// The user-submitted tier is translated by its authors, so the API
		// wants to know which language's episodes to list. The publisher's own
		// tier is per-site and rejects the parameter.
		qs.Set("readingLanguageCode", t.lang(s))
	}

	raw, err := t.get(ctx, s, t.mobileURL(s, apiPrefix+"/"+kind+"/"+titleNo+"/episodes?"+qs.Encode()))
	if err != nil {
		return nil, err
	}

	var list episodeList
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("%s: episodes for %s: parse JSON: %w", ID, id, err)
	}

	out := make([]theme.Chapter, 0, len(list.Result.EpisodeList))
	for _, e := range list.Result.EpisodeList {
		rel := t.relativeID(s, e.ViewerLink)
		if rel == "" {
			continue
		}
		ch := theme.Chapter{
			ID:    rel,
			Title: theme.Collapse(e.EpisodeTitle),
			// The API's own episode number, not one parsed out of the title.
			// Titles here read "[Season 2] Ep. 14", and a number-finding
			// heuristic returns the season — which would sort season 2 before
			// season 1 episode 3 and produce a volume that reads out of order.
			// This number is monotonic across the whole series by
			// construction, which is exactly what the ordering contract wants.
			Number: float64(e.EpisodeNo),
			Volume: seasonLabel(e.EpisodeTitle),
		}
		if e.ExposureDateMillis > 0 {
			ch.Published = time.UnixMilli(e.ExposureDateMillis).UTC()
		}
		out = append(out, ch)
	}

	// PLAN §7.2's ordering contract. The API answers ascending today, so this
	// is very nearly a no-op — and it runs anyway, for the reason mangadex
	// gives: "ascending today" is a property of someone else's server, and the
	// cost of checking is one pass over a slice we already own.
	return theme.SortAndMark(out), nil
}

// seasonLabel pulls a season out of an episode title, for Chapter.Volume.
//
// PLAN §7.2 wants a volume label where the source has one, because M4 groups
// by volume and falls back to runs of ten only when there is nothing better.
// A season is the nearest thing this platform publishes, and it is a label
// rather than a number for the same reason the field is: "1" and "Season 1"
// are both what sites give.
func seasonLabel(title string) string {
	m := seasonRE.FindStringSubmatch(title)
	if m == nil {
		return ""
	}
	return "Season " + m[1]
}

// Pages implements theme.Theme.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	path := t.chapterPath(chapterID)
	doc, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}

	full := o.Bool(KeyFullQualityImages)

	var out []string
	doc.Find("#_imageList img").Each(func(_ int, img *goquery.Selection) {
		// The real URL is in data-url; src holds a transparent placeholder
		// until the reader scrolls. Reading src would yield a chapter of
		// nothing, which is why this does not use theme.ImageURL: that helper
		// knows the lazy-load attributes WordPress plugins use, and this is
		// not one of them.
		raw := strings.TrimSpace(img.AttrOr("data-url", ""))
		if raw == "" {
			return
		}
		if full {
			raw = dropQualityParam(raw)
		}
		if abs := t.absolute(s, raw); abs != "" {
			out = append(out, abs)
		}
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("%s: %s: no page images in the viewer", ID, path)
	}
	return out, nil
}

// dropQualityParam removes the rendition parameter, yielding the original.
func dropQualityParam(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if !q.Has("type") {
		return raw
	}
	q.Del("type")
	u.RawQuery = q.Encode()
	return u.String()
}

// PageReferer implements theme.PageReferrer (PLAN §7.6).
//
// The image host answers 403 "Referral Denied" to a request that does not name
// one of the publisher's pages, and 200 to one that does. The page named here
// is **the viewer page Pages() fetches** — the same URL, built the same way —
// so the header is a true statement and not the fabricated one §7.6 forbids.
// When the chapter ID does not resolve, this returns "" and no header is sent.
func (t *Theme) PageReferer(s *theme.Source, chapterID string) string {
	return t.absolute(s, t.chapterPath(chapterID))
}
