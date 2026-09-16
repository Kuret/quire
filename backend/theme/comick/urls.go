package comick

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickl/quire/backend/theme"
)

// IDs are stored site-relative. Here that matters more than usual: the
// software runs on several hostnames, they do not all behave alike (see the
// package comment), and a user moving a source from one to another should keep
// their library rather than orphan it.

// seriesID is the stored form of a series: /comic/{slug}.
func (t *Theme) seriesID(slug string) string {
	return "/" + comicSegment + "/" + strings.Trim(strings.TrimSpace(slug), "/")
}

// seriesPath turns a series ID into a request path.
func (t *Theme) seriesPath(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return strings.TrimSuffix(id, "/")
	}
	return t.seriesID(id)
}

// seriesSlug pulls the slug back out of a series ID. The chapter-list API is
// addressed by slug, not by the path.
func (t *Theme) seriesSlug(id string) string {
	segs := strings.Split(strings.Trim(t.seriesPath(id), "/"), "/")
	if len(segs) != 2 || segs[0] != comicSegment || segs[1] == "" {
		return ""
	}
	return segs[1]
}

// chapterID builds the stored form of a chapter.
//
// The site addresses a chapter by a composite of three of its fields —
// identifier, chapter number and language — and it wants all three: the
// identifier alone answers 404, which was checked rather than assumed. A
// one-shot has no chapter number and the segment is built with it empty, which
// is what the site's own frontend does with the same template.
func (t *Theme) chapterID(slug string, e chapterEntry) string {
	seg := e.HID + "-chapter-" + strings.TrimSpace(e.Chap.String()) + "-" + strings.TrimSpace(e.Lang)
	return "/" + comicSegment + "/" + strings.Trim(slug, "/") + "/" + seg
}

// chapterPath turns a chapter ID into a request path.
func (t *Theme) chapterPath(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return strings.TrimSuffix(id, "/")
	}
	return "/" + strings.Trim(id, "/")
}

// absolute resolves a possibly-relative URL against the source base.
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

// get fetches a path and returns the body.
//
// Everything this theme asks for is discovery in PLAN §7.4's sense: a search,
// a listing, a link being followed. The chapter page is markup naming the
// images, not the images themselves; those are fetched by the download queue.
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
