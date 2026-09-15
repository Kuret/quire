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
	"net/url"
	"strings"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
)

// Theme is the interface of PLAN §7.2, unchanged. Everything a theme needs
// beyond this — overrides validation, capability checks — is an *optional*
// side interface below, so this one stays exactly as the plan specifies it.
type Theme interface {
	ID() string

	// Fingerprint scores a probed page 0..100 for "is this my shape?"
	Fingerprint(p *probe.Page) int

	Search(ctx context.Context, s *Source, q string, page int) ([]SeriesStub, error)
	Series(ctx context.Context, s *Source, id string) (*Series, error)
	Chapters(ctx context.Context, s *Source, id string) ([]Chapter, error)
	Pages(ctx context.Context, s *Source, chapterID string) ([]string, error)
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

// Fetcher is the slice of fetch.Client a theme uses. Themes depend on this
// interface rather than the concrete client so tests can serve committed
// fixtures without a network, a server or a loopback exemption.
type Fetcher interface {
	// Get is a discovery request (PLAN §7.4): a search, a listing, a link
	// being followed. robots.txt gates it strictly.
	Get(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error)

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

	PostForm(ctx context.Context, p *fetch.Policy, rawurl string, form url.Values) (*fetch.Response, error)
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

	// RateLimit narrows the global politeness caps; it can never widen them.
	RateLimit *fetch.RateLimit `json:"rateLimit,omitempty"`

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

// Policy is the fetch-layer view of this source.
func (s *Source) Policy() (*fetch.Policy, error) {
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("theme: source %q: parse baseUrl: %w", s.ID, err)
	}
	return &fetch.Policy{BaseURL: u, AllowedHosts: s.AllowedHosts, RateLimit: s.RateLimit}, nil
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
}

// Series is a full series page.
type Series struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	AltTitles   []string `json:"altTitles,omitempty"`
	CoverURL    string   `json:"coverUrl,omitempty"`
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
type Chapter struct {
	// ID is the theme's handle, site-relative for the same reason as
	// SeriesStub.ID.
	ID    string `json:"id"`
	Title string `json:"title"`

	// Number is the parsed chapter number, or -1 when the title carries none.
	// Float because half-chapters ("10.5") are routine.
	Number float64 `json:"number"`

	// Published is the chapter date, zero when the site gave none or gave it
	// in a format the source's dateFormat override does not describe.
	Published time.Time `json:"published,omitzero"`

	Scanlator string `json:"scanlator,omitempty"`
}
