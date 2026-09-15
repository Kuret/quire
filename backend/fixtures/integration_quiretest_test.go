//go:build quiretest

// The end-to-end test: the real fetch client, a real socket, and a real theme.
//
// It needs the `quiretest` build tag because the SSRF guard rejects loopback
// and this server is on 127.0.0.1. The exemption is granted by
// fetch.AllowLoopbackForTests, which only exists under that tag — see
// backend/fetch/loopback_quiretest.go for why that is the shape it takes, and
// why no configuration file can reach it.
//
//	make test            runs these (it passes -tags quiretest)
//	go test ./...        compiles the production shape and skips them
//
// Everything below runs against committed synthetic fixtures on loopback.
// Nothing here goes online.

package fixtures_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/fixtures"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/mangadex"
)

const (
	mangaUUID   = "11111111-2222-4333-8444-555555555555"
	chapterUUID = "aaaaaaaa-1111-4111-8111-111111111111"
)

// mangadexRoutes serves the mangadex theme's own committed fixtures at the
// paths the real API uses.
func mangadexRoutes() map[string]fixtures.Route {
	return map[string]fixtures.Route{
		"GET /manga":              {File: "search.json"},
		"GET /manga/" + mangaUUID: {File: "series.json"},
		"GET /manga/" + mangaUUID + "/feed?offset=0": {File: "feed.json"},
		"GET /manga/" + mangaUUID + "/feed?offset=4": {File: "feed-page-2.json"},
		"GET /at-home/server/" + chapterUUID:         {File: "at-home.json"},
	}
}

// start brings up the server and a client pointed at it.
//
// The client is the production one. Only two things are injected: the loopback
// exemption, and Sleep, so the politeness delays are observed rather than
// waited out — a test that actually slept the §7.4 floor would take minutes.
func start(t *testing.T, opts fixtures.Options) (*fixtures.Server, *fetch.Client, *fetch.Policy, *[]time.Duration) {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = "../theme/mangadex/testdata"
	}
	srv := fixtures.Start(opts)
	t.Cleanup(srv.Close)

	base, err := url.Parse(srv.URL())
	if err != nil {
		t.Fatal(err)
	}
	var slept []time.Duration
	c := fetch.NewClient(fetch.AllowLoopbackForTests(fetch.Options{
		Version: "integration",
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	}))
	return srv, c, &fetch.Policy{BaseURL: base}, &slept
}

// fetcher adapts a fetch.Client to theme.Fetcher with a fixed policy. The
// production path builds the policy from the source; here the source's base
// URL is an ephemeral port, so it is built once and reused.
type fetcher struct {
	c *fetch.Client
	p *fetch.Policy
}

func (f fetcher) Get(ctx context.Context, _ *fetch.Policy, u string) (*fetch.Response, error) {
	return f.c.Get(ctx, f.p, u)
}

func (f fetcher) GetRetrieval(ctx context.Context, _ *fetch.Policy, u string) (*fetch.Response, error) {
	return f.c.GetRetrieval(ctx, f.p, u)
}

func (f fetcher) PostForm(ctx context.Context, _ *fetch.Policy, u string, form url.Values) (*fetch.Response, error) {
	return f.c.PostForm(ctx, f.p, u, form)
}

// The whole path, over a socket, against MangaDex's actual robots.txt.
//
// This is the case PLAN §7.4 was decided for, and until there was a server it
// could only be argued about. /manga and the feeds are allowed and fetched as
// discovery; /at-home/ is disallowed and fetched as retrieval; all four stage-5
// steps have to produce plausible non-empty results.
func TestMangaDexEndToEnd(t *testing.T) {
	srv, c, p, _ := start(t, fixtures.Options{
		Routes: mangadexRoutes(),
		// Exactly what api.mangadex.org publishes, confirmed 2026-09-15.
		Robots: "User-agent: *\nDisallow: /at-home/\n",
	})

	s := &theme.Source{
		ID: "integration", Name: "Fixture MangaDex", Lang: "en",
		Theme: mangadex.ID, BaseURL: srv.URL(), AddedAt: time.Now(),
	}
	th := mangadex.New(fetcher{c, p})
	ctx := context.Background()

	stubs, err := th.Search(ctx, s, "tower", 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(stubs) == 0 {
		t.Fatal("search returned nothing")
	}

	ser, err := th.Series(ctx, s, stubs[0].ID)
	if err != nil {
		t.Fatalf("series: %v", err)
	}
	if ser.Title == "" {
		t.Error("series has no title")
	}

	chs, err := th.Chapters(ctx, s, stubs[0].ID)
	if err != nil {
		t.Fatalf("chapters: %v", err)
	}
	if len(chs) == 0 {
		t.Fatal("chapters returned nothing")
	}

	pages, err := th.Pages(ctx, s, chs[0].ID)
	if err != nil {
		t.Fatalf("pages: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("pages returned nothing")
	}

	// robots.txt was really fetched — not stubbed — and cached, so a browse
	// session costs one request for it rather than one per page.
	if n := srv.Hits("/robots.txt"); n != 1 {
		t.Errorf("robots.txt fetched %d times, want 1", n)
	}
	// The disallowed endpoint was reached, because it was asked for as
	// retrieval. This is the assertion the whole §7.4 decision comes down to.
	if n := srv.Hits("/at-home/server/" + chapterUUID); n != 1 {
		t.Errorf("/at-home/ was requested %d times, want 1", n)
	}
	// Both feed pages: paging works over a socket too.
	if n := srv.Hits("/manga/" + mangaUUID + "/feed"); n != 2 {
		t.Errorf("feed requested %d times, want 2", n)
	}
	if c.TotalBytes() == 0 {
		t.Error("nothing was accounted for")
	}
	// The honest User-Agent reached the server as sent.
	for _, r := range srv.Requests() {
		if !strings.HasPrefix(r.UserAgent, "Quire/") {
			t.Errorf("%s %s sent User-Agent %q", r.Method, r.URI, r.UserAgent)
		}
	}
}

// The other half: the same path as discovery is refused, and never reaches the
// server. A robots gate that let the request out and discarded the answer
// would pass an assertion about the error and fail this one.
func TestDisallowedPathAsDiscoveryNeverReachesTheServer(t *testing.T) {
	srv, c, p, _ := start(t, fixtures.Options{
		Routes: mangadexRoutes(),
		Robots: "User-agent: *\nDisallow: /at-home/\n",
	})

	path := "/at-home/server/" + chapterUUID
	_, err := c.Get(context.Background(), p, srv.URL()+path)
	if !errors.Is(err, fetch.ErrRobotsDenied) {
		t.Fatalf("err = %v, want ErrRobotsDenied", err)
	}
	if n := srv.Hits(path); n != 0 {
		t.Errorf("the disallowed path was requested %d times", n)
	}
}

// PLAN §7.4: Retry-After honoured. The route refuses twice with a 429 and a
// Retry-After of 2s, then answers.
func TestRetryAfterOverASocket(t *testing.T) {
	srv, c, p, slept := start(t, fixtures.Options{
		Routes: map[string]fixtures.Route{
			"GET /flaky": {
				Body:       "finally",
				FailTimes:  2,
				FailStatus: http.StatusTooManyRequests,
				RetryAfter: "2",
			},
		},
	})

	res, err := c.Get(context.Background(), p, srv.URL()+"/flaky")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || string(res.Body) != "finally" {
		t.Fatalf("status = %d body = %q", res.StatusCode, res.Body)
	}
	if res.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", res.Attempts)
	}
	// Two Retry-After waits of exactly 2s, plus whatever the limiter asked for
	// between requests to the same host. The header is honoured verbatim, so
	// the two exact values have to be in there.
	var exact int
	for _, d := range *slept {
		if d == 2*time.Second {
			exact++
		}
	}
	if exact < 2 {
		t.Errorf("slept %v; want two waits of exactly 2s from Retry-After", *slept)
	}
}

// A 5xx is retried with backoff rather than reported on the first try.
func TestServerErrorIsRetried(t *testing.T) {
	srv, c, p, _ := start(t, fixtures.Options{
		Routes: map[string]fixtures.Route{
			"GET /wobbly": {Body: "recovered", FailTimes: 1, FailStatus: http.StatusBadGateway},
		},
	})

	res, err := c.Get(context.Background(), p, srv.URL()+"/wobbly")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != "recovered" || res.Attempts != 2 {
		t.Errorf("body = %q attempts = %d, want \"recovered\" in 2", res.Body, res.Attempts)
	}
}

// The response cap, in both the forms a server can produce it.
func TestResponseCapOverASocket(t *testing.T) {
	srv, _, p, _ := start(t, fixtures.Options{})

	for _, tc := range []struct{ name, path string }{
		{"declared Content-Length", "/sim/large?bytes=200000"},
		{"undeclared, caught while reading", "/sim/large?bytes=200000&chunked=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capped := fetch.NewClient(fetch.AllowLoopbackForTests(fetch.Options{
				Version:          "integration",
				MaxResponseBytes: 8 << 10,
				Sleep:            func(context.Context, time.Duration) error { return nil },
			}))
			_, err := capped.Get(context.Background(), p, srv.URL()+tc.path)
			if !errors.Is(err, fetch.ErrTooLarge) {
				t.Fatalf("err = %v, want ErrTooLarge", err)
			}
		})
	}
}

// Byte accounting counts what actually crossed the socket.
func TestByteAccountingOverASocket(t *testing.T) {
	srv, c, p, _ := start(t, fixtures.Options{})
	if _, err := c.Get(context.Background(), p, srv.URL()+"/sim/large?bytes=4096"); err != nil {
		t.Fatal(err)
	}
	// 4096 for the body; robots.txt is empty here and adds nothing.
	if got := c.TotalBytes(); got != 4096 {
		t.Errorf("TotalBytes = %d, want 4096", got)
	}
}

// A redirect chain inside the source's own domain is followed, and FinalURL
// reports where it ended — which is what PLAN §7.5 stage 2 shows the user.
func TestRedirectChainIsFollowed(t *testing.T) {
	srv, c, p, _ := start(t, fixtures.Options{})
	res, err := c.Get(context.Background(), p, srv.URL()+"/sim/redirect?n=3")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != "ok" {
		t.Fatalf("body = %q", res.Body)
	}
	if res.FinalURL == nil || res.FinalURL.Path != "/sim/ok" {
		t.Errorf("FinalURL = %v, want /sim/ok", res.FinalURL)
	}
	if n := srv.Hits("/sim/redirect"); n != 3 {
		t.Errorf("redirect hit %d times, want 3", n)
	}
}

// The SSRF guard runs on a hop the *server* chose, not only on the URL Quire
// started with. This is the one that cannot be tested without a socket: it
// needs the Go client to genuinely decide to follow a redirect.
func TestRedirectLeavingTheDomainIsRefused(t *testing.T) {
	srv, c, p, _ := start(t, fixtures.Options{})

	target := "https://elsewhere.invalid/landing"
	_, err := c.Get(context.Background(), p, srv.URL()+"/sim/redirect?to="+url.QueryEscape(target))
	if err == nil {
		t.Fatal("a redirect off the source's registrable domain was followed")
	}
	if !errors.Is(err, fetch.ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
	if !strings.Contains(err.Error(), "allowedHosts") {
		t.Errorf("err = %v; it should say how to permit the host deliberately", err)
	}
}

// A slow response still completes. This is less about the assertion than about
// having somewhere to reproduce "the site is slow and the UI looks hung".
func TestSlowResponse(t *testing.T) {
	srv, c, p, _ := start(t, fixtures.Options{})
	start := time.Now()
	res, err := c.Get(context.Background(), p, srv.URL()+"/sim/slow?ms=150")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != "slow but fine" {
		t.Fatalf("body = %q", res.Body)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("returned in %v, faster than the server took", elapsed)
	}
}

// The politeness floor applies to a real socket, and it applies per host.
func TestRateLimitingOverASocket(t *testing.T) {
	srv, c, p, slept := start(t, fixtures.Options{
		Routes: map[string]fixtures.Route{"GET /page": {Body: "x"}},
	})
	for range 3 {
		if _, err := c.Get(context.Background(), p, srv.URL()+"/page"); err != nil {
			t.Fatal(err)
		}
	}
	var total time.Duration
	for _, d := range *slept {
		total += d
	}
	if want := 2 * fetch.DefaultCaps.MinHostDelay; total < want {
		t.Errorf("three requests waited %v, want at least %v", total, want)
	}
}

// PLAN §7.4's three robots outcomes, over the wire rather than in the parser.
func TestRobotsOutcomesOverASocket(t *testing.T) {
	t.Run("404 allows", func(t *testing.T) {
		srv, c, p, _ := start(t, fixtures.Options{
			Routes:       map[string]fixtures.Route{"GET /page": {Body: "x"}},
			RobotsStatus: http.StatusNotFound,
		})
		if _, err := c.Get(context.Background(), p, srv.URL()+"/page"); err != nil {
			t.Fatalf("a missing robots.txt refused a request: %v", err)
		}
	})

	t.Run("5xx is unknown, not permission", func(t *testing.T) {
		srv, c, p, _ := start(t, fixtures.Options{
			Routes:       map[string]fixtures.Route{"GET /page": {Body: "x"}},
			RobotsStatus: http.StatusInternalServerError,
		})
		_, err := c.Get(context.Background(), p, srv.URL()+"/page")
		if !errors.Is(err, fetch.ErrRobotsUnavailable) {
			t.Fatalf("err = %v, want ErrRobotsUnavailable", err)
		}
		if errors.Is(err, fetch.ErrRobotsDenied) {
			t.Error("an unreadable robots.txt was reported as a refusal that never happened")
		}
	})
}
