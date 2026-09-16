// Package comick implements a JSON-API aggregator as a Quire theme.
//
// # What makes it different from everything else here
//
// It is the second JSON API in the registry after mangadex, and it is a
// different animal from that one too. mangadex is a documented, versioned,
// public API with published rate limits. This is an application's *own*
// backend: the endpoints exist to serve the site's frontend, they are
// undocumented, and two of the four things this theme needs are not endpoints
// at all but JSON embedded in an HTML page.
//
//	/api/search                        → JSON
//	/api/comics/{slug}/chapter-list    → JSON
//	/comic/{slug}                      → HTML with a #comic-data JSON blob
//	/comic/{slug}/{chapter}            → HTML with a #sv-data JSON blob
//
// The blobs are not a fallback or a scrape of rendered markup — they are the
// data the page is built from, served as JSON in a script element of declared
// type. Parsing them is reading the site's own payload, and it is considerably
// more stable than reading what its JavaScript does with it afterwards.
//
// # Mirrors, and why this theme names none of them
//
// The software runs on more than one hostname and **the mirrors do not behave
// alike**: on 2026-09-16 one of them answered a browser challenge on the same
// API path another answered 200 JSON on. PLAN §7.6 is clear about what a
// challenge means, and §7.5 stage 3 refuses such a source outright.
//
// This theme therefore has no opinion about which hostname to use, because
// PLAN §1.3 is the answer: **Quire ships no source URLs.** The user supplies
// the host, the probe tells them whether that host works, and a challenged one
// is refused with a verdict that says so. Recommending a mirror would be
// shipping a source list one entry at a time, and it would age badly — the
// challenged and unchallenged hosts can trade places without notice.
//
// # Provenance
//
// Written from JSON and HTML observed live on 2026-09-16, and from endpoint
// shapes read as *facts* from the prior art PLAN §1.4 permits reading. No
// selector block and no line of anyone's code is transcribed. Fixtures are
// synthetic responses on example.invalid (PLAN §1.3).
package comick

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
const ID = "comick"

const (
	// comicSegment is the path segment series and chapters both live under.
	comicSegment = "comic"

	// searchPath is the search API.
	searchPath = "/api/search"

	// comicDataID is the script element holding a series page's own payload.
	comicDataID = "comic-data"

	// readerDataID is the same for a chapter page.
	readerDataID = "sv-data"
)

// Override keys.
const (
	// KeyMaxContentRating names the highest rating to include in search
	// results.
	//
	// **It is applied to the response, not sent as a query parameter.** The
	// API carries a rating on every result, so filtering here is exact; the
	// endpoint also accepts a parameter of that name, but its semantics are
	// undocumented — maximum, exact match or repeated key — and guessing wrong
	// would silently drop results a user was looking for. Filtering what came
	// back cannot do that.
	KeyMaxContentRating = "maxContentRating"

	// KeyIncludeAllLanguages lists chapters in every language rather than the
	// source's own.
	//
	// Off by default because a multilingual list has duplicate chapter numbers
	// in it, which PLAN §7.2's ordering contract has nothing sensible to do
	// with: two chapter 5s cannot both come fifth. On for someone who reads
	// two languages and would rather sort it out themselves.
	KeyIncludeAllLanguages = "includeAllLanguages"
)

// contentRatings is the API's rating scale, weakest first. The override names
// a maximum and this slice turns it into the set to keep.
var contentRatings = []string{"safe", "suggestive", "erotica", "pornographic"}

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key:     KeyMaxContentRating,
			Kind:    theme.KindString,
			Default: "suggestive",
			Enum:    contentRatings,
			Why: "Every search result carries a content rating. This names the highest to " +
				"include, and is applied to the response rather than sent as a query " +
				"parameter, because the endpoint's own filter semantics are undocumented " +
				"and guessing at them would drop results silently.",
		},
		{
			Key:     KeyIncludeAllLanguages,
			Kind:    theme.KindBool,
			Default: false,
			Why: "The chapter list is filtered to the source's language by default. " +
				"Listing every language produces duplicate chapter numbers, which the " +
				"ordering contract cannot resolve — two chapter 5s cannot both come fifth.",
		},
	},
}

// Theme is the comick theme.
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

// DiscoveryOnly implements theme.DiscoveryClassifier (PLAN §12.2).
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// imageHosts is where this software serves page images and covers from.
//
// A different registrable domain from the site, so without the declaration
// PLAN §7.4's boundary refuses every page and §7.5 stage 5 fails the probe.
//
// One entry, in the wildcard form, and both halves of that are deliberate. The
// payload names the shard in a `cdn_id` field and the hostnames are numbered
// labels on that one domain, so a wildcard is the shape of the fact and the
// apex serves nothing. Naming a *second* domain on the strength of hostnames
// this software used to use would be widening the boundary on a guess, and the
// cost of being wrong in that direction is worse than a download that fails
// with a host named in the error.
//
// PLAN §1.3's exception for image hosts is what permits naming it at all.
var imageHosts = []string{"*.comicknew.pictures"}

// AllowedHosts implements theme.Theme.
func (t *Theme) AllowedHosts() []string {
	return append([]string(nil), imageHosts...)
}

// SuggestedName implements theme.Theme.
//
// One piece of software, and the name is the same whichever host a user points
// at — which is what makes a suggestion right here even though the theme
// deliberately names no host. Without it a new source would be called whatever
// the frontend's <title> says this week.
func (t *Theme) SuggestedName() string { return "Comick" }

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

var disqualifyingSelectors = []string{
	"#_imageList",          // webtoons' viewer
	"#chapter-images",      // weebcentral's reader
	"#viewer .reader-page", // fanfox's mobile reader
}

// Fingerprint scores a page for "is this the shape this theme reads?".
//
// The signals are the two embedded payloads and the API's own envelope, which
// is as structural as PLAN §7.5 asks for: they are element IDs and JSON keys,
// not class names that a reskin would change.
//
// There is deliberately **no host signal**, unlike mangadex's — which gates on
// its one hostname and scores 0 for everything else. That works there because
// MangaDex is one site. This software runs on several hostnames that come and
// go, so a host gate would mean a theme that stops recognising its own site
// the week it moves.
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

	// The two embedded payloads. Each is one element ID, and between them they
	// cover the series page and the reader.
	add(p.Has("#"+comicDataID), 45)
	add(p.Has("#"+readerDataID), 45)

	// The search API's envelope, for a probe that lands on it.
	add(p.Contains(`"next_cursor"`) && p.Contains(`"data"`), 35)

	// The site's own link shapes and API paths, in markup or in payload.
	add(p.Contains("/api/comics/"), 25)
	add(p.Contains("/chapter-list"), 20)
	add(p.Contains(searchPath), 15)
	add(p.Contains("/"+comicSegment+"/"), 10)

	// Identifier vocabulary the payloads share. Weak individually — plenty of
	// JSON has a "slug" — and only useful alongside the rest.
	add(p.Contains(`"hid"`), 15)
	add(p.Contains(`"chap"`), 10)

	if score > 100 {
		score = 100
	}
	return score
}

// searchResponse is the search API's envelope.
type searchResponse struct {
	Data []searchEntry `json:"data"`
}

type searchEntry struct {
	Slug             string `json:"slug"`
	Title            string `json:"title"`
	ContentRating    string `json:"content_rating"`
	DefaultThumbnail string `json:"default_thumbnail"`
}

// Search implements theme.Theme.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}

	qs := url.Values{}
	if q = strings.TrimSpace(q); q != "" {
		qs.Set("q", q)
	}
	// Comics only. Without it the endpoint also answers with people and
	// groups, which are not series and have no chapters.
	qs.Set("type", "comic")
	if page > 1 {
		qs.Set("page", strconv.Itoa(page))
	}

	raw, err := t.get(ctx, s, searchPath+"?"+qs.Encode())
	if err != nil {
		return nil, err
	}
	var res searchResponse
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("%s: search: parse JSON: %w", ID, err)
	}

	allowed := ratingsUpTo(o.String(KeyMaxContentRating))

	out := make([]theme.SeriesStub, 0, len(res.Data))
	for _, e := range res.Data {
		if e.Slug == "" {
			continue
		}
		// The filter is applied here rather than asked for, because the
		// endpoint's own parameter is undocumented and this one cannot be
		// wrong: the rating is on the record in front of us. An entry with no
		// rating at all is kept — refusing to show something because the site
		// forgot to classify it is the wrong way round.
		if e.ContentRating != "" && !allowed[e.ContentRating] {
			continue
		}
		out = append(out, theme.SeriesStub{
			ID:       t.seriesID(e.Slug),
			Title:    theme.Collapse(e.Title),
			CoverURL: t.absolute(s, e.DefaultThumbnail),
		})
	}
	return out, nil
}

// ratingsUpTo turns a maximum rating into the set at or below it.
func ratingsUpTo(max string) map[string]bool {
	out := make(map[string]bool, len(contentRatings))
	for _, r := range contentRatings {
		out[r] = true
		if r == max {
			break
		}
	}
	return out
}

// comicData is the series page's embedded payload. Only the fields this theme
// uses are named; the rest of it is the frontend's business.
type comicData struct {
	Title            string     `json:"title"`
	Slug             string     `json:"slug"`
	Desc             string     `json:"desc"`
	Status           int        `json:"status"`
	DefaultThumbnail string     `json:"default_thumbnail"`
	Authors          []namedRef `json:"authors"`
	Artists          []namedRef `json:"artists"`
	Titles           []altTitle `json:"md_titles"`
	Genres           []genreRef `json:"md_comic_md_genres"`
}

type namedRef struct {
	Name string `json:"name"`
}

type altTitle struct {
	Title string `json:"title"`
}

type genreRef struct {
	Genre struct {
		Name string `json:"name"`
	} `json:"md_genres"`
}

// Series implements theme.Theme.
//
// It reads the page's own JSON payload rather than its rendered markup. The
// payload is what the frontend builds the page from, so it is both complete —
// the rendered page truncates the description and hides the alternate titles
// behind a control — and stable in a way a utility-class selector is not.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	body, err := t.get(ctx, s, t.seriesPath(id))
	if err != nil {
		return nil, err
	}
	raw, err := embeddedJSON(body, comicDataID)
	if err != nil {
		return nil, fmt.Errorf("%s: series %s: %w", ID, id, err)
	}
	var d comicData
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, fmt.Errorf("%s: series %s: parse payload: %w", ID, id, err)
	}

	out := &theme.Series{
		ID:          id,
		Title:       theme.Collapse(d.Title),
		Description: theme.Collapse(d.Desc),
		CoverURL:    t.absolute(s, d.DefaultThumbnail),
		Status:      parseStatus(d.Status),
	}
	for _, a := range d.Authors {
		if a.Name != "" {
			out.Authors = append(out.Authors, a.Name)
		}
	}
	for _, a := range d.Artists {
		if a.Name != "" {
			out.Artists = append(out.Artists, a.Name)
		}
	}
	for _, a := range d.Titles {
		if a.Title != "" {
			out.AltTitles = append(out.AltTitles, theme.Collapse(a.Title))
		}
	}
	for _, g := range d.Genres {
		if g.Genre.Name != "" {
			out.Genres = append(out.Genres, g.Genre.Name)
		}
	}
	return out, nil
}

// parseStatus maps the payload's status code to PLAN §7.2's vocabulary.
//
// 1, 2 and 3 were observed directly on 2026-09-16 across a search of forty
// series. 4 is inferred from the gap and from the vocabulary every site of
// this kind uses; docs/THEME-NOTES.md records which is which. Anything else
// becomes StatusUnknown rather than being invented.
func parseStatus(code int) string {
	switch code {
	case 1:
		return theme.StatusOngoing
	case 2:
		return theme.StatusCompleted
	case 3:
		return theme.StatusCancelled
	case 4:
		return theme.StatusHiatus
	default:
		return theme.StatusUnknown
	}
}

// chapterListResponse is the chapter-list API's envelope.
type chapterListResponse struct {
	Data []chapterEntry `json:"data"`
}

type chapterEntry struct {
	HID       string   `json:"hid"`
	Chap      string   `json:"chap"`
	Title     string   `json:"title"`
	Vol       string   `json:"vol"`
	Lang      string   `json:"lang"`
	GroupName []string `json:"group_name"`
	CreatedAt string   `json:"created_at"`
}

// Chapters implements theme.Theme.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%s: source %q: %w", ID, s.ID, err)
	}
	slug := t.seriesSlug(id)
	if slug == "" {
		return nil, fmt.Errorf("%s: source %q: %q is not a series id", ID, s.ID, id)
	}

	path := "/api/comics/" + url.PathEscape(slug) + "/chapter-list"
	lang := strings.TrimSpace(s.Lang)
	if !o.Bool(KeyIncludeAllLanguages) && lang != "" {
		qs := url.Values{}
		qs.Set("lang", lang)
		path += "?" + qs.Encode()
	}

	raw, err := t.get(ctx, s, path)
	if err != nil {
		return nil, err
	}
	var res chapterListResponse
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("%s: chapters for %s: parse JSON: %w", ID, id, err)
	}

	out := make([]theme.Chapter, 0, len(res.Data))
	for _, e := range res.Data {
		if e.HID == "" {
			continue
		}
		ch := theme.Chapter{
			ID:        t.chapterID(slug, e),
			Title:     chapterTitle(e),
			Volume:    strings.TrimSpace(e.Vol),
			Number:    chapterNumber(e.Chap),
			Scanlator: strings.Join(e.GroupName, ", "),
		}
		if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(e.CreatedAt)); err == nil {
			ch.Published = ts
		}
		out = append(out, ch)
	}

	// PLAN §7.2's ordering contract. The API answers newest-first.
	return theme.SortAndMark(out), nil
}

// chapterNumber parses the payload's chapter field, which is a string because
// half-chapters are routine, and which is empty for a one-shot.
func chapterNumber(chap string) float64 {
	chap = strings.TrimSpace(chap)
	if chap == "" {
		return -1
	}
	n, err := strconv.ParseFloat(chap, 64)
	if err != nil {
		return -1
	}
	return n
}

// chapterTitle builds a display name from the payload's two title fields.
//
// The site gives a chapter number and, separately and often emptily, a name.
// Neither alone is enough: a bare number is a poor PDF title and a bare name
// loses the position in the series.
func chapterTitle(e chapterEntry) string {
	chap := strings.TrimSpace(e.Chap)
	name := theme.Collapse(e.Title)
	switch {
	case chap != "" && name != "":
		return "Chapter " + chap + ": " + name
	case chap != "":
		return "Chapter " + chap
	case name != "":
		return name
	default:
		return "Oneshot"
	}
}

// readerData is the chapter page's embedded payload.
type readerData struct {
	Chapter struct {
		Images []readerImage `json:"images"`
	} `json:"chapter"`
}

type readerImage struct {
	URL string `json:"url"`
}

// Pages implements theme.Theme.
//
// The chapter page carries its image list as JSON in a script element, which
// is the data the reader is built from rather than a rendering of it. There is
// no lazy-load attribute to work around and no separate endpoint to call.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	path := t.chapterPath(chapterID)
	body, err := t.get(ctx, s, path)
	if err != nil {
		return nil, err
	}
	raw, err := embeddedJSON(body, readerDataID)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", ID, path, err)
	}
	var d readerData
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, fmt.Errorf("%s: %s: parse payload: %w", ID, path, err)
	}

	out := make([]string, 0, len(d.Chapter.Images))
	for _, img := range d.Chapter.Images {
		if abs := t.absolute(s, img.URL); abs != "" {
			out = append(out, abs)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: %s: the chapter payload names no images", ID, path)
	}
	return out, nil
}

// PageReferer implements theme.PageReferrer (PLAN §7.6).
//
// The image host answers 403 with a challenge interstitial to a request that
// does not name one of the site's pages, and 200 to one that does. The page
// named is **the chapter page Pages() fetches** — the same path, resolved
// against the same base — so the header is a true statement rather than the
// fabrication §7.6 forbids. An unresolvable chapter ID yields "".
//
// Note what this is not: the image host's 403 carries challenge markers, and
// §7.5 stage 3 would call a *challenged* host terminal. It is not being
// bypassed. The host is not asking "are you a browser", it is asking "did this
// come from one of our pages", and the answer Quire gives is the true one.
func (t *Theme) PageReferer(s *theme.Source, chapterID string) string {
	return t.absolute(s, t.chapterPath(chapterID))
}

// embeddedJSON pulls the contents of a script element carrying a JSON payload.
//
// It requires the element to declare itself as JSON. The page has a dozen
// other script elements in it, several of them third-party, and matching on
// the identifier alone would mean handing whichever of them happened to take
// that identifier next to a JSON parser.
func embeddedJSON(body, id string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("parse HTML: %w", err)
	}
	sel := doc.Find(`script#` + id + `[type="application/json"]`)
	if sel.Length() == 0 {
		return "", fmt.Errorf("no %s payload in the page", id)
	}
	raw := strings.TrimSpace(sel.First().Text())
	if raw == "" {
		return "", fmt.Errorf("the %s payload is empty", id)
	}
	return raw, nil
}
