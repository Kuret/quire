package theme_test

import (
	"testing"

	"github.com/rickl/quire/backend/theme"
)

// PLAN §9's circular-fixture warning, in its second form.
//
// The first form was an invented *string*: madara scored 40 of its 60 points
// on an asset path no live install serves. The second is an invented
// *relationship* — every fixture put its links on the same host as the source
// base, so "links are on the base host" was never a claim any test could
// disprove. A real madara site redirects its apex to www, emits absolute links
// there, and the exact-host filter discarded all of them: a perfect
// fingerprint followed by a search that returned nothing.
//
// Both halves of the repair are needed. This is the rule; the fixtures of six
// themes now carry a link on the sibling host, so removing the rule turns six
// suites red rather than none.
func TestSameSiteAcceptsTheWWWSibling(t *testing.T) {
	cases := []struct {
		host, base string
		want       bool
		why        string
	}{
		{"example.invalid", "example.invalid", true, "the same host"},
		{"www.example.invalid", "example.invalid", true,
			"the user typed the apex and the site redirects to www — the case that cost a live search"},
		{"example.invalid", "www.example.invalid", true, "and the other direction"},
		{"www.example.invalid", "www.example.invalid", true, "both www"},
		{"WWW.Example.Invalid", "example.invalid", true, "hosts are case-insensitive"},
		{" example.invalid ", "example.invalid", true, "and may arrive padded"},

		// The generalisation that was considered and rejected. A theme returns
		// a *path*, so accepting a sibling means the path is later fetched
		// from the base host — fine for www, which serves the same site, and
		// wrong for anything else.
		{"images.example.invalid", "example.invalid", false,
			"a different subdomain is a different server; its paths do not exist on the base"},
		{"cdn.example.invalid", "www.example.invalid", false, "likewise"},
		{"m.example.invalid", "example.invalid", false,
			"a mobile host is a theme's own business to accept, not this rule's"},
		{"example.invalid.evil.test", "example.invalid", false, "a suffix is not a sibling"},
		{"evilexample.invalid", "example.invalid", false, "nor is a prefix"},
		{"elsewhere.invalid", "example.invalid", false, "plainly another site"},
		{"", "example.invalid", false, "no host"},
		{"example.invalid", "", false, "no base"},
		{"www.", "", false, "degenerate input must not collapse to a match"},
	}

	for _, tc := range cases {
		t.Run(tc.host+"|"+tc.base, func(t *testing.T) {
			if got := theme.SameSite(tc.host, tc.base); got != tc.want {
				t.Errorf("SameSite(%q, %q) = %v, want %v — %s", tc.host, tc.base, got, tc.want, tc.why)
			}
		})
	}
}
