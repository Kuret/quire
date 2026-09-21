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

	// Unresolved marks the one refusal that is not a judgement about the site:
	// the host could not be looked up at all.
	//
	// It is a field rather than a third Kind because it is still an invalid_url
	// verdict and callers matching on Kind must keep working. The probe reads it
	// to tell "this name does not resolve *here*" apart from "this address is
	// one Quire refuses", because on the device those need different questions
	// — measured 2026-09-20, a MagicDNS name on the owner's mesh does not
	// resolve on the tablet at all (its resolver is public DNS, and a userspace
	// VPN installs none), and the proxy that can reach it is exactly what the
	// user would be offered.
	Unresolved bool

	// OffDomain marks the one ErrBlockedAddress refusal that checkDomain
	// raises rather than checkAddress: the URL's host is a perfectly ordinary
	// address, just not one within the source's registrable domain or its
	// declared allowedHosts.
	//
	// It exists so a caller can tell this refusal apart from every other
	// ErrBlockedAddress by structure rather than by matching Reason's text —
	// text is for the person reading it, not for a caller deciding whether to
	// offer a question, and a caller that matched on it would break the
	// moment the wording changed. The probe uses it to raise its "allow this
	// image host" question (PLAN §7.5 stage 5) for exactly this refusal and
	// no other: a private, loopback or link-local address is never OffDomain,
	// however it was named, because checkAddress runs after this check and is
	// not consulted here at all.
	OffDomain bool
}

func (e *GuardError) Error() string {
	return fmt.Sprintf("fetch: %s: %s (%v)", e.URL, e.Reason, e.Kind)
}

func (e *GuardError) Unwrap() error { return e.Kind }

// Guard implements the PLAN §7.4 SSRF guard. It runs on the initial URL and,
// via http.Client.CheckRedirect, on every redirect hop — because a guard that
// only checks the URL the user typed is not a guard.
//
// # What the address rules are actually for
//
// Quire scrapes sites it does not trust, and **those sites' content supplies
// URLs Quire then fetches with nobody watching**: theme.SeriesStub.CoverURL is
// pulled out of a scraped listing page and fetched automatically to draw the
// series grid. A hostile or compromised source can therefore aim Quire at the
// user's own network and read the result's timing, size and success.
//
// The concrete case this was measured against, on 2026-09-20: the owner runs
// Shelfmark on their tailnet, and `GET /api/restart` on it answers **with no
// authentication at all**. A source returning a cover URL of
// `http://<their-shelfmark>:8084/api/restart` would restart that service every
// time a listing was rendered. No credential is stolen and no reply is needed
// — the request *is* the attack. That is what refusing private, loopback,
// link-local and CGNAT addresses buys, and it is why the exemption below is
// scoped to one host the user named rather than to a source, a network or a
// theme.
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
	return g.checkAddress(ctx, u, p)
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
		URL:       u.Redacted(),
		Reason:    fmt.Sprintf("leaves the source's domain %q; add it to allowedHosts if that is intended", RegistrableDomain(base)),
		Kind:      ErrBlockedAddress,
		OffDomain: true,
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
//
// p is consulted for one thing only: whether u's host is the single host the
// user confirmed is their own (selfHosted below).
func (g *Guard) checkAddress(ctx context.Context, u *url.URL, p *Policy) error {
	host := u.Hostname()
	exempt := selfHosted(u, p)

	// A confirmed host reached through the source's own proxy is not resolved
	// here at all, and its addresses are not checked.
	//
	// This is not the address rules being relaxed; it is an admission that they
	// were never meaningful for such a request. With a proxy, **name resolution
	// and connection both happen at the proxy** — nothing Quire resolves here is
	// what gets connected to, so checking it would be a check of a fact about
	// somebody else's resolver, dressed up as a safety property. Pretending
	// otherwise is worse than being explicit.
	//
	// It is also the only thing that makes the measured case work. On the
	// owner's tablet (2026-09-20) `nslookup zima.<mesh>.ts.net` fails against
	// public DNS while `http_proxy=localhost:1055 wget .../api/health` returns
	// {"status":"ok"}: the name exists only inside the mesh, the kernel has no
	// TUN device and so the VPN installs no resolver, and that name will never
	// resolve on this device. A probe that insisted on resolving it first would
	// refuse a source that works.
	//
	// **Three conditions, all of them the user's own doing, and none of them
	// content's.** The URL's host must be the source's own host (so no scraped
	// URL, no allowedHosts entry and no redirect target can be here), the source
	// must carry a proxy the user typed, and the user must have confirmed this
	// source is theirs. A proxy alone does not buy it: an unconfirmed source is
	// resolved and address-checked exactly as before, so a proxy cannot become a
	// way to stop looking. The scheme and domain checks run before this and are
	// untouched.
	if exempt && proxiedOwnHost(u, p) {
		return nil
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if reason := g.addrReason(addr, exempt); reason != "" {
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
		return &GuardError{
			URL: u.Redacted(), Reason: fmt.Sprintf("cannot resolve %q: %v", host, err),
			Kind: ErrInvalidURL, Unresolved: true,
		}
	}
	if len(ips) == 0 {
		return &GuardError{
			URL: u.Redacted(), Reason: fmt.Sprintf("%q resolves to no addresses", host),
			Kind: ErrInvalidURL, Unresolved: true,
		}
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return &GuardError{URL: u.Redacted(), Reason: "unparseable resolved address", Kind: ErrBlockedAddress}
		}
		if reason := g.addrReason(addr.Unmap(), exempt); reason != "" {
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
	cgnat,                                    // CGNAT; see SelfHostableAddr
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

// cgnat is the shared address space of RFC 6598. It is called out by name
// because it is where a tailnet lives, which makes it the range a self-hosted
// service the user reaches over Tailscale is actually on — and it is still in
// extraBlocked, because a *scraped* URL pointing into it is exactly the attack
// on Guard's doc comment.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// SelfHostableAddr reports whether addr is one the guard refuses by default
// but a user may confirm as a service of their own: the RFC 1918 and ULA
// private ranges, and CGNAT.
//
// It is deliberately narrower than "everything addrReason refuses", and the
// blanket version was rejected:
//
//   - loopback is the reMarkable itself, never a service on the user's
//     network, so confirming a host that resolves there says something the
//     user cannot have meant;
//   - link-local covers 169.254.169.254, the cloud metadata service, which is
//     the single most valuable SSRF target there is;
//   - multicast, the unspecified address and the reserved/documentation ranges
//     are not hosts anyone runs a server on.
//
// Exported because theme.Source's stored confirmation is validated against it:
// the set of addresses a confirmation may record and the set the guard will
// honour have to be the same set, and two copies of it would drift.
func SelfHostableAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	return addr.IsPrivate() || cgnat.Contains(addr)
}

// selfHosted reports whether u names the one host the user confirmed is theirs.
//
// The match is on the host and nothing else — exact, case-folded, trailing dot
// removed. It deliberately does not reuse hostMatches: a subdomain of the
// approved host is not the approved host, and a registrable-domain match would
// hand a whole domain's worth of names an address exemption the user agreed to
// for one.
//
// Because the match is per-URL rather than per-source, it survives the two
// places content gets to choose a URL. A cover URL scraped from a page is
// checked as itself, so a cover pointing at any other address on the same
// private network is refused however the source was configured; and a redirect
// is re-checked on every hop with the same policy, so an exemption cannot be
// carried off the approved host by a 302. Redirect targets are content, and an
// exemption that survived one would be precisely the hole this is avoiding.
func selfHosted(u *url.URL, p *Policy) bool {
	if p == nil || p.SelfHostedHost == "" || u == nil {
		return false
	}
	return sameHostname(u.Hostname(), p.SelfHostedHost)
}

// sameHostname is the exact host match both per-source assertions turn on: the
// self-hosted exemption above, and the scope of a per-source proxy (proxy.go).
// Case-folded and with a trailing dot removed, because DNS ignores both; and
// nothing else — no subdomain, no registrable domain — because a permission
// the user gave for one host is for that host.
//
// One function rather than two copies: the exemption and the proxy have to
// cover the same set of URLs, and two spellings of "the same host" would
// eventually disagree about one.
func sameHostname(a, b string) bool {
	a = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(a)), ".")
	b = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(b)), ".")
	return a != "" && a == b
}

// SelfHostableTarget reports the address u's host resolves to, when that
// address is one the user could confirm as a service of their own
// (SelfHostableAddr). ok is false for anything else, including a host that does
// not resolve.
//
// It exists so the probe can *offer* a self-hosted source instead of stopping
// at `blocked_address`: the offer has to name the address plainly ("zima.example
// resolves to 100.79.171.1, which is a private address"), and the confirmation
// it records is the address the probe actually saw rather than a name that will
// be re-resolved later.
//
// **It exempts nothing and relaxes nothing.** It only answers a question. The
// guard still has the last word: a probe that gets a yes puts the confirmed host
// on the policy and runs CheckURL again, so the exemption is granted by the same
// code path it always was.
//
// A host that resolves to a mixture of confirmable and other addresses is
// refused outright rather than offered on the strength of its first answer —
// that shape is a rebinding attempt, and checkAddress refuses it for the same
// reason.
func (g *Guard) SelfHostableTarget(ctx context.Context, u *url.URL) (netip.Addr, bool) {
	if u == nil {
		return netip.Addr{}, false
	}
	host := u.Hostname()
	if host == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		return addr, SelfHostableAddr(addr)
	}

	resolve := g.resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	ips, err := resolve(ctx, host)
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, false
	}
	var first netip.Addr
	for i, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return netip.Addr{}, false
		}
		addr = addr.Unmap()
		if !SelfHostableAddr(addr) {
			return netip.Addr{}, false
		}
		if i == 0 {
			first = addr
		}
	}
	return first, true
}

// addrReason returns a plain-language reason to refuse addr, or "" to allow it.
//
// selfHosted is true only when the caller has already established that this
// URL's host is the one host the user confirmed is a service on their own
// network. It lifts the refusal for the SelfHostableAddr ranges and for
// nothing else — including the 6to4 recursion below, which asks about an
// embedded address rather than the one the user agreed to and so is never
// exempt.
func (g *Guard) addrReason(addr netip.Addr, selfHosted bool) string {
	if !addr.IsValid() {
		return "invalid IP address"
	}
	addr = addr.Unmap()
	if selfHosted && SelfHostableAddr(addr) {
		return ""
	}
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
			if r := g.addrReason(embedded, false); r != "" {
				return fmt.Sprintf("6to4 address embedding %s: %s", embedded, r)
			}
		}
	}
	return ""
}

func isRobotsURL(u *url.URL) bool { return u.Path == "/robots.txt" }
