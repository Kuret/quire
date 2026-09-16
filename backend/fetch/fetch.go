// Package fetch is the only way Quire talks HTTP.
//
// It implements the PLAN §7.4 invariants, all of which are stated there as
// non-negotiable and *not configurable by a source entry*:
//
//   - global and per-host concurrency caps, and a minimum inter-request delay
//     per host (limiter.go);
//   - robots.txt fetched, cached and honoured for *discovery* requests
//     (robots.go), which is what RFC 9309 scopes it to; a page the user asked
//     for by name is retrieval, not crawling (kind.go). **Since 2026-09-16 the
//     consultation is off by default** and is turned on by a single global
//     setting — Options.ConsultRobots and SetConsultRobots. See the comment on
//     Options.ConsultRobots for the decision and PLAN §7.4 for the reasoning;
//     every other invariant in this list is unaffected and stays
//     non-configurable;
//   - Retry-After honoured, exponential backoff with jitter on 429/5xx;
//   - an honest User-Agent naming Quire, its version and the project URL —
//     never a browser string (PLAN §7.6);
//   - an SSRF guard on every resolved URL, including every redirect hop
//     (guard.go);
//   - a response size cap and total-bytes accounting.
//
// A source's own rateLimit may only *narrow* these. Client takes the stricter
// of the global floor and the per-source value on every request, so a hostile
// or merely optimistic config file cannot make Quire ruder than its floor.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ProjectURL is part of the User-Agent. PLAN §7.4 wants the UA to say who we
// are and where to complain, so an operator seeing our traffic in their logs
// can find the project rather than guess.
const ProjectURL = "https://github.com/rickl/quire"

// DefaultMaxResponseBytes caps a single response body. The device has little
// memory and no swap; a body larger than this is refused rather than buffered.
const DefaultMaxResponseBytes int64 = 8 << 20

// DefaultMaxAttempts is the total number of tries for one request, including
// the first. Only 429 and 5xx are retried; a 404 is an answer, not a failure.
const DefaultMaxAttempts = 3

// MaxRetryAfter bounds how long we will honour a Retry-After header. A site
// may legitimately say "come back in a day"; we treat that as a failure to
// report rather than a sleep to perform.
const MaxRetryAfter = 2 * time.Minute

var (
	// ErrTooLarge is returned when a body exceeds the response size cap. It is
	// reported from the Content-Length when the server declares one, so an
	// oversized body is usually refused before it is transferred.
	ErrTooLarge = errors.New("response exceeds size cap")

	// ErrRobotsDenied is returned when robots.txt disallows the path. PLAN §7.5
	// turns this into the robots_denied verdict: the source is not added.
	//
	// It means the site said no. It never means we failed to ask — that is
	// ErrRobotsUnavailable, and conflating the two would have Quire report a
	// refusal that never happened.
	ErrRobotsDenied = errors.New("disallowed by robots.txt")

	// ErrRobotsUnavailable is returned when robots.txt could not be read: a
	// 5xx that survived its retries, or a transport error. The outcome is
	// *unknown*, not permission — an unreadable robots.txt is an absence of
	// information, and PLAN §7.6's posture is to take "no" for an answer
	// rather than to help ourselves to the benefit of the doubt.
	//
	// Callers map this to the `unreachable` verdict (PLAN §7.5 stage 2), never
	// to robots_denied. See the RobotsCache comment for the full policy.
	ErrRobotsUnavailable = errors.New("robots.txt could not be read")

	// ErrBudgetExhausted is returned when the client's total-bytes budget is
	// spent. PLAN §6 M4 wants a warning threshold rather than a filled disk;
	// this is the hard stop behind it.
	ErrBudgetExhausted = errors.New("total byte budget exhausted")
)

// RateLimit is the per-source politeness block from schema/source.schema.json.
// Both fields are optional; zero means "no opinion, use the floor".
type RateLimit struct {
	RequestsPerMinute int `json:"requestsPerMinute,omitempty"`
	Concurrency       int `json:"concurrency,omitempty"`
}

// Policy is everything a request needs to know about the source it belongs to.
// fetch deliberately does not import the theme package: the dependency runs the
// other way, and a Policy is the narrow slice of a source that matters here.
type Policy struct {
	// BaseURL is the source root. Its registrable domain is the boundary a
	// redirect may not cross (PLAN §7.4).
	BaseURL *url.URL

	// AllowedHosts lists extra hosts or registrable domains a redirect may land
	// on. Image CDNs are the usual reason.
	AllowedHosts []string

	// RateLimit narrows the global caps. It can never widen them.
	RateLimit *RateLimit
}

// Response is a fully read HTTP response. Bodies are small (HTML pages, JSON
// fragments) and are consumed more than once by the parsers, so buffering here
// is simpler than handing callers a ReadCloser they must remember to close.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte

	// FinalURL is the URL after redirects. PLAN §7.5 stage 2 needs it to tell
	// the user their URL moved.
	FinalURL *url.URL

	// Attempts is how many tries this took, for probe diagnostics.
	Attempts int
}

// Options configures a Client. The zero value is usable; every field has a
// sane default, and the politeness floors cannot be relaxed from here — Caps
// is a *floor*, so raising a value only ever makes Quire stricter is false:
// see NewClient, which clamps Caps against DefaultCaps.
type Options struct {
	// Version is the Quire build stamp that goes into the User-Agent.
	Version string

	// Transport, if set, replaces the default HTTP transport. Tests use this;
	// production does not.
	Transport http.RoundTripper

	// Caps are the global politeness caps. They are clamped against
	// DefaultCaps so a caller cannot widen the floor.
	Caps Caps

	// MaxResponseBytes overrides DefaultMaxResponseBytes downwards only.
	MaxResponseBytes int64

	// TotalByteBudget, when positive, is a hard ceiling on all bytes this
	// client will read. Zero means unlimited.
	TotalByteBudget int64

	// MaxAttempts overrides DefaultMaxAttempts.
	MaxAttempts int

	// Now and Sleep are injectable for tests. Nil means real time.
	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error

	// Rand supplies backoff jitter. Nil means real randomness.
	Rand func() float64

	// resolve is the DNS hook for the SSRF guard, injectable from tests in the
	// same package. Nil means the real resolver.
	resolve func(ctx context.Context, host string) ([]net.IP, error)

	// allowLoopback lets the in-package tests point a Client at an httptest
	// server. It is unexported and has no JSON or flag surface, so no
	// configuration file can ever reach it.
	allowLoopback bool

	// ConsultRobots turns the robots.txt consultation on. It is **off by
	// default** (PLAN §7.4, superseded 2026-09-16): RFC 9309 scopes robots.txt
	// to "automatic clients known as crawlers", a person searching and tapping
	// is driving every request, and no comparable reader consults it at all.
	//
	// It is one global switch rather than a per-source flag, so there is one
	// behaviour and nothing to reason about per site. The parser, the cache and
	// the three-way handling of an unreadable robots.txt are all still here and
	// still tested: turning this back on is a setting, not a rewrite.
	//
	// It narrows nothing else. The limiter, the per-host delay, the honest
	// User-Agent, Retry-After, the size cap, the byte budget and the SSRF guard
	// apply identically either way, and PLAN §7.6 is untouched — a challenge is
	// a site actively refusing us, which is a different thing from an advisory
	// file aimed at crawlers, and there is still no bypass path here.
	ConsultRobots bool

	// Logger receives the info line written every time the robots consultation
	// is skipped. Nil means slog.Default().
	Logger *slog.Logger
}

// Client is a polite, guarded HTTP client shared by every theme.
type Client struct {
	hc         *http.Client
	lim        *Limiter
	guard      *Guard
	robots     *RobotsCache
	ua         string
	maxBody    int64
	budget     int64
	maxAttempt int
	totalBytes atomic.Int64
	// consultRobots is atomic because the setting is user-visible: it can be
	// toggled while requests are in flight, and the next request should see it.
	consultRobots atomic.Bool
	log           *slog.Logger
	sleep         func(context.Context, time.Duration) error
	now           func() time.Time
	rand          func() float64
}

// NewClient builds a Client. Caps are clamped against DefaultCaps so the
// politeness floor survives any caller.
func NewClient(opts Options) *Client {
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepCtx
	}
	if opts.Rand == nil {
		opts.Rand = rand.Float64
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = DefaultMaxAttempts
	}
	maxBody := DefaultMaxResponseBytes
	if opts.MaxResponseBytes > 0 && opts.MaxResponseBytes < maxBody {
		maxBody = opts.MaxResponseBytes
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	guard := &Guard{resolve: opts.resolve, allowLoopback: opts.allowLoopback}
	c := &Client{
		log:        opts.Logger,
		lim:        NewLimiter(DefaultCaps.narrowedBy(opts.Caps), opts.Now, opts.Sleep),
		guard:      guard,
		ua:         fmt.Sprintf("Quire/%s (+%s)", opts.Version, ProjectURL),
		maxBody:    maxBody,
		budget:     opts.TotalByteBudget,
		maxAttempt: opts.MaxAttempts,
		sleep:      opts.Sleep,
		now:        opts.Now,
		rand:       opts.Rand,
	}

	transport := opts.Transport
	if transport == nil {
		transport = defaultTransport()
	}
	c.hc = &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		// Every redirect hop is re-guarded. A site that 302s to 127.0.0.1 or
		// off its own registrable domain is stopped here, not after the fact.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many redirects")
			}
			pol, _ := req.Context().Value(policyKey{}).(*Policy)
			return guard.CheckURL(req.Context(), req.URL, pol)
		},
	}
	c.consultRobots.Store(opts.ConsultRobots)
	c.robots = NewRobotsCache(c)
	return c
}

// SetConsultRobots turns the robots.txt consultation on or off. It is the one
// global switch of PLAN §7.4; there is deliberately no per-source equivalent,
// because two overlapping mechanisms would be worse than either.
//
// It takes effect on the next request. Requests already past the check are not
// recalled, which matters only for a toggle flipped mid-probe.
func (c *Client) SetConsultRobots(on bool) { c.consultRobots.Store(on) }

// ConsultRobots reports whether robots.txt is currently consulted.
func (c *Client) ConsultRobots() bool { return c.consultRobots.Load() }

func defaultTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	// The concurrency caps are enforced by the Limiter, but keeping the pool
	// small stops a burst of goroutines holding sockets open on the device.
	t.MaxIdleConnsPerHost = 2
	t.MaxConnsPerHost = int(DefaultCaps.HostConcurrency)
	t.ResponseHeaderTimeout = 30 * time.Second
	return t
}

// UserAgent is the honest UA string this client sends. PLAN §7.6: Quire never
// impersonates a browser, because pretending to be one is both dishonest and
// the first step towards defeating a challenge we have decided not to defeat.
func (c *Client) UserAgent() string { return c.ua }

// TotalBytes is the running total of response bytes read (PLAN §7.4).
func (c *Client) TotalBytes() int64 { return c.totalBytes.Load() }

// Get performs a guarded, rate-limited, robots-checked GET.
//
// It is a *discovery* request (PLAN §7.4): searches, listings and link
// following, where a Disallow is a refusal. The strict reading is the default
// so that a caller who has not thought about it gets the safe answer; asking
// for the other one takes a deliberate call to GetRetrieval.
func (c *Client) Get(ctx context.Context, p *Policy, rawurl string) (*Response, error) {
	return c.GetFrom(ctx, p, rawurl, Referrer{})
}

// GetFrom is Get with a Referer naming the page rawurl was taken from.
//
// A zero Referrer sends no header, and Get above is exactly this call with one
// — which is the point: there is no default, no fallback and nothing derived
// from the URL being fetched. A Referer appears only because a caller had a
// real page to name and said so.
func (c *Client) GetFrom(ctx context.Context, p *Policy, rawurl string, from Referrer) (*Response, error) {
	return c.do(ctx, p, KindDiscovery, http.MethodGet, rawurl, nil, from.header())
}

// GetRetrieval performs a GET for one thing the user explicitly asked for: a
// series they opened, a chapter they chose, a page of it.
//
// Everything Get does, this does too — the limiter, the per-host delay, the
// honest User-Agent, Retry-After, backoff, the size cap, byte accounting and
// the SSRF guard. The *only* difference is that robots.txt does not gate it,
// because RFC 9309 scopes robots to crawlers and this is not crawling. See
// Kind for why that distinction is drawn here and not at a config file.
func (c *Client) GetRetrieval(ctx context.Context, p *Policy, rawurl string) (*Response, error) {
	return c.GetRetrievalFrom(ctx, p, rawurl, Referrer{})
}

// GetRetrievalFrom is GetRetrieval with a Referer naming the page rawurl was
// taken from. It is the reason Referrer exists: page images are the one thing
// Quire fetches that hosts routinely refuse to serve unless the request says
// which of their pages it came from.
//
// A zero Referrer sends no header.
func (c *Client) GetRetrievalFrom(ctx context.Context, p *Policy, rawurl string, from Referrer) (*Response, error) {
	return c.do(ctx, p, KindRetrieval, http.MethodGet, rawurl, nil, from.header())
}

// PostForm performs a guarded POST of an application/x-www-form-urlencoded
// body. Madara's chapter list lives behind exactly such a POST (PLAN §7.3).
func (c *Client) PostForm(ctx context.Context, p *Policy, rawurl string, form url.Values) (*Response, error) {
	body := []byte(form.Encode())
	h := http.Header{}
	h.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	// Madara's admin-ajax endpoint only answers the AJAX shape when asked as
	// one. This is a protocol fact about the endpoint, not browser cosplay.
	h.Set("X-Requested-With", "XMLHttpRequest")
	// Discovery: the only POST a theme makes is a chapter *listing*, which is
	// Quire working out what exists. There is deliberately no retrieval POST —
	// nothing the user asks for by name is fetched with one, so the looser
	// reading has no caller and is not offered.
	return c.do(ctx, p, KindDiscovery, http.MethodPost, rawurl, body, h)
}

type policyKey struct{}

func (c *Client) do(ctx context.Context, p *Policy, kind Kind, method, rawurl string, body []byte, hdr http.Header) (*Response, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w: %v", ErrInvalidURL, err)
	}
	if !u.IsAbs() {
		if p == nil || p.BaseURL == nil {
			return nil, fmt.Errorf("fetch: %w: relative URL with no base", ErrInvalidURL)
		}
		u = p.BaseURL.ResolveReference(u)
	}
	if err := c.guard.CheckURL(ctx, u, p); err != nil {
		return nil, err
	}

	// robots.txt is consulted before the limiter, because a denied path should
	// not consume a request slot or a delay.
	//
	// Retrieval skips the consultation entirely rather than consulting and
	// ignoring the answer: a request the user asked for by name is not
	// crawling, so there is nothing for robots to be asked about, and fetching
	// robots.txt to discard its verdict would be a request we did not need to
	// make. Note what is *not* skipped — the guard above and the limiter
	// below both still run.
	if kind.gatedByRobots() && !isRobotsURL(u) {
		if c.consultRobots.Load() {
			ok, err := c.robots.Allowed(ctx, p, u)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("fetch: %s: %w", u.Path, ErrRobotsDenied)
			}
		} else {
			// PLAN §7.4 requires this line. A safeguard that is off silently is
			// worse than one that was never there, so every suppressed check
			// leaves a record naming the path it would have covered.
			//
			// The wording is deliberately neutral about *why* the request is
			// being made. The owner's argument is that a person searching and
			// tapping is browsing rather than crawling, and for those requests
			// it holds — but the probe and the watch checks run unattended, so
			// a line claiming every suppressed check was user-driven would be
			// untrue. The request kind is logged instead: it is cheap, it is a
			// fact, and it lets a reader tell the two apart.
			c.log.Info("robots.txt not consulted; the check is off (PLAN §7.4)",
				"host", u.Host, "path", u.Path, "kind", kind.String(), "method", method)
		}
	}

	var last *Response
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempt; attempt++ {
		release, err := c.lim.Acquire(ctx, u.Host, policyRate(p))
		if err != nil {
			return nil, err
		}
		resp, err := c.attempt(ctx, p, method, u, body, hdr)
		release()

		if err != nil {
			return nil, err
		}
		resp.Attempts = attempt
		last, lastErr = resp, nil

		if !retryable(resp.StatusCode) || attempt == c.maxAttempt {
			return resp, nil
		}
		wait := c.backoff(attempt, resp.Header.Get("Retry-After"))
		if wait < 0 {
			// Retry-After asked for longer than we are willing to wait. Report
			// the response as-is rather than sleeping for minutes.
			return resp, nil
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return last, nil
}

func (c *Client) attempt(ctx context.Context, p *Policy, method string, u *url.URL, body []byte, hdr http.Header) (*Response, error) {
	ctx = context.WithValue(ctx, policyKey{}, p)
	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return nil, fmt.Errorf("fetch: build request: %w", err)
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept-Language", "*")
	if body != nil {
		req.ContentLength = int64(len(body))
	}

	hres, err := c.hc.Do(req)
	if err != nil {
		// Unwrap the guard's own errors so a redirect rejection is reported as
		// a blocked address rather than as a generic url.Error.
		var ge *GuardError
		if errors.As(err, &ge) {
			return nil, ge
		}
		return nil, fmt.Errorf("fetch: %s %s: %w", method, u.Redacted(), err)
	}
	defer hres.Body.Close()

	if hres.ContentLength > c.maxBody {
		return nil, fmt.Errorf("fetch: %s: %w (declared %d > %d)", u.Redacted(), ErrTooLarge, hres.ContentLength, c.maxBody)
	}
	if c.budget > 0 && c.totalBytes.Load() >= c.budget {
		return nil, fmt.Errorf("fetch: %w", ErrBudgetExhausted)
	}

	// Read one byte past the cap so an undeclared oversized body is caught.
	buf, err := io.ReadAll(io.LimitReader(hres.Body, c.maxBody+1))
	c.totalBytes.Add(int64(len(buf)))
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: read body: %w", u.Redacted(), err)
	}
	if int64(len(buf)) > c.maxBody {
		return nil, fmt.Errorf("fetch: %s: %w (> %d)", u.Redacted(), ErrTooLarge, c.maxBody)
	}

	final := hres.Request.URL
	if final == nil {
		final = u
	}
	return &Response{StatusCode: hres.StatusCode, Header: hres.Header, Body: buf, FinalURL: final}, nil
}

// retryable reports whether a status deserves another go. 429 and 5xx are
// transient by definition; 4xx otherwise is an answer.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status <= 599)
}

// backoff returns how long to wait before the next attempt. A parseable
// Retry-After wins outright (PLAN §7.4); otherwise exponential with full
// jitter, so a fleet of devices retrying after the same 503 does not
// synchronise into a second thundering herd. A negative return means
// "Retry-After is longer than we are prepared to wait; give up".
func (c *Client) backoff(attempt int, retryAfter string) time.Duration {
	if d, ok := parseRetryAfter(retryAfter, c.now()); ok {
		if d > MaxRetryAfter {
			return -1
		}
		if d < 0 {
			d = 0
		}
		return d
	}
	base := time.Second << (attempt - 1) // 1s, 2s, 4s, ...
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	// Full jitter over [base/2, base].
	return base/2 + time.Duration(c.rand()*float64(base/2))
}

// parseRetryAfter accepts both forms RFC 9110 allows: delta-seconds and an
// HTTP-date.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return t.Sub(now), true
	}
	return 0, false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func policyRate(p *Policy) *RateLimit {
	if p == nil {
		return nil
	}
	return p.RateLimit
}
