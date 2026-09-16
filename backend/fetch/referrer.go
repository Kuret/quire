package fetch

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Referrer is the page a URL was taken from, for the `Referer` header.
//
// # Why this is allowed at all
//
// PLAN §7.6 forbids pretending to be something we are not: a spoofed
// User-Agent claims to be a browser we are not, a forged TLS fingerprint
// claims a client we are not, a replayed clearance cookie claims a challenge
// we did not pass. Every one of those is a lie told to a server.
//
// A Referer naming the page an image URL was actually extracted from is not a
// lie. It is a true statement about the request, in the header designed to
// carry exactly that fact. Hotlink protection asks "did this come from one of
// our pages?" and Quire's honest answer is yes — it fetched that chapter page
// and took the URL out of its markup. We satisfy the check by telling the
// truth, which is the opposite of circumventing it.
//
// A browser challenge asks "are you a browser?", and there the only way
// through is to lie. That stays refused, stays terminal, and is untouched by
// any of this.
//
// # Why it is a type and not a string
//
// Because the value that keeps this honest is "a page we really fetched", and
// a bare string parameter is an invitation to pass a plausible-looking
// constant. So:
//
//   - The zero Referrer sends **no header**. That is what a caller who has not
//     thought about it gets, and it is the safe answer — Get and GetRetrieval
//     are literally GetFrom and GetRetrievalFrom with a zero value. There is no
//     default, no fallback, and nothing inferred from the URL being fetched.
//   - The value travels **with the request**, as an argument. There is
//     deliberately no client-wide or per-source Referer setting: one would be
//     pinned to a constant within a week, and a constant Referer naming a page
//     we did not fetch is precisely the lie §7.6 forbids.
//   - The preferred constructor is [Response.Referrer], which can only exist
//     because this client fetched that page. [PageReferrer] is the escape for
//     a caller holding the URL rather than the response, and it validates what
//     it can — absolute, http(s), a real host.
//
// None of that can stop a determined caller inventing one. It is meant to make
// the truthful thing the easy thing and the untruthful thing deliberate, which
// is as far as a type can go.
type Referrer struct {
	// url is the already-sanitised value. Empty means "send nothing".
	url string
}

// String returns the URL this Referrer names, or "" for the zero value.
func (r Referrer) String() string { return r.url }

// IsZero reports whether this Referrer names nothing, in which case no header
// is sent.
func (r Referrer) IsZero() bool { return r.url == "" }

// header renders the Referrer as request headers, or nil for the zero value.
func (r Referrer) header() http.Header {
	if r.url == "" {
		return nil
	}
	h := http.Header{}
	h.Set("Referer", r.url)
	return h
}

// Referrer names this response's page, for use as the Referer of a request for
// something extracted from it.
//
// This is the constructor to reach for. It cannot produce a URL that was not
// fetched, because the only way to hold a *Response is to have fetched one,
// and it uses FinalURL rather than the requested URL so that a redirect is
// reflected honestly — the page the markup actually came from is the page to
// name.
func (r *Response) Referrer() Referrer {
	if r == nil || r.FinalURL == nil {
		return Referrer{}
	}
	ref, err := PageReferrer(r.FinalURL.String())
	if err != nil {
		return Referrer{}
	}
	return ref
}

// PageReferrer builds a Referrer from the URL of a page the caller has
// actually fetched.
//
// It exists for callers that hold the page's address rather than its response
// — a theme that knows which URL its Pages() read, for instance. The caller is
// making a claim the type cannot check, so what can be checked is:
//
//   - absolute, with an http or https scheme. A relative or non-web Referer is
//     a caller mistake, not a header worth sending.
//   - a non-empty host.
//
// and what is removed before sending is what should never leave the device:
//
//   - **userinfo**, because a Referer is the classic way credentials end up in
//     someone else's access log. §7.4 rejects credentialled URLs outright and
//     this is the same rule applied one layer out.
//   - **the fragment**, which RFC 9110 says is not sent and which is ours
//     alone in any case.
//
// The query string is kept: it is part of the page's identity, and on more
// than one of the sites this exists for the chapter is identified by nothing
// else.
func PageReferrer(rawurl string) (Referrer, error) {
	u, err := url.Parse(strings.TrimSpace(rawurl))
	if err != nil {
		return Referrer{}, fmt.Errorf("fetch: referrer %q: %w", rawurl, err)
	}
	switch {
	case !u.IsAbs():
		return Referrer{}, fmt.Errorf("fetch: referrer %q: must be absolute", rawurl)
	case u.Scheme != "http" && u.Scheme != "https":
		return Referrer{}, fmt.Errorf("fetch: referrer %q: scheme must be http or https", rawurl)
	case u.Host == "":
		return Referrer{}, fmt.Errorf("fetch: referrer %q: no host", rawurl)
	}

	clean := *u
	clean.User = nil
	clean.Fragment = ""
	clean.RawFragment = ""
	return Referrer{url: clean.String()}, nil
}
