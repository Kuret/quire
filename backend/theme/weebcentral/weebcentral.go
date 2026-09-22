// Package weebcentral implements an htmx + Alpine reader platform as a Quire
// theme.
//
// # What makes it a theme worth having
//
// It is the first thing in the registry that is neither WordPress nor a JSON
// API. madara and mangathemesia are two skins over the same CMS; mangakakalot
// is a PHP family; mangadex is a documented API. This one is a server-rendered
// *fragment* application: the pages a browser sees are assembled from named
// endpoints that answer HTML snippets, swapped in by htmx. Three consequences
// shape everything below.
//
//  1. **The useful endpoints are the fragments, not the pages.** Search is
//     /search/data, which answers rows and nothing else — no <html>, no
//     <title>. The whole chapter list is /series/{id}/full-chapter-list. The
//     page images are /chapters/{id}/images. Fetching the human-facing page
//     and parsing it would be both heavier and, for chapters, wrong.
//
//  2. **The series page's chapter list is truncated.** It renders the most
//     recent entries and hides the rest behind the fragment endpoint. Reading
//     it gives a plausible short list rather than an error, which is the worst
//     kind of wrong, so Chapters() never touches the series page and a test
//     asserts it.
//
//  3. **Entities are addressed by an opaque 26-character identifier**, with a
//     human-readable slug appended for looks. The slug is not part of the
//     identity and the chapter-list fragment refuses it, so urls.go keeps the
//     two apart.
//
// # Provenance
//
// Written from HTML observed live on 2026-09-16 and from the endpoint shapes
// recorded in PLAN §1.4's prior art, which is read for *facts* — which path
// answers which fragment, where the list lives — and never for expression. No
// selector block, and no line of anyone's code, is transcribed here. The
// fixtures under testdata/ are synthetic pages on example.invalid, per PLAN
// §1.3; nothing in this package or its tests names a real site.
package weebcentral

import (
	"context"
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
const ID = "weebcentral"

// The two path segments the site addresses its entities with. They are
// constants rather than overrides because this theme drives one deployment,
// not a family: there is no second site to rename them, and an override nobody
// can usefully set is a knob that only ever gets typed wrong.
const (
	seriesSegment  = "series"
	chapterSegment = "chapters"

	// searchPath is the fragment endpoint behind the search box. The
	// human-facing /search renders a whole page around the same rows.
	searchPath = "/search/data"

	// chapterListSuffix is appended to /series/{id} — *without* the slug.
	chapterListSuffix = "full-chapter-list"

	// imagesSuffix is appended to /chapters/{id}.
	imagesSuffix = "images"

	// pageSize is what the site's own next-page button asks for, so paging
	// through Search matches what the site considers a page.
	pageSize = 32
)

// Override keys.
const (
	// KeyBrowseSort is the sort order used when the query is empty.
	//
	// PLAN §7.5's M3 correction made "Browse" a search with an empty query,
	// because the Theme interface has no popular/latest method. This site
	// sorts by relevance by default, and relevance against an empty query is
	// meaningless — the browse list comes back in an order with no meaning
	// rather than in a useful one. Naming the sort is the fix, and it is a
	// user preference rather than a constant because "what should Browse show
	// me" genuinely differs between someone catching up and someone looking
	// for something new.
	KeyBrowseSort = "browseSort"

	// KeyIncludeAdultContent controls whether adult-flagged series are
	// included.
	//
	// The site includes them by default. Quire does not, for the same reason
	// mangadex caps its content rating: the device is a shared-surface
	// e-reader whose library is visible from the stock UI, and a browse list
	// is not a place to be surprised. Opt-in, never inferred.
	KeyIncludeAdultContent = "includeAdultContent"
)

// browseSorts is the site's own sort vocabulary, sent verbatim. The values are
// the site's spelling, not ours: an enum that needed translating would be a
// mapping table to keep in step with someone else's UI.
var browseSorts = []string{
	"Popularity",
	"Latest Updates",
	"Recently Added",
	"Alphabet",
	"Best Match",
}

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key:     KeyBrowseSort,
			Kind:    theme.KindString,
			Default: "Popularity",
			Enum:    browseSorts,
			Why: "Browse is a search with an empty query (PLAN §7.5), and the site's " +
				"default sort is relevance, which against an empty query orders nothing. " +
				"This names the order the browse list comes back in.",
		},
		{
			Key:     KeyIncludeAdultContent,
			Kind:    theme.KindBool,
			Default: false,
			Why: "The site includes adult-flagged series in its listings by default. " +
				"Quire asks it not to unless the user says otherwise, because a browse " +
				"list on a device whose library is visible from the stock reader is not " +
				"a place to be surprised.",
		},
	},
}

// Theme is the weebcentral theme.
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
// This theme issues no retrieval-classified request today — every call it
// makes is a listing or a link being followed — so the copy is equivalent. It
// implements the interface anyway, for the reason theme.go gives: a caller
// that needs the guarantee should be able to *require* it, and taking a
// theme's silence for "it never retrieves anything" is how §7.4's narrow
// exception stops being narrow.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// imageHosts are where this site serves comic pages and covers from.
//
// None of them is under the site's own registrable domain, so without this
// declaration PLAN §7.4's redirect boundary refuses every page image and
// §7.5's stage 5 fails the probe outright — the source would be added, listed,
// browsed, and then download nothing. The hosts are named here rather than
// left to the user for the reason §7.2 gives: making somebody discover a CDN's
// name by reading an SSRF rejection is a terrible first run, and a list in code
// is reviewable in a diff.
//
// PLAN §1.3 forbids this repository from naming an aggregator's domain. These
// are the stated exception, and the same one *.mangadex.network was granted:
// the guard cannot work without them, and an image host that serves nothing
// but images is not a browsable site.
//
// Every entry is the wildcard form, which matches subdomains and never the
// bare domain. That is the honest shape here: the apexes serve nothing, and
// the hostnames that do are per-shard labels ("scans", "scans-hot",
// "official") on four sibling domains that appear to exist for no purpose but
// this. Three of the four were observed serving pages on 2026-09-16; the
// fourth is named because the set is plainly one deliberate family and a
// missing member is a download failure the user cannot diagnose — see
// docs/THEME-NOTES.md, which records which is which.
//
// Covers are included on purpose, unlike mangadex's, because here they are
// genuinely off-domain: the search rows and the series page both point their
// thumbnails at a separate host, so leaving it out would mean a browse list
// of empty frames.
var imageHosts = []string{
	"*.lastation.us",
	"*.planeptune.us",
	"*.lowee.us",
	"*.leanbox.us",
	"*.compsci88.com",
}

// AllowedHosts implements theme.Theme.
func (t *Theme) AllowedHosts() []string {
	return append([]string(nil), imageHosts...)
}

// SuggestedName implements theme.Theme.
//
// A real name, unlike madara's and mangathemesia's empty strings, because this
// theme drives one site rather than a family of independently branded ones.
// The page <title> would in fact do here — but only by luck, and PLAN §7.5's
// stage-6 correction is that a theme which knows its site's name should say so
// rather than leave the default to whatever the site puts in a <title> this
// week.
func (t *Theme) SuggestedName() string { return "Weeb Central" }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// disqualifying markers put the score at zero outright.
//
// PLAN §7.5 wants several weak structural signals rather than one brittle
// strong one, and the cost of that is that weak signals accumulate on pages
// they were not meant for. These are the other direction: each is another
// theme's own unmistakable marker, and a page carrying one is that theme's
// page whatever else it happens to look like. The site this theme drives is
// not WordPress and shares no lineage with any of them, so there is no
// legitimate page on which any of these can appear.
var disqualifying = []string{
	"/wp-content/",              // madara, mangathemesia, and WordPress at large
	"ts_reader.run(",            // mangathemesia's reader bootstrap
	"data-chapter-url-template", // mangakakalot's client-rendered chapter list
}

// Fingerprint scores a page for "is this the shape this theme reads?".
//
// The signals are the application's own plumbing rather than its styling,
// which is what PLAN §7.5 means by cheap and structural. Class names are
// useless here — the site is built on a utility-CSS framework, so its classes
// are "flex items-center gap-2", which describes half the web. What is
// distinctive is *where it fetches from*: the named fragment endpoints in
// hx-get attributes, and the two element IDs the fragments are swapped into.
//
// Scores land where §7.5's threshold of 60 expects them, with the home page —
// the page a probe actually gets to see — highest, and each fragment able to
// clear the bar on its own so that a probe landing on one is not left saying
// "unrecognised".
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

	// The reader fragment's container. The single most specific marker here:
	// an element ID this exact application swaps its page images into.
	add(p.Has("#chapter-images"), 45)

	// The type-ahead box, present on every page's header.
	add(p.Has("#quick-search-input, #quick-search-result"), 20)

	// The chapter-list container, and the Alpine expression the rows carry.
	add(p.Has("#chapter-list"), 15)
	add(p.Contains("checkNewChapter("), 20)

	// Named fragment endpoints, in hx-get attributes or in links to them.
	add(p.Contains(searchPath), 25)
	add(p.Contains("display_mode="), 20)
	add(p.Contains("/hot-series"), 15)
	add(p.Contains("/latest-updates/"), 10)
	add(p.Contains("/recently-added/"), 10)
	add(p.Contains("/"+chapterListSuffix), 20)

	// The site's own chapter badge, on every chapter row everywhere.
	add(p.Contains("/static/images/chapter-badge"), 20)
	add(p.Contains("/static/images/broken_image"), 15)

	// htmx itself. Weak on purpose — plenty of sites use it — and here only to
	// separate two candidates that already agree on the rest.
	add(p.Contains("hx-get="), 10)

	// Chapter links. Deliberately not paired with a /series/ signal: series
	// paths are common enough that scoring them would leak onto other themes'
	// pages for nothing, and this one is already carried.
	add(p.Has(`a[href*="/`+chapterSegment+`/"]`), 10)

	if score > 100 {
		score = 100
	}
	return score
}

// Search implements theme.Theme.
//
// Discovery in PLAN §7.4's sense, and it goes to the fragment endpoint rather
// than to the human-facing search page: the rows are identical and the
// fragment is a fraction of the bytes, which on this device is the difference
// that matters.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	q = strings.TrimSpace(q)

	// Relevance ordering is right for a query and meaningless without one.
	sort := "Best Match"
	if q == "" {
		sort = o.String(KeyBrowseSort)
	}

	qs := url.Values{}
	qs.Set("text", q)
	qs.Set("sort", sort)
	qs.Set("order", "Descending")
	if !o.Bool(KeyIncludeAdultContent) {
		qs.Set("adult", "False")
	}
	qs.Set("limit", strconv.Itoa(pageSize))
	qs.Set("offset", strconv.Itoa((page-1)*pageSize))
	// The row layout. The compact one omits the title element entirely, so
	// this is not cosmetic: without it the rows come back unnamed.
	qs.Set("display_mode", "Full Display")

	reqPath := searchPath + "?" + qs.Encode()
	doc, err := t.doc(ctx, s, reqPath)
	if err != nil {
		return nil, err
	}
	// The fragment request just made above — the page these covers are
	// actually parsed from, resolved the same way t.doc resolved it. PLAN
	// §7.6: truthful, per-request, never a constant.
	pageURL, err := s.Resolve(reqPath)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve %s: %w", ID, reqPath, err)
	}

	var out []theme.SeriesStub
	seen := make(map[string]bool)
	doc.Find("article a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		// Chapter rows are mixed in with series rows. A chapter returned as a
		// search result is a row that opens onto nothing.
		id := t.seriesID(s, href)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true

		stub := theme.SeriesStub{
			ID:       id,
			Title:    rowTitle(a),
			CoverURL: t.absolute(s, coverURL(a)),
		}
		if stub.CoverURL != "" {
			stub.CoverReferrer = pageURL
		}
		out = append(out, stub)
	})
	return out, nil
}

// rowTitle reads the title out of a search row.
//
// The title is a bare <div> with no class, the last child of the anchor. Every
// other element in the row carries classes, which is what makes "the one
// without them" a stable rule rather than a positional guess — the site adds
// and removes badge elements around it freely.
func rowTitle(a *goquery.Selection) string {
	if s := theme.Text(a.Find("div:not([class])").Last()); s != "" {
		return s
	}
	// Last resort: the anchor's own text, which includes the badges. Better a
	// slightly noisy title than an unnamed row.
	return theme.Text(a)
}

// coverURL reads a cover out of a <picture>/<img> pair.
//
// The markup offers the same image at several sizes: a <source> per breakpoint
// carrying a srcset, then an <img> as the fallback. The first <source> is the
// full-size rendition and the <img> is a lower-quality format, so preferring
// the source is preferring the better image — which matters, because M4
// resizes for a 1620x2160 screen and cannot add back detail that was never
// fetched.
func coverURL(sel *goquery.Selection) string {
	if ss, ok := sel.Find("source[srcset]").First().Attr("srcset"); ok {
		if u := firstSrcsetCandidate(ss); u != "" {
			return u
		}
	}
	return strings.TrimSpace(sel.Find("img[src]").First().AttrOr("src", ""))
}

// firstSrcsetCandidate takes the first URL of a srcset, dropping its width or
// density descriptor.
func firstSrcsetCandidate(srcset string) string {
	first, _, _ := strings.Cut(srcset, ",")
	fields := strings.Fields(first)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	seriesPath := t.seriesPath(id)
	doc, err := t.doc(ctx, s, seriesPath)
	if err != nil {
		return nil, err
	}

	out := &theme.Series{ID: id, Title: theme.Text(doc.Find("h1").First())}
	out.CoverURL = t.absolute(s, seriesCover(doc))
	if out.CoverURL != "" {
		// The series page just fetched above, the page the cover markup was
		// actually parsed from — not a value assembled by hand.
		out.CoverReferrer = t.absolute(s, seriesPath)
	}

	// The metadata is a flat list of rows, each a <strong> label followed by
	// the value. Matching on the label text rather than on row position is
	// what lets the site add a row — it has several times — without silently
	// shifting every field by one.
	doc.Find("li").Each(func(_ int, li *goquery.Selection) {
		label := li.Children().Filter("strong").First()
		if label.Length() == 0 {
			// A nested row, e.g. one of the associated names. Its parent
			// handles it.
			return
		}
		name := strings.ToLower(strings.Trim(theme.Text(label), ": "))

		switch {
		case strings.Contains(name, "associated name"):
			li.Find("li").Each(func(_ int, n *goquery.Selection) {
				if v := theme.Text(n); v != "" {
					out.AltTitles = append(out.AltTitles, v)
				}
			})
		case strings.Contains(name, "description"):
			out.Description = theme.Text(li.Find("p").First())
		case strings.Contains(name, "author"):
			li.Find("a").Each(func(_ int, a *goquery.Selection) {
				if v := theme.Text(a); v != "" {
					out.Authors = append(out.Authors, v)
				}
			})
		case strings.Contains(name, "tag"):
			li.Find("a").Each(func(_ int, a *goquery.Selection) {
				if v := theme.Text(a); v != "" {
					out.Genres = append(out.Genres, v)
				}
			})
		case strings.Contains(name, "status"):
			out.Status = parseStatus(theme.Text(li.Find("a").First()))
		}
	})

	return out, nil
}

// seriesCover finds the cover on a series page.
//
// It takes the first <picture> whose image is not a site asset. The site's own
// chrome — the brand mark, the chapter badges — is served from /static/, and
// skipping that is more durable than counting elements or leaning on a
// utility-class selector that changes with the layout.
func seriesCover(doc *goquery.Document) string {
	var found string
	doc.Find("picture").EachWithBreak(func(_ int, p *goquery.Selection) bool {
		u := coverURL(p)
		if u == "" || strings.Contains(u, "/static/") {
			return true
		}
		found = u
		return false
	})
	if found != "" {
		return found
	}
	// No <picture> at all: some rows ship a plain <img>.
	return strings.TrimSpace(doc.Find(`img[alt$="cover"]`).First().AttrOr("src", ""))
}

// parseStatus normalises the site's status vocabulary to Quire's.
//
// PLAN §7.2's Series.Status: a value we do not recognise becomes
// StatusUnknown rather than being passed through, so the UI never has to
// render a site's own words.
func parseStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "complete", "completed":
		return theme.StatusCompleted
	case "ongoing":
		return theme.StatusOngoing
	case "hiatus":
		return theme.StatusHiatus
	case "canceled", "cancelled":
		return theme.StatusCancelled
	default:
		return theme.StatusUnknown
	}
}

// Chapters implements theme.Theme.
//
// It goes to the fragment endpoint and never to the series page. The series
// page carries a chapter list too — a *truncated* one, the most recent entries
// only — and reading it would return a short list rather than an error. A
// user would see the newest chapters, download them, and never learn that the
// back catalogue existed. TestChaptersNeverReadsTheSeriesPage is the guard.
//
// Note the path: /series/{id}/full-chapter-list takes the bare identifier, not
// the /series/{id}/{slug} form the rest of the theme passes around.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	token := t.seriesToken(id)
	if token == "" {
		return nil, fmt.Errorf("%s: source %q: %q is not a series id", ID, s.ID, id)
	}

	doc, err := t.doc(ctx, s, "/"+seriesSegment+"/"+token+"/"+chapterListSuffix)
	if err != nil {
		return nil, err
	}

	var out []theme.Chapter
	seen := make(map[string]bool)
	doc.Find("div[x-data] > a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		cid := t.chapterID(s, href)
		if cid == "" || seen[cid] {
			return
		}
		seen[cid] = true

		title := chapterTitle(a)
		ch := theme.Chapter{
			ID:     cid,
			Title:  title,
			Number: theme.ChapterNumber(title),
		}

		// The date is a machine-readable attribute rather than a rendered
		// string, so there is no dateFormat override here and no locale to get
		// wrong. A row without one yields the zero time, which PLAN §7.2 says
		// is fine: a chapter with no date is still a chapter worth reading.
		if dt, ok := a.Find("time[datetime]").First().Attr("datetime"); ok {
			if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(dt)); err == nil {
				ch.Published = ts
			}
		}

		// The badge distinguishes an official release from a scanlation. It is
		// the only attribution the list carries, and it is worth keeping: a
		// volume assembled from official pages reads differently from one
		// assembled from a fan translation, and the user should be able to see
		// which they are queuing.
		a.Find("img[src]").EachWithBreak(func(_ int, img *goquery.Selection) bool {
			if strings.Contains(strings.ToLower(img.AttrOr("src", "")), "official") {
				ch.Scanlator = "Official"
				return false
			}
			return true
		})

		out = append(out, ch)
	})

	// PLAN §7.2's ordering contract. The site lists newest first, so this
	// reverses the list rather than sorting it — which is the point of
	// order.go's rule: the unnumbered entries ("Special Chapter — ...") have
	// no position a sort could give them, and reversing keeps each one where
	// the site put it, between its neighbours.
	return theme.SortAndMark(out), nil
}

// chapterTitle reads a chapter's name out of its row.
//
// The name is the first <span> inside the row's growing span. Taking the
// anchor's whole text instead would append the timestamp and, on a chapter the
// reader has opened before, the words "Last Read" — both of which would end up
// in a PDF's title.
func chapterTitle(a *goquery.Selection) string {
	if s := theme.Text(a.Find("span.grow > span").First()); s != "" {
		return s
	}
	if s := theme.Text(a.Find("span.flex > span").First()); s != "" {
		return s
	}
	// The row markup changed under us. A noisy title beats no chapter.
	return theme.Text(a.Clone().Find("time").Remove().End())
}

// Pages implements theme.Theme.
//
// The simplest Pages() in the registry: one fragment, every image eagerly in
// the markup, real URLs in src. There is no inline JSON blob to unpick as in
// mangathemesia, no separate API call as in mangakakalot, and no lazy-load
// attribute holding the real URL while src holds a placeholder.
//
// Because of that last point this reads src and *only* src, rather than
// theme.ImageURL. Every <img> here carries an onerror handler that assigns a
// same-site placeholder, and a helper looking for "any image URL on this node"
// is exactly the kind of thing that would find it. A chapter of broken-image
// icons is a silent failure; a missing page is a loud one.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	path := t.chapterPath(chapterID)
	qs := url.Values{}
	// The vertical layout, which is the one that ships the whole chapter in a
	// single fragment. The paged layouts return one image at a time.
	qs.Set("is_prev", "False")
	qs.Set("reading_style", "long_strip")

	doc, err := t.doc(ctx, s, path+"/"+imagesSuffix+"?"+qs.Encode())
	if err != nil {
		return nil, err
	}

	sel := doc.Find("#chapter-images img[src]")
	if sel.Length() == 0 {
		// The container's ID changed. Fall back to the images in the fragment
		// at large rather than returning an empty chapter, which downstream
		// reads as "this chapter has no pages" and not as "something broke".
		sel = doc.Find("section img[src]")
	}

	var out []string
	sel.Each(func(_ int, img *goquery.Selection) {
		src := strings.TrimSpace(img.AttrOr("src", ""))
		if src == "" || strings.Contains(src, "/static/") {
			return
		}
		if abs := t.absolute(s, src); abs != "" {
			out = append(out, abs)
		}
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("%s: %s: no page images in the reader fragment", ID, path)
	}
	return out, nil
}

// absolute resolves a possibly-relative URL against the source base, returning
// "" for anything unparseable.
func (t *Theme) absolute(s *theme.Source, ref string) string {
	if ref == "" {
		return ""
	}
	abs, err := s.Resolve(ref)
	if err != nil {
		return ""
	}
	return abs
}
