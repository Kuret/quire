package weebcentral_test

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
	"github.com/rickl/quire/backend/theme/themetest"
	"github.com/rickl/quire/backend/theme/weebcentral"
)

// Every test here runs offline: the fixtures are synthetic pages on
// example.invalid and DNS is disabled for the package, so a future "just point
// it at the real site to check" is a red test rather than a quiet network call.
// See docs/THEME-NOTES.md, "What the fixtures do and do not prove".
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func clock() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-06", Name: "Example Comics", Lang: "en",
		Theme: weebcentral.ID, BaseURL: "https://example.invalid",
		AddedAt: clock(),
	}
}

const seriesID = "/series/01EXAMPLE0LANTERNKEEPER01/The-Lantern-Keeper"

// The search fragment, not the human-facing search page. The query string is
// part of the contract: display_mode decides whether the rows carry a title at
// all, so a theme that dropped it would return a page of unnamed results.
func TestSearchAsksTheFragmentEndpointWithADisplayMode(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data?adult=False&display_mode=Full+Display&limit=32&offset=0&order=Descending&sort=Best+Match&text=lantern": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "  lantern  ", 1)
	if err != nil {
		t.Fatalf("the query was not spelled the way the site expects it: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(got), got)
	}
	want := theme.SeriesStub{
		ID:       seriesID,
		Title:    "The Lantern Keeper",
		CoverURL: "https://covers.example.invalid/cover/normal/01EXAMPLE0LANTERNKEEPER01.webp",
	}
	if got[0] != want {
		t.Errorf("result 0 = %+v, want %+v", got[0], want)
	}
	// The second row's cover is a bare <img> rather than a <picture>.
	if got[1].CoverURL != "https://covers.example.invalid/cover/normal/01EXAMPLE0SALTANDCINDER01.webp" {
		t.Errorf("result 1 cover = %q", got[1].CoverURL)
	}
}

// A chapter link in the search fragment is not a search result. Returning one
// gives the user a row that opens onto nothing.
func TestSearchDropsChapterRows(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "lantern", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if !strings.HasPrefix(s.ID, "/series/") {
			t.Errorf("result ID %q is not a series path", s.ID)
		}
	}
}

// Browse is a search with an empty query (PLAN §7.5, M3 correction 1), and
// relevance ordering against an empty query orders nothing — hence the
// browseSort override, which must actually reach the request.
func TestBrowseSendsTheConfiguredSortRatherThanRelevance(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/data": {File: "search.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	src := site()
	src.Overrides = map[string]any{"browseSort": "Latest Updates"}
	if _, err := th.Search(context.Background(), src, "", 2); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	u, err := url.Parse(calls[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if got := q.Get("sort"); got != "Latest Updates" {
		t.Errorf("sort = %q, want the configured browseSort", got)
	}
	if got := q.Get("text"); got != "" {
		t.Errorf("text = %q, want empty", got)
	}
	// Page 2 is an offset, not a page number.
	if got := q.Get("offset"); got != "32" {
		t.Errorf("offset = %q, want 32 for page 2", got)
	}
}

// Adult content is opt-in, and "opt-in" has to mean the request says so.
func TestAdultContentIsExcludedUnlessAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		include bool
		want    string
	}{
		{name: "default", include: false, want: "False"},
		{name: "opted in", include: true, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /search/data": {File: "search.html"},
			})
			th := weebcentral.NewWithClock(f, clock)
			src := site()
			src.Overrides = map[string]any{"includeAdultContent": tc.include}
			if _, err := th.Search(context.Background(), src, "lantern", 1); err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(f.Calls()[0].URL)
			if err != nil {
				t.Fatal(err)
			}
			if got := u.Query().Get("adult"); got != tc.want {
				t.Errorf("adult = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSeriesReadsTheLabelledRows(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Lantern Keeper" {
		t.Errorf("title = %q", got.Title)
	}
	if got.CoverURL != "https://covers.example.invalid/cover/normal/01EXAMPLE0LANTERNKEEPER01.webp" {
		t.Errorf("cover = %q", got.CoverURL)
	}
	if got.Status != theme.StatusCompleted {
		t.Errorf("status = %q, want the site's %q normalised to Quire's vocabulary", got.Status, "Complete")
	}
	if len(got.Authors) != 2 || got.Authors[0] != "IWANO Miyuki" {
		t.Errorf("authors = %v, want both names", got.Authors)
	}
	if len(got.Genres) != 5 {
		t.Errorf("genres = %v, want 5", got.Genres)
	}
	if len(got.AltTitles) != 2 || got.AltTitles[1] != "The Keeper of Lanterns" {
		t.Errorf("altTitles = %v", got.AltTitles)
	}
	if !strings.HasPrefix(got.Description, "A lamplighter walks") {
		t.Errorf("description = %q", got.Description)
	}
}

// The series page carries a *truncated* chapter list, so reading it would
// return three chapters out of eight and look entirely healthy. The only
// correct source is the fragment endpoint, and the fragment endpoint takes the
// bare identifier rather than the id/slug path.
func TestChaptersNeverReadsTheSeriesPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /series/01EXAMPLE0LANTERNKEEPER01/full-chapter-list": {File: "chapters.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Requested("GET", seriesID) {
		t.Error("the series page was fetched; its chapter list is truncated")
	}
	if len(got) != 8 {
		t.Fatalf("got %d chapters, want all 8: %+v", len(got), got)
	}
}

// PLAN §7.2's ordering contract. testdata/chapters.html is deliberately
// descending, so this fails if SortAndMark is removed — which is the only way
// a test about ordering is worth anything.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /series/01EXAMPLE0LANTERNKEEPER01/full-chapter-list": {File: "chapters.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}

	wantIDs := []string{
		"/chapters/01EXAMPLE0CHAPTERONE00001",
		"/chapters/01EXAMPLE0CHAPTERTWO00001",
		"/chapters/01EXAMPLE0CHAPTERTHREE001",
		"/chapters/01EXAMPLE0CHAPTERTHREE501",
		"/chapters/01EXAMPLE0CHAPTERFOUR0001",
		// The unnumbered extra keeps the place the site gave it, between 4 and
		// 5. A sort by number would have flung it to one end.
		"/chapters/01EXAMPLE0SPECIALHARBOUR1",
		"/chapters/01EXAMPLE0CHAPTERFIVE0001",
		"/chapters/01EXAMPLE0CHAPTERSIX00001",
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("chapter %d = %q (%q), want %q", i, got[i].ID, got[i].Title, want)
		}
	}
	if !theme.OrderIsKnown(got) {
		t.Error("the list is marked order-unknown; the direction is plain from the numbers")
	}
	if got[3].Number != 3.5 {
		t.Errorf("number = %v, want the half-chapter parsed as 3.5", got[3].Number)
	}
	if got[5].Number != -1 {
		t.Errorf("the unnumbered chapter got number %v, want -1", got[5].Number)
	}
}

// The chapter title must not pick up the timestamp or the reading-progress
// badge that share the row, and the official-release badge is the only
// attribution the list carries.
func TestChapterRowsCarryTitleDateAndAttribution(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /series/01EXAMPLE0LANTERNKEEPER01/full-chapter-list": {File: "chapters.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}

	last := got[len(got)-1]
	if last.Title != "Chapter 6" {
		t.Errorf("title = %q, want just the chapter name", last.Title)
	}
	if want := time.Date(2026, 9, 7, 17, 4, 15, 717000000, time.UTC); !last.Published.Equal(want) {
		t.Errorf("published = %v, want %v", last.Published, want)
	}
	if last.Scanlator != "" {
		t.Errorf("scanlator = %q, want none: this row's badge is the ordinary one", last.Scanlator)
	}

	official := got[6] // "Chapter 5", the row with the -official badge
	if official.Scanlator != "Official" {
		t.Errorf("scanlator = %q, want Official", official.Scanlator)
	}

	special := got[5]
	if !special.Published.IsZero() {
		t.Errorf("published = %v, want the zero time: that row has no <time>", special.Published)
	}
}

func TestPagesReadsTheReaderFragment(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /chapters/01EXAMPLE0CHAPTERSIX00001/images?is_prev=False&reading_style=long_strip": {File: "reader.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/chapters/01EXAMPLE0CHAPTERSIX00001")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://pages.example.invalid/manga/The-Lantern-Keeper/0006-001.png",
		"https://pages.example.invalid/manga/The-Lantern-Keeper/0006-002.png",
		"https://pages.example.invalid/manga/The-Lantern-Keeper/0006-003.png",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			// The second page also carries a srcset naming a small rendition.
			// Taking it would mean downloading a 400px image for a 1620px
			// screen.
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Every <img> in the reader fragment names a same-site placeholder in its
// onerror handler. A theme reading "any image URL on this node" would hand the
// download queue a chapter of broken-image icons — a silent failure, which is
// worse than a loud one.
func TestPagesIgnoresTheSiteAssetPlaceholder(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /chapters/01EXAMPLE0CHAPTERSIX00001/images": {File: "reader.html"},
	})
	th := weebcentral.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/chapters/01EXAMPLE0CHAPTERSIX00001")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range got {
		if strings.Contains(u, "/static/") {
			t.Errorf("page %q is a site asset, not a comic page", u)
		}
	}
}

// PLAN §7.2: AllowedHosts widens the registrable-domain boundary and §7.5
// stage 5 fetches a real image through it, so an image host this theme
// extracts and does not declare fails the probe. The reader fixture's host
// stands in for the real ones, which are the §1.3 exception and live in the
// theme's source rather than in a fixture.
func TestEveryDeclaredHostIsANarrowWildcard(t *testing.T) {
	hosts := weebcentral.New(nil).AllowedHosts()
	if len(hosts) == 0 {
		t.Fatal("no hosts declared; this theme's images are all off-domain")
	}
	for _, h := range hosts {
		if !strings.HasPrefix(h, "*.") {
			t.Errorf("host %q is not the subdomain-only form; the apexes serve nothing", h)
		}
		if strings.Count(h, ".") < 2 {
			t.Errorf("host %q is too broad to be a single CDN", h)
		}
	}
}

// PLAN §7.5 stage 6: the theme's own name beats a page <title>, and this theme
// drives one site so it has one to give.
func TestSuggestedNameIsTheSiteName(t *testing.T) {
	if got := weebcentral.New(nil).SuggestedName(); got == "" {
		t.Error("no suggested name; a single-site theme has one to offer")
	}
}

// A theme that cannot be asked for a discovery-only copy has not answered
// PLAN §12.2's question (theme.go, DiscoveryClassifier).
func TestDiscoveryOnlyIsAvailable(t *testing.T) {
	var th theme.Theme = weebcentral.New(nil)
	d, ok := th.(theme.DiscoveryClassifier)
	if !ok {
		t.Fatal("theme does not implement DiscoveryClassifier")
	}
	if d.DiscoveryOnly().ID() != weebcentral.ID {
		t.Error("the discovery-only copy is a different theme")
	}
}

// The home page is the one page PLAN §7.5 stage 4 is guaranteed to see, but a
// probe can land on any of these, and "unrecognised" on a page we can plainly
// read is the failure this test exists to prevent.
func TestFingerprintClearsTheThresholdOnEveryFixture(t *testing.T) {
	const confident = 60
	for _, file := range []string{"home.html", "search.html", "series.html", "chapters.html", "reader.html"} {
		t.Run(file, func(t *testing.T) {
			b, err := os.ReadFile("testdata/" + file)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse("https://example.invalid/")
			if err != nil {
				t.Fatal(err)
			}
			score := weebcentral.New(nil).Fingerprint(probe.NewPage(u, nil, 200, nil, b))
			if score < confident {
				t.Errorf("score = %d, want at least %d", score, confident)
			}
			t.Logf("score = %d", score)
		})
	}
}

// The negative signals, which are what keeps this theme off another's pages.
// A page carrying another theme's unmistakable marker is that theme's page
// however much of this one's plumbing it also happens to contain.
func TestFingerprintIsZeroedByAnotherThemesMarker(t *testing.T) {
	u, err := url.Parse("https://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile("testdata/home.html")
	if err != nil {
		t.Fatal(err)
	}
	th := weebcentral.New(nil)
	if th.Fingerprint(probe.NewPage(u, nil, 200, nil, base)) < 60 {
		t.Fatal("the unmodified home page does not score; the rest of this test proves nothing")
	}

	for _, marker := range []string{
		`<link rel="stylesheet" href="/wp-content/plugins/madara/css/style.css">`,
		`<script>ts_reader.run({"sources":[]});</script>`,
		`<div id="chapter-list-container" data-chapter-url-template="/x/{slug}"></div>`,
	} {
		t.Run(marker[:20], func(t *testing.T) {
			poisoned := append(append([]byte(nil), base...), []byte(marker)...)
			if score := th.Fingerprint(probe.NewPage(u, nil, 200, nil, poisoned)); score != 0 {
				t.Errorf("score = %d, want 0: %s belongs to another theme", score, marker)
			}
		})
	}
}
