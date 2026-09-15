package prober_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// stubTheme scores a fixed number and implements just enough of theme.Theme to
// be registered. Stage 4's tie-breaking is about the *scores*, so a real theme
// would only make the arithmetic harder to see.
type stubTheme struct {
	id    string
	score int
}

func (s stubTheme) ID() string                  { return s.id }
func (s stubTheme) Fingerprint(*probe.Page) int { return s.score }
func (s stubTheme) ValidateOverrides(map[string]any) error {
	return nil
}
func (s stubTheme) OverrideKeys() []theme.OverrideDoc { return nil }

func (s stubTheme) Search(context.Context, *theme.Source, string, int) ([]theme.SeriesStub, error) {
	return []theme.SeriesStub{{ID: "/series/one/", Title: "One"}}, nil
}

func (s stubTheme) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return &theme.Series{ID: "/series/one/", Title: "One"}, nil
}

func (s stubTheme) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return []theme.Chapter{{ID: "/series/one/1/", Title: "Chapter 1", Number: 1}}, nil
}

func (s stubTheme) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return []string{"https://example.invalid/p/1.jpg"}, nil
}

// PLAN §7.5 stage 4: "Ties or near-ties go to the user as a choice." Guessing
// between two themes is how a source ends up half-working with no explanation.
func TestNearTieAsksTheUser(t *testing.T) {
	newProber := func(t *testing.T) (*prober.Prober, *themetest.Fetcher) {
		f := themetest.New(t, map[string]themetest.Route{
			"GET /": {File: "home-unrecognised.html"},
		})
		reg := theme.NewRegistry()
		reg.MustRegister(stubTheme{id: "alpha", score: 72})
		reg.MustRegister(stubTheme{id: "beta", score: 68})
		return prober.New(prober.Options{
			Fetcher: f, Registry: reg, Guard: allowGuard{}, Now: clock,
			NewID: func() string { return "src-test" },
		}), f
	}

	t.Run("user picks the runner-up", func(t *testing.T) {
		p, _ := newProber(t)
		ui := &recordUI{answers: []string{"beta"}}
		res, err := p.Run(context.Background(), "https://example.invalid", ui)
		if err != nil {
			t.Fatal(err)
		}
		if len(ui.questions) != 1 || ui.questions[0].Kind != "theme" {
			t.Fatalf("expected one theme question, got %+v", ui.questions)
		}
		if got := len(ui.questions[0].Options); got != 3 {
			t.Errorf("question offered %d options, want both themes plus a way out", got)
		}
		if res.ThemeID != "beta" {
			t.Errorf("theme = %q, want the one the user chose", res.ThemeID)
		}
	})

	t.Run("user declines", func(t *testing.T) {
		p, _ := newProber(t)
		ui := &recordUI{answers: []string{"cancel"}}
		res, err := p.Run(context.Background(), "https://example.invalid", ui)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Cancelled || res.Addable {
			t.Errorf("declining must stop the probe and add nothing: %+v", res)
		}
	})
}

// A clear winner is not a question: only near-ties are.
func TestClearWinnerIsNotAQuestion(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /": {File: "home-unrecognised.html"},
	})
	reg := theme.NewRegistry()
	reg.MustRegister(stubTheme{id: "alpha", score: 90})
	reg.MustRegister(stubTheme{id: "beta", score: 40})
	p := prober.New(prober.Options{
		Fetcher: f, Registry: reg, Guard: allowGuard{}, Now: clock,
		NewID: func() string { return "src-test" },
	})

	ui := &recordUI{}
	res, err := p.Run(context.Background(), "https://example.invalid", ui)
	if err != nil {
		t.Fatal(err)
	}
	if len(ui.questions) != 0 {
		t.Errorf("a clear winner asked the user: %+v", ui.questions)
	}
	if res.ThemeID != "alpha" || res.Verdict != theme.VerdictOK {
		t.Errorf("got %q/%q, want alpha/ok", res.ThemeID, res.Verdict)
	}
}

// PLAN §7.6 has no bypass path, and the probe must not acquire one by
// accident. This is a blunt instrument on purpose: it reads the package's own
// source and fails on the vocabulary of circumvention.
func TestNoBypassVocabularyInSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	// Each of these is something PLAN §7.6 names as forbidden. A legitimate
	// future need for one of these words is a conversation, not a quiet edit.
	banned := []string{
		"mozilla/5.0",
		"chrome/",
		"cf_clearance=",
		"captcha solver",
		"flaresolverr",
		"captcha_key",
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		src := strings.ToLower(string(b))
		for _, word := range banned {
			if strings.Contains(src, word) {
				t.Errorf("%s contains %q — PLAN §7.6 forbids working around a challenge", e.Name(), word)
			}
		}
	}
}

// The signals we ship must be the signals docs/THEME-NOTES.md describes.
// Overclaiming in that file is the specific failure PLAN §7.5 warns about, and
// silently adding a marker nobody documented is how it starts.
func TestEveryChallengeMarkerIsDocumented(t *testing.T) {
	notes, err := os.ReadFile("../../../docs/THEME-NOTES.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ToLower(string(notes))
	for _, line := range prober.DescribeSignals() {
		marker, _, _ := strings.Cut(line, " → ")
		if !strings.Contains(doc, strings.ToLower(marker)) {
			t.Errorf("challenge marker %q is not described in docs/THEME-NOTES.md", marker)
		}
	}
}

// Guard against the probe treating an ordinary page behind a CDN as a
// challenge. Most of the web is behind one of these; refusing all of it would
// be far worse than missing a challenge.
func TestOrdinarySiteBehindACDNIsNotRefused(t *testing.T) {
	routes := madaraRoutes()
	routes["GET /"] = themetest.Route{
		File: "home-madara.html",
		Header: map[string][]string{
			"Server":     {"cloudflare"},
			"Cf-Ray":     {"0000000000000000-AMS"},
			"Set-Cookie": {"cf_clearance=abc; Path=/"},
		},
	}
	res := run(t, routes, &recordUI{})
	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s); a readable site behind a CDN must not be refused", res.Verdict, res.Detail)
	}
}
