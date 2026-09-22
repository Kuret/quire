package asurascans

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/rickl/quire/backend/theme"
)

// apiLabel is the subdomain label the site's own front end calls its JSON API
// on. It is prepended to the source's own host rather than written out as a
// full domain, so nothing in this file names the site itself (PLAN §1.3) — the
// derivation works for whatever host a source is actually configured with,
// live or a ".invalid" fixture host alike.
const apiLabel = "api"

// genre is one entry of a series' genre list.
type genre struct {
	Name string `json:"name"`
}

// seriesEntry is one series as the API renders it — identical in shape
// whether it arrives from a listing, a search or a direct lookup. Fields the
// theme has no use for (popularity_rank, bookmark_count, rating,
// public_url, source_url, banner, latest_chapters, ...) are simply left
// unmapped; encoding/json ignores what a struct does not name.
type seriesEntry struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	AltTitles   []string `json:"alt_titles"`
	Description string   `json:"description"`
	Cover       string   `json:"cover"`
	Status      string   `json:"status"`
	Author      string   `json:"author"`
	Artist      string   `json:"artist"`
	Genres      []genre  `json:"genres"`
}

// seriesListResponse is what /api/series and /api/search answer: a page of
// entries plus paging metadata neither Search caller needs today.
type seriesListResponse struct {
	Data []seriesEntry `json:"data"`
}

// seriesDetailResponse is what /api/series/{slug} answers. The site's own
// recommendation rail rides along as "recommended_series"; Series() has no
// use for it and it is left unmapped.
type seriesDetailResponse struct {
	Series seriesEntry `json:"series"`
}

// chapterEntry is one row of a series' chapter list.
type chapterEntry struct {
	Number      float64 `json:"number"`
	PublishedAt string  `json:"published_at"`
}

type chapterListResponse struct {
	Data []chapterEntry `json:"data"`
}

// chapterPageResponse is what /api/series/{slug}/chapters/{number} answers.
type chapterPageResponse struct {
	Data struct {
		// AccessGate is non-empty when the chapter exists but is not readable
		// without whatever the site is asking for (a subscription, an early-
		// access wait) — see Pages' comment on why this is checked rather than
		// trusted to show up as an empty page list.
		AccessGate string `json:"access_gate"`
		Chapter    struct {
			Pages []struct {
				URL string `json:"url"`
			} `json:"pages"`
		} `json:"chapter"`
	} `json:"data"`
}

// apiURL builds an absolute API request URL from the source's own base.
//
// The API lives on a sibling subdomain of the site the user actually typed —
// observed live as a fact about this one deployment, not assumed — so the
// label is prepended to the source's own host rather than to a domain named
// in this file. That is what lets the offline tests run against a
// ".invalid" fixture host and a live source resolve to the real API, from
// the same code. A leading "www." is dropped first, so a source added from
// the www. host does not produce "api.www.…", which is not the API's name.
func apiURL(s *theme.Source, path string, v url.Values) (string, error) {
	base, err := url.Parse(strings.TrimSpace(s.BaseURL))
	if err != nil {
		return "", fmt.Errorf("%s: parse baseUrl: %w", ID, err)
	}
	host := strings.TrimPrefix(strings.ToLower(base.Hostname()), "www.")
	if host == "" {
		return "", fmt.Errorf("%s: source %q: baseUrl %q has no host", ID, s.ID, s.BaseURL)
	}
	u := *base
	u.Host = apiLabel + "." + host
	if p := base.Port(); p != "" {
		u.Host += ":" + p
	}
	u.Path = path
	u.RawPath = ""
	if len(v) > 0 {
		u.RawQuery = v.Encode()
	} else {
		u.RawQuery = ""
	}
	u.Fragment = ""
	return u.String(), nil
}

// apiGet fetches path from the API host and decodes the JSON envelope into
// out. It is discovery in PLAN §7.4's sense at every call site this theme
// has: a search, a series lookup, a chapter list, or the manifest of a
// chapter's page URLs — never the images themselves, which the download
// queue fetches separately through the ordinary guarded client.
func (t *Theme) apiGet(ctx context.Context, s *theme.Source, path string, v url.Values, out any) error {
	p, err := s.Policy()
	if err != nil {
		return err
	}
	u, err := apiURL(s, path, v)
	if err != nil {
		return err
	}
	resp, err := t.f.Get(ctx, p, u)
	if err != nil {
		return fmt.Errorf("%s: get %s: %w", ID, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s: %s: HTTP %d", ID, path, resp.StatusCode)
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("%s: %s: decode: %w", ID, path, err)
	}
	return nil
}
