package theme_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/mangathemesia"
)

// This file is the cross-theme half of PLAN §7.5 stage 4: it is not enough
// that each theme recognises its own pages, they must also *fail* to recognise
// each other's. A confident wrong answer is worse than "unrecognised", because
// it turns every later empty result into a mystery.
//
// The confidence threshold. Stage 4 needs two numbers, not one: a score at or
// above this means "this is the theme"; a best score below it means the probe
// should say unrecognised rather than guess.
const confident = 60

// loadPage reads a fixture and wraps it as a probed page.
func loadPage(t *testing.T, path string) *probe.Page {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	u, err := url.Parse("https://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	return probe.NewPage(u, nil, 200, nil, b)
}

func newRegistry(t *testing.T) *theme.Registry {
	t.Helper()
	reg := theme.NewRegistry()
	if err := reg.Register(madara.New(nil)); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(mangathemesia.New(nil)); err != nil {
		t.Fatal(err)
	}
	// mangadex is registered here for the same reason the other two are: it
	// must score 0 on every page below. It gates on the host rather than on
	// markup, and every fixture page is served from example.invalid, so a
	// non-zero score would mean the gate had stopped gating.
	if err := reg.Register(mangadex.New(nil)); err != nil {
		t.Fatal(err)
	}
	return reg
}

// scorers is how many themes Registry.Fingerprint scores. The generic escape
// hatch is excluded by the registry itself, so it is not counted here.
const scorers = 3

func TestFingerprintDistinguishesTheTwoThemes(t *testing.T) {
	reg := newRegistry(t)

	tests := []struct {
		name string
		file string
		// wantWinner is the theme that must come first, or "" when the page
		// must be recognised by nobody.
		wantWinner string
	}{
		{
			name:       "a madara series page",
			file:       "madara/testdata/series.html",
			wantWinner: madara.ID,
		},
		{
			name:       "a madara search page",
			file:       "madara/testdata/search.html",
			wantWinner: madara.ID,
		},
		{
			name:       "a madara reader page",
			file:       "madara/testdata/reader.html",
			wantWinner: madara.ID,
		},
		{
			name:       "a mangathemesia series page",
			file:       "mangathemesia/testdata/series.html",
			wantWinner: mangathemesia.ID,
		},
		{
			name:       "a mangathemesia search page",
			file:       "mangathemesia/testdata/search.html",
			wantWinner: mangathemesia.ID,
		},
		{
			name:       "a mangathemesia reader page",
			file:       "mangathemesia/testdata/reader.html",
			wantWinner: mangathemesia.ID,
		},
		{
			// The deliberate near-miss: WordPress, comic-shaped, and neither
			// family. Nobody may claim it.
			name:       "a WordPress comic site of neither family",
			file:       "testdata/near-miss-wordpress.html",
			wantWinner: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := loadPage(t, filepath.FromSlash(tc.file))
			scores := reg.Fingerprint(page)
			// Every registered theme must score, so the count tracks the
			// registry above rather than being a literal that quietly drifts
			// when a theme is added and stops being asked.
			if len(scores) != scorers {
				t.Fatalf("got %d scores, want %d", len(scores), scorers)
			}
			t.Logf("scores: %+v", scores)

			if tc.wantWinner == "" {
				if scores[0].Score >= confident {
					t.Fatalf("%s scored %d on a page of neither family; the probe would mis-assign it",
						scores[0].ThemeID, scores[0].Score)
				}
				return
			}

			if scores[0].ThemeID != tc.wantWinner {
				t.Fatalf("winner = %s (%d), want %s; full scores %+v",
					scores[0].ThemeID, scores[0].Score, tc.wantWinner, scores)
			}
			if scores[0].Score < confident {
				t.Errorf("%s scored only %d on its own page; the probe would call it unrecognised",
					scores[0].ThemeID, scores[0].Score)
			}
			// The loser must not merely lose, it must be nowhere near.
			if scores[1].Score >= confident {
				t.Errorf("%s also scored %d on a %s page; the two themes are not distinguishable",
					scores[1].ThemeID, scores[1].Score, tc.wantWinner)
			}
			if gap := scores[0].Score - scores[1].Score; gap < 30 {
				t.Errorf("the gap between %s (%d) and %s (%d) is only %d; too close to call reliably",
					scores[0].ThemeID, scores[0].Score, scores[1].ThemeID, scores[1].Score, gap)
			}
		})
	}
}

// TestFingerprintOfAnEmptyPage checks the degenerate inputs the probe will
// meet in the wild: a site that answered with nothing, or with something that
// is not HTML at all. Neither may produce a confident answer, and neither may
// panic.
func TestFingerprintOfDegenerateInput(t *testing.T) {
	reg := newRegistry(t)
	u, err := url.Parse("https://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		body string
	}{
		{"an empty body", ""},
		{"whitespace", "   \n\t "},
		{"JSON, not HTML", `{"error":"not found"}`},
		{"a bare error string", "403 Forbidden"},
		{"an unterminated tag", "<html><body><div class=\"wp-manga"},
		{"binary rubbish", "\x00\x01\x02\xff\xfe"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scores := reg.Fingerprint(probe.NewPage(u, nil, 200, nil, []byte(tc.body)))
			for _, s := range scores {
				if s.Score >= confident {
					t.Errorf("%s scored %d on %s", s.ThemeID, s.Score, tc.name)
				}
			}
		})
	}
}

// TestPageDocumentIsParsedOnce pins the reason probe.Page exists at all: stage
// 4 asks every theme to score the same page, and the device cannot afford to
// re-parse a few hundred KiB of HTML once per theme.
func TestPageDocumentIsParsedOnce(t *testing.T) {
	page := loadPage(t, filepath.FromSlash("madara/testdata/series.html"))
	first, err := page.Document()
	if err != nil {
		t.Fatal(err)
	}
	second, err := page.Document()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("Document() reparsed the page; every theme's Fingerprint would pay for it")
	}
	if got := page.Title(); got == "" {
		t.Error("Title() is empty; PLAN §7.5 stage 6 defaults a source name from it")
	}
}
