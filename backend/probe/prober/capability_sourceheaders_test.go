package prober_test

import (
	"net/http"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// headeredStubTheme is stubTheme plus theme.SourceHeaders, standing in for a
// theme like globalcomix whose source needs a static header attached to its
// own requests — including the page-image fetch stage 5 performs.
type headeredStubTheme struct {
	stubTheme
	headers http.Header
}

func (h headeredStubTheme) SourceHeaders(*theme.Source) http.Header { return h.headers }

// cookieStubTheme is stubTheme plus theme.CookieUser, standing in for a theme
// whose source carries a server-issued session cookie into the page-image
// fetches that follow a grant.
type cookieStubTheme struct {
	stubTheme
	use bool
}

func (c cookieStubTheme) UsesCookies(*theme.Source) bool { return c.use }

// TestStage5PageImageFetchCarriesSourceHeaders is the mutation (a) test for
// capability.go's fetchOnePageImage: reverting its policy construction from
// theme.PolicyFor(th, src) back to the bare src.Policy() it used to call
// makes this fail, because a bare Source.Policy() never carries a theme's
// SourceHeaders — PolicyFor is the only place that side interface is
// consulted (see theme.PolicyFor).
func TestStage5PageImageFetchCarriesSourceHeaders(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	th := headeredStubTheme{
		stubTheme: stubTheme{id: "alpha", score: 90},
		headers:   http.Header{"X-Reading-Grant": []string{"granted-token"}},
	}
	res := runWithFetcher(t, f, th)
	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
	}

	var found bool
	for _, c := range f.Calls() {
		if c.URL != "https://example.invalid/p/1.jpg" {
			continue
		}
		found = true
		if c.Policy == nil || c.Policy.Headers.Get("X-Reading-Grant") != "granted-token" {
			t.Errorf("page image request policy = %+v, want X-Reading-Grant: granted-token", c.Policy)
		}
	}
	if !found {
		t.Fatal("the page image was never fetched")
	}
}

// TestStage5PageImageFetchCarriesCookieJar is the same mutation (a) proof for
// theme.CookieUser: without going through theme.PolicyFor, a theme that opts
// into cookies never gets a jar on the policy stage 5 fetches the page image
// with.
func TestStage5PageImageFetchCarriesCookieJar(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	th := cookieStubTheme{stubTheme: stubTheme{id: "alpha", score: 90}, use: true}
	res := runWithFetcher(t, f, th)
	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
	}

	var found bool
	for _, c := range f.Calls() {
		if c.URL != "https://example.invalid/p/1.jpg" {
			continue
		}
		found = true
		if c.Policy == nil || c.Policy.Cookies == nil {
			t.Errorf("page image request policy = %+v, want a cookie jar", c.Policy)
		}
	}
	if !found {
		t.Fatal("the page image was never fetched")
	}
}
