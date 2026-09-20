package mangathemesia_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/mangathemesia"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

var fixedNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixedNow }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-04", Name: "Example Scans", Lang: "en",
		Theme: mangathemesia.ID, BaseURL: "https://example.invalid",
		AddedAt: fixedNow,
	}
}

func TestSearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /": {File: "search.html"},
	})
	th := mangathemesia.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "lantern", 1)
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.SeriesStub{
		{
			// The title comes from the anchor attribute, not the .tt, so the
			// "HOT" badge does not end up in it.
			ID:       "/manga/the-lantern-keeper/",
			Title:    "The Lantern Keeper",
			CoverURL: "https://example.invalid/wp-content/uploads/2026/01/lantern-keeper.jpg",
		},
		{
			// The older .utao/.uta/.imgu card markup.
			ID:       "/manga/salt-and-cedar/",
			Title:    "Salt and Cedar",
			CoverURL: "https://example.invalid/wp-content/uploads/2026/02/salt-cedar.jpg",
		},
		{
			// No title attribute, so the .tt is used; cover from a srcset.
			ID:       "/manga/paper-lanterns-of-the-ninth-ward/",
			Title:    "Paper Lanterns of the Ninth Ward",
			CoverURL: "https://example.invalid/wp-content/uploads/2026/03/ninth-ward.jpg",
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

func TestSearchDoesNotFilterOnPostType(t *testing.T) {
	// Unlike madara, this theme's search is the stock WordPress one. Sending a
	// post_type filter it does not implement would silently narrow results.
	f := themetest.New(t, map[string]themetest.Route{"GET /": {File: "search.html"}})
	th := mangathemesia.NewWithClock(f, clock)
	if _, err := th.Search(context.Background(), site(), "lantern", 1); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if strings.Contains(calls[0].URL, "post_type") {
		t.Errorf("search URL %q carries a post_type filter this theme does not use", calls[0].URL)
	}
	if !strings.Contains(calls[0].URL, "s=lantern") {
		t.Errorf("search URL %q has no query", calls[0].URL)
	}
}

func TestSeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
	})
	th := mangathemesia.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), "/manga/the-lantern-keeper/")
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"Title", got.Title, "The Lantern Keeper"},
		{"CoverURL", got.CoverURL, "https://example.invalid/wp-content/uploads/2026/01/lantern-keeper.jpg"},
		{"Status", got.Status, theme.StatusOngoing},
		{"Authors", strings.Join(got.Authors, "|"), "E. Marchetti|M. Ferrer"},
		// The Artist row is the theme's "-" placeholder; it must yield nothing
		// rather than an artist literally called "-".
		{"Artists", strings.Join(got.Artists, "|"), ""},
		{"Genres", strings.Join(got.Genres, "|"), "Fantasy|Slice of Life"},
		{"AltTitles", strings.Join(got.AltTitles, "|"), "Rōtō no Bannin|The Keeper of Lanterns"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
	if !strings.HasPrefix(got.Description, "Every dusk, Mira walks") {
		t.Errorf("Description = %q", got.Description)
	}
}

// TestChaptersComeFromTheSeriesHTML is the structural difference from madara
// stated as a test: one GET, no AJAX, no post ID.
func TestChaptersComeFromTheSeriesHTML(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
	})
	th := mangathemesia.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), "/manga/the-lantern-keeper/")
	if err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d requests, want exactly 1: %+v", len(calls), calls)
	}
	if calls[0].Method != "GET" {
		t.Errorf("method = %s, want GET; this theme has no AJAX chapter endpoint", calls[0].Method)
	}

	// PLAN §7.2: ascending reading order. series.html lists 4, 3.5, 3, 1 —
	// newest first, as this theme's #chapterlist renders it — and the theme
	// returns the reverse. The decorative-title case is the interesting one to
	// have here: "Final Lamp" is chapter 1, and only data-num says so, which
	// is exactly the kind of identifier a generic sort at the call site would
	// have had no way to read.
	want := []struct {
		id        string
		title     string
		number    float64
		published time.Time
	}{
		// The display title is decorative; data-num carries the real number.
		{"/the-lantern-keeper-chapter-1/", "Final Lamp", 1, time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)},
		{"/the-lantern-keeper-chapter-3/", "Chapter 3", 3, time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)},
		{"/the-lantern-keeper-chapter-3-5/", "Chapter 3.5 Interlude", 3.5, time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)},
		{"/the-lantern-keeper-chapter-4/", "Chapter 4", 4, time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d chapters, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].ID != w.id {
			t.Errorf("chapter %d ID = %q, want %q", i, got[i].ID, w.id)
		}
		if got[i].Title != w.title {
			t.Errorf("chapter %d Title = %q, want %q", i, got[i].Title, w.title)
		}
		if got[i].Number != w.number {
			t.Errorf("chapter %d Number = %v, want %v", i, got[i].Number, w.number)
		}
		if !got[i].Published.Equal(w.published) {
			t.Errorf("chapter %d Published = %v, want %v", i, got[i].Published, w.published)
		}
	}
}

// TestPagesComeFromTheInlineBlob is the other structural difference: the DOM
// holds a truncated list, the ts_reader blob holds the real one.
func TestPagesComeFromTheInlineBlob(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /the-lantern-keeper-chapter-4/": {File: "reader.html"},
	})
	th := mangathemesia.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/the-lantern-keeper-chapter-4/")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"https://cdn.example.invalid/pages/lantern-keeper/4/001.jpg",
		"https://cdn.example.invalid/pages/lantern-keeper/4/002.jpg",
		"https://cdn.example.invalid/pages/lantern-keeper/4/003.jpg",
		"https://cdn.example.invalid/pages/lantern-keeper/4/004.jpg",
		// A site-relative entry in the blob resolves against baseUrl.
		"https://example.invalid/wp-content/uploads/pages/lantern-keeper/4/005.jpg",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d — the DOM only holds 2, so this is the blob test failing: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d = %q, want %q", i, got[i], want[i])
		}
	}
	// The mirror source must not have been used.
	for _, u := range got {
		if strings.Contains(u, "mirror") {
			t.Errorf("page %q came from the mirror source, not the site's own", u)
		}
	}
}

func TestPagesFallBackToTheDOM(t *testing.T) {
	tests := []struct {
		name       string
		pageSource any
	}{
		{name: "explicitly configured for dom", pageSource: "dom"},
		{name: "default, but the page carries no blob", pageSource: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /salt-and-cedar-chapter-2/": {File: "reader-server-rendered.html"},
			})
			th := mangathemesia.NewWithClock(f, clock)
			src := site()
			if tc.pageSource != nil {
				src.Overrides = map[string]any{"pageSource": tc.pageSource}
			}

			got, err := th.Pages(context.Background(), src, "/salt-and-cedar-chapter-2/")
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"https://cdn.example.invalid/pages/salt-cedar/2/001.jpg",
				// data-src wins over the loading.gif in src.
				"https://cdn.example.invalid/pages/salt-cedar/2/002.jpg",
				"https://example.invalid/wp-content/uploads/pages/salt-cedar/2/003.jpg",
			}
			if len(got) != len(want) {
				t.Fatalf("got %d pages, want %d: %v", len(got), len(want), got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("page %d = %q, want %q", i, got[i], want[i])
				}
			}
		})
	}
}

// TestEndToEnd is PLAN §6 M2's acceptance path for the second theme.
func TestEndToEnd(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":                              {File: "search.html"},
		"GET /manga/the-lantern-keeper/":     {File: "series.html"},
		"GET /the-lantern-keeper-chapter-4/": {File: "reader.html"},
	})
	th := mangathemesia.NewWithClock(f, clock)
	ctx := context.Background()
	src := site()

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
	// The reader fixture is chapter 4, the newest — and therefore the *last*
	// entry now that the list is in ascending reading order (PLAN §7.2).
	pages, err := th.Pages(ctx, src, chapters[len(chapters)-1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 5 {
		t.Fatalf("got %d page URLs, want 5", len(pages))
	}
}

// TestSecondSiteNeedsOnlyAConfigEntry is the M2 acceptance criterion applied
// to this theme too: a differently-configured site, from JSON alone.
func TestSecondSiteNeedsOnlyAConfigEntry(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
		"GET /komik/the-lantern-keeper/": {File: "series.html"},
	})
	th := mangathemesia.NewWithClock(f, clock)

	reg := theme.NewRegistry()
	if err := reg.Register(th); err != nil {
		t.Fatal(err)
	}

	configs := []struct {
		name     string
		json     string
		seriesID string
	}{
		{
			name: "defaults",
			json: `{"id":"user-added-04","name":"Example Scans","lang":"en",
			        "theme":"mangathemesia","baseUrl":"https://example.invalid",
			        "addedAt":"2026-03-04T12:00:00Z"}`,
			seriesID: "the-lantern-keeper",
		},
		{
			name: "renamed path segment and a narrower rate limit",
			json: `{"id":"user-added-05","name":"Another Scans","lang":"id",
			        "theme":"mangathemesia","baseUrl":"https://example.invalid",
			        "overrides":{"seriesSubPath":"komik","pageSource":"dom"},
			        "rateLimit":{"requestsPerMinute":6,"concurrency":1},
			        "addedAt":"2026-03-04T12:00:00Z"}`,
			seriesID: "the-lantern-keeper",
		},
	}

	for _, c := range configs {
		t.Run(c.name, func(t *testing.T) {
			var src theme.Source
			if err := json.Unmarshal([]byte(c.json), &src); err != nil {
				t.Fatal(err)
			}
			if err := reg.Validate(&src); err != nil {
				t.Fatalf("config rejected: %v", err)
			}
			// A bare slug, not a path: the theme must build the URL from
			// seriesSubPath alone.
			chapters, err := th.Chapters(context.Background(), &src, c.seriesID)
			if err != nil {
				t.Fatal(err)
			}
			if len(chapters) != 4 {
				t.Fatalf("got %d chapters, want 4", len(chapters))
			}
		})
	}
}

// PLAN §7.2, 2026-09-15: ascending reading order. series.html lists 4, 3.5, 3,
// 1 — this theme's #chapterlist renders newest-first, exactly as madara's does.
//
// The decorative-title chapter is what makes this more than a reversal test:
// "Final Lamp" is chapter 1 and only data-num says so. A caller sorting
// generically on what it could see would have had nothing to sort by.
func TestChaptersAreAscending(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
	})
	th := mangathemesia.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), "/manga/the-lantern-keeper/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("got %d chapters; the fixture has four", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Number < got[i-1].Number {
			t.Fatalf("chapter %d (%v) comes after %d (%v); the list is still in the "+
				"site's newest-first order", i, got[i].Number, i-1, got[i-1].Number)
		}
	}
	if got[0].Title != "Final Lamp" {
		t.Errorf("first chapter is %q, want the decoratively titled chapter 1", got[0].Title)
	}
	if !theme.OrderIsKnown(got) {
		t.Error("a fully numbered list was reported as having an unknown order")
	}
}
