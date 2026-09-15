package mangathemesia

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/rickl/quire/backend/theme"
)

// As in madara, IDs are stored site-relative so a source can be re-pointed at
// a mirror without orphaning anything.

func (t *Theme) seriesPath(s *theme.Source, id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return id
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return "/manga/" + strings.Trim(id, "/") + "/"
	}
	return "/" + o.PathSegment(KeySeriesSubPath) + "/" + strings.Trim(id, "/") + "/"
}

// seriesID keeps only links under the series segment. Unlike madara, this
// theme's search has no post_type filter, so its results genuinely do contain
// pages and posts; the filter is load-bearing rather than defensive.
func (t *Theme) seriesID(s *theme.Source, href string) string {
	rel := t.relativeID(s, href)
	if rel == "" {
		return ""
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		return rel
	}
	if seg := o.PathSegment(KeySeriesSubPath); seg != "" && !strings.HasPrefix(rel, "/"+seg+"/") {
		return ""
	}
	return rel
}

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
	out := abs.EscapedPath()
	if abs.RawQuery != "" {
		out += "?" + abs.RawQuery
	}
	return out
}

// doc GETs a path and parses it.
func (t *Theme) doc(ctx context.Context, s *theme.Source, path string) (*goquery.Document, error) {
	_, doc, err := t.page(ctx, s, path)
	return doc, err
}

// page GETs a path and returns both the raw body and the parsed document.
// Pages() needs the raw body: the ts_reader blob lives in inline script, and
// going back to the DOM to re-serialise it would be a waste on a device this
// small.
func (t *Theme) page(ctx context.Context, s *theme.Source, path string) (string, *goquery.Document, error) {
	p, err := s.Policy()
	if err != nil {
		return "", nil, err
	}
	abs, err := s.Resolve(path)
	if err != nil {
		return "", nil, err
	}
	resp, err := t.f.Get(ctx, p, abs)
	if err != nil {
		return "", nil, fmt.Errorf("%s: get %s: %w", ID, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, fmt.Errorf("%s: %s: HTTP %d", ID, path, resp.StatusCode)
	}
	body := string(resp.Body)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("%s: %s: parse HTML: %w", ID, path, err)
	}
	return body, doc, nil
}
