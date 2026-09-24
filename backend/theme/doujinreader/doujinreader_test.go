package doujinreader_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/doujinreader"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test here runs offline: the fixtures are synthetic pages on
// example.invalid and DNS is disabled for the package, so a future "just
// point it at the real site to check" is a red test rather than a quiet
// network call.
func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

func clock() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

func site() *theme.Source {
	return &theme.Source{
		ID: "user-added-09", Name: "Example Doujin Reader", Lang: "en",
		Theme: doujinreader.ID, BaseURL: "https://example.invalid",
		AddedAt: clock(),
	}
}

// An empty query is the UI's Browse (PLAN §7.5, M3 correction 1). This
// family's own search answers a blank query with nothing at all — confirmed
// live against one of the real sites during development — so Browse must go
// to the home listing instead.
func TestSearchWithNoQueryBrowsesTheHomeListing(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /": {File: "home.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	// The listing page just fetched — the exact URL, per PLAN §7.6 — is the
	// referrer every cover in this result set must carry.
	const browseReferrer = "https://example.invalid/"
	want := []theme.SeriesStub{
		{
			ID:            "/g/42",
			Title:         "A Story About Nothing in Particular",
			CoverURL:      "https://images.example.invalid/005/42/thumb.jpg",
			CoverReferrer: browseReferrer,
		},
		{
			// No .caption on this card and an empty thumbnail alt; the title
			// comes from the .g_title heading instead.
			ID:            "/g/43",
			Title:         "Second Sample Story, Side A",
			CoverURL:      "https://images.example.invalid/005/43/thumb.jpg",
			CoverReferrer: browseReferrer,
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

// Regression for the bug where Search("", page>1) built a bare "/?page={n}",
// which hentaifox.com silently ignores (it re-answers page 1's own content
// rather than erroring or redirecting) — confirmed live against
// hentaifox.com and nhentai.xxx during development. Page 2 must instead come
// from whatever URL page 1's own pagination widget names (paginatedListing,
// listing.go), exercised here with hentaifox's path-style shape (/page/2/).
func TestSearchEmptyQueryPage2FollowsHentaifoxStylePagination(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-hentaifox-page1.html"},
		"GET /page/2/": {File: "home-hentaifox-page2.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	page1, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	page2, err := th.Search(context.Background(), site(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 1 || page1[0].ID != "/g/42" {
		t.Fatalf("page 1 = %+v, want just /g/42", page1)
	}
	if len(page2) != 1 || page2[0].ID != "/g/99" {
		t.Fatalf("page 2 = %+v, want just /g/99 (page 1's own pagination link's page) — not page 1 repeated", page2)
	}
}

// The same regression, exercised with nhentai.xxx's own pagination shape (a
// query string, ?page=2) — proving paginatedListing follows whichever URL
// the widget actually names rather than assuming hentaifox's path style.
func TestSearchEmptyQueryPage2FollowsNhentaiStylePagination(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-nhentai-page1.html"},
		"GET /?page=2": {File: "home-nhentai-page2.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	page1, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	page2, err := th.Search(context.Background(), site(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 1 || page1[0].ID != "/g/42" {
		t.Fatalf("page 1 = %+v, want just /g/42", page1)
	}
	if len(page2) != 1 || page2[0].ID != "/g/77" {
		t.Fatalf("page 2 = %+v, want just /g/77 (page 1's own pagination link's page) — not page 1 repeated", page2)
	}
}

// A real query goes to the site's own text search.
func TestSearchWithAQuery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/?q=sample&page=1": {File: "search.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "sample", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	want := theme.SeriesStub{
		ID:            "/g/42",
		Title:         "A Story About Nothing in Particular",
		CoverURL:      "https://images.example.invalid/005/42/thumb.jpg",
		CoverReferrer: "https://example.invalid/search/?q=sample&page=1",
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("result:\n got %+v\nwant %+v", got[0], want)
	}
}

// The fixture card carries a second link to a page of the same gallery
// (/g/42/1/) alongside its own link (/g/42/); the page link must never be
// read as a second, different gallery.
func TestSearchNeverReturnsAReaderLinkAsAGallery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /search/?q=sample&page=1": {File: "search.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Search(context.Background(), site(), "sample", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.ID != "/g/42" {
			t.Errorf("result ID = %q, want the gallery's own two-segment path /g/42, not the three-segment reader link", s.ID)
		}
	}
}

// The taxonomy is scoped to the page's single .info block; a footer link and
// a related-gallery strip's own tag/category links must not bleed into it.
func TestSeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /g/42/": {File: "series.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), "/g/42")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "A Story About Nothing in Particular" {
		t.Errorf("title = %q", got.Title)
	}
	if got.CoverURL != "https://images.example.invalid/005/42/cover.jpg" {
		t.Errorf("cover = %q", got.CoverURL)
	}
	if want := "https://example.invalid/g/42/"; got.CoverReferrer != want {
		t.Errorf("cover referrer = %q, want the gallery page actually fetched, %q", got.CoverReferrer, want)
	}
	// Both taxonomy skins compared live are exercised: one nests a plain
	// count span inside the anchor, the other wraps the name itself in
	// ".tag_name" — both must yield the bare name.
	if strings.Join(got.Genres, "|") != "placeholder one|placeholder two" {
		t.Errorf("genres = %v", got.Genres)
	}
	if strings.Join(got.Artists, "|") != "sample artist" {
		t.Errorf("artists = %v", got.Artists)
	}
	if strings.Join(got.Authors, "|") != "sample circle" {
		t.Errorf("authors (circle credit) = %v", got.Authors)
	}
	if got.Status != theme.StatusUnknown {
		t.Errorf("status = %q, want unset: this family has no publication status to report", got.Status)
	}
}

// The structural point the whole theme turns on: this family has no chapters,
// so exactly one chapter stands for the whole gallery.
func TestChaptersIsExactlyOneChapterForTheWholeGallery(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /g/42/": {File: "series.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Chapters(context.Background(), site(), "/g/42")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d chapters, want exactly 1: %+v", len(got), got)
	}
	if got[0].ID != "/g/42" {
		t.Errorf("chapter ID = %q, want the gallery's own ID", got[0].ID)
	}
	if got[0].Title != "A Story About Nothing in Particular" {
		t.Errorf("chapter title = %q", got[0].Title)
	}
	if got[0].Number != 1 {
		t.Errorf("chapter number = %v, want 1", got[0].Number)
	}
	if got[0].OrderUnknown {
		t.Error("a single chapter has nothing to sort; order should read as known")
	}
}

// Pages has no bulk endpoint to read: each reader page names only its own
// image, so Pages visits every one of them. It first fetches the gallery's
// own detail page to discover the reader's path segment from its thumbnail
// grid (series.html's "gallery_bottom" section) — see the package comment on
// why that segment cannot be assumed. The three reader fixtures also cover
// both element spellings (#fimg, #gimg) and three different extensions, which
// is why the thumbnail's own extension cannot be reused for the page.
func TestPagesVisitsEveryReaderPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /g/42/":   {File: "series.html"},
		"GET /g/42/1/": {File: "reader1.html"},
		"GET /g/42/2/": {File: "reader2.html"},
		"GET /g/42/3/": {File: "reader3.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/g/42")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://images.example.invalid/005/42/1.webp",
		"https://images.example.invalid/005/42/2.jpg",
		"https://images.example.invalid/005/42/3.png",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("pages =\n %v\nwant\n %v", got, want)
	}
}

// FirstPage implements theme.FirstPageProber: the probe's capability check
// needs one image, not the whole gallery, and this family's Pages() would
// otherwise cost one request per page (see the package comment and
// theme.FirstPageProber). Deliberately no routes are registered for reader
// pages 2 and 3 here — themetest.Fetcher fails the test outright on a
// request to an unregistered route, so if FirstPage regressed into walking
// every page like Pages does, this test would fail on the unrouted request
// rather than merely on a wrong answer.
func TestFirstPageStopsAfterOnePage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /g/42/":   {File: "series.html"},
		"GET /g/42/1/": {File: "reader1.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.FirstPage(context.Background(), site(), "/g/42")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://images.example.invalid/005/42/1.webp"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("pages =\n %v\nwant\n %v", got, want)
	}
}

// A gallery whose page count field cannot be read at all must still yield the
// one page that was actually fetched, rather than nothing.
func TestPagesTolerateAMissingPageCount(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /g/42/": {File: "series.html"},
		"GET /g/42/1/": {
			Body: `<html><body><img id="fimg" data-src="https://images.example.invalid/005/42/1.webp"></body></html>`,
		},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/g/42")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "https://images.example.invalid/005/42/1.webp" {
		t.Errorf("pages = %v, want exactly the one page fetched", got)
	}
}

// The structural failure a hard-coded route would hit: a gallery whose own
// reader lives under a *different* path segment than its detail page —
// measured live on two of the three real sites compared, each pairing its
// detail and reader routes differently. Pages must read the real segment off
// the detail page's own thumbnail grid rather than assume it, and needs no
// override to do it.
func TestPagesDiscoversAMismatchedReaderSegment(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /gallery/99/": {File: "series-mismatched-reader.html"},
		"GET /read/99/1/": {
			Body: `<html><body><input type="hidden" name="pages" id="pages" value="1" />` +
				`<img id="fimg" data-src="https://images.example.invalid/007/99/1.webp"></body></html>`,
		},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Pages(context.Background(), site(), "/gallery/99")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "https://images.example.invalid/007/99/1.webp" {
		t.Errorf("pages = %v", got)
	}
}

// A gallery detail page that carries no discoverable link to its own reader
// at all — none of the three real sites compared behave this way, but a
// mirror that renders its thumbnail grid entirely by client-side script
// would — falls back to the configured readerPath override rather than
// failing outright.
func TestPagesFallsBackToReaderPathOverrideWhenNothingIsDiscoverable(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /gallery/7/": {Body: `<html><body><h1>No Thumbnail Grid Here</h1></body></html>`},
		"GET /read/7/1/": {
			Body: `<html><body><input type="hidden" name="pages" id="pages" value="1" />` +
				`<img id="fimg" data-src="https://images.example.invalid/003/7/1.webp"></body></html>`,
		},
	})
	th := doujinreader.NewWithClock(f, clock)

	s := site()
	s.Overrides = map[string]any{doujinreader.KeyReaderPath: "read"}
	got, err := th.Pages(context.Background(), s, "/gallery/7")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "https://images.example.invalid/003/7/1.webp" {
		t.Errorf("pages = %v", got)
	}
}

// See the package comment: two of three sites compared serve images from
// their own registrable domain and need nothing declared, and the third's
// separate CDN domain is that operator's own choice rather than a pattern the
// family repeats.
func TestAllowedHostsIsNil(t *testing.T) {
	if got := doujinreader.New(nil).AllowedHosts(); got != nil {
		t.Errorf("AllowedHosts() = %v, want nil", got)
	}
}

// This is a family of independently branded, independently operated sites
// with no shared name to lend a new source.
func TestSuggestedNameIsEmpty(t *testing.T) {
	if got := doujinreader.New(nil).SuggestedName(); got != "" {
		t.Errorf("SuggestedName() = %q, want \"\"", got)
	}
}

// The probe fingerprints whatever page the user pasted, which is usually the
// home page and may be any of the others. Each must be recognised on its own.
func TestFingerprintRecognisesEveryPageOfTheFamily(t *testing.T) {
	th := doujinreader.New(nil)
	for _, f := range []string{"home.html", "search.html", "series.html", "reader1.html", "reader3.html"} {
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

// A page of an unrelated, WordPress-based comic family must not be claimed:
// none of this family's own markers are there, so the score should be low.
func TestFingerprintDoesNotClaimAWordPressComicPage(t *testing.T) {
	th := doujinreader.New(nil)
	body := `<html><body><div class="wp-manga"><div class="chapter-list-container" data-api-url="/api"></div></div></body></html>`
	if got := th.Fingerprint(probe.NewPage(nil, nil, 200, nil, []byte(body))); got >= 60 {
		t.Errorf("scored %d on a page carrying another family's own markers", got)
	}
}
