package theme_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Absolute adequacy, as distinct from relative ordering.
//
// The rest of fingerprint_test.go proves that each theme beats every other
// theme on its own pages. That is necessary and it is not sufficient, and on
// 2026-09-16 the difference cost a user the largest family we support: madara
// won on every fixture by miles, and scored **43 against a threshold of 60** on
// a real page of a real madara site, because the fixtures it had been tuned
// against invented the plugin path that carried 40 of its points.
//
// docs/THEME-NOTES.md had predicted this in "What the fixtures do and do not
// prove": fixtures pin our parser, not reality. The corollary nobody had drawn
// is that **a fixture whose score does not resemble the real page's is worse
// than no fixture** — it is evidence that is not evidence, and it is what a
// green suite was resting on.
//
// So this file asserts two things the old suite never did:
//
//  1. every page a probe might land on clears the threshold **with margin**,
//     not merely above zero and not merely first;
//  2. the *home* page does so on its own, because that is the page the user
//     pastes and the only one stage 4 usually sees.
//
// It cannot check fidelity itself — that needs the live site, and PLAN §1.3
// forbids reaching one from here. What it can do is make the number explicit,
// so that the next person to tune a fingerprint against a fixture has to look
// at it.

// margin is how far above the threshold a landing page of the family is
// expected to score.
//
// Ten, and the number is measured rather than chosen: scored against live home
// pages on 2026-09-16, madara came to 100 (clamped, 111 raw), mangathemesia 85,
// mangakakalot 70, weebcentral 95, webtoons 90, fanfox 70 and comick 100. The
// weakest real landing page of any family we ship is 70, so ten points is what
// the evidence supports.
//
// comick is in that list only after a repair: it measured **25** on its real
// root on the same day, because its fingerprint had been built from the pages
// the theme reads rather than the page the probe judges. Re-measured at 100
// after the rebuild. That is the second time this exact mistake has been found
// by running the real prober, which is why §9 now requires the measurement. Raising it would be a number nobody had measured; dropping it would
// re-admit the state madara was in, where one renamed class name was the
// difference between "recognised" and "unrecognised".
const margin = 10

func TestFingerprintsClearTheThresholdWithMargin(t *testing.T) {
	// The pages a probe can plausibly *land* on: a site root, a listing, a
	// search, a series. Those are what stage 4 judges, and the home page most
	// of all — it is what the user pastes.
	//
	// Reader fixtures are deliberately absent, and that is a judgement worth
	// stating. A reader page is reached after a source is added, never by the
	// probe in normal use, and several of ours are cut down to the reader
	// markup a parser test needs. Requiring them to clear a margin would mean
	// inflating fingerprint weights to satisfy a fixture rather than a site —
	// which is the exact mistake this file exists to prevent.
	//
	// Listed rather than globbed, because some fixtures are deliberately *not*
	// ordinary pages of the family — an empty listing, an error body — and a
	// glob would either fail on those or quietly stop checking when one was
	// added.
	cases := []struct {
		theme string
		files []string
	}{
		{"madara", []string{
			"madara/testdata/search.html",
			"madara/testdata/series.html",
			"madara/testdata/series-inline-chapters.html",
			"../probe/prober/testdata/home-madara.html",
		}},
		{"mangathemesia", []string{
			"mangathemesia/testdata/search.html",
			"mangathemesia/testdata/series.html",
		}},
		{"mangakakalot", []string{
			"mangakakalot/testdata/home.html",
			"mangakakalot/testdata/browse.html",
			"mangakakalot/testdata/series.html",
		}},
		// The four added on 2026-09-16, each measured against its real root on
		// the same day: weebcentral 95, webtoons 90, fanfox 70, comick 100.
		{"weebcentral", []string{
			"weebcentral/testdata/home.html",
			"weebcentral/testdata/series.html",
		}},
		{"webtoons", []string{
			"webtoons/testdata/home.html",
			"webtoons/testdata/search.html",
			"webtoons/testdata/series.html",
		}},
		{"fanfox", []string{
			"fanfox/testdata/home.html",
			"fanfox/testdata/search.html",
			"fanfox/testdata/series.html",
			"fanfox/testdata/directory.html",
		}},
		{"comick", []string{
			"comick/testdata/home.html",
			"comick/testdata/series.html",
		}},
	}

	reg := newRegistry(t)
	for _, tc := range cases {
		for _, f := range tc.files {
			t.Run(tc.theme+"/"+filepath.Base(f), func(t *testing.T) {
				p := loadPage(t, f)
				scores := reg.Fingerprint(p)

				var got int
				for _, s := range scores {
					if s.ThemeID == tc.theme {
						got = s.Score
					}
				}
				if got < confident+margin {
					t.Errorf("%s scored %d on %s; the threshold is %d and a real page of the family "+
						"should clear it by at least %d. A fingerprint this close to the line fails "+
						"the first time a site changes a class name.",
						tc.theme, got, f, confident, margin)
				}
			})
		}
	}
}

// Renderings we could not observe live, and are therefore not entitled to make
// a confident claim about. They must clear the threshold — otherwise they are
// not doing their job at all — but no margin is asserted, because the number a
// real page of this shape scores is unknown to us.
//
// mangakakalot's search page is the case: every mirror answered `/search/story/`
// with a Cloudflare challenge (see docs/THEME-NOTES.md), so its markup here
// follows the selector set an existing extension documents rather than anything
// measured. Asserting a margin on it would be asserting confidence we do not
// have — which is the whole failure mode this file exists to catch.
func TestUnverifiedRenderingsAtLeastClearTheThreshold(t *testing.T) {
	reg := newRegistry(t)
	cases := map[string]string{
		"mangakakalot": "mangakakalot/testdata/search.html",
		// An htmx *fragment*, not a page: this site's search box swaps rows
		// into the page it is already on, and a probe lands on /search, which
		// is a full document and measured 75 live on 2026-09-16. The fragment
		// carries no header and no chrome, so holding it to a landing page's
		// margin would mean inflating weights for a rendering no probe sees —
		// the mistake this file exists to catch, pointed the other way.
		"weebcentral": "weebcentral/testdata/search.html",
	}
	for id, f := range cases {
		t.Run(id+"/"+filepath.Base(f), func(t *testing.T) {
			scores := reg.Fingerprint(loadPage(t, f))
			if len(scores) == 0 || scores[0].ThemeID != id || scores[0].Score < confident {
				t.Errorf("%s did not clear the threshold on %s: %+v", id, f, scores)
			}
		})
	}
}

// The home page is the page the probe actually judges, and it carries none of
// the series, reader or search markup the other fixtures do. A theme that only
// clears the threshold on a series page is a theme the user cannot add.
func TestHomePagesAreRecognisedOnTheirOwn(t *testing.T) {
	reg := newRegistry(t)
	cases := map[string]string{
		"madara":       "../probe/prober/testdata/home-madara.html",
		"mangakakalot": "mangakakalot/testdata/home.html",
		"weebcentral":  "weebcentral/testdata/home.html",
		"webtoons":     "webtoons/testdata/home.html",
		"fanfox":       "fanfox/testdata/home.html",
		// comick's is the one that was missing entirely, which is how its
		// fingerprint came to score 25 on the real thing.
		"comick": "comick/testdata/home.html",
	}
	for id, f := range cases {
		t.Run(id, func(t *testing.T) {
			scores := reg.Fingerprint(loadPage(t, f))
			if len(scores) == 0 || scores[0].ThemeID != id {
				t.Fatalf("%s did not win its own home page: %+v", id, scores)
			}
			if scores[0].Score < confident+margin {
				t.Errorf("%s scored %d on its home page, want at least %d", id, scores[0].Score, confident+margin)
			}
		})
	}
}

// The specific lie that started it: the asset path our fixtures used did not
// exist on any live install, and the fingerprint checked only that one. Both
// halves are pinned so neither can quietly come back.
func TestMadaraAssetPathsAreTheOnesLiveSitesServe(t *testing.T) {
	src, err := os.ReadFile("madara/madara.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/wp-content/themes/madara/", "/wp-content/plugins/madara-core/"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("madara's fingerprint no longer accepts %q, which is what live installs serve", want)
		}
	}

	for _, f := range []string{
		"madara/testdata/search.html",
		"madara/testdata/series.html",
		"madara/testdata/reader.html",
		"../probe/prober/testdata/home-madara.html",
	} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// Comments are stripped first: the fixtures' own headers *quote* the
		// wrong path to explain the correction, and probe.Page.Contains
		// strips comments before matching for the same reason.
		if strings.Contains(stripComments(string(b)), "/wp-content/plugins/madara/") {
			t.Errorf("%s still serves assets from /wp-content/plugins/madara/, a path no live install uses; "+
				"a fixture that invents a signal is how the fingerprint came to score 43 on a real site", f)
		}
	}
}

// stripComments removes HTML comments, so a fixture may describe its own
// history without that description counting as markup.
func stripComments(s string) string {
	for {
		i := strings.Index(s, "<!--")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "-->")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+3:]
	}
}
