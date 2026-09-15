package fetch_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

// stubResolve answers from a table, so every guard branch can be exercised
// without a resolver, a network, or a name that has to exist anywhere.
func stubResolve(table map[string][]string) func(context.Context, string) ([]net.IP, error) {
	return func(_ context.Context, host string) ([]net.IP, error) {
		addrs, ok := table[host]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		out := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			out = append(out, net.ParseIP(a))
		}
		return out, nil
	}
}

func TestGuardRejections(t *testing.T) {
	t.Parallel()

	resolver := stubResolve(map[string][]string{
		"site.example.invalid":      {"203.0.113.9"}, // TEST-NET-3: itself blocked
		"public.example.invalid":    {"198.51.100.1"},
		"good.example.invalid":      {"93.184.216.34"},
		"img.good.example.invalid":  {"93.184.216.36"},
		"cdn.elsewhere.invalid":     {"93.184.216.35"},
		"loop.example.invalid":      {"127.0.0.1"},
		"lan.example.invalid":       {"192.168.1.10"},
		"meta.example.invalid":      {"169.254.169.254"}, // the cloud metadata service
		"split.example.invalid":     {"93.184.216.34", "10.0.0.5"},
		"cgnat.example.invalid":     {"100.100.0.1"},
		"ula.example.invalid":       {"fd00::1"},
		"v6loop.example.invalid":    {"::1"},
		"mapped.example.invalid":    {"::ffff:127.0.0.1"},
		"nat64.example.invalid":     {"64:ff9b::7f00:1"},
		"empty.example.invalid":     {},
		"bench.example.invalid":     {"198.18.0.1"},
		"reserved.example.invalid":  {"240.0.0.1"},
		"sixtofour.example.invalid": {"2002:0a00:0001::1"}, // 6to4 wrapping 10.0.0.1
	})
	g := fetch.NewGuard(resolver)

	base, _ := url.Parse("https://good.example.invalid/")
	policy := &fetch.Policy{BaseURL: base}
	cdnPolicy := &fetch.Policy{BaseURL: base, AllowedHosts: []string{"elsewhere.invalid"}}

	tests := []struct {
		name    string
		raw     string
		policy  *fetch.Policy
		wantErr error  // nil means allowed
		wantSub string // substring the plain-language reason must contain
	}{
		{name: "plain https is allowed", raw: "https://good.example.invalid/manga/x", policy: policy},
		{name: "http is allowed", raw: "http://good.example.invalid/", policy: policy},
		{name: "subdomain of base is allowed", raw: "https://img.good.example.invalid/1.jpg", policy: policy},

		{name: "file scheme", raw: "file:///etc/passwd", wantErr: fetch.ErrInvalidURL, wantSub: "only http and https"},
		{name: "gopher scheme", raw: "gopher://good.example.invalid/", wantErr: fetch.ErrInvalidURL, wantSub: "only http and https"},
		{name: "ftp scheme", raw: "ftp://good.example.invalid/", wantErr: fetch.ErrInvalidURL, wantSub: "only http and https"},
		{name: "data scheme", raw: "data:text/html,hi", wantErr: fetch.ErrInvalidURL},
		{name: "no host", raw: "https:///path", wantErr: fetch.ErrInvalidURL, wantSub: "no host"},
		{name: "credentials in URL", raw: "https://good.example.invalid@evil.invalid/", wantErr: fetch.ErrInvalidURL, wantSub: "credentials"},

		{name: "loopback literal", raw: "http://127.0.0.1:8080/", wantErr: fetch.ErrBlockedAddress, wantSub: "loopback"},
		{name: "loopback by name", raw: "http://loop.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "loopback"},
		{name: "ipv6 loopback", raw: "http://v6loop.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "loopback"},
		{name: "ipv4-mapped loopback", raw: "http://mapped.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "loopback"},

		{name: "rfc1918 literal", raw: "http://10.1.2.3/", wantErr: fetch.ErrBlockedAddress, wantSub: "private"},
		{name: "rfc1918 by name", raw: "http://lan.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "private"},
		{name: "unique local ipv6", raw: "http://ula.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "private"},

		{name: "link-local metadata service", raw: "http://meta.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "link-local"},
		{name: "link-local literal", raw: "http://169.254.169.254/latest/meta-data/", wantErr: fetch.ErrBlockedAddress, wantSub: "link-local"},

		{name: "unspecified address", raw: "http://0.0.0.0/", wantErr: fetch.ErrBlockedAddress, wantSub: "not routable"},
		{name: "link-local multicast", raw: "http://224.0.0.1/", wantErr: fetch.ErrBlockedAddress, wantSub: "link-local"},
		{name: "global multicast", raw: "http://239.1.1.1/", wantErr: fetch.ErrBlockedAddress, wantSub: "multicast"},
		{name: "cgnat", raw: "http://cgnat.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "reserved range"},
		{name: "benchmarking range", raw: "http://bench.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "reserved range"},
		{name: "reserved 240/4", raw: "http://reserved.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "reserved range"},
		{name: "nat64 embedded loopback", raw: "http://nat64.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "reserved range"},
		{name: "6to4 embedding rfc1918", raw: "http://sixtofour.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "6to4"},

		// DNS rebinding: one good address is not enough.
		{name: "split-horizon rebinding", raw: "http://split.example.invalid/", wantErr: fetch.ErrBlockedAddress, wantSub: "private"},

		{name: "unresolvable name", raw: "https://nowhere.example.invalid/", wantErr: fetch.ErrInvalidURL, wantSub: "cannot resolve"},
		{name: "resolves to nothing", raw: "https://empty.example.invalid/", wantErr: fetch.ErrInvalidURL, wantSub: "no addresses"},

		{
			name:    "redirect leaving the registrable domain",
			raw:     "https://cdn.elsewhere.invalid/1.jpg",
			policy:  policy,
			wantErr: fetch.ErrBlockedAddress,
			wantSub: "allowedHosts",
		},
		{
			name:   "allowedHosts permits the CDN",
			raw:    "https://cdn.elsewhere.invalid/1.jpg",
			policy: cdnPolicy,
		},
		{
			name:    "allowedHosts does not permit a lookalike",
			raw:     "https://cdn.elsewhere.invalid.evil.invalid/1.jpg",
			policy:  cdnPolicy,
			wantErr: fetch.ErrBlockedAddress,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tc.raw)
			if err != nil {
				if tc.wantErr == nil {
					t.Fatalf("parse %q: %v", tc.raw, err)
				}
				return
			}
			err = g.CheckURL(context.Background(), u, tc.policy)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("CheckURL(%q) = %v, want allowed", tc.raw, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckURL(%q) = nil, want %v", tc.raw, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CheckURL(%q) = %v, want it to wrap %v", tc.raw, err, tc.wantErr)
			}
			var ge *fetch.GuardError
			if !errors.As(err, &ge) {
				t.Fatalf("CheckURL(%q) = %T, want a *GuardError carrying a reason", tc.raw, err)
			}
			if tc.wantSub != "" && !strings.Contains(ge.Reason, tc.wantSub) {
				t.Fatalf("reason %q does not mention %q", ge.Reason, tc.wantSub)
			}
		})
	}
}

func TestRegistrableDomain(t *testing.T) {
	t.Parallel()
	tests := []struct{ host, want string }{
		{"example.invalid", "example.invalid"},
		{"a.b.example.invalid", "example.invalid"},
		{"EXAMPLE.INVALID.", "example.invalid"},
		{"example.co.uk", "example.co.uk"},
		{"a.example.co.uk", "example.co.uk"},
		{"203.0.113.1", "203.0.113.1"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			if got := fetch.RegistrableDomain(tc.host); got != tc.want {
				t.Fatalf("RegistrableDomain(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}
