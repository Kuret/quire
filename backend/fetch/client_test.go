package fetch_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/internal/nonet"
)

// TestMain forbids DNS for the whole package. The httptest servers below are
// reached by literal 127.0.0.1, so they need no resolver; anything that tries
// to look up a real name fails loudly instead of quietly going online.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

// testClient builds a Client pointed at an httptest server. AllowLoopback is a
// test-only hook (export_test.go); production has no path to it.
func testClient(t *testing.T, srv *httptest.Server, opts fetch.Options) (*fetch.Client, *fetch.Policy) {
	t.Helper()
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	opts = fetch.AllowLoopback(opts)
	if opts.Version == "" {
		opts.Version = "test"
	}
	// Tests must not actually sleep out the politeness delay.
	if opts.Sleep == nil {
		opts.Sleep = func(context.Context, time.Duration) error { return nil }
	}
	opts.Caps = fetch.Caps{}
	return fetch.NewClient(opts), &fetch.Policy{BaseURL: base}
}

func TestUserAgentIsHonest(t *testing.T) {
	t.Parallel()
	var got atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("User-Agent"))
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{Version: "1.2.3"})
	if _, err := c.Get(context.Background(), p, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	ua, _ := got.Load().(string)

	if want := "Quire/1.2.3 (+" + fetch.ProjectURL + ")"; ua != want {
		t.Fatalf("User-Agent = %q, want %q", ua, want)
	}
	// PLAN §7.6: never impersonate a browser. Pin the specific lie we must
	// never tell, so a future "just add Mozilla to get past this site" is a
	// failing test rather than a quiet commit.
	for _, banned := range []string{"Mozilla", "AppleWebKit", "Chrome", "Safari", "Gecko", "Edg/"} {
		if strings.Contains(ua, banned) {
			t.Fatalf("User-Agent %q contains browser token %q", ua, banned)
		}
	}
}

func TestRetryAfterAndBackoff(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	var slept []time.Duration
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "")
			return
		}
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "second time lucky")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{
		Sleep: func(_ context.Context, d time.Duration) error {
			mu.Lock()
			slept = append(slept, d)
			mu.Unlock()
			return nil
		},
	})

	resp, err := c.Get(context.Background(), p, srv.URL+"/x")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", resp.Attempts)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(slept) == 0 {
		t.Fatal("no sleep recorded; Retry-After was ignored")
	}
	// The last sleep before the successful attempt must be the 3s the server
	// asked for, not our own backoff guess.
	if !contains(slept, 3*time.Second) {
		t.Fatalf("sleeps %v do not include the 3s Retry-After asked for", slept)
	}
}

func contains(ds []time.Duration, want time.Duration) bool {
	for _, d := range ds {
		if d == want {
			return true
		}
	}
	return false
}

func TestBackoffShape(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	c := fetch.NewClient(fetch.Options{
		Now:  func() time.Time { return now },
		Rand: func() float64 { return 0.5 },
	})

	tests := []struct {
		name       string
		attempt    int
		retryAfter string
		want       time.Duration
		giveUp     bool
	}{
		{name: "no header, first retry", attempt: 1, want: 750 * time.Millisecond},
		{name: "no header, second retry", attempt: 2, want: 1500 * time.Millisecond},
		{name: "no header, third retry", attempt: 3, want: 3 * time.Second},
		{name: "delta seconds", attempt: 1, retryAfter: "7", want: 7 * time.Second},
		{name: "zero seconds", attempt: 1, retryAfter: "0", want: 0},
		{name: "http date", attempt: 1, retryAfter: now.Add(9 * time.Second).Format(http.TimeFormat), want: 9 * time.Second},
		{name: "http date in the past clamps to zero", attempt: 1, retryAfter: now.Add(-time.Hour).Format(http.TimeFormat), want: 0},
		{name: "garbage header falls back to backoff", attempt: 1, retryAfter: "soon please", want: 750 * time.Millisecond},
		{name: "absurd retry-after gives up", attempt: 1, retryAfter: "86400", giveUp: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, giveUp := c.Backoff(tc.attempt, tc.retryAfter)
			if giveUp != tc.giveUp {
				t.Fatalf("giveUp = %v, want %v (d=%v)", giveUp, tc.giveUp, got)
			}
			if !tc.giveUp && got != tc.want {
				t.Fatalf("backoff = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRetriesServerErrorsButNotClientErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		status       int
		wantAttempts int
	}{
		{"429 is retried", http.StatusTooManyRequests, 3},
		{"500 is retried", http.StatusInternalServerError, 3},
		{"503 is retried", http.StatusServiceUnavailable, 3},
		{"404 is an answer", http.StatusNotFound, 1},
		{"403 is an answer", http.StatusForbidden, 1},
		{"200 is an answer", http.StatusOK, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					fmt.Fprint(w, "")
					return
				}
				hits.Add(1)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			c, p := testClient(t, srv, fetch.Options{MaxAttempts: 3})
			resp, err := c.Get(context.Background(), p, srv.URL+"/x")
			if err != nil {
				t.Fatal(err)
			}
			if resp.Attempts != tc.wantAttempts {
				t.Fatalf("attempts = %d, want %d", resp.Attempts, tc.wantAttempts)
			}
			if int(hits.Load()) != tc.wantAttempts {
				t.Fatalf("server saw %d requests, want %d", hits.Load(), tc.wantAttempts)
			}
		})
	}
}

func TestResponseSizeCapAndByteAccounting(t *testing.T) {
	t.Parallel()

	t.Run("declared oversize is refused before transfer", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(make([]byte, 4096)) // Content-Length is set for us
		}))
		defer srv.Close()
		c, p := testClient(t, srv, fetch.Options{MaxResponseBytes: 1024})
		if _, err := c.Get(context.Background(), p, srv.URL+"/x"); !errors.Is(err, fetch.ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})

	t.Run("undeclared oversize is caught while reading", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Chunked: no Content-Length for the cheap check to catch.
			w.Header().Set("Transfer-Encoding", "chunked")
			w.WriteHeader(200)
			for range 8 {
				w.Write(make([]byte, 1024))
				w.(http.Flusher).Flush()
			}
		}))
		defer srv.Close()
		c, p := testClient(t, srv, fetch.Options{MaxResponseBytes: 2048})
		if _, err := c.Get(context.Background(), p, srv.URL+"/x"); !errors.Is(err, fetch.ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})

	t.Run("bytes are accounted", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "0123456789")
		}))
		defer srv.Close()
		c, p := testClient(t, srv, fetch.Options{})
		for range 3 {
			if _, err := c.Get(context.Background(), p, srv.URL+"/x"); err != nil {
				t.Fatal(err)
			}
		}
		// Three 10-byte bodies, plus the robots.txt fetch — which this handler
		// also answers with the same 10 bytes.
		if got := c.TotalBytes(); got != 40 {
			t.Fatalf("TotalBytes = %d, want 40", got)
		}
	})

	t.Run("total budget is a hard stop", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, strings.Repeat("x", 100))
		}))
		defer srv.Close()
		c, p := testClient(t, srv, fetch.Options{TotalByteBudget: 150})
		var lastErr error
		for range 5 {
			if _, err := c.Get(context.Background(), p, srv.URL+"/x"); err != nil {
				lastErr = err
				break
			}
		}
		if !errors.Is(lastErr, fetch.ErrBudgetExhausted) {
			t.Fatalf("err = %v, want ErrBudgetExhausted", lastErr)
		}
	})
}

func TestRobotsDeniedIsReportedNotIgnored(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nDisallow: /manga/\nAllow: /manga/public/\n")
			return
		}
		fmt.Fprint(w, "page body")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	ctx := context.Background()

	if _, err := c.Get(ctx, p, srv.URL+"/manga/secret"); !errors.Is(err, fetch.ErrRobotsDenied) {
		t.Fatalf("err = %v, want ErrRobotsDenied", err)
	}
	if _, err := c.Get(ctx, p, srv.URL+"/manga/public/ok"); err != nil {
		t.Fatalf("allowed path was refused: %v", err)
	}
}

func TestRobotsIsFetchedOnlyOncePerHost(t *testing.T) {
	t.Parallel()
	var robotsHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsHits.Add(1)
			fmt.Fprint(w, "User-agent: *\nDisallow:\n")
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Get(context.Background(), p, fmt.Sprintf("%s/p/%d", srv.URL, i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := robotsHits.Load(); got != 1 {
		t.Fatalf("robots.txt fetched %d times, want 1", got)
	}
}

func TestRobotsFailsOpenOnServerError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
	}{
		{"404 means no robots file", 404},
		{"500 is a blip, not a refusal", 500},
		{"503 is a blip, not a refusal", 503},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					w.WriteHeader(tc.status)
					return
				}
				fmt.Fprint(w, "ok")
			}))
			defer srv.Close()
			c, p := testClient(t, srv, fetch.Options{MaxAttempts: 1})
			if _, err := c.Get(context.Background(), p, srv.URL+"/manga/x"); err != nil {
				t.Fatalf("robots %d should fail open, got %v", tc.status, err)
			}
		})
	}
}

func TestRedirectIsGuarded(t *testing.T) {
	t.Parallel()

	// A server that redirects off its own registrable domain. The guard must
	// stop the hop; without it the client would happily follow.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "")
		case "/away":
			http.Redirect(w, r, "https://somewhere-else.example.invalid/", http.StatusFound)
		case "/local":
			http.Redirect(w, r, "/landed", http.StatusFound)
		default:
			fmt.Fprint(w, "landed")
		}
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})

	if _, err := c.Get(context.Background(), p, srv.URL+"/away"); !errors.Is(err, fetch.ErrBlockedAddress) {
		t.Fatalf("off-domain redirect: err = %v, want ErrBlockedAddress", err)
	}

	resp, err := c.Get(context.Background(), p, srv.URL+"/local")
	if err != nil {
		t.Fatalf("same-host redirect was refused: %v", err)
	}
	if string(resp.Body) != "landed" {
		t.Fatalf("body = %q, want %q", resp.Body, "landed")
	}
	if resp.FinalURL == nil || resp.FinalURL.Path != "/landed" {
		t.Fatalf("FinalURL = %v, want path /landed", resp.FinalURL)
	}
}

func TestNonHTTPSchemeIsRefusedBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	c := fetch.NewClient(fetch.Options{})
	for _, raw := range []string{"file:///etc/passwd", "ftp://x.example.invalid/", "gopher://x.example.invalid/"} {
		if _, err := c.Get(context.Background(), nil, raw); !errors.Is(err, fetch.ErrInvalidURL) {
			t.Errorf("Get(%q) = %v, want ErrInvalidURL", raw, err)
		}
	}
}
