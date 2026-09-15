package fetch

// Kind says what a request *is for*. It is the one input that decides whether
// robots.txt gates a request, and it is the caller's to state explicitly.
//
// PLAN §7.4, decided 2026-09-15: robots.txt is the Robots **Exclusion**
// Protocol, and RFC 9309 scopes it to "automatic clients known as crawlers".
// The line is not program-versus-human — a browser is a program too — it is
// discovery versus retrieval:
//
//   - **Discovery**: search, popular/latest listings, following links, the
//     probe's own crawling. Quire is deciding *what exists*. robots applies
//     strictly; a Disallow is a refusal and the request does not happen.
//   - **Retrieval**: one series, chapter or page the user explicitly asked
//     for. This is not crawling, and robots does not gate it.
//
// Three properties of this type matter more than the distinction itself:
//
//  1. **It is never inferred.** No path pattern, no method, no heuristic on the
//     URL. A caller that wants retrieval semantics says so at the call site,
//     where the reason is visible.
//  2. **It is never configurable.** Kind has no JSON tags, appears in no
//     schema, and is not reachable from a Source. The classification is
//     Quire's own; a source entry cannot flip a discovery request into a
//     retrieval one to get past a Disallow.
//  3. **It narrows nothing else.** Rate limits, per-host delays, the honest
//     User-Agent, Retry-After, backoff, the response-size cap, byte accounting
//     and the SSRF guard apply identically to both kinds. Retrieval is not a
//     fast lane; it is the same polite request asking a different question.
//
// The zero value is KindDiscovery, so a caller who says nothing gets the
// strict reading.
type Kind uint8

const (
	// KindDiscovery is a request that explores the site: search results,
	// listings, link following, probing. robots.txt is honoured strictly.
	KindDiscovery Kind = iota

	// KindRetrieval is a request for one thing the user named: a series they
	// opened, a chapter they chose, a page of it. robots.txt does not gate it.
	//
	// The case that forced this, rather than an abstraction invented in
	// advance: MangaDex publishes a documented public API with published rate
	// limits for third-party clients, and disallows `/at-home/` — the endpoint
	// that hands out page images — while allowing `/manga` and the feeds.
	// Under a blanket rule, Quire could search MangaDex and list its chapters
	// but never read one, and by extension could never use any officially
	// supported API whose robots.txt was written for search engines. That is
	// not what the operator is saying.
	KindRetrieval
)

func (k Kind) String() string {
	switch k {
	case KindDiscovery:
		return "discovery"
	case KindRetrieval:
		return "retrieval"
	default:
		return "unknown"
	}
}

// gatedByRobots reports whether a request of this kind must consult robots.txt.
// It is the whole of the policy, in one place, so there is exactly one line to
// read when asking "can this be bypassed?".
func (k Kind) gatedByRobots() bool { return k != KindRetrieval }
