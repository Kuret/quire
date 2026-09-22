package globalcomix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
)

// The shapes below are transcribed from responses observed live against
// https://api.globalcomix.com on 2026-09-21/22 (PLAN §1.4: endpoint shapes are
// facts, not expression). Only the fields Quire reads are declared.

// envelope is the response shape every endpoint on this API shares: a `meta`
// block naming the HTTP-equivalent code and the request's own params, a
// `payload.results` holding the actual answer, and — on failure — a short
// machine-readable `error` string such as "ROUTE_NOT_FOUND" or
// "CLIENT_HEADER_MISSING", which is worth surfacing verbatim because it is
// more informative than the bare status code.
type envelope[T any] struct {
	Meta struct {
		Code int `json:"code"`
	} `json:"meta"`
	Payload struct {
		Results T `json:"results"`
	} `json:"payload"`
	Error string `json:"error,omitempty"`
}

// searchResults is /v1/search/query's payload. One request answers three
// entity kinds at once; this theme only ever reads the "series" one, which is
// GlobalComix's word for what theme.Theme calls a series.
type searchResults struct {
	Series struct {
		Total int          `json:"total"`
		Items []searchItem `json:"items"`
	} `json:"series"`
}

type searchItem struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	CoverImageURL string `json:"cover_image_url"`
	ArtistName    string `json:"artist_name"`
}

// artist is the creator record embedded in a series or a release. RomanName is
// preferred over Name when present: GlobalComix stores the searchable slug-ish
// form ("dc") in Name and the display form ("DC") in RomanName.
type artist struct {
	Name      string `json:"name"`
	RomanName string `json:"roman_name"`
}

func (a *artist) displayName() string {
	if a == nil {
		return ""
	}
	if n := strings.TrimSpace(a.RomanName); n != "" {
		return n
	}
	return strings.TrimSpace(a.Name)
}

// seriesResult is /v1/comics/{id}'s payload.
type seriesResult struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	StatusName   string  `json:"status_name"`
	CategoryName string  `json:"category_name"`
	ImageURL     string  `json:"image_url"`
	Artist       *artist `json:"artist"`
}

// release is one entry of /v1/comics/{id}/releases's payload, which is a bare
// array rather than a nested object.
//
// Order is GlobalComix's own reading-order integer — the position the API
// itself lists releases in — and Chapter is the same axis spelled as the
// number the site displays; the two agree on every release seen live. There is
// no separate volume concept: ChapterType ("v", "e", "c", …) says what kind of
// unit a release is, not which volume it belongs to, so theme.Chapter.Volume
// is left empty rather than populated with something that is not a volume
// label.
type release struct {
	ID            int     `json:"id"`
	Key           string  `json:"key"`
	Chapter       string  `json:"chapter"`
	Title         string  `json:"title"`
	Order         int     `json:"order"`
	PublishedTime string  `json:"published_time"`
	Artist        *artist `json:"artist"`
}

// readerCDNAccess is readV3's ticket to the page-image host: a short-lived
// grant naming the base URL, the URL template to fill in per page, and the
// release it is valid for.
//
// # Why a page image fetch needs the cookie jar
//
// The grant is not a bearer token in the URL — despite BaseURL and
// URLTemplate looking self-sufficient, the reader-CDN answers 401
// {"error":"auth_required"} to a request carrying only these. What actually
// authorises it is a `gc_reader_auth` cookie, a signed JWT the *readV3
// response itself sets via Set-Cookie*, scoped to exactly this release's path
// (confirmed live 2026-09-22: the same request without the cookie is refused,
// and with it — literally as a `Cookie:` header, nothing else worked, not a
// query parameter and not `Authorization: Bearer` — the reader-CDN answers
// 200 with real image bytes).
//
// That is exactly the shape theme.CookieUser exists for: Theme implements it
// (see UsesCookies in globalcomix.go), so readV3's request and every page
// image fetch that follows share one theme.PolicyFor Policy and therefore one
// fetch.CookieJar. The readV3 response's Set-Cookie lands in that jar, and
// the reader-CDN request that follows presents it automatically — no code
// here copies the cookie by hand, which is the point: it is bookkeeping the
// fetch layer already does for any two requests sharing a Policy.
type readerCDNAccess struct {
	BaseURL     string `json:"base_url"`
	ReleaseKey  string `json:"release_key"`
	URLTemplate string `json:"url_template"`
	PageCount   int    `json:"page_count"`
}

// readResult is /v1/readV3/{releaseKey}'s payload — the one Pages() reads.
type readResult struct {
	Key             string            `json:"key"`
	PageCount       int               `json:"page_count"`
	ReaderCDNAccess *readerCDNAccess  `json:"reader_cdn_access"`
	PageObjects     []json.RawMessage `json:"page_objects"`
}

// clientHeaderName is the request header every endpoint on this API requires
// — confirmed live 2026-09-22: every call, including the otherwise
// unauthenticated /v1/app/init, answers 400 {"error":"CLIENT_HEADER_MISSING"}
// without it, and no query-string form of it is accepted (tried "client",
// "api_key" and the header's own name as a parameter; all three still report
// the header missing).
//
// It is not a login credential: it is served in plain text, unauthenticated,
// to every visitor inside the page's own HTML (window.gc.global.api_key),
// which is how this value was found at all. It identifies the *client
// application* (GlobalComix's own web build) rather than a user or an
// account, which is exactly the shape theme.SourceHeaders exists for — see
// Theme.SourceHeaders in globalcomix.go, the single place this header is
// attached.
const clientHeaderName = "X-Gc-Client"

// defaultClientHeaderValue is the value window.gc.global.api_key carried on
// https://globalcomix.com/ on 2026-09-22 (the same page load that confirmed
// clientHeaderName above). It is a public identifier the site's own web
// client sends on every request, not a secret — the same standard PLAN §1.4
// already applies to every other observed shape in this package.
//
// # Why a constant, and not left out the way the header itself was
//
// The header cannot be *discovered* by this theme's own requests — it is what
// gets a request past CLIENT_HEADER_MISSING in the first place, a chicken
// short of an egg — so there is nowhere else for it to come from. A constant
// is also the honest description of what it is: one fixed, current value read
// out of the client's own shipped config, exactly like apiHost and
// siteDomain above.
//
// # What happens when it rotates
//
// Nothing here can detect a rotation in advance, and nothing here should
// pretend to: it is GlobalComix's own client-application identifier, tied to
// their release cadence, not Quire's. When it rotates, every request this
// theme sends starts answering 400 CLIENT_HEADER_MISSING (or an equivalent
// "client" error — envelope.Error is surfaced verbatim in every error this
// package returns), which is a visible, specific failure rather than a silent
// one: a probe re-run reports it in ProbeResult.Detail, and a chapter that
// stops downloading reports the same string.
//
// clientHeaderOverride below is the user's way out in that moment: it needs
// no code change and no Quire release, only whatever value the site's own
// window.gc.global.api_key carries when the user next loads globalcomix.com —
// the exact fact this constant already is, just fetched again. That is
// deliberately the same escape hatch KeyPageQuality's override give a
// different piece of per-site variation, not a new mechanism invented for
// this one value.
const defaultClientHeaderValue = "gck_b4d492261ec541eda44ce41de79da424"

// clientHeaderOverride is the override key documented in defaultClientHeaderValue's
// comment: an empty value (the default for every source) means "use the
// built-in constant", and a non-empty one replaces it outright rather than
// merging with it — there is exactly one client identifier a request can
// carry, so there is nothing to merge.
const clientHeaderOverride = "clientKey"

// clientHeaderFor resolves the header value SourceHeaders sends: the
// override when the source's config carries one, the built-in default
// otherwise.
func clientHeaderFor(s *theme.Source) (string, error) {
	ov, err := spec.Resolve(s.Overrides)
	if err != nil {
		return "", err
	}
	if v := strings.TrimSpace(ov.String(clientHeaderOverride)); v != "" {
		return v, nil
	}
	return defaultClientHeaderValue, nil
}

// getJSON performs a discovery GET and decodes the envelope.
//
// th is threaded through to theme.PolicyFor rather than s.Policy() being
// called directly, so the request carries both opt-in capabilities this
// theme declares: the client header (theme.SourceHeaders) every endpoint on
// this API requires, and — when Pages() is the caller — the reading-grant
// cookie (theme.CookieUser) the reader-CDN needs.
func getJSON[T any](ctx context.Context, th theme.Theme, f theme.Fetcher, s *theme.Source, u string) (T, error) {
	var zero T
	p, err := theme.PolicyFor(th, s)
	if err != nil {
		return zero, err
	}
	res, err := f.Get(ctx, p, u)
	if err != nil {
		return zero, fmt.Errorf("globalcomix: %s: %w", redact(u), err)
	}
	return decodeEnvelope[T](res, u)
}

// getJSONRetrieval performs a *retrieval* GET and decodes the envelope. It
// exists for readV3, which Pages() reaches for because the user opened a
// specific chapter — see the comment at that call site.
func getJSONRetrieval[T any](ctx context.Context, th theme.Theme, f theme.Fetcher, s *theme.Source, u string) (T, error) {
	var zero T
	p, err := theme.PolicyFor(th, s)
	if err != nil {
		return zero, err
	}
	res, err := f.GetRetrieval(ctx, p, u)
	if err != nil {
		return zero, fmt.Errorf("globalcomix: %s: %w", redact(u), err)
	}
	return decodeEnvelope[T](res, u)
}

func decodeEnvelope[T any](res *fetch.Response, u string) (T, error) {
	var zero T
	var env envelope[T]
	if err := json.Unmarshal(res.Body, &env); err != nil {
		if res.StatusCode != http.StatusOK {
			return zero, fmt.Errorf("globalcomix: %s: HTTP %d", redact(u), res.StatusCode)
		}
		return zero, fmt.Errorf("globalcomix: %s: decode response: %w", redact(u), err)
	}
	if env.Error != "" {
		return zero, fmt.Errorf("globalcomix: %s: %s (HTTP %d)", redact(u), env.Error, res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("globalcomix: %s: HTTP %d", redact(u), res.StatusCode)
	}
	return env.Payload.Results, nil
}

// redact keeps a search term out of an error message; it is the user's own
// words and does not belong in a log line.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	return u.String()
}
