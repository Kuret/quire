package mangakakalot

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/theme"
)

// IDs are stored site-relative, as in the other themes, so a source pointed at
// a different mirror of the same family keeps working. That matters more here
// than elsewhere: mirrors are this family's defining characteristic.

// seriesSubPath is the configured segment, with the default applied.
func (t *Theme) seriesSubPath(s *theme.Source) string {
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return "manga"
	}
	if seg := o.PathSegment(KeySeriesSubPath); seg != "" {
		return seg
	}
	return "manga"
}

// seriesPath turns a stored series ID into a path to fetch.
func (t *Theme) seriesPath(s *theme.Source, id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return id
	}
	return "/" + t.seriesSubPath(s) + "/" + strings.Trim(id, "/")
}

// seriesSlug is the last segment of a series ID: the handle the chapter API
// and the chapter URLs are both built from.
func (t *Theme) seriesSlug(s *theme.Source, id string) string {
	p := strings.Trim(t.seriesPath(s, id), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// chapterAPIPath is the JSON endpoint holding the chapter list. limit=-1 asks
// for the whole list, which is what makes this one request rather than a walk
// through the API's pagination.
func (t *Theme) chapterAPIPath(s *theme.Source, id string) string {
	return "/api/manga/" + url.PathEscape(t.seriesSlug(s, id)) + "/chapters?limit=-1"
}

// chapterID is the stored handle for one chapter: site-relative, and the same
// path the reader is served from.
func (t *Theme) chapterID(s *theme.Source, seriesSlug, chapterSlug string) string {
	return "/" + t.seriesSubPath(s) + "/" + seriesSlug + "/" + strings.Trim(chapterSlug, "/")
}

// chapterPath turns a stored chapter ID into a path to fetch.
func (t *Theme) chapterPath(s *theme.Source, id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return id
	}
	return "/" + strings.Trim(id, "/")
}

// seriesID keeps only links that point at a series, and drops the ones that
// point at a chapter of it. A listing card links to both — the cover goes to
// the series, the "Chapter 12" line goes to the reader — so without the second
// check a search would return reader URLs as if they were series.
func (t *Theme) seriesID(s *theme.Source, href string) string {
	rel := t.relativeID(s, href)
	if rel == "" {
		return ""
	}
	seg := t.seriesSubPath(s)
	prefix := "/" + seg + "/"
	if !strings.HasPrefix(rel, prefix) {
		return ""
	}
	if strings.Contains(strings.TrimPrefix(rel, prefix), "/") {
		return "" // /manga/{slug}/{chapter}: a chapter, not a series
	}
	return rel
}

// relativeID reduces an absolute link on the site's own host to a path, and
// rejects anything that left it.
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
func (t *Theme) doc(ctx context.Context, s *theme.Source, path string) (*goquery.Document, error) {
	_, doc, err := t.page(ctx, s, path)
	return doc, err
}

// get GETs a path and returns the raw body. The chapter list is JSON, so there
// is nothing to parse as HTML.
func (t *Theme) get(ctx context.Context, s *theme.Source, path string) (string, error) {
	p, err := s.Policy()
	if err != nil {
		return "", err
	}
	abs, err := s.Resolve(path)
	if err != nil {
		return "", fmt.Errorf("%s: resolve %s: %w", ID, path, err)
	}
	resp, err := t.f.Get(ctx, p, abs)
	if err != nil {
		return "", fmt.Errorf("%s: get %s: %w", ID, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%s: %s: HTTP %d", ID, path, resp.StatusCode)
	}
	return string(resp.Body), nil
}

// page GETs a path and returns both the raw body and the parsed document.
// Pages() needs the raw body because the reader's image list is also carried
// in inline script, and re-serialising the DOM to get at it would be a waste
// on a device this small.
func (t *Theme) page(ctx context.Context, s *theme.Source, path string) (string, *goquery.Document, error) {
	body, err := t.get(ctx, s, path)
	if err != nil {
		return "", nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("%s: parse %s: %w", ID, path, err)
	}
	return body, doc, nil
}
