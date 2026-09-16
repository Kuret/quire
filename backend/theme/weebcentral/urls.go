package weebcentral

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/theme"
)

// As in every other theme, IDs are stored site-relative, so a source that is
// later re-pointed keeps working and nothing in the library holds an absolute
// URL. Here the relative form is also the *stable* one: the identifier is a
// 26-character opaque token and the slug after it is decoration the site
// regenerates when a title is renamed.

// seriesPath turns a series ID into a request path. IDs arrive as
// "/series/{id}/{slug}"; a bare identifier is accepted too, because that is
// what a user pasting from the address bar tends to have.
func (t *Theme) seriesPath(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return strings.TrimSuffix(id, "/")
	}
	return "/" + seriesSegment + "/" + strings.Trim(id, "/")
}

// seriesToken extracts the opaque identifier from a series ID.
//
// It is needed on its own because the chapter-list fragment is addressed by
// identifier and *not* by the full path: /series/{id}/full-chapter-list, with
// no slug in between. Appending to the series path would produce
// /series/{id}/{slug}/full-chapter-list, which answers 404 — a mistake that
// costs an afternoon to find, since everything else about the ID works.
func (t *Theme) seriesToken(id string) string {
	p := strings.Trim(t.seriesPath(id), "/")
	segs := strings.Split(p, "/")
	if len(segs) < 2 || segs[0] != seriesSegment {
		return ""
	}
	return segs[1]
}

// chapterPath turns a chapter ID into a request path.
func (t *Theme) chapterPath(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return strings.TrimSuffix(id, "/")
	}
	return "/" + chapterSegment + "/" + strings.Trim(id, "/")
}

// seriesID keeps only links that are a series, returning "" for everything
// else.
//
// The filter is load-bearing rather than defensive: the search fragment mixes
// series rows with chapter rows, and a chapter link that came back as a search
// result would give the user a row that opens onto nothing.
func (t *Theme) seriesID(s *theme.Source, href string) string {
	rel := t.relativeID(s, href)
	if rel == "" {
		return ""
	}
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	if len(segs) < 2 || segs[0] != seriesSegment || segs[1] == "" {
		return ""
	}
	return rel
}

// chapterID keeps only links that are a chapter.
func (t *Theme) chapterID(s *theme.Source, href string) string {
	rel := t.relativeID(s, href)
	if rel == "" {
		return ""
	}
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	if len(segs) != 2 || segs[0] != chapterSegment || segs[1] == "" {
		return ""
	}
	return rel
}

// relativeID resolves href against the source base and returns the site
// path, or "" if it left the site.
//
// The absolute form matters here more than in most themes: the fragments this
// theme reads emit *absolute* hrefs in some places and root-relative ones in
// others, in the same document, so both have to arrive at the same ID.
func (t *Theme) relativeID(s *theme.Source, href string) string {
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
	if !strings.EqualFold(abs.Hostname(), base.Hostname()) {
		return ""
	}
	return strings.TrimSuffix(abs.EscapedPath(), "/")
}

// doc GETs a path and parses it.
//
// Every request this theme makes is discovery in PLAN §7.4's sense — a search,
// a listing, a link being followed — including the page-image fragment, which
// is markup naming the images rather than the images themselves. Nothing here
// reaches for GetRetrieval; the download queue fetching the image URLs this
// returns is a separate matter and a separate call site.
func (t *Theme) doc(ctx context.Context, s *theme.Source, path string) (*goquery.Document, error) {
	p, err := s.Policy()
	if err != nil {
		return nil, err
	}
	abs, err := s.Resolve(path)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve %s: %w", ID, path, err)
	}
	resp, err := t.f.Get(ctx, p, abs)
	if err != nil {
		return nil, fmt.Errorf("%s: get %s: %w", ID, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: %s: HTTP %d", ID, path, resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(resp.Body)))
	if err != nil {
		return nil, fmt.Errorf("%s: %s: parse HTML: %w", ID, path, err)
	}
	return doc, nil
}
