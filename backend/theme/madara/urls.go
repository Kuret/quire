package madara

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/rickl/quire/backend/theme"
)

// Series and chapter IDs are stored *site-relative* ("/manga/some-slug/"),
// never absolute. That is what makes PLAN §7.2's "never derived from the URL,
// so a source can be re-pointed" work at the item level too: a user who moves
// a source to a mirror keeps their library, because the stored IDs never
// mentioned the old host.

func (t *Theme) overrides(s *theme.Source) (theme.Overrides, error) {
	return spec.Resolve(s.Overrides)
}

// seriesPath turns a stored series ID into a request path. An ID that is
// already a path is used as-is; a bare slug is placed under mangaSubPath, so a
// caller that only has a slug still works.
func (t *Theme) seriesPath(s *theme.Source, id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "/") {
		return id
	}
	o, err := spec.Resolve(s.Overrides)
	if err != nil {
		// Validation happens at load time; a source that failed it never gets
		// here. Fall back to the plugin's own default rather than panicking.
		return "/manga/" + strings.Trim(id, "/") + "/"
	}
	return "/" + o.PathSegment(KeyMangaSubPath) + "/" + strings.Trim(id, "/") + "/"
}

// searchCandidate is a search result before the dominant-segment filter has
// had a chance to say whether it is a series or a decoy (a tag, genre or
// author link the same markup also carries).
type searchCandidate struct {
	id    string
	title string
	cover string
}

// firstSegment returns the first path segment of a site-relative ID, or ""
// for a bare "/".
func firstSegment(id string) string {
	trimmed := strings.TrimPrefix(id, "/")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

// dominantSegment picks the path segment the most candidates agree on, which
// is the theme's evidence-based stand-in for "the site's series path" — see
// the long comment at Search's call site for why a configured or guessed
// default cannot do this job. Ties keep whichever segment was seen first, so
// the result is deterministic for a given response.
func dominantSegment(candidates []searchCandidate) string {
	counts := map[string]int{}
	var order []string
	for _, c := range candidates {
		seg := firstSegment(c.id)
		if seg == "" {
			continue
		}
		if _, seen := counts[seg]; !seen {
			order = append(order, seg)
		}
		counts[seg]++
	}
	best, bestN := "", 0
	for _, seg := range order {
		if counts[seg] > bestN {
			best, bestN = seg, counts[seg]
		}
	}
	return best
}

// relativeID reduces an href to a site-relative path, or "" if it points off
// the source entirely.
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
	// theme.SameSite rather than an exact host comparison: a site that
	// redirects its apex to www emits absolute links there, and comparing
	// exactly discarded every one of them. See site.go — this was worth a
	// live search returning zero results.
	if !theme.SameSite(abs.Hostname(), base.Hostname()) {
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
	p, err := s.Policy()
	if err != nil {
		return nil, err
	}
	abs, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	resp, err := t.f.Get(ctx, p, abs)
	if err != nil {
		return nil, fmt.Errorf("%s: get %s: %w", ID, path, err)
	}
	return parse(resp.StatusCode, resp.Body, path)
}

// postDoc POSTs a form and parses the fragment that comes back. The fragment
// is a bare <ul> of chapters, which goquery wraps in a document for us.
func (t *Theme) postDoc(ctx context.Context, s *theme.Source, path string, form url.Values) (*goquery.Document, error) {
	p, err := s.Policy()
	if err != nil {
		return nil, err
	}
	abs, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	if form == nil {
		form = url.Values{}
	}
	resp, err := t.f.PostForm(ctx, p, abs, form)
	if err != nil {
		return nil, fmt.Errorf("%s: post %s: %w", ID, path, err)
	}
	// A 404 from the AJAX endpoint is how a site says "I am the other shape".
	// Report an empty fragment rather than an error so Chapters can fall back
	// to the series HTML.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		return goquery.NewDocumentFromReader(strings.NewReader(""))
	}
	return parse(resp.StatusCode, resp.Body, path)
}

func parse(status int, body []byte, path string) (*goquery.Document, error) {
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("%s: %s: HTTP %d", ID, path, status)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("%s: %s: parse HTML: %w", ID, path, err)
	}
	return doc, nil
}
