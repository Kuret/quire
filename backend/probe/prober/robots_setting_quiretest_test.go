//go:build quiretest

package prober_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
)

// PLAN §7.4's robots.txt setting is off by default, and PLAN §7.6 is untouched
// by that: a challenge is a site *actively refusing us*, which is a different
// thing from an advisory file aimed at crawlers. The stage-3 gate must still be
// terminal with the setting off, and the source still refused.
//
// This runs the real fetch.Client — not the fixture fetcher — against a site
// that both disallows everything in robots.txt and serves a challenge, because
// the claim being tested is about the interaction of the two. The loopback
// exemption is the quiretest-tagged hook (backend/fetch/loopback_quiretest.go);
// the file carries the same tag so it is not compiled into anything shipped.
func TestChallengeGateSurvivesRobotsBeingOff(t *testing.T) {
	var robotsHits int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsHits++
			fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
			return
		}
		// The mitigation stated in a header, as in TestVerdictBlockedChallenge.
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "<html><head><title>example</title></head><body><p>Sorry.</p></body></html>")
	}))
	defer srv.Close()

	client := fetch.NewClient(fetch.AllowLoopbackForTests(fetch.Options{
		Version: "test",
		// Explicitly off: this is the state being tested.
		ConsultRobots: false,
		Sleep:         func(context.Context, time.Duration) error { return nil },
	}))
	if client.ConsultRobots() {
		t.Fatal("the client under test is consulting robots.txt")
	}

	p := prober.New(prober.Options{
		Fetcher:  client,
		Registry: registry(client),
		Guard:    allowGuard{},
		Now:      clock,
		NewID:    func() string { return "src-test" },
	})

	res, err := p.Run(context.Background(), srv.URL, &recordUI{answers: []string{"continue", "continue"}})
	if err != nil {
		t.Fatalf("probe returned an error: %v", err)
	}
	if res.Verdict != theme.VerdictBlockedChallenge {
		t.Fatalf("verdict = %q (%s), want blocked_challenge; PLAN §7.6 is not weakened by the robots setting", res.Verdict, res.Detail)
	}
	if res.Addable || res.Draft != nil {
		t.Fatal("a challenge-protected site must never be addable, robots setting or no robots setting")
	}
	if !strings.Contains(strings.ToLower(res.Detail), "browser challenge") {
		t.Errorf("detail %q does not say plainly that a browser challenge is in the way", res.Detail)
	}
	if robotsHits != 0 {
		t.Errorf("robots.txt was fetched %d time(s) with the setting off", robotsHits)
	}
}
