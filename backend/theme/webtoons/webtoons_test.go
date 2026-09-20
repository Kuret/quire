package webtoons_test

import (
	"context"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
	"github.com/rickl/quire/backend/theme/webtoons"
)

// Every test here runs offline: the fixtures are synthetic pages on
// example.invalid and DNS is disabled for the package, so a future "just point
// it at the real site to check" is a red test rather than a quiet network call.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func clock() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-07", Name: "Example Comics", Lang: "en",
		Theme: webtoons.ID, BaseURL: "https://www.example.invalid",
		AddedAt: clock(),
	}
}

const (
	seriesID  = "/en/fantasy/the-lantern-keeper/list?title_no=95"
	chapterID = "/en/fantasy/the-lantern-keeper/season-2-ep-2/viewer?title_no=95&episode_no=6"
)

// The results are the cards; the tier tabs and the promotional episode link
// that sit among them are not series and must not be returned.
func TestSearchReturnsSeriesAndNotTabsOrEpisodes(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /en/search?keyword=lantern": {File: "search.html"},
	})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "  lantern  ", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (the same series appears twice and must be returned once): %+v", len(got), got)
	}
	want := theme.SeriesStub{
		ID:       seriesID,
		Title:    "The Lantern Keeper",
		CoverURL: "https://pages.example.invalid/20240101_1/lantern_thumb.jpg?type=q90",
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("result 0 = %+v, want %+v", got[0], want)
	}
	for _, s := range got {
		if !strings.Contains(s.ID, "/list?") {
			t.Errorf("result ID %q is not a series listing", s.ID)
		}
		if strings.Contains(s.ID, "/search") || strings.Contains(s.ID, "/viewer") {
			t.Errorf("result ID %q is a tab or an episode, not a series", s.ID)
		}
	}
}

// The scope override is a path segment, not a parameter, and paging only
// exists on the scoped searches.
func TestSearchScopeIsAPathSegment(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /en/search/canvas?keyword=lantern&page=2": {File: "search.html"},
	})
	th := webtoons.NewWithClock(f, clock)

	src := site()
	src.Overrides = map[string]any{"searchScope": "canvas"}
	if _, err := th.Search(context.Background(), src, "lantern", 2); err != nil {
		t.Fatal(err)
	}
	if !f.Requested("GET", "/en/search/canvas") {
		t.Errorf("the scope did not reach the path; calls were %+v", f.Calls())
	}
}

// The unscoped search has no second page. Asking for one anyway would invent
// results the site never returned.
func TestUnscopedSearchHasNoSecondPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "lantern", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d results, want none", len(got))
	}
	if len(f.Calls()) != 0 {
		t.Errorf("a request was made for a page that does not exist: %+v", f.Calls())
	}
}

func TestSeriesReadsTheDetailHeader(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Lantern Keeper" {
		t.Errorf("title = %q", got.Title)
	}
	if got.CoverURL != "https://pages.example.invalid/20240101_1/lantern_cover.png" {
		t.Errorf("cover = %q", got.CoverURL)
	}
	// The author block holds a button whose label must not become part of the
	// name.
	if len(got.Authors) != 1 || got.Authors[0] != "IWANO Miyuki" {
		t.Errorf("authors = %q, want just the name", got.Authors)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "Fantasy" {
		t.Errorf("genres = %v, want both headings", got.Genres)
	}
	if !strings.HasPrefix(got.Description, "A lamplighter walks") {
		t.Errorf("description = %q", got.Description)
	}
	// The platform publishes a schedule where other sites publish a status.
	// "EVERY MONDAY" is not one of PLAN §7.2's five values and must not be
	// passed through.
	if got.Status != theme.StatusOngoing {
		t.Errorf("status = %q, want the schedule normalised to ongoing", got.Status)
	}
}

// The series page's episode list is paginated ten at a time, so reading it
// gives a short list that looks healthy. The whole list is only on the API.
func TestChaptersUsesTheMobileAPIAndNotTheSeriesPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/v1/webtoon/95/episodes?pageSize=5000": {
			File: "episodes.json", Header: map[string][]string{"Content-Type": {"application/json"}},
		},
	})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Requested("GET", "/en/fantasy/the-lantern-keeper/list") {
		t.Error("the series page was fetched; its episode list is paginated")
	}
	if len(got) != 6 {
		t.Fatalf("got %d chapters, want 6: %+v", len(got), got)
	}

	// The request must have gone to the mobile host, which is where this API
	// lives and which is a different hostname from the configured base.
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	u, err := url.Parse(calls[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "m.example.invalid" {
		t.Errorf("host = %q, want the mobile sibling of the configured base", u.Hostname())
	}
}

// The user-submitted tier is a different path segment on the API, and it is
// the one that needs a language.
func TestCanvasSeriesUseTheOtherAPISegment(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/v1/canvas/1022970/episodes?pageSize=5000&readingLanguageCode=en": {
			File: "episodes.json", Header: map[string][]string{"Content-Type": {"application/json"}},
		},
	})
	th := webtoons.NewWithClock(f, clock)

	if _, err := th.Chapters(context.Background(), site(), "/en/canvas/the-lantern-theater/list?title_no=1022970"); err != nil {
		t.Fatalf("the canvas tier was not addressed the way the API expects: %v", err)
	}
}

// PLAN §7.2's ordering contract. testdata/episodes.json is descending on
// purpose — the live API is not — so this fails if SortAndMark is removed.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/v1/webtoon/95/episodes": {
			File: "episodes.json", Header: map[string][]string{"Content-Type": {"application/json"}},
		},
	})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{1, 2, 3, 4, 5, 6} {
		if got[i].Number != want {
			t.Fatalf("chapter %d number = %v (%q), want %v", i, got[i].Number, got[i].Title, want)
		}
	}
	if !theme.OrderIsKnown(got) {
		t.Error("the list is marked order-unknown; every episode carries a number")
	}
	if got[0].ID != "/en/fantasy/the-lantern-keeper/season-1-ep-1/viewer?title_no=95&episode_no=1" {
		t.Errorf("first chapter = %q, want season 1 episode 1", got[0].ID)
	}
}

// The chapter number comes from the API's episode number, never from the
// title. A number-finding heuristic reads "[Season 2] Ep. 1" as chapter 2 and
// would sort it before season 1 episode 3 — a volume that reads out of order.
func TestChapterNumberIsTheEpisodeNumberNotTheSeason(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/v1/webtoon/95/episodes": {
			File: "episodes.json", Header: map[string][]string{"Content-Type": {"application/json"}},
		},
	})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}

	// "[Season 2] Ep. 1" is episode 5 of the series, not chapter 2.
	var seasonTwoEpOne theme.Chapter
	for _, c := range got {
		if c.Title == "[Season 2] Ep. 1" {
			seasonTwoEpOne = c
		}
	}
	if seasonTwoEpOne.ID == "" {
		t.Fatal("the padded title was not collapsed")
	}
	if seasonTwoEpOne.Number != 5 {
		t.Errorf("number = %v, want 5: the title's leading number is the season", seasonTwoEpOne.Number)
	}
	if seasonTwoEpOne.Volume != "Season 2" {
		t.Errorf("volume = %q, want the season as the volume label", seasonTwoEpOne.Volume)
	}

	// An episode with no season in its title has no volume, and one with no
	// exposure date has the zero time rather than an error.
	for _, c := range got {
		if c.Title == "The Harbour Road" {
			if c.Volume != "" {
				t.Errorf("volume = %q, want none: that title names no season", c.Volume)
			}
			if !c.Published.IsZero() {
				t.Errorf("published = %v, want the zero time", c.Published)
			}
		}
	}
}

// The real URL is in data-url; src is a placeholder until the reader scrolls.
func TestPagesReadTheLazyAttributeAndNotSrc(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + chapterID: {File: "viewer.html"},
	})
	th := webtoons.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), chapterID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://pages.example.invalid/20260907_11/lantern_0006_001.jpg?type=q90",
		"https://pages.example.invalid/20260907_11/lantern_0006_002.jpg?type=q90",
		"https://pages.example.invalid/20260907_11/lantern_0006_003.jpg?type=q90",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d (the spacer <img> must be skipped): %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, u := range got {
		if strings.HasPrefix(u, "data:") {
			t.Errorf("page %q is the placeholder from src", u)
		}
	}
}

// The quality override drops the rendition parameter, and only when asked.
func TestFullQualityImagesDropsTheRenditionParameter(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + chapterID: {File: "viewer.html"},
	})
	th := webtoons.NewWithClock(f, clock)

	src := site()
	src.Overrides = map[string]any{"fullQualityImages": true}
	got, err := th.Pages(context.Background(), src, chapterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range got {
		if strings.Contains(u, "type=") {
			t.Errorf("page %q still carries a rendition parameter", u)
		}
	}
	if got[0] != "https://pages.example.invalid/20260907_11/lantern_0006_001.jpg" {
		t.Errorf("page 0 = %q", got[0])
	}
}

// PLAN §7.6: the Referer must name a page this theme actually fetched. Here
// that is the viewer page Pages() requests, built by the same call.
func TestPageRefererNamesTheViewerPageThatWasFetched(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + chapterID: {File: "viewer.html"},
	})
	th := webtoons.NewWithClock(f, clock)
	src := site()

	if _, err := th.Pages(context.Background(), src, chapterID); err != nil {
		t.Fatal(err)
	}

	var pr theme.PageReferrer = th
	got := pr.PageReferer(src, chapterID)
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if got != calls[0].URL {
		t.Errorf("PageReferer = %q, but Pages fetched %q; the header would name a page we did not read", got, calls[0].URL)
	}
}

// A chapter ID that does not resolve yields no header rather than a guess.
func TestPageRefererIsEmptyWhenThereIsNothingToName(t *testing.T) {
	th := webtoons.New(nil)
	src := site()
	src.BaseURL = "://not a url"
	if got := th.PageReferer(src, chapterID); got != "" {
		t.Errorf("PageReferer = %q, want empty: a fabricated Referer is what PLAN §7.6 forbids", got)
	}
}

// PLAN §7.2: an image host this theme extracts from and does not declare fails
// §7.5 stage 5 rather than surfacing later at download.
func TestDeclaredHostsAreExactAndNarrow(t *testing.T) {
	hosts := webtoons.New(nil).AllowedHosts()
	if len(hosts) == 0 {
		t.Fatal("no hosts declared; every page image here is off-domain")
	}
	for _, h := range hosts {
		// No wildcard: the parent domain carries a great deal that has nothing
		// to do with comics, so `*.` there would widen the boundary far past
		// what this theme needs.
		if strings.HasPrefix(h, "*.") {
			t.Errorf("host %q is a wildcard on a domain that serves much more than images", h)
		}
		if strings.Count(h, ".") < 2 {
			t.Errorf("host %q is too broad", h)
		}
	}
}

func TestSuggestedNameIsThePublisherName(t *testing.T) {
	if got := webtoons.New(nil).SuggestedName(); got == "" {
		t.Error("no suggested name; a single-publisher theme has one to offer")
	}
}

func TestDiscoveryOnlyIsAvailable(t *testing.T) {
	var th theme.Theme = webtoons.New(nil)
	d, ok := th.(theme.DiscoveryClassifier)
	if !ok {
		t.Fatal("theme does not implement DiscoveryClassifier")
	}
	if d.DiscoveryOnly().ID() != webtoons.ID {
		t.Error("the discovery-only copy is a different theme")
	}
}

func TestFingerprintClearsTheThresholdOnEveryFixture(t *testing.T) {
	const confident = 60
	for _, file := range []string{"home.html", "search.html", "series.html", "viewer.html"} {
		t.Run(file, func(t *testing.T) {
			score := webtoons.New(nil).Fingerprint(pageFromFile(t, file))
			if score < confident {
				t.Errorf("score = %d, want at least %d", score, confident)
			}
			t.Logf("score = %d", score)
		})
	}
}

func TestFingerprintIsZeroedByAnotherThemesMarker(t *testing.T) {
	th := webtoons.New(nil)
	base := pageFromFile(t, "home.html")
	if th.Fingerprint(base) < 60 {
		t.Fatal("the unmodified home page does not score; the rest of this test proves nothing")
	}
	for _, marker := range []string{
		`<link rel="stylesheet" href="/wp-content/plugins/madara/css/style.css">`,
		`<script>ts_reader.run({"sources":[]});</script>`,
		`<div id="chapter-list-container" data-chapter-url-template="/x/{slug}"></div>`,
		`<div x-data="{ n: checkNewChapter('2026-01-01') }"></div>`,
	} {
		t.Run(marker[:20], func(t *testing.T) {
			poisoned := append(append([]byte(nil), base.Body...), []byte(marker)...)
			u, _ := url.Parse("https://www.example.invalid/")
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
	u, err := url.Parse("https://www.example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	return probe.NewPage(u, nil, 200, nil, b)
}
