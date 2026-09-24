package doujinreader

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/theme"
)

// This file implements theme.Lister. Verified live against hentaifox.com and
// nhentai.xxx on 2026-09-24 (the user's own configured sources — PLAN §1.3's
// investigation exception).
//
// # What this family does not offer, and why
//
// No sort or status group is offered at all:
//
//   - There is no site-wide Popular, New or Top-rated listing on either site.
//     A "Popular" sort *does* exist (?sort=popular on /search/, and a
//     "/popular/" suffix on a taxonomy page such as /parody/{slug}/), but only
//     scoped to a search query or a specific tag/parody/artist — every
//     top-level path guessed for a site-wide equivalent (/popular/, /new/,
//     /latest/, /top/, /trending/, .../?sort=popular on the bare listing)
//     either 404s or silently ignores the parameter and returns the same
//     unsorted default page. Offering ListingPopular anyway, pointed at the
//     default listing, would answer it with ListingLatest's own page — the
//     false claim PLAN §2 forbids.
//   - ListingCompleted is not offered for the reason Series() already leaves
//     Status unset (see doujinreader.go's package comment): this family
//     ships galleries, not serials, and a gallery is never "ongoing" or
//     "finished" to begin with.
//
// Genre listings — this family's tags — are offered, sourced from the site's
// own /tags/ index.
//
// # Why one page of /tags/, ranked, rather than the whole index
//
// /tags/ paginates in the thousands (64 pages observed live on hentaifox.com
// alone), and PLAN §1.3's browse listing guidance is explicit that a genre
// list is "a sensible set ... not thousands". Crawling all of it once a day
// just to offer a tag picker would also be a lot of unasked-for load on a
// site that gets nothing out of Quire's user in return.
//
// So this reads exactly the first page and ranks what it finds. Every tag
// entry on both sites carries its own real gallery count next to its name
// (hentaifox: a `.badge` with a bare number; nhentai.xxx: a `.tag_count` with
// the same count abbreviated, "63K") — published data, not something this
// theme estimates — and genreCandidates sorts by it, descending, before
// capping to maxGenreListings. The one page is alphabetically first, so the
// ranking is only ever as good as what that page happens to contain; it is
// not "the site's true top N tags", but it is real counts off a real page,
// or bust — and in practice both sites interleave enough high-count tags
// into their first page (nhentai.xxx's page 1 alone carries "ahegao" at 63K)
// that the ranked top of it is a usable set.

// tagsListPath is the site's tag index, page 1. Both sites paginate it
// differently past this point (hentaifox: /tags/pag/{n}/; nhentai.xxx:
// /tags/?page={n}), which is exactly the kind of per-mirror route this
// family's package comment says to discover rather than configure — and
// which this file has no reason to, since it only ever reads page 1.
const tagsListPath = "/tags/"

// maxGenreListings caps how many of the ranked tags Listings offers. Not a
// site limit — both sites paginate far past this — but PLAN §1.3's own
// guidance for this family: a picker, not the whole tag index.
const maxGenreListings = 30

// Listings implements theme.Lister.
func (t *Theme) Listings(ctx context.Context, s *theme.Source) ([]theme.Listing, error) {
	doc, err := t.doc(ctx, s, tagsListPath)
	if err != nil {
		return nil, err
	}

	cands := t.genreCandidates(s, doc)
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].count > cands[j].count })
	if len(cands) > maxGenreListings {
		cands = cands[:maxGenreListings]
	}

	out := make([]theme.Listing, 0, len(cands))
	for _, c := range cands {
		out = append(out, theme.GenreListing(c.slug, c.label))
	}
	return out, nil
}

// genreCandidate is one row of the tag index page: a slug this theme can
// browse by, its display label, and the gallery count the site published
// alongside it — the ranking signal Listings sorts by.
type genreCandidate struct {
	slug  string
	label string
	count int
}

// genreCandidates reads every tag entry off one /tags/ page. Unlike a gallery
// card, a tag entry has no reliable wrapper element to scope a search to —
// both sites render the tag index as a flat list of anchors with nothing else
// on the page competing for the same taxonomy links — so this iterates every
// `a[href*="/tag/"]` directly rather than scoping to a card first, the way
// Series() must for a gallery's own taxonomy links.
func (t *Theme) genreCandidates(s *theme.Source, doc *goquery.Document) []genreCandidate {
	seen := make(map[string]bool)
	var out []genreCandidate
	doc.Find(`a[href*="/tag/"]`).Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		slug, ok := t.tagLink(s, href)
		if !ok || seen[slug] {
			return
		}
		label := tagIndexLabel(a)
		if label == "" {
			return
		}
		seen[slug] = true
		out = append(out, genreCandidate{slug: slug, label: label, count: tagCount(a)})
	})
	return out
}

// tagIndexLabel reads one /tags/ entry's own name, without the gallery-count
// badge next to it.
//
// This is a separate function from Series()'s tagText rather than a reuse of
// it: the tags-index page has its own count element, `.badge`
// (hentaifox.com), which tagText does not know to strip because nothing on a
// gallery's own taxonomy list (what tagText reads) uses that class — see
// tagCount below for the same element read as a number instead of stripped
// away.
func tagIndexLabel(a *goquery.Selection) string {
	if name := a.Find(".tag_name").First(); name.Length() > 0 {
		return theme.Text(name)
	}
	if name := a.Find(".list_tag").First(); name.Length() > 0 {
		return theme.Text(name)
	}
	clone := a.Clone()
	clone.Find(".badge, .tag_count").Remove()
	return theme.Text(clone)
}

// tagCount reads a tag entry's own published gallery count — hentaifox's
// `.badge` or nhentai.xxx's `.tag_count`, whichever the anchor carries — or 0
// when neither is present or parses. 0 is not "no tag": it only affects where
// this entry lands in Listings' ranking, never whether it is offered.
func tagCount(a *goquery.Selection) int {
	raw := theme.Text(a.Find(".badge, .tag_count").First())
	return parseCount(raw)
}

// parseCount reads a gallery count as either site spells it: a bare integer
// (optionally with thousands commas), or one abbreviated with a "K"/"M"
// suffix (nhentai.xxx: "63K", "1.5K"). Anything else — including empty —
// reads as 0.
func parseCount(raw string) int {
	raw = strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(raw, ",", "")))
	if raw == "" {
		return 0
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(raw, "K"):
		mult = 1_000
		raw = strings.TrimSuffix(raw, "K")
	case strings.HasSuffix(raw, "M"):
		mult = 1_000_000
		raw = strings.TrimSuffix(raw, "M")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return int(n * mult)
}

// List implements theme.Lister.
func (t *Theme) List(ctx context.Context, s *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	if page < 1 {
		page = 1
	}
	if listingID == theme.ListingLatest {
		// Today's Browse, delegated exactly (theme.Lister's doc comment).
		return t.Search(ctx, s, "", page)
	}
	slug, ok := theme.GenreID(listingID)
	if !ok {
		return nil, fmt.Errorf("%s: source %q: unknown listing %q", ID, s.ID, listingID)
	}
	return t.listGenre(ctx, s, slug, page)
}

// listGenre reads one page of a tag's own listing, /tag/{slug}/.
//
// Page 1 is fetched directly. A later page is discovered rather than
// assumed: see the package comment on why routes here are read off the page
// rather than configured — the two sites observed paginate a tag listing
// differently (hentaifox: /tag/{slug}/pag/{n}/; nhentai.xxx:
// /tag/{slug}/?page={n}), the same divergence the package comment already
// documents for the gallery and reader routes. Page 1's own pagination
// widget is what actually names page N's real URL on whichever mirror this
// source is, so this reads that link rather than guessing between the two
// known shapes — at the cost of an extra request to reach page 1 first
// whenever the caller asks straight for page 2 or later. A page number this
// widget does not offer (past the last page) yields an empty result, not an
// error: that is what "no more results" looks like to a pager.
func (t *Theme) listGenre(ctx context.Context, s *theme.Source, slug string, page int) ([]theme.SeriesStub, error) {
	return t.paginatedListing(ctx, s, "/tag/"+slug+"/", page)
}

// paginatedListing fetches page 1 of the gallery listing at path and, for any
// later page, follows *that page's own* pagination link rather than building
// one from a guessed query shape — the same approach the home listing's
// Search("") page>1 needs (doujinreader.go) and this file's own listGenre
// used to duplicate: hentaifox's real pagination is /page/{n}/ on the home
// listing and /tag/{slug}/pag/{n}/ on a tag listing, both silently ignored by
// a bare ?page={n}, while nhentai.xxx uses ?page={n} on both. Reading page
// 1's own '.pagination' widget for page n's real link works on either mirror
// without knowing which one this source is. A page number the widget does
// not offer (past the last page) is an empty result, not an error — that is
// what "no more results" looks like to a pager.
func (t *Theme) paginatedListing(ctx context.Context, s *theme.Source, path string, page int) ([]theme.SeriesStub, error) {
	first, err := t.doc(ctx, s, path)
	if err != nil {
		return nil, err
	}
	if page == 1 {
		return t.scrapeGalleries(s, first, t.absolutise(s, path)), nil
	}

	href, ok := t.paginationLink(first, page)
	if !ok {
		return nil, nil
	}
	abs := t.absolutise(s, href)
	if abs == "" {
		return nil, nil
	}
	// t.doc resolves whatever it is given against the source base; an already
	// absolute URL (as abs is, and as a protocol-relative pagination href
	// resolves to) survives that resolution unchanged.
	doc, err := t.doc(ctx, s, abs)
	if err != nil {
		return nil, err
	}
	return t.scrapeGalleries(s, doc, abs), nil
}

// paginationLink reads the href of the pagination widget's link for page n
// off a listing page's own markup — the same '.pagination .page-link'
// structure observed on both sites' home, tag and search listings — rather
// than assuming either mirror's URL shape.
func (t *Theme) paginationLink(doc *goquery.Document, n int) (href string, ok bool) {
	want := strconv.Itoa(n)
	doc.Find(".pagination a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if strings.TrimSpace(a.Text()) != want {
			return true
		}
		if h, exists := a.Attr("href"); exists && h != "" && h != "#" {
			href, ok = h, true
			return false
		}
		return true
	})
	return href, ok
}

// tagLink reports whether href names a tag, and if so its slug: a path of
// exactly two segments whose first is "tag" — the shape every tag link on
// this family takes, distinct from a gallery link (galleryLink) only in
// requiring the first segment's literal name rather than accepting any.
func (t *Theme) tagLink(s *theme.Source, href string) (slug string, ok bool) {
	segs := t.pathSegments(s, href)
	if len(segs) != 2 || segs[0] != "tag" || segs[1] == "" {
		return "", false
	}
	return segs[1], true
}
