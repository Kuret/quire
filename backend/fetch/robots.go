package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// robotsTTL is how long a fetched robots.txt is trusted. Long enough that a
// browse session costs one fetch; short enough that a site that tightens its
// rules is respected the same day.
const robotsTTL = 12 * time.Hour

// robotsMaxBytes caps the robots.txt body. Google's limit is 500 KiB; anything
// beyond that is not a robots file.
const robotsMaxBytes = 512 << 10

// robotsTransportRetries is how many extra attempts a transport error earns
// before the answer is declared unknown. Status-code retries are handled by
// Client.do, which already backs off on 5xx; this covers the case where there
// was no response at all.
const robotsTransportRetries = 2

// RobotsCache fetches, caches and applies robots.txt per host (PLAN §7.4).
//
// The outcome policy is deliberate, and it is three cases rather than two,
// because "we were refused" and "we could not ask" are different facts and
// only one of them is a denial:
//
//   - **2xx: parse and apply.** A body we cannot fully parse yields the rules
//     we did understand; the rest is ignored.
//
//   - **404, or any other 4xx: allowed.** No robots file means no
//     restrictions. This is the conventional reading and what every major
//     crawler does.
//
//   - **5xx, or a transport error: unknown.** Retried with backoff; if it
//     still fails, the request does not proceed. An unreadable robots.txt is
//     not a "yes" — it is an absence of information, and PLAN §7.6's posture
//     of taking "no" for an answer means we do not help ourselves to the
//     benefit of the doubt. Earlier revisions of this file allowed the
//     request in this case; that was wrong.
//
// The third case is reported as ErrRobotsUnavailable, which callers map to the
// `unreachable` verdict — never to `robots_denied`. The site did not deny us;
// we could not ask. PLAN §6 M3 requires a verdict to be a complete and honest
// answer, and asserting a refusal that never happened would fail that twice
// over: it would be untrue, and it would send the user off to argue with a
// robots policy that does not exist.
//
// An unknown outcome is deliberately *not* cached. A transient 503 must not
// lock a host out for the whole TTL; the next request asks again.
type RobotsCache struct {
	client *Client

	mu      sync.Mutex
	entries map[string]*robotsEntry
	now     func() time.Time
}

type robotsEntry struct {
	rules   *robotsRules
	err     error
	fetched time.Time
	// ready is closed when the fetch finishes, so N concurrent requests to a
	// cold host result in exactly one robots.txt fetch. Waiters share the
	// outcome, including a failure — they do not each retry it.
	ready chan struct{}
}

// NewRobotsCache builds a cache backed by c.
func NewRobotsCache(c *Client) *RobotsCache {
	return &RobotsCache{client: c, entries: make(map[string]*robotsEntry), now: c.now}
}

// Allowed reports whether u may be fetched under the host's robots.txt.
func (rc *RobotsCache) Allowed(ctx context.Context, p *Policy, u *url.URL) (bool, error) {
	rules, err := rc.rulesFor(ctx, p, u)
	if err != nil {
		return false, err
	}
	if rules == nil {
		return true, nil
	}
	return rules.allowed(pathWithQuery(u)), nil
}

// CrawlDelay returns the host's Crawl-delay, or 0. The caller narrows the
// limiter with it — robots may ask us to be slower, never faster.
func (rc *RobotsCache) CrawlDelay(ctx context.Context, p *Policy, u *url.URL) (time.Duration, error) {
	rules, err := rc.rulesFor(ctx, p, u)
	if err != nil || rules == nil {
		return 0, err
	}
	return rules.crawlDelay, nil
}

func (rc *RobotsCache) rulesFor(ctx context.Context, p *Policy, u *url.URL) (*robotsRules, error) {
	key := strings.ToLower(u.Scheme + "://" + u.Host)

	// The loop exists for exactly one transition: a cached entry that has gone
	// stale is dropped and refetched. Every other path returns.
	for {
		rc.mu.Lock()
		e, ok := rc.entries[key]
		if !ok {
			e = &robotsEntry{ready: make(chan struct{})}
			rc.entries[key] = e
			rc.mu.Unlock()

			rules, err := rc.fetch(ctx, p, u.Scheme+"://"+u.Host+"/robots.txt")

			rc.mu.Lock()
			e.rules, e.err, e.fetched = rules, err, rc.now()
			if err != nil {
				// Do not cache an unknown: a transient failure must not lock
				// the host out for the whole TTL.
				if rc.entries[key] == e {
					delete(rc.entries, key)
				}
			}
			rc.mu.Unlock()
			close(e.ready)
			return rules, err
		}

		ready := e.ready
		rc.mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		rc.mu.Lock()
		rules, err, fetched := e.rules, e.err, e.fetched
		rc.mu.Unlock()

		if err != nil {
			// Share the failure rather than re-fetching. Without this, N
			// waiters on a failed fetch would each start their own, turning
			// one broken host into a retry storm.
			return nil, err
		}
		if rc.now().Sub(fetched) < robotsTTL {
			return rules, nil
		}

		rc.mu.Lock()
		// Another goroutine may already have started the refresh.
		if rc.entries[key] == e {
			delete(rc.entries, key)
		}
		rc.mu.Unlock()
	}
}

// fetch retrieves and parses robots.txt.
//
// The three returns map to the three cases in the RobotsCache comment:
// (rules, nil) for a parsed file, (nil, nil) for "no file, everything
// allowed", and (nil, ErrRobotsUnavailable) for "we could not ask".
func (rc *RobotsCache) fetch(ctx context.Context, p *Policy, rawurl string) (*robotsRules, error) {
	var lastErr error
	var lastStatus int

	// Client.do already retries 5xx with backoff, so a status-code failure
	// arrives here having been tried properly. A transport error never reached
	// a server at all, so it gets its own bounded retry.
	for attempt := 0; ; attempt++ {
		// Discovery, and isRobotsURL stops the recursion. Fetching robots.txt
		// is Quire finding out what it may crawl, which is the definition of
		// the discovery kind even though this particular URL is never gated.
		resp, err := rc.client.do(ctx, p, KindDiscovery, "GET", rawurl, nil, nil)
		switch {
		case err != nil:
			// A guarded or malformed URL is not a transient fault; retrying it
			// would waste the device's time to reach the same answer.
			var ge *GuardError
			if errors.As(err, &ge) || errors.Is(err, ErrInvalidURL) || errors.Is(err, ErrBudgetExhausted) {
				return nil, robotsUnavailable(rawurl, err)
			}
			lastErr = err
		case resp.StatusCode == 200:
			body := resp.Body
			if len(body) > robotsMaxBytes {
				body = body[:robotsMaxBytes]
			}
			return parseRobots(string(body), rc.client.ua), nil
		case resp.StatusCode >= 400 && resp.StatusCode <= 499:
			// No robots file. Everything is allowed.
			return nil, nil
		case resp.StatusCode >= 200 && resp.StatusCode <= 399:
			// A 2xx that is not 200 (204, say) or a redirect we did not
			// follow to a body: nothing to parse, and nothing that says no.
			return nil, nil
		default:
			// 5xx, already retried by Client.do. The answer is unknown.
			lastStatus = resp.StatusCode
			return nil, robotsUnavailable(rawurl, fmt.Errorf("HTTP %d", lastStatus))
		}

		if attempt >= robotsTransportRetries || ctx.Err() != nil {
			return nil, robotsUnavailable(rawurl, lastErr)
		}
		if err := rc.client.sleep(ctx, rc.client.backoff(attempt+1, "")); err != nil {
			return nil, robotsUnavailable(rawurl, lastErr)
		}
	}
}

// robotsUnavailable wraps a cause as the "we could not ask" outcome. The
// message says so in the words the user will see, because PLAN §6 M3 wants a
// verdict to read as a complete answer rather than as an error code.
func robotsUnavailable(rawurl string, cause error) error {
	if cause == nil {
		cause = errors.New("no response")
	}
	return fmt.Errorf("fetch: %s: %w: %w", rawurl, ErrRobotsUnavailable, cause)
}

// robotsRules is the merged group that applies to us.
type robotsRules struct {
	rules      []robotsRule
	crawlDelay time.Duration
}

type robotsRule struct {
	pattern string
	allow   bool
}

// parseRobots extracts the group matching our product token, falling back to
// the "*" group. Our token is "Quire"; ua is the full User-Agent so the
// matcher can find it case-insensitively as a substring, the way the de facto
// standard specifies product-token matching.
func parseRobots(body, ua string) *robotsRules {
	token := "quire"
	_ = ua

	type group struct {
		agents     []string
		rules      []robotsRule
		crawlDelay time.Duration
	}
	var groups []*group
	var cur *group
	startingGroup := false

	for line := range strings.Lines(body) {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)

		switch name {
		case "user-agent":
			// Consecutive User-agent lines share one group of rules.
			if cur == nil || !startingGroup {
				cur = &group{}
				groups = append(groups, cur)
				startingGroup = true
			}
			cur.agents = append(cur.agents, strings.ToLower(value))
		case "allow", "disallow":
			if cur == nil {
				continue // a rule before any User-agent line belongs to nobody
			}
			startingGroup = false
			// An empty Disallow means "allow everything"; record it as an
			// Allow of / so longest-match handles it naturally.
			if value == "" {
				if name == "disallow" {
					cur.rules = append(cur.rules, robotsRule{pattern: "/", allow: true})
				}
				continue
			}
			cur.rules = append(cur.rules, robotsRule{pattern: value, allow: name == "allow"})
		case "crawl-delay":
			if cur == nil {
				continue
			}
			startingGroup = false
			if f, err := strconv.ParseFloat(value, 64); err == nil && f > 0 {
				cur.crawlDelay = time.Duration(f * float64(time.Second))
			}
		default:
			// Sitemap and anything else: not our business.
			startingGroup = false
		}
	}

	var specific, wildcard *group
	for _, g := range groups {
		for _, a := range g.agents {
			if a == "*" && wildcard == nil {
				wildcard = g
			}
			if a != "*" && strings.Contains(token, a) && specific == nil {
				specific = g
			}
		}
	}
	g := specific
	if g == nil {
		g = wildcard
	}
	if g == nil {
		return nil
	}
	return &robotsRules{rules: g.rules, crawlDelay: g.crawlDelay}
}

// allowed applies longest-match-wins, with Allow beating Disallow on a tie —
// the rule every modern implementation uses.
func (r *robotsRules) allowed(path string) bool {
	if r == nil || len(r.rules) == 0 {
		return true
	}
	bestLen, bestAllow := -1, true
	for _, rule := range r.rules {
		if !robotsMatch(rule.pattern, path) {
			continue
		}
		n := len(strings.TrimSuffix(rule.pattern, "$"))
		if n > bestLen || (n == bestLen && rule.allow) {
			bestLen, bestAllow = n, rule.allow
		}
	}
	if bestLen < 0 {
		return true
	}
	return bestAllow
}

// robotsMatch implements the two wildcards the standard defines: '*' for any
// run of characters and a trailing '$' anchoring the end of the path.
func robotsMatch(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")

	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			if !strings.HasPrefix(path[pos:], part) {
				return false
			}
			pos += len(part)
			continue
		}
		j := strings.Index(path[pos:], part)
		if j < 0 {
			return false
		}
		pos += j + len(part)
	}
	if anchored {
		// The last literal must reach the end, unless it was followed by a '*'.
		if last := parts[len(parts)-1]; last == "" {
			return true
		}
		return pos == len(path)
	}
	return true
}

// pathWithQuery is what robots patterns match against: the path plus any query
// string, since patterns routinely end in "?s=" or similar.
func pathWithQuery(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}
