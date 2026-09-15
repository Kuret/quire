package mangadex_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Offline, like every other theme's tests (PLAN §6 M2): committed synthetic
// fixtures, the themetest fetcher, and DNS disabled for the whole package so
// "just point it at the real API to check" is a red test rather than a quiet
// network call.
//
// Read docs/THEME-NOTES.md on what fixtures do and do not prove before
// treating a green run here as evidence that MangaDex works. These pin *our*
// parsing of a documented shape. What proves the site works is a live stage 5.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

var fixedNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixedNow }

const (
	mangaUUID   = "11111111-2222-4333-8444-555555555555"
	chapterUUID = "aaaaaaaa-1111-4111-8111-111111111111"
)

// site is a source on example.invalid. Validate() permits a .invalid host for
// exactly this reason and refuses anything else that is not the real API.
func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-05", Name: "MangaDex", Lang: "en",
		Theme: mangadex.ID, BaseURL: "https://api.example.invalid",
		AddedAt: fixedNow,
	}
}

func TestSearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "tower", 1)
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.SeriesStub{
		{
			// No English title, only ja-ro: the title has to come from
			// altTitles. MangaDex has no one canonical title, which is the
			// first thing that makes it unlike the HTML themes.
			ID:       "/manga/" + mangaUUID,
			Title:    "Record of the Distant Tower",
			CoverURL: "https://uploads.example.invalid/covers/" + mangaUUID + "/distant-tower-1.jpg.512.jpg",
		},
		{
			ID:       "/manga/22222222-3333-4444-8555-666666666666",
			Title:    "Salt and Cedar",
			CoverURL: "https://uploads.example.invalid/covers/22222222-3333-4444-8555-666666666666/salt-and-cedar.png.512.jpg",
		},
		{
			// A cover_art relationship with no attributes is what arrives when
			// includes[] was not sent; no cover rather than a broken URL.
			ID:       "/manga/33333333-4444-4555-8666-777777777777",
			Title:    "The Ninth Ward Lanterns",
			CoverURL: "",
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

// PLAN §7.2's Source.lang has to actually filter. MangaDex is multilingual by
// design, and a source added with lang "en" that returns Portuguese is the
// specific bug this theme exists to not have.
func TestSearchSendsLanguageAndRatingFilters(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.Search(context.Background(), site(), "tower", 1); err != nil {
		t.Fatal(err)
	}

	q := lastQuery(t, f)
	if got := q["availableTranslatedLanguage[]"]; len(got) != 1 || got[0] != "en" {
		t.Errorf("availableTranslatedLanguage[] = %v, want [en]; a search that offers "+
			"series with no chapters in the user's language wastes their tap", got)
	}
	if got := q.Get("title"); got != "tower" {
		t.Errorf("title = %q, want %q", got, "tower")
	}
	// The default maxContentRating is "suggestive", so two ratings and not four.
	if got := q["contentRating[]"]; len(got) != 2 || got[0] != "safe" || got[1] != "suggestive" {
		t.Errorf("contentRating[] = %v, want [safe suggestive]", got)
	}
}

func TestSearchPagination(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {File: "search.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.Search(context.Background(), site(), "tower", 3); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f)
	if got := q.Get("offset"); got != "40" {
		t.Errorf("page 3 asked for offset %q, want %q", got, "40")
	}
	if got := q.Get("limit"); got != "20" {
		t.Errorf("limit = %q, want 20", got)
	}
}

func TestSeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/" + mangaUUID: {File: "series.json"},
	})
	th := mangadex.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), "/manga/"+mangaUUID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Title != "Record of the Distant Tower" {
		t.Errorf("title = %q", got.Title)
	}
	if want := "A surveyor walks the length of a tower nobody finished."; got.Description != want {
		t.Errorf("description = %q, want the en one (%q)", got.Description, want)
	}
	if got.Status != theme.StatusOngoing {
		t.Errorf("status = %q, want %q", got.Status, theme.StatusOngoing)
	}
	// Deduplicated, in API order.
	if strings.Join(got.Authors, "|") != "Mira Vance|Ono Haruki" {
		t.Errorf("authors = %v", got.Authors)
	}
	if strings.Join(got.Artists, "|") != "Ono Haruki" {
		t.Errorf("artists = %v", got.Artists)
	}
	// "Survival" has no pt-br name and must still appear, in English.
	if strings.Join(got.Genres, "|") != "Adventure|Survival" {
		t.Errorf("genres = %v", got.Genres)
	}
	// The promoted title must not be repeated among the alternatives.
	for _, alt := range got.AltTitles {
		if alt == got.Title {
			t.Errorf("altTitles repeats the main title: %v", got.AltTitles)
		}
	}
	if len(got.AltTitles) == 0 {
		t.Error("altTitles is empty; the fixture carries two other languages")
	}
}

// The same series, read by a Portuguese source. Nothing but Source.lang
// changes, and both the title and the description follow it.
func TestSeriesFollowsSourceLanguage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/" + mangaUUID: {File: "series.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	s := site()
	s.Lang = "pt-br"

	got, err := th.Series(context.Background(), s, "/manga/"+mangaUUID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Um agrimensor percorre uma torre que ninguém terminou."; got.Description != want {
		t.Errorf("description = %q, want the pt-br one", got.Description)
	}
	if got.Genres[0] != "Aventura" {
		t.Errorf("genres = %v, want the pt-br name first", got.Genres)
	}
}

func TestChapters(t *testing.T) {
	_, th := feedFixture(t)

	got, err := th.Chapters(context.Background(), site(), "/manga/"+mangaUUID)
	if err != nil {
		t.Fatal(err)
	}

	want := []theme.Chapter{
		{
			ID:        "/chapter/" + chapterUUID,
			Title:     "Chapter 1: The Long Walk",
			Volume:    "1",
			Number:    1,
			Published: time.Date(2026, 1, 4, 9, 30, 0, 0, time.UTC),
			Scanlator: "Tower Survey Scans",
		},
		{
			// No volume, no title: the composed form degrades cleanly.
			ID:        "/chapter/aaaaaaaa-2222-4222-8222-222222222222",
			Title:     "Chapter 12.5",
			Number:    12.5,
			Published: time.Date(2026, 2, 11, 18, 5, 0, 0, time.UTC),
		},
		{
			// A oneshot: theme.Chapter documents Number as -1 when the title
			// carries none.
			ID:        "/chapter/aaaaaaaa-3333-4333-8333-333333333333",
			Title:     "Oneshot: The Surveyor's Notebook",
			Volume:    "2",
			Number:    -1,
			Published: time.Date(2026, 4, 2, 7, 15, 0, 0, time.UTC),
			Scanlator: "Ninth Ward Translations",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d chapters, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chapter %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// The filter is sent to the server *and* applied locally. The pt-br entry in
// the fixture is there to prove the second half: a refactor that drops the
// query parameter produces a short list, not a multilingual one.
func TestChaptersFilterByLanguage(t *testing.T) {
	f, th := feedFixture(t)
	if _, err := th.Chapters(context.Background(), site(), "/manga/"+mangaUUID); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Calls() {
		u, err := url.Parse(c.URL)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(u.Path, "/feed") {
			continue
		}
		if got := u.Query()["translatedLanguage[]"]; len(got) != 1 || got[0] != "en" {
			t.Errorf("feed request %s: translatedLanguage[] = %v, want [en]", u.RequestURI(), got)
		}
	}
}

// Excluded by default and included on request. A chapter hosted on the
// publisher's own site has no images here, so offering it would produce a
// download that cannot succeed.
func TestChaptersExternalChaptersAreHiddenByDefault(t *testing.T) {
	f, th := feedFixture(t)
	got, err := th.Chapters(context.Background(), site(), "/manga/"+mangaUUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if strings.Contains(c.Title, "Elsewhere") {
			t.Fatalf("an externally hosted chapter was listed: %+v", c)
		}
	}
	_ = f

	f2, th2 := feedFixture(t)
	s := site()
	s.Overrides = map[string]any{mangadex.KeyIncludeExternal: true}
	got2, err := th2.Chapters(context.Background(), s, "/manga/"+mangaUUID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range got2 {
		if strings.Contains(c.Title, "Elsewhere") {
			found = true
		}
	}
	if !found {
		t.Errorf("includeExternal did not include the external chapter: %+v", got2)
	}
	_ = f2
}

// Paging is the difference between a chapter list and the first page of one.
func TestChaptersPaginate(t *testing.T) {
	f, th := feedFixture(t)
	if _, err := th.Chapters(context.Background(), site(), "/manga/"+mangaUUID); err != nil {
		t.Fatal(err)
	}

	var offsets []string
	for _, c := range f.Calls() {
		u, err := url.Parse(c.URL)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(u.Path, "/feed") {
			offsets = append(offsets, u.Query().Get("offset"))
		}
	}
	if strings.Join(offsets, ",") != "0,4" {
		t.Errorf("feed offsets = %v, want [0 4]; total exceeds the first page", offsets)
	}
}

func TestPages(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /at-home/server/" + chapterUUID: {File: "at-home.json"},
	})
	th := mangadex.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/chapter/"+chapterUUID)
	if err != nil {
		t.Fatal(err)
	}

	const base = "https://images.example.invalid/data/0f1e2d3c4b5a69788796a5b4c3d2e1f0/"
	want := []string{
		base + "1-aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff6666aaaa7777bbbb8888.png",
		base + "2-bbbb1111cccc2222dddd3333eeee4444ffff5555aaaa6666bbbb7777cccc8888.png",
		base + "3-cccc1111dddd2222eeee3333ffff4444aaaa5555bbbb6666cccc7777dddd8888.png",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d:\n%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("page %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
	// Full quality, not the recompressed set: M4 re-encodes for the device
	// anyway, so starting from a data-saver JPEG would compound the loss.
	for _, u := range got {
		if strings.Contains(u, "/data-saver/") || strings.HasSuffix(u, ".jpg") {
			t.Errorf("page %s came from the data-saver set", u)
		}
	}
}

// The PLAN §7.4 decision, from the theme's side. /at-home/ is the one path
// api.mangadex.org disallows, and it is the one path Quire fetches as
// retrieval — because the user opened this chapter and asked for it.
func TestPagesIsFetchedAsRetrieval(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /at-home/server/" + chapterUUID: {File: "at-home.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.Pages(context.Background(), site(), "/chapter/"+chapterUUID); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(calls), calls)
	}
	if calls[0].Kind != fetch.KindRetrieval {
		t.Errorf("Pages fetched /at-home/ as %v; robots.txt disallows that path, so "+
			"discovery would be refused and the chapter unreadable", calls[0].Kind)
	}
}

// The other half of the same claim, and the more important one: everything
// used to *find* a chapter is discovery and stays bound by robots.
func TestDiscoveryCallsAreNotRetrieval(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga":              {File: "search.json"},
		"GET /manga/" + mangaUUID: {File: "series.json"},
	})
	th := mangadex.NewWithClock(f, clock)
	ctx := context.Background()
	if _, err := th.Search(ctx, site(), "tower", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Series(ctx, site(), "/manga/"+mangaUUID); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Calls() {
		if c.Kind != fetch.KindDiscovery {
			t.Errorf("%s %s was fetched as %v; searching and listing is crawling, and "+
				"robots binds it", c.Method, c.URL, c.Kind)
		}
	}
}

func TestPagesRejectsASeriesID(t *testing.T) {
	f := themetest.New(t, nil)
	th := mangadex.NewWithClock(f, clock)
	if _, err := th.Pages(context.Background(), site(), "/manga/"+mangaUUID); err == nil {
		t.Fatal("a series id was accepted as a chapter id")
	}
}

// A 429 that survived the fetch layer's Retry-After handling is reported as
// rate limiting, not as a decode failure. MangaDex does return them.
func TestRateLimitedResponseIsReportedPlainly(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {
			Status: http.StatusTooManyRequests,
			Header: http.Header{"Retry-After": []string{"30"}},
			Body:   `{"result":"error","errors":[{"status":429}]}`,
		},
	})
	th := mangadex.NewWithClock(f, clock)
	_, err := th.Search(context.Background(), site(), "tower", 1)
	if err == nil {
		t.Fatal("a 429 was treated as a successful search")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %v, want it to say it was rate limited", err)
	}
}

// Search terms are the user's own words and must not end up in an error
// string that may be logged or shown.
func TestErrorsDoNotLeakTheQuery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga": {Status: http.StatusInternalServerError, Body: "nope"},
	})
	th := mangadex.NewWithClock(f, clock)
	_, err := th.Search(context.Background(), site(), "something private", 1)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "private") {
		t.Errorf("err = %v; it repeats the search term", err)
	}
}

func lastQuery(t *testing.T, f *themetest.Fetcher) url.Values {
	t.Helper()
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("no requests were made")
	}
	u, err := url.Parse(calls[len(calls)-1].URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func feedFixture(t *testing.T) (*themetest.Fetcher, *mangadex.Theme) {
	t.Helper()
	f := themetest.New(t, map[string]themetest.Route{
		feedKey(0): {File: "feed.json"},
		feedKey(4): {File: "feed-page-2.json"},
	})
	return f, mangadex.NewWithClock(f, clock)
}

// feedKey builds the route key for one feed page. themetest matches on the
// full request URI when a route spells out a query, which is how the two feed
// pages stay distinguishable; the parameters are reproduced here rather than
// wildcarded so that a change to what Chapters() sends is a visible test
// failure and not a silently unrouted request.
func feedKey(offset int) string {
	v := url.Values{}
	v.Add("contentRating[]", "safe")
	v.Add("contentRating[]", "suggestive")
	v.Add("includes[]", "scanlation_group")
	v.Set("limit", "500")
	v.Set("offset", itoa(offset))
	v.Set("order[chapter]", "asc")
	v.Add("translatedLanguage[]", "en")
	return "GET /manga/" + mangaUUID + "/feed?" + v.Encode()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Fingerprint. PLAN §7.5 stage 4 takes the highest score above a threshold of
// 60; the existing themes score 65–100 on a match and 0 on a miss, with the
// best near-miss at 5.
//
// This theme is the one that cannot afford a partial score. It drives a single
// site, so "sort of looks like MangaDex" has no useful meaning — and a JSON
// API is a far easier shape to resemble by accident than a WordPress theme's
// markup. Hence the hard gate on the host.
func TestFingerprint(t *testing.T) {
	tests := []struct {
		name    string
		page    *probe.Page
		min     int
		exact   int
		exactly bool
	}{
		{
			name: "the API root",
			page: apiPage("https://api.mangadex.org/", "https://api.mangadex.org/docs/", "MangaDex", `{"result":"ok"}`),
			min:  100,
		},
		{
			name: "the ping endpoint",
			page: apiPage("https://api.mangadex.org/ping", "https://api.mangadex.org/ping", "MangaDex", "pong"),
			min:  100,
		},
		{
			name: "a collection response",
			page: apiPage("https://api.mangadex.org/manga?title=x", "https://api.mangadex.org/manga?title=x", "MangaDex", `{"result":"ok","response":"collection","data":[]}`),
			min:  100,
		},
		{
			name: "the browser-facing host",
			page: apiPage("https://mangadex.org/", "https://mangadex.org/", "MangaDex", "<html><body>…</body></html>"),
			min:  60,
		},
		{
			// The whole point of the gate. Everything a shape-only fingerprint
			// would key on is present and it still scores nothing.
			name:    "another JSON API wearing the same clothes",
			page:    apiPage("https://api.example.invalid/manga", "https://api.example.invalid/manga", "MangaDex", `{"result":"ok","response":"collection","data":[]}`),
			exact:   0,
			exactly: true,
		},
		{
			name:    "a WordPress site",
			page:    apiPage("https://example.invalid/", "https://example.invalid/", "Apache", "<html><head><meta name=\"generator\" content=\"WordPress 6.5\"></head></html>"),
			exact:   0,
			exactly: true,
		},
		{
			// A redirect off mangadex.org means it is not MangaDex, whatever
			// the user typed.
			name:    "a mangadex.org URL that redirected away",
			page:    apiPage("https://api.mangadex.org/", "https://elsewhere.invalid/", "MangaDex", `{"result":"ok"}`),
			exact:   0,
			exactly: true,
		},
	}

	th := mangadex.NewWithClock(themetest.New(t, nil), clock)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := th.Fingerprint(tc.page)
			if tc.exactly {
				if got != tc.exact {
					t.Errorf("score = %d, want exactly %d", got, tc.exact)
				}
				return
			}
			if got < tc.min {
				t.Errorf("score = %d, want at least %d", got, tc.min)
			}
			if got > 100 {
				t.Errorf("score = %d, above the 0..100 range", got)
			}
		})
	}
}

func apiPage(raw, final, server, body string) *probe.Page {
	u, _ := url.Parse(raw)
	fu, _ := url.Parse(final)
	h := http.Header{}
	h.Set("Server", server)
	return probe.NewPage(u, fu, http.StatusOK, h, []byte(body))
}

// A single-site theme has to say which site. Without Validate, an imported
// sources file could point Quire's MangaDex support at any host that answers
// MangaDex-shaped JSON.
func TestValidateBindsTheSource(t *testing.T) {
	th := mangadex.NewWithClock(themetest.New(t, nil), clock)
	for _, tc := range []struct {
		base string
		ok   bool
	}{
		{"https://api.mangadex.org", true},
		{"https://api.example.invalid", true}, // the offline tests
		{"https://mangadex.org", false},       // the site, not the API
		{"https://api.mangadex.org.evil.test", false},
		{"https://elsewhere.test", false},
	} {
		s := site()
		s.BaseURL = tc.base
		err := th.Validate(s)
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.base, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s was accepted", tc.base)
		}
	}
}

func TestOverridesRejectUnknownKeys(t *testing.T) {
	th := mangadex.NewWithClock(themetest.New(t, nil), clock)
	if err := th.ValidateOverrides(map[string]any{"mangaSubPath": "manga"}); err == nil {
		t.Error("an unknown override key was accepted (PLAN §7.2)")
	}
	if err := th.ValidateOverrides(map[string]any{mangadex.KeyMaxContentRating: "erotica"}); err != nil {
		t.Errorf("a documented key was rejected: %v", err)
	}
	if err := th.ValidateOverrides(map[string]any{mangadex.KeyMaxContentRating: "everything"}); err == nil {
		t.Error("a value outside the enum was accepted")
	}
}

// PLAN §7.2, 2026-09-15. MangaDex is the theme that made theme-declared
// allowedHosts necessary: page images come from generated labels on
// mangadex.network, a different registrable domain from the API.
//
// This pins the declaration itself. That it actually satisfies the SSRF guard
// against a real /at-home/ host shape is asserted in
// backend/fetch/allowedhosts_test.go, where the guard lives — the claim needs
// both halves and they cannot both be checked from here.
func TestAllowedHostsDeclaresThePageImageCDN(t *testing.T) {
	th := mangadex.NewWithClock(themetest.New(t, nil), clock)
	hosts := th.AllowedHosts()
	if len(hosts) != 1 || hosts[0] != "*.mangadex.network" {
		t.Fatalf("AllowedHosts() = %v, want [*.mangadex.network]", hosts)
	}
	// Covers are on uploads.mangadex.org — the same registrable domain as the
	// API — so they need no declaration and must not have been given one.
	for _, h := range hosts {
		if strings.Contains(h, "mangadex.org") {
			t.Errorf("%q is already inside the source's own registrable domain; "+
				"declaring it implies a widening that is not happening", h)
		}
	}
}

// PLAN §7.2, 2026-09-15: Chapters returns ascending reading order.
//
// Asserted rather than assumed. The theme asks for order[chapter]=asc and
// MangaDex honours it, so testing against the ordinary fixtures would prove
// only that the server sorted — not that the theme would notice if it stopped.
// feed-descending.json is the same endpoint answering newest-first, which is a
// documented, supported ordering of it.
func TestChaptersAreAscendingEvenWhenTheServerIsNot(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		feedKey(0): {File: "feed-descending.json"},
	})
	th := mangadex.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), "/manga/"+mangaUUID)
	if err != nil {
		t.Fatal(err)
	}

	// Titles, so a failure reads as reading order rather than as UUIDs.
	var titles []string
	for _, c := range got {
		titles = append(titles, c.Title)
	}
	want := []string{
		"Chapter 1: The First Step",
		// The unnumbered extra keeps the neighbours the site gave it rather
		// than being flung to one end by a sort.
		"Oneshot: Omake",
		"Chapter 3: The Third Ascent",
		"Chapter 3.5: Interlude",
		"Chapter 4: The Last Landing",
	}
	if strings.Join(titles, " | ") != strings.Join(want, " | ") {
		t.Fatalf("reading order:\n got %v\nwant %v", titles, want)
	}
	if !theme.OrderIsKnown(got) {
		t.Error("the order was established but reported as unknown")
	}

	// The volume label survives onto the field, which is what PLAN §6 M4
	// groups on. It is no longer buried in the display title.
	if got[0].Volume != "1" || got[len(got)-1].Volume != "2" {
		t.Errorf("volumes = %q..%q, want 1..2", got[0].Volume, got[len(got)-1].Volume)
	}
	for _, c := range got {
		if strings.HasPrefix(c.Title, "Vol.") {
			t.Errorf("title %q still carries the volume; it belongs in Volume", c.Title)
		}
	}
}

// Covers are requested as thumbnails, not as originals.
//
// MangaDex serves print-resolution artwork by default. On the device every
// cover failed with "response exceeds size cap (declared 10852108 > 8388608)"
// — measured live on 2026-09-15, one cover was 10,852,108 bytes as the
// original and 235,535 bytes at .512.jpg, to render a 300×450 thumbnail.
//
// The caps are not the thing to change, so this pins the suffix. 512 and not
// 256: the render is 300px wide, so 256 would be upscaled, and at 235 KB the
// larger one clears both fetch's 8 MiB response cap and covers' 4 MiB
// MaxSourceBytes by more than an order of magnitude.
func TestCoversAskForAThumbnail(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/" + mangaUUID: {File: "series.json"},
	})
	th := mangadex.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), "/manga/"+mangaUUID)
	if err != nil {
		t.Fatal(err)
	}
	const want = "https://uploads.example.invalid/covers/" + mangaUUID +
		"/distant-tower-1.jpg.512.jpg"
	if got.CoverURL != want {
		t.Errorf("CoverURL = %q\n            want %q", got.CoverURL, want)
	}
	if strings.HasSuffix(got.CoverURL, ".256.jpg") {
		t.Error("256px would be upscaled into a 300px-wide render")
	}
	// The filename is kept whole, extension included — the suffix is appended
	// to it, not substituted for it. MangaDex's scheme is
	// "<filename>.<size>.jpg", so a cover stored as .png stays .png.512.jpg.
	if !strings.Contains(got.CoverURL, "distant-tower-1.jpg.") {
		t.Errorf("the original filename was rewritten rather than suffixed: %q", got.CoverURL)
	}
}
