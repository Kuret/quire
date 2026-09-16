package fanfox

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/theme"
)

// IDs are stored site-relative, as everywhere else, so a source re-pointed at
// a mirror keeps working and nothing in the library holds an absolute URL.

// seriesPath turns a series ID into a request path.
func (t *Theme) seriesPath(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return id
	}
	return "/" + seriesSegment + "/" + strings.Trim(id, "/") + "/"
}

// seriesID keeps only links that are a series, returning "" for anything else.
//
// A series path is /{seriesSegment}/{slug}/ and nothing deeper: one more
// segment makes it a chapter, and a chapter returned as a search result is a
// row that opens onto a single page of one chapter.
func (t *Theme) seriesID(s *theme.Source, href string) string {
	rel := t.relativeID(s, href)
	if rel == "" {
		return ""
	}
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	if len(segs) != 2 || segs[0] != seriesSegment || segs[1] == "" {
		return ""
	}
	return "/" + segs[0] + "/" + segs[1] + "/"
}

// chapterID keeps only links that are a chapter page.
//
// The shape is /{seriesSegment}/{slug}[/{volume}]/{chapter}/{page}.html, so a
// chapter link is any series link with more segments beneath it.
func (t *Theme) chapterID(s *theme.Source, href string) string {
	rel := t.relativeID(s, href)
	if rel == "" {
		return ""
	}
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	if len(segs) < 3 || segs[0] != seriesSegment {
		return ""
	}
	return rel
}

// relativeID resolves href against the source base and returns the site path,
// or "" if it left the site.
//
// The mobile host counts as the site: the reader's own links are absolute and
// point at it, and rejecting them would break any theme code that followed
// one.
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
	if !sameSite(abs.Hostname(), base.Hostname()) {
		return ""
	}
	return abs.EscapedPath()
}

// sameSite reports whether host is the configured host or its mobile sibling.
func sameSite(host, base string) bool {
	// theme.SameSite covers the apex/www equivalence; the mobile sibling is
	// this theme's own addition, because the scroll reader lives there and its
	// links are absolute.
	return theme.SameSite(host, base) || theme.SameSite(host, mobileHost(base))
}

// mobileHost is the `m.` sibling of a host. It stays under the same
// registrable domain as the configured base, so PLAN §7.4's boundary permits
// it with no AllowedHosts entry; the declaration this theme does make is for
// the image CDNs, which are genuinely different domains.
func mobileHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "m.") {
		return host
	}
	return "m." + strings.TrimPrefix(host, "www.")
}

// readerURL is the absolute mobile-reader URL for a chapter.
//
// Two changes to the chapter path: the host gains its `m.` prefix, and the
// series segment becomes the scroll-view segment. It is one function because
// two callers need exactly the same URL — Pages() to fetch it, and
// PageReferer() to name it — and a Referer naming a URL built by a second,
// nearly-identical path would eventually stop matching the one we fetched,
// which is how a truthful header becomes a false one.
func (t *Theme) readerURL(s *theme.Source, chapterID string) string {
	rel := strings.TrimSpace(chapterID)
	if rel == "" {
		return ""
	}
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	if len(segs) < 3 || segs[0] != seriesSegment {
		return ""
	}
	segs[0] = readerSegment

	base, err := url.Parse(s.BaseURL)
	if err != nil {
		return ""
	}
	m := *base
	m.Host = mobileHost(base.Hostname())
	if p := base.Port(); p != "" {
		m.Host += ":" + p
	}
	m.Path = "/" + strings.Join(segs, "/")
	m.RawQuery = ""
	m.Fragment = ""
	return m.String()
}

// absolute resolves a possibly-relative URL against the source base.
//
// Protocol-relative URLs ("//host/path") are routine in this site's markup and
// url.ResolveReference handles them, inheriting the base's scheme — which is
// https, so a page image never silently downgrades.
func (t *Theme) absolute(s *theme.Source, ref string) string {
	if strings.TrimSpace(ref) == "" {
		return ""
	}
	abs, err := s.Resolve(ref)
	if err != nil {
		return ""
	}
	return abs
}

// get fetches a path or absolute URL and returns the body.
//
// Everything here is discovery in PLAN §7.4's sense — a search, a listing, a
// link being followed. The page images themselves are retrieval and are
// fetched by the download queue, not by this theme.
func (t *Theme) get(ctx context.Context, s *theme.Source, ref string) (string, error) {
	p, err := s.Policy()
	if err != nil {
		return "", err
	}
	abs := ref
	if !strings.Contains(ref, "://") {
		if abs, err = s.Resolve(ref); err != nil {
			return "", fmt.Errorf("%s: resolve %s: %w", ID, ref, err)
		}
	}
	resp, err := t.f.Get(ctx, p, abs)
	if err != nil {
		return "", fmt.Errorf("%s: get %s: %w", ID, ref, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%s: %s: HTTP %d", ID, ref, resp.StatusCode)
	}
	return string(resp.Body), nil
}

// doc fetches a path and parses it as HTML.
func (t *Theme) doc(ctx context.Context, s *theme.Source, path string) (*goquery.Document, error) {
	body, err := t.get(ctx, s, path)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %s: parse HTML: %w", ID, path, err)
	}
	return doc, nil
}
