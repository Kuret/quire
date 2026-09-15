package fetch_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme/mangadex"
)

// PLAN §7.2's theme-declared allowedHosts (added 2026-09-15) and PLAN §7.4's
// SSRF guard meet here.
//
// A theme may name hosts outside the source's registrable domain — MangaDex's
// page images come from *.mangadex.network, and without that declaration M4
// could list a chapter and download none of it. The seeding happens in the
// probe; what this file pins is the *limit* of what such a declaration can
// ever buy, because that is the property that matters and the one it would be
// easy to erode later.
//
// The limit, stated once: allowedHosts widens the registrable-domain boundary
// and nothing else. Guard.CheckURL runs the address check after the domain
// check, unconditionally, so a name that resolves into a private, loopback or
// link-local range is refused however it was named and by whom.

func policyAllowing(base string, hosts ...string) *fetch.Policy {
	u, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	return &fetch.Policy{BaseURL: u, AllowedHosts: hosts}
}

func check(t *testing.T, g *fetch.Guard, p *fetch.Policy, raw string) error {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return g.CheckURL(context.Background(), u, p)
}

// The case this feature exists for: a CDN on a different registrable domain,
// declared, resolving to a perfectly ordinary public address.
func TestAllowedHostsPermitsADeclaredCDN(t *testing.T) {
	t.Parallel()
	g := fetch.NewGuard(stubResolve(map[string][]string{
		"api.example.invalid":         {"93.184.216.34"},
		"abc123.cdn-example.invalid":  {"93.184.216.40"},
		"other.cdn-example.invalid":   {"93.184.216.41"},
		"cdn-example.invalid":         {"93.184.216.42"},
		"abc123.notthecdn.invalid":    {"93.184.216.43"},
		"cdn-example.invalid.evil.io": {"93.184.216.44"},
	}))

	t.Run("wildcard matches a subdomain", func(t *testing.T) {
		p := policyAllowing("https://api.example.invalid", "*.cdn-example.invalid")
		if err := check(t, g, p, "https://abc123.cdn-example.invalid/data/x.png"); err != nil {
			t.Fatalf("a declared CDN subdomain was refused: %v", err)
		}
		if err := check(t, g, p, "https://other.cdn-example.invalid/data/y.png"); err != nil {
			t.Fatalf("a second generated label was refused: %v", err)
		}
	})

	t.Run("wildcard does not match the bare domain", func(t *testing.T) {
		// The narrower form means what it says. A CDN whose hostnames are all
		// generated labels serves nothing from the apex, so permitting the
		// apex would be a widening nobody asked for.
		p := policyAllowing("https://api.example.invalid", "*.cdn-example.invalid")
		if err := check(t, g, p, "https://cdn-example.invalid/data/x.png"); err == nil {
			t.Fatal("*.domain permitted the bare domain")
		}
	})

	t.Run("the bare form matches both", func(t *testing.T) {
		p := policyAllowing("https://api.example.invalid", "cdn-example.invalid")
		for _, u := range []string{
			"https://cdn-example.invalid/x.png",
			"https://abc123.cdn-example.invalid/x.png",
		} {
			if err := check(t, g, p, u); err != nil {
				t.Errorf("%s was refused: %v", u, err)
			}
		}
	})

	t.Run("a lookalike is not matched", func(t *testing.T) {
		p := policyAllowing("https://api.example.invalid", "*.cdn-example.invalid")
		for _, u := range []string{
			// Suffix matching must be on a label boundary, not on the string.
			"https://abc123.notthecdn.invalid/x.png",
			// And a declared domain appearing as a *prefix* of someone else's
			// host buys nothing.
			"https://cdn-example.invalid.evil.io/x.png",
		} {
			if err := check(t, g, p, u); err == nil {
				t.Errorf("%s was permitted by an unrelated allowedHosts entry", u)
			}
		}
	})
}

// The property that matters, and the reason this file exists.
//
// A theme's declared host list is compiled into Quire and reviewed in a diff,
// which makes it trustworthy about *which CDN it uses* and says nothing about
// where that name points. A theme — or a hand-edited source entry, or an
// imported one — must not be able to name its way onto the local network.
func TestAllowedHostsCannotReachAPrivateAddress(t *testing.T) {
	t.Parallel()

	g := fetch.NewGuard(stubResolve(map[string][]string{
		"api.example.invalid": {"93.184.216.34"},
		// Every one of these is *declared* below. Each resolves somewhere the
		// guard refuses to go.
		"loopback.cdn.invalid": {"127.0.0.1"},
		"lan.cdn.invalid":      {"192.168.1.10"},
		"metadata.cdn.invalid": {"169.254.169.254"}, // the cloud metadata service
		"ula.cdn.invalid":      {"fd00::1"},
		"v6loop.cdn.invalid":   {"::1"},
		"cgnat.cdn.invalid":    {"100.100.0.1"},
		"split.cdn.invalid":    {"93.184.216.34", "10.0.0.5"}, // one good, one not
		"apex.cdn.invalid":     {"10.1.2.3"},
	}))

	for _, tc := range []struct {
		name string
		url  string
	}{
		{"loopback", "https://loopback.cdn.invalid/x.png"},
		{"private range", "https://lan.cdn.invalid/x.png"},
		{"link-local metadata service", "https://metadata.cdn.invalid/latest/meta-data/"},
		{"IPv6 unique-local", "https://ula.cdn.invalid/x.png"},
		{"IPv6 loopback", "https://v6loop.cdn.invalid/x.png"},
		{"carrier-grade NAT", "https://cgnat.cdn.invalid/x.png"},
		{"split horizon: one public answer and one private", "https://split.cdn.invalid/x.png"},
		{"the bare declared domain", "https://apex.cdn.invalid/x.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Declared as widely as it is possible to declare it: the exact
			// host, its wildcard, and its bare domain, all at once. If any
			// amount of declaring could buy a private address, this would.
			p := policyAllowing("https://api.example.invalid",
				"cdn.invalid", "*.cdn.invalid", mustHost(t, tc.url))

			err := check(t, g, p, tc.url)
			if err == nil {
				t.Fatalf("%s was permitted; allowedHosts must widen the domain boundary and nothing else", tc.url)
			}
			if !errors.Is(err, fetch.ErrBlockedAddress) {
				t.Fatalf("err = %v, want ErrBlockedAddress", err)
			}
			// And the reason must be about the address, not the domain — the
			// domain check passed, which is the point.
			if strings.Contains(err.Error(), "allowedHosts") {
				t.Errorf("err = %v; it blames the domain boundary, but the host was declared "+
					"and it was the address rules that refused it", err)
			}
		})
	}
}

// The scheme and credential rules are not negotiable either. They run before
// the domain check, so a declared host cannot carry a file: URL or a userinfo
// prefix past them.
func TestAllowedHostsCannotRelaxSchemeOrCredentials(t *testing.T) {
	t.Parallel()
	g := fetch.NewGuard(stubResolve(map[string][]string{
		"api.example.invalid": {"93.184.216.34"},
		"cdn.invalid":         {"93.184.216.40"},
	}))
	p := policyAllowing("https://api.example.invalid", "cdn.invalid", "*.cdn.invalid")

	for _, tc := range []struct{ name, url string }{
		{"non-http scheme on a declared host", "file://cdn.invalid/etc/passwd"},
		{"credentials on a declared host", "https://user:pw@cdn.invalid/x.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := check(t, g, p, tc.url)
			if err == nil {
				t.Fatalf("%s was permitted", tc.url)
			}
			if !errors.Is(err, fetch.ErrInvalidURL) {
				t.Fatalf("err = %v, want ErrInvalidURL", err)
			}
		})
	}
}

// An empty or malformed entry must not become a wildcard. This is the shape of
// bug that turns a config typo into "everything is allowed".
func TestAllowedHostsEmptyEntriesMatchNothing(t *testing.T) {
	t.Parallel()
	g := fetch.NewGuard(stubResolve(map[string][]string{
		"api.example.invalid": {"93.184.216.34"},
		"anywhere.invalid":    {"93.184.216.50"},
	}))

	for _, entry := range []string{"", "   ", "*", "*.", "."} {
		p := policyAllowing("https://api.example.invalid", entry)
		if err := check(t, g, p, "https://anywhere.invalid/x.png"); err == nil {
			t.Errorf("allowedHosts entry %q permitted an arbitrary host", entry)
		}
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname()
}

// The real case, end to end at the guard level: what the mangadex theme
// declares, against the host shape MangaDex actually returns from
// /at-home/server/{id}.
//
// This lives here rather than in the theme's own tests because it takes both
// halves to mean anything — the declaration is the theme's, the rule is the
// guard's, and a test that only checked one would pass while the pair was
// broken. fetch_test may import a package that imports fetch; there is no
// cycle, because this is the external test package.
//
// The host below was observed live on 2026-09-15. It is a generated label, so
// the exact string is not stable and is not what is being asserted — the shape
// is: a random label under mangadex.network, which is a different registrable
// domain from api.mangadex.org.
func TestMangaDexPageImageHostIsCoveredByItsDeclaration(t *testing.T) {
	t.Parallel()

	const (
		api   = "https://api.mangadex.org"
		image = "https://cmdxd98sb0x3yprd.mangadex.network/data/0f1e2d3c4b5a6978/1-abc.png"
		cover = "https://uploads.mangadex.org/covers/11111111-2222-4333-8444-555555555555/x.jpg"
	)

	// Resolution is stubbed: this test asserts the *domain boundary*, and must
	// not depend on the network or on where these names point today.
	g := fetch.NewGuard(stubResolve(map[string][]string{
		"api.mangadex.org":                  {"93.184.216.34"},
		"cmdxd98sb0x3yprd.mangadex.network": {"93.184.216.40"},
		"uploads.mangadex.org":              {"93.184.216.41"},
	}))

	declared := mangadex.New(nil).AllowedHosts()
	seeded := policyAllowing(api, declared...)
	unseeded := policyAllowing(api)

	if err := check(t, g, seeded, image); err != nil {
		t.Errorf("with the theme's declaration seeded, a page image was refused: %v\n"+
			"M4 would list a chapter it cannot download", err)
	}
	if err := check(t, g, unseeded, image); err == nil {
		t.Error("without the declaration the page image was permitted; " +
			"then stage 6 seeding buys nothing and this feature is pointless")
	}

	// Covers need no declaration: same registrable domain as the API.
	if err := check(t, g, unseeded, cover); err != nil {
		t.Errorf("a cover was refused without any declaration: %v", err)
	}
}
