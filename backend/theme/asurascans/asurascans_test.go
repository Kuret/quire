package asurascans_test

import (
	"context"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/asurascans"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test here runs offline: the fixtures are synthetic and DNS is
// disabled for the package, so a future "just point it at the real site to
// check" is a red test rather than a quiet network call. See
// docs/THEME-NOTES.md, "What the fixtures do and do not prove".
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func clock() time.Time { return time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC) }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-01", Name: "Example Comics", Lang: "en",
		Theme: asurascans.ID, BaseURL: "https://example.invalid",
		AddedAt: clock(),
	}
}

const seriesID = "/comics/example-series-one"

func TestSearchWithAQueryHitsTheAPIsSearchParameter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series?limit=20&offset=0&search=solo": {File: "search-solo.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "  solo  ", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	want := theme.SeriesStub{
		ID:       seriesID,
		Title:    "Example Series One",
		CoverURL: "https://cdn.example.invalid/asura-images/covers/example-series-one.webp",
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

// An empty query is PLAN §7.5's Browse case. The site's own relevance
// ranking means nothing without a term, so the browseSort override's ranking
// is sent explicitly instead of a "search" parameter.
func TestSearchWithNoQueryUsesBrowseSort(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series?limit=20&offset=0&order=desc&sort=popular": {File: "browse.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Example Series Two" {
		t.Fatalf("got %+v, want the browse listing's one result", got)
	}
}

func TestSearchHonoursBrowseSortOverride(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series?limit=20&offset=0&order=desc&sort=newest": {File: "browse.json"},
	})
	s := site()
	s.Overrides = map[string]any{asurascans.KeyBrowseSort: "newest"}
	th := asurascans.NewWithClock(f, clock)

	if _, err := th.Search(context.Background(), s, "", 1); err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestSearchPagesWithOffset(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series?limit=20&offset=20&search=solo": {File: "search-solo.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	if _, err := th.Search(context.Background(), site(), "solo", 2); err != nil {
		t.Fatalf("Search page 2: %v", err)
	}
}

func TestSeriesParsesFullMetadata(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series/example-series-one": {File: "series-example-series-one.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if got.Title != "Example Series One" {
		t.Errorf("Title = %q", got.Title)
	}
	if len(got.AltTitles) != 2 || got.AltTitles[0] != "Placeholder Alt Title" {
		t.Errorf("AltTitles = %+v", got.AltTitles)
	}
	if got.CoverURL != "https://cdn.example.invalid/asura-images/covers/example-series-one.webp" {
		t.Errorf("CoverURL = %q", got.CoverURL)
	}
	wantDesc := "A placeholder synopsis, first paragraph.\n\nSecond paragraph with a \nline break."
	if got.Description != wantDesc {
		t.Errorf("Description = %q, want %q", got.Description, wantDesc)
	}
	if len(got.Authors) != 1 || got.Authors[0] != "Placeholder Author" {
		t.Errorf("Authors = %+v", got.Authors)
	}
	// The API's own placeholder for an unset field is a bare "-"; an author
	// called "-" would be worse than no author.
	if len(got.Artists) != 0 {
		t.Errorf("Artists = %+v, want none (the API sent the placeholder \"-\")", got.Artists)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "Action" || got.Genres[1] != "Fantasy" {
		t.Errorf("Genres = %+v", got.Genres)
	}
	if got.Status != theme.StatusHiatus {
		t.Errorf("Status = %q, want hiatus", got.Status)
	}
	if got.ID != seriesID {
		t.Errorf("ID = %q, want %q", got.ID, seriesID)
	}
}

func TestSeriesRejectsAnIDThatIsNotOne(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := asurascans.NewWithClock(f, clock)
	if _, err := th.Series(context.Background(), site(), ""); err == nil {
		t.Fatal("want an error for an empty id")
	}
}

// The fixture lists chapters newest-first, matching the real API's own order
// (and the site's own server-rendered list). This is the adversarial case
// PLAN §6/docs/THEME-NOTES.md ask for: a fixture that happens to already be
// ascending would not exercise SortAndMark's reversal at all.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series/example-series-one/chapters": {File: "chapters-example-series-one.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatalf("Chapters: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d chapters, want 5", len(got))
	}
	for i, ch := range got {
		want := float64(i + 1)
		if ch.Number != want {
			t.Fatalf("chapter %d: Number = %v, want %v (list was not reversed to ascending)", i, ch.Number, want)
		}
		if ch.OrderUnknown {
			t.Fatalf("chapter %d: OrderUnknown, want a known order (every chapter was numbered)", i)
		}
	}
	if !theme.OrderIsKnown(got) {
		t.Fatal("OrderIsKnown = false, want true")
	}
	if got[4].ID != seriesID+"/chapter/5" {
		t.Errorf("last chapter ID = %q, want %s/chapter/5", got[4].ID, seriesID)
	}
	if got[4].Title != "Chapter 5" {
		t.Errorf("last chapter Title = %q, want %q", got[4].Title, "Chapter 5")
	}
	if !got[4].Published.Equal(time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("last chapter Published = %v", got[4].Published)
	}
	// The API's "unset" sentinel timestamp must not read as a real date.
	if !got[0].Published.IsZero() {
		t.Errorf("first chapter Published = %v, want the zero time for the API's 0001-01-01 sentinel", got[0].Published)
	}
}

func TestPagesReturnsImageURLsInOrder(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series/example-series-one/chapters/5": {File: "chapter-5.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), seriesID+"/chapter/5")
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	want := []string{
		"https://cdn.example.invalid/asura-images/chapters/example-series-one/5/001.webp",
		"https://cdn.example.invalid/asura-images/chapters/example-series-one/5/002.webp",
		"https://cdn.example.invalid/asura-images/chapters/example-series-one/5/003.webp",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A chapter can exist and still not be readable — a subscription wall, an
// early-access window not yet open. The fixture's page list is empty and its
// access_gate is set; Pages must say so rather than reporting "no pages" the
// same way it would for a chapter that genuinely has none.
func TestPagesReportsAGatedChapterAsAnError(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series/example-series-one/chapters/3": {File: "chapter-3-gated.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	_, err := th.Pages(context.Background(), site(), seriesID+"/chapter/3")
	if err == nil {
		t.Fatal("want an error for a gated chapter")
	}
}

// A gated chapter can still ship a preview page or two, so the access_gate
// check has to be tested independently of "the page list came back empty" —
// a theme that dropped the access_gate check but kept the empty-list check
// would still pass the test above.
func TestPagesReportsAGatedChapterAsAnErrorEvenWithPreviewPages(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series/example-series-one/chapters/3": {File: "chapter-3-gated-with-preview.json"},
	})
	th := asurascans.NewWithClock(f, clock)

	_, err := th.Pages(context.Background(), site(), seriesID+"/chapter/3")
	if err == nil {
		t.Fatal("want an error for a gated chapter that still shipped a preview page")
	}
}

func TestPagesRejectsAnIDThatIsNotOne(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := asurascans.NewWithClock(f, clock)
	if _, err := th.Pages(context.Background(), site(), "/comics/example-series-one"); err == nil {
		t.Fatal("want an error for a series id passed where a chapter id belongs")
	}
}

// Every request this theme makes — search, series, chapters, and the page
// manifest — is discovery. DiscoveryOnly must therefore be a genuine no-op: a
// theme that silently reclassified nothing would pass this test by accident,
// which is why it inspects the recorded Kind rather than just checking for an
// error.
func TestEveryCallIsDiscovery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/series?limit=20&offset=0&search=solo": {File: "search-solo.json"},
		"GET /api/series/example-series-one":            {File: "series-example-series-one.json"},
		"GET /api/series/example-series-one/chapters":   {File: "chapters-example-series-one.json"},
		"GET /api/series/example-series-one/chapters/5": {File: "chapter-5.json"},
	})
	th := asurascans.NewWithClock(f, clock).DiscoveryOnly()

	if _, err := th.Search(context.Background(), site(), "solo", 1); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := th.Series(context.Background(), site(), seriesID); err != nil {
		t.Fatalf("Series: %v", err)
	}
	if _, err := th.Chapters(context.Background(), site(), seriesID); err != nil {
		t.Fatalf("Chapters: %v", err)
	}
	if _, err := th.Pages(context.Background(), site(), seriesID+"/chapter/5"); err != nil {
		t.Fatalf("Pages: %v", err)
	}
	for _, req := range f.Calls() {
		if req.Kind != fetch.KindDiscovery {
			t.Errorf("%s %s: Kind = %v, want discovery", req.Method, req.URL, req.Kind)
		}
	}
}

func TestOverridesRejectAnUnknownKey(t *testing.T) {
	th := asurascans.New(nil)
	if err := th.ValidateOverrides(map[string]any{"nope": true}); err == nil {
		t.Fatal("want an error for an unknown override key")
	}
}

func TestOverridesRejectAnInvalidSortValue(t *testing.T) {
	th := asurascans.New(nil)
	if err := th.ValidateOverrides(map[string]any{asurascans.KeyBrowseSort: "not-a-real-sort"}); err == nil {
		t.Fatal("want an error for a sort value outside the enum")
	}
}

func TestAllowedHostsIsNil(t *testing.T) {
	th := asurascans.New(nil)
	if got := th.AllowedHosts(); got != nil {
		t.Fatalf("AllowedHosts() = %v, want nil: the API and image hosts are plain "+
			"subdomains of the source's own host, already covered by the same-"+
			"registrable-domain rule", got)
	}
}

func TestSuggestedName(t *testing.T) {
	if got := asurascans.New(nil).SuggestedName(); got != "Asura Scans" {
		t.Fatalf("SuggestedName() = %q", got)
	}
}

// --- Fingerprint ---

func page(t *testing.T, file string, status int) *probe.Page {
	t.Helper()
	b, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	u, _ := url.Parse("https://example.invalid/")
	return probe.NewPage(u, nil, status, nil, b)
}

func TestFingerprintClearsTheThresholdOnTheHomePage(t *testing.T) {
	th := asurascans.New(nil)
	got := th.Fingerprint(page(t, "home.html", 200))
	if got < 70 {
		t.Fatalf("home page score = %d, want at least 70 (threshold 60 plus the fidelity margin)", got)
	}
}

func TestFingerprintClearsTheThresholdOnASeriesPage(t *testing.T) {
	th := asurascans.New(nil)
	got := th.Fingerprint(page(t, "series-page.html", 200))
	if got < 70 {
		t.Fatalf("series page score = %d, want at least 70", got)
	}
}

func TestFingerprintIsZeroForAnUnrelatedPage(t *testing.T) {
	th := asurascans.New(nil)
	u, _ := url.Parse("https://example.invalid/")
	got := th.Fingerprint(probe.NewPage(u, nil, 200, nil, []byte("<html><body>Hello</body></html>")))
	if got != 0 {
		t.Fatalf("unrelated page score = %d, want 0", got)
	}
}

// A page that shares this theme's markers but is unmistakably another
// family's must score 0, not merely low: madara, mangathemesia and
// weebcentral each have a marker no legitimate page of this theme can carry.
func TestFingerprintIsDisqualifiedByAnotherFamilysMarker(t *testing.T) {
	th := asurascans.New(nil)
	b, err := os.ReadFile("testdata/home.html")
	if err != nil {
		t.Fatal(err)
	}
	poisoned := append(append([]byte(nil), b...), []byte("<script>ts_reader.run({});</script>")...)
	u, _ := url.Parse("https://example.invalid/")
	got := th.Fingerprint(probe.NewPage(u, nil, 200, nil, poisoned))
	if got != 0 {
		t.Fatalf("poisoned page score = %d, want 0", got)
	}
}

func TestFingerprintNilPage(t *testing.T) {
	if got := asurascans.New(nil).Fingerprint(nil); got != 0 {
		t.Fatalf("Fingerprint(nil) = %d, want 0", got)
	}
}
