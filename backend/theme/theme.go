// Package theme is the source engine: a theme is a Go implementation of one
// site *family's* shape, and a source is a user-supplied instantiation of a
// theme (PLAN §7.2).
//
// Quire ships no sources. The repository contains no site URLs and no default
// catalogue (PLAN §1.3); everything in this package operates on a Source the
// user typed in themselves. Nothing here names, embeds or defaults to a real
// site, and the test fixtures are synthetic pages under example.invalid.
package theme

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
)

// Theme is the interface of PLAN §7.2. Everything a theme needs beyond it —
// overrides validation, capability checks — is an *optional* side interface
// below, so this one stays exactly as the plan specifies it.
//
// It has grown once since M2, and only because the plan grew: AllowedHosts was
// added to §7.2 on 2026-09-15, when MangaDex turned out to serve page images
// from a different registrable domain and the alternative was making users
// discover a CDN's name by reading an SSRF rejection. It is on the required
// interface rather than an optional side one so that every theme has to answer
// the question — nil is a fine answer, but it is an answered one.
type Theme interface {
	ID() string

	// Fingerprint scores a probed page 0..100 for "is this my shape?"
	Fingerprint(p *probe.Page) int

	Search(ctx context.Context, s *Source, q string, page int) ([]SeriesStub, error)
	Series(ctx context.Context, s *Source, id string) (*Series, error)
	// Chapters returns the series' chapters in **ascending reading order**,
	// earliest first — whatever order the site itself lists them in. See
	// order.go: the guarantee is the theme's because only the theme knows
	// what its identifiers mean, and a caller that sorted generically would
	// put "10" before "9" and fling "Omake" to one end.
	//
	// A theme that cannot establish an order says so with MarkOrderUnknown
	// rather than guessing. SortAndMark does both halves for it.
	Chapters(ctx context.Context, s *Source, id string) ([]Chapter, error)
	Pages(ctx context.Context, s *Source, chapterID string) ([]string, error)

	// AllowedHosts lists hosts outside the source's own registrable domain
	// that this theme legitimately needs — in practice, image CDNs.
	//
	// Two forms are accepted: "example.invalid" matches that host and anything
	// under it, and "*.example.invalid" matches subdomains only, never the
	// bare domain. Prefer the second where a CDN's hostnames are generated
	// labels and the domain itself serves nothing.
	//
	// PLAN §7.5 stage 6 seeds the stored source's allowedHosts from this, so
	// the list ends up on the source entry: visible to the user, editable by
	// them, and unchanged if the theme later changes its mind. A user should
	// not have to discover a CDN's name by reading an SSRF rejection.
	//
	// The list lives in the theme's source code precisely so it is reviewable
	// in a diff. That is the difference between "this theme's images come from
	// here" and an arbitrary host list pasted into a config file, which is
	// what PLAN §7.4's boundary exists to constrain.
	//
	// It widens the *registrable-domain boundary* and nothing else. A host
	// named here still has to survive the address rules — private, loopback
	// and link-local ranges stay refused — so a theme cannot name its way onto
	// the local network. fetch.Guard.CheckURL enforces that by running the
	// address check after the domain check, and there are tests on it.
	//
	// Most themes return nil. A family of independently hosted sites has no
	// CDN in common to name, and guessing at one would widen the boundary for
	// every site in the family.
	AllowedHosts() []string

	// SuggestedName is the display name a source of this shape should carry by
	// default, or "" for "no opinion".
	//
	// PLAN §7.5 stage 6 names a new source from the site's <title>, which is
	// the right default for a family of independent sites and is wrong for a
	// single one. The first real use of the mangadex theme produced a source
	// called "MangaDex API documentation" — accurate, since that is what an
	// API root's title says, and meaningless to anyone who did not type the
	// URL themselves.
	//
	// So: a suggestion wins over the title, and "" falls back to it.
	//
	// The three site-family themes return "". madara and mangathemesia each
	// cover hundreds of independently branded sites and have no shared name to
	// offer; for them the page title genuinely is the best available answer,
	// and inventing one would be worse than the title ever is.
	//
	// **This is not a hook for title cleaning.** Stripping " — Home",
	// " | Official Site" or " API documentation" is an unwinnable game that
	// eventually mangles a site whose real name ends in one of those. A theme
	// either knows its site's name outright or has no opinion. Everything else
	// is the user's rename to make.
	//
	// It is on the required interface for the same reason AllowedHosts is:
	// "" is a fine answer, but it should be an answered one.
	SuggestedName() string
}

// OverrideValidator is implemented by a theme that has overrides — which, in
// practice, is all of them. It is separate from Theme so Theme stays literally
// as PLAN §7.2 writes it.
//
// PLAN §7.2: "unknown keys are a validation error, not silently ignored."
// Registry.Validate calls this before a source is ever used.
type OverrideValidator interface {
	// ValidateOverrides checks raw and reports the first problem. It must
	// reject any key the theme does not declare.
	ValidateOverrides(raw map[string]any) error

	// OverrideKeys lists the accepted keys, so an error message and the M3
	// "add a source" UI can both say what is on offer.
	OverrideKeys() []OverrideDoc
}

// SourceValidator is implemented by a theme that needs to check more of a
// source than the registry can see — in practice the generic escape hatch,
// which has a selector vocabulary and a script to compile. Registry.Validate
// calls it last, after its own checks have passed.
type SourceValidator interface {
	Validate(s *Source) error
}

// PageReferrer is implemented by a theme whose image host refuses a request
// that does not say which of the site's pages it came from.
//
// PLAN §7.6, decided 2026-09-16: a truthful `Referer` is permitted, because
// naming the page an image URL was actually extracted from is a true statement
// rather than pretence. Three sites parse perfectly to page URLs and then
// answer 403 on the image host without one.
//
// It is the *theme's* to answer for the same reason the ordering contract is:
// only the theme knows which URL its Pages() read. It is a side interface
// rather than part of Theme because most themes have no such host, and "" is
// the right answer for them.
//
// # The contract, and it is not optional
//
// The returned URL must be **a page this theme actually fetched** in the
// course of producing that chapter's page URLs — for every theme here, the
// exact URL Pages() requested. A URL the theme did not fetch is a fabricated
// Referer, which §7.6 forbids as squarely as it forbids a spoofed User-Agent.
// When a theme cannot say, it returns "" and no header is sent; that is a
// perfectly good answer and is what the zero fetch.Referrer exists for.
//
// It is a pure function of the source and the chapter ID rather than state
// left behind by the last Pages() call, so that it cannot go stale, cannot be
// read for the wrong chapter, and cannot differ between two callers. Every
// implementation here is the same one line its Pages() uses to build its own
// request.
type PageReferrer interface {
	// PageReferer returns the absolute URL of the page whose markup names the
	// page images of chapterID, or "" when the theme has none to name.
	PageReferer(s *Source, chapterID string) string
}

// PageRefererFor resolves the Referer to send with chapterID's page images.
//
// It is the single place the PageReferrer side interface is consulted, so that
// both call sites — the download queue and the probe's stage 5 image fetch —
// answer the question the same way. A theme that does not implement
// PageReferrer, or that returns "" because it has no page to name, yields the
// zero fetch.Referrer and therefore **no header at all**: PLAN §7.6 is explicit
// that "no header" is the correct answer when we do not know, because a Referer
// naming a page we did not fetch is a lie.
//
// A non-empty value that fetch.PageReferrer refuses is a bug in the theme, not
// a reason to invent something: the error is returned so the caller can say so,
// and the Referrer handed back is the zero one, which sends nothing.
func PageRefererFor(th Theme, s *Source, chapterID string) (fetch.Referrer, error) {
	pr, ok := th.(PageReferrer)
	if !ok {
		return fetch.Referrer{}, nil
	}
	raw := pr.PageReferer(s, chapterID)
	if raw == "" {
		return fetch.Referrer{}, nil
	}
	ref, err := fetch.PageReferrer(raw)
	if err != nil {
		return fetch.Referrer{}, err
	}
	return ref, nil
}

// CoverRefererFrom resolves the Referer to send with a cover image.
//
// It is PageRefererFor one layer up, for the other thing an image host refuses
// without a `Referer`: the cover thumbnails on the series grid. The difference
// is where the value comes from. A chapter's page referrer is a pure function
// of the chapter ID, so it can be asked for; a cover has no such handle — the
// same URL may be reached from a listing or from a series page — so the theme
// **carries it on the data**, in SeriesStub.CoverReferrer and
// Series.CoverReferrer, naming the page that response was parsed out of.
// Reconstructing it at fetch time would be the guessing PLAN §7.6 forbids.
//
// The contract is the same as PageReferrer's and is not optional: raw must be
// a page this theme actually fetched while producing that cover URL. A theme
// with nothing to name leaves the field empty, which yields the zero
// fetch.Referrer and therefore **no header at all** — the correct answer when
// we do not know, and the reason mangadex, whose cover host asks for nothing,
// is untouched by any of this.
//
// A non-empty value fetch.PageReferrer refuses is a bug in the theme rather
// than a reason to invent something: the error comes back so the caller can
// say so, and the Referrer handed back is the zero one, which sends nothing.
func CoverRefererFrom(raw string) (fetch.Referrer, error) {
	if raw == "" {
		return fetch.Referrer{}, nil
	}
	ref, err := fetch.PageReferrer(raw)
	if err != nil {
		return fetch.Referrer{}, err
	}
	return ref, nil
}

// Fetcher is the slice of fetch.Client a theme uses. Themes depend on this
// interface rather than the concrete client so tests can serve committed
// fixtures without a network, a server or a loopback exemption.
type Fetcher interface {
	// Get is a discovery request (PLAN §7.4): a search, a listing, a link
	// being followed. robots.txt gates it strictly.
	Get(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error)

	// GetFrom is Get with a Referer naming the page rawurl was taken from. A
	// zero fetch.Referrer sends no header, so Get above is exactly this call
	// with one.
	//
	// It is on this interface rather than reached for by asserting the
	// concrete client, because an optional assertion would let a test fake
	// injected as a Fetcher silently miss it — and the case that matters most
	// here is the *absence* of a header, which would then be untested by
	// accident rather than guaranteed by rule. See PageRefererFor.
	GetFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error)

	// GetRetrieval is a request for one thing the user named. Every other
	// invariant — rate limits, the honest UA, Retry-After, the size cap, the
	// SSRF guard — applies exactly as it does to Get; only the robots gate
	// differs, because RFC 9309 scopes robots to crawlers.
	//
	// A theme should reach for this only where the call is genuinely the
	// user's own request, and should say why at the call site. Most themes
	// never need it: it exists because MangaDex disallows the endpoint that
	// serves page images while allowing everything used to find them.
	GetRetrieval(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error)

	// GetRetrievalFrom is GetRetrieval with a Referer naming the page rawurl
	// was taken from. A zero fetch.Referrer sends no header. It is what the
	// download queue fetches page images with; see PageRefererFor.
	GetRetrievalFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error)

	PostForm(ctx context.Context, p *fetch.Policy, rawurl string, form url.Values) (*fetch.Response, error)
}

// DiscoveryClassifier is implemented by a theme that can hand back a copy of
// itself whose every request is classified as discovery (PLAN §7.4).
//
// It exists for PLAN §12.2's watched-series check. That check asks a theme for
// a chapter list, which for a series the *user tapped* is retrieval — one named
// resource they asked for. Automatically, on every app open, for forty series
// at once, it is not: it is repeated and unattended, which is the behaviour
// §7.4's exception was written to exclude. The exception was granted on the
// condition that it stay narrow, and "the user watched this series once" is
// exactly the reasoning that would swallow it.
//
// Classification is Quire's own and is made per request-kind inside the fetch
// layer (§7.4), never inferred from the URL — so downgrading it has to happen
// where the request kind is chosen, which is the theme's fetcher. Hence a copy
// of the theme rather than a flag on the call.
//
// A caller that needs the guarantee should *require* this interface rather than
// falling back to the theme as it stands. A theme that does not implement it is
// a theme that has not answered the question, and taking silence for "it never
// retrieves anything" is how the exception stops being narrow.
type DiscoveryClassifier interface {
	// DiscoveryOnly returns an equivalent theme that never issues a retrieval-
	// classified request. The receiver is left alone.
	DiscoveryOnly() Theme
}

// FileTheme is implemented by a theme whose chapters are finished files rather
// than page images.
//
// Every other theme in this package ends at Pages(): a chapter is a list of
// image URLs, which M4 fetches, resizes and assembles into a PDF. A theme that
// drives a book service has none of that — the thing the user chose is already
// an epub or a pdf, and there is nothing to assemble. Such a theme returns an
// error from Pages (an empty slice would read as "no pages found", which is a
// different and misleading claim) and implements this instead.
//
// It is a side interface rather than part of Theme for the same reason
// PageReferrer and DiscoveryClassifier are: it describes a property most themes
// do not have, and a required method that eight themes answered "not me" to
// would be noise on the interface PLAN §7.2 specifies.
//
// # Why a URL and not bytes
//
// Retrieve could hand back the file itself; it deliberately does not. Returning
// a URL keeps the transfer inside the ordinary guarded client — the SSRF guard,
// the rate limiter, the honest User-Agent, Retry-After and the response size
// cap all still apply to it, exactly as they do to a page image. A theme that
// returned bytes would have had to fetch them by some other route, and PLAN
// §7.4's boundary is not something to have two of.
type FileTheme interface {
	// Retrieve starts the retrieval of one chapter and blocks until the file
	// can be fetched, reporting progress as it goes. It returns a URL the
	// caller fetches through the ordinary guarded client, and the name to give
	// the document.
	//
	// progress is called with short, user-facing notes; it may be nil, and an
	// implementation must tolerate that. The call can legitimately take
	// minutes, so the context is the caller's way out and must be honoured.
	Retrieve(ctx context.Context, s *Source, chapterID string, progress func(note string)) (fileURL, filename string, err error)
}

// DiscoveryFetcher wraps f so that retrieval requests are made as discovery
// instead. It is what a theme's DiscoveryOnly is expected to be built from.
//
// Nothing else changes: rate limits, the honest User-Agent, Retry-After, the
// size cap and the SSRF guard are all in the fetch layer and apply to both
// kinds identically. The only difference is that robots.txt now gates every
// request — which is the stricter direction, and the point.
func DiscoveryFetcher(f Fetcher) Fetcher {
	if f == nil {
		return nil
	}
	if d, ok := f.(discoveryFetcher); ok {
		return d // already downgraded; wrapping twice buys nothing
	}
	return discoveryFetcher{Fetcher: f}
}

type discoveryFetcher struct{ Fetcher }

func (d discoveryFetcher) GetRetrieval(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	return d.Fetcher.Get(ctx, p, rawurl)
}

func (d discoveryFetcher) GetRetrievalFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error) {
	return d.Fetcher.GetFrom(ctx, p, rawurl, from)
}

// Source is a configured source, stored on device and validated against
// schema/source.schema.json. The JSON tags and the schema are kept in step by
// TestSourceRoundTripsAgainstSchema.
type Source struct {
	// ID is a stable local identifier, never derived from the URL, so a source
	// can be re-pointed without orphaning downloaded volumes.
	ID string `json:"id"`

	// Name is the display name, defaulted from the site <title> at probe time.
	Name string `json:"name"`

	// Lang is a BCP-47 tag for the source's content.
	Lang string `json:"lang"`

	// Theme is the registered theme ID driving this source.
	Theme string `json:"theme"`

	// BaseURL is the site root, scheme included.
	BaseURL string `json:"baseUrl"`

	// Overrides are the per-theme, schema-declared knobs. Unknown keys are a
	// validation error (PLAN §7.2).
	Overrides map[string]any `json:"overrides,omitempty"`

	// Selectors and Script are the generic theme's escape hatch, and are legal
	// *only* when Theme == "generic". The JSON schema enforces that with an
	// allOf clause; Validate enforces it again in Go, because a source can
	// reach this struct from an import that never met the schema.
	Selectors map[string]string `json:"selectors,omitempty"`
	Script    string            `json:"script,omitempty"`

	// AllowedHosts are extra hosts a redirect may land on (PLAN §7.4).
	AllowedHosts []string `json:"allowedHosts,omitempty"`

	// SelfHosted records that the user told Quire this source is a service on
	// their own network, and is what lets its base host resolve into a private
	// or CGNAT range that fetch.Guard would otherwise refuse. Nil — the normal
	// case, and the case for every source anyone adds from the web — means the
	// address rules apply unchanged.
	SelfHosted *SelfHosted `json:"selfHosted,omitempty"`

	// RateLimit narrows the global politeness caps; it can never widen them.
	RateLimit *fetch.RateLimit `json:"rateLimit,omitempty"`

	// SplitStrips overrides vertical-scroll strip detection (PLAN §12.3):
	// "auto", "never" or "always". Empty means the schema default, "auto".
	//
	// It is a string rather than an imageproc.SplitMode so this package need
	// not depend on the image pipeline for an enum. The three spellings live in
	// exactly one place — imageproc.ParseSplitMode — and
	// TestSplitStripsEnumAgreesEverywhere holds the schema, this field's
	// validation and that function to the same list.
	SplitStrips string `json:"splitStrips,omitempty"`

	// Enabled is the per-source toggle. A nil pointer means the schema default
	// of true; it is a pointer so "absent" and "false" stay distinguishable
	// across an export/import round trip.
	Enabled *bool `json:"enabled,omitempty"`

	AddedAt time.Time `json:"addedAt"`

	// LastProbe is the result of the most recent probe, re-run on failure
	// (PLAN §6 M7) so a site that switched themes is reported as such rather
	// than as "no results found".
	LastProbe *ProbeResult `json:"lastProbe,omitempty"`
}

// SelfHosted is a user's confirmation that a source runs on their own network.
//
// # Why this is a record and not a boolean
//
// fetch.Guard refuses private, loopback, link-local and CGNAT addresses because
// a scraped page supplies URLs Quire then fetches unattended — see the comment
// on fetch.Guard for the measured case, an unauthenticated `/api/restart` on
// the owner's own tailnet. The one thing that reasoning does not cover is a
// host the *user themselves typed*, and this is that exception, written down.
//
// So it carries the evidence of the act rather than just its outcome:
// ConfirmedAddr is the address the host resolved to when the user agreed, and
// ConfirmedAt is when. Validate refuses the whole source if that evidence is
// missing or does not describe an address the exemption could ever cover
// (fetch.SelfHostableAddr), which is what makes the flag impossible to set
// *implicitly*: there is nothing here a probed page, a theme, a redirect or a
// one-word edit to sources.json can produce by accident, and no code path in
// Quire writes it except state.Store.ConfirmSelfHosted, which a person has to
// call with an address in hand.
//
// **It is not re-checked against the live resolution at fetch time.** That was
// considered and rejected: addresses on a home network move with a DHCP lease
// or a tailnet re-key, and pinning to the recorded one would turn the feature
// into a support burden for no gain the host scoping does not already give —
// the exemption is already confined to one host, so hostile DNS for it can
// only reach what the user pointed Quire at in the first place. The record's
// job is to show what was agreed, not to be the check.
type SelfHosted struct {
	// ConfirmedAddr is the address the source's host resolved to at the moment
	// the user approved it, as text ("192.168.1.10", "fd00::1", "100.100.0.1").
	ConfirmedAddr string `json:"confirmedAddr"`

	// ConfirmedAt is when the user approved it.
	ConfirmedAt time.Time `json:"confirmedAt"`
}

// validateSelfHosted checks the confirmation record on s, if there is one. A
// source whose record does not hold up is refused outright rather than having
// the exemption quietly dropped: a half-legible record of consent to reach the
// local network is not something to interpret generously.
func (s *Source) validateSelfHosted() error {
	if s.SelfHosted == nil {
		return nil
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(s.SelfHosted.ConfirmedAddr))
	if err != nil {
		return fmt.Errorf("theme: source %q: selfHosted.confirmedAddr %q is not an IP address: %w",
			s.ID, s.SelfHosted.ConfirmedAddr, err)
	}
	if !fetch.SelfHostableAddr(addr) {
		// Either the address needs no confirmation (it is public), or it is one
		// no confirmation can cover (loopback, link-local, multicast). Both are
		// a record of something other than "this is my server", so neither is
		// accepted as one.
		return fmt.Errorf("theme: source %q: selfHosted.confirmedAddr %s is not an address a source can be confirmed on; "+
			"only private and CGNAT addresses can be", s.ID, addr)
	}
	if s.SelfHosted.ConfirmedAt.IsZero() {
		return fmt.Errorf("theme: source %q: selfHosted.confirmedAt is missing; a confirmation records when it was given", s.ID)
	}
	// When the user typed an address rather than a name there is nothing to
	// resolve, so the record can be checked against the source outright.
	if u, err := url.Parse(s.BaseURL); err == nil {
		if host, hErr := netip.ParseAddr(u.Hostname()); hErr == nil && host.Unmap() != addr.Unmap() {
			return fmt.Errorf("theme: source %q: selfHosted.confirmedAddr %s is not baseUrl's address %s", s.ID, addr, host)
		}
	}
	return nil
}

// ProbeResult is the stored outcome of PLAN §7.5.
type ProbeResult struct {
	// Verdict is the §7.5 enum. "blocked_challenge" is terminal.
	Verdict string `json:"verdict"`

	At time.Time `json:"at"`

	// Detail is plain language, shown to the user as-is.
	Detail string `json:"detail,omitempty"`

	// ThemeScores are the stage-4 fingerprint scores, kept so an
	// "unrecognised" verdict can name what was tried.
	ThemeScores map[string]int `json:"themeScores,omitempty"`
}

// The PLAN §7.5 verdict enum, spelled out so callers do not retype strings.
const (
	VerdictOK               = "ok"
	VerdictPartial          = "partial"
	VerdictUnrecognised     = "unrecognised"
	VerdictBlockedChallenge = "blocked_challenge"
	VerdictRobotsDenied     = "robots_denied"
	VerdictUnreachable      = "unreachable"
	VerdictInvalidURL       = "invalid_url"
	VerdictBlockedAddress   = "blocked_address"
)

// IsEnabled applies the schema's default of true.
func (s *Source) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// SplitStripsValues are the accepted spellings of Source.SplitStrips, in the
// schema's order. Empty is also accepted and means the default, "auto".
var SplitStripsValues = []string{"auto", "never", "always"}

// validSplitStrips reports whether v is one of SplitStripsValues, or empty.
func validSplitStrips(v string) bool {
	if v == "" {
		return true
	}
	return slices.Contains(SplitStripsValues, v)
}

// Policy is the fetch-layer view of this source.
func (s *Source) Policy() (*fetch.Policy, error) {
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("theme: source %q: parse baseUrl: %w", s.ID, err)
	}
	p := &fetch.Policy{BaseURL: u, AllowedHosts: s.AllowedHosts, RateLimit: s.RateLimit}
	if s.SelfHosted != nil {
		// Checked here as well as in Registry.Validate, and it fails the whole
		// Policy rather than dropping the field: this is the last point before
		// the guard, and it is reached by sources that arrived from an import or
		// a hand-edited file as well as from Add.
		if err := s.validateSelfHosted(); err != nil {
			return nil, err
		}
		// The exemption covers the source's own host and only that host, taken
		// from BaseURL rather than stored as a second copy of the name — one
		// name that can disagree with the other is worse than none.
		//
		// The corollary, for whoever adds the re-point PLAN §7.2 keeps hinting
		// at: **re-pointing a confirmed source must clear SelfHosted**, because
		// the confirmation was given for the host that is being replaced.
		// Nothing re-points a source today, and the IP-literal check in
		// validateSelfHosted catches the case where the host is an address.
		p.SelfHostedHost = u.Hostname()
	}
	return p, nil
}

// Resolve turns a possibly relative href from a page into an absolute URL
// string against the source's base. Themes store *relative* IDs wherever they
// can, so a source that is later re-pointed at a mirror keeps working.
func (s *Source) Resolve(ref string) (string, error) {
	base, err := url.Parse(s.BaseURL)
	if err != nil {
		return "", fmt.Errorf("theme: source %q: parse baseUrl: %w", s.ID, err)
	}
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", fmt.Errorf("theme: source %q: parse %q: %w", s.ID, ref, err)
	}
	return base.ResolveReference(r).String(), nil
}

// SeriesStub is a search result: the least we need to show a row and fetch the
// rest on demand.
type SeriesStub struct {
	// ID is the theme's handle for the series. It is opaque to callers; every
	// theme uses a site-relative path so it survives a base-URL change.
	ID       string `json:"id"`
	Title    string `json:"title"`
	CoverURL string `json:"coverUrl,omitempty"`

	// CoverReferrer is the absolute URL of the page CoverURL was extracted
	// from, or "" when the theme has none to name. See CoverRefererFrom.
	CoverReferrer string `json:"coverReferrer,omitempty"`
}

// Series is a full series page.
type Series struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	AltTitles []string `json:"altTitles,omitempty"`
	CoverURL  string   `json:"coverUrl,omitempty"`

	// CoverReferrer is the absolute URL of the page CoverURL was extracted
	// from, or "" when the theme has none to name. See CoverRefererFrom.
	CoverReferrer string `json:"coverReferrer,omitempty"`

	Description string   `json:"description,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	Artists     []string `json:"artists,omitempty"`
	Genres      []string `json:"genres,omitempty"`

	// Status is normalised to one of the Status constants; a value the theme
	// does not recognise becomes StatusUnknown rather than being passed
	// through, so the UI never has to render a site's own vocabulary.
	Status string `json:"status,omitempty"`
}

// Normalised series statuses.
const (
	StatusUnknown   = ""
	StatusOngoing   = "ongoing"
	StatusCompleted = "completed"
	StatusHiatus    = "hiatus"
	StatusCancelled = "cancelled"
)

// Chapter is one chapter in a series.
//
// A slice of these is returned in **ascending reading order, earliest first**
// (PLAN §7.2). See order.go for why that guarantee lives in the theme and what
// happens when it cannot be met.
type Chapter struct {
	// ID is the theme's handle, site-relative for the same reason as
	// SeriesStub.ID.
	ID    string `json:"id"`
	Title string `json:"title"`

	// Volume is the source's own volume label, "" when it publishes none.
	//
	// It is a label rather than a number because that is what sites give:
	// "3", "Vol. 3", "TBD". PLAN §6 M4 groups chapters into volume PDFs and
	// falls back to runs of ten when a source has no volume structure — this
	// field is what tells it which case it is in, and the fallback should be
	// the exception rather than, as it was until 2026-09-15, the only
	// mechanism.
	Volume string `json:"volume,omitempty"`

	// Number is the parsed chapter number, or -1 when the title carries none.
	// Float because half-chapters ("10.5") are routine.
	Number float64 `json:"number"`

	// OrderUnknown says Quire could not establish a reading order for the list
	// this chapter came from, so the list is in the site's own order and that
	// order means nothing to us.
	//
	// It is set on every chapter of such a list, because it describes the list
	// and not the chapter; OrderIsKnown reads it back at that level. A caller
	// assembling several chapters into one PDF must consult it rather than
	// assume — a volume whose pages run backwards is the bug this exists to
	// prevent, and admitting the order is unknown is better than guessing it.
	OrderUnknown bool `json:"orderUnknown,omitempty"`

	// Published is the chapter date, zero when the site gave none or gave it
	// in a format the source's dateFormat override does not describe.
	Published time.Time `json:"published,omitzero"`

	Scanlator string `json:"scanlator,omitempty"`
}
