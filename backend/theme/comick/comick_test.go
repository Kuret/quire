package comick_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/comick"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test here runs offline: the fixtures are synthetic responses on
// example.invalid and DNS is disabled for the package.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func clock() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-09", Name: "Example Comics", Lang: "en",
		Theme: comick.ID, BaseURL: "https://example.invalid",
		AddedAt: clock(),
	}
}

func jsonRoute(file string) themetest.Route {
	return themetest.Route{File: file, Header: map[string][]string{"Content-Type": {"application/json"}}}
}

const (
	seriesID  = "/comic/the-lantern-keeper"
	chapterID = "/comic/the-lantern-keeper/RH6nHLl-chapter-7-en"
)

func TestSearchReadsTheAPIEnvelope(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search?q=lantern&type=comic": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "  lantern  ", 1)
	if err != nil {
		t.Fatal(err)
	}
	// Default maxContentRating is "suggestive", so the erotica entry is out,
	// and the entry with no slug cannot be addressed.
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(got), got)
	}
	want := theme.SeriesStub{
		ID:       seriesID,
		Title:    "The Lantern Keeper",
		CoverURL: "https://pages.example.invalid/the-lantern-keeper/covers/8edfff1d.jpg",
		// The listing this cover was read from, which is the URL Search
		// fetched. PLAN §7.6 — see TestCoverReferrerIsThePageTheCoverCameFrom.
		CoverReferrer: "https://example.invalid/api/search?q=lantern&type=comic",
	}
	if got[0] != want {
		t.Errorf("result 0 = %+v, want %+v", got[0], want)
	}
	for _, s := range got {
		if s.ID == "/comic" || s.ID == "/comic/" {
			t.Errorf("the entry with no slug produced an ID %q that resolves to the listing root", s.ID)
		}
	}
}

// The rating filter is applied to the response rather than asked for, because
// the endpoint's own parameter semantics are undocumented and guessing wrong
// drops results silently.
func TestContentRatingFiltersTheResponseAndIsNotSentAsAParameter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		max    string
		titles []string
	}{
		{name: "default suggestive", max: "", titles: []string{"The Lantern Keeper", "Salt and Cinder", "The Harbour Road"}},
		{name: "safe only", max: "safe", titles: []string{"The Lantern Keeper", "The Harbour Road"}},
		{name: "everything", max: "pornographic", titles: []string{"The Lantern Keeper", "Salt and Cinder", "Paper Boats", "The Harbour Road"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /api/search": jsonRoute("search.json"),
			})
			th := comick.NewWithClock(f, clock)
			src := site()
			if tc.max != "" {
				src.Overrides = map[string]any{"maxContentRating": tc.max}
			}

			got, err := th.Search(context.Background(), src, "lantern", 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.titles) {
				t.Fatalf("got %d results, want %d: %+v", len(got), len(tc.titles), got)
			}
			for i, want := range tc.titles {
				if got[i].Title != want {
					t.Errorf("result %d = %q, want %q", i, got[i].Title, want)
				}
			}

			u, err := url.Parse(f.Calls()[0].URL)
			if err != nil {
				t.Fatal(err)
			}
			if u.Query().Has("content_rating") {
				t.Error("the rating was sent as a query parameter; its semantics there are undocumented")
			}
		})
	}
}

// An entry the site never classified is kept. Refusing to show something
// because the site forgot to rate it is the wrong way round.
func TestAnUnratedEntryIsKept(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/search": jsonRoute("search.json"),
	})
	th := comick.NewWithClock(f, clock)
	src := site()
	src.Overrides = map[string]any{"maxContentRating": "safe"}

	got, err := th.Search(context.Background(), src, "lantern", 1)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range got {
		if s.Title == "The Harbour Road" {
			found = true
		}
	}
	if !found {
		t.Error("the unrated entry was filtered out")
	}
}

// The series page's markup is what the frontend does with the payload; the
// payload is the data. Reading it is both more complete and more stable.
func TestSeriesReadsTheEmbeddedPayload(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Lantern Keeper" {
		t.Errorf("title = %q", got.Title)
	}
	// The rendered paragraph is truncated; the payload's is not.
	if strings.HasSuffix(got.Description, "...") || !strings.HasSuffix(got.Description, "before she reaches it.") {
		t.Errorf("description = %q, want the payload's complete text", got.Description)
	}
	if got.Status != theme.StatusCompleted {
		t.Errorf("status = %q, want the integer code normalised", got.Status)
	}
	if len(got.Authors) != 2 || got.Authors[0] != "IWANO Miyuki" {
		t.Errorf("authors = %v", got.Authors)
	}
	if len(got.Artists) != 0 {
		t.Errorf("artists = %v, want none: the payload's list is empty", got.Artists)
	}
	if len(got.AltTitles) != 2 {
		t.Errorf("altTitles = %v, want both", got.AltTitles)
	}
	if len(got.Genres) != 3 {
		t.Errorf("genres = %v, want 3", got.Genres)
	}
	if got.CoverURL != "https://pages.example.invalid/the-lantern-keeper/covers/8edfff1d.jpg" {
		t.Errorf("cover = %q", got.CoverURL)
	}
}

// The chapter list is addressed by slug, and filtered to the source's language
// unless the user asks otherwise.
func TestChaptersAskTheAPIBySlugAndLanguage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/comics/the-lantern-keeper/chapter-list?lang=en": jsonRoute("chapter-list.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("got %d chapters, want 7", len(got))
	}
}

func TestIncludeAllLanguagesDropsTheFilter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/comics/the-lantern-keeper/chapter-list": jsonRoute("chapter-list.json"),
	})
	th := comick.NewWithClock(f, clock)

	src := site()
	src.Overrides = map[string]any{"includeAllLanguages": true}
	if _, err := th.Chapters(context.Background(), src, seriesID); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(f.Calls()[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Has("lang") {
		t.Error("the language filter was still sent")
	}
}

// PLAN §7.2's ordering contract. testdata/chapter-list.json is descending on
// purpose, so this fails if SortAndMark is removed — and the unnumbered
// one-shot in the middle fails it a second way if the list is *sorted by
// number* rather than reversed.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/comics/the-lantern-keeper/chapter-list": jsonRoute("chapter-list.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"/comic/the-lantern-keeper/LB0hBFf-chapter-3-en",
		"/comic/the-lantern-keeper/MC1iCGg-chapter-4-en",
		// The one-shot keeps the place the site gave it, between 4 and 5. A
		// sort by number would have flung it to one end.
		"/comic/the-lantern-keeper/ND2jDHh-chapter--en",
		"/comic/the-lantern-keeper/OE3kEIi-chapter-5-en",
		"/comic/the-lantern-keeper/PF4lFJj-chapter-6-en",
		"/comic/the-lantern-keeper/QG5mGKk-chapter-6.5-en",
		"/comic/the-lantern-keeper/RH6nHLl-chapter-7-en",
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("chapter %d = %q (%q), want %q", i, got[i].ID, got[i].Title, want)
		}
	}
	if !theme.OrderIsKnown(got) {
		t.Error("the list is marked order-unknown; the direction is plain from the numbers")
	}
	if got[5].Number != 6.5 {
		t.Errorf("number = %v, want the half-chapter parsed as 6.5", got[5].Number)
	}
	if got[2].Number != -1 {
		t.Errorf("the one-shot got number %v, want -1", got[2].Number)
	}
}

func TestChapterFieldsAreBuiltFromBothTitleFields(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/comics/the-lantern-keeper/chapter-list": jsonRoute("chapter-list.json"),
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}

	last := got[len(got)-1]
	if last.Title != "Chapter 7: The Last Lamp" {
		t.Errorf("title = %q, want the number and the name", last.Title)
	}
	if last.Volume != "2" {
		t.Errorf("volume = %q", last.Volume)
	}
	if last.Scanlator != "Harbour Road Scans, Lamplight" {
		t.Errorf("scanlator = %q, want both groups", last.Scanlator)
	}
	if want := time.Date(2026, 9, 7, 17, 4, 15, 717343000, time.UTC); !last.Published.Equal(want) {
		t.Errorf("published = %v, want %v", last.Published, want)
	}

	// A chapter with a number and no name.
	if got[4].Title != "Chapter 6" {
		t.Errorf("title = %q, want just the number", got[4].Title)
	}
	// A chapter with a name and no number, and an unparseable date.
	if got[2].Title != "Side Story: The Harbour Road" {
		t.Errorf("title = %q, want just the name", got[2].Title)
	}
	if !got[2].Published.IsZero() {
		t.Errorf("published = %v, want the zero time: that date does not parse", got[2].Published)
	}
	if got[2].Volume != "" {
		t.Errorf("volume = %q, want none: the payload's is null", got[2].Volume)
	}
}

func TestPagesReadTheReaderPayload(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + chapterID: {File: "reader.html"},
	})
	th := comick.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), chapterID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://pages.example.invalid/the-lantern-keeper/0_7/en/64be7829/0.webp",
		"https://pages.example.invalid/the-lantern-keeper/0_7/en/64be7829/1.webp",
		"https://pages.example.invalid/the-lantern-keeper/0_7/en/64be7829/2.webp",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d (the empty entry must be skipped): %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The payload is matched by identifier *and* declared type. The page carries a
// third-party script and a like-named non-JSON one, and handing either to a
// JSON parser is how this breaks quietly.
func TestTheEmbeddedPayloadMustDeclareItselfAsJSON(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + chapterID: {Body: `<html><body><script id="sv-data">not json</script></body></html>`},
	})
	th := comick.NewWithClock(f, clock)

	_, err := th.Pages(context.Background(), site(), chapterID)
	if err == nil {
		t.Fatal("a script element with no JSON type was accepted as the payload")
	}
	if !strings.Contains(err.Error(), "sv-data") {
		t.Errorf("error %q does not name what was missing", err)
	}
}

// PLAN §7.6: the Referer must name a page this theme actually fetched — here
// the chapter page Pages() requests.
func TestPageRefererNamesTheChapterPageThatWasFetched(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + chapterID: {File: "reader.html"},
	})
	th := comick.NewWithClock(f, clock)
	src := site()

	if _, err := th.Pages(context.Background(), src, chapterID); err != nil {
		t.Fatal(err)
	}
	var pr theme.PageReferrer = th
	got := pr.PageReferer(src, chapterID)
	if got != f.Calls()[0].URL {
		t.Errorf("PageReferer = %q, but Pages fetched %q; the header would name a page we did not read", got, f.Calls()[0].URL)
	}
}

func TestPageRefererIsEmptyWhenThereIsNothingToName(t *testing.T) {
	th := comick.New(nil)
	src := site()
	src.BaseURL = "://not a url"
	if got := th.PageReferer(src, chapterID); got != "" {
		t.Errorf("PageReferer = %q, want empty: a fabricated Referer is what PLAN §7.6 forbids", got)
	}
}

func TestDeclaredHostIsANarrowWildcard(t *testing.T) {
	hosts := comick.New(nil).AllowedHosts()
	if len(hosts) != 1 {
		t.Fatalf("hosts = %v, want exactly the one domain the payload names", hosts)
	}
	if !strings.HasPrefix(hosts[0], "*.") {
		t.Errorf("host %q is not the subdomain-only form; the apex serves nothing", hosts[0])
	}
}

func TestSuggestedNameIsTheSoftwareName(t *testing.T) {
	if got := comick.New(nil).SuggestedName(); got == "" {
		t.Error("no suggested name; the name is the same whichever host a user points at")
	}
}

func TestDiscoveryOnlyIsAvailable(t *testing.T) {
	var th theme.Theme = comick.New(nil)
	d, ok := th.(theme.DiscoveryClassifier)
	if !ok {
		t.Fatal("theme does not implement DiscoveryClassifier")
	}
	if d.DiscoveryOnly().ID() != comick.ID {
		t.Error("the discovery-only copy is a different theme")
	}
}

func TestFingerprintClearsTheThresholdOnEveryFixture(t *testing.T) {
	const confident = 60
	for _, file := range []string{"home.html", "series.html", "reader.html"} {
		t.Run(file, func(t *testing.T) {
			score := comick.New(nil).Fingerprint(pageFromFile(t, file))
			if score < confident {
				t.Errorf("score = %d, want at least %d", score, confident)
			}
			t.Logf("score = %d", score)
		})
	}
}

// A bare API response is **not** a page the probe judges, and this records what
// it actually scores rather than pretending otherwise.
//
// PLAN §7.5 stage 2 fetches the root the user pasted; nothing in the probe ever
// lands on /api/search. So the shell signals — which are what carry every real
// rendering — cannot fire here, and what is left is the envelope and one
// identifier key: 50, under the threshold of 60.
//
// That is the honest number and it is deliberately not inflated. §9's rule is
// to fingerprint the page the probe judges, and the corollary is that padding
// weights to make a body the probe never sees clear the bar would be tuning
// against a fixture rather than a site — the exact mistake that put this
// theme's home-page score at 25.
//
// It is pinned so that a later change has to look at it. The fixture's score
// and the live response's agreed exactly when measured on 2026-09-16, which is
// the property that matters: an earlier version of the fixture scored 65 here
// and 50 on the wire, because it wrote unescaped slashes the server escapes.
func TestABareAPIResponseScoresBelowTheThresholdAndThatIsCorrect(t *testing.T) {
	const measured = 50
	got := comick.New(nil).Fingerprint(pageFromFile(t, "search.json"))
	if got != measured {
		t.Errorf("score = %d, want %d — the number measured against the live response on "+
			"2026-09-16. If the fingerprint changed, re-measure rather than editing this.", got, measured)
	}
	if got >= 60 {
		t.Errorf("score = %d: a body the probe never fetches now clears the threshold, "+
			"which means weights were tuned for something other than a real landing page", got)
	}
}

func TestFingerprintIsZeroedByAnotherThemesMarker(t *testing.T) {
	th := comick.New(nil)
	base := pageFromFile(t, "home.html")
	if th.Fingerprint(base) < 60 {
		t.Fatal("the unmodified home page does not score; the rest of this test proves nothing")
	}
	for _, marker := range []string{
		`<link rel="stylesheet" href="/wp-content/plugins/madara/css/style.css">`,
		`<script>ts_reader.run({"sources":[]});</script>`,
		`<div id="chapter-list-container" data-chapter-url-template="/x/{slug}"></div>`,
		`<div x-data="{ n: checkNewChapter('2026-01-01') }"></div>`,
		`<div id="_imageList"></div>`,
		`<section id="chapter-images"></section>`,
		`<div id="viewer"><img class="reader-page"></div>`,
	} {
		t.Run(marker[:20], func(t *testing.T) {
			poisoned := append(append([]byte(nil), base.Body...), []byte(marker)...)
			u, _ := url.Parse("https://example.invalid/")
			if score := th.Fingerprint(probe.NewPage(u, nil, 200, nil, poisoned)); score != 0 {
				t.Errorf("score = %d, want 0: %s belongs to another theme", score, marker)
			}
		})
	}
}

func pageFromFile(t *testing.T, file string) *probe.Page {
	t.Helper()
	b, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse("https://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	return probe.NewPage(u, nil, 200, nil, b)
}
