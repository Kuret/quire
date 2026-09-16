package theme

import "strings"

// SameSite reports whether host belongs to the same site as base.
//
// # Why this exists, and what it cost to find out
//
// Every theme filters the links it parses to "on this site", because a listing
// page is full of navigation, adverts and outbound links, and a theme returns
// *paths* — so a link from another host, rewritten onto the source's base,
// names a resource that does not exist.
//
// Until 2026-09-16 that filter was `host == base.Hostname()` in five themes,
// and the real prober found what it costs: a live madara site was recognised
// with a perfect fingerprint and then **searched to zero results**. The user
// had typed the apex; the site redirects to its `www.` host and emits absolute
// links there; so every single result was discarded as off-site and Search
// returned an empty list. Not an error — an empty list, which reads as "this
// site has nothing" and is the same class of silent wrongness as a volume that
// reads backwards.
//
// # The rule, and why it is not wider
//
// A host matches if it is the base host, or if the two are equal once a
// leading `www.` is removed from either. That is the one equivalence that is
// true by construction: `www.example.com` and `example.com` are the same site,
// and the choice between them is the operator's redirect policy rather than
// anything a user should have to get right when pasting a URL.
//
// It is deliberately **not** "same registrable domain", which was the obvious
// generalisation and is wrong here. Themes discard the host and keep the path,
// so accepting `images.example.com/manga/x` would produce the ID `/manga/x`
// and later fetch it from the *base* host — a different resource, or a 404,
// arrived at silently. The registrable domain is the right boundary for PLAN
// §7.4's redirect guard, which keeps the whole URL; it is the wrong one for a
// rule whose output is a bare path.
//
// A theme that has a second sibling worth accepting — a mobile host serving an
// API, say — composes this with its own rule rather than widening this one.
func SameSite(host, base string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	base = strings.ToLower(strings.TrimSpace(base))
	if host == "" || base == "" {
		return false
	}
	return host == base || strings.TrimPrefix(host, "www.") == strings.TrimPrefix(base, "www.")
}
