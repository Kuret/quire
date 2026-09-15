package fetch_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
)

// TestRateLimitNarrowsButNeverWidens is the load-bearing test for PLAN §7.4's
// "not configurable by a source entry". A source's rateLimit is user-supplied
// data; it must only ever be able to make Quire slower and gentler.
func TestRateLimitNarrowsButNeverWidens(t *testing.T) {
	t.Parallel()

	floor := fetch.DefaultCaps

	tests := []struct {
		name string
		rl   *fetch.RateLimit
		want fetch.Caps
	}{
		{
			name: "no rateLimit leaves the floor alone",
			rl:   nil,
			want: floor,
		},
		{
			name: "the schema default matches the floor exactly",
			rl:   &fetch.RateLimit{RequestsPerMinute: 30, Concurrency: 2},
			want: floor,
		},
		{
			name: "a slower rate is honoured",
			rl:   &fetch.RateLimit{RequestsPerMinute: 6},
			want: fetch.Caps{GlobalConcurrency: floor.GlobalConcurrency, HostConcurrency: floor.HostConcurrency, MinHostDelay: 10 * time.Second},
		},
		{
			name: "lower concurrency is honoured",
			rl:   &fetch.RateLimit{Concurrency: 1},
			want: fetch.Caps{GlobalConcurrency: floor.GlobalConcurrency, HostConcurrency: 1, MinHostDelay: floor.MinHostDelay},
		},
		{
			name: "a faster rate is ignored",
			rl:   &fetch.RateLimit{RequestsPerMinute: 120},
			want: floor,
		},
		{
			name: "higher concurrency is ignored",
			rl:   &fetch.RateLimit{Concurrency: 8},
			want: floor,
		},
		{
			name: "the schema maximums cannot widen the floor",
			rl:   &fetch.RateLimit{RequestsPerMinute: 120, Concurrency: 8},
			want: floor,
		},
		{
			name: "an out-of-schema absurdity cannot widen the floor either",
			rl:   &fetch.RateLimit{RequestsPerMinute: 1_000_000, Concurrency: 1_000},
			want: floor,
		},
		{
			name: "mixed: slower rate accepted, higher concurrency rejected",
			rl:   &fetch.RateLimit{RequestsPerMinute: 10, Concurrency: 8},
			want: fetch.Caps{GlobalConcurrency: floor.GlobalConcurrency, HostConcurrency: floor.HostConcurrency, MinHostDelay: 6 * time.Second},
		},
		{
			name: "negative values are treated as no opinion",
			rl:   &fetch.RateLimit{RequestsPerMinute: -1, Concurrency: -5},
			want: floor,
		},
	}

	lim := fetch.NewLimiter(floor, nil, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lim.EffectiveCaps(tc.rl)
			if got != tc.want {
				t.Fatalf("EffectiveCaps(%+v) = %+v, want %+v", tc.rl, got, tc.want)
			}
			// The invariant, stated directly: never looser than the floor.
			if got.GlobalConcurrency > floor.GlobalConcurrency {
				t.Errorf("global concurrency %d exceeds the floor %d", got.GlobalConcurrency, floor.GlobalConcurrency)
			}
			if got.HostConcurrency > floor.HostConcurrency {
				t.Errorf("host concurrency %d exceeds the floor %d", got.HostConcurrency, floor.HostConcurrency)
			}
			if got.MinHostDelay < floor.MinHostDelay {
				t.Errorf("delay %v is shorter than the floor %v", got.MinHostDelay, floor.MinHostDelay)
			}
		})
	}
}

// TestOptionsCannotWidenTheFloor covers the other direction a caller might try:
// constructing a Client with generous Caps.
func TestOptionsCannotWidenTheFloor(t *testing.T) {
	t.Parallel()
	got := fetch.DefaultCaps.NarrowedBy(fetch.Caps{
		GlobalConcurrency: 64,
		HostConcurrency:   64,
		MinHostDelay:      0,
	})
	if got != fetch.DefaultCaps {
		t.Fatalf("caps = %+v, want the untouched floor %+v", got, fetch.DefaultCaps)
	}
}

func TestLimiterEnforcesHostConcurrency(t *testing.T) {
	t.Parallel()

	var inflight, peak atomic.Int32
	lim := fetch.NewLimiter(
		fetch.Caps{GlobalConcurrency: 8, HostConcurrency: 2, MinHostDelay: 0},
		nil,
		func(context.Context, time.Duration) error { return nil },
	)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := lim.Acquire(context.Background(), "example.invalid", nil)
			if err != nil {
				t.Error(err)
				return
			}
			n := inflight.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			inflight.Add(-1)
			release()
		}()
	}
	wg.Wait()

	if peak.Load() > 2 {
		t.Fatalf("peak in-flight per host = %d, want at most 2", peak.Load())
	}
}

func TestLimiterEnforcesGlobalConcurrencyAcrossHosts(t *testing.T) {
	t.Parallel()

	var inflight, peak atomic.Int32
	lim := fetch.NewLimiter(
		fetch.Caps{GlobalConcurrency: 3, HostConcurrency: 2, MinHostDelay: 0},
		nil,
		func(context.Context, time.Duration) error { return nil },
	)

	hosts := []string{"a.example.invalid", "b.example.invalid", "c.example.invalid", "d.example.invalid"}
	var wg sync.WaitGroup
	for _, h := range hosts {
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				release, err := lim.Acquire(context.Background(), h, nil)
				if err != nil {
					t.Error(err)
					return
				}
				n := inflight.Add(1)
				for {
					old := peak.Load()
					if n <= old || peak.CompareAndSwap(old, n) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				inflight.Add(-1)
				release()
			}()
		}
	}
	wg.Wait()

	if peak.Load() > 3 {
		t.Fatalf("peak in-flight globally = %d, want at most 3", peak.Load())
	}
}

// TestLimiterSpacesRequestsPerHost uses a fake clock so the test is instant and
// the assertion is exact rather than timing-dependent.
func TestLimiterSpacesRequestsPerHost(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	now := time.Unix(0, 0)
	var waits []time.Duration

	lim := fetch.NewLimiter(
		fetch.Caps{GlobalConcurrency: 1, HostConcurrency: 1, MinHostDelay: 2 * time.Second},
		func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
		func(_ context.Context, d time.Duration) error {
			mu.Lock()
			defer mu.Unlock()
			waits = append(waits, d)
			now = now.Add(d) // the fake clock advances by exactly the sleep
			return nil
		},
	)

	for range 4 {
		release, err := lim.Acquire(context.Background(), "example.invalid", nil)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}

	mu.Lock()
	defer mu.Unlock()
	// The first request goes immediately; the next three each wait the delay.
	want := []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
}

func TestLimiterRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	lim := fetch.NewLimiter(fetch.Caps{GlobalConcurrency: 1, HostConcurrency: 1, MinHostDelay: 0}, nil, nil)

	release, err := lim.Acquire(context.Background(), "example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lim.Acquire(ctx, "example.invalid", nil); err == nil {
		t.Fatal("Acquire on a cancelled context returned no error")
	}
}
