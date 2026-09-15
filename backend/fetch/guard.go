package fetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// The two failure verdicts PLAN §7.5 stage 1 names. Callers match on these
// with errors.Is to turn a guard rejection into a user-facing verdict.
var (
	// ErrInvalidURL covers anything malformed or non-http(s).
	ErrInvalidURL = errors.New("invalid_url")

	// ErrBlockedAddress covers a URL that resolves somewhere we refuse to go,
	// and a redirect that leaves the source's registrable domain.
	ErrBlockedAddress = errors.New("blocked_address")
)

// GuardError explains a rejection in enough detail to show a user. The Reason
// is plain language on purpose (PLAN §6 M3: a verdict is a complete answer,
// not an error code).
type GuardError struct {
	URL    string
	Reason string
	Kind   error // ErrInvalidURL or ErrBlockedAddress
}

func (e *GuardError) Error() string {
	return fmt.Sprintf("fetch: %s: %s (%v)", e.URL, e.Reason, e.Kind)
}

func (e *GuardError) Unwrap() error { return e.Kind }

// Guard implements the PLAN §7.4 SSRF guard. It runs on the initial URL and,
// via http.Client.CheckRedirect, on every redirect hop — because a guard that
// only checks the URL the user typed is not a guard.
type Guard struct {
	// resolve is injectable so tests can exercise every rejection branch
	// without a resolver, a network, or a name that has to exist.
	resolve func(ctx context.Context, host string) ([]net.IP, error)

	// allowLoopback exists only for the in-package tests, which need to talk
	// to an httptest server. It is unexported and set only from
	// export_test.go, so no configuration path can reach it.
	allowLoopback bool
}

// CheckURL applies every guard rule to u. p may be nil, in which case only the
// scheme and address rules apply (there is no base domain to stay within).
func (g *Guard) CheckURL(ctx context.Context, u *url.URL, p *Policy) error {
	if err := g.checkScheme(u); err != nil {
		return err
	}
	if err := g.checkDomain(u, p); err != nil {
		return err
	}
	return g.checkAddress(ctx, u)
}

func (g *Guard) checkScheme(u *url.URL) error {
	if u == nil {
		return &GuardError{URL: "<nil>", Reason: "no URL", Kind: ErrInvalidURL}
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		// file:, gopher:, ftp: and friends are the classic SSRF pivots.
		return &GuardError{
			URL:    u.Redacted(),
			Reason: fmt.Sprintf("only http and https are allowed, not %q", u.Scheme),
			Kind:   ErrInvalidURL,
		}
	}
	if u.Hostname() == "" {
		return &GuardError{URL: u.Redacted(), Reason: "no host in URL", Kind: ErrInvalidURL}
	}
	if u.User != nil {
		// userinfo in a URL is almost always an attempt to make a hostile host
		// look like a familiar one (https://good.example@evil.example/).
		return &GuardError{URL: u.Redacted(), Reason: "credentials in URL are not allowed", Kind: ErrInvalidURL}
	}
	return nil
}

// checkDomain enforces the redirect boundary: a request must stay within the
// source's registrable domain unless allowedHosts says otherwise.
//
// **allowedHosts widens this rule and nothing else.** CheckURL runs
// checkAddress *after* this, unconditionally, so a private, loopback or
// link-local address is refused however it was named and whoever named it —
// a source entry, an import, or a theme's own declared host list. An entry
// here buys a name past the domain boundary; it never buys an address past the
// address rules. That ordering is the whole of the guarantee, so do not
// reorder CheckURL without reading the tests that pin it.
func (g *Guard) checkDomain(u *url.URL, p *Policy) error {
	if p == nil || p.BaseURL == nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	base := strings.ToLower(p.BaseURL.Hostname())
	if host == base {
		return nil
	}
	if RegistrableDomain(host) == RegistrableDomain(base) && RegistrableDomain(host) != "" {
		return nil
	}
	for _, allowed := range p.AllowedHosts {
		if hostMatches(host, strings.ToLower(strings.TrimSpace(allowed))) {
			return nil
		}
	}
	return &GuardError{
		URL:    u.Redacted(),
		Reason: fmt.Sprintf("leaves the source's domain %q; add it to allowedHosts if that is intended", RegistrableDomain(base)),
		Kind:   ErrBlockedAddress,
	}
}

// hostMatches accepts an exact host, or a host under an allowed registrable
// domain. "cdn.example.invalid" is matched by both itself and "example.invalid".
func hostMatches(host, allowed string) bool {
	if allowed == "" {
		return false
	}
	// "*.example.invalid" is the narrower form: subdomains only, never the
	// bare domain. A CDN whose hostnames are all generated labels can say so
	// exactly, rather than permitting a domain it never actually serves from.
	if sub, ok := strings.CutPrefix(allowed, "*."); ok {
		return sub != "" && strings.HasSuffix(host, "."+sub)
	}
	return host == allowed || strings.HasSuffix(host, "."+allowed)
}

// RegistrableDomain returns the eTLD+1 of host, or "" if there isn't one.
// Exported because the probe (PLAN §7.5 stage 2) has to tell the user when a
// redirect changed it.
func RegistrableDomain(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return ""
	}
	if _, err := netip.ParseAddr(host); err == nil {
		// An IP literal has no registrable domain; it is its own boundary.
		return host
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return d
}

// checkAddress resolves the host and rejects the request if *any* address it
// resolves to is one we refuse. Checking every address rather than the first
// is deliberate: a DNS name that returns one public and one private address is
// a rebinding attempt, not a multihomed server worth accommodating.
func (g *Guard) checkAddress(ctx context.Context, u *url.URL) error {
	host := u.Hostname()

	if addr, err := netip.ParseAddr(host); err == nil {
		if reason := g.addrReason(addr); reason != "" {
			return &GuardError{URL: u.Redacted(), Reason: reason, Kind: ErrBlockedAddress}
		}
		return nil
	}

	resolve := g.resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return &GuardError{URL: u.Redacted(), Reason: fmt.Sprintf("cannot resolve %q: %v", host, err), Kind: ErrInvalidURL}
	}
	if len(ips) == 0 {
		return &GuardError{URL: u.Redacted(), Reason: fmt.Sprintf("%q resolves to no addresses", host), Kind: ErrInvalidURL}
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return &GuardError{URL: u.Redacted(), Reason: "unparseable resolved address", Kind: ErrBlockedAddress}
		}
		if reason := g.addrReason(addr.Unmap()); reason != "" {
			return &GuardError{URL: u.Redacted(), Reason: reason, Kind: ErrBlockedAddress}
		}
	}
	return nil
}

// Networks refused outright. netip's predicates cover most of it; these are
// the ranges it has no predicate for but which are just as much "not the
// public internet".
var extraBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, incl. 255.255.255.255
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 — a v4 address in v6 clothing
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("100::/64"),        // discard-only
}

// addrReason returns a plain-language reason to refuse addr, or "" to allow it.
func (g *Guard) addrReason(addr netip.Addr) string {
	if !addr.IsValid() {
		return "invalid IP address"
	}
	addr = addr.Unmap()
	switch {
	case addr.IsUnspecified():
		return "0.0.0.0 and :: are not routable destinations"
	case addr.IsLoopback():
		if g.allowLoopback {
			return ""
		}
		return "loopback addresses are not reachable sources"
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return "link-local addresses are not reachable sources"
	case addr.IsInterfaceLocalMulticast(), addr.IsMulticast():
		return "multicast addresses are not reachable sources"
	case addr.IsPrivate():
		return "private addresses are not reachable sources"
	}
	// 4in6 that survived Unmap (e.g. ::ffff:0:127.0.0.1 forms) and NAT64.
	for _, p := range extraBlocked {
		if p.Contains(addr) {
			if g.allowLoopback && addr.IsLoopback() {
				return ""
			}
			return fmt.Sprintf("%s is in the reserved range %s", addr, p)
		}
	}
	if addr.Is6() {
		// Anything with an embedded v4 address gets the v4 rules too.
		if v4 := addr.As16(); v4[0] == 0x20 && v4[1] == 0x02 { // 2002::/16, 6to4
			embedded := netip.AddrFrom4([4]byte{v4[2], v4[3], v4[4], v4[5]})
			if r := g.addrReason(embedded); r != "" {
				return fmt.Sprintf("6to4 address embedding %s: %s", embedded, r)
			}
		}
	}
	return ""
}

func isRobotsURL(u *url.URL) bool { return u.Path == "/robots.txt" }
