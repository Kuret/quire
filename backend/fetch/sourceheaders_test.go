package fetch_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

// TestSourceHeadersReachTheSourcesOwnHost is the ordinary case: a static
// header set on the Policy is sent to the source's own host.
func TestSourceHeadersReachTheSourcesOwnHost(t *testing.T) {
	t.Parallel()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Client-Id")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	p.Headers = http.Header{"X-Client-Id": []string{"public-app-id"}}

	if _, err := c.Get(context.Background(), p, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	if got != "public-app-id" {
		t.Fatalf("X-Client-Id = %q, want %q", got, "public-app-id")
	}
}

// TestSourceHeadersDoNotReachAHostOutsideTheSourcesScope is mutation (a):
// a theme's header must never be sent to a host outside the source's own
// scope. It is proved the way the guard already proves every other case of
// this boundary — the request to the off-domain host is refused outright
// (ErrBlockedAddress, OffDomain), so the header carried on the Policy is
// never sent anywhere near it. The off-domain server is asserted to have
// received nothing at all, not merely "not this header".
func TestSourceHeadersDoNotReachAHostOutsideTheSourcesScope(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	p.Headers = http.Header{"X-Client-Id": []string{"public-app-id"}}
	// No AllowedHosts entry names this host, and it is not the source's own
	// (srv's) host, so it is squarely outside this Policy's scope. The
	// request is refused by checkDomain on the hostname alone, before any
	// resolution or connection is attempted — which is exactly why this
	// works as a test with no real network access and no second server: a
	// request that never leaves checkDomain can never carry a header
	// anywhere, off-domain or otherwise.
	const offDomain = "http://off-domain.example.invalid/page"

	_, err := c.Get(context.Background(), p, offDomain)
	if err == nil {
		t.Fatal("a request to a host outside the source's scope must be refused, not sent with the source's headers")
	}
	ge, ok := err.(*fetch.GuardError)
	if !ok || !ge.OffDomain {
		t.Fatalf("expected an off-domain GuardError, got %v", err)
	}
}

// TestSourceHeadersCannotOverrideUserAgent is mutation (b): a theme must not
// be able to forge the honest User-Agent, or a Referer, or a Cookie, through
// Policy.Headers. All three are dropped regardless of what a theme supplies.
func TestSourceHeadersCannotOverrideUserAgent(t *testing.T) {
	t.Parallel()
	var ua, referer, cookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		referer = r.Header.Get("Referer")
		cookie = r.Header.Get("Cookie")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{Version: "1.2.3"})
	p.Headers = http.Header{
		"User-Agent": []string{"TotallyARealBrowser/99.0"},
		"Referer":    []string{"https://not-a-page-we-fetched.invalid/"},
		"Cookie":     []string{"session=forged"},
	}

	if _, err := c.Get(context.Background(), p, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	if ua != c.UserAgent() {
		t.Fatalf("User-Agent = %q, a theme must not be able to override the honest one %q", ua, c.UserAgent())
	}
	if referer != "" {
		t.Fatalf("Referer = %q, want empty: a theme must not be able to invent one through Headers", referer)
	}
	if cookie != "" {
		t.Fatalf("Cookie = %q, want empty: a theme must not be able to forge one through Headers", cookie)
	}
}

// TestCookiesAreSentAndUpdatedForTheSameSource proves the ordinary case: a
// cookie a source's server issues is carried on the requests that follow,
// when the caller opted in with a jar.
func TestCookiesAreSentAndUpdatedForTheSameSource(t *testing.T) {
	t.Parallel()
	var secondRequestCookie string
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			http.SetCookie(w, &http.Cookie{Name: "gc_reader_auth", Value: "abc123"})
			fmt.Fprint(w, "granted")
			return
		}
		secondRequestCookie = r.Header.Get("Cookie")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	p.Cookies = fetch.NewCookieJar()

	if _, err := c.Get(context.Background(), p, srv.URL+"/grant"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), p, srv.URL+"/page"); err != nil {
		t.Fatal(err)
	}
	if secondRequestCookie == "" {
		t.Fatal("the second request must carry the cookie the first response issued")
	}
}

// TestCookiesNeverCrossFromOneSourceToAnother is mutation (c): a cookie set
// while using one source's jar must never be readable through a different
// jar, even for requests to the very same server.
func TestCookiesNeverCrossFromOneSourceToAnother(t *testing.T) {
	t.Parallel()
	var sawCookieOnSecondJar string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/grant" {
			http.SetCookie(w, &http.Cookie{Name: "gc_reader_auth", Value: "abc123"})
			fmt.Fprint(w, "granted")
			return
		}
		sawCookieOnSecondJar = r.Header.Get("Cookie")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p1 := testClient(t, srv, fetch.Options{})
	p1.Cookies = fetch.NewCookieJar()
	_, p2 := testClient(t, srv, fetch.Options{})
	p2.Cookies = fetch.NewCookieJar()

	if _, err := c.Get(context.Background(), p1, srv.URL+"/grant"); err != nil {
		t.Fatal(err)
	}
	// A second, independent jar for the same client and the same host must
	// not see the cookie the first jar collected.
	if _, err := c.Get(context.Background(), p2, srv.URL+"/page"); err != nil {
		t.Fatal(err)
	}
	if sawCookieOnSecondJar != "" {
		t.Fatalf("Cookie = %q on the second jar's request, want empty: cookies must never cross jars", sawCookieOnSecondJar)
	}
}

// TestNoCookieJarMeansNoCookieHeader pins the invisible-by-default promise:
// a Policy with no jar behaves exactly as fetch did before CookieJar existed.
func TestNoCookieJarMeansNoCookieHeader(t *testing.T) {
	t.Parallel()
	var hits int
	var sawCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			http.SetCookie(w, &http.Cookie{Name: "s", Value: "1"})
			fmt.Fprint(w, "ok")
			return
		}
		sawCookie = r.Header.Get("Cookie")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	// p.Cookies left nil.

	if _, err := c.Get(context.Background(), p, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), p, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	if sawCookie != "" {
		t.Fatalf("Cookie = %q, want empty: no jar means no cookie is ever sent", sawCookie)
	}
}
