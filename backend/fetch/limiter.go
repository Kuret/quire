package fetch

import (
	"context"
	"sync"
	"time"
)

// Caps are the politeness caps of PLAN §7.4. They exist in exactly two places:
// DefaultCaps, which is the floor and is not configurable, and a per-source
// rateLimit, which may only narrow it.
type Caps struct {
	// GlobalConcurrency bounds in-flight requests across all hosts.
	GlobalConcurrency int
	// HostConcurrency bounds in-flight requests to any single host.
	HostConcurrency int
	// MinHostDelay is the minimum interval between the *starts* of two
	// requests to the same host.
	MinHostDelay time.Duration
}

// DefaultCaps is the floor. PLAN §7.4 calls these non-negotiable and not
// configurable by a source entry, so they are a constant here rather than
// anything a config file can reach.
//
// MinHostDelay of 2s is 30 requests per minute, which is what
// schema/source.schema.json documents as the default and what we treat as the
// ceiling. A source asking for the schema's maximum of 120/min therefore still
// gets 30: the schema bounds what a user may *write*, this bounds what Quire
// will actually *do*, and the stricter of the two always wins.
var DefaultCaps = Caps{
	GlobalConcurrency: 4,
	HostConcurrency:   2,
	MinHostDelay:      2 * time.Second,
}

// narrowedBy returns the stricter of c and other, field by field. A zero field
// in other means "no opinion" and leaves c's value alone. This is the only
// function that combines caps, and it can only ever make them tighter — which
// is the property the rate-limit tests pin.
func (c Caps) narrowedBy(other Caps) Caps {
	out := c
	if other.GlobalConcurrency > 0 && other.GlobalConcurrency < out.GlobalConcurrency {
		out.GlobalConcurrency = other.GlobalConcurrency
	}
	if other.HostConcurrency > 0 && other.HostConcurrency < out.HostConcurrency {
		out.HostConcurrency = other.HostConcurrency
	}
	if other.MinHostDelay > out.MinHostDelay {
		out.MinHostDelay = other.MinHostDelay
	}
	return out
}

// capsFromRateLimit converts a source's rateLimit block into Caps. Note the
// direction: a higher requestsPerMinute becomes a *shorter* delay, which
// narrowedBy will then discard for being looser than the floor.
func capsFromRateLimit(rl *RateLimit) Caps {
	if rl == nil {
		return Caps{}
	}
	var c Caps
	c.HostConcurrency = rl.Concurrency
	if rl.RequestsPerMinute > 0 {
		c.MinHostDelay = time.Minute / time.Duration(rl.RequestsPerMinute)
	}
	return c
}

// EffectiveCaps is the caps actually applied to a request for the given
// source. Exported because it is the honest answer to "how fast will Quire
// talk to my site?", and because it is the unit the narrowing tests exercise.
func (l *Limiter) EffectiveCaps(rl *RateLimit) Caps {
	return l.caps.narrowedBy(capsFromRateLimit(rl))
}

// Limiter enforces the concurrency caps and the inter-request delay.
//
// Ordering matters: the global slot is taken first, then the host slot, then
// the delay is observed while *holding* the host slot. Waiting out the delay
// outside the host slot would let N goroutines all decide the host is free at
// the same instant.
type Limiter struct {
	caps   Caps
	global chan struct{}
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error

	mu    sync.Mutex
	hosts map[string]*hostState
}

// hostState tracks one host. The in-flight count is guarded by a cond rather
// than a buffered channel because the permitted concurrency is a *per-call*
// value — the stricter of the floor and this source's rateLimit — and two
// sources may share a host with different limits.
type hostState struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	next     time.Time // earliest time the next request to this host may start
}

// acquireSlot blocks until fewer than limit requests are in flight to the host.
func (hs *hostState) acquireSlot(ctx context.Context, limit int) error {
	// A goroutine parked in cond.Wait cannot select on ctx.Done, so wake all
	// waiters when the context ends.
	stop := context.AfterFunc(ctx, func() {
		hs.mu.Lock()
		defer hs.mu.Unlock()
		hs.cond.Broadcast()
	})
	defer stop()

	hs.mu.Lock()
	defer hs.mu.Unlock()
	for hs.inflight >= limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		hs.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	hs.inflight++
	return nil
}

func (hs *hostState) releaseSlot() {
	hs.mu.Lock()
	hs.inflight--
	hs.mu.Unlock()
	hs.cond.Broadcast()
}

// NewLimiter builds a Limiter over the given caps.
func NewLimiter(caps Caps, now func() time.Time, sleep func(context.Context, time.Duration) error) *Limiter {
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = sleepCtx
	}
	if caps.GlobalConcurrency < 1 {
		caps.GlobalConcurrency = 1
	}
	if caps.HostConcurrency < 1 {
		caps.HostConcurrency = 1
	}
	return &Limiter{
		caps:   caps,
		global: make(chan struct{}, caps.GlobalConcurrency),
		now:    now,
		sleep:  sleep,
		hosts:  make(map[string]*hostState),
	}
}

// Acquire blocks until a request to host may proceed, and returns the release
// function. The returned function is safe to call exactly once.
func (l *Limiter) Acquire(ctx context.Context, host string, rl *RateLimit) (func(), error) {
	eff := l.EffectiveCaps(rl)

	select {
	case l.global <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	releaseGlobal := func() { <-l.global }

	hs := l.hostState(host)
	if err := hs.acquireSlot(ctx, eff.HostConcurrency); err != nil {
		releaseGlobal()
		return nil, err
	}
	releaseHost := hs.releaseSlot

	if err := l.waitForSlot(ctx, hs, eff.MinHostDelay); err != nil {
		releaseHost()
		releaseGlobal()
		return nil, err
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			releaseHost()
			releaseGlobal()
		})
	}, nil
}

// waitForSlot reserves this host's next start time and sleeps until it.
// Reserving before sleeping is what makes concurrent callers queue up at
// delay-sized intervals instead of all waking at once.
func (l *Limiter) waitForSlot(ctx context.Context, hs *hostState, delay time.Duration) error {
	hs.mu.Lock()
	now := l.now()
	start := hs.next
	if start.Before(now) {
		start = now
	}
	hs.next = start.Add(delay)
	hs.mu.Unlock()

	if wait := start.Sub(now); wait > 0 {
		return l.sleep(ctx, wait)
	}
	return nil
}

func (l *Limiter) hostState(host string) *hostState {
	l.mu.Lock()
	defer l.mu.Unlock()
	if hs, ok := l.hosts[host]; ok {
		return hs
	}
	hs := &hostState{}
	hs.cond = sync.NewCond(&hs.mu)
	l.hosts[host] = hs
	return hs
}
