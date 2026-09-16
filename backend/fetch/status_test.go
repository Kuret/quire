package fetch_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

// PLAN §6 M3: "verdicts surface as plain language, not error codes". A user who
// probed a site and was told "HTTP 444" was handed a puzzle: 444 is nginx
// closing the connection without answering — a rate limit or a cheap bot filter
// nearly every time — and it is not in Go's http.StatusText, so it renders as a
// bare number with nothing attached to it.
//
// Each case here asserts two things: the sentence says what the reader can do,
// and the number is not left standing on its own.

func TestStatusSentenceIsActionable(t *testing.T) {
	cases := []struct {
		name string
		code int
		// want are fragments the sentence must contain, lowercased.
		want []string
	}{
		{"too many requests", 429, []string{"rate limiting", "few minutes", "trying again"}},
		// The specific hole: Go does not name 444, so before this it was a
		// bare number and nothing else.
		{"nginx no response", 444, []string{"rate limiting", "few minutes", "trying again"}},
		{"forbidden", 403, []string{"refused this request"}},
		{"bad gateway", 502, []string{"trouble", "later"}},
		{"service unavailable", 503, []string{"trouble", "later"}},
		{"gateway timeout", 504, []string{"trouble", "later"}},
		// A 404 on a path one of our themes expected points at us, not at the
		// site, and saying so is what makes it reportable.
		{"not found", 404, []string{"isn't there", "quire's theme"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fetch.StatusSentence("the site", tc.code)
			low := strings.ToLower(got)
			for _, frag := range tc.want {
				if !strings.Contains(low, frag) {
					t.Errorf("StatusSentence(%d) = %q, missing %q", tc.code, got, frag)
				}
			}
			if strings.Contains(got, strconv.Itoa(tc.code)) {
				t.Errorf("StatusSentence(%d) = %q still shows the bare code", tc.code, got)
			}
			if !strings.Contains(got, "the site") {
				t.Errorf("StatusSentence(%d) = %q does not name the subject", tc.code, got)
			}
		})
	}
}

// A status nobody has a specific answer for keeps its number — throwing it away
// would lose the only fact there is — but it is wrapped in a sentence rather
// than presented naked.
func TestStatusSentenceWrapsUnknownCodes(t *testing.T) {
	got := fetch.StatusSentence("the site", 418)
	if !strings.Contains(got, "418") {
		t.Errorf("StatusSentence(418) = %q dropped the only fact available", got)
	}
	if len(strings.Fields(got)) < 6 {
		t.Errorf("StatusSentence(418) = %q is barely more than the code", got)
	}
}

// PLAN §7.4 already honours Retry-After and backs off. The wording must not
// promise Quire will retry by itself: re-probing is the user's control.
func TestStatusSentenceNeverPromisesAutomaticRetry(t *testing.T) {
	for _, code := range []int{403, 404, 429, 444, 502, 503, 504, 418} {
		low := strings.ToLower(fetch.StatusSentence("the site", code))
		for _, forbidden := range []string{"quire will try", "retrying automatically", "quire is retrying"} {
			if strings.Contains(low, forbidden) {
				t.Errorf("StatusSentence(%d) says %q; the probe does not retry", code, forbidden)
			}
		}
	}
}

func TestStatusSentenceNamesTheSubject(t *testing.T) {
	got := fetch.StatusSentence("the image host", 429)
	if !strings.Contains(got, "the image host") {
		t.Errorf("StatusSentence = %q does not say which host refused", got)
	}
}

// A theme's error reduces to "HTTP 444" once Go's wrapping is trimmed off.
// That is the string that reached the user.
func TestExplainStatus(t *testing.T) {
	cases := []struct{ name, in, wantFrag string }{
		{"bare code", "HTTP 444", "rate limiting"},
		{"bare code with stop", "HTTP 429.", "rate limiting"},
		{"prefixed", "search: HTTP 503", "trouble"},
		{"lowercase", "http 403", "refused this request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fetch.ExplainStatus(tc.in, "the site")
			if !strings.Contains(strings.ToLower(got), tc.wantFrag) {
				t.Errorf("ExplainStatus(%q) = %q, missing %q", tc.in, got, tc.wantFrag)
			}
		})
	}

	t.Run("prefix is kept", func(t *testing.T) {
		got := fetch.ExplainStatus("searching: HTTP 444", "the site")
		if !strings.HasPrefix(got, "searching: ") {
			t.Errorf("ExplainStatus = %q dropped the context of the failure", got)
		}
	})

	// It expands what is there; it does not invent. A message with no status in
	// it comes back untouched.
	t.Run("no status is untouched", func(t *testing.T) {
		in := "dial tcp: no route to host"
		if got := fetch.ExplainStatus(in, "the site"); got != in {
			t.Errorf("ExplainStatus(%q) = %q, want it unchanged", in, got)
		}
	})
}
