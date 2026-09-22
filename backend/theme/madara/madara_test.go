package madara_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test in this package runs entirely against committed fixtures. The
// resolver is disabled so that a future "just point it at a real site to
// check" is a red test rather than a quiet network call (PLAN §6 M2).
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

// fixedNow pins the clock so the relative-date assertions ("2 days ago") are
// exact rather than approximately right.
var fixedNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixedNow }

// siteA is the default-shaped source: everything on defaults, chapters over
// the current AJAX endpoint.
func siteA() *theme.Source {
	return &theme.Source{
		ID: "user-added-01", Name: "Example Reader", Lang: "en",
		Theme: madara.ID, BaseURL: "https://example.invalid",
		AddedAt: fixedNow,
	}
}

// The failure the real prober found on 2026-09-16: a live madara install
// fingerprinted at 100 and then searched to **zero results**.
//
// The user had typed the apex; the site redirects to its www host and renders
// absolute links there; and the host filter compared exactly, so every result
// was discarded as off-site. Not an error — an empty list, which reads as "this
// site has nothing".
//
// testdata/search.html now puts two of its three results on the www sibling,
// so this is a property of the corpus and not only of this test. See
// backend/theme/site.go.
func TestSearchFindsResultsWhenTheSiteRedirectsToWWW(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /?post_type=wp-manga&s=lantern": {File: "search.html"},
	})
	th := madara.NewWithClock(f, clock)

	// The base is the apex, as a user would paste it.
	src := siteA()
	src.BaseURL = "https://example.invalid"

	got, err := th.Search(context.Background(), src, "lantern", 1)
	if err != nil {
		t.Fatal(err)
	}
	// Two of the three results are on the www sibling. An exact host filter
	// leaves the one relative link and drops the rest, which is the shape of
	// the live failure: not an error, a short list.
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3: links on the site's www host were discarded "+
			"as off-site, which is what a live install that redirects its apex produces: %+v", len(got), got)
	}
	var onWWW int
	for _, r := range got {
		if !strings.HasPrefix(r.ID, "/") {
			t.Errorf("result ID %q is not site-relative", r.ID)
		}
		if strings.Contains(r.ID, "://") {
			t.Errorf("result ID %q kept a host", r.ID)
		}
	}
	// And the fixture really does exercise it.
	b, err := os.ReadFile("testdata/search.html")
	if err != nil {
		t.Fatal(err)
	}
	onWWW = strings.Count(string(b), `href="https://www.example.invalid/manga/`)
	if onWWW == 0 {
		t.Error("the fixture no longer puts any result on the www sibling; this test proves nothing")
	}
}

func TestSearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /": {File: "search.html"},
	})
	th := madara.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), siteA(), "lantern", 1)
	if err != nil {
		t.Fatal(err)
	}

	// The search page just fetched — the exact URL, per PLAN §7.6 — is the
	// referrer every cover in this result set must carry.
	const referrer = "https://example.invalid/?post_type=wp-manga&s=lantern"
	want := []theme.SeriesStub{
		{
			ID:            "/manga/the-lantern-keeper/",
			Title:         "The Lantern Keeper",
			CoverURL:      "https://example.invalid/wp-content/uploads/2026/01/lantern-keeper-193x278.jpg",
			CoverReferrer: referrer,
		},
		{
			// Whitespace and a newline inside the anchor must be collapsed.
			ID:            "/manga/salt-and-cedar/",
			Title:         "Salt and Cedar",
			CoverURL:      "https://example.invalid/wp-content/uploads/2026/02/salt-cedar-193x278.jpg",
			CoverReferrer: referrer,
		},
		{
			// <h4> skin variation, and a srcset rather than a src.
			ID:            "/manga/paper-lanterns-of-the-ninth-ward/",
			Title:         "Paper Lanterns of the Ninth Ward",
			CoverURL:      "https://example.invalid/wp-content/uploads/2026/03/ninth-ward-193x278.jpg",
			CoverReferrer: referrer,
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// The failure the real prober found on 2026-09-21: two live madara installs
// fingerprinted confidently and then searched to zero results, because their
// series links sit under a path segment ("/serie/", "/porncomic/") that is
// neither the "manga" default nor a documented common rename, and no
// override exists yet to tell a fresh probe what it is. Search must work
// this out from the response itself rather than from a preconfigured guess.
func TestSearchFindsResultsWhenTheSubPathIsRenamedWithNoOverrideSet(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /": {File: "search-renamed-subpath.html"},
	})
	th := madara.NewWithClock(f, clock)

	// siteA carries no mangaSubPath override, so the resolved default is
	// "manga" — which does not appear anywhere in this fixture.
	got, err := th.Search(context.Background(), siteA(), "lantern", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/toons/the-lantern-keeper/",
		"/toons/salt-and-cedar/",
		"/toons/paper-lanterns-of-the-ninth-ward/",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("result %d ID = %q, want %q", i, got[i].ID, id)
		}
	}
	// The taxonomy decoy under the sibling segment "/toons-genre/" must lose
	// the vote and never appear.
	for _, r := range got {
		if strings.HasPrefix(r.ID, "/toons-genre/") {
			t.Errorf("taxonomy decoy leaked into results: %+v", r)
		}
	}
}

func TestSearchBuildsTheWordPressQuery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "search.html"},
		"GET /page/3/": {File: "search.html"},
	})
	th := madara.NewWithClock(f, clock)

	if _, err := th.Search(context.Background(), siteA(), "lantern", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Search(context.Background(), siteA(), "lantern", 3); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	for _, want := range []string{"s=lantern", "post_type=wp-manga"} {
		if !strings.Contains(calls[0].URL, want) {
			t.Errorf("page 1 URL %q does not contain %q", calls[0].URL, want)
		}
	}
	// Page 1 must not use /page/1/; WordPress serves that as a redirect.
	if strings.Contains(calls[0].URL, "/page/") {
		t.Errorf("page 1 URL %q should not be paginated", calls[0].URL)
	}
	if !strings.Contains(calls[1].URL, "/page/3/") {
		t.Errorf("page 3 URL %q is not paginated", calls[1].URL)
	}
}

func TestSeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
	})
	th := madara.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), siteA(), "/manga/the-lantern-keeper/")
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"ID", got.ID, "/manga/the-lantern-keeper/"},
		{"Title", got.Title, "The Lantern Keeper"},
		{"CoverURL", got.CoverURL, "https://example.invalid/wp-content/uploads/2026/01/lantern-keeper-193x278.jpg"},
		{"CoverReferrer", got.CoverReferrer, "https://example.invalid/manga/the-lantern-keeper/"},
		{"Status", got.Status, theme.StatusOngoing},
		{"Authors", strings.Join(got.Authors, "|"), "E. Marchetti"},
		{"Artists", strings.Join(got.Artists, "|"), "E. Marchetti|Studio Quay"},
		{"Genres", strings.Join(got.Genres, "|"), "Fantasy|Slice of Life"},
		{"AltTitles", strings.Join(got.AltTitles, "|"), "Rōtō no Bannin|The Keeper of Lanterns"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
	if !strings.HasPrefix(got.Description, "Every dusk, Mira walks") {
		t.Errorf("Description = %q, want it to start with the summary text", got.Description)
	}
	// The summary is one collapsed line, not the source's indentation.
	if strings.Contains(got.Description, "\n") {
		t.Errorf("Description contains a newline: %q", got.Description)
	}
}

func TestChaptersOverTheCurrentAjaxEndpoint(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"POST /manga/the-lantern-keeper/ajax/chapters/": {File: "chapters-ajax.html"},
	})
	th := madara.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), siteA(), "/manga/the-lantern-keeper/")
	if err != nil {
		t.Fatal(err)
	}

	// The series page must NOT have been fetched: the whole point of the
	// current endpoint is that it needs no post ID.
	if f.Requested("GET", "/manga/the-lantern-keeper/") {
		t.Error("current-shape chapters fetched the series page unnecessarily")
	}

	// PLAN §7.2: ascending reading order, earliest first — which is the
	// *reverse* of what the fixture contains. chapters-ajax.html lists 4, 3.5,
	// 3, 1 because that is what madara's markup does, and this list is the
	// answer the theme has to produce from it. Before the normalisation landed
	// (2026-09-15) this test expected the fixture's own order, and M5 assembled
	// a volume called "The Lantern Keeper — 4–1".
	want := []struct {
		id        string
		number    float64
		published time.Time
	}{
		// "Chapter 2 (coming soon)" has no href and must be skipped entirely.
		{"/manga/the-lantern-keeper/chapter-1/", 1, time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)},
		{"/manga/the-lantern-keeper/chapter-3/", 3, time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)},
		{"/manga/the-lantern-keeper/chapter-3-5/", 3.5, time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)},
		// The <a title="March 2, 2026"> wins over the "2 days ago" text.
		{"/manga/the-lantern-keeper/chapter-4/", 4, time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d chapters, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].ID != w.id {
			t.Errorf("chapter %d ID = %q, want %q", i, got[i].ID, w.id)
		}
		if got[i].Number != w.number {
			t.Errorf("chapter %d Number = %v, want %v", i, got[i].Number, w.number)
		}
		if !got[i].Published.Equal(w.published) {
			t.Errorf("chapter %d Published = %v, want %v", i, got[i].Published, w.published)
		}
	}
}

func TestChaptersOverTheLegacyAdminAjaxEndpoint(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /comics/the-lantern-keeper/": {File: "series.html"},
		"POST /wp-admin/admin-ajax.php":   {File: "chapters-admin-ajax.html"},
	})
	th := madara.NewWithClock(f, clock)

	// A third site, configured entirely from JSON: a renamed path segment and
	// the legacy AJAX shape.
	src := &theme.Source{
		ID: "user-added-03", Name: "Third Example", Lang: "en",
		Theme: madara.ID, BaseURL: "https://example.invalid",
		Overrides: map[string]any{
			"mangaSubPath": "comics",
			"ajaxStyle":    "legacy",
		},
		AddedAt: fixedNow,
	}

	got, err := th.Chapters(context.Background(), src, "/comics/the-lantern-keeper/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d chapters, want 2: %+v", len(got), got)
	}
	// Ascending (PLAN §7.2). chapters-admin-ajax.html lists 2 then 1, as
	// madara's markup does; the theme hands back 1 then 2.
	if got[0].ID != "/comics/the-lantern-keeper/chapter-1/" {
		t.Errorf("chapter 0 ID = %q, want chapter-1 first", got[0].ID)
	}
	if got[1].ID != "/comics/the-lantern-keeper/chapter-2/" {
		t.Errorf("chapter 1 ID = %q, want chapter-2 second", got[1].ID)
	}

	// The legacy shape needs the WordPress post ID off the series page, and
	// must send it as manga_get_chapters.
	var posted bool
	for _, c := range f.Calls() {
		if c.Method == "POST" && strings.Contains(c.URL, "admin-ajax.php") {
			posted = true
			if c.Form.Get("action") != "manga_get_chapters" {
				t.Errorf("action = %q, want manga_get_chapters", c.Form.Get("action"))
			}
			if c.Form.Get("manga") != "4271" {
				t.Errorf("manga = %q, want the post ID 4271 from the series page", c.Form.Get("manga"))
			}
		}
	}
	if !posted {
		t.Error("legacy shape never POSTed to admin-ajax.php")
	}
}

func TestChaptersFallBackToTheSeriesHTML(t *testing.T) {
	// A site whose AJAX endpoint 404s — which is how a site of the inline
	// shape answers — must still yield chapters, with no override needed.
	f := themetest.New(t, map[string]themetest.Route{
		"POST /series/salt-and-cedar/ajax/chapters/": {Status: 404, Body: "0"},
		"GET /series/salt-and-cedar/":                {File: "series-inline-chapters.html"},
	})
	th := madara.NewWithClock(f, clock)

	src := siteA()
	src.Overrides = map[string]any{"mangaSubPath": "series"}

	got, err := th.Chapters(context.Background(), src, "/series/salt-and-cedar/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d chapters, want 2: %+v", len(got), got)
	}
	// "3 days ago" resolves against the injected clock. It belongs to the
	// newest chapter, which is now *last* — the list is ascending (PLAN §7.2),
	// so indexing from the front would be reading the wrong one.
	want := fixedNow.AddDate(0, 0, -3)
	newest := got[len(got)-1]
	if !newest.Published.Equal(want) {
		t.Errorf("relative date = %v, want %v", newest.Published, want)
	}
	// And the ordering itself, so this test would also have caught the bug.
	if got[0].Number >= newest.Number {
		t.Errorf("chapters are not ascending: %v then %v", got[0].Number, newest.Number)
	}
}

func TestPages(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/chapter-4/": {File: "reader.html"},
	})
	th := madara.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), siteA(), "/manga/the-lantern-keeper/chapter-4/")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		// data-src wins over the base64 placeholder in src, and is trimmed.
		"https://cdn.example.invalid/pages/lantern-keeper/4/001.jpg",
		// a plain src is used when there is no lazy attribute
		"https://cdn.example.invalid/pages/lantern-keeper/4/002.jpg",
		// data-lazy-src wins over a real-looking loading.gif in src
		"https://cdn.example.invalid/pages/lantern-keeper/4/003.jpg",
		// a protocol-relative URL resolves against the source's scheme
		"https://cdn.example.invalid/pages/lantern-keeper/4/004.jpg",
		// a site-relative path resolves against baseUrl
		"https://example.invalid/wp-content/uploads/pages/lantern-keeper/4/005.jpg",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEndToEnd is PLAN §6 M2's acceptance path in one test: search, then the
// series it found, then that series' chapters, then that chapter's page URLs.
func TestEndToEnd(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":                          {File: "search.html"},
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
		"POST /manga/the-lantern-keeper/ajax/chapters/": {File: "chapters-ajax.html"},
		"GET /manga/the-lantern-keeper/chapter-4/":      {File: "reader.html"},
	})
	th := madara.NewWithClock(f, clock)
	ctx := context.Background()
	src := siteA()

	results, err := th.Search(ctx, src, "lantern", 1)
	if err != nil || len(results) == 0 {
		t.Fatalf("search: %v (%d results)", err, len(results))
	}
	series, err := th.Series(ctx, src, results[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if series.Title != "The Lantern Keeper" {
		t.Fatalf("series title = %q", series.Title)
	}
	chapters, err := th.Chapters(ctx, src, series.ID)
	if err != nil || len(chapters) == 0 {
		t.Fatalf("chapters: %v (%d)", err, len(chapters))
	}
	// The reader fixture is chapter 4, the newest — which is the *last*
	// chapter now that the list is in ascending reading order (PLAN §7.2).
	pages, err := th.Pages(ctx, src, chapters[len(chapters)-1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 5 {
		t.Fatalf("got %d page URLs, want 5", len(pages))
	}
}

// TestThirdSiteNeedsOnlyAConfigEntry is the acceptance criterion of PLAN §6 M2
// stated as a test: "adding a third site to an existing theme requires only a
// config entry and no code".
//
// The theme instance below is the *same* one that served siteA. Only the
// Source differs, and it is built from the JSON a user would paste in.
func TestThirdSiteNeedsOnlyAConfigEntry(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		// Site one: stock paths, AJAX chapter list.
		"GET /manga/the-lantern-keeper/":                {File: "series.html"},
		"POST /manga/the-lantern-keeper/ajax/chapters/": {File: "chapters-ajax.html"},
		// Site two: a renamed segment and the list inline.
		"POST /series/salt-and-cedar/ajax/chapters/": {Status: 404, Body: "0"},
		"GET /series/salt-and-cedar/":                {File: "series-inline-chapters.html"},
		// Site three: a different renamed segment and the legacy endpoint.
		"GET /comics/the-lantern-keeper/": {File: "series.html"},
		"POST /wp-admin/admin-ajax.php":   {File: "chapters-admin-ajax.html"},
	})
	th := madara.NewWithClock(f, clock)

	sites := []struct {
		name         string
		configJSON   string
		seriesID     string
		wantChapters int
	}{
		{
			name: "first site, all defaults",
			configJSON: `{
				"id": "user-added-01", "name": "Example Reader", "lang": "en",
				"theme": "madara", "baseUrl": "https://example.invalid",
				"addedAt": "2026-03-04T12:00:00Z"
			}`,
			seriesID:     "/manga/the-lantern-keeper/",
			wantChapters: 4,
		},
		{
			name: "second site, renamed path segment",
			configJSON: `{
				"id": "user-added-02", "name": "Second Example", "lang": "en",
				"theme": "madara", "baseUrl": "https://example.invalid",
				"overrides": {"mangaSubPath": "series"},
				"addedAt": "2026-03-04T12:00:00Z"
			}`,
			seriesID:     "/series/salt-and-cedar/",
			wantChapters: 2,
		},
		{
			name: "third site, legacy endpoint and another renamed segment",
			configJSON: `{
				"id": "user-added-03", "name": "Third Example", "lang": "en",
				"theme": "madara", "baseUrl": "https://example.invalid",
				"overrides": {"mangaSubPath": "comics", "ajaxStyle": "legacy", "dateFormat": "MMMM d, yyyy"},
				"rateLimit": {"requestsPerMinute": 10, "concurrency": 1},
				"addedAt": "2026-03-04T12:00:00Z"
			}`,
			seriesID:     "/comics/the-lantern-keeper/",
			wantChapters: 2,
		},
	}

	reg := theme.NewRegistry()
	if err := reg.Register(th); err != nil {
		t.Fatal(err)
	}

	for _, site := range sites {
		t.Run(site.name, func(t *testing.T) {
			src, err := decodeSource(site.configJSON)
			if err != nil {
				t.Fatal(err)
			}
			if err := reg.Validate(src); err != nil {
				t.Fatalf("config rejected: %v", err)
			}
			chapters, err := th.Chapters(context.Background(), src, site.seriesID)
			if err != nil {
				t.Fatal(err)
			}
			if len(chapters) != site.wantChapters {
				t.Fatalf("got %d chapters, want %d", len(chapters), site.wantChapters)
			}
		})
	}
}

// PLAN §7.2, 2026-09-15: Chapters returns ascending reading order.
//
// The fixture is adversarial on purpose and always was: madara's markup lists
// chapters newest first, so chapters-ajax.html contains 4, 3.5, 3, 1 and the
// theme has to turn that round. Deleting the theme.SortAndMark call in
// parseChapters makes this fail on the first assertion.
//
// This is the bug M5 found. §6 M4 groups a run of chapters into one volume
// PDF; a descending list produces a PDF that reads backwards, inside a file
// whose reading position xochitl then owns, and nothing downstream notices.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"POST /manga/the-lantern-keeper/ajax/chapters/": {File: "chapters-ajax.html"},
	})
	th := madara.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), siteA(), "/manga/the-lantern-keeper/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("got %d chapters; the fixture has four", len(got))
	}

	for i := 1; i < len(got); i++ {
		if got[i].Number < got[i-1].Number {
			t.Fatalf("chapter %d (%v) comes after %d (%v); the list is descending, "+
				"which is the order madara's markup uses and not the one PLAN §7.2 asks for",
				i, got[i].Number, i-1, got[i-1].Number)
		}
	}
	if got[0].Number != 1 {
		t.Errorf("first chapter is %v, want 1", got[0].Number)
	}
	if last := got[len(got)-1]; last.Number != 4 {
		t.Errorf("last chapter is %v, want 4", last.Number)
	}
	if !theme.OrderIsKnown(got) {
		t.Error("a fully numbered list was reported as having an unknown order")
	}
}
