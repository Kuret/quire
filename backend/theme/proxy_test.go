package theme_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
)

// The store-side half of the per-source proxy. What a proxy reaches — and,
// mostly, what it does not — is in backend/fetch/proxy_test.go. What matters
// here is that an unusable proxy is refused where it is *set*, and that a
// usable one reaches the fetch layer scoped to the one source it belongs to.

func proxiedSource(base, proxy string) *theme.Source {
	return &theme.Source{
		ID: "zima", Name: "Zima", Lang: "en", Theme: "alpha", BaseURL: base,
		Proxy:   proxy,
		AddedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
	}
}

func proxyRegistry() *theme.Registry {
	reg := theme.NewRegistry()
	reg.MustRegister(&fake{id: "alpha"})
	return reg
}

// TestPolicyCarriesTheProxy is the join between the stored field and the fetch
// layer, and it is deliberately paired with BaseURL: the proxy's whole scope is
// that host, so a Policy that carried one without the other would be a proxy
// with nothing to be scoped to.
func TestPolicyCarriesTheProxy(t *testing.T) {
	src := proxiedSource("http://zima.example:8084/", "http://localhost:1055")
	p, err := src.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.Proxy == nil {
		t.Fatal("Policy carries no proxy for a source that has one")
	}
	if got := p.Proxy.String(); got != "http://localhost:1055" {
		t.Errorf("Policy proxy = %q, want the source's own", got)
	}
	if p.BaseURL == nil || p.BaseURL.Hostname() != "zima.example" {
		t.Fatalf("Policy BaseURL = %v, want the host the proxy is scoped to", p.BaseURL)
	}

	// A proxy is not a permission. It must not quietly confer the self-hosted
	// exemption, which is a separate thing the user says separately.
	if p.SelfHostedHost != "" {
		t.Errorf("SelfHostedHost = %q; a proxy must not confer the address exemption", p.SelfHostedHost)
	}

	src.Proxy = ""
	p, err = src.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.Proxy != nil {
		t.Errorf("Policy proxy = %v for a source with none, want nil", p.Proxy)
	}
}

// TestValidateRefusesAProxyItCannotUse: the whole source is refused, rather
// than the field being dropped. A source silently fetched directly because its
// proxy did not parse fails with a timeout on a network the user knows is
// reachable, which is the least debuggable outcome available.
func TestValidateRefusesAProxyItCannotUse(t *testing.T) {
	reg := proxyRegistry()

	bad := []string{
		"localhost:1055",              // no scheme
		"socks4://127.0.0.1:1080",     // not one of the three accepted
		"file:///etc/passwd",          // the classic pivot
		"http://localhost:1055/proxy", // a page served through a proxy
		"http://",                     // no host
	}
	for _, raw := range bad {
		src := proxiedSource("http://zima.example", raw)
		if err := reg.Validate(src); err == nil {
			t.Errorf("Validate accepted proxy %q", raw)
		}
		if _, err := src.Policy(); !errors.Is(err, fetch.ErrInvalidProxy) {
			t.Errorf("Policy() with proxy %q = %v, want ErrInvalidProxy", raw, err)
		}
	}

	for _, raw := range []string{"", "http://localhost:1055", "https://proxy.example:8443", "socks5://127.0.0.1:1080"} {
		src := proxiedSource("http://zima.example", raw)
		if err := reg.Validate(src); err != nil {
			t.Errorf("Validate rejected proxy %q: %v", raw, err)
		}
	}
}

// TestProxyAndConfirmationCompose. They are independent — a source on a
// routable network may want a proxy, and a confirmed source on a real
// interface needs none — but the case that prompted all this has both.
func TestProxyAndConfirmationCompose(t *testing.T) {
	src := proxiedSource("http://zima.example", "http://localhost:1055")
	src.SelfHosted = &theme.SelfHosted{
		ConfirmedAddr: "100.79.171.1",
		ConfirmedAt:   time.Date(2026, 9, 20, 9, 30, 0, 0, time.UTC),
	}
	if err := proxyRegistry().Validate(src); err != nil {
		t.Fatalf("a confirmed, proxied source does not validate: %v", err)
	}
	p, err := src.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.Proxy == nil || p.SelfHostedHost != "zima.example" {
		t.Fatalf("Policy = proxy %v, selfHosted %q; want both", p.Proxy, p.SelfHostedHost)
	}
}

// TestAConfirmationReachedThroughAProxyValidates is the record for the source
// that resolves to nothing here (measured 2026-09-20: a mesh name, a public
// resolver, and no TUN device for the VPN to install one with).
func TestAConfirmationReachedThroughAProxyValidates(t *testing.T) {
	src := proxiedSource("http://zima.example", "http://localhost:1055")
	src.SelfHosted = &theme.SelfHosted{
		ViaProxy:    true,
		ConfirmedAt: time.Date(2026, 9, 20, 9, 30, 0, 0, time.UTC),
	}
	if err := proxyRegistry().Validate(src); err != nil {
		t.Fatalf("a proxy-confirmed source does not validate: %v", err)
	}
	// No address is recorded, and none is invented.
	if src.SelfHosted.ConfirmedAddr != "" {
		t.Errorf("ConfirmedAddr = %q, want nothing recorded", src.SelfHosted.ConfirmedAddr)
	}
	p, err := src.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.SelfHostedHost != "zima.example" || p.Proxy == nil {
		t.Fatalf("policy = selfHosted %q, proxy %v; want both", p.SelfHostedHost, p.Proxy)
	}

	// Take the proxy away and the confirmation has nothing left to stand on.
	// This is the shape a hand-edited flag would have, and it is refused.
	src.Proxy = ""
	if err := proxyRegistry().Validate(src); err == nil {
		t.Error("Validate accepted a proxy-confirmation with no proxy")
	}
	if _, err := src.Policy(); err == nil {
		t.Error("Policy() accepted a proxy-confirmation with no proxy")
	}
}
