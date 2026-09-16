//go:build quiretest

// These tests run the **real** fetch.Guard, which is why they carry the tag:
// backend/fetch/guard_quiretest.go exports a constructor that substitutes DNS
// and nothing else, and it exists only under `quiretest`.
//
// They used to run a hand-written stand-in that answered the way the guard was
// believed to answer. Stage 1's whole job is to hand an address to the guard,
// so asserting that against a model of the guard proves the model, not the
// boundary — the same trap as a fingerprint confirmed by the fixture that
// invented it.

package prober_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// guardResolve is the only thing these tests substitute: where DNS answers come
// from. Every rule the guard applies is the production one.
//
// Only names that have to resolve are listed. Anything else answers "no such
// host", which is how a bare word like "weebcentral" is refused — by the real
// resolver path, not by a rule invented here.
func guardResolve(_ context.Context, host string) ([]net.IP, error) {
	table := map[string][]string{
		"weebcentral.com":     {"93.184.216.34"},
		"www.weebcentral.com": {"93.184.216.34"},
		"localhost":           {"127.0.0.1"},
	}
	addrs, ok := table[strings.ToLower(host)]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, net.ParseIP(a))
	}
	return out, nil
}

// spyGuard records the URL stage 1 handed over and then asks the real guard,
// so these tests can assert what normalisation produced *and* that it still
// reaches CheckURL. It decides nothing itself.
//
// The recording matters because the new bare-domain path is exactly where the
// SSRF guard could be lost: an address that never reached the guard would pass
// a test that only looked at verdicts.
type spyGuard struct {
	inner *fetch.Guard
	got   *url.URL
}

func (g *spyGuard) CheckURL(ctx context.Context, u *url.URL, p *fetch.Policy) error {
	g.got = u
	return g.inner.CheckURL(ctx, u, p)
}

// probeInput runs a probe that stops at stage 2, so the result reflects stage 1
// alone. It returns the verdict and the URL the guard was handed ("" if stage 1
// refused the input before the guard).
func probeInput(t *testing.T, raw string) (prober.Result, string) {
	t.Helper()
	f := themetest.New(t, nil)
	g := &spyGuard{inner: fetch.NewGuardForTests(guardResolve)}
	p := prober.New(prober.Options{
		Fetcher:  errFetcher{err: errors.New("dial tcp: no route to host")},
		Registry: registry(f),
		Guard:    g,
		Now:      clock,
		NewID:    func() string { return "src-test" },
	})
	res, err := p.Run(context.Background(), raw, &recordUI{})
	if err != nil {
		t.Fatalf("probe returned an error: %v", err)
	}
	if g.got == nil {
		return res, ""
	}
	return res, g.got.String()
}

// PLAN §7.5 stage 1: typing on an e-ink display is slow, so an address with no
// scheme is normalised to https rather than refused.
func TestStage1NormalisesWhatPeopleType(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bare domain", "weebcentral.com", "https://weebcentral.com/"},
		{"www", "www.weebcentral.com", "https://www.weebcentral.com/"},
		{"trailing slash", "weebcentral.com/", "https://weebcentral.com/"},
		{"deep link", "weebcentral.com/series/abc", "https://weebcentral.com/"},
		{"query", "weebcentral.com/search?q=one+piece", "https://weebcentral.com/"},
		{"port", "weebcentral.com:8080", "https://weebcentral.com:8080/"},
		{"leading and trailing space", "  weebcentral.com  ", "https://weebcentral.com/"},
		{"tab and newline", "\tweebcentral.com\n", "https://weebcentral.com/"},
		{"typed https", "https://weebcentral.com/x", "https://weebcentral.com/"},
		// url.Parse lower-cases the scheme; the host is left as typed and DNS
		// is case-insensitive, so nothing is lost.
		{"typed https uppercase", "HTTPS://WeebCentral.com", "https://WeebCentral.com/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := probeInput(t, tc.in)
			if got != tc.want {
				t.Errorf("%q normalised to %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A typed http:// is the user being specific. Quire never silently upgrades it,
// and never silently downgrades an assumed https either.
func TestStage1RespectsATypedHTTP(t *testing.T) {
	res, got := probeInput(t, "http://weebcentral.com")
	if got != "http://weebcentral.com/" {
		t.Errorf("guard saw %q, want the http:// the user typed", got)
	}
	if strings.Contains(res.Detail, "assumed https") {
		t.Errorf("detail %q claims a guess Quire did not make", res.Detail)
	}
}

// When Quire supplied the scheme and the site could not be reached, it says so
// — and points at http:// as something the user can type, never as something it
// will fall back to on its own.
func TestStage1AdmitsTheAssumedHTTPS(t *testing.T) {
	res, _ := probeInput(t, "weebcentral.com")
	if res.Verdict != theme.VerdictUnreachable {
		t.Fatalf("verdict = %q (%s), want unreachable", res.Verdict, res.Detail)
	}
	for _, want := range []string{"assumed https://", "http://weebcentral.com"} {
		if !strings.Contains(res.Detail, want) {
			t.Errorf("detail %q does not mention %q", res.Detail, want)
		}
	}
}

// Near-misses a touch keyboard produces are errors that name the typo, not
// guesses. Guessing between "a missing slash" and "a host called https:" is
// what this stage exists to avoid.
func TestStage1RefusesNearMisses(t *testing.T) {
	cases := []struct{ name, in, wantDetail string }{
		{"one slash", "https:/weebcentral.com", "https://"},
		{"one slash http", "http:/weebcentral.com", "http://"},
		{"no slashes", "https:weebcentral.com", "https://"},
		{"missing colon", "https//weebcentral.com", ":"},
		{"email", "rick@weebcentral.com", "email address"},
		{"words", "where do i find one piece", ""},
		{"bare word", "weebcentral", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := probeInput(t, tc.in)
			if res.Verdict != theme.VerdictInvalidURL {
				t.Fatalf("verdict = %q (%s), want invalid_url", res.Verdict, res.Detail)
			}
			if res.Addable {
				t.Error("an address Quire could not read must not be addable")
			}
			if tc.wantDetail != "" && !strings.Contains(res.Detail, tc.wantDetail) {
				t.Errorf("detail %q does not mention %q", res.Detail, tc.wantDetail)
			}
		})
	}
}

// A misspelled scheme keeps its spelling, so the guard is the thing that
// refuses it and the user is told which scheme Quire will not use.
func TestStage1LeavesAnUnknownSchemeToTheGuard(t *testing.T) {
	cases := []struct{ name, in, wantScheme string }{
		{"typo", "htp://weebcentral.com", "htp"},
		{"ftp", "ftp://weebcentral.com/pub", "ftp"},
		{"file", "file:///etc/passwd", "file"},
		{"javascript", "javascript:alert(1)", "javascript"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, got := probeInput(t, tc.in)
			if !strings.HasPrefix(strings.ToLower(got), tc.wantScheme+":") {
				t.Fatalf("guard saw %q, want the %s scheme passed through unchanged", got, tc.wantScheme)
			}
			if res.Verdict != theme.VerdictInvalidURL {
				t.Fatalf("verdict = %q (%s), want invalid_url", res.Verdict, res.Detail)
			}
			if !strings.Contains(res.Detail, "only http and https") {
				t.Errorf("detail %q does not say which schemes Quire will use", res.Detail)
			}
		})
	}
}

// PLAN §7.4's SSRF guard is unchanged by normalisation, and the new
// no-scheme path is exactly where that protection could be lost. Every case
// runs twice: as the user would type it, and with a scheme typed out.
func TestStage1SSRFGuardHoldsWithAndWithoutAScheme(t *testing.T) {
	hosts := []string{"localhost", "127.0.0.1", "[::1]", "192.168.1.5", "10.0.0.1", "169.254.169.254", "0.0.0.0"}
	for _, host := range hosts {
		for _, in := range []string{host, "https://" + host, "http://" + host, host + "/library"} {
			t.Run(in, func(t *testing.T) {
				res, got := probeInput(t, in)
				if got == "" {
					t.Fatal("the guard was never asked about this address")
				}
				if res.Verdict != theme.VerdictBlockedAddress {
					t.Fatalf("verdict = %q (%s), want blocked_address", res.Verdict, res.Detail)
				}
				if res.Addable {
					t.Error("a blocked address must not be addable")
				}
			})
		}
	}
}
