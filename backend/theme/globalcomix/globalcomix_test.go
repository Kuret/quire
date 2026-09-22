package globalcomix_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/globalcomix"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Offline, like every other theme's tests (PLAN §6 M2): committed synthetic
// fixtures, the themetest fetcher, and DNS disabled for the whole package.
//
// Read docs/THEME-NOTES.md, "What the fixtures do and do not prove", and this
// package's own doc comment before trusting a green run here for more than it
// is worth: these fixtures pin *our parsing* of a documented request/response
// shape. They say nothing about whether Quire can complete a live request
// against the real API, because the fixture fetcher never checks whether a
// theme sent the header the real API demands — nothing here can send it yet.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

var fixedNow = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixedNow }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-07", Name: "GlobalComix", Lang: "en",
		Theme: globalcomix.ID, BaseURL: "https://api.example.invalid",
		AddedAt: fixedNow,
	}
}

func TestSearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "lighthouse", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []theme.SeriesStub{
		{
			ID:       "/comics/90001",
			Title:    "The Lighthouse Keeper's Almanac",
			CoverURL: "https://covers.example.invalid/90001/small.webp",
			Authors:  []string{"Riverside Studio"},
		},
		{
			// Empty artist_name must come back as no authors, not [""].
			ID:       "/comics/90002",
			Title:    "Salt and Cedar",
			CoverURL: "https://covers.example.invalid/90002/small.webp",
		},
		// The id-0 entry is skipped entirely.
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Title != want[i].Title || got[i].CoverURL != want[i].CoverURL {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
		if len(got[i].Authors) != len(want[i].Authors) {
			t.Errorf("result %d: authors = %v, want %v", i, got[i].Authors, want[i].Authors)
		}
	}
}

func TestSearchSendsPagingAndContext(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)
	if _, err := th.Search(context.Background(), site(), "lighthouse", 3); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q.Get("context"); got != "series" {
		t.Errorf("context = %q, want %q", got, "series")
	}
	if got := q.Get("p"); got != "3" {
		t.Errorf("p = %q, want %q", got, "3")
	}
	if got := q.Get("q"); got != "lighthouse" {
		t.Errorf("q = %q, want %q", got, "lighthouse")
	}
}

// PLAN §12.1: search with an empty query is browse. An empty q must not be
// sent as an empty parameter, which the API could read as "match nothing" —
// it must simply be absent.
func TestSearchEmptyQueryOmitsParam(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)
	if _, err := th.Search(context.Background(), site(), "", 1); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if _, ok := q["q"]; ok {
		t.Errorf("q param present for an empty search, want absent: %v", q)
	}
}

func TestSeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/comics/90001": {File: "series.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), "/comics/90001")
	if err != nil {
		t.Fatal(err)
	}
	want := &theme.Series{
		ID:          "/comics/90001",
		Title:       "The Lighthouse Keeper's Almanac",
		CoverURL:    "https://covers.example.invalid/90001/large.webp",
		Description: "A keeper of a remote lighthouse records what the tide brings in.",
		Authors:     []string{"Riverside Studio"},
		Artists:     []string{"Riverside Studio"},
		Genres:      []string{"Drama"},
		// "Hiatus" mixed case must still map to the normalised constant.
		Status: theme.StatusHiatus,
	}
	if got.ID != want.ID || got.Title != want.Title || got.CoverURL != want.CoverURL ||
		got.Description != want.Description || got.Status != want.Status {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestSeriesRejectsForeignID(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := globalcomix.NewWithClock(f, clock)
	if _, err := th.Series(context.Background(), site(), "not-an-id"); err == nil {
		t.Error("want an error for an ID this theme never produced")
	}
}

func TestChapters(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/comics/90001/releases": {File: "releases.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), "/comics/90001")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d chapters, want 3 (the empty-key release must be skipped): %+v", len(got), got)
	}
	if !theme.OrderIsKnown(got) {
		t.Fatal("order should be known: every release carries a number")
	}
}

// The fixture lists releases newest-first, on purpose (see releases.json).
// This is the one thing this test exists to catch: deleting the
// theme.SortAndMark call in Chapters must fail it.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/comics/90001/releases": {File: "releases.json"},
	})
	th := globalcomix.NewWithClock(f, clock)
	got, err := th.Chapters(context.Background(), site(), "/comics/90001")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Number <= got[i-1].Number {
			t.Fatalf("chapter %d (number %v) does not come after chapter %d (number %v): not ascending",
				i, got[i].Number, i-1, got[i-1].Number)
		}
	}
	// The middle release (order 2) has an empty title, so its number must
	// still come from the bare "chapter" field.
	if got[1].Number != 2 {
		t.Errorf("chapter with empty title: Number = %v, want 2", got[1].Number)
	}
	if got[1].Title != "Chapter 2" {
		t.Errorf("chapter with empty title: Title = %q, want %q", got[1].Title, "Chapter 2")
	}
}

func TestPages(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/readV3/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01": {File: "readv3.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/releases/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://reader-cdn.example.invalid/r/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01/p/1/desktop.webp",
		"https://reader-cdn.example.invalid/r/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01/p/2/desktop.webp",
		"https://reader-cdn.example.invalid/r/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01/p/3/desktop.webp",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Pages() is the retrieval call (PLAN §7.4): the user opened this chapter.
// Search, Series and Chapters must stay discovery.
func TestPagesIsFetchedAsRetrieval(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/readV3/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01": {File: "readv3.json"},
	})
	th := globalcomix.NewWithClock(f, clock)
	if _, err := th.Pages(context.Background(), site(), "/releases/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Kind != fetch.KindRetrieval {
		t.Fatalf("Pages() call kind = %+v, want exactly one KindRetrieval call", calls)
	}
}

func TestDiscoveryCallsAreNotRetrieval(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query":          {File: "search.json"},
		"GET /v1/comics/90001":          {File: "series.json"},
		"GET /v1/comics/90001/releases": {File: "releases.json"},
	})
	th := globalcomix.NewWithClock(f, clock)
	if _, err := th.Search(context.Background(), site(), "x", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Series(context.Background(), site(), "/comics/90001"); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Chapters(context.Background(), site(), "/comics/90001"); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Calls() {
		if c.Kind != fetch.KindDiscovery {
			t.Errorf("call %s got kind %v, want KindDiscovery", c.URL, c.Kind)
		}
	}
}

func TestQualityOverride(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/readV3/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01": {File: "readv3.json"},
	})
	th := globalcomix.NewWithClock(f, clock)
	s := site()
	s.Overrides = map[string]any{globalcomix.KeyPageQuality: "mobile"}

	got, err := th.Pages(context.Background(), s, "/releases/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01")
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "https://reader-cdn.example.invalid/r/aaaaaaaa-1111-4a11-8a11-aaaaaaaaaa01/p/1/mobile.webp" {
		t.Errorf("got %q, want the mobile rendition", got[0])
	}
}

// TestValidateAcceptsSiteDomain: a baseUrl naming the browser-facing site
// (rather than the API host) is accepted, not rejected. That used to be an
// error, but endpoint() (see normaliseAPIHost) now rewrites every request to
// the API host regardless of which of the two a source names, so the stored
// host is a cosmetic difference, not a functional one — rejecting it here
// would only reproduce the contradiction of a source that probes as
// "addable" and then fails to be added.
func TestValidateAcceptsSiteDomain(t *testing.T) {
	th := globalcomix.New(nil)
	s := &theme.Source{ID: "x", Theme: globalcomix.ID, BaseURL: "https://globalcomix.com"}
	if err := th.Validate(s); err != nil {
		t.Errorf("want no error for the site domain, since requests are normalised to the API host anyway: %v", err)
	}
}

// TestValidateRejectsForeignHost confirms the part of the old rule that is
// still real: a host outside globalcomix.com's registrable domain entirely
// (not merely "on it but not the API host") must still be rejected, since
// normaliseAPIHost never rewrites such a host and nothing else in this theme
// would stop it from being used as-is.
func TestValidateRejectsForeignHost(t *testing.T) {
	th := globalcomix.New(nil)
	s := &theme.Source{ID: "x", Theme: globalcomix.ID, BaseURL: "https://not-globalcomix.example.com"}
	if err := th.Validate(s); err == nil {
		t.Error("want an error: this host is not on globalcomix.com's registrable domain")
	}
}

func TestValidateAcceptsAPIHost(t *testing.T) {
	th := globalcomix.New(nil)
	s := &theme.Source{ID: "x", Theme: globalcomix.ID, BaseURL: "https://api.globalcomix.com"}
	if err := th.Validate(s); err != nil {
		t.Errorf("want no error for the real API host: %v", err)
	}
}

// TestEndpointNormalisesSiteDomainToAPIHost is the fix for pasting the site's
// own URL: a source whose baseUrl is the browser-facing domain still reaches
// the real API host, because endpoint() rewrites it before building the
// request — see normaliseAPIHost's doc comment.
func TestEndpointNormalisesSiteDomainToAPIHost(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	s := &theme.Source{ID: "x", Theme: globalcomix.ID, BaseURL: "https://globalcomix.com"}
	if _, err := th.Search(context.Background(), s, "lighthouse", 1); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	u, err := url.Parse(calls[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "api.globalcomix.com" {
		t.Errorf("request went to host %q, want api.globalcomix.com", u.Hostname())
	}
}

// TestEndpointLeavesOfflineTestHostAlone guards the offline-test rationale
// endpoint's doc comment gives: a base on a domain that is not
// globalcomix.com at all — in particular the .invalid host PLAN §6 M2's
// fixtures use everywhere else in this file — must never be rewritten, or
// every other test in this package would start requesting a host with no
// fixture behind it.
func TestEndpointLeavesOfflineTestHostAlone(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /v1/search/query": {File: "search.json"},
	})
	th := globalcomix.NewWithClock(f, clock)

	if _, err := th.Search(context.Background(), site(), "lighthouse", 1); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	u, err := url.Parse(calls[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "api.example.invalid" {
		t.Errorf("request went to host %q, want the untouched offline-test host api.example.invalid", u.Hostname())
	}
}

func TestFingerprintGatesOnRegistrableDomain(t *testing.T) {
	th := globalcomix.New(nil)
	u, _ := url.Parse("https://totally-unrelated.example.invalid/")
	p := probe.NewPage(u, u, 200, http.Header{}, []byte(`window.gc.global com.globalcomix.mobileapp assets.globalcomix.com`))
	if got := th.Fingerprint(p); got != 0 {
		t.Errorf("Fingerprint on a foreign domain = %d, want 0 even though the body contains every other signal", got)
	}
}

func TestFingerprintScoresTheRealHomePageShape(t *testing.T) {
	th := globalcomix.New(nil)
	u, _ := url.Parse("https://globalcomix.com/")
	body := []byte(`<html><head><title>GlobalComix - Read &amp; Publish Comics &amp; Manga Online Free</title>
<meta name="google-play-app" content="app-id=com.globalcomix.mobileapp">
</head><body><script>window.gc.global = {"api_url":"https://api.globalcomix.com","assets_url":"https://assets.globalcomix.com"};</script></body></html>`)
	p := probe.NewPage(u, u, 200, http.Header{}, body)
	got := th.Fingerprint(p)
	if got < 60 {
		t.Errorf("Fingerprint on the real home-page shape = %d, want >= the probe threshold of 60", got)
	}
}

func TestAllowedHostsIsNil(t *testing.T) {
	th := globalcomix.New(nil)
	if got := th.AllowedHosts(); got != nil {
		t.Errorf("AllowedHosts() = %v, want nil: every URL this theme produces is under globalcomix.com", got)
	}
}

// TestSourceHeadersSendsClientHeader confirms theme.PolicyFor picks up
// Theme's SourceHeaders implementation: the client-identifier header the real
// API requires on every call (CLIENT_HEADER_MISSING otherwise).
func TestSourceHeadersSendsClientHeader(t *testing.T) {
	th := globalcomix.New(nil)
	s := site()
	pol, err := theme.PolicyFor(th, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := pol.Headers.Get("X-Gc-Client"); got == "" {
		t.Error("Policy.Headers carries no X-Gc-Client header; every real request would be refused with CLIENT_HEADER_MISSING")
	}
}

// TestSourceHeadersOverride confirms a source's clientKey override replaces
// the built-in default, for the day the real value rotates.
func TestSourceHeadersOverride(t *testing.T) {
	th := globalcomix.New(nil)
	s := site()
	s.Overrides = map[string]any{"clientKey": "gck_replacement"}
	pol, err := theme.PolicyFor(th, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pol.Headers.Get("X-Gc-Client"), "gck_replacement"; got != want {
		t.Errorf("X-Gc-Client = %q, want the override %q", got, want)
	}
}

// TestUsesCookiesGivesTheSourceAJar confirms theme.PolicyFor picks up Theme's
// CookieUser implementation: without it, the reader-CDN's cookie requirement
// (see readerCDNAccess in api.go) can never be met, since nothing else in
// this package can carry a Set-Cookie into a later request.
func TestUsesCookiesGivesTheSourceAJar(t *testing.T) {
	th := globalcomix.New(nil)
	s := site()
	pol, err := theme.PolicyFor(th, s)
	if err != nil {
		t.Fatal(err)
	}
	if pol.Cookies == nil {
		t.Error("Policy.Cookies is nil; the reading-grant cookie readV3 sets could never reach a later page-image request")
	}
}

// TestPagesAndSearchShareOneCookieJar confirms every request this theme makes
// for one source shares the same jar — not a fresh one per call — since a
// jar recreated per request would never carry readV3's Set-Cookie forward at
// all.
func TestPagesAndSearchShareOneCookieJar(t *testing.T) {
	th := globalcomix.New(nil)
	s := site()
	p1, err := theme.PolicyFor(th, s)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := theme.PolicyFor(th, s)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Cookies != p2.Cookies {
		t.Error("two Policy values for the same source carry different cookie jars, want the same one")
	}
}

func lastQuery(t *testing.T, f *themetest.Fetcher) url.Values {
	t.Helper()
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("no requests recorded")
	}
	u, err := url.Parse(calls[len(calls)-1].URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}
