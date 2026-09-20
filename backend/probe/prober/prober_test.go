package prober_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangathemesia"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test here runs against committed fixtures with the resolver disabled,
// so "just point it at a real site to check" is a red test rather than a quiet
// network call (PLAN §6 M2, docs/THEME-NOTES.md).
//
// Read that file's fidelity caveat before treating a green run as proof: these
// fixtures pin *our* stage logic. What proves a live site works is stage 5 at
// runtime, against whatever site the user chose.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

var fixedNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixedNow }

// registry is the production fan-out: both tier-1 themes, plus the generic
// escape hatch, which Registry.Fingerprint excludes from scoring.
func registry(f theme.Fetcher) *theme.Registry {
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, clock))
	reg.MustRegister(mangathemesia.NewWithClock(f, clock))
	reg.MustRegister(generic.NewWithClock(f, clock))
	return reg
}

// allowGuard is a stage 1 guard that permits everything. The guard itself has
// its own tests in backend/fetch; what these tests need is control over its
// answer.
type allowGuard struct{}

func (allowGuard) CheckURL(context.Context, *url.URL, *fetch.Policy) error { return nil }

// refuseGuard answers with a real fetch.GuardError, so the mapping from guard
// error to verdict is exercised rather than mocked.
type refuseGuard struct{ kind error }

func (g refuseGuard) CheckURL(_ context.Context, u *url.URL, _ *fetch.Policy) error {
	return &fetch.GuardError{URL: u.Redacted(), Reason: "test refusal", Kind: g.kind}
}

// recordUI collects progress and answers questions from a script.
type recordUI struct {
	progress  []prober.Progress
	questions []prober.Question
	answers   []string
}

func (u *recordUI) Progress(p prober.Progress) { u.progress = append(u.progress, p) }

func (u *recordUI) Ask(_ context.Context, q prober.Question) (prober.Answer, error) {
	u.questions = append(u.questions, q)
	if len(u.answers) == 0 {
		return prober.Answer{ID: "cancel"}, nil
	}
	a := u.answers[0]
	u.answers = u.answers[1:]
	return prober.Answer{ID: a}, nil
}

// madaraRoutes is the full happy path: a home page to fingerprint, then the
// four requests stage 5 makes.
func madaraRoutes() map[string]themetest.Route {
	return map[string]themetest.Route{
		"GET /":                          {File: "home-madara.html"},
		"GET /?post_type=wp-manga&s=":    {File: "search.html"},
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
		"POST /manga/the-lantern-keeper/ajax/chapters/": {File: "chapters-ajax.html"},
		"GET /manga/the-lantern-keeper/chapter-4/":      {File: "reader.html"},
		// PLAN §7.5 stage 5 fetches one page image rather than only extracting
		// its URL (corrected 2026-09-16), so the happy path has one more route
		// than it used to. It is a route like any other: no test here reaches
		// the network, and an unrouted image is a failing test rather than a
		// quiet call out.
		"GET /pages/lantern-keeper/4/001.jpg": imageRoute(),
	}
}

// imageRoute is a one-pixel response that looks like what an image host sends:
// a declared image content type and bytes that sniff as one.
func imageRoute() themetest.Route {
	return themetest.Route{
		Body:   string(onePixelPNG),
		Header: http.Header{"Content-Type": []string{"image/png"}},
	}
}

// onePixelPNG is the smallest real PNG: signature, IHDR, IDAT, IEND. Written
// out rather than base64-decoded so that what it is stays visible.
var onePixelPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

func run(t *testing.T, routes map[string]themetest.Route, ui prober.UI, opts ...func(*prober.Options)) prober.Result {
	t.Helper()
	f := themetest.New(t, routes)
	o := prober.Options{
		Fetcher:  f,
		Registry: registry(f),
		Guard:    allowGuard{},
		Now:      clock,
		NewID:    func() string { return "src-test" },
	}
	for _, fn := range opts {
		fn(&o)
	}
	res, err := prober.New(o).Run(context.Background(), "https://example.invalid", ui)
	if err != nil {
		t.Fatalf("probe returned an error: %v", err)
	}
	return res
}

// PLAN §6 M3 acceptance, case 1: paste a URL of a supported theme and the
// source is added and browsable with no manual configuration.
func TestVerdictOK(t *testing.T) {
	ui := &recordUI{}
	res := run(t, madaraRoutes(), ui)

	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
	}
	if !res.Addable || res.Draft == nil {
		t.Fatalf("an ok verdict must be addable with a draft; got addable=%v draft=%v", res.Addable, res.Draft)
	}
	if res.ThemeID != madara.ID {
		t.Errorf("theme = %q, want %q", res.ThemeID, madara.ID)
	}

	d := res.Draft
	// Stage 6: name from the site title, with the marketing tail trimmed.
	if d.Name != "Example Reader" {
		t.Errorf("draft name = %q, want %q", d.Name, "Example Reader")
	}
	// lang comes from <html lang="en-GB">, narrowed to the base tag.
	if d.Lang != "en" {
		t.Errorf("draft lang = %q, want %q", d.Lang, "en")
	}
	if d.BaseURL != "https://example.invalid" {
		t.Errorf("draft baseUrl = %q", d.BaseURL)
	}
	// No manual configuration: the draft carries no overrides at all.
	if len(d.Overrides) != 0 {
		t.Errorf("draft carries overrides %v; a supported theme must need none", d.Overrides)
	}
	if d.LastProbe == nil || d.LastProbe.Verdict != theme.VerdictOK || !d.LastProbe.At.Equal(fixedNow) {
		t.Errorf("draft lastProbe = %+v, want ok at the probe clock", d.LastProbe)
	}
	// The draft must pass the same gate every source passes.
	if err := registry(themetest.New(t, nil)).Validate(d); err != nil {
		t.Errorf("draft does not validate: %v", err)
	}

	if len(ui.questions) != 0 {
		t.Errorf("a clean probe asked the user %d question(s): %+v", len(ui.questions), ui.questions)
	}
}

// PLAN §7.5: "Stream progress to the UI so a slow site doesn't look hung."
func TestProgressStreamsEveryStage(t *testing.T) {
	ui := &recordUI{}
	run(t, madaraRoutes(), ui)

	started := map[int]bool{}
	finished := map[int]bool{}
	for _, p := range ui.progress {
		if p.Total != prober.StageCount {
			t.Errorf("progress %+v: Total = %d, want %d", p, p.Total, prober.StageCount)
		}
		if p.Name == "" {
			t.Errorf("progress %+v has no stage name", p)
		}
		if p.Done {
			finished[p.Stage] = true
		} else {
			started[p.Stage] = true
		}
	}
	for stage := 1; stage <= prober.StageCount; stage++ {
		if !started[stage] || !finished[stage] {
			t.Errorf("stage %d: started=%v finished=%v, want both", stage, started[stage], finished[stage])
		}
	}
}

// PLAN §6 M3 acceptance, case 3: an unsupported shape gets a clear
// "unrecognised" naming what was tried.
func TestVerdictUnrecognised(t *testing.T) {
	res := run(t, map[string]themetest.Route{
		"GET /": {File: "home-unrecognised.html"},
	}, &recordUI{})

	if res.Verdict != theme.VerdictUnrecognised {
		t.Fatalf("verdict = %q (%s), want unrecognised", res.Verdict, res.Detail)
	}
	if res.Addable || res.Draft != nil {
		t.Fatal("an unrecognised site must not be addable")
	}
	for _, id := range []string{madara.ID, mangathemesia.ID} {
		if !strings.Contains(res.Detail, id) {
			t.Errorf("detail %q does not name the theme %q that was tried", res.Detail, id)
		}
		if _, ok := res.ThemeScores[id]; !ok {
			t.Errorf("themeScores missing %q: %v", id, res.ThemeScores)
		}
	}
	// The escape hatch must not appear: letting it score would turn "we don't
	// recognise this site" into "we sort of recognise it".
	if _, ok := res.ThemeScores[theme.GenericID]; ok {
		t.Errorf("the generic theme scored: %v", res.ThemeScores)
	}
}

// PLAN §6 M3 acceptance, case 2: a challenge-protected URL is refused clearly
// and nothing is added. PLAN §7.6: there is no bypass.
func TestVerdictBlockedChallenge(t *testing.T) {
	cases := []struct {
		name  string
		route themetest.Route
	}{
		{
			// A vendor's own challenge-platform asset path, on the 503 the
			// managed edge serves with it.
			name: "edge interstitial",
			route: themetest.Route{
				File:   "home-challenge.html",
				Status: http.StatusServiceUnavailable,
				Header: http.Header{
					"Server": []string{"cloudflare"},
					"Cf-Ray": []string{"0000000000000000-AMS"},
				},
			},
		},
		{
			// The mitigation stated in a header, with an otherwise ordinary
			// looking body.
			name: "cf-mitigated header",
			route: themetest.Route{
				Status: http.StatusForbidden,
				Body:   "<html><head><title>example.invalid</title></head><body><p>Sorry.</p></body></html>",
				Header: http.Header{"Cf-Mitigated": []string{"challenge"}},
			},
		},
		{
			// A clearance cookie issued where we can see no content at all.
			name: "clearance cookie",
			route: themetest.Route{
				Status: http.StatusForbidden,
				Body:   "<html><head><title>Error</title></head><body></body></html>",
				Header: http.Header{"Set-Cookie": []string{"cf_clearance=abc; Path=/; HttpOnly"}},
			},
		},
		{
			// The generic gate: 200, tiny, nothing recognised, noscript.
			name:  "generic js gate",
			route: themetest.Route{File: "home-jsgate.html"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ui := &recordUI{answers: []string{"continue", "continue"}}
			res := run(t, map[string]themetest.Route{"GET /": tc.route}, ui)

			if res.Verdict != theme.VerdictBlockedChallenge {
				t.Fatalf("verdict = %q (%s), want blocked_challenge", res.Verdict, res.Detail)
			}
			if res.Addable || res.Draft != nil {
				t.Fatal("a challenge-protected site must never be addable, degraded or otherwise")
			}
			// PLAN §6 M3: a complete, final answer — not a retry prompt.
			low := strings.ToLower(res.Detail)
			if !strings.Contains(low, "browser challenge") {
				t.Errorf("detail %q does not say plainly that a browser challenge is in the way", res.Detail)
			}
			for _, forbidden := range []string{"try again", "retry", "user agent", "user-agent", "workaround", "proxy"} {
				if strings.Contains(low, forbidden) {
					t.Errorf("detail %q suggests %q; PLAN §7.6 forbids offering a way around a challenge", res.Detail, forbidden)
				}
			}
			if len(ui.questions) != 0 {
				t.Errorf("a refused site must not ask the user anything: %+v", ui.questions)
			}
		})
	}
}

// PLAN §7.5 stage 3 keeps a login wall separate from a challenge: it is a
// warning, and the user decides.
func TestLoginWallWarnsRatherThanBlocks(t *testing.T) {
	home, err := os.ReadFile("testdata/home-madara.html")
	if err != nil {
		t.Fatal(err)
	}
	// The same recognisable site, with a sign-in form bolted on.
	gated := strings.Replace(string(home), "<footer",
		`<form class="login" action="/login/"><input type="text" name="user"><input type="password" name="pass"></form><footer`, 1)

	routes := madaraRoutes()
	routes["GET /"] = themetest.Route{Body: gated}
	res := run(t, routes, &recordUI{})

	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s); a login wall is a warning, not a refusal", res.Verdict, res.Detail)
	}
	if !res.Addable {
		t.Error("a site with a login wall must still be addable; the user decides")
	}
}

// PLAN §7.5 stage 5: page extraction failing means the source is useless, so it
// is refused even though everything else worked.
func TestVerdictPartialRefusedWhenPagesFail(t *testing.T) {
	routes := madaraRoutes()
	routes["GET /manga/the-lantern-keeper/chapter-4/"] = themetest.Route{
		Body: "<html><body><div class=\"reading-content\"></div></body></html>",
	}
	res := run(t, routes, &recordUI{})

	if res.Verdict != theme.VerdictPartial {
		t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
	}
	if res.Addable || res.Draft != nil {
		t.Fatal("a source Quire cannot read page images from must be refused, not added degraded")
	}
	if !strings.Contains(res.Detail, "page images") {
		t.Errorf("detail %q does not name the failing step", res.Detail)
	}
}

// Part B (2026-09-20) adds a multi-candidate retry for file-based themes, and
// this pins that a page-based (manga) theme's behaviour is unchanged by it: a
// site whose first search result has no chapters is refused exactly as
// before, even when a later result would have had some. Trying more than one
// book is right for a service whose per-item availability depends on the
// user's own configuration; a manga site's first search hit is representative
// of the whole catalogue, and trying a second one there would only be masking
// the site failing on the item it was actually asked about.
func TestStageFivePageBasedThemeDoesNotRetryASecondSearchResult(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	var chaptersCalls []string
	th := stubTheme{
		id:    "alpha",
		score: 90,
		stubs: []theme.SeriesStub{
			{ID: "/series/empty/", Title: "Empty"},
			{ID: "/series/one/", Title: "One"},
		},
		chaptersByID: map[string][]theme.Chapter{
			"/series/empty/": nil,
			"/series/one/":   {{ID: "/series/one/1/", Title: "Chapter 1", Number: 1}},
		},
		chaptersCalls: &chaptersCalls,
	}
	res := runWithFetcher(t, f, th)

	if res.Verdict != theme.VerdictPartial {
		t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
	}
	if res.Addable || res.Draft != nil {
		t.Fatal("a page-based theme whose first result had no chapters was accepted instead of refused")
	}
	if !strings.Contains(res.Detail, "couldn't list any chapters") {
		t.Errorf("detail %q does not name the failing step", res.Detail)
	}
	// The tell: it must never have asked about the second result at all.
	if got := chaptersCalls; len(got) != 1 || got[0] != "/series/empty/" {
		t.Errorf("Chapters was asked about %v, want exactly the first result and no more", got)
	}
}

// ...and the narrow case where a degraded add *is* offered: search, chapters
// and pages all work, only the series detail does not.
func TestVerdictPartialDegradedAddAllowed(t *testing.T) {
	routes := madaraRoutes()
	routes["GET /manga/the-lantern-keeper/"] = themetest.Route{
		Body: "<html><body><div class=\"site-content\"></div></body></html>",
	}
	res := run(t, routes, &recordUI{})

	if res.Verdict != theme.VerdictPartial {
		t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
	}
	if !res.Addable || res.Draft == nil {
		t.Fatal("search, chapters and pages all worked; PLAN §7.5 allows a degraded add here")
	}
	if res.Draft.LastProbe.Verdict != theme.VerdictPartial {
		t.Errorf("stored verdict = %q, want partial", res.Draft.LastProbe.Verdict)
	}
}

// PLAN §7.4: robots said no. The source is not added.
func TestVerdictRobotsDenied(t *testing.T) {
	res := run(t, map[string]themetest.Route{
		"GET /": {Err: errors.New("fetch: /: " + fetch.ErrRobotsDenied.Error())},
	}, &recordUI{}, func(o *prober.Options) {
		o.Fetcher = errFetcher{err: fetch.ErrRobotsDenied}
	})

	if res.Verdict != theme.VerdictRobotsDenied {
		t.Fatalf("verdict = %q (%s), want robots_denied", res.Verdict, res.Detail)
	}
	if !strings.Contains(strings.ToLower(res.Detail), "robots.txt") {
		t.Errorf("detail %q does not say what said no", res.Detail)
	}
}

// PLAN §7.4, decided 2026-09-15: an unreadable robots.txt is *unknown*, not
// permission — and reporting it as a denial would be a refusal that never
// happened. It is `unreachable`.
func TestUnreadableRobotsIsUnreachableNotDenied(t *testing.T) {
	res := run(t, nil, &recordUI{}, func(o *prober.Options) {
		o.Fetcher = errFetcher{err: fetch.ErrRobotsUnavailable}
	})

	if res.Verdict != theme.VerdictUnreachable {
		t.Fatalf("verdict = %q (%s), want unreachable", res.Verdict, res.Detail)
	}
	if strings.Contains(res.Detail, "asks automated tools not to") {
		t.Errorf("detail %q reports a denial that never happened", res.Detail)
	}
}

func TestVerdictUnreachable(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		res := run(t, nil, &recordUI{}, func(o *prober.Options) {
			o.Fetcher = errFetcher{err: errors.New("dial tcp: no route to host")}
		})
		if res.Verdict != theme.VerdictUnreachable {
			t.Fatalf("verdict = %q (%s), want unreachable", res.Verdict, res.Detail)
		}
	})

	t.Run("http error status", func(t *testing.T) {
		res := run(t, map[string]themetest.Route{
			"GET /": {Status: http.StatusNotFound, Body: "<html><head><title>Not found</title></head><body><p>Nothing here. Try the archive index, the sitemap, or the search box at the top of every page on this site.</p></body></html>"},
		}, &recordUI{})
		if res.Verdict != theme.VerdictUnreachable {
			t.Fatalf("verdict = %q (%s), want unreachable", res.Verdict, res.Detail)
		}
		if !strings.Contains(res.Detail, "404") {
			t.Errorf("detail %q does not say what the site answered", res.Detail)
		}
	})
}

func TestVerdictInvalidURL(t *testing.T) {
	cases := []struct{ name, url string }{
		{"empty", "   "},
		{"not http", "ftp://example.invalid/pub"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, nil)
			p := prober.New(prober.Options{
				Fetcher:  f,
				Registry: registry(f),
				Guard:    refuseGuard{kind: fetch.ErrInvalidURL},
				Now:      clock,
			})
			res, err := p.Run(context.Background(), tc.url, &recordUI{})
			if err != nil {
				t.Fatal(err)
			}
			if res.Verdict != theme.VerdictInvalidURL {
				t.Fatalf("verdict = %q (%s), want invalid_url", res.Verdict, res.Detail)
			}
			if res.Addable {
				t.Error("an invalid URL must not be addable")
			}
		})
	}
}

func TestVerdictBlockedAddress(t *testing.T) {
	f := themetest.New(t, nil)
	p := prober.New(prober.Options{
		Fetcher:  f,
		Registry: registry(f),
		Guard:    refuseGuard{kind: fetch.ErrBlockedAddress},
		Now:      clock,
	})
	res, err := p.Run(context.Background(), "https://example.invalid", &recordUI{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != theme.VerdictBlockedAddress {
		t.Fatalf("verdict = %q (%s), want blocked_address", res.Verdict, res.Detail)
	}
	if !strings.Contains(res.Detail, "test refusal") {
		t.Errorf("detail %q does not carry the guard's reason", res.Detail)
	}
}

// PLAN §7.5 stage 1 normalises before it judges: a pasted address with no
// scheme is not a mistake.
func TestBareHostGetsHTTPS(t *testing.T) {
	f := themetest.New(t, madaraRoutes())
	p := prober.New(prober.Options{
		Fetcher: f, Registry: registry(f), Guard: allowGuard{}, Now: clock,
		NewID: func() string { return "src-test" },
	})
	res, err := p.Run(context.Background(), " example.invalid ", &recordUI{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
	}
	if res.Draft.BaseURL != "https://example.invalid" {
		t.Errorf("baseUrl = %q, want https://example.invalid", res.Draft.BaseURL)
	}
}

// PLAN §7.5 stage 2: "if it differs from what the user typed, say so and ask
// before continuing."
func TestRedirectOffDomainAsksFirst(t *testing.T) {
	routes := madaraRoutes()
	routes["GET /"] = themetest.Route{File: "home-madara.html", FinalURL: "https://elsewhere.invalid/"}

	t.Run("cancelled", func(t *testing.T) {
		ui := &recordUI{answers: []string{"cancel"}}
		res := run(t, routes, ui)

		if len(ui.questions) != 1 || ui.questions[0].Kind != "redirect" {
			t.Fatalf("expected one redirect question, got %+v", ui.questions)
		}
		if !strings.Contains(ui.questions[0].Text, "elsewhere.invalid") {
			t.Errorf("question %q does not name where the site sent us", ui.questions[0].Text)
		}
		if !res.Cancelled {
			t.Error("answering no must stop the probe")
		}
		if res.Verdict != "" {
			t.Errorf("verdict = %q; stopping is not a verdict about the site", res.Verdict)
		}
		if res.Addable {
			t.Error("nothing may be added after the user said no")
		}
	})

	t.Run("continued", func(t *testing.T) {
		// Answering yes re-points the source at where it actually landed, so
		// the stage 5 requests are made against that host. The fixture's
		// absolute links still name the *old* domain, so the theme skips them
		// and follows the relative one — which is what a site mid-move looks
		// like, and is why the series routes below are the relative entry.
		routes := map[string]themetest.Route{
			"GET /":                                      {File: "home-madara.html", FinalURL: "https://elsewhere.invalid/"},
			"GET /?post_type=wp-manga&s=":                {File: "search.html"},
			"GET /manga/salt-and-cedar/":                 {File: "series.html"},
			"POST /manga/salt-and-cedar/ajax/chapters/":  {File: "chapters-ajax.html"},
			"GET /manga/the-lantern-keeper/chapter-3-5/": {File: "reader.html"},
			"GET /pages/lantern-keeper/4/001.jpg":        imageRoute(),
		}
		ui := &recordUI{answers: []string{"continue"}}
		res := run(t, routes, ui)

		if res.Verdict != theme.VerdictOK {
			t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
		}
		if res.Draft.BaseURL != "https://elsewhere.invalid" {
			t.Errorf("baseUrl = %q, want the domain the user agreed to", res.Draft.BaseURL)
		}
	})
}

// errFetcher fails every request with the same error, for the verdicts whose
// cause is a transport-layer refusal rather than a page.
type errFetcher struct{ err error }

func (f errFetcher) Get(context.Context, *fetch.Policy, string) (*fetch.Response, error) {
	return nil, f.err
}

func (f errFetcher) GetFrom(context.Context, *fetch.Policy, string, fetch.Referrer) (*fetch.Response, error) {
	return nil, f.err
}

func (f errFetcher) GetRetrieval(context.Context, *fetch.Policy, string) (*fetch.Response, error) {
	return nil, f.err
}

func (f errFetcher) GetRetrievalFrom(context.Context, *fetch.Policy, string, fetch.Referrer) (*fetch.Response, error) {
	return nil, f.err
}

func (f errFetcher) PostForm(context.Context, *fetch.Policy, string, url.Values) (*fetch.Response, error) {
	return nil, f.err
}

// runWithTheme is run() with a specific theme registered instead of the
// production fan-out, for the stage-5 image tests: they need a theme whose
// search, series and chapters are canned so that the only thing under test is
// what happens to the page image.
func runWithTheme(t *testing.T, routes map[string]themetest.Route, th theme.Theme) prober.Result {
	t.Helper()
	return runWithFetcher(t, themetest.New(t, routes), th)
}

func runWithFetcher(t *testing.T, f *themetest.Fetcher, th theme.Theme) prober.Result {
	t.Helper()
	reg := theme.NewRegistry()
	reg.MustRegister(th)
	res, err := prober.New(prober.Options{
		Fetcher: f, Registry: reg, Guard: allowGuard{}, Now: clock,
		NewID: func() string { return "src-test" },
	}).Run(context.Background(), "https://example.invalid", &recordUI{answers: []string{"continue", "continue"}})
	if err != nil {
		t.Fatalf("probe returned an error: %v", err)
	}
	return res
}
