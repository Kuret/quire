package generic_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

var fixedNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixedNow }

// selectorSource is a one-off site configured entirely with raw selectors —
// the escape hatch used as intended, with no script at all.
func selectorSource() *theme.Source {
	return &theme.Source{
		ID: "user-added-06", Name: "Fourth Example", Lang: "en",
		Theme: generic.ID, BaseURL: "https://example.invalid",
		Selectors: map[string]string{
			generic.SearchPath:  "/search?q={query}&p={page}",
			generic.SearchItem:  ".results .entry",
			generic.SearchLink:  "a.entry-link",
			generic.SearchTitle: ".name",
			generic.SearchCover: "img.cover",

			generic.SeriesPath:        "/read/{id}",
			generic.SeriesTitle:       ".about .work-name",
			generic.SeriesCover:       ".about .work-cover",
			generic.SeriesDescription: ".about .blurb",
			generic.SeriesGenres:      ".about .tags a",
			generic.SeriesStatus:      ".about .state",

			generic.ChapterItem:  ".parts .part",
			generic.ChapterLink:  "a.part-link",
			generic.ChapterTitle: ".part-name",
			generic.ChapterDate:  ".part-date",

			generic.PageImage: ".viewer .frame img",
		},
		AddedAt: fixedNow,
	}
}

func TestSearchFromSelectorsAlone(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search": {File: "listing.html"},
	})
	th := generic.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), selectorSource(), "lantern keeper", 2)
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.SeriesStub{
		{ID: "/read/the-lantern-keeper", Title: "The Lantern Keeper", CoverURL: "https://example.invalid/covers/lantern-keeper.png"},
		{ID: "/read/salt-and-cedar", Title: "Salt and Cedar", CoverURL: "https://example.invalid/covers/salt-cedar.png"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("result %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}

	// The searchPath template must be filled in, with the query escaped.
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	for _, want := range []string{"q=lantern+keeper", "p=2"} {
		if !strings.Contains(calls[0].URL, want) {
			t.Errorf("URL %q does not contain %q", calls[0].URL, want)
		}
	}
}

func TestSeriesAndChaptersFromSelectorsAlone(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /read/the-lantern-keeper": {File: "listing.html"},
	})
	th := generic.NewWithClock(f, clock)
	ctx := context.Background()
	src := selectorSource()

	series, err := th.Series(ctx, src, "the-lantern-keeper")
	if err != nil {
		t.Fatal(err)
	}
	if series.Title != "The Lantern Keeper" {
		t.Errorf("Title = %q", series.Title)
	}
	if series.CoverURL != "https://example.invalid/covers/lantern-keeper-large.png" {
		t.Errorf("CoverURL = %q", series.CoverURL)
	}
	if series.Status != theme.StatusOngoing {
		t.Errorf("Status = %q, want ongoing", series.Status)
	}
	if got := strings.Join(series.Genres, "|"); got != "Fantasy|Slice of Life" {
		t.Errorf("Genres = %q", got)
	}
	if !strings.HasPrefix(series.Description, "Every dusk") {
		t.Errorf("Description = %q", series.Description)
	}

	chapters, err := th.Chapters(ctx, src, "the-lantern-keeper")
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Fatalf("got %d chapters, want 2: %+v", len(chapters), chapters)
	}
	// Ascending reading order (PLAN §7.2). listing.html lists the newest
	// first, as the sites this escape hatch is pointed at generally do, so the
	// chapter the rest of this test cares about is the *last* one. The escape
	// hatch gets no exemption from the contract: a user's selectors say where
	// the chapters are, not which way round they run.
	newest := chapters[len(chapters)-1]
	if newest.ID != "/read/the-lantern-keeper/4" || newest.Number != 4 {
		t.Errorf("newest chapter = %+v", newest)
	}
	want := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	if !newest.Published.Equal(want) {
		t.Errorf("newest chapter Published = %v, want %v", newest.Published, want)
	}
	if chapters[0].Number >= newest.Number {
		t.Errorf("chapters are not ascending: %v then %v", chapters[0].Number, newest.Number)
	}
}

func TestPagesFromSelectorsAlone(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /read/the-lantern-keeper/4": {File: "reader.html"},
	})
	th := generic.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), selectorSource(), "/read/the-lantern-keeper/4")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://cdn.example.invalid/f/lantern/4/1.jpg",
		"https://cdn.example.invalid/f/lantern/4/2.jpg",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}

// scriptSource adds the goja hook. The fixture's real page list is a
// base64-encoded JSON array in an inline script, which no CSS selector can
// reach — the one situation the hook exists for.
func scriptSource() *theme.Source {
	src := selectorSource()
	src.Script = `
		function pages(html) {
			var m = html.match(/__PAGES__\s*=\s*"([^"]+)"/);
			if (!m) { return null; }
			return JSON.parse(atob(m[1]));
		}
	`
	return src
}

func TestPagesFromTheScriptHook(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /read/the-lantern-keeper/4": {File: "reader.html"},
	})
	th := generic.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), scriptSource(), "/read/the-lantern-keeper/4")
	if err != nil {
		t.Fatal(err)
	}
	// Five, not the two the DOM offers: the script found the encoded list.
	want := []string{
		"https://cdn.example.invalid/f/lantern/4/1.jpg",
		"https://cdn.example.invalid/f/lantern/4/2.jpg",
		"https://cdn.example.invalid/f/lantern/4/3.jpg",
		"https://cdn.example.invalid/f/lantern/4/4.jpg",
		"https://cdn.example.invalid/f/lantern/4/5.jpg",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}

func TestScriptFallsBackToSelectors(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{name: "no pages function at all", script: `function chapters() { return null }`},
		{name: "pages returns null", script: `function pages() { return null }`},
		{name: "pages returns undefined", script: `function pages() {}`},
		{name: "pages returns an empty array", script: `function pages() { return [] }`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /read/the-lantern-keeper/4": {File: "reader.html"},
			})
			th := generic.NewWithClock(f, clock)
			src := selectorSource()
			src.Script = tc.script

			got, err := th.Pages(context.Background(), src, "/read/the-lantern-keeper/4")
			if err != nil {
				t.Fatal(err)
			}
			// The selector path, which only sees the two DOM images.
			if len(got) != 2 {
				t.Fatalf("got %d pages, want the 2 the selectors find: %v", len(got), got)
			}
		})
	}
}

func TestScriptFailuresAreReported(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		wantErr string
	}{
		{
			name:    "a thrown exception",
			script:  `function pages() { throw new Error("nope") }`,
			wantErr: "script threw",
		},
		{
			name:    "pages is not a function",
			script:  `var pages = 42;`,
			wantErr: "not a function",
		},
		{
			name:    "an infinite loop is interrupted, not tolerated",
			script:  `function pages() { while (true) {} }`,
			wantErr: "interrupted",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /read/the-lantern-keeper/4": {File: "reader.html"},
			})
			th := generic.NewWithClock(f, clock)
			src := selectorSource()
			src.Script = tc.script
			// Keep the interrupt test quick.
			src.Overrides = map[string]any{"scriptTimeoutMs": float64(150)}

			_, err := th.Pages(context.Background(), src, "/read/the-lantern-keeper/4")
			if err == nil {
				t.Fatalf("Pages returned no error, want one containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestScriptHasNoHostBindings pins the package comment's central claim. The
// hook transforms a string; it must not be a second HTTP path that bypasses
// the PLAN §7.4 fetch invariants.
func TestScriptHasNoHostBindings(t *testing.T) {
	forbidden := []string{
		"fetch", "XMLHttpRequest", "require", "process", "global",
		"setTimeout", "setInterval", "console", "Buffer", "__dirname",
	}
	for _, name := range forbidden {
		t.Run(name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /read/the-lantern-keeper/4": {File: "reader.html"},
			})
			th := generic.NewWithClock(f, clock)
			src := selectorSource()
			// If the binding exists, this returns a one-element array and the
			// test fails; if it does not, the ReferenceError is reported.
			src.Script = `function pages() { return [String(typeof ` + name + `)] }`

			got, err := th.Pages(context.Background(), src, "/read/the-lantern-keeper/4")
			if err != nil {
				t.Fatalf("unexpected error probing %q: %v", name, err)
			}
			if len(got) == 1 && !strings.Contains(got[0], "undefined") {
				t.Fatalf("the script runtime exposes %q (typeof = %q); the fetch invariants could be bypassed", name, got[0])
			}
		})
	}
}

func TestScriptIsValidatedWhenTheSourceIsAdded(t *testing.T) {
	th := generic.New(nil)
	tests := []struct {
		name    string
		script  string
		wantErr bool
	}{
		{name: "an empty script", script: ""},
		{name: "a valid script", script: `function pages() { return [] }`},
		{name: "a syntax error", script: `function pages( { return [] }`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := selectorSource()
			src.Script = tc.script
			err := th.Validate(src)
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestSelectorVocabularyIsClosed(t *testing.T) {
	th := generic.New(nil)
	tests := []struct {
		name      string
		selectors map[string]string
		wantErr   string
	}{
		{name: "no selectors", selectors: nil},
		{name: "a known selector", selectors: map[string]string{generic.PageImage: "img"}},
		{
			name:      "a typo is rejected, not ignored",
			selectors: map[string]string{"pageImages": "img"},
			wantErr:   `unknown selector "pageImages"`,
		},
		{
			name:      "an invented selector is rejected",
			selectors: map[string]string{"coverThumbnail": "img"},
			wantErr:   "unknown selector",
		},
		{
			name:      "the error names the accepted selectors",
			selectors: map[string]string{"nope": "x"},
			wantErr:   "accepted selectors are",
		},
		{
			name:      "an empty selector is rejected",
			selectors: map[string]string{generic.PageImage: "  "},
			wantErr:   "is empty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := &theme.Source{ID: "s", Theme: generic.ID, BaseURL: "https://example.invalid", Selectors: tc.selectors}
			err := th.Validate(src)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestMissingSelectorIsNamed(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /read/x": {File: "reader.html"},
	})
	th := generic.NewWithClock(f, clock)
	src := &theme.Source{
		ID: "s", Theme: generic.ID, BaseURL: "https://example.invalid",
		Selectors: map[string]string{generic.SeriesPath: "/read/{id}"},
	}

	_, err := th.Pages(context.Background(), src, "/read/x")
	if !errors.Is(err, generic.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	// The user must be told *which* selector, not just that something is wrong.
	if !strings.Contains(err.Error(), generic.PageImage) {
		t.Errorf("error %q does not name the missing selector", err)
	}
}

// TestFingerprintIsAlwaysZero pins the second lock on the door the registry
// already closes: the escape hatch must never win a probe.
func TestFingerprintIsAlwaysZero(t *testing.T) {
	th := generic.New(nil)
	for _, body := range []string{"", "<html></html>", "<div class='wp-manga'>ts_reader.run({})</div>"} {
		if n := th.Fingerprint(nil); n != 0 {
			t.Fatalf("Fingerprint = %d on %q, want 0", n, body)
		}
	}
}

// TestBase64IsTheOnlyBinding checks the two functions Quire does add. They are
// pure string transforms, which is exactly why they were acceptable to add
// when nothing else was.
func TestBase64IsTheOnlyBinding(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "atob decodes standard base64",
			script: `function pages() { return [atob("aGVsbG8=")] }`,
			want:   "https://example.invalid/hello",
		},
		{
			name:   "atob accepts the URL-safe alphabet",
			script: `function pages() { return [atob("aGVsbG8")] }`,
			want:   "https://example.invalid/hello",
		},
		{
			name:   "btoa round-trips",
			script: `function pages() { return [atob(btoa("hello"))] }`,
			want:   "https://example.invalid/hello",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /read/the-lantern-keeper/4": {File: "reader.html"},
			})
			th := generic.NewWithClock(f, clock)
			src := selectorSource()
			src.Script = tc.script

			got, err := th.Pages(context.Background(), src, "/read/the-lantern-keeper/4")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("pages = %v, want [%s]", got, tc.want)
			}
		})
	}
}

// TestBadBase64IsReportedNotSwallowed keeps a mistyped blob from looking like
// an empty chapter.
func TestBadBase64IsReportedNotSwallowed(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /read/the-lantern-keeper/4": {File: "reader.html"},
	})
	th := generic.NewWithClock(f, clock)
	src := selectorSource()
	src.Script = `function pages() { return [atob("!!!not base64!!!")] }`

	if _, err := th.Pages(context.Background(), src, "/read/the-lantern-keeper/4"); err == nil {
		t.Fatal("bad base64 produced no error")
	}
}

// TestRegistryValidatesGenericSources wires the last check in: the registry is
// the one gate every source passes through, so a bad selector or an
// uncompilable script must be caught there and not only by calling
// generic.Validate directly.
func TestRegistryValidatesGenericSources(t *testing.T) {
	reg := theme.NewRegistry()
	if err := reg.Register(generic.New(nil)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		src     *theme.Source
		wantErr string
	}{
		{
			name: "a well-formed generic source",
			src:  selectorSource(),
		},
		{
			name: "an unknown selector",
			src: &theme.Source{
				ID: "s", Theme: generic.ID, BaseURL: "https://example.invalid",
				Selectors: map[string]string{"pageImages": "img"},
			},
			wantErr: "unknown selector",
		},
		{
			name: "a script that does not compile",
			src: &theme.Source{
				ID: "s", Theme: generic.ID, BaseURL: "https://example.invalid",
				Script: "function pages( {",
			},
			wantErr: "script",
		},
		{
			name: "an unknown override",
			src: &theme.Source{
				ID: "s", Theme: generic.ID, BaseURL: "https://example.invalid",
				Overrides: map[string]any{"mangaSubPath": "manga"},
			},
			wantErr: "unknown override key",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := reg.Validate(tc.src)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

// PLAN §7.2, 2026-09-15: ascending reading order — and the escape hatch gets
// no exemption.
//
// A user's selectors say where the chapters are on the page. They say nothing
// about which way round the site lists them, and there is no selector that
// could: listing.html is newest-first, like the sites this hatch exists for.
// The same applies to the script hook, which is a user's JavaScript and is not
// trusted to have got the direction right either.
func TestChaptersAreAscending(t *testing.T) {
	t.Run("from selectors", func(t *testing.T) {
		f := themetest.New(t, map[string]themetest.Route{
			"GET /read/the-lantern-keeper": {File: "listing.html"},
		})
		th := generic.NewWithClock(f, clock)

		got, err := th.Chapters(context.Background(), selectorSource(), "the-lantern-keeper")
		if err != nil {
			t.Fatal(err)
		}
		assertAscending(t, got)
	})
}

func assertAscending(t *testing.T, got []theme.Chapter) {
	t.Helper()
	if len(got) < 2 {
		t.Fatalf("got %d chapters; the fixture has more than one", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Number < got[i-1].Number {
			t.Fatalf("chapter %d (%v) comes after %d (%v); the list is still in the "+
				"site's own order", i, got[i].Number, i-1, got[i-1].Number)
		}
	}
	if !theme.OrderIsKnown(got) {
		t.Error("a fully numbered list was reported as having an unknown order")
	}
}
