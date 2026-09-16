package prober_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §6 M3: a verdict is a complete, final answer in plain language, not an
// error code. A real probe of a site answered:
//
//	"Quire recognised this site but searching it didn't work. HTTP 444. …"
//
// The verdict was right — the site genuinely could not be searched — but the
// user was left with a status code and nothing to do. 444 is nginx closing the
// connection without answering.
//
// What it is *not* is a diagnosis. Measured the same day, that site answers 444
// to every search query shorter than three characters and 200 to longer ones,
// with no rate limit involved. So these tests check both halves: the sentence
// says what happened, and does not claim a cause the status cannot tell us.

// The exact shape that produced the bare code: a theme error whose tail is a
// status Go does not even have a name for.
func TestSearchRefusalSaysWhatTheCodeMeans(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want []string
		// gone is the bare code, which must not survive into the sentence.
		gone string
	}{
		{
			// 444 says the server dropped us and nothing more. Measured on
			// 2026-09-16: comick.art answers 444 to any query under three
			// characters, so "rate limiting" here would be a false cause.
			name: "nginx 444",
			err:  errors.New("comick: /v1.0/search: HTTP 444"),
			want: []string{"closed the connection without answering"},
			gone: "444",
		},
		{
			name: "too many requests",
			err:  errors.New("comick: /v1.0/search: HTTP 429"),
			want: []string{"asking too often", "few minutes"},
			gone: "429",
		},
		{
			name: "forbidden",
			err:  errors.New("comick: /v1.0/search: HTTP 403"),
			want: []string{"refused this request"},
			gone: "403",
		},
		{
			name: "site trouble",
			err:  errors.New("comick: /v1.0/search: HTTP 503"),
			want: []string{"couldn't serve the request", "later"},
			gone: "503",
		},
		{
			name: "the address is gone",
			err:  errors.New("comick: /v1.0/search: HTTP 404"),
			want: []string{"isn't there", "may mean"},
			gone: "404",
		},
	}

	routes := map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runWithTheme(t, routes, stubTheme{id: "alpha", score: 90, searchErr: tc.err})

			// The classification is unchanged: this is about the wording.
			if res.Verdict != theme.VerdictPartial {
				t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
			}
			if res.Addable {
				t.Fatal("a source that cannot be searched must not be addable")
			}
			low := strings.ToLower(res.Detail)
			for _, frag := range tc.want {
				if !strings.Contains(low, frag) {
					t.Errorf("detail %q does not tell the user %q", res.Detail, frag)
				}
			}
			if strings.Contains(res.Detail, tc.gone) {
				t.Errorf("detail %q still hands the user a bare status code", res.Detail)
			}
			// PLAN §7.5: a challenge is a challenge and nothing else is one.
			if strings.Contains(low, "challenge") {
				t.Errorf("detail %q calls an ordinary refusal a challenge", res.Detail)
			}
		})
	}
}

// Stage 2 reaches the homepage itself, and had the same bare-code hole.
func TestReachabilityRefusalSaysWhatTheCodeMeans(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   []string
		gone   string
	}{
		{"nginx 444", 444, []string{"closed the connection without answering"}, "444"},
		{"too many requests", http.StatusTooManyRequests, []string{"asking too often", "few minutes"}, "429"},
		{"site trouble", http.StatusBadGateway, []string{"couldn't serve the request", "later"}, "502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, map[string]themetest.Route{
				"GET /": {Status: tc.status, Body: "<html><body><p>No.</p></body></html>"},
			}, &recordUI{})

			if res.Verdict != theme.VerdictUnreachable {
				t.Fatalf("verdict = %q (%s), want unreachable", res.Verdict, res.Detail)
			}
			low := strings.ToLower(res.Detail)
			for _, frag := range tc.want {
				if !strings.Contains(low, frag) {
					t.Errorf("detail %q does not tell the user %q", res.Detail, frag)
				}
			}
			if strings.Contains(res.Detail, tc.gone) {
				t.Errorf("detail %q still hands the user a bare status code", res.Detail)
			}
		})
	}
}

// Stage 5's image fetch is the other place a status reached the user as a bare
// number ("the image host answered 429.").
func TestImageHostRateLimitSaysWhatToDo(t *testing.T) {
	res := runWithTheme(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": {Status: http.StatusTooManyRequests, Body: "slow down"},
	}, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictPartial {
		t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
	}
	low := strings.ToLower(res.Detail)
	if !strings.Contains(low, "asking too often") || !strings.Contains(low, "few minutes") {
		t.Errorf("detail %q does not say what a 429 on the image host means", res.Detail)
	}
	if !strings.Contains(low, "image host") {
		t.Errorf("detail %q does not say which host refused", res.Detail)
	}
	if strings.Contains(res.Detail, "429") {
		t.Errorf("detail %q still hands the user a bare status code", res.Detail)
	}
}

// PLAN §7.5 and §7.6: a challenge is terminal and is worded on its own terms.
// "Maybe it's rate limiting, try again in a few minutes" must never leak into
// that path — it would turn a final refusal into a false hope.
func TestChallengeWordingIsUnchangedByStatusWording(t *testing.T) {
	res := runWithTheme(t, map[string]themetest.Route{
		"GET /": {File: "home-unrecognised.html"},
		"GET /p/1.jpg": {
			Status: http.StatusForbidden,
			Body:   "<html><head><title>Just a moment...</title></head><body></body></html>",
			Header: http.Header{"Cf-Mitigated": []string{"challenge"}},
		},
	}, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictBlockedChallenge {
		t.Fatalf("verdict = %q (%s), want blocked_challenge", res.Verdict, res.Detail)
	}
	low := strings.ToLower(res.Detail)
	if !strings.Contains(low, "browser challenge") {
		t.Errorf("detail %q no longer says plainly that a browser challenge is in the way", res.Detail)
	}
	if !strings.Contains(low, "doesn't work around challenges") {
		t.Errorf("detail %q no longer says Quire will not work around the challenge", res.Detail)
	}
	for _, forbidden := range []string{"rate limiting", "asking too often", "few minutes", "try again", "trying again", "later"} {
		if strings.Contains(low, forbidden) {
			t.Errorf("detail %q leaks %q into a terminal challenge verdict", res.Detail, forbidden)
		}
	}
}
