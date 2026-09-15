package fetch

import (
	"context"
	"net"
	"time"
)

// The test-only hooks. They live in a _test.go file on purpose: production
// builds do not contain them, so there is no code path by which a config file,
// a source entry or a goja script could reach the loopback exemption.

// WithStubResolver returns o with the SSRF guard's DNS replaced.
func WithStubResolver(o Options, f func(ctx context.Context, host string) ([]net.IP, error)) Options {
	o.resolve = f
	return o
}

// AllowLoopback lets a test point a Client at an httptest server.
func AllowLoopback(o Options) Options {
	o.allowLoopback = true
	return o
}

// NewGuard builds a bare Guard for the guard tests.
func NewGuard(resolve func(ctx context.Context, host string) ([]net.IP, error)) *Guard {
	return &Guard{resolve: resolve}
}

// ParseRobots exposes the robots.txt parser to tests.
func ParseRobots(body string) *robotsRules { return parseRobots(body, "Quire/test") }

// RobotsAllows applies parsed rules to a path.
func RobotsAllows(r *robotsRules, path string) bool { return r.allowed(path) }

// CapsFromRateLimit exposes the rateLimit -> Caps conversion.
func CapsFromRateLimit(rl *RateLimit) Caps { return capsFromRateLimit(rl) }

// NarrowedBy exposes the only function that combines caps.
func (c Caps) NarrowedBy(other Caps) Caps { return c.narrowedBy(other) }

// Backoff exposes the retry delay calculation.
func (c *Client) Backoff(attempt int, retryAfter string) (d time.Duration, giveUp bool) {
	got := c.backoff(attempt, retryAfter)
	return got, got < 0
}
