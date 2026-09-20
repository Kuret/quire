package fetch_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
)

// A per-source proxy is a hole in the same wall the self-hosted exemption cuts
// through, so these tests are written the same way round: every case names a
// request the proxy must *not* carry, and asserts where the bytes actually
// went.
//
// The danger is specific. With a proxy, name resolution and connection both
// happen at the proxy, so the address the guard resolved and checked is no
// longer a description of where the request goes. A content-supplied URL that
// inherited the proxy would therefore walk straight past the address rules —
// the hole fetch.Guard's comment is about, where a cover URL scraped from a
// listing reaches a service on the user's own network unattended.

// proxiedHost is the source's own host: the one host a proxy covers. It
// resolves into CGNAT, so it is also a host that needs the self-hosted
// confirmation — the two settings compose, which is the realistic shape.
const proxiedHost = "zima.example"

// proxySpy is an HTTP proxy that answers everything itself rather than
// forwarding. It does not need to forward: what every test here asks is
// *whether a request arrived*, and a proxy that answered is a proxy that was
// used.
type proxySpy struct {
	srv  *httptest.Server
	hits atomic.Int32

	// gotURL is the request-target of the last request, which for a proxy is
	// the absolute URL of the origin the client wanted.
	gotURL atomic.Value

	// redirectTo, when set, is where the proxy sends the first request instead
	// of answering it.
	redirectTo string
}

func newProxySpy(t *testing.T, body string) *proxySpy {
	t.Helper()
	p := &proxySpy{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.hits.Add(1)
		p.gotURL.Store(r.URL.String())
		if p.redirectTo != "" {
			http.Redirect(w, r, p.redirectTo, http.StatusFound)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *proxySpy) url() string { return p.srv.URL }

func (p *proxySpy) lastURL() string {
	s, _ := p.gotURL.Load().(string)
	return s
}

// originSpy is an ordinary web server: the thing a *direct* connection reaches.
type originSpy struct {
	srv  *httptest.Server
	hits atomic.Int32
	path atomic.Value
}

func newOriginSpy(t *testing.T, body string) *originSpy {
	t.Helper()
	o := &originSpy{}
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.hits.Add(1)
		o.path.Store(r.URL.Path)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *originSpy) lastPath() string {
	s, _ := o.path.Load().(string)
	return s
}

// proxyClient builds a client whose DNS says the source's host is on a tailnet
// (CGNAT), which is what the 2026-09-20 measurement was against.
func proxyClient(t *testing.T) *fetch.Client {
	t.Helper()
	opts := fetch.WithStubResolver(fetch.Options{Version: "test"}, stubResolve(map[string][]string{
		proxiedHost:     {"100.79.171.1"},
		"other.example": {"93.184.216.34"},
	}))
	opts.Sleep = func(context.Context, time.Duration) error { return nil }
	return fetch.NewClient(fetch.AllowLoopback(opts))
}

// proxyPolicy is the policy a confirmed, proxied source produces: its own host
// exempt from the address rules, its own host proxied, and the loopback origin
// allowed past the domain boundary so the "not through the proxy" cases can be
// reached at all.
func proxyPolicy(t *testing.T, proxy string, allowed ...string) *fetch.Policy {
	t.Helper()
	base, err := url.Parse("http://" + proxiedHost)
	if err != nil {
		t.Fatal(err)
	}
	p := &fetch.Policy{BaseURL: base, SelfHostedHost: proxiedHost, AllowedHosts: allowed}
	if proxy != "" {
		pu, err := fetch.ParseProxyURL(proxy)
		if err != nil {
			t.Fatalf("ParseProxyURL(%q): %v", proxy, err)
		}
		p.Proxy = pu
	}
	return p
}

// hostOf is the "127.0.0.1" of an httptest URL, for allowedHosts.
func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname()
}

// TestProxyCarriesTheSourcesOwnHost is the feature itself: the device cannot
// route to the source, so the request goes through the proxy the user gave.
func TestProxyCarriesTheSourcesOwnHost(t *testing.T) {
	t.Parallel()
	proxy := newProxySpy(t, "through the proxy")
	c := proxyClient(t)
	p := proxyPolicy(t, proxy.url())

	resp, err := c.Get(context.Background(), p, "http://"+proxiedHost+"/index.html")
	if err != nil {
		t.Fatalf("a proxied request to the source's own host failed: %v", err)
	}
	if got := string(resp.Body); got != "through the proxy" {
		t.Errorf("body = %q, want the proxy's answer", got)
	}
	if n := proxy.hits.Load(); n != 1 {
		t.Fatalf("the proxy saw %d requests, want 1", n)
	}
	// A proxy is asked for an absolute URL, which is also the proof that the
	// host was never resolved here: the name went to the proxy verbatim.
	if got, want := proxy.lastURL(), "http://"+proxiedHost+"/index.html"; got != want {
		t.Errorf("the proxy was asked for %q, want %q", got, want)
	}
}

// TestProxyIsNotUsedForAnotherHost is the SSRF case, and the one that matters.
// The URL is the shape a scraped cover URL has: chosen by content, on a host
// that is not the source's own. It must be fetched directly — the proxy is the
// user's route to *their* server, not a general-purpose forwarder content can
// aim.
func TestProxyIsNotUsedForAnotherHost(t *testing.T) {
	t.Parallel()
	proxy := newProxySpy(t, "through the proxy")
	origin := newOriginSpy(t, "direct")
	c := proxyClient(t)
	p := proxyPolicy(t, proxy.url(), hostOf(t, origin.srv.URL))

	resp, err := c.Get(context.Background(), p, origin.srv.URL+"/cover.jpg")
	if err != nil {
		t.Fatalf("an unproxied request failed: %v", err)
	}
	if got := string(resp.Body); got != "direct" {
		t.Errorf("body = %q, want the origin's own answer — the proxy answered instead", got)
	}
	if n := origin.hits.Load(); n != 1 {
		t.Errorf("the origin saw %d direct requests, want 1", n)
	}
	if n := proxy.hits.Load(); n != 0 {
		t.Fatalf("the proxy carried %d requests for a host it does not cover; "+
			"a content-supplied URL must never inherit it", n)
	}
}

// TestProxyIsNotUsedForARedirectOffTheHost is the same rule one hop later. A
// redirect target is content too, and an exemption that survived a 302 would be
// exactly the hole the self-hosted work was careful not to open.
func TestProxyIsNotUsedForARedirectOffTheHost(t *testing.T) {
	t.Parallel()
	origin := newOriginSpy(t, "landed direct")
	proxy := newProxySpy(t, "")
	proxy.redirectTo = origin.srv.URL + "/landed"
	c := proxyClient(t)
	p := proxyPolicy(t, proxy.url(), hostOf(t, origin.srv.URL))

	resp, err := c.Get(context.Background(), p, "http://"+proxiedHost+"/start")
	if err != nil {
		t.Fatalf("the redirected request failed: %v", err)
	}
	if got := string(resp.Body); got != "landed direct" {
		t.Errorf("body = %q, want the origin's own answer", got)
	}
	if got := origin.lastPath(); got != "/landed" {
		t.Errorf("the origin was asked for %q, want /landed", got)
	}
	// One request through the proxy: the first hop, which was on the source's
	// own host. The hop that left it went direct.
	if n := proxy.hits.Load(); n != 1 {
		t.Fatalf("the proxy carried %d requests; the hop that left the confirmed host must not be one of them", n)
	}
}

// TestNoProxyIsExactlyAsBefore pins the ordinary case: a source with no proxy
// behaves as it always did, and the policy field being absent changes nothing
// about the request.
func TestNoProxyIsExactlyAsBefore(t *testing.T) {
	t.Parallel()
	proxy := newProxySpy(t, "through the proxy")
	origin := newOriginSpy(t, "direct")
	c := proxyClient(t)
	p := proxyPolicy(t, "", hostOf(t, origin.srv.URL))

	resp, err := c.Get(context.Background(), p, origin.srv.URL+"/index.html")
	if err != nil {
		t.Fatalf("an unproxied source failed: %v", err)
	}
	if got := string(resp.Body); got != "direct" {
		t.Errorf("body = %q, want the origin's own answer", got)
	}
	if n := proxy.hits.Load(); n != 0 {
		t.Fatalf("a source with no proxy sent %d requests to one anyway", n)
	}
}

// TestProxyDoesNotLiftTheAddressRules keeps the two assertions apart. A proxy
// says how to reach a host; it says nothing about which addresses Quire will
// go to, and a private address still needs the user's confirmation.
func TestProxyDoesNotLiftTheAddressRules(t *testing.T) {
	t.Parallel()
	proxy := newProxySpy(t, "through the proxy")
	c := proxyClient(t)
	p := proxyPolicy(t, proxy.url())
	p.SelfHostedHost = "" // the proxy stays; the confirmation is withdrawn

	_, err := c.Get(context.Background(), p, "http://"+proxiedHost+"/")
	if !errors.Is(err, fetch.ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress: a proxy must not be a way past the address rules", err)
	}
	if n := proxy.hits.Load(); n != 0 {
		t.Fatalf("the proxy carried %d requests for an address the guard refused", n)
	}
}

// TestParseProxyURLRefusesWhatItCannotUse checks the validation happens at the
// point the value is set — the caller gets a sentence, not a timeout later.
func TestParseProxyURLRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()

	good := []string{
		"http://localhost:1055",
		"https://proxy.example:8443",
		"socks5://127.0.0.1:1080",
		"http://user:pass@proxy.example:3128", // proxy auth is a real thing
		"http://localhost:1055/",              // a bare trailing slash is not a path
	}
	for _, raw := range good {
		if _, err := fetch.ParseProxyURL(raw); err != nil {
			t.Errorf("ParseProxyURL(%q) = %v, want it accepted", raw, err)
		}
	}

	bad := []string{
		"",
		"   ",
		"localhost:1055",              // no scheme: a host:port is not a proxy URL
		"socks4://127.0.0.1:1080",     // not one of the three
		"ftp://proxy.example:21",      //
		"file:///etc/passwd",          // the classic pivot
		"http://",                     // no host
		"http://localhost:1055/proxy", // a page served through a proxy, not a proxy
		"http://localhost:1055?x=1",
	}
	for _, raw := range bad {
		if _, err := fetch.ParseProxyURL(raw); !errors.Is(err, fetch.ErrInvalidProxy) {
			t.Errorf("ParseProxyURL(%q) = %v, want ErrInvalidProxy", raw, err)
		}
	}
}

// TestParseProxyURLNormalisesTheScheme: what is stored is what the fetch layer
// will act on, so the case of a typed scheme cannot matter later.
func TestParseProxyURLNormalisesTheScheme(t *testing.T) {
	t.Parallel()
	u, err := fetch.ParseProxyURL("HTTP://Localhost:1055")
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" {
		t.Errorf("scheme = %q, want %q", u.Scheme, "http")
	}
}
