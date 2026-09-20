package fanfox_test

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
	"github.com/rickl/quire/backend/theme/fanfox"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test here runs offline: the fixtures are synthetic pages on
// example.invalid and DNS is disabled for the package.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func clock() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-08", Name: "Example Comics", Lang: "en",
		Theme: fanfox.ID, BaseURL: "https://example.invalid",
		AddedAt: clock(),
	}
}

const (
	seriesID  = "/manga/the_lantern_keeper/"
	chapterID = "/manga/the_lantern_keeper/v01/c006/1.html"
)

// Each row holds four anchors pointing at four different things. Reading every
// anchor gives four results per series, three of them duplicates and one of
// them not a series at all.
func TestSearchReturnsOneResultPerRow(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search?page=1&stype=1&title=the+lantern+keeper": {File: "search.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "  the lantern keeper  ", 1)
	if err != nil {
		t.Fatalf("the query was not spelled the way the site expects it: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(got), got)
	}
	want := theme.SeriesStub{
		ID:       seriesID,
		Title:    "The Lantern Keeper",
		CoverURL: "https://covers.example.invalid/store/manga/4085/cover.jpg?token=abc&ttl=1789610400&v=1362908853",
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("result 0 = %+v, want %+v", got[0], want)
	}
	// The second row's anchor has no title attribute, so the title has to come
	// from the item-title paragraph.
	if got[1].Title != "Salt and Cinder" {
		t.Errorf("result 1 title = %q", got[1].Title)
	}
	for _, s := range got {
		if !strings.HasPrefix(s.ID, "/manga/") || strings.Count(strings.Trim(s.ID, "/"), "/") != 1 {
			t.Errorf("result ID %q is not a series path", s.ID)
		}
	}
}

// Browse is a search with an empty query (PLAN §7.5, M3 correction 1), and
// this site's search endpoint cannot answer one.
func TestBrowseReadsTheDirectory(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if f.Requested("GET", "/search") {
		t.Error("the search endpoint was asked to answer an empty query")
	}
}

// Two things here are unlike anything else in the registry: directory pages
// are files, and the sort is a valueless parameter that url.Values would
// mangle into "latest=".
func TestDirectoryPagesAreFilesAndTheSortHasNoValue(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /directory/3.html?latest": {File: "directory.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	src := site()
	src.Overrides = map[string]any{"browseOrder": "latest"}
	if _, err := th.Search(context.Background(), src, "", 3); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if !strings.HasSuffix(calls[0].URL, "/directory/3.html?latest") {
		t.Errorf("asked for %q; the page is a file and the sort carries no value", calls[0].URL)
	}
}

func TestSeriesReadsTheInfoBlock(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Lantern Keeper" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Status != theme.StatusCompleted {
		t.Errorf("status = %q", got.Status)
	}
	if len(got.Authors) != 2 {
		t.Errorf("authors = %v, want both", got.Authors)
	}
	if len(got.Genres) != 3 {
		t.Errorf("genres = %v, want 3", got.Genres)
	}
	// The summary is truncated in one paragraph and complete in a hidden one.
	if strings.Contains(got.Description, "...") || !strings.HasSuffix(got.Description, "before she reaches it.") {
		t.Errorf("description = %q, want the complete text and not the truncated one", got.Description)
	}
	if got.CoverURL != "https://covers.example.invalid/store/manga/4085/cover.jpg?token=abc&ttl=1789610400" {
		t.Errorf("cover = %q", got.CoverURL)
	}
}

// PLAN §7.2's ordering contract. testdata/series.html is descending on
// purpose, so this fails if SortAndMark is removed.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"/manga/the_lantern_keeper/v00/c001/1.html",
		"/manga/the_lantern_keeper/v00/c002/1.html",
		"/manga/the_lantern_keeper/v01/c004/1.html",
		"/manga/the_lantern_keeper/v01/c005/1.html",
		"/manga/the_lantern_keeper/v01/c006/1.html",
		"/manga/the_lantern_keeper/v01/c006.5/1.html",
		"/manga/the_lantern_keeper/v02/c007/1.html",
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d chapters, want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("chapter %d = %q, want %q", i, got[i].ID, want)
		}
	}
	if !theme.OrderIsKnown(got) {
		t.Error("the list is marked order-unknown; every chapter is numbered")
	}
	if got[5].Number != 6.5 {
		t.Errorf("number = %v, want the decimal chapter parsed as 6.5", got[5].Number)
	}
}

// This site publishes real volume structure in its URLs, which is what lets
// PLAN §6 M4 group by volume instead of falling back to runs of ten.
func TestVolumeComesFromTheChapterURL(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	wantVols := []string{"0", "0", "1", "1", "1", "1", "2"}
	for i, want := range wantVols {
		if got[i].Volume != want {
			t.Errorf("chapter %d (%s) volume = %q, want %q", i, got[i].ID, got[i].Volume, want)
		}
	}
	// "v00" is a real volume on this site — extras and prologues live there —
	// so trimming its zeros away to "" would lose a group M4 can use.
	if got[0].Volume == "" {
		t.Error("volume 0 was trimmed to nothing; it is a real volume here")
	}
}

func TestChapterDatesParseAbsoluteAndRelative(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET " + seriesID: {File: "series.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), seriesID)
	if err != nil {
		t.Fatal(err)
	}
	// Ascending, so the last is the newest — the one dated "3 days ago".
	newest := got[len(got)-1]
	wantRel := clock().AddDate(0, 0, -3)
	if newest.Published.Year() != wantRel.Year() || newest.Published.YearDay() != wantRel.YearDay() {
		t.Errorf("relative date = %v, want three days before the injected clock (%v)", newest.Published, wantRel)
	}
	// And an absolute one, in the format the dateFormat override describes.
	first := got[0]
	if first.Published.IsZero() {
		t.Error("the absolute date did not parse; the dateFormat default does not match the fixture")
	}
	if y, m, d := first.Published.Date(); y != 2026 || m != time.August || d != 3 {
		t.Errorf("absolute date = %v, want 2026-08-03", first.Published)
	}
}

// The desktop reader hands out one page at a time behind an obfuscated script.
// The mobile scroll view ships every URL, and reaching it means changing host
// *and* rewriting one path segment.
func TestPagesUseTheMobileScrollReader(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /roll_manga/the_lantern_keeper/v01/c006/1.html": {File: "reader.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), chapterID)
	if err != nil {
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
	if u.Hostname() != "m.example.invalid" {
		t.Errorf("host = %q, want the mobile sibling", u.Hostname())
	}

	want := []string{
		"https://pages.example.invalid/store/manga/4085/01-006.0/compressed/f000.jpg?token=abc&ttl=1789560000",
		"https://pages.example.invalid/store/manga/4085/01-006.0/compressed/f001.jpg?token=def&ttl=1789560000",
		"https://pages.example.invalid/store/manga/4085/01-006.0/compressed/f002.jpg?token=ghi&ttl=1789560000",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, u := range got {
		// src holds a loading animation; reading it returns a chapter of
		// spinners. The protocol-relative URLs must also come back as https
		// rather than downgrading.
		if strings.Contains(u, "Loading") {
			t.Errorf("page %q is the placeholder from src", u)
		}
		if !strings.HasPrefix(u, "https://") {
			t.Errorf("page %q did not inherit the base scheme", u)
		}
	}
}

// A licensed series answers 200 with a notice where the images should be.
// Reporting that as an empty chapter reads as a Quire bug rather than the
// site's decision — and it is not a challenge either, so it must not be called
// one.
func TestALicensedSeriesSaysSoRatherThanReturningNothing(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /roll_manga/salt_and_cinder/v01/c006/1.html": {File: "reader-licensed.html"},
	})
	th := fanfox.NewWithClock(f, clock)

	_, err := th.Pages(context.Background(), site(), "/manga/salt_and_cinder/v01/c006/1.html")
	if err == nil {
		t.Fatal("Pages returned no error for a chapter the site serves no images for")
	}
	if !strings.Contains(err.Error(), "licensed") {
		t.Errorf("error %q does not say why; the user cannot tell this from a bug", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "challenge") {
		t.Errorf("error %q calls this a challenge; it is a 200 and an honest no", err)
	}
}

// PLAN §7.6: the Referer must name a page this theme actually fetched — here
// the mobile reader page, built by the same call Pages() uses.
func TestPageRefererNamesTheReaderPageThatWasFetched(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /roll_manga/the_lantern_keeper/v01/c006/1.html": {File: "reader.html"},
	})
	th := fanfox.NewWithClock(f, clock)
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
	th := fanfox.New(nil)
	if got := th.PageReferer(site(), "/manga/the_lantern_keeper/"); got != "" {
		t.Errorf("PageReferer = %q for a series id, want empty", got)
	}
}

func TestEveryDeclaredHostIsANarrowWildcard(t *testing.T) {
	hosts := fanfox.New(nil).AllowedHosts()
	if len(hosts) == 0 {
		t.Fatal("no hosts declared; pages and covers are both off-domain here")
	}
	for _, h := range hosts {
		if !strings.HasPrefix(h, "*.") {
			t.Errorf("host %q is not the subdomain-only form; the apexes serve nothing", h)
		}
		if strings.Count(h, ".") < 2 {
			t.Errorf("host %q is too broad", h)
		}
	}
}

func TestSuggestedNameIsTheSiteName(t *testing.T) {
	if got := fanfox.New(nil).SuggestedName(); got == "" {
		t.Error("no suggested name; a single-site theme has one to offer")
	}
}

func TestDiscoveryOnlyIsAvailable(t *testing.T) {
	var th theme.Theme = fanfox.New(nil)
	d, ok := th.(theme.DiscoveryClassifier)
	if !ok {
		t.Fatal("theme does not implement DiscoveryClassifier")
	}
	if d.DiscoveryOnly().ID() != fanfox.ID {
		t.Error("the discovery-only copy is a different theme")
	}
}

func TestFingerprintClearsTheThresholdOnEveryFixture(t *testing.T) {
	const confident = 60
	for _, file := range []string{"home.html", "search.html", "series.html", "reader.html", "directory.html"} {
		t.Run(file, func(t *testing.T) {
			score := fanfox.New(nil).Fingerprint(pageFromFile(t, file))
			if score < confident {
				t.Errorf("score = %d, want at least %d", score, confident)
			}
			t.Logf("score = %d", score)
		})
	}
}

func TestFingerprintIsZeroedByAnotherThemesMarker(t *testing.T) {
	th := fanfox.New(nil)
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
