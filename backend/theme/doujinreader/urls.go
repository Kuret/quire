package doujinreader

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rickl/quire/backend/theme"
)

// IDs are stored as the relative path a gallery was actually linked by —
// "/g/700156" or "/gallery/173573", whichever the site used — with no
// trailing slash. See the package comment: this is what lets the gallery
// route be *discovered* per site rather than guessed at with an override.

// galleryLink reports whether href names a gallery, and if so its canonical
// ID: a link is a gallery's own page when it resolves to a path of exactly
// two non-empty segments and the second is purely numeric — the shape every
// gallery link on this family takes, and the shape no taxonomy link
// (/tag/{slug}/, /category/{slug}/, ...) or reader link
// (/{segment}/{id}/{page}/) ever does, whatever the first segment is called.
func (t *Theme) galleryLink(s *theme.Source, href string) (id string, ok bool) {
	segs := t.pathSegments(s, href)
	if len(segs) != 2 {
		return "", false
	}
	if _, err := strconv.Atoi(segs[1]); err != nil {
		return "", false
	}
	return "/" + segs[0] + "/" + segs[1], true
}

// readerSegment reports the path segment a reader link for gallery numID
// uses, when href is such a link: a path of exactly three segments, the
// second matching numID and the third purely numeric (a page number). It is
// how Pages finds the reader's own route on a gallery whose detail page lives
// under a different one — see the package comment.
func (t *Theme) readerSegment(s *theme.Source, numID, href string) (segment string, ok bool) {
	segs := t.pathSegments(s, href)
	if len(segs) != 3 || segs[1] != numID {
		return "", false
	}
	if _, err := strconv.Atoi(segs[2]); err != nil {
		return "", false
	}
	return segs[0], true
}

// pathSegments resolves href against the source, checks it stayed on the same
// site, and splits its path into non-empty segments. A link that left the
// site or resolved to the root yields no segments, which every caller above
// already treats as "not a match".
func (t *Theme) pathSegments(s *theme.Source, href string) []string {
	rel := t.relativeID(s, href)
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return nil
	}
	return strings.Split(rel, "/")
}

// relativeID reduces an absolute link on the site's own host to a path
// without a trailing slash, and rejects anything that left the site.
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
	// theme.SameSite rather than an exact host comparison: an apex that
	// redirects to www would otherwise discard every link on the page. See
	// mangakakalot/urls.go, which found this the hard way against a live site.
	if !theme.SameSite(abs.Hostname(), base.Hostname()) {
		return ""
	}
	return strings.TrimSuffix(abs.EscapedPath(), "/")
}

// lastSegment returns the final path segment of a stored gallery ID — the
// bare gallery number, whatever segment precedes it.
func lastSegment(id string) string {
	id = strings.Trim(id, "/")
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// absolutise resolves a possibly relative or protocol-relative image URL
// against the source, returning "" on a URL that will not resolve rather than
// propagating an error a caller has no useful way to act on for one image
// among many.
func (t *Theme) absolutise(s *theme.Source, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	abs, err := s.Resolve(raw)
	if err != nil {
		return ""
	}
	return abs
}

// doc GETs a path and parses it.
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
		return nil, fmt.Errorf("%s: parse %s: %w", ID, path, err)
	}
	return doc, nil
}
