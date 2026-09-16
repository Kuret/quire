package prober_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// modelGuard stands in for fetch.Guard, which cannot be built outside its own
// package with a stub resolver. It answers the way the real guard does for the
// three rules stage 1 depends on — scheme, address, resolvability — and records
// the URL it was handed, so these tests can assert both what normalisation
// produced *and* that it still reaches the guard.
//
// It exists because the new bare-domain path is exactly where the SSRF guard
// could be lost: a normalised address that never reached CheckURL would pass a
// test that only looked at verdicts.
type modelGuard struct{ got *url.URL }

// blockedHosts are the PLAN §7.4 refusals these tests care about, spelled as
// the host stage 1 will have produced.
var blockedHosts = map[string]string{
	"localhost":       "loopback addresses are not allowed",
	"127.0.0.1":       "loopback addresses are not allowed",
	"[::1]":           "loopback addresses are not allowed",
	"192.168.1.5":     "private addresses are not allowed",
	"10.0.0.1":        "private addresses are not allowed",
	"169.254.169.254": "link-local addresses are not allowed",
	"0.0.0.0":         "unspecified addresses are not allowed",
}

func (g *modelGuard) CheckURL(_ context.Context, u *url.URL, _ *fetch.Policy) error {
	g.got = u
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return &fetch.GuardError{URL: u.Redacted(), Reason: "only http and https are allowed, not " + s, Kind: fetch.ErrInvalidURL}
	}
	if u.User != nil {
		return &fetch.GuardError{URL: u.Redacted(), Reason: "credentials in URL are not allowed", Kind: fetch.ErrInvalidURL}
	}
	if reason, blocked := blockedHosts[strings.ToLower(u.Host)]; blocked {
		return &fetch.GuardError{URL: u.Redacted(), Reason: reason, Kind: fetch.ErrBlockedAddress}
	}
	// The real guard resolves the host. A single label with no dot is not a
	// name that resolves anywhere, and that is how a bare word is refused.
	if !strings.Contains(u.Hostname(), ".") {
		return &fetch.GuardError{URL: u.Redacted(), Reason: "cannot resolve " + u.Hostname(), Kind: fetch.ErrInvalidURL}
	}
	return nil
}

// probeInput runs a probe that stops at stage 2, so the result reflects stage 1
// alone. It returns the verdict and the URL the guard was handed ("" if stage 1
// refused the input before the guard).
func probeInput(t *testing.T, raw string) (prober.Result, string) {
	t.Helper()
	f := themetest.New(t, nil)
	g := &modelGuard{}
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
