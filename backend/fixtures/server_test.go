package fixtures_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fixtures"
)

// These tests cover the server itself, with an ordinary http.Client, and run
// under the plain build. The interesting tests — the real fetch client against
// this server — need the loopback exemption and so live behind the `quiretest`
// tag in integration_quiretest_test.go. A fixture server that quietly served
// the wrong thing would make those tests lie, which is why it gets its own.

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(b)
}

func TestServesRoutesAndRobots(t *testing.T) {
	srv := fixtures.Start(fixtures.Options{
		Robots: "User-agent: *\nDisallow: /at-home/\n",
		Routes: map[string]fixtures.Route{
			"GET /page": {Body: "hello"},
			"/any":      {Body: "any method"},
		},
	})
	defer srv.Close()

	if _, body := get(t, srv.URL()+"/robots.txt"); !strings.Contains(body, "Disallow: /at-home/") {
		t.Errorf("robots.txt = %q", body)
	}
	if _, body := get(t, srv.URL()+"/page"); body != "hello" {
		t.Errorf("/page = %q", body)
	}
	if _, body := get(t, srv.URL()+"/any"); body != "any method" {
		t.Errorf("a method-less route did not match a GET: %q", body)
	}
	res, _ := get(t, srv.URL()+"/nothing-here")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("an unrouted path answered %d; a fixture server must not invent content", res.StatusCode)
	}
	if n := srv.Hits("/page"); n != 1 {
		t.Errorf("Hits(/page) = %d, want 1", n)
	}
}

// The partial-query match is what lets a paginated endpoint be routed without
// transcribing every parameter the caller happens to send.
func TestPartialQueryMatch(t *testing.T) {
	srv := fixtures.Start(fixtures.Options{
		Routes: map[string]fixtures.Route{
			"GET /feed?offset=0": {Body: "page one"},
			"GET /feed?offset=4": {Body: "page two"},
		},
	})
	defer srv.Close()

	if _, body := get(t, srv.URL()+"/feed?limit=500&offset=0&order=asc"); body != "page one" {
		t.Errorf("offset=0 served %q", body)
	}
	if _, body := get(t, srv.URL()+"/feed?limit=500&offset=4&order=asc"); body != "page two" {
		t.Errorf("offset=4 served %q", body)
	}
	res, _ := get(t, srv.URL()+"/feed?offset=99")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("an offset with no route answered %d, want 404", res.StatusCode)
	}
}

// Fail-then-succeed is the shape of a real rate limit, and the one thing a
// directory of static files cannot express.
func TestFailTimesThenSucceeds(t *testing.T) {
	srv := fixtures.Start(fixtures.Options{
		Routes: map[string]fixtures.Route{
			"GET /flaky": {Body: "ok now", FailTimes: 2, FailStatus: http.StatusTooManyRequests, RetryAfter: "7"},
		},
	})
	defer srv.Close()

	for i := range 2 {
		res, _ := get(t, srv.URL()+"/flaky")
		if res.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("attempt %d: status = %d, want 429", i+1, res.StatusCode)
		}
		if got := res.Header.Get("Retry-After"); got != "7" {
			t.Errorf("attempt %d: Retry-After = %q, want 7", i+1, got)
		}
	}
	res, body := get(t, srv.URL()+"/flaky")
	if res.StatusCode != http.StatusOK || body != "ok now" {
		t.Errorf("third attempt: %d %q", res.StatusCode, body)
	}
}

func TestSimulations(t *testing.T) {
	srv := fixtures.Start(fixtures.Options{})
	defer srv.Close()

	t.Run("status with Retry-After", func(t *testing.T) {
		res, _ := get(t, srv.URL()+"/sim/status/503?retryAfter=30")
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d", res.StatusCode)
		}
		if got := res.Header.Get("Retry-After"); got != "30" {
			t.Errorf("Retry-After = %q", got)
		}
	})

	t.Run("large with a declared length", func(t *testing.T) {
		res, body := get(t, srv.URL()+"/sim/large?bytes=9000")
		if len(body) != 9000 {
			t.Errorf("body is %d bytes, want 9000", len(body))
		}
		if got := res.Header.Get("Content-Length"); got != strconv.Itoa(9000) {
			t.Errorf("Content-Length = %q, want 9000", got)
		}
	})

	t.Run("large without one", func(t *testing.T) {
		res, body := get(t, srv.URL()+"/sim/large?bytes=9000&chunked=1")
		if len(body) != 9000 {
			t.Errorf("body is %d bytes, want 9000", len(body))
		}
		// The cap has to catch this one while reading, which is the case
		// worth having: a server that declares nothing.
		if got := res.Header.Get("Content-Length"); got != "" {
			t.Errorf("Content-Length = %q, want none", got)
		}
	})

	t.Run("slow", func(t *testing.T) {
		start := time.Now()
		_, body := get(t, srv.URL()+"/sim/slow?ms=120")
		if body != "slow but fine" {
			t.Errorf("body = %q", body)
		}
		if d := time.Since(start); d < 120*time.Millisecond {
			t.Errorf("answered in %v, faster than it was told to be", d)
		}
	})

	t.Run("redirect chain", func(t *testing.T) {
		_, body := get(t, srv.URL()+"/sim/redirect?n=3")
		if body != "ok" {
			t.Errorf("chain ended at %q", body)
		}
		if n := srv.Hits("/sim/redirect"); n != 3 {
			t.Errorf("chain was %d hops, want 3", n)
		}
	})
}

// A fixture server exists to serve a directory of test data. A route that
// climbs out of it is the one interesting way it could be misused, so it is
// refused rather than merely unlikely.
func TestFixturePathCannotEscapeTheDirectory(t *testing.T) {
	srv := fixtures.Start(fixtures.Options{
		Dir: "../theme/mangadex/testdata",
		Routes: map[string]fixtures.Route{
			"GET /ok":     {File: "search.json"},
			"GET /escape": {File: "../../../../etc/passwd"},
		},
	})
	defer srv.Close()

	if res, _ := get(t, srv.URL()+"/ok"); res.StatusCode != http.StatusOK {
		t.Fatalf("a normal fixture answered %d", res.StatusCode)
	}
	res, body := get(t, srv.URL()+"/escape")
	if res.StatusCode == http.StatusOK {
		t.Fatalf("a path outside the fixture directory was served: %q", body[:min(80, len(body))])
	}
}
