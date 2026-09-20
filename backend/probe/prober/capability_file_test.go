package prober_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/shelfmark"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.5 stage 5 against a theme.FileTheme — a source whose "chapters" are
// finished files (2026-09-20).
//
// Two things are being pinned here and they pull in opposite directions, which
// is why both are tested:
//
//   - A file theme must not be refused for want of page images. It has none by
//     design; Pages() returns an error rather than an empty slice. Reporting
//     "it couldn't find the page images in a chapter" for a source that works
//     is a false *refusal*.
//   - A file theme must not be accepted on the fingerprint alone. A self-hosted
//     application serves a near-empty shell, so the markup is weak evidence;
//     the strong evidence is an API response only that application produces.
//     Accepting without it is the false `ok` the comment at the top of
//     capability.go is most emphatic about.

// shelfmarkFixture reads one of the shelfmark package's captures.
//
// They are read from where they live rather than copied here on purpose: they
// are one real instance's responses from 2026-09-20, and a second copy in this
// package would be a second thing to keep true.
func shelfmarkFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "theme", "shelfmark", "testdata", name))
	if err != nil {
		t.Fatalf("read shelfmark fixture %s: %v", name, err)
	}
	return string(b)
}

// shelfmarkRoutes is a working instance: the web shell the probe lands on, the
// config that identifies it, and the three API calls stage 5 makes.
func shelfmarkRoutes(t *testing.T) map[string]themetest.Route {
	t.Helper()
	return map[string]themetest.Route{
		"GET /":                    {Body: shelfmarkFixture(t, "index.html")},
		"GET /api/config":          {Body: shelfmarkFixture(t, "config.json")},
		"GET /api/metadata/search": {Body: shelfmarkFixture(t, "metadata-search-dune.json")},
		"GET /api/metadata/book/openlibrary/OL893414W": {Body: shelfmarkFixture(t, "metadata-book-dune.json")},
		"GET /api/releases":                            {Body: shelfmarkFixture(t, "releases-dune.json")},
	}
}

// The happy path: a real instance is recognised, verified and accepted —
// without a single page image, because there are none to have.
func TestStageFiveAcceptsAShelfmarkInstance(t *testing.T) {
	f := themetest.New(t, shelfmarkRoutes(t))
	res := runWithFetcher(t, f, shelfmark.New(f))

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
	if res.Draft == nil || res.Draft.Theme != shelfmark.ID {
		t.Fatalf("draft = %+v, want one bound to the shelfmark theme", res.Draft)
	}
	// The strong check must actually have been made. Without it this verdict
	// rests on a <head> anyone can write.
	if !f.Requested("GET", shelfmark.ConfigPath) {
		t.Error("stage 5 accepted the source without ever asking for /api/config")
	}
	// And the behavioural half: it searched, and it listed something to fetch.
	if !f.Requested("GET", "/api/metadata/search") {
		t.Error("stage 5 never searched the instance")
	}
	if !f.Requested("GET", "/api/releases") {
		t.Error("stage 5 never asked what could be downloaded for a book")
	}
	// The one sentence the user reads. It must not mention pages: this source
	// has none, and saying it found some would be a claim about a check that
	// never ran.
	if strings.Contains(strings.ToLower(res.Detail), "page") {
		t.Errorf("detail %q talks about pages for a source that has none", res.Detail)
	}
}

// The false negative reported against a real instance (2026-09-20), pinned
// end to end. The device log for that instance showed the empty-query listing
// answering HTTP 400 — Shelfmark's search requires a query, correctly — and
// the package's old fallback query, "one", coming back with zero results from
// that instance's metadata provider. The probe then had nothing to search
// with at all. shelfmark.Theme.ProbeQuery fixes this by naming a query its own
// backend actually answers; this test reproduces the exact request shape and
// checks the source is accepted rather than refused.
func TestStageFiveWorksAroundAListingThatRequiresAQuery(t *testing.T) {
	routes := shelfmarkRoutes(t)
	delete(routes, "GET /api/metadata/search")
	routes["GET /api/metadata/search?page=1&query="] = themetest.Route{
		Status: 400,
		Body:   `{"error":"Either 'query' or search field values are required"}`,
	}
	routes["GET /api/metadata/search?page=1&query=one"] = themetest.Route{
		Body: `{"books":[],"has_more":false,"page":1,"total_found":0}`,
	}
	routes["GET /api/metadata/search?page=1&query=dune"] = themetest.Route{
		Body: shelfmarkFixture(t, "metadata-search-dune.json"),
	}

	f := themetest.New(t, routes)
	res := runWithFetcher(t, f, shelfmark.New(f))

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
	if !f.Requested("GET", "/api/metadata/search") {
		t.Error("stage 5 never searched the instance")
	}
}

// The rejection that matters. The markup says Shelfmark — that is stage 4's
// whole evidence, and it is evidence anyone can manufacture — but /api/config
// does not carry the key set, so this is some other server wearing the name.
func TestStageFiveRejectsAnInstanceWhoseConfigIsWrong(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			// The instance's own answer to an unknown path, which arrives with
			// **HTTP 200**. A check that trusted the status would confirm any
			// server at all.
			name: "an unknown path answered 200",
			body: shelfmarkFixture(t, "not-found.json"),
		},
		{
			// A config object that is a config object, and is not this
			// application's: three of the four keys, which is what makes the
			// co-occurrence rule worth having.
			name: "three of the four keys",
			body: `{"supported_formats":["epub"],"release_search_timeout":300,` +
				`"metadata_search_fields":["title"]}`,
		},
		{
			name: "not JSON at all",
			body: `<!doctype html><html><body>Shelfmark</body></html>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := shelfmarkRoutes(t)
			routes["GET /api/config"] = themetest.Route{Body: tc.body}
			f := themetest.New(t, routes)
			res := runWithFetcher(t, f, shelfmark.New(f))

			if res.Addable || res.Draft != nil {
				t.Fatalf("a server that is not a Shelfmark instance was offered for adding: %q (%s)",
					res.Verdict, res.Detail)
			}
			if res.Verdict != theme.VerdictPartial {
				t.Errorf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
			}
			// The failing step has to be named, or the user is told a site they
			// can see working was refused for no stated reason.
			if !strings.Contains(res.Detail, "isn't the application Quire took it for") {
				t.Errorf("detail %q does not say the strong check is what failed", res.Detail)
			}
			// And nothing else was asked of it: a server that is not the
			// application cannot be searched as one, and a second failure on
			// top of the real one only obscures it.
			if f.Requested("GET", "/api/metadata/search") {
				t.Error("stage 5 went on searching a server that had already failed the strong check")
			}
		})
	}
}

// A release the theme could not address is refused outright: it is in the
// list and there is no way to ask for it, which is a real theme bug rather
// than routine emptiness, and accepting it would be the false `ok` — the
// user adds the source and every download fails.
//
// This is deliberately kept apart from "the book has no releases at all",
// which moved to TestStageFiveAddsAFileThemeInDegradedStateWhenNoBookHasReleases
// below (2026-09-20, Part B): the two ways a book can yield nothing to fetch
// are different failures, and only one of them is the site's fault.
func TestStageFiveRefusesAFileThemeWhoseReleaseHasNoAddress(t *testing.T) {
	chapters := []theme.Chapter{{ID: "", Title: "EPUB · 1.7MB · Direct Download", Number: -1}}
	res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
		fileStub{id: "books", score: 90, chapters: chapters})

	if res.Addable || res.Draft != nil {
		t.Fatalf("a source offering nothing to download was offered for adding: %s", res.Detail)
	}
	if !strings.Contains(res.Detail, "nothing Quire could actually ask it to fetch") {
		t.Errorf("detail %q does not name the failing step", res.Detail)
	}
	if strings.Contains(strings.ToLower(res.Detail), "page") {
		t.Errorf("detail %q refuses a book source for want of page images", res.Detail)
	}
	if !strings.Contains(res.Detail, "download a book") {
		t.Errorf("detail %q does not say what the source could not do", res.Detail)
	}
}

// The false negative this file exists to fix (2026-09-20): a book service's
// first search hit having no releases is routine, not a broken source. Stage
// 5 must try more than one book before giving up, and — once it has tried up
// to three and every one came back genuinely empty — offer to add the source
// anyway, because the strong check already proved this is the real
// application and release availability is a property of the books and of the
// user's own configured indexers, not of the connection to the site.
func TestStageFiveAddsAFileThemeInDegradedStateWhenNoBookHasReleases(t *testing.T) {
	stubs := []theme.SeriesStub{
		{ID: "/book/one", Title: "One"},
		{ID: "/book/two", Title: "Two"},
		{ID: "/book/three", Title: "Three"},
	}
	res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
		confirmingFileStub{fileStub: fileStub{id: "books", score: 90, stubs: stubs, chaptersByID: map[string][]theme.Chapter{
			"/book/one":   nil,
			"/book/two":   nil,
			"/book/three": nil,
		}}})

	if !res.Addable || res.Draft == nil {
		t.Fatalf("a confirmed instance with three empty books was refused instead of offered in degraded state: %s", res.Detail)
	}
	if res.Verdict != theme.VerdictPartial {
		t.Errorf("verdict = %q, want partial", res.Verdict)
	}
	if !strings.Contains(res.Detail, "epub or pdf") {
		t.Errorf("detail %q does not say what was not found", res.Detail)
	}
	if !strings.Contains(strings.ToLower(res.Detail), "sources") {
		t.Errorf("detail %q does not say this depends on the instance's configured sources", res.Detail)
	}
}

// The false negative one stage earlier (2026-09-20): a confirmed instance
// whose metadata provider is having its own empty window must not be refused
// outright. Measured against a live Shelfmark instance: openlibrary
// intermittently answers every query with zero results, HTTP 200, for a
// window and then recovers.
func TestStageFiveAddsAConfirmedInstanceThatFoundNothingToSearch(t *testing.T) {
	res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
		confirmingFileStub{fileStub: fileStub{id: "books", score: 90, stubs: []theme.SeriesStub{}}})

	if !res.Addable || res.Draft == nil {
		t.Fatalf("a confirmed instance whose search found nothing was refused instead of offered: %s", res.Detail)
	}
	if res.Verdict != theme.VerdictPartial {
		t.Errorf("verdict = %q, want partial", res.Verdict)
	}
	if !strings.Contains(res.Detail, "no results") {
		t.Errorf("detail %q does not say the search came back empty", res.Detail)
	}
	if !strings.Contains(strings.ToLower(res.Detail), "upstream") {
		t.Errorf("detail %q does not say this can be the source's own upstream being temporarily empty", res.Detail)
	}
}

// A theme with no strong check at all must still be refused when its search
// finds nothing: there is no positive evidence to weigh against an empty
// result, unlike the confirmed case above. This is the same distinction
// addableEmptyReleases draws, one stage earlier.
func TestStageFiveRefusesAnUnconfirmedThemeThatFoundNothingToSearch(t *testing.T) {
	res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
		fileStub{id: "books", score: 90, stubs: []theme.SeriesStub{}})

	if res.Addable || res.Draft != nil {
		t.Fatalf("an unconfirmed theme whose search found nothing was offered for adding: %s", res.Detail)
	}
}

// The retry that fixes the bug directly: the first two books tried have
// nothing, but the third does, and stage 5 must not give up before trying it.
// A probe that condemned the source on the first arbitrary book is exactly
// the false refusal reported against a real Shelfmark instance.
func TestStageFiveTriesMoreThanOneBookBeforeGivingUp(t *testing.T) {
	stubs := []theme.SeriesStub{
		{ID: "/book/one", Title: "One"},
		{ID: "/book/two", Title: "Two"},
	}
	res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
		fileStub{id: "books", score: 90, stubs: stubs, chaptersByID: map[string][]theme.Chapter{
			"/book/one": nil,
			"/book/two": {{ID: "release-1", Title: "EPUB · 1.7MB · Direct Download", Number: 1}},
		}})

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok once the second book had a release", res.Verdict, res.Detail)
	}
}

// A genuine fetch failure along the way stays a refusal, even when every
// candidate ends up empty overall: "the request failed" and "the book really
// has nothing" are different claims, and only the second is what Part B
// exists to stop condemning a source for.
func TestStageFiveRefusesWhenABookFetchGenuinelyFails(t *testing.T) {
	stubs := []theme.SeriesStub{
		{ID: "/book/one", Title: "One"},
		{ID: "/book/two", Title: "Two"},
	}
	res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
		fileStub{id: "books", score: 90, stubs: stubs,
			chaptersByID: map[string][]theme.Chapter{"/book/one": nil},
			chaptersErrByID: map[string]error{
				"/book/two": errors.New("books: /api/releases: HTTP 500"),
			},
		})

	if res.Addable || res.Draft != nil {
		t.Fatalf("a source where a book fetch genuinely failed was offered in degraded state: %s", res.Detail)
	}
	if !strings.Contains(res.Detail, "couldn't list anything to download") {
		t.Errorf("detail %q does not name the failing step", res.Detail)
	}
}

// The strong check stays non-degradable even here: a source whose strong
// check failed is refused before any book is even tried, so an empty release
// list downstream of that never gets a chance to matter.
func TestStageFiveRefusesEvenWithEmptyReleasesWhenStrongCheckFails(t *testing.T) {
	res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
		confirmingFileStub{
			fileStub:   fileStub{id: "books", score: 90, chapters: nil},
			confirmErr: errors.New("books: not the real application"),
		})

	if res.Addable || res.Draft != nil {
		t.Fatalf("a source that failed its strong check was offered for adding: %s", res.Detail)
	}
	if !strings.Contains(res.Detail, "isn't the application Quire took it for") {
		t.Errorf("detail %q does not say the strong check is what failed", res.Detail)
	}
}

// A theme with no strong check is unaffected: eight of the nine themes drive
// families of independently hosted sites with no endpoint that could identify
// them, and refusing them for not answering a question nobody asked would
// refuse every source Quire can already read.
func TestAThemeWithNoStrongCheckIsStillAccepted(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	res := runWithFetcher(t, f, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
}

// fileStub is a theme.FileTheme with nothing to offer: a book, and no releases
// for it. It stands in for an instance whose sources were all unreachable.
type fileStub struct {
	id    string
	score int

	// chapters, when set and stubs is nil, are the one default book's
	// releases. Kept as its own field so single-book tests do not have to
	// build a map for one entry.
	chapters []theme.Chapter

	// stubs overrides the default single search result, for tests exercising
	// stage 5's multi-candidate retry. Nil means the default one-book result.
	stubs []theme.SeriesStub

	// chaptersByID overrides Chapters per stub ID when stubs is set. Nil
	// falls back to chapters for every ID, which keeps single-book tests
	// unchanged.
	chaptersByID map[string][]theme.Chapter

	// chaptersErrByID makes Chapters fail outright for a given stub ID,
	// instead of merely coming back empty — for the case that must stay a
	// refusal: a real fetch failure is not "this book has no releases".
	chaptersErrByID map[string]error
}

func (f fileStub) ID() string                             { return f.id }
func (f fileStub) Fingerprint(*probe.Page) int            { return f.score }
func (f fileStub) AllowedHosts() []string                 { return nil }
func (f fileStub) SuggestedName() string                  { return "" }
func (f fileStub) ValidateOverrides(map[string]any) error { return nil }
func (f fileStub) OverrideKeys() []theme.OverrideDoc      { return nil }

func (f fileStub) Search(context.Context, *theme.Source, string, int) ([]theme.SeriesStub, error) {
	if f.stubs != nil {
		return f.stubs, nil
	}
	return []theme.SeriesStub{{ID: "/book/one", Title: "One"}}, nil
}

func (f fileStub) Series(_ context.Context, _ *theme.Source, id string) (*theme.Series, error) {
	return &theme.Series{ID: id, Title: id}, nil
}

func (f fileStub) Chapters(_ context.Context, _ *theme.Source, id string) ([]theme.Chapter, error) {
	if f.chaptersErrByID != nil {
		if err, ok := f.chaptersErrByID[id]; ok {
			return nil, err
		}
	}
	if f.chaptersByID != nil {
		return f.chaptersByID[id], nil
	}
	return f.chapters, nil
}

// Pages errors rather than returning nothing, exactly as a real file theme
// does: an empty slice would read as "this chapter has no pages", which is a
// different and false claim.
func (f fileStub) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return nil, errors.New("books: this theme has no page images")
}

func (f fileStub) Retrieve(context.Context, *theme.Source, string, func(string)) (string, string, error) {
	return "", "", errors.New("books: nothing to retrieve")
}

// confirmingFileStub adds a theme.Confirmer to fileStub, for the one test
// where the strong check itself — not the release list — is what must decide
// the outcome.
type confirmingFileStub struct {
	fileStub
	confirmErr error
}

func (f confirmingFileStub) Confirm(context.Context, *theme.Source) error { return f.confirmErr }

// The budget that stops a file-based probe from stacking up three 6-minute
// timeouts is enforced: the first candidate is always tried, but after a
// pathological delay (measured from the loop start), further candidates are
// not attempted.
func TestStageFiveStopsAfterBudgetWhenMultipleBooksAreEmpty(t *testing.T) {
	stubs := []theme.SeriesStub{
		{ID: "/book/one", Title: "One"},
		{ID: "/book/two", Title: "Two"},
		{ID: "/book/three", Title: "Three"},
	}
	stubWithTracking := &trackingFileStub{
		fileStub: fileStub{id: "books", score: 90, stubs: stubs, chaptersByID: map[string][]theme.Chapter{
			"/book/one":   nil,
			"/book/two":   nil,
			"/book/three": nil,
		}},
	}

	// A fake clock that switches behavior when stageCapability's file-based
	// candidate loop starts. We detect this by observing when the stub's
	// Chapters method is called for the first time, then transition the clock
	// to return advanced time (exceeding the budget) on subsequent calls.
	baseTime := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	loopStarted := false
	fakeClock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		if loopStarted {
			// We're in the candidate loop. Return advanced time to trigger budget.
			return baseTime.Add(100 * time.Second)
		}
		return baseTime
	}

	// Wrap the stub to detect when the loop starts.
	wrappedStub := &loopDetectingFileStub{
		delegate: stubWithTracking,
		onFirstChapters: func() {
			mu.Lock()
			loopStarted = true
			mu.Unlock()
		},
	}

	// Use custom probe creation to inject the fake clock and custom theme.
	f := themetest.New(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}})
	reg := theme.NewRegistry()
	reg.MustRegister(wrappedStub)
	res, err := prober.New(prober.Options{
		Fetcher:  f,
		Registry: reg,
		Guard:    &testAllowGuard{},
		Now:      fakeClock,
		NewID:    func() string { return "src-test" },
	}).Run(context.Background(), "https://example.invalid", &testRecordUI{})
	if err != nil {
		t.Fatalf("probe returned an error: %v", err)
	}

	// The first candidate should have been tried (Chapters called for /book/one).
	if !stubWithTracking.chapersCalled["/book/one"] {
		t.Error("Chapters was not called for the first candidate (/book/one)")
	}

	// The third candidate should NOT have been tried (Chapters not called for /book/three)
	// due to budget exhaustion.
	if stubWithTracking.chapersCalled["/book/three"] {
		t.Error("Chapters was called for the third candidate (/book/three), but budget should have stopped further attempts")
	}

	// The source should be offered in degraded state since all attempts were
	// empty (fileEmpty=true) and no errors occurred.
	if !res.Addable || res.Verdict != theme.VerdictPartial {
		t.Errorf("verdict = %q, addable = %v; expected partial and addable due to empty books", res.Verdict, res.Addable)
	}
}

// trackingFileStub wraps fileStub to track which book IDs had Chapters called.
type trackingFileStub struct {
	fileStub
	chapersCalled map[string]bool
}

// Confirm makes trackingFileStub a theme.Confirmer that always passes: this
// test is about the candidate-loop budget, not about the strong check, and it
// needs a genuine strongConfirmed=true for addableEmptyReleases to be
// reachable at all (2026-09-20 precision fix — see confirmTheme).
func (f *trackingFileStub) Confirm(context.Context, *theme.Source) error { return nil }

func (f *trackingFileStub) Chapters(ctx context.Context, src *theme.Source, id string) ([]theme.Chapter, error) {
	if f.chapersCalled == nil {
		f.chapersCalled = make(map[string]bool)
	}
	f.chapersCalled[id] = true
	return f.fileStub.Chapters(ctx, src, id)
}

// testAllowGuard permits all addresses (for testing only).
type testAllowGuard struct{}

func (testAllowGuard) CheckURL(context.Context, *url.URL, *fetch.Policy) error { return nil }

// testRecordUI collects progress without blocking on questions.
type testRecordUI struct{}

func (testRecordUI) Progress(prober.Progress) {}

func (testRecordUI) Ask(context.Context, prober.Question) (prober.Answer, error) {
	return prober.Answer{ID: "continue"}, nil
}

// loopDetectingFileStub wraps a file stub and detects when the candidate loop
// in stageCapability begins (by observing the first Chapters call).
type loopDetectingFileStub struct {
	delegate        *trackingFileStub
	onFirstChapters func()
	once            sync.Once
}

func (l *loopDetectingFileStub) ID() string { return l.delegate.ID() }
func (l *loopDetectingFileStub) Fingerprint(p *probe.Page) int {
	return l.delegate.Fingerprint(p)
}
func (l *loopDetectingFileStub) AllowedHosts() []string { return l.delegate.AllowedHosts() }
func (l *loopDetectingFileStub) SuggestedName() string  { return l.delegate.SuggestedName() }
func (l *loopDetectingFileStub) ValidateOverrides(m map[string]interface{}) error {
	return l.delegate.ValidateOverrides(m)
}
func (l *loopDetectingFileStub) OverrideKeys() []theme.OverrideDoc {
	return l.delegate.OverrideKeys()
}
func (l *loopDetectingFileStub) Search(ctx context.Context, src *theme.Source, q string, n int) ([]theme.SeriesStub, error) {
	return l.delegate.Search(ctx, src, q, n)
}
func (l *loopDetectingFileStub) Series(ctx context.Context, src *theme.Source, id string) (*theme.Series, error) {
	return l.delegate.Series(ctx, src, id)
}
func (l *loopDetectingFileStub) Chapters(ctx context.Context, src *theme.Source, id string) ([]theme.Chapter, error) {
	l.once.Do(l.onFirstChapters)
	return l.delegate.Chapters(ctx, src, id)
}
func (l *loopDetectingFileStub) Pages(ctx context.Context, src *theme.Source, id string) ([]string, error) {
	return l.delegate.Pages(ctx, src, id)
}
func (l *loopDetectingFileStub) Retrieve(ctx context.Context, src *theme.Source, id string, f func(string)) (string, string, error) {
	return l.delegate.Retrieve(ctx, src, id, f)
}
func (l *loopDetectingFileStub) Confirm(ctx context.Context, src *theme.Source) error {
	return l.delegate.Confirm(ctx, src)
}
