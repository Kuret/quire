package fetch

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"golang.org/x/net/publicsuffix"
)

// CookieJar carries cookies a source's own server issued, for the requests
// fetch.Client sends back to that same server.
//
// # Why this exists at all, and why it is not a bypass
//
// It exists for exactly one shape of API: a reading grant or a login-free
// session endpoint that answers with a `Set-Cookie`, and asks to see it again
// on the requests that follow. That is a fact the server itself created and
// chose to hand back to us; carrying it forward is bookkeeping, not pretence.
// It is not a way to hold a clearance cookie from a challenge we did not pass,
// or any other credential Quire manufactured or replayed to look like
// something it is not — PLAN §7.6's line against imitating a browser or
// defeating detection is untouched by this type, and if a caller ever finds
// itself using a CookieJar to get past a challenge, that is the bypass §7.6
// forbids and the design is wrong, not this comment.
//
// # Scope: one jar per source, never shared
//
// There is deliberately no client-wide or global jar. fetch.Client is shared
// by every source a user has configured, and a jar on it would let one
// source's session cookie be read back on a request to a completely
// different source — a real cross-source tracking vector, and precisely the
// fault this type exists to avoid. A CookieJar is instead owned by one
// theme.Source (see theme.PolicyFor) and reaches the fetch layer only
// through that source's own fetch.Policy, so two sources never observe the
// same instance even when they share a theme.
//
// # Scope: per-host within the jar
//
// The domain matching within a single jar is net/http/cookiejar's own,
// built on the public suffix list exactly as it is for RFC 6265: a cookie
// set by one host cannot be read back on an unrelated host, even inside the
// same jar, and a server cannot set a cookie for a domain its own request
// did not match. That policy is not added by this package; it is what the
// standard library already enforces, for a bookkeeping reason rather than a
// browser-identity one, and reusing it here does not widen it — a request
// still has to clear fetch.Guard.CheckURL (the SSRF guard, the redirect
// domain check) before it is ever sent, jar or no jar.
//
// # Lifetime
//
// A jar lives only as long as the in-memory theme.Source value that created
// it: created lazily on first use, kept in memory only, and never written to
// state.json or any other file. A restarted app, a re-added source, or a
// second, independently loaded copy of the same source's configuration
// starts with an empty jar. Nothing here needs to survive longer than that —
// the session it carries is only ever a means to keep fetching pages within
// one run, not a credential worth persisting.
type CookieJar struct {
	jar *cookiejar.Jar
}

// NewCookieJar builds an empty CookieJar. It is exported for tests; ordinary
// callers get one from theme.PolicyFor, which creates and reuses exactly one
// per source.
func NewCookieJar() *CookieJar {
	// cookiejar.New's only failure mode is a nil PublicSuffixList, which this
	// call never passes, so the error is always nil.
	j, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &CookieJar{jar: j}
}

// Cookies implements http.CookieJar.
func (c *CookieJar) Cookies(u *url.URL) []*http.Cookie {
	if c == nil || c.jar == nil {
		return nil
	}
	return c.jar.Cookies(u)
}

// SetCookies implements http.CookieJar.
func (c *CookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if c == nil || c.jar == nil {
		return
	}
	c.jar.SetCookies(u, cookies)
}
