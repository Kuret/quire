// Package fanfox implements an older PHP reader platform as a Quire theme.
//
// # What makes it different from everything else here
//
// It is the oldest-generation software in the registry: server-rendered PHP
// with numbered utility class names (`manga-list-1-list`, `manga-list-4-list`)
// that have plainly accreted over a decade. Three things follow, and all three
// are worth knowing before reading the code.
//
//  1. **The desktop reader is not usable and the mobile one is.** The desktop
//     reader hands out one page at a time behind an obfuscated script. The
//     mobile host — `m.` instead of the configured hostname, same registrable
//     domain — serves a scroll view that ships every page URL in the markup at
//     once. So Pages() changes host and rewrites one path segment, and that is
//     the whole trick.
//
//  2. **The volume is in the URL.** Chapter links read
//     `/{series}/v01/c006/1.html`, so this is one of the few sites here that
//     publishes real volume structure. PLAN §6 M4 groups chapters into volume
//     PDFs and falls back to runs of ten only when there is nothing better;
//     here there is something better, and it is free.
//
//  3. **Licensed series are visible and unreadable.** A series the publisher
//     has licensed still has a page, a chapter list and working links, and its
//     reader answers 200 with "it's licensed and not available" in place of
//     the images. Pages() reports that as what it is rather than as an empty
//     chapter, because an empty chapter reads as "nothing to download".
//
// # Provenance
//
// Written from HTML observed live on 2026-09-16, and from endpoint shapes read
// as *facts* from the prior art PLAN §1.4 permits reading. No selector block
// and no line of anyone's code is transcribed. Fixtures are synthetic pages on
// example.invalid (PLAN §1.3).
package fanfox

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// ID is the registered theme ID.
const ID = "fanfox"

const (
	// seriesSegment is the path segment series live under, and the one the
	// mobile reader replaces.
	seriesSegment = "manga"

	// readerSegment is what seriesSegment becomes on the mobile host: the
	// scroll view, which is the only surface that lists every page at once.
	readerSegment = "roll_manga"

	// browsePath is the directory listing, which is what an empty query gets.
	browsePath = "/directory/"
)

// Override keys.
const (
	// KeyBrowseOrder is what the directory is sorted by when the query is
	// empty.
	//
	// Browse is a search with an empty query (PLAN §7.5, M3 correction 1), and
	// this site's search endpoint has nothing to say about an empty string, so
	// Browse goes to the directory instead. The directory has exactly two
	// orders and they answer different questions — "what is popular" and "what
	// moved today" — so which one a reader wants is a preference.
	KeyBrowseOrder = "browseOrder"

	// KeyDateFormat is the site's chapter date format.
	//
	// It is an override rather than a constant for the reason madara and
	// mangathemesia have one: the rendered date follows a server-side locale
	// setting, and a site that changes it should be a config change rather
	// than a new build. The default is what this one renders today.
	KeyDateFormat = "dateFormat"
)

var browseOrders = []string{"popular", "latest"}

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key:     KeyBrowseOrder,
			Kind:    theme.KindString,
			Default: "popular",
			Enum:    browseOrders,
			Why: "Browse is a search with an empty query (PLAN §7.5) and this site's " +
				"search endpoint cannot answer one, so Browse reads the directory. The " +
				"directory has two orders, and which one is wanted is a preference.",
		},
		{
			Key:     KeyDateFormat,
			Kind:    theme.KindString,
			Default: "MMM d,yyyy",
			Why: "Chapter dates are rendered by the server in a locale-dependent format. " +
				"Relative dates (\"Today\", \"3 days ago\") are recognised without it; this " +
				"is for the absolute ones.",
		},
	},
}

// Theme is the fanfox theme.
type Theme struct {
	f   theme.Fetcher
	now func() time.Time
}

// New builds the theme over a fetcher.
func New(f theme.Fetcher) *Theme { return &Theme{f: f, now: time.Now} }

// NewWithClock is New with an injectable clock. Relative chapter dates are
// resolved against it, so a fixture's "3 days ago" is a fixed date in a test.
func NewWithClock(f theme.Fetcher, now func() time.Time) *Theme {
	return &Theme{f: f, now: now}
}

// DiscoveryOnly implements theme.DiscoveryClassifier (PLAN §12.2).
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// imageHosts are where this site serves page images and covers from.
//
// Two different registrable domains, neither of them the site's: pages come
// from one and covers from another, both of them leftovers from names the
// platform used to operate under. Without the declaration PLAN §7.4's boundary
// refuses every page and §7.5 stage 5 fails the probe.
//
// The wildcard form is the honest one here — the hosts are shard labels
// (`zjcdn`, `fmcdn`) and the apexes serve nothing — and PLAN §1.3's exception
// for image hosts is what permits naming them at all.
var imageHosts = []string{
	"*.mangafox.me",
	"*.mfcdn.net",
}

// AllowedHosts implements theme.Theme.
func (t *Theme) AllowedHosts() []string {
	return append([]string(nil), imageHosts...)
}

// SuggestedName implements theme.Theme. One site, and a <title> that is a
// sentence of marketing copy — the case PLAN §7.5's stage-6 correction exists
// for.
func (t *Theme) SuggestedName() string { return "Manga Fox" }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// disqualifying markers put the score at zero: each belongs to software this
// site has nothing to do with.
var disqualifying = []string{
	"/wp-content/",              // madara, mangathemesia
	"ts_reader.run(",            // mangathemesia
	"data-chapter-url-template", // mangakakalot
	"checkNewChapter(",          // weebcentral
}

// disqualifyingSelectors are the same idea for markers that are elements
// rather than strings. They are a separate list because a substring search for
// "#_imageList" finds nothing in a document that spells it id="_imageList",
// which is a mistake worth making impossible rather than remembering.
var disqualifyingSelectors = []string{
	"#_imageList", // webtoons' viewer
}

// Fingerprint scores a page for "is this the shape this theme reads?".
//
// The signals are the site's numbered list classes, which are as distinctive
// as a fingerprint gets precisely because they are so graceless: no other
// software in this registry names a grid `manga-list-4-list`. They are weighted
// as a set rather than individually, because the numbers vary by page and no
// single page carries them all.
func (t *Theme) Fingerprint(p *probe.Page) int {
	if p == nil {
		return 0
	}
	for _, m := range disqualifying {
		if p.Contains(m) {
			return 0
		}
	}
	for _, sel := range disqualifyingSelectors {
		if p.Has(sel) {
			return 0
		}
	}

	score := 0
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	// Listings: home, directory and search. The numbered families.
	add(p.Has(".manga-list-1-list, .manga-list-2-list, .manga-list-4-list"), 30)
	add(p.Has(".manga-list-1-item-title, .manga-list-2-item-title, .manga-list-4-item-title"), 20)
	add(p.Has(".manga-list-1-cover, .manga-list-4-cover"), 15)

	// Series page.
	add(p.Has(".detail-info-right"), 25)
	add(p.Has("ul.detail-main-list"), 25)
	add(p.Has(".detail-main-list-main .title3"), 15)
	add(p.Has(".detail-info-cover-img"), 10)

	// Mobile reader. Weighted to clear the threshold on their own with a
	// margin: a reader page carries none of the listing or series markup, and
	// these two elements together are this software and nothing else.
	add(p.Has("#viewer .reader-page"), 45)
	add(p.Has(".roll-page"), 20)

	// The chapter URL shape, which carries the volume.
	add(p.Contains("/"+seriesSegment+"/"), 5)

	if score > 100 {
		score = 100
	}
	return score
}

// Search implements theme.Theme.
//
// An empty query is PLAN §7.5's Browse, and this site's search endpoint cannot
// answer one — it returns the whole catalogue in no useful order — so Browse
// reads the directory instead, which is what the directory is for.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	var path string
	if q = strings.TrimSpace(q); q == "" {
		path = browsePath
		if page > 1 {
			// Pages are files, not parameters: /directory/3.html.
			path += strconv.Itoa(page) + ".html"
		}
		if o.String(KeyBrowseOrder) == "latest" {
			// A valueless parameter, which is how the site spells it. Building
			// it with url.Values would produce "latest=" and sort by rank.
			path += "?latest"
		}
	} else {
		qs := url.Values{}
		qs.Set("title", q)
		qs.Set("page", strconv.Itoa(page))
		// The search form's own hidden fields. Without stype the endpoint
		// searches nothing and answers an empty list, which looks exactly like
		// "no results" and is not.
		qs.Set("stype", "1")
		path = "/search?" + qs.Encode()
	}

	doc, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}

	var out []theme.SeriesStub
	seen := make(map[string]bool)
	doc.Find(".manga-list-1-list li, .manga-list-2-list li, .manga-list-4-list li").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a[href]").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		id := t.seriesID(s, href)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true

		// The title is on the anchor's title attribute and, separately, in the
		// item-title paragraph. Prefer the attribute: the paragraph sometimes
		// carries a "NEW" badge as a sibling text node.
		title := theme.Collapse(a.AttrOr("title", ""))
		if title == "" {
			title = theme.Text(li.Find(".manga-list-1-item-title, .manga-list-2-item-title, .manga-list-4-item-title").First())
		}

		out = append(out, theme.SeriesStub{
			ID:       id,
			Title:    title,
			CoverURL: t.absolute(s, strings.TrimSpace(li.Find("img[src]").First().AttrOr("src", ""))),
		})
	})
	return out, nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	doc, err := t.doc(ctx, s, t.seriesPath(id))
	if err != nil {
		return nil, err
	}

	out := &theme.Series{
		ID:       id,
		Title:    theme.Text(doc.Find(".detail-info-right-title-font").First()),
		CoverURL: t.absolute(s, strings.TrimSpace(doc.Find(".detail-info-cover-img").First().AttrOr("src", ""))),
		Status:   parseStatus(theme.Text(doc.Find(".detail-info-right-title-tip").First())),
	}

	// The summary is truncated in one paragraph and complete in a hidden one
	// beside it. Preferring the hidden one is preferring the whole text.
	if full := theme.Text(doc.Find("p.fullcontent").First()); full != "" {
		out.Description = full
	} else {
		out.Description = theme.Text(doc.Find(".detail-info-right-content").First())
	}

	doc.Find(".detail-info-right-say a").Each(func(_ int, a *goquery.Selection) {
		if v := theme.Text(a); v != "" {
			out.Authors = append(out.Authors, v)
		}
	})
	doc.Find(".detail-info-right-tag-list a").Each(func(_ int, a *goquery.Selection) {
		if v := theme.Text(a); v != "" {
			out.Genres = append(out.Genres, v)
		}
	})

	return out, nil
}

// parseStatus normalises the site's word to PLAN §7.2's vocabulary. Anything
// unrecognised becomes StatusUnknown rather than being passed through.
func parseStatus(s string) string {
	switch l := strings.ToLower(strings.TrimSpace(s)); {
	case strings.Contains(l, "completed"):
		return theme.StatusCompleted
	case strings.Contains(l, "ongoing"):
		return theme.StatusOngoing
	default:
		return theme.StatusUnknown
	}
}

// Chapters implements theme.Theme.
//
// The list is in the series HTML — no AJAX, no second endpoint — and it is
// newest-first.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}
	doc, err := t.doc(ctx, s, t.seriesPath(id))
	if err != nil {
		return nil, err
	}

	pattern := o.String(KeyDateFormat)
	now := t.now()

	var out []theme.Chapter
	seen := make(map[string]bool)
	doc.Find("ul.detail-main-list li a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		cid := t.chapterID(s, href)
		if cid == "" || seen[cid] {
			return
		}
		seen[cid] = true

		title := theme.Text(a.Find(".detail-main-list-main .title3").First())
		if title == "" {
			// The list markup changed. The anchor's title attribute carries
			// the series name as well, which is noisy but not wrong.
			title = theme.Collapse(a.AttrOr("title", ""))
		}

		vol, num := volumeAndNumber(cid)
		if num < 0 {
			// No number in the path either. The rendered title is the last
			// thing left to read it from.
			num = theme.ChapterNumber(title)
		}

		ch := theme.Chapter{ID: cid, Title: title, Volume: vol, Number: num}
		if d := theme.Text(a.Find(".detail-main-list-main .title2").First()); d != "" {
			ch.Published = theme.ParseDate(d, pattern, now)
		}
		out = append(out, ch)
	})

	// PLAN §7.2's ordering contract. The site lists newest first.
	return theme.SortAndMark(out), nil
}

// chapterRefRE pulls the volume and chapter out of a chapter path. Both parts
// are optional: series with no volume structure omit the first, and the
// platform writes an unassigned volume as a word rather than a number.
var chapterRefRE = regexp.MustCompile(`/v([^/]+)/c([0-9]+(?:\.[0-9]+)?)`)

// chapterNumOnlyRE is the same for a series with no volumes at all.
var chapterNumOnlyRE = regexp.MustCompile(`/c([0-9]+(?:\.[0-9]+)?)`)

// volumeAndNumber reads the volume label and chapter number out of a chapter
// path.
//
// The URL is the authority rather than the rendered title, because the title
// is a free-text field an uploader fills in and the path is generated. A
// volume the site has not assigned yet is written as a word, which passes
// through as the label PLAN §7.2 says it is — "a label rather than a number
// because that is what sites give" names this exact case.
func volumeAndNumber(path string) (volume string, number float64) {
	number = -1
	if m := chapterRefRE.FindStringSubmatch(path); m != nil {
		volume = strings.TrimLeft(m[1], "0")
		if volume == "" {
			// "v00" is a real volume label on this site: extras and prologues
			// are filed under it.
			volume = "0"
		}
		if n, err := strconv.ParseFloat(m[2], 64); err == nil {
			number = n
		}
		return volume, number
	}
	if m := chapterNumOnlyRE.FindStringSubmatch(path); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			number = n
		}
	}
	return "", number
}

// licensedRE matches the notice a licensed series' reader serves in place of
// its images.
var licensedRE = regexp.MustCompile(`(?i)licensed and not available`)

// Pages implements theme.Theme.
//
// It changes host and rewrites one path segment: the desktop reader hands out
// one page at a time behind an obfuscated script, and the mobile scroll view
// ships every URL in the markup. The mobile host is a sibling of the
// configured one under the same registrable domain, so PLAN §7.4's boundary
// permits it without an AllowedHosts entry.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	ref := t.readerURL(s, chapterID)
	if ref == "" {
		return nil, fmt.Errorf("%s: source %q: %q is not a chapter id", ID, s.ID, chapterID)
	}

	body, err := t.get(ctx, s, ref)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %s: parse HTML: %w", ID, ref, err)
	}

	var out []string
	doc.Find("#viewer img").Each(func(_ int, img *goquery.Selection) {
		// The real URL is in data-original; src holds a loading animation
		// until the reader scrolls. Reading src returns a chapter of spinners,
		// which is why this does not use theme.ImageURL.
		raw := strings.TrimSpace(img.AttrOr("data-original", ""))
		if raw == "" {
			return
		}
		if abs := t.absolute(s, raw); abs != "" {
			out = append(out, abs)
		}
	})

	if len(out) == 0 {
		// A licensed series has a page, a chapter list and working links, and
		// its reader answers 200 with a notice where the images should be.
		// Saying so is the difference between a user knowing why and a user
		// seeing an empty chapter and assuming Quire is broken.
		if licensedRE.MatchString(body) {
			return nil, fmt.Errorf("%s: %s: the site says this series is licensed and it serves no pages for it", ID, ref)
		}
		return nil, fmt.Errorf("%s: %s: no page images in the reader", ID, ref)
	}
	return out, nil
}

// PageReferer implements theme.PageReferrer (PLAN §7.6).
//
// The image host answers 403 with a challenge interstitial to a request that
// does not name one of the site's pages, and 200 to one that does. The page
// named is **the mobile reader page Pages() fetches** — the same URL, from the
// same call — so the header states a fact rather than the fabrication §7.6
// forbids. An unresolvable chapter ID yields "" and no header.
func (t *Theme) PageReferer(s *theme.Source, chapterID string) string {
	return t.readerURL(s, chapterID)
}
