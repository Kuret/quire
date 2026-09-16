package mangakakalot_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/mangakakalot"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test here runs offline: the fixtures are synthetic pages on
// example.invalid and DNS is disabled for the package, so a future "just point
// it at the real site to check" is a red test rather than a quiet network call.
// See docs/THEME-NOTES.md, "What the fixtures do and do not prove".
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func clock() time.Time { return time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC) }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-05", Name: "Example Comics", Lang: "en",
		Theme: mangakakalot.ID, BaseURL: "https://example.invalid",
		AddedAt: clock(),
	}
}

// An empty query is the UI's Browse (PLAN §7.5, M3 correction 1), and this
// family answers it from a listing rather than from its search endpoint.
func TestSearchWithNoQueryBrowsesTheListing(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga-list/latest-manga?page=1": {File: "browse.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []theme.SeriesStub{
		{
			ID:       "/manga/the-lantern-keeper",
			Title:    "The Lantern Keeper",
			CoverURL: "https://images.example.invalid/thumb/the-lantern-keeper.webp",
		},
		{
			// No title attribute on this card, so the link text is the title;
			// the cover is only in data-src, which is where a lazy skin puts it.
			ID:       "/manga/paper-birds",
			Title:    "Paper Birds",
			CoverURL: "https://images.example.invalid/thumb/paper-birds.webp",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// A real query goes to the site's own search, whose query is a path segment
// and is normalised to the site's spelling first.
func TestSearchUsesTheNormalisedQueryPath(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/story/the_lantern_keeper?page=2": {File: "search.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "  The Lantern Keeper!  ", 2)
	if err != nil {
		t.Fatalf("the query was not spelled the way the site expects it: %v", err)
	}
	want := []theme.SeriesStub{
		{
			ID:       "/manga/the-lantern-keeper",
			Title:    "The Lantern Keeper",
			CoverURL: "https://images.example.invalid/thumb/the-lantern-keeper.webp",
		},
		{
			// No cover at all on this row, which must not drop the result.
			ID:    "/manga/lantern-keeper-side-stories",
			Title: "Lantern Keeper: Side Stories",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// Every card links to the series and to its newest chapter. A chapter URL
// returned as a series would give the user a search result that opens a reader.
func TestSearchNeverReturnsAChapterAsASeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga-list/latest-manga?page=1": {File: "browse.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if strings.Count(strings.Trim(s.ID, "/"), "/") != 1 {
			t.Errorf("result ID %q is not a series path", s.ID)
		}
	}
}

func TestSeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper": {File: "series.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), "/manga/the-lantern-keeper")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Lantern Keeper" {
		t.Errorf("title = %q", got.Title)
	}
	if got.CoverURL != "https://images.example.invalid/thumb/the-lantern-keeper.webp" {
		t.Errorf("cover = %q", got.CoverURL)
	}
	if got.Status != theme.StatusOngoing {
		t.Errorf("status = %q, want %q", got.Status, theme.StatusOngoing)
	}
	if strings.Join(got.Authors, "|") != "A. Hollow|B. Vane" {
		t.Errorf("authors = %v", got.Authors)
	}
	// The Artist row is the site's "Updating" placeholder, which is not an
	// artist and must not be shown as one.
	if len(got.Artists) != 0 {
		t.Errorf("artists = %v, want none: the row is a placeholder", got.Artists)
	}
	if strings.Join(got.Genres, "|") != "Drama|Fantasy" {
		t.Errorf("genres = %v", got.Genres)
	}
	if strings.Join(got.AltTitles, "|") != "Der Laternenwaerter|灯守り" {
		t.Errorf("alt titles = %v", got.AltTitles)
	}
	if !strings.HasPrefix(got.Description, "A lamplighter") {
		t.Errorf("description = %q; the decorative summary heading was not dropped", got.Description)
	}
}

// The structural difference that makes this its own theme: the series page has
// no chapter list in it, and the list comes from the JSON API.
func TestChaptersComeFromTheAPIAndAscend(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/manga/the-lantern-keeper/chapters?limit=-1": {
			File: "chapters.json", Header: map[string][]string{"Content-Type": {"application/json"}},
		},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), "/manga/the-lantern-keeper")
	if err != nil {
		t.Fatal(err)
	}

	// The series HTML is never fetched: there is nothing in it to fetch it for.
	if f.Requested("GET", "/manga/the-lantern-keeper") {
		t.Error("the series page was fetched for a chapter list that is not in it")
	}

	// PLAN §7.2's ordering contract. The fixture is descending, as the real API
	// is; without the reversal a volume assembled from this family reads
	// backwards.
	wantIDs := []string{
		"/manga/the-lantern-keeper/chapter-1",
		"/manga/the-lantern-keeper/chapter-3",
		"/manga/the-lantern-keeper/chapter-3-5",
		"/manga/the-lantern-keeper/chapter-4",
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d chapters, want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("chapter %d ID = %q, want %q", i, got[i].ID, want)
		}
		if got[i].OrderUnknown {
			t.Errorf("chapter %d is marked order-unknown; the numbers are all there", i)
		}
	}
	if got[0].Number != 1 || got[3].Number != 4 {
		t.Errorf("numbers = %v, %v; want 1 and 4", got[0].Number, got[3].Number)
	}
	// The decorative title carries a second number ("Chapter 4: Four Lamps");
	// the API's own number is the chapter's.
	if got[3].Title != "Chapter 4: Four Lamps" {
		t.Errorf("title = %q", got[3].Title)
	}
	// chapter_num was null on this one, so the number had to come from the
	// title rather than defaulting to zero and sorting to the front.
	if got[1].Number != 3 {
		t.Errorf("null chapter_num gave number %v, want 3", got[1].Number)
	}
	if !got[0].Published.Equal(time.Date(2026, 2, 6, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("published = %v", got[0].Published)
	}
}

// A chapter with no slug has no URL, so it is not a chapter we can offer.
func TestChaptersSkipEntriesWithNoSlug(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/manga/the-lantern-keeper/chapters?limit=-1": {File: "chapters.json"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), "/manga/the-lantern-keeper")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if strings.HasSuffix(c.ID, "/") {
			t.Errorf("chapter %q has no slug and should have been skipped", c.ID)
		}
	}
}

// An API that answers with success:false has told us something, and reporting
// it as "no chapters" would turn a failure into an empty series.
func TestChaptersReportAnUnsuccessfulAPI(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/manga/the-lantern-keeper/chapters?limit=-1": {Body: `{"success":false,"data":null}`},
	})
	th := mangakakalot.NewWithClock(f, clock)

	if _, err := th.Chapters(context.Background(), site(), "/manga/the-lantern-keeper"); err == nil {
		t.Fatal("an unsuccessful API response was reported as an empty chapter list")
	}
}

func TestPagesFromTheRenderedReader(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/chapter-4": {File: "reader.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/manga/the-lantern-keeper/chapter-4")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://images.example.invalid/the-lantern-keeper/4/0.webp",
		"https://images.example.invalid/the-lantern-keeper/4/1.webp",
		"https://images.example.invalid/the-lantern-keeper/4/2.webp",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("pages =\n %v\nwant\n %v", got, want)
	}
}

// A mirror whose reader lazy-loads has no usable image sources in the DOM, and
// the inline arrays are the only real answer. No reconfiguration required.
func TestPagesFallBackToTheInlineArrays(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/chapter-4": {File: "reader-lazy.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/manga/the-lantern-keeper/chapter-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d pages, want 3: %v", len(got), got)
	}
	// The escaped slashes of the JS literal are not part of the URL.
	for _, u := range got {
		if strings.Contains(u, `\`) {
			t.Errorf("page URL %q still carries the JavaScript escaping", u)
		}
		if !strings.HasPrefix(u, "https://images.example.invalid/") {
			t.Errorf("page URL %q was not hung on the CDN the page named", u)
		}
	}
}

// The override forces the other source first; both must agree on this page,
// because on the real family both are really there.
func TestPageSourceOverride(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/chapter-4": {File: "reader.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	s := site()
	s.Overrides = map[string]any{mangakakalot.KeyPageSource: mangakakalot.PageSourceScript}
	got, err := th.Pages(context.Background(), s, "/manga/the-lantern-keeper/chapter-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "https://images.example.invalid/the-lantern-keeper/4/0.webp" {
		t.Errorf("pages = %v", got)
	}
}

// A mirror that renamed its path segment is a configuration change, not a new
// theme — which is the whole reason the segment is an override.
func TestSeriesSubPathOverrideReachesEveryEndpoint(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /comic/the-lantern-keeper":                       {File: "series.html"},
		"GET /api/manga/the-lantern-keeper/chapters?limit=-1": {File: "chapters.json"},
		"GET /comic/the-lantern-keeper/chapter-4":             {File: "reader.html"},
		"GET /manga-list/latest-manga?page=1":                 {File: "browse.html"},
	})
	th := mangakakalot.NewWithClock(f, clock)

	s := site()
	s.Overrides = map[string]any{mangakakalot.KeySeriesSubPath: "comic"}

	if _, err := th.Series(context.Background(), s, "the-lantern-keeper"); err != nil {
		t.Fatalf("series: %v", err)
	}
	chs, err := th.Chapters(context.Background(), s, "the-lantern-keeper")
	if err != nil {
		t.Fatalf("chapters: %v", err)
	}
	if len(chs) == 0 || !strings.HasPrefix(chs[0].ID, "/comic/") {
		t.Fatalf("chapter IDs were not built under the configured segment: %+v", chs)
	}
	if _, err := th.Pages(context.Background(), s, chs[len(chs)-1].ID); err != nil {
		t.Fatalf("pages: %v", err)
	}
}

// PLAN §7.2 (2026-09-15): the theme declares the hosts it needs, because it is
// the thing that knows its own CDN and the alternative is a user meeting an
// SSRF rejection naming a host they have never heard of.
func TestAllowedHostsCoverTheImagesTheFixturesUse(t *testing.T) {
	got := mangakakalot.New(nil).AllowedHosts()
	if len(got) == 0 {
		t.Fatal("this family serves its images from separate hosts; declaring none refuses every download")
	}
	for _, h := range got {
		if h != strings.ToLower(h) || strings.Contains(h, "/") {
			t.Errorf("%q is not a bare, normalised host pattern", h)
		}
	}
}

// The probe fingerprints whatever page the user pasted, which is usually the
// home page and may be any of the others. Each must be recognised on its own.
func TestFingerprintRecognisesEveryPageOfTheFamily(t *testing.T) {
	th := mangakakalot.New(nil)
	for _, f := range []string{"home.html", "browse.html", "search.html", "series.html", "reader.html"} {
		t.Run(f, func(t *testing.T) {
			b, err := os.ReadFile("testdata/" + f)
			if err != nil {
				t.Fatal(err)
			}
			p := probe.NewPage(nil, nil, 200, nil, b)
			if got := th.Fingerprint(p); got < 60 {
				t.Errorf("scored %d, below the probe's threshold of 60", got)
			}
		})
	}
}
