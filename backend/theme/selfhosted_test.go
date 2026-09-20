package theme_test

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
)

// These tests are the store-side half of the self-hosted exemption. The guard
// half — what the exemption reaches and, mostly, what it does not — is in
// backend/fetch/selfhosted_test.go. What matters here is that a confirmation
// only ever reaches the guard when it is a complete, legible record of
// something a person did.

func confirmedSource(base, addr string) *theme.Source {
	return &theme.Source{
		ID: "shelfmark", Name: "Shelfmark", Lang: "en", Theme: "alpha", BaseURL: base,
		AddedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		SelfHosted: &theme.SelfHosted{
			ConfirmedAddr: addr,
			ConfirmedAt:   time.Date(2026, 9, 20, 9, 30, 0, 0, time.UTC),
		},
	}
}

// viaProxySource is the other shape a confirmation comes in: no address,
// because the host resolves to nothing here, and the proxy the user typed
// standing as the evidence instead. proxy may be empty, which is the shape that
// must be refused.
func viaProxySource(base, proxy string) *theme.Source {
	return &theme.Source{
		ID: "zima", Name: "Zima", Lang: "en", Theme: "alpha", BaseURL: base, Proxy: proxy,
		AddedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		SelfHosted: &theme.SelfHosted{
			ViaProxy:    true,
			ConfirmedAt: time.Date(2026, 9, 20, 9, 30, 0, 0, time.UTC),
		},
	}
}

// TestPolicyCarriesTheConfirmationForItsOwnHostOnly is the join between the
// stored record and the guard: the exemption on the Policy is the source's own
// host, and a source with no confirmation carries none at all.
func TestPolicyCarriesTheConfirmationForItsOwnHostOnly(t *testing.T) {
	src := confirmedSource("http://shelfmark.internal.invalid:8084/", "192.168.1.10")
	p, err := src.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.SelfHostedHost != "shelfmark.internal.invalid" {
		t.Fatalf("SelfHostedHost = %q, want the source's own host", p.SelfHostedHost)
	}
	// The confirmation must not leak into the field that widens the *domain*
	// boundary: those are different questions and PLAN §7.4 promises
	// allowedHosts can never reach the local network.
	for _, h := range p.AllowedHosts {
		if strings.Contains(h, "shelfmark") {
			t.Fatalf("AllowedHosts = %v, want the confirmation kept out of it", p.AllowedHosts)
		}
	}

	src.SelfHosted = nil
	p, err = src.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.SelfHostedHost != "" {
		t.Fatalf("SelfHostedHost = %q for an unconfirmed source, want empty", p.SelfHostedHost)
	}
}

// TestUnusableConfirmationsAreRefused covers the "refuse to make it silent"
// half. Each of these is a record that does not describe an act of consent, so
// the source is refused outright rather than fetched with the exemption
// quietly dropped — a half-legible record of permission to reach the local
// network is not something to interpret generously.
func TestUnusableConfirmationsAreRefused(t *testing.T) {
	reg := theme.NewRegistry()
	reg.MustRegister(&fake{id: "alpha"})

	tests := []struct {
		name    string
		src     *theme.Source
		wantSub string
	}{
		{
			// Still refused, and still the case this whole type exists for: a
			// record with nothing in it is the bare "let this one through" flag.
			// The wording moved on 2026-09-20, when a confirmation gained a
			// second thing it may record — a proxy, for a host that resolves
			// nowhere on this device (SelfHosted.ViaProxy) — so an empty record
			// is now "neither" rather than "not an IP address".
			name:    "no address at all",
			src:     confirmedSource("http://shelfmark.internal.invalid/", ""),
			wantSub: "neither an address nor a proxy",
		},
		{
			// The half of that which would otherwise be the new way in: the
			// flag on its own, with no proxy behind it to be evidence of
			// anything.
			name:    "reached through a proxy, with no proxy",
			src:     viaProxySource("http://shelfmark.internal.invalid/", ""),
			wantSub: "has no proxy",
		},
		{
			name:    "a host name where an address belongs",
			src:     confirmedSource("http://shelfmark.internal.invalid/", "shelfmark.internal.invalid"),
			wantSub: "not an IP address",
		},
		{
			name:    "a public address needs no confirmation",
			src:     confirmedSource("http://shelfmark.internal.invalid/", "93.184.216.34"),
			wantSub: "not an address a source can be confirmed on",
		},
		{
			name:    "loopback is the device itself, not a service on the network",
			src:     confirmedSource("http://shelfmark.internal.invalid/", "127.0.0.1"),
			wantSub: "not an address a source can be confirmed on",
		},
		{
			name:    "the cloud metadata address",
			src:     confirmedSource("http://shelfmark.internal.invalid/", "169.254.169.254"),
			wantSub: "not an address a source can be confirmed on",
		},
		{
			name:    "an address that is not the base URL's own",
			src:     confirmedSource("http://192.168.1.10:8084/", "192.168.1.11"),
			wantSub: "is not baseUrl's address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reg.Validate(tt.src)
			if err == nil {
				t.Fatalf("Validate accepted %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.wantSub)
			}
			// And the same source cannot reach the fetch layer by another door.
			if _, pErr := tt.src.Policy(); pErr == nil {
				t.Fatalf("Policy() accepted %s", tt.name)
			}
		})
	}
}

// TestUndatedConfirmationIsRefused is its own test because the date is the
// part of the record with no security effect, and is therefore the part most
// likely to be dropped as unnecessary. It is what makes the entry a record of
// something that happened rather than a bare flag.
func TestUndatedConfirmationIsRefused(t *testing.T) {
	reg := theme.NewRegistry()
	reg.MustRegister(&fake{id: "alpha"})

	src := confirmedSource("http://shelfmark.internal.invalid/", "192.168.1.10")
	src.SelfHosted.ConfirmedAt = time.Time{}
	err := reg.Validate(src)
	if err == nil || !strings.Contains(err.Error(), "confirmedAt") {
		t.Fatalf("err = %v, want a complaint about confirmedAt", err)
	}
}

// TestAGoodConfirmationIsAccepted is the other side of the two tests above: the
// record the user's own act produces validates, so the refusals are specific
// rather than a blanket "no".
func TestAGoodConfirmationIsAccepted(t *testing.T) {
	reg := theme.NewRegistry()
	reg.MustRegister(&fake{id: "alpha"})

	for _, addr := range []string{"192.168.1.10", "10.0.0.5", "fd00::1", "100.100.0.1"} {
		src := confirmedSource("http://shelfmark.internal.invalid/", addr)
		if err := reg.Validate(src); err != nil {
			t.Fatalf("Validate(%s) = %v, want accepted", addr, err)
		}
		parsed, err := netip.ParseAddr(addr)
		if err != nil {
			t.Fatal(err)
		}
		if !fetch.SelfHostableAddr(parsed) {
			t.Fatalf("%s validated but is not self-hostable", addr)
		}
	}
}
