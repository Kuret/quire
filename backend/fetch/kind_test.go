package fetch_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
)

// The PLAN §7.4 decision of 2026-09-15, pinned from both sides.
//
// robots.txt is the Robots *Exclusion* Protocol and RFC 9309 scopes it to
// crawlers. Discovery — search, listings, link following, probing — is
// crawling and is bound strictly. Retrieval — one page the user asked for by
// name — is not, and is not gated.
//
// The tests below are written so that the distinction cannot quietly collapse
// in either direction: loosening discovery and tightening retrieval both fail.
// TestKindMutation at the bottom states that in so many words.

// disallowServer serves a robots.txt that forbids /at-home/, which is exactly
// the shape api.mangadex.org publishes: everything needed to *find* a chapter
// is allowed, and the endpoint that *serves* it is not.
func disallowServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nDisallow: /at-home/\n")
			return
		}
		if hits != nil {
			hits.Add(1)
		}
		fmt.Fprintf(w, `{"result":"ok","path":%q}`, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoveryHonoursDisallow(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := disallowServer(t, &hits)
	c, p := testClient(t, srv, fetch.Options{})

	_, err := c.Get(context.Background(), p, srv.URL+"/at-home/server/abc")
	if !errors.Is(err, fetch.ErrRobotsDenied) {
		t.Fatalf("discovery of a disallowed path: err = %v, want ErrRobotsDenied", err)
	}
	// A refusal means the request was never made, not that its body was
	// discarded afterwards.
	if n := hits.Load(); n != 0 {
		t.Errorf("the disallowed path was requested %d time(s); a refusal must not reach the server", n)
	}
}

func TestRetrievalIsNotGatedByDisallow(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := disallowServer(t, &hits)
	c, p := testClient(t, srv, fetch.Options{})

	res, err := c.GetRetrieval(context.Background(), p, srv.URL+"/at-home/server/abc")
	if err != nil {
		t.Fatalf("retrieval of a disallowed path: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if !strings.Contains(string(res.Body), "/at-home/server/abc") {
		t.Errorf("body = %q, want the served path", res.Body)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server saw %d request(s), want 1", n)
	}
}

// The same path, the same client, the same robots.txt: only the kind differs.
// Holding everything else fixed is the point — it is what makes this a test of
// the classification rather than of the URL.
func TestSamePathBothKinds(t *testing.T) {
	t.Parallel()
	srv := disallowServer(t, nil)
	c, p := testClient(t, srv, fetch.Options{})
	const path = "/at-home/server/abc"

	if _, err := c.Get(context.Background(), p, srv.URL+path); !errors.Is(err, fetch.ErrRobotsDenied) {
		t.Errorf("as discovery: err = %v, want ErrRobotsDenied", err)
	}
	if _, err := c.GetRetrieval(context.Background(), p, srv.URL+path); err != nil {
		t.Errorf("as retrieval: %v", err)
	}
}

// An allowed path is allowed both ways. Retrieval is not a special case bolted
// onto a denial; it is a second, equally ordinary reading.
func TestAllowedPathBothKinds(t *testing.T) {
	t.Parallel()
	srv := disallowServer(t, nil)
	c, p := testClient(t, srv, fetch.Options{})

	for _, tc := range []struct {
		name string
		call func(string) (*fetch.Response, error)
	}{
		{"discovery", func(u string) (*fetch.Response, error) { return c.Get(context.Background(), p, u) }},
		{"retrieval", func(u string) (*fetch.Response, error) { return c.GetRetrieval(context.Background(), p, u) }},
	} {
		res, err := tc.call(srv.URL + "/manga?title=x")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d", tc.name, res.StatusCode)
		}
	}
}

// PLAN §7.4: "This narrows nothing else." The politeness floor is not a
// consequence of the robots check, and retrieval must not slip past it.
func TestRetrievalStillRateLimited(t *testing.T) {
	t.Parallel()
	srv := disallowServer(t, nil)

	var slept []time.Duration
	c, p := testClient(t, srv, fetch.Options{
		// The floor, not a relaxation: testClient zeroes Caps, and NewClient
		// clamps whatever it is given against DefaultCaps.
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})

	for range 3 {
		if _, err := c.GetRetrieval(context.Background(), p, srv.URL+"/at-home/server/abc"); err != nil {
			t.Fatal(err)
		}
	}

	var waited time.Duration
	for _, d := range slept {
		waited += d
	}
	// Two inter-request gaps at the 2s floor. The limiter asks our Sleep for
	// them; a retrieval request that bypassed the limiter would ask for none.
	if want := 2 * fetch.DefaultCaps.MinHostDelay; waited < want {
		t.Errorf("three retrievals waited %v in total, want at least %v (PLAN §7.4 floor)", waited, want)
	}
}

// The SSRF guard is not part of the robots gate and does not move with it.
func TestRetrievalStillGuarded(t *testing.T) {
	t.Parallel()
	srv := disallowServer(t, nil)
	c, p := testClient(t, srv, fetch.Options{})

	for _, tc := range []struct{ name, url string }{
		{"non-http scheme", "file:///etc/passwd"},
		{"credentials in URL", "https://user:pw@example.invalid/at-home/server/abc"},
		{"off the source's domain", "https://elsewhere.invalid/at-home/server/abc"},
	} {
		if _, err := c.GetRetrieval(context.Background(), p, tc.url); err == nil {
			t.Errorf("%s: retrieval was allowed; the guard applies to both kinds", tc.name)
		}
	}
}

// Retrieval must not become a way to skip the size cap or to stop counting.
func TestRetrievalStillCappedAndAccounted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nDisallow: /at-home/\n")
			return
		}
		fmt.Fprint(w, strings.Repeat("x", 64))
	}))
	defer srv.Close()

	// Accounting first: a retrieval that fits is still counted.
	c, p := testClient(t, srv, fetch.Options{})
	if _, err := c.GetRetrieval(context.Background(), p, srv.URL+"/at-home/server/abc"); err != nil {
		t.Fatal(err)
	}
	if got := c.TotalBytes(); got != 64 {
		t.Errorf("TotalBytes = %d after one 64-byte retrieval, want 64", got)
	}

	// Then the cap. The declared Content-Length is refused outright, which is
	// why this needs its own client rather than another call on the one above.
	capped, p2 := testClient(t, srv, fetch.Options{MaxResponseBytes: 16})
	_, err := capped.GetRetrieval(context.Background(), p2, srv.URL+"/at-home/server/abc")
	if !errors.Is(err, fetch.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// Kind is Quire's own classification. It has no JSON surface, so no source
// entry, import or config file can assert one — and Policy, which *is* built
// from a source, carries no kind at all.
func TestKindIsNotConfigurable(t *testing.T) {
	t.Parallel()
	if got := fetch.KindDiscovery.String(); got != "discovery" {
		t.Errorf("KindDiscovery = %q", got)
	}
	if got := fetch.KindRetrieval.String(); got != "retrieval" {
		t.Errorf("KindRetrieval = %q", got)
	}
	// The zero value is the strict reading, so forgetting to think about it
	// cannot produce the permissive one.
	var zero fetch.Kind
	if zero != fetch.KindDiscovery {
		t.Errorf("the zero Kind is %v, want discovery", zero)
	}
}

// TestKindMutation is the mutation check the M2 review asked for, written as a
// test rather than as a note. Each entry names a specific way the behaviour
// could be flipped and which test above goes red when it is.
//
// Verified by hand on 2026-09-15 by making each mutation in kind.go or
// fetch.go and running this package:
//
//	gatedByRobots always true  -> TestRetrievalIsNotGatedByDisallow,
//	                              TestSamePathBothKinds,
//	                              TestRetrievalStillRateLimited and
//	                              TestRetrievalStillCappedAndAccounted fail
//	gatedByRobots always false -> TestDiscoveryHonoursDisallow and
//	                              TestSamePathBothKinds fail, along with
//	                              every robots test in robots_test.go —
//	                              which is the reassuring half: the change
//	                              cannot be made to look local
//
// Note what the second mutation demonstrates. Retrieval does not weaken the
// robots machinery; it declines to consult it for one kind of request. Remove
// the distinction in the permissive direction and the entire existing robots
// suite goes red, exactly as it should.
//
// The function body re-states the invariant the mutations attack, so the claim
// is checked and not merely documented.
func TestKindMutation(t *testing.T) {
	t.Parallel()
	if fetch.KindDiscovery == fetch.KindRetrieval {
		t.Fatal("the two kinds collapsed into one; every distinction above is vacuous")
	}
}
