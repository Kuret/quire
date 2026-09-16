package fetch_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
)

// The robots.txt consultation is a single global setting, off by default
// (PLAN §7.4, superseded 2026-09-16). These tests cover the three things that
// makes it defensible rather than merely convenient:
//
//  1. off is the default, and a fetch past a Disallow leaves a record;
//  2. on is a setting, not a rewrite — the whole suite in client_test.go and
//     robots_test.go still describes what happens when it is on;
//  3. off narrows *nothing else*. The SSRF guard, the limiter and the challenge
//     gate are what keep this a narrow change, so each is asserted here to be
//     unaffected rather than assumed to be.

// disallowEverything is a site whose robots.txt refuses every path, so any
// request reaching the handler is a request that went past a Disallow.
func disallowEverything(t *testing.T) (srv *httptest.Server, robotsHits, pageHits *atomic.Int64) {
	t.Helper()
	robotsHits = &atomic.Int64{}
	pageHits = &atomic.Int64{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsHits.Add(1)
			fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
			return
		}
		pageHits.Add(1)
		fmt.Fprint(w, "<html><body>content</body></html>")
	}))
	t.Cleanup(srv.Close)
	return srv, robotsHits, pageHits
}

// A client built the way production builds one does not consult robots.txt.
// This is the test that notices if the default is ever flipped back by accident
// rather than by decision.
func TestRobotsIsNotConsultedByDefault(t *testing.T) {
	t.Parallel()

	if c := fetch.NewClient(fetch.Options{}); c.ConsultRobots() {
		t.Fatal("NewClient consults robots.txt by default; PLAN §7.4 says the setting is off unless turned on")
	}

	srv, robotsHits, pageHits := disallowEverything(t)
	c, p := rawTestClient(t, srv, fetch.Options{})

	resp, err := c.Get(context.Background(), p, srv.URL+"/browse")
	if err != nil {
		t.Fatalf("a Disallowed path must be fetched while the setting is off: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := pageHits.Load(); got != 1 {
		t.Fatalf("the page was requested %d time(s), want 1", got)
	}
	// Not consulted means not fetched: asking and then ignoring the answer
	// would be a request we did not need to make.
	if got := robotsHits.Load(); got != 0 {
		t.Errorf("robots.txt was fetched %d time(s) while the setting is off", got)
	}
}

// PLAN §7.4: "Log at info when a fetch proceeds past a Disallow, naming the
// path." A safeguard that is off silently is worse than one that was never
// there, and this line is the only record of what the setting did.
func TestSuppressedRobotsCheckIsLogged(t *testing.T) {
	t.Parallel()

	srv, _, _ := disallowEverything(t)
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c, p := rawTestClient(t, srv, fetch.Options{Logger: logger})

	if _, err := c.Get(context.Background(), p, srv.URL+"/browse/page/2"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	line := buf.String()
	mu.Unlock()

	if !strings.Contains(line, "level=INFO") {
		t.Errorf("the suppressed check was not logged at info level:\n%s", line)
	}
	if !strings.Contains(line, "robots") {
		t.Errorf("the log line does not mention robots.txt:\n%s", line)
	}
	if !strings.Contains(line, "/browse/page/2") {
		t.Errorf("the log line does not name the path it covered:\n%s", line)
	}
	// The owner's argument — a person searching and tapping is browsing, not
	// crawling — holds for the requests a person drives, but the probe and the
	// watch checks run unattended. The line records the request *kind* so a
	// reader can tell those apart, rather than asserting something untrue about
	// who asked.
	if !strings.Contains(line, "kind=discovery") {
		t.Errorf("the log line does not say which kind of request it was:\n%s", line)
	}
}

// Retrieval was never gated by robots.txt, so there is no check to suppress and
// nothing to report. A line claiming otherwise would be noise that misdescribes
// what happened.
func TestRetrievalLogsNoSuppressedCheck(t *testing.T) {
	t.Parallel()

	srv, _, _ := disallowEverything(t)
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c, p := rawTestClient(t, srv, fetch.Options{Logger: logger})

	if _, err := c.GetRetrieval(context.Background(), p, srv.URL+"/series/one/"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(buf.String(), "robots") {
		t.Errorf("a retrieval request reported a suppressed robots check it never had:\n%s", buf.String())
	}
}

// Turning the setting on is a setting, not a rewrite: the same client, the same
// site, and the Disallow is honoured again — including the ErrRobotsDenied
// spelling that PLAN §7.5 turns into the robots_denied verdict.
func TestRobotsSettingTogglesAtRuntime(t *testing.T) {
	t.Parallel()

	srv, robotsHits, _ := disallowEverything(t)
	c, p := rawTestClient(t, srv, fetch.Options{})

	c.SetConsultRobots(true)
	if !c.ConsultRobots() {
		t.Fatal("SetConsultRobots(true) did not take")
	}
	_, err := c.Get(context.Background(), p, srv.URL+"/browse")
	if !errors.Is(err, fetch.ErrRobotsDenied) {
		t.Fatalf("with the setting on, err = %v, want ErrRobotsDenied", err)
	}
	if got := robotsHits.Load(); got == 0 {
		t.Error("robots.txt was never fetched with the setting on")
	}

	c.SetConsultRobots(false)
	if _, err := c.Get(context.Background(), p, srv.URL+"/browse"); err != nil {
		t.Fatalf("with the setting off again, the fetch must proceed: %v", err)
	}
}

// The SSRF guard is not part of this change. PLAN §7.4 lists it separately and
// calls it non-configurable, and 10.0.0.0/8 stays refused with the robots
// setting off exactly as it is with it on.
func TestRobotsOffDoesNotRelaxTheGuard(t *testing.T) {
	t.Parallel()

	srv, _, _ := disallowEverything(t)
	c, p := rawTestClient(t, srv, fetch.Options{})

	// A literal, so the package-wide DNS ban is not in play: the guard is being
	// asked about the address itself.
	_, err := c.Get(context.Background(), p, "http://10.1.2.3/browse")
	if !errors.Is(err, fetch.ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress; the robots setting must not reach the SSRF guard", err)
	}
}

// Nor is the limiter. The per-host minimum delay is the politeness that
// actually protects a site's server, and it applies whether or not robots.txt
// was asked.
func TestRobotsOffDoesNotRelaxTheLimiter(t *testing.T) {
	t.Parallel()

	srv, _, _ := disallowEverything(t)
	var mu sync.Mutex
	var slept []time.Duration
	c, p := rawTestClient(t, srv, fetch.Options{
		Sleep: func(_ context.Context, d time.Duration) error {
			mu.Lock()
			slept = append(slept, d)
			mu.Unlock()
			return nil
		},
	})

	for _, path := range []string{"/browse", "/browse/2"} {
		if _, err := c.Get(context.Background(), p, srv.URL+path); err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	var longest time.Duration
	for _, d := range slept {
		if d > longest {
			longest = d
		}
	}
	if longest <= 0 {
		t.Fatalf("no inter-request delay was applied (%v); the per-host floor must survive the robots setting", slept)
	}
}

// lockedWriter serialises writes from the client's goroutines so the test can
// read the buffer without racing the logger.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
