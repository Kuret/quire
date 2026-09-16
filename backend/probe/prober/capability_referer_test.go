package prober_test

import (
	"net/url"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.6, decided 2026-09-16: a truthful Referer is permitted, and stage 5's
// image fetch is one of the two places that sends one. Without it, three sites
// that parse perfectly would be probed as "couldn't fetch one of the pages" —
// the image host answers 403 without a Referer and 200 with one.
//
// The other half of the rule is the one these tests pin hardest: a theme with
// no page to name sends **no header at all**, not an empty one and not a
// stand-in derived from the base URL.

// refererStubTheme names a different page per chapter, so "the chapter stage 5
// actually read" is distinguishable from any other answer.
type refererStubTheme struct {
	stubTheme
	byChapter map[string]string
}

func (s refererStubTheme) PageReferer(_ *theme.Source, chapterID string) string {
	return s.byChapter[chapterID]
}

// imageRequest returns the fetcher's call for the page image, which is the only
// request stage 5 makes to the image path.
func imageRequest(t *testing.T, f *themetest.Fetcher, path string) themetest.Request {
	t.Helper()
	for _, c := range f.Calls() {
		u, err := url.Parse(c.URL)
		if err != nil {
			continue
		}
		if u.EscapedPath() == path {
			return c
		}
	}
	t.Fatalf("stage 5 never fetched %s", path)
	return themetest.Request{}
}

func TestProbeImageFetchCarriesThePageReferer(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	const want = "https://example.invalid/series/one/1/"
	th := refererStubTheme{
		stubTheme: stubTheme{id: "alpha", score: 90},
		byChapter: map[string]string{"/series/one/1/": want},
	}

	res := runWithFetcher(t, f, th)
	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
	}

	req := imageRequest(t, f, "/p/1.jpg")
	if req.Referrer.IsZero() {
		t.Fatal("stage 5 fetched the image with no Referer; the three sites this exists for answer 403 to that")
	}
	if got := req.Referrer.String(); got != want {
		t.Errorf("Referer = %q, want the chapter page stage 5 read, %q", got, want)
	}
}

func TestProbeImageFetchSendsNoRefererWhenTheThemeHasNone(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})

	res := runWithFetcher(t, f, stubTheme{id: "alpha", score: 90})
	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
	}

	req := imageRequest(t, f, "/p/1.jpg")
	// Absence, explicitly. An empty Referer is a different request from no
	// Referer, and PLAN §7.6 requires the second: a header naming a page we did
	// not fetch is a lie, so silence is the honest answer.
	if !req.Referrer.IsZero() {
		t.Fatalf("a theme with no PageReferrer must send no Referer, got %q", req.Referrer.String())
	}
}

// A theme that names a page address Quire cannot use is a bug in the theme.
// Stage 5 says so rather than quietly dropping the header or inventing one.
func TestProbeImageFetchReportsAnUnusablePageReferer(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /": {File: "home-unrecognised.html"},
	})
	th := refererStubTheme{
		stubTheme: stubTheme{id: "alpha", score: 90},
		byChapter: map[string]string{"/series/one/1/": "/series/one/1/"}, // relative
	}

	res := runWithFetcher(t, f, th)
	if res.Addable {
		t.Fatal("a theme whose Referer cannot be built must not be waved through as addable")
	}
	if res.Verdict == theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want a failing one", res.Verdict, res.Detail)
	}
}
