// Package doujinreader implements the shape of a doujinshi/gallery site
// family: several independently branded, independently hosted sites running
// what is structurally the same backend.
//
// # Why this is a family and not three one-offs (PLAN §1.4 provenance)
//
// The candidates were nhentai.xxx, hentaifox.com and asmhentai.com, all
// flagged by a batch probe as "Laravel, CSRF markers, HTTP 200" — which is
// weak evidence on its own, since a shared web framework says nothing about
// the application built on it. Fetching the three live and comparing search,
// gallery and reader pages found a great deal more than a shared framework:
//
//   - Every gallery card, wherever it appears (home, search, a gallery's own
//     "related" strip), is wrapped in an element carrying `data-tags`,
//     `data-artists`, `data-languages` and `data-categories` attributes — the
//     same taxonomy, spelled the same way, on all three.
//   - The taxonomy itself resolves through identically named routes on all
//     three: `/tag/`, `/artist/`, `/language/`, `/category/` (and `/parody/`
//     or `/group/` on the ones that have circles or parodies).
//   - The gallery detail page carries the *same AJAX contract* for "load more
//     thumbnails": hidden inputs literally named `load_id` and `load_dir`,
//     `load_id` being a folder-relative media handle and `load_dir` a
//     zero-padded three-digit folder, both feeding an endpoint that returns
//     more thumbnails. hentaifox even names the endpoint file itself in its
//     bundled JS: `/includes/thumbs_loader.php`.
//   - Every image — cover, thumbnail, full page — sits at
//     `{cdn}/{load_dir}/{load_id}/{name}`, where `{name}` is `cover.jpg`,
//     `{n}t.jpg` for a thumbnail or `{n}.{ext}` for the full page. All three.
//   - The reader page is the same document on all three down to the element
//     IDs: `img#fimg` (two of three; hentaifox's is `img#gimg`, everything
//     else about it matches), a `reader_overlay`/`preloader` wrapper, and a
//     hidden `input[name=pages]` giving the total page count. Even the alt
//     text is the same template: "{title} page {n} full".
//   - All three embed the identical ad-network snippet,
//     `go.hentaigold.net/id/{n}/`, written with the same
//     `document.write("<scr"+"ipt ...")` obfuscation.
//
// None of that is a property of Laravel. It is a property of one specific
// application, sold or copied across operators, which is exactly the "family"
// PLAN §7.3 asks a theme to answer for. What differs between the three is
// cosmetic (class names on the taxonomy widget: `tag_btn`/`t_badge` vs
// `tag_name`/`tag_count` vs `badge`/`gallery_count`) or a route the operator
// renamed — and the renaming is not even consistent *within* a site: hentaifox
// serves its own gallery detail pages at `/gallery/{id}/` but its reader at
// `/g/{id}/{page}/`, asmhentai has exactly the opposite split, and nhentai.xxx
// uses `/g/` for both. A theme that hard-codes one prefix, or guesses at a
// default and asks the user to override it, gets two of the three real sites
// wrong out of the box — measured against live traffic during development.
// Both routes are read from the page instead: see "Why routes are discovered,
// not configured" below.
//
// # Why routes are discovered, not configured
//
// A gallery's own ID is stored as the *relative path it was actually linked
// by* — "/g/700156" or "/gallery/173573", whichever the site used — exactly
// as mangakakalot stores its series IDs, and for the same reason (see
// mangakakalot's package comment): the theme does not have to guess a prefix
// back out of a bare number if it never throws the prefix away. Search finds
// a gallery link by *shape* — a path of exactly two segments whose second is
// purely numeric — rather than by a configured or assumed first segment, so
// it works whether a mirror calls it "g", "gallery" or something no site
// observed here happens to use.
//
// The reader path needs the same treatment but cannot reuse the gallery
// path's own prefix (see hentaifox and asmhentai above), so Pages reads it
// off the one thing that reliably names it: the gallery detail page's own
// thumbnail grid, every entry of which links straight to that page's reader
// (`/{readerSegment}/{id}/{n}/`, confirmed on all three sites). KeyReaderPath
// exists only as the fallback for the case that grid cannot be found at all —
// a defensive default, not the theme's normal path.
//
// # Why one chapter per gallery
//
// This family ships galleries, not serials: one work is a fixed set of N
// images with no chapter list anywhere in it — there is nothing here
// analogous to madara's chapter list or mangakakalot's chapter API, because
// the sites themselves have no such concept. theme.Chapter still has to be
// answered, so Chapters returns exactly **one** synthetic chapter per gallery,
// named for the gallery and holding every page. This is the representation
// that keeps the rest of Quire honest: the reader shows one entry rather than
// an empty chapter list or N chapters of one page each, and the download
// queue produces exactly one document for the whole gallery rather than
// splitting a single work across dozens of one-page "volumes".
//
// # Why AllowedHosts is nil
//
// Two of the three sites serve images from a subdomain of their own site
// (`i3.hentaifox.com` under `hentaifox.com`, `images.asmhentai.com` under
// `asmhentai.com`) — already inside the registrable-domain boundary and
// needing nothing declared. The third serves them from a wholly different
// registrable domain (`i5.nhentaimg.com`, for `nhentai.xxx`). That is not a
// property of the family, it is a property of *that* operator's CDN choice,
// and nothing here lets a theme predict what the next mirror's operator will
// pick. Naming nhentai.xxx's CDN specifically would be guessing at a pattern
// the other two sites disprove; a user adding a mirror shaped like it has to
// add its image host to the source's own allowedHosts, same as any
// independently hosted family whose CDN choice the theme cannot know in
// advance.
//
// Provenance: the shapes above were read from live responses to a plain,
// unauthenticated GET during development, and confirmed across all three
// sites before any code was written. Nothing here is transcribed from any
// site's own source; the parsing is written from scratch against observed
// markup. No site of the family is named anywhere else in this repository
// (PLAN §1.3), and the fixtures are synthetic pages under example.invalid with
// every title, tag and byline replaced by a neutral placeholder — see
// testdata/home.html for the fuller note.
package doujinreader

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
const ID = "doujinreader"

// Override keys.
const (
	// KeyReaderPath is the fallback path segment for the page reader,
	// /{readerPath}/{id}/{page}/, used only when a gallery's own detail page
	// carries no discoverable link to its reader at all (see the package
	// comment: normally Pages reads the real segment straight off that page's
	// thumbnail grid). It exists for the mirror whose grid turns out to be
	// rendered entirely by client-side script, which none of the three sites
	// compared do, but which this family's operators are exactly the kind to
	// eventually try.
	KeyReaderPath = "readerPath"

	// KeySearchPath is the path segment the site's own text search lives
	// under: /{searchPath}/?q={query}&page={page}. All three sites observed
	// use "search"; it is still a key because an empty search answers nothing
	// on this family (confirmed live: hentaifox returned zero results for
	// q=""), so a mirror that renamed it would otherwise silently break every
	// non-empty query too.
	KeySearchPath = "searchPath"
)

var spec = theme.OverrideSpec{
	ThemeID: ID,
	Keys: []theme.OverrideDoc{
		{
			Key: KeyReaderPath, Kind: theme.KindString, Default: "g",
			Why: "used only when a gallery's own detail page has no discoverable link to its reader at all; every real site compared has one, so this is a defensive fallback rather than the normal path",
		},
		{
			Key: KeySearchPath, Kind: theme.KindString, Default: "search",
			Why: "the site's own text search is a path, not a query parameter alone: /{searchPath}/?q={query}",
		},
	},
}

// Theme is the doujinreader theme.
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
// Empty. This is a family of independently branded, independently operated
// sites with no shared name to lend them, exactly like madara and
// mangathemesia; the page title is the better default and the user can rename
// the source anyway (PLAN §7.5 stage 6).
func (t *Theme) SuggestedName() string { return "" }

// AllowedHosts implements theme.Theme.
//
// nil. See the package comment: two of the three sites observed serve images
// from their own registrable domain and need nothing declared; the third's
// choice of a separate CDN domain is that operator's own and not a pattern
// the family repeats, so naming it here would be a guess presented as a fact.
func (t *Theme) AllowedHosts() []string { return nil }

// ValidateOverrides implements theme.OverrideValidator.
func (t *Theme) ValidateOverrides(raw map[string]any) error { return spec.Validate(raw) }

// OverrideKeys implements theme.OverrideValidator.
func (t *Theme) OverrideKeys() []theme.OverrideDoc { return spec.Docs() }

// Fingerprint scores a page for "is this the doujinreader family?".
//
// The probe fingerprints whatever page the user pasted (PLAN §7.5 stage 4),
// which is usually the site's home page and carries the listing shape rather
// than a gallery's own detail or reader markup — so the listing signals have
// to reach the threshold on their own, and the gallery/reader signals exist
// for the pages that carry them.
func (t *Theme) Fingerprint(p *probe.Page) int {
	score := 0
	add := func(cond bool, n int) {
		if cond {
			score += n
		}
	}

	// Home, search and "related galleries" cards: whatever the wrapper is
	// called, it carries this family's taxonomy attributes together. No other
	// family here scores on data attributes at all, which is what makes the
	// combination distinctive rather than a coincidence of one of them.
	add(p.Has(`[data-tags][data-categories]`), 40)

	// The site's own text search, present in the site-wide header of every
	// page type observed — home, search, gallery and reader alike.
	//
	// The family's ad-network snippet (go.hentaigold.net) was considered and
	// dropped as a signal: every occurrence found on all three sites is
	// wrapped in the "hide from old browsers" HTML comment
	// (`<!--//<![CDATA[ ... //]]>-->`), which probe.Page.Contains strips
	// before matching by design (see probe.Page.Contains) — so this would
	// never actually fire against a live page, only against a fixture that
	// forgot to reproduce the comment.
	add(p.Has(`form[action^="/search/"]`), 25)

	// Gallery detail page: the "load more thumbnails" AJAX contract.
	add(p.Has(`input[name="load_id"], input#load_id`), 20)
	add(p.Has(`input[name="load_dir"], input#load_dir`), 15)

	// Reader page: the per-page image and its own page-count field.
	add(p.Has(`img#fimg, img#gimg`), 20)
	add(p.Has(`input[name="pages"], input#pages`), 15)
	add(p.Has(`.reader_overlay, .full_image`), 10)

	// Negative signals. A page carrying another family's own marker is that
	// family's page however many generic class names it shares with this one.
	add(p.Contains("wp-manga"), -40)
	add(p.Contains("ts_reader.run("), -40)
	add(p.Has(`#chapter-list-container[data-api-url]`), -40)

	if score < 0 {
		return 0
	}
	return score
}

// Search implements theme.Theme.
//
// A real query goes to the site's own text search. An empty query — the UI's
// Browse (PLAN §7.5, M3 correction 1) — goes to the home listing instead: the
// search endpoint answers a blank query with nothing at all, confirmed live
// against one of the family's sites, exactly the failure mode mangakakalot's
// own Search comment describes for its family's equivalent endpoint.
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
		path = fmt.Sprintf("/?page=%d", page)
	} else {
		path = fmt.Sprintf("/%s/?q=%s&page=%d", o.PathSegment(KeySearchPath), url.QueryEscape(q), page)
	}

	doc, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var out []theme.SeriesStub
	// The one element every rendering of a gallery card carries, whatever the
	// wrapper around it is called on this particular site: the taxonomy data
	// attributes. See the package comment.
	doc.Find("[data-tags]").Each(func(_ int, card *goquery.Selection) {
		id := t.firstGalleryID(s, card)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true

		out = append(out, theme.SeriesStub{
			ID:       id,
			Title:    theme.Collapse(cardTitle(card)),
			CoverURL: t.absolutise(s, theme.ImageURL(card.Find("img").First())),
		})
	})
	return out, nil
}

// firstGalleryID returns the gallery ID of the first link inside card that
// actually points at a gallery. A card carries several anchors — a category
// label, a language flag, the thumbnail, sometimes a caption — and only one of
// them names the gallery itself.
func (t *Theme) firstGalleryID(s *theme.Source, card *goquery.Selection) string {
	var id string
	card.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, ok := a.Attr("href")
		if !ok {
			return true
		}
		if gid, ok := t.galleryLink(s, href); ok {
			id = gid
			return false
		}
		return true
	})
	return id
}

// cardTitle reads a gallery card's title, preferring the elements that carry
// nothing but the title over one that also carries a category label or a
// decorative badge.
func cardTitle(card *goquery.Selection) string {
	if t := theme.Text(card.Find(".caption").First()); t != "" {
		return t
	}
	if t := theme.Text(card.Find(".g_title").First()); t != "" {
		return t
	}
	if alt := strings.TrimSpace(card.Find("img").First().AttrOr("alt", "")); alt != "" {
		return alt
	}
	return ""
}

// Series implements theme.Theme.
//
// Everything here is scoped to the page's single `.info` block, present
// exactly once on all three sites observed and wrapping the title and the
// whole taxonomy. Scoping matters: the same taxonomy routes recur elsewhere on
// the page (a footer link to a "popular tag", a related-gallery strip's own
// cards), and an unscoped selector would credit this gallery with another
// one's tags — or its own site's navigation.
func (t *Theme) Series(ctx context.Context, s *theme.Source, id string) (*theme.Series, error) {
	doc, err := t.doc(ctx, s, id+"/")
	if err != nil {
		return nil, err
	}

	out := &theme.Series{ID: id}
	out.Title = theme.Text(doc.Find("h1").First())
	out.CoverURL = t.absolutise(s, theme.ImageURL(doc.Find(".cover img").First()))

	info := doc.Find(".info").First()
	info.Find(`a[href*="/tag/"]`).Each(func(_ int, a *goquery.Selection) {
		if g := tagText(a); g != "" {
			out.Genres = append(out.Genres, g)
		}
	})
	info.Find(`a[href*="/artist/"]`).Each(func(_ int, a *goquery.Selection) {
		if n := tagText(a); n != "" {
			out.Artists = append(out.Artists, n)
		}
	})
	// A "circle" or "group" credit is the closest thing this family has to an
	// author: the team the work is published under, as distinct from the
	// individual artist credited separately above.
	info.Find(`a[href*="/group/"], a[href*="/circle/"]`).Each(func(_ int, a *goquery.Selection) {
		if n := tagText(a); n != "" {
			out.Authors = append(out.Authors, n)
		}
	})

	// Status is deliberately left unset. None of these sites publish anything
	// resembling ongoing/completed/hiatus for a gallery — a gallery is a
	// single upload, not a serial with installments still to come — so there
	// is nothing here for the field to honestly report.
	return out, nil
}

// tagText reads one taxonomy link's own name, without the badge or count some
// skins render alongside it inside the same anchor.
func tagText(a *goquery.Selection) string {
	if name := a.Find(".tag_name").First(); name.Length() > 0 {
		return theme.Text(name)
	}
	clone := a.Clone()
	clone.Find(".t_badge, .tag_count, .gallery_count").Remove()
	return theme.Text(clone)
}

// Chapters implements theme.Theme.
//
// See the package comment: this family has no chapters, so exactly one
// synthetic chapter is returned, named for the gallery and standing in for
// every page it holds. A single element has nothing to sort, so the order is
// trivially known.
func (t *Theme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	doc, err := t.doc(ctx, s, id+"/")
	if err != nil {
		return nil, err
	}
	title := theme.Text(doc.Find("h1").First())
	if title == "" {
		title = id
	}
	return theme.SortAndMark([]theme.Chapter{{ID: id, Title: title, Number: 1}}), nil
}

// Pages implements theme.Theme.
//
// There is no bulk endpoint listing a gallery's pages: unlike the "load more
// thumbnails" AJAX (which only ever returns more *thumbnails*, still at the
// small, fixed thumbnail extension), the full-resolution image for page N is
// only ever named on page N's own reader document, and different pages of the
// same gallery were observed at different extensions (a cover in .jpg, a full
// page in .webp) — so the thumbnail's extension cannot be trusted for the
// page image, and there is nothing to do but visit each reader page in turn.
//
// The reader's own path segment is read off the gallery detail page's
// thumbnail grid rather than assumed: see the package comment for why it
// cannot be assumed to match the gallery path's own segment. The page count
// comes from the first reader page visited, which carries it in the same
// `pages` hidden field as every other reader page of the gallery.
func (t *Theme) Pages(ctx context.Context, s *theme.Source, chapterID string) ([]string, error) {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return nil, err
	}
	gallery, err := t.doc(ctx, s, chapterID+"/")
	if err != nil {
		return nil, err
	}
	numID := lastSegment(chapterID)
	segment := o.PathSegment(KeyReaderPath)
	gallery.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, ok := a.Attr("href")
		if !ok {
			return true
		}
		if seg, ok := t.readerSegment(s, numID, href); ok {
			segment = seg
			return false
		}
		return true
	})

	readerPath := func(n int) string {
		return "/" + segment + "/" + numID + "/" + strconv.Itoa(n) + "/"
	}

	first, err := t.doc(ctx, s, readerPath(1))
	if err != nil {
		return nil, err
	}
	total := readTotalPages(first)
	if total < 1 {
		total = 1
	}

	out := make([]string, 0, total)
	if u := readerImage(first); u != "" {
		out = append(out, t.absolutise(s, u))
	}
	for n := 2; n <= total; n++ {
		doc, err := t.doc(ctx, s, readerPath(n))
		if err != nil {
			return nil, fmt.Errorf("%s: page %d of %d: %w", ID, n, total, err)
		}
		if u := readerImage(doc); u != "" {
			out = append(out, t.absolutise(s, u))
		}
	}
	return out, nil
}

// readerImage reads the full-resolution page image off a reader document.
// hentaifox names the element #gimg; the other two sites observed name it
// #fimg. Everything else about the element — the lazy-load data-src, the
// alt-text template — is the same.
func readerImage(doc *goquery.Document) string {
	return theme.ImageURL(doc.Find("img#fimg, img#gimg").First())
}

// readTotalPages reads the gallery's total page count off a reader document's
// own `pages` hidden field, or -1 when it is missing or not a number.
func readTotalPages(doc *goquery.Document) int {
	v, ok := doc.Find(`input[name="pages"], input#pages`).First().Attr("value")
	if !ok {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return -1
	}
	return n
}
