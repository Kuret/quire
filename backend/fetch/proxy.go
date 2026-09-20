package fetch

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrInvalidProxy is returned by ParseProxyURL. It is a validation failure at
// the point the value is *set*, never at the point it is used: a proxy that is
// only rejected when a download starts is a proxy the user will debug from a
// failed chapter instead of from the field they typed it into.
var ErrInvalidProxy = errors.New("invalid proxy URL")

// ProxySchemes are the proxy kinds Quire accepts, in the order they are offered
// to a user who asks what is allowed.
//
// http and https are the ordinary HTTP proxy shapes (net/http speaks both,
// CONNECT included); socks5 covers an `ssh -D` tunnel, which is the other thing
// people already have. Anything else — socks4, a bare host:port, a
// scheme-less string — is refused rather than guessed at, because a proxy is
// the one setting where a wrong guess sends the request somewhere the user did
// not choose.
var ProxySchemes = []string{"http", "https", "socks5"}

// ParseProxyURL validates a per-source proxy URL and returns it parsed.
//
// # Why a source may need a proxy at all
//
// Measured on the owner's reMarkable, 2026-09-20: the device kernel has **no
// TUN device** — /dev/net does not exist, there is no `tun` in /proc/misc, and
// there is no module to load. A mesh VPN on this hardware can therefore only
// run in userspace, where the daemon itself reaches the network but the kernel
// has no route to it: a ping from the daemon's own tooling succeeds while an
// ordinary `wget` to the same address times out. The way out is the daemon's
// local proxy, and with one listening on localhost:1055 the tablet fetched
// {"status":"ok"} from the instance.
//
// **Nothing here knows about any of that.** A proxy URL is "reach this source
// through here", and it serves an `ssh -D` tunnel, a corporate proxy or a
// reverse proxy on the same terms. Quire names no VPN in this field, in the
// schema or in the UI, and it should stay that way: the moment the field means
// one product it stops describing what it does.
//
// # What is deliberately not checked
//
// The proxy's own address gets none of the guard's address rules. The measured
// case *is* localhost:1055, and a proxy on the loopback interface or on a
// private address is the normal shape rather than the suspicious one. That is
// safe for the same reason the whole feature is: this value can only arrive
// from the user, it is never derived from a page, and the URLs it is allowed to
// carry are scoped to one host the user vouched for (see Policy.Proxy).
//
// Credentials are allowed, unlike in a target URL, where the guard refuses them
// as a disguised host (see checkScheme). A proxy is not a place anyone browses
// to, so there is no familiar-looking host to impersonate, and proxy-auth
// credentials in the URL are how every other tool takes them.
func ParseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: it is empty", ErrInvalidProxy)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProxy, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if !contains(ProxySchemes, scheme) {
		return nil, fmt.Errorf("%w: %q is not a proxy Quire can use; it must start %s",
			ErrInvalidProxy, raw, schemeList())
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("%w: %q names no host", ErrInvalidProxy, raw)
	}
	// A path, query or fragment on a proxy URL is somebody having pasted the
	// address of a *page* served through the proxy. Saying so beats silently
	// dropping it, and net/http would ignore it anyway.
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%w: %q has a path; a proxy is just a host and port, like http://localhost:1055",
			ErrInvalidProxy, u.Redacted())
	}
	u.Scheme = scheme
	return u, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func schemeList() string {
	parts := make([]string, 0, len(ProxySchemes))
	for _, s := range ProxySchemes {
		parts = append(parts, s+"://")
	}
	return strings.Join(parts, ", ")
}

// proxyFor returns the proxy this request must be sent through, or nil for a
// direct connection.
//
// # The scope, which is the whole of the security argument
//
// A proxy applies to **requests whose host is the source's own host, and to
// nothing else**: not to a URL the site's content supplied, not to a host
// allowedHosts widened the boundary for, and not to a redirect that left that
// host. Each hop is judged on its own here, because http.Transport consults
// this per request and the policy travels on the request context, so a 302 off
// the confirmed host loses the proxy at the moment it leaves.
//
// The reason is that a proxy moves two things out of Quire's sight. Name
// resolution and connection both happen **at the proxy**, so the address the
// guard resolved and checked is no longer a description of where the bytes
// actually go. If a content-chosen URL inherited the proxy, the proxy would
// become an SSRF bypass — precisely the hole the self-hosted work was careful
// not to open, where a cover URL scraped out of a listing page reaches a
// service on the user's own network with nobody watching (see Guard).
//
// Scoping to the source's own host keeps the guard meaningful, because that
// host is the one the user typed and vouched for. It is the same host, matched
// the same way, as the self-hosted exemption — exact, case-folded, trailing dot
// removed — though the two settings are independent: a source on a routable
// network may want a proxy, and a self-hosted source on a real interface needs
// none.
func proxyFor(req *http.Request) *url.URL {
	if req == nil || req.URL == nil {
		return nil
	}
	p, _ := req.Context().Value(policyKey{}).(*Policy)
	if p == nil || p.Proxy == nil || p.BaseURL == nil {
		return nil
	}
	if !proxiedOwnHost(req.URL, p) {
		return nil
	}
	return p.Proxy
}

// proxiedOwnHost reports whether u is the source's own host *and* the source
// has a proxy — the exact scope described above.
//
// It is one function because two places depend on the same answer and they must
// not drift: this file decides which requests go through the proxy, and
// guard.go decides which requests are therefore pointless to resolve locally. A
// URL that got one without the other would either be resolved here and
// connected elsewhere, or skipped here and connected here — both of which are
// the confusion this feature has to avoid.
func proxiedOwnHost(u *url.URL, p *Policy) bool {
	if u == nil || p == nil || p.Proxy == nil || p.BaseURL == nil {
		return false
	}
	return sameHostname(u.Hostname(), p.BaseURL.Hostname())
}

// transportProxy is what http.Transport.Proxy is set to. The per-source proxy
// wins for the one host it covers; everything else falls through to
// http.ProxyFromEnvironment, which is what the transport did before this
// existed and is the user's own machine-wide setting rather than anything a
// source can influence.
func transportProxy(req *http.Request) (*url.URL, error) {
	if u := proxyFor(req); u != nil {
		return u, nil
	}
	return http.ProxyFromEnvironment(req)
}
