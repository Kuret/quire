// Package mangathemesia implements the WordPress theme PLAN §7.3 names as the
// second tier-1 family — formerly WPMangaStream, and the "structurally very
// different" counterpart M2's acceptance test asks for.
//
// It differs from madara in every way that matters to this engine:
//
//   - The chapter list is *in the series HTML*, in a #chapterlist, rather than
//     behind an AJAX POST. There is no second round trip and no post ID.
//   - The page list is *not in the DOM at all*. PLAN §7.3: "Page list
//     typically embedded in an inline ts_reader.run({...}) JSON blob". Parsing
//     the reader's <img> tags gets you a partial, lazily-hydrated list; the
//     JSON blob is the authoritative one.
//   - Series metadata sits in a flat .tsinfo list of .imptdt rows rather than
//     in heading/content pairs.
//
// That difference is the point: two themes that parsed the same way would not
// have proven the interface generalises.
//
// What this package encodes is the shape of the markup a distributed WordPress
// theme generates, which is an observable fact about that software. PLAN §1.4
// governs — nothing here is transcribed from another project.
package mangathemesia

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
const ID = "mangathemesia"

// Override keys.
const (
	// KeySeriesSubPath is the path segment series live under. The theme ships
	// "manga"; sites rename it as freely as the madara family does. It is a
	// separate key from madara's mangaSubPath rather than a shared one because
	// the two themes are independently configurable and sharing a name across
	// themes would invite pasting one theme's overrides into the other.
	KeySeriesSubPath = "seriesSubPath"

	// KeyDateFormat is the site's chapter date format, same spelling as
	// elsewhere.
	KeyDateFormat = "dateFormat"

	// KeyPageSource selects where Pages() reads from. "ts_reader" is the
	// authoritative inline JSON; "dom" is the fallback for the minority of
	// sites that render the reader server-side and ship no blob.
	KeyPageSource = "pageSource"
)

// Values for KeyPageSource.
const (
	PageSourceTSReader = "ts_reader"
	PageSourceDOM      = "dom"
)

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key: KeySeriesSubPath, Kind: theme.KindString, Default: "manga",
			Why: "series live under /{seriesSubPath}/{slug}/; sites rename the segment (manga, series, komik, ...)",
		},
		{
			Key: KeyDateFormat, Kind: theme.KindString, Default: "MMMM d, yyyy",
			Why: "chapter dates follow the site's WordPress date setting",
		},
		{
			Key: KeyPageSource, Kind: theme.KindString, Default: PageSourceTSReader,
			Enum: []string{PageSourceTSReader, PageSourceDOM},
			Why:  "the page list is normally an inline ts_reader.run({...}) blob, but a minority of sites render the reader server-side instead",
		},
	},
}

// Theme is the mangathemesia theme.
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
// watched-series check of PLAN §12.2. mangathemesia issues no retrieval-
// classified request today; see madara's for why it implements this regardless.
func (t *Theme) DiscoveryOnly() theme.Theme {
	c := *t
	c.f = theme.DiscoveryFetcher(t.f)
	return &c
}

// SuggestedName implements theme.Theme.
//
// Empty, for the same reason as madara: a distributed WordPress theme has no
// name of its own to lend the sites running it.
func (t *Theme) SuggestedName() string { return "" }

// AllowedHosts implements theme.Theme.
//
// Nil, for the same reason as madara: independently hosted WordPress sites
// with no shared CDN. Checked against our fixtures — the images in
// testdata/reader.html and the ts_reader blob are all on the site's own host,
// and no fixture redirects off-domain.
func (t *Theme) AllowedHosts() []string { return nil }

// ID implements theme.Theme.
func (t *Theme) ID() string { return ID }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// Fingerprint scores a page for "is this MangaThemesia?".
//
// `ts_reader.run(` is the near-conclusive signal: it is this theme's own
// reader bootstrap and nothing else emits it. The rest are the theme's
// characteristic class names, which are distinctive as a set but individually
// short enough that none of them alone should decide anything — which is what
// keeps a madara page, also WordPress, from scoring here.
func (t *Theme) Fingerprint(p *probe.Page) int {
	score := 0
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	add(p.Contains("ts_reader.run("), 45)
	add(p.Has("#readerarea"), 20)
	add(p.Has("#chapterlist"), 20)
	add(p.Has(".seriestucon, .seriestuheader"), 15)
	add(p.Has(".tsinfo .imptdt"), 15)
	add(p.Has(".eplister, .epcur"), 10)

	// Listing markup. A search or archive page carries none of the signals
	// above — there is no reader and no series info block — so these have to
	// carry it on their own. They are weighted to just reach the probe's
	// confidence threshold together, and no single one of them is decisive.
	add(p.Has(".listupd .bsx"), 20)
	// Measured on a live home page 2026-09-16, after madara's fingerprint was
	// found to score 43 on a real site of its own family. This one held up —
	// a real home page scored 65 against a threshold of 60 — but five points
	// is one skin variation away from failing, so two more signals that were
	// really there were added. Both are this theme's own card internals, and
	// neither appears on a page of any other theme we ship.
	add(p.Has(".listupd .bs .bsx .bigor, .bsx .bigor"), 10)
	add(p.Has(".bsx .imgseries, .bsx .ply, .bsx .rt .rating"), 10)
	add(p.Has(".listupd .bsx .bigor .tt, .listupd .bsx .tt"), 15)
	add(p.Has(".listupd .utao .uta .imgu"), 15)
	add(p.Has(".bsx .limit .type, .bsx .limit"), 10)
	add(p.Has(".adds .epxs"), 10)
	add(p.Has(".bixbox"), 10)
	add(p.Has(".mgen"), 5)

	// A negative signal, and the reason the near-miss test passes: a page that
	// carries the madara plugin's own asset path is not this theme, however
	// many generic WordPress class names it happens to share. Without this a
	// heavily-skinned madara site could creep into double figures here.
	if p.Contains("/wp-content/plugins/madara/") {
		score -= 30
	}
	return score
}

// Search implements theme.Theme. The theme uses WordPress's stock search with
// no post_type filter, so results include pages and posts; requiring the
// series path segment is what keeps those out.
func (t *Theme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	path := "/"
	if page > 1 {
		path = fmt.Sprintf("/page/%d/", page)
	}
	qs := url.Values{}
	qs.Set("s", q)

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

	var out []theme.SeriesStub
	doc.Find(".listupd .bs .bsx > a, .listupd .utao .uta .imgu > a").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		id := t.seriesID(s, href)
		if id == "" {
			return
		}
		// The title is in the anchor's title attribute and, redundantly, in a
		// .tt element. Prefer the attribute: the .tt often carries a "HOT" or
		// "NEW" badge as a sibling text node.
		title := strings.TrimSpace(a.AttrOr("title", ""))
		if title == "" {
			title = theme.Text(a.Find(".tt, .luf h4, h4").First())
		}
		stub := theme.SeriesStub{
			ID:       id,
			Title:    theme.Collapse(title),
			CoverURL: theme.ImageURL(a.Find("img").First()),
		}
		if stub.CoverURL != "" {
			stub.CoverReferrer = pageURL
		}
		out = append(out, stub)
	})
	return out, nil
}

// Series implements theme.Theme.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	seriesPath := t.seriesPath(s, id)
	doc, err := t.doc(ctx, s, seriesPath)
	if err != nil {
		return nil, err
	}

	out := &theme.Series{ID: id}
	out.Title = theme.Text(doc.Find(".seriestuheader h1.entry-title, h1.entry-title").First())
	out.CoverURL = theme.ImageURL(doc.Find(".thumb img, .seriestucontl .thumb img").First())
	if out.CoverURL != "" {
		// The series page just fetched above, the page the cover markup was
		// actually parsed from — not a value assembled by hand.
		if pageURL, err := s.Resolve(seriesPath); err == nil {
			out.CoverReferrer = pageURL
		}
	}
	out.Description = theme.Text(doc.Find(".entry-content[itemprop=description], [itemprop=description], .seriestuhead .entry-content").First())

	doc.Find(".mgen a, .seriestugenre a").Each(func(_ int, a *goquery.Selection) {
		if g := theme.Text(a); g != "" {
			out.Genres = append(out.Genres, g)
		}
	})

	if alt := theme.Text(doc.Find(".seriestualt").First()); alt != "" {
		for _, a := range strings.Split(alt, ",") {
			if a = strings.TrimSpace(a); a != "" {
				out.AltTitles = append(out.AltTitles, a)
			}
		}
	}

	// The info block is a flat list of rows, each a label in <i> or <h2>
	// followed by the value. Matching on the label text rather than on row
	// position is what lets a translated site still work.
	doc.Find(".tsinfo .imptdt, .infotable tr").Each(func(_ int, row *goquery.Selection) {
		label := strings.ToLower(theme.Text(row.Find("i, h2, td:first-child").First()))
		// Remove *only* the label node. A Status row is two sibling <i>s, so
		// removing every <i> would take the value away with the label.
		value := row.Clone()
		value.Find("i, h2, td:first-child").First().Remove()
		text := theme.Text(value)
		switch {
		case strings.Contains(label, "status"):
			out.Status = normaliseStatus(text)
		case strings.Contains(label, "author"):
			out.Authors = appendNames(out.Authors, text)
		case strings.Contains(label, "artist"):
			out.Artists = appendNames(out.Artists, text)
		}
	})
	return out, nil
}

// Chapters implements theme.Theme. One GET, no AJAX: the list is in the series
// HTML. This is the structural difference from madara that M2 wanted.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	doc, err := t.doc(ctx, s, t.seriesPath(s, id))
	if err != nil {
		return nil, err
	}

	format := o.String(KeyDateFormat)
	now := t.now()

	var out []theme.Chapter
	doc.Find("#chapterlist li, .eplister li").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		rel := t.relativeID(s, href)
		if rel == "" {
			return
		}
		title := theme.Text(a.Find(".chapternum").First())
		if title == "" {
			title = theme.Text(a)
		}
		ch := theme.Chapter{ID: rel, Title: title, Number: theme.ChapterNumber(title)}
		// The theme also stashes the number in data-num, which is more
		// reliable than the display title when the title is decorative.
		if num, ok := li.Attr("data-num"); ok {
			if n := theme.ChapterNumber(num); n >= 0 {
				ch.Number = n
			}
		}
		ch.Published = theme.ParseDate(theme.Text(a.Find(".chapterdate").First()), format, now)
		out = append(out, ch)
	})
	// PLAN §7.2: ascending reading order. #chapterlist is rendered newest
	// first, exactly like madara's; testdata/series.html keeps that order so
	// the test has something real to correct.
	return theme.SortAndMark(out), nil
}

// tsReaderCallRE finds the start of the reader's bootstrap call. It only
// locates the opening brace; the argument itself is extracted by balancing
// braces, because the blob nests objects inside its "sources" array and any
// regex that tried to match the whole literal would either stop at the first
// inner '}' or run past the call entirely.
//
// A full JS parser would be the wrong tool here: we need one object literal
// out of one known call, not a language.
var tsReaderCallRE = regexp.MustCompile(`ts_reader\s*\.\s*run\s*\(\s*\{`)

// balancedObject returns the JSON object literal starting at the '{' at index
// start, or "" if it is unterminated. String contents are skipped so a brace
// inside a URL or a title cannot unbalance the count.
func balancedObject(s string, start int) string {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// tsReader is the subset of the blob we need. The blob also carries prev/next
// links, a reading direction and the site's own UI preferences; none of that
// is ours to care about.
type tsReader struct {
	Sources []struct {
		Source string   `json:"source"`
		Images []string `json:"images"`
	} `json:"sources"`
}

// Pages implements theme.Theme.
//
// The inline blob is authoritative. The DOM fallback exists because a minority
// of sites render the reader server-side, and because a blob we cannot parse
// should degrade to a partial answer rather than to no answer at all — but a
// site configured for "dom" skips the blob entirely.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	body, doc, err := t.page(ctx, s, chapterID)
	if err != nil {
		return nil, err
	}

	if o.String(KeyPageSource) == PageSourceTSReader {
		if urls := parseTSReader(body); len(urls) > 0 {
			return t.absolutise(s, urls), nil
		}
	}
	return t.absolutise(s, domPages(doc)), nil
}

// parseTSReader extracts the image list from the inline blob. Several sources
// may be present (mirrors); the first is the site's own and is the one the
// reader itself defaults to.
func parseTSReader(body string) []string {
	// Try every occurrence rather than only the first. The call name shows up
	// in HTML comments and in the theme's own documentation blocks, so the
	// first textual match is not reliably the real one; the first that yields
	// images is.
	for _, loc := range tsReaderCallRE.FindAllStringIndex(body, -1) {
		blob := balancedObject(body, loc[1]-1) // the match ends on the '{'
		if blob == "" {
			continue
		}
		var r tsReader
		if err := json.Unmarshal([]byte(blob), &r); err != nil {
			continue
		}
		for _, src := range r.Sources {
			if len(src.Images) > 0 {
				return src.Images
			}
		}
	}
	return nil
}

// domPages is the fallback: the reader's own <img> tags.
func domPages(doc *goquery.Document) []string {
	var out []string
	doc.Find("#readerarea img, .reader-area img").Each(func(_ int, img *goquery.Selection) {
		if u := theme.ImageURL(img); u != "" {
			out = append(out, u)
		}
	})
	return out
}

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

// appendNames splits a comma-separated value, dropping the placeholders the
// theme's admin UI leaves behind when a field was never filled in.
func appendNames(dst []string, text string) []string {
	for _, n := range strings.Split(text, ",") {
		n = strings.TrimSpace(n)
		if n == "" || strings.EqualFold(n, "-") || strings.EqualFold(n, "updating") || strings.EqualFold(n, "n/a") {
			continue
		}
		dst = append(dst, n)
	}
	return dst
}

// normaliseStatus maps the site's own status words onto our small enum.
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
