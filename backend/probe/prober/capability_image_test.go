package prober_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.5 stage 5, corrected 2026-09-16: **fetch** one page image, do not
// merely extract its URL.
//
// The correction was forced by a real probe. A site extracted 76 page URLs
// perfectly and then answered 403 with a Cloudflare interstitial on the image
// host. Under the old check that site was accepted as `ok` and the user found
// out minutes later, when a download failed, that it was useless. These tests
// are the three answers the fetch can give, kept apart on purpose: a challenge
// is terminal, an ordinary refusal is `partial`, and neither may be reported as
// the other.

// The whole reason this step exists. Extraction succeeds; the image host
// challenges; the source is refused, not accepted with a note.
func TestImageHostChallengeIsTerminal(t *testing.T) {
	routes := map[string]themetest.Route{
		"GET /": {File: "home-unrecognised.html"},
		"GET /p/1.jpg": {
			Status: http.StatusForbidden,
			Body:   "<html><head><title>Just a moment...</title></head><body></body></html>",
			Header: http.Header{"Cf-Mitigated": []string{"challenge"}},
		},
	}
	res := runWithTheme(t, routes, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictBlockedChallenge {
		t.Fatalf("verdict = %q (%s), want blocked_challenge", res.Verdict, res.Detail)
	}
	// PLAN §7.6: terminal. Not degraded, not retried, not offered.
	if res.Addable || res.Draft != nil {
		t.Fatal("a site whose images are behind a challenge must not be addable")
	}
	// A bare blocked_challenge after a search that worked is baffling. The
	// sentence has to say where the challenge was.
	low := strings.ToLower(res.Detail)
	if !strings.Contains(low, "browser challenge") {
		t.Errorf("detail %q does not say plainly that a browser challenge is in the way", res.Detail)
	}
	if !strings.Contains(low, "example.invalid") || !strings.Contains(low, "pages") {
		t.Errorf("detail %q does not say which host serves the pages", res.Detail)
	}
	// PLAN §6 M3: a complete final answer, not a retry prompt.
	for _, forbidden := range []string{"try again", "retry", "workaround"} {
		if strings.Contains(low, forbidden) {
			t.Errorf("detail %q suggests %q; the verdict is final", res.Detail, forbidden)
		}
	}
}

// A refusal with no challenge markers is a refusal, not a challenge. Claiming
// one would assert something we did not observe, and would send the reader
// looking for a CAPTCHA that is not there.
func TestImageHostRefusalWithoutMarkersIsPartial(t *testing.T) {
	routes := map[string]themetest.Route{
		"GET /": {File: "home-unrecognised.html"},
		"GET /p/1.jpg": {
			Status: http.StatusForbidden,
			Body:   "denied",
		},
	}
	res := runWithTheme(t, routes, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictPartial {
		t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
	}
	if res.Addable || res.Draft != nil {
		t.Fatal("a source whose pages cannot be fetched is no use, so nothing may be added")
	}
	if !strings.Contains(res.Detail, "couldn't fetch one") {
		t.Errorf("detail %q does not name page fetching as the failing step", res.Detail)
	}
	if strings.Contains(strings.ToLower(res.Detail), "challenge") {
		t.Errorf("detail %q calls an unmarked 403 a challenge", res.Detail)
	}
}

// A 200 that is not an image — a soft-404 page, an interstitial served with a
// cheerful status — is also a failure. "Something arrived" is not the question.
func TestImageThatIsNotAnImageIsPartial(t *testing.T) {
	routes := map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": {Body: "<html><body><p>Image not found. Browse our catalogue instead.</p></body></html>"},
	}
	res := runWithTheme(t, routes, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictPartial {
		t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
	}
	if !strings.Contains(res.Detail, "not an image") {
		t.Errorf("detail %q does not say what actually came back", res.Detail)
	}
}

// The happy path still passes, and the image really was fetched — otherwise
// this whole change is a comment.
func TestImageFetchedOnTheHappyPath(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	res := runWithFetcher(t, f, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
	if !f.Requested("GET", "/p/1.jpg") {
		t.Error("stage 5 never fetched the page image it extracted")
	}
}
