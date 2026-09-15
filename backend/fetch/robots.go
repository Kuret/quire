package fetch

import (
	"context"
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

// RobotsCache fetches, caches and applies robots.txt per host (PLAN §7.4).
//
// The failure policy is deliberate and stated here because it is a judgement
// call, not an obvious one:
//
//   - 2xx: parse and apply.
//   - 404 or any other 4xx: no robots file, everything allowed. This is the
//     conventional reading and what every major crawler does.
//   - 5xx, or a transport error: allow. A site that is briefly broken has not
//     asked us to stay out, and failing closed here would turn a blip into a
//     "robots_denied" verdict that lies to the user.
//   - a body we cannot parse: allow the parts we understood, ignore the rest.
type RobotsCache struct {
	client *Client

	mu      sync.Mutex
	entries map[string]*robotsEntry
	now     func() time.Time
}

type robotsEntry struct {
	rules   *robotsRules
	fetched time.Time
	// ready is closed when the fetch finishes, so N concurrent requests to a
	// cold host result in exactly one robots.txt fetch.
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

	rc.mu.Lock()
	e, ok := rc.entries[key]
	if ok {
		ready := e.ready
		rc.mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		rc.mu.Lock()
		fresh := rc.now().Sub(e.fetched) < robotsTTL
		rules := e.rules
		rc.mu.Unlock()
		if fresh {
			return rules, nil
		}
		rc.mu.Lock()
		// Another goroutine may already have started the refresh.
		if rc.entries[key] == e {
			delete(rc.entries, key)
		}
		rc.mu.Unlock()
		return rc.rulesFor(ctx, p, u)
	}
	e = &robotsEntry{ready: make(chan struct{})}
	rc.entries[key] = e
	rc.mu.Unlock()

	rules := rc.fetch(ctx, p, u.Scheme+"://"+u.Host+"/robots.txt")

	rc.mu.Lock()
	e.rules = rules
	e.fetched = rc.now()
	rc.mu.Unlock()
	close(e.ready)
	return rules, nil
}

// fetch retrieves and parses robots.txt, returning nil for "no rules apply".
func (rc *RobotsCache) fetch(ctx context.Context, p *Policy, rawurl string) *robotsRules {
	resp, err := rc.client.do(ctx, p, "GET", rawurl, nil, nil)
	if err != nil || resp == nil {
		return nil // fail open; see the type comment
	}
	if resp.StatusCode != 200 {
		return nil
	}
	body := resp.Body
	if len(body) > robotsMaxBytes {
		body = body[:robotsMaxBytes]
	}
	return parseRobots(string(body), rc.client.ua)
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
