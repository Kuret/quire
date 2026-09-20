package fetch_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
)

// The self-hosted exemption is a hole deliberately cut in the address rules,
// so these tests are written from the outside of it: every case names a way
// content could try to widen the hole, and asserts the refusal.
//
// The shape of the danger, from the comment on fetch.Guard: a scraped page
// supplies URLs Quire fetches unattended, and an unauthenticated /api/restart
// on a service the user runs is reachable by nothing more than a cover URL.
// Several cases below use exactly that URL, because the test should say what
// it is defending.

// The two hosts in play. "shelfmark" is the approved one; "nas" is another box
// on the same network, which is what the exemption must not reach.
const (
	approvedHost = "shelfmark.internal.invalid"
	otherHost    = "nas.internal.invalid" // same registrable domain, deliberately
	restartPath  = "/api/restart"
)

// selfHostedGuard builds a guard whose DNS says what a home network's does.
func selfHostedGuard() *fetch.Guard {
	return fetch.NewGuard(caseInsensitive(homeNetwork()))
}

// caseInsensitive makes a stub resolver behave like DNS, which does not care
// about case. Without it a test could only ever ask about lower-case names,
// and the host comparison this feature turns on is exactly the kind of thing
// that gets case wrong.
func caseInsensitive(f func(context.Context, string) ([]net.IP, error)) func(context.Context, string) ([]net.IP, error) {
	return func(ctx context.Context, host string) ([]net.IP, error) {
		return f(ctx, strings.ToLower(host))
	}
}

func homeNetwork() func(context.Context, string) ([]net.IP, error) {
	return stubResolve(map[string][]string{
		approvedHost:            {"192.168.1.10"},
		otherHost:               {"192.168.1.11"},
		"cdn." + approvedHost:   {"192.168.1.12"},
		"tailnet.example.test":  {"100.100.0.1"}, // CGNAT: where a tailnet lives
		"shelfmark":             {"100.100.0.2"}, // a single-label MagicDNS name
		"nas":                   {"100.100.0.3"},
		"moved." + otherHost:    {"192.168.1.13"},
		"public.example.test":   {"93.184.216.34"},
		"rebound.example.test":  {"169.254.169.254"}, // cloud metadata
		"looped.example.test":   {"127.0.0.1"},
		"sixtofour.example.tst": {"2002:c0a8:010a::1"}, // 6to4 wrapping 192.168.1.10
	})
}

func policyFor(t *testing.T, base string, selfHosted bool, allowed ...string) *fetch.Policy {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	p := &fetch.Policy{BaseURL: u, AllowedHosts: allowed}
	if selfHosted {
		p.SelfHostedHost = u.Hostname()
	}
	return p
}

// TestSelfHostedExemptionIsScopedToOneHost is the whole security argument as a
// table: the confirmed host gets past the address rules, and nothing else does
// — not another box on the same private network, not a subdomain, not a host a
// theme's AllowedHosts named, and not a URL that came out of a page.
func TestSelfHostedExemptionIsScopedToOneHost(t *testing.T) {
	t.Parallel()

	g := selfHostedGuard()
	confirmed := policyFor(t, "http://"+approvedHost+":8084/", true)
	unconfirmed := policyFor(t, "http://"+approvedHost+":8084/", false)
	// A theme that declares an image CDN, pointed at the other box. PLAN §7.4
	// and theme.Theme.AllowedHosts both promise this cannot reach the network.
	viaAllowedHosts := policyFor(t, "http://"+approvedHost+":8084/", true, otherHost)
	tailnet := policyFor(t, "http://tailnet.example.test/", true)
	magicDNS := policyFor(t, "http://shelfmark/", true)
	literal := policyFor(t, "http://192.168.1.10:8084/", true)

	tests := []struct {
		name    string
		raw     string
		policy  *fetch.Policy
		wantErr error // nil means allowed
		wantSub string
	}{
		// The point of the feature.
		{name: "the confirmed host on a private address", raw: "http://" + approvedHost + ":8084/books", policy: confirmed},
		{name: "the confirmed host on CGNAT, where a tailnet lives", raw: "http://tailnet.example.test/books", policy: tailnet},
		{name: "a single-label MagicDNS name", raw: "http://shelfmark/books", policy: magicDNS},
		{name: "the confirmed host written as its address", raw: "http://192.168.1.10:8084/books", policy: literal},
		{name: "the confirmed host, matched case-insensitively", raw: "http://" + strings.ToUpper(approvedHost) + "/books", policy: confirmed},

		// Without the confirmation, nothing changes.
		{
			name: "the same host with no confirmation", raw: "http://" + approvedHost + ":8084/books",
			policy: unconfirmed, wantErr: fetch.ErrBlockedAddress, wantSub: "private",
		},

		// Everything else on the same network.
		{
			name: "another box on the same private network", raw: "http://" + otherHost + restartPath,
			policy: confirmed, wantErr: fetch.ErrBlockedAddress, wantSub: "private",
		},
		{
			name: "a cover URL scraped from a page, aimed at the other box", raw: "http://" + otherHost + restartPath,
			policy: confirmed, wantErr: fetch.ErrBlockedAddress, wantSub: "private",
		},
		{
			name: "a cover URL aimed at the other box, no confirmation anywhere", raw: "http://" + otherHost + restartPath,
			policy: unconfirmed, wantErr: fetch.ErrBlockedAddress, wantSub: "private",
		},
		{
			name: "a theme's allowedHosts entry cannot reach the network", raw: "http://" + otherHost + restartPath,
			policy: viaAllowedHosts, wantErr: fetch.ErrBlockedAddress, wantSub: "private",
		},
		{
			name: "a subdomain of the confirmed host is not the confirmed host", raw: "http://cdn." + approvedHost + "/1.jpg",
			policy: confirmed, wantErr: fetch.ErrBlockedAddress, wantSub: "private",
		},
		{
			name: "another CGNAT host on the tailnet", raw: "http://nas" + restartPath,
			policy: magicDNS, wantErr: fetch.ErrBlockedAddress,
		},
		{
			// The registrable-domain boundary gets to this one first — an IP
			// literal is its own domain — so the reason names that rather than
			// the address rules. Either way it is refused, which is the point.
			name: "another private address as a literal", raw: "http://192.168.1.11" + restartPath,
			policy: literal, wantErr: fetch.ErrBlockedAddress, wantSub: "leaves the source's domain",
		},
		{
			// And with no base to leave, so the address rules are what refuse it.
			name: "another private address, address rules alone", raw: "http://192.168.1.11" + restartPath,
			policy: &fetch.Policy{SelfHostedHost: "192.168.1.10"}, wantErr: fetch.ErrBlockedAddress, wantSub: "private",
		},

		// Ranges no confirmation covers, even for the confirmed host: a
		// confirmation is a claim about a service on the user's network, and
		// none of these is one.
		{
			name: "the confirmed host resolving to loopback", raw: "http://looped.example.test/",
			policy: policyFor(t, "http://looped.example.test/", true), wantErr: fetch.ErrBlockedAddress, wantSub: "loopback",
		},
		{
			name: "the confirmed host resolving to the metadata service", raw: "http://rebound.example.test/",
			policy: policyFor(t, "http://rebound.example.test/", true), wantErr: fetch.ErrBlockedAddress, wantSub: "link-local",
		},
		{
			name: "6to4 wrapping the confirmed address", raw: "http://sixtofour.example.tst/",
			policy: policyFor(t, "http://sixtofour.example.tst/", true), wantErr: fetch.ErrBlockedAddress, wantSub: "6to4",
		},

		// The rest of the guard is untouched by the exemption.
		{
			name: "a non-http scheme on the confirmed host", raw: "file://" + approvedHost + "/etc/passwd",
			policy: confirmed, wantErr: fetch.ErrInvalidURL, wantSub: "only http and https",
		},
		{
			name: "credentials on the confirmed host", raw: "http://" + approvedHost + "@evil.example.test/",
			policy: confirmed, wantErr: fetch.ErrInvalidURL, wantSub: "credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.raw, err)
			}
			got := g.CheckURL(context.Background(), u, tt.policy)
			if tt.wantErr == nil {
				if got != nil {
					t.Fatalf("CheckURL(%q) = %v, want allowed", tt.raw, got)
				}
				return
			}
			if !errors.Is(got, tt.wantErr) {
				t.Fatalf("CheckURL(%q) = %v, want %v", tt.raw, got, tt.wantErr)
			}
			var ge *fetch.GuardError
			if tt.wantSub != "" && errors.As(got, &ge) && !strings.Contains(ge.Reason, tt.wantSub) {
				t.Fatalf("reason = %q, want it to mention %q", ge.Reason, tt.wantSub)
			}
		})
	}
}

// TestSelfHostedExemptionIsGoneAfterARedirect checks the hop the table above
// cannot: a redirect target is chosen by the *site*, so an exemption that
// survived one would be the content-controlled hole the exemption exists to
// avoid. This drives the real Client, because CheckRedirect is the wiring that
// has to carry the policy for the rule to apply at all.
func TestSelfHostedExemptionIsGoneAfterARedirect(t *testing.T) {
	t.Parallel()

	opts := fetch.WithStubResolver(fetch.Options{
		Version:   "test",
		Sleep:     func(context.Context, time.Duration) error { return nil },
		Transport: redirectTo("http://" + otherHost + restartPath),
	}, stubResolve(map[string][]string{
		approvedHost: {"192.168.1.10"},
		otherHost:    {"192.168.1.11"},
	}))
	c := fetch.NewClient(opts)

	p := policyFor(t, "http://"+approvedHost+":8084/", true)
	_, err := c.Get(context.Background(), p, "http://"+approvedHost+":8084/series")
	if !errors.Is(err, fetch.ErrBlockedAddress) {
		t.Fatalf("redirect off the confirmed host: err = %v, want ErrBlockedAddress", err)
	}

	// The same client, same confirmed host, redirected somewhere on the host
	// the user actually approved: still allowed, so the case above is the
	// redirect boundary biting and not the client simply refusing to redirect.
	c2 := fetch.NewClient(fetch.WithStubResolver(fetch.Options{
		Version:   "test",
		Sleep:     func(context.Context, time.Duration) error { return nil },
		Transport: redirectTo("http://" + approvedHost + ":8084/landed"),
	}, stubResolve(map[string][]string{approvedHost: {"192.168.1.10"}})))
	resp, err := c2.Get(context.Background(), p, "http://"+approvedHost+":8084/series")
	if err != nil {
		t.Fatalf("redirect within the confirmed host was refused: %v", err)
	}
	if resp.FinalURL == nil || resp.FinalURL.Path != "/landed" {
		t.Fatalf("FinalURL = %v, want path /landed", resp.FinalURL)
	}
}

// redirectTo is a transport that answers the first request with a 302 to loc
// and anything else with 200. Nothing is dialled, so no address in these tests
// has to exist — which is the point: the guard must refuse the hop before a
// connection is attempted.
func redirectTo(loc string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		status := http.StatusOK
		if r.URL.String() != loc {
			h.Set("Location", loc)
			status = http.StatusFound
		}
		return &http.Response{
			StatusCode: status,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader("landed")),
			Request:    r,
		}, nil
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestSelfHostableAddrIsNarrow pins the set of addresses a confirmation can
// ever cover. theme.Source's validation and the guard both read this function,
// so widening it widens the exemption everywhere at once.
func TestSelfHostableAddrIsNarrow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr string
		want bool
	}{
		{"192.168.1.10", true},     // RFC 1918
		{"10.0.0.5", true},         //
		{"172.16.3.4", true},       //
		{"fd00::1", true},          // ULA
		{"100.100.0.1", true},      // CGNAT, RFC 6598
		{"127.0.0.1", false},       // the device itself
		{"::1", false},             //
		{"169.254.169.254", false}, // the cloud metadata service
		{"224.0.0.1", false},       // multicast
		{"0.0.0.0", false},         //
		{"93.184.216.34", false},   // public: needs no confirmation
		{"198.18.0.1", false},      // benchmarking
		{"2001:db8::1", false},     // documentation
	}
	for _, tt := range tests {
		addr, err := netip.ParseAddr(tt.addr)
		if err != nil {
			t.Fatalf("parse %q: %v", tt.addr, err)
		}
		if got := fetch.SelfHostableAddr(addr); got != tt.want {
			t.Errorf("SelfHostableAddr(%s) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}
