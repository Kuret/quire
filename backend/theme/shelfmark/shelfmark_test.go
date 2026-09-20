package shelfmark_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/shelfmark"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Every test in this file is offline (PLAN §6 M2) and runs against the
// captures in testdata/, which are a real self-hosted instance's own responses
// from 2026-09-20 with personal data removed. The numbers asserted here — 40
// books, 50 releases, 34 kept — are that instance's, not invented ones, which
// is what makes them worth pinning.

const base = "https://books.example.invalid"

// The book the whole capture is about, and one release of it.
const (
	duneBookID = "/book/openlibrary/OL893414W"

	// The first release in the capture: an epub, 0.4MB, from direct_download,
	// and — usefully for the title test — not actually the novel.
	relSourceID  = "97d5f8dbaca182b7352a81a3fb54df85"
	duneRelease  = "/release/openlibrary/OL893414W/direct_download/97d5f8dbaca182b7352a81a3fb54df85"
	relTitleText = "O'Reilly, Timothy - The Maker of Dune"
)

func source() *theme.Source {
	return &theme.Source{ID: "books", Name: "Shelfmark", Theme: shelfmark.ID, Lang: "en", BaseURL: base}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// --- Fingerprint -----------------------------------------------------------

// The probe lands on the web UI, which is an empty single-page-app shell, so
// the <head> is all the evidence there is. The two claims that matter are that
// the real shell is recognised *confidently* and that a page which merely
// carries the name is not — see the theme's Fingerprint comment for the
// arithmetic.
func TestFingerprint(t *testing.T) {
	// PLAN §7.5 stage 4's threshold. Kept as a literal here rather than
	// imported so that lowering it somewhere else cannot quietly make this
	// test pass.
	const confident = 60

	tests := []struct {
		name string
		body string
		// atLeast and atMost bracket the acceptable score; 0/0 means "must
		// score nothing at all".
		atLeast, atMost int
	}{
		{
			name:    "the instance's own index.html",
			body:    string(readFixture(t, "index.html")),
			atLeast: confident,
			atMost:  100,
		},
		{
			// The lookalike that matters: something *called* Shelfmark, with
			// no trace of the application. A confident answer here would have
			// the probe hand a user's library app to this theme.
			name: "a page that is merely called Shelfmark",
			body: `<!doctype html><html><head><title>Shelfmark</title>` +
				`<meta name="description" content="A tidy little bookmarking app"></head><body></body></html>`,
			atLeast: 0,
			atMost:  confident - 1,
		},
		{
			// /api/health is the reason CheckConfig exists. {"status":"ok"} is
			// the most generic response on the internet and must score
			// nothing, not merely little.
			name: "the health endpoint",
			body: `{"status":"ok"}`,
		},
		{
			name: "the config endpoint",
			body: string(readFixture(t, "config.json")),
		},
		{
			name: "a not-found body",
			body: string(readFixture(t, "not-found.json")),
		},
		{"an empty body", "", 0, 0},
		{"binary rubbish", "\x00\x01\xff\xfe", 0, 0},
		{
			name: "a page carrying the description in a comment",
			body: `<!doctype html><html><head><title>Something Else</title>` +
				`<!-- <meta name="description" content="Shelfmark - Book search and download"> -->` +
				`</head><body></body></html>`,
		},
	}

	th := shelfmark.New(nil)
	u, err := url.Parse(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := th.Fingerprint(probe.NewPage(u, nil, 200, nil, []byte(tc.body)))
			if got < tc.atLeast || got > tc.atMost {
				t.Errorf("Fingerprint() = %d, want between %d and %d", got, tc.atLeast, tc.atMost)
			}
		})
	}

	if got := th.Fingerprint(nil); got != 0 {
		t.Errorf("Fingerprint(nil) = %d, want 0", got)
	}
}

// --- The config check ------------------------------------------------------

// The strong check, and the one the fingerprint is explicitly not. Note the
// last two cases: a healthy-looking response and a not-found body must both
// fail, because this API answers an unknown path with HTTP 200 and the body is
// the only thing that can be believed.
func TestCheckConfig(t *testing.T) {
	full := readFixture(t, "config.json")

	// The config with one required key removed, generated rather than written
	// out, so the case stays honest if the capture changes.
	without := func(key string) []byte {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(full, &m); err != nil {
			t.Fatal(err)
		}
		delete(m, key)
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	tests := []struct {
		name    string
		body    []byte
		wantErr bool
	}{
		{"the instance's own config", full, false},
		{"without supported_formats", without("supported_formats"), true},
		{"without release_search_timeout", without("release_search_timeout"), true},
		{"without metadata_search_fields", without("metadata_search_fields"), true},
		{"without books_output_mode", without("books_output_mode"), true},
		{"a healthy but otherwise silent server", []byte(`{"status":"ok"}`), true},
		{"the not-found body", readFixture(t, "not-found.json"), true},
		{"not JSON at all", []byte("<html></html>"), true},
		{"an empty body", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := shelfmark.CheckConfig(tc.body)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckConfig() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// Confirm is CheckConfig with the fetch attached, which is what the capability
// probe will call. The assertion that it asked for /api/config and nothing
// else is part of the point: a "confirmation" that never made a request would
// confirm anything.
func TestConfirmFetchesTheConfig(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/config": {File: "config.json"},
	})
	if err := shelfmark.New(f).Confirm(context.Background(), source()); err != nil {
		t.Fatalf("Confirm() = %v, want nil", err)
	}
	calls := f.Calls()
	if len(calls) != 1 || !strings.HasSuffix(calls[0].URL, shelfmark.ConfigPath) {
		t.Fatalf("Confirm() made %v, want one GET of %s", calls, shelfmark.ConfigPath)
	}
	if calls[0].Kind != fetch.KindDiscovery {
		t.Errorf("Confirm() asked as %v, want discovery", calls[0].Kind)
	}

	// And it must actually *check* what came back. A confirmation that fetches
	// and then shrugs is worse than none: the whole point is that /api/health
	// and a 200-with-an-error body are not evidence.
	for _, tc := range []struct{ name, body string }{
		{"a not-found body", `{"error":"Resource not found"}`},
		{"a healthy but otherwise silent server", `{"status":"ok"}`},
		{"some other application's config", `{"supported_formats":["epub"],"theme":"dark"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := themetest.New(t, map[string]themetest.Route{
				"GET /api/config": {Body: tc.body, Status: http.StatusOK},
			})
			if err := shelfmark.New(g).Confirm(context.Background(), source()); err == nil {
				t.Error("Confirm() accepted a server that is not a Shelfmark instance")
			}
		})
	}
}

// --- Search ----------------------------------------------------------------

func TestSearchParsesTheCapture(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/metadata/search": {File: "metadata-search-dune.json"},
	})
	res, err := shelfmark.New(f).SearchPage(context.Background(), source(), "dune", 1)
	if err != nil {
		t.Fatalf("SearchPage() = %v", err)
	}

	// 40 is what the instance returned; every one of them is addressable, so
	// none may be dropped.
	if got := len(res.Books); got != 40 {
		t.Fatalf("got %d books, want 40", got)
	}
	if !res.HasMore {
		t.Error("HasMore = false; the capture says there is another page")
	}
	if res.Page != 1 {
		t.Errorf("Page = %d, want 1", res.Page)
	}
	// total_found was 0 on a response carrying 40 books. Pinned so that nobody
	// later "fixes" the code by believing it.
	if res.TotalFound != 0 {
		t.Errorf("TotalFound = %d; the capture really does say 0", res.TotalFound)
	}

	first := res.Books[0]
	if first.ID != duneBookID {
		t.Errorf("first ID = %q, want %q", first.ID, duneBookID)
	}
	if first.Title != "Dune" {
		t.Errorf("first Title = %q, want %q", first.Title, "Dune")
	}
	// Covers are instance-relative and must be resolved against the source, or
	// the guard sees a URL with no host.
	if !strings.HasPrefix(first.CoverURL, base+"/api/covers/") {
		t.Errorf("first CoverURL = %q, want an absolute URL under %s/api/covers/", first.CoverURL, base)
	}

	// The query and the page must actually reach the instance.
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("made %d requests, want 1", len(calls))
	}
	u, err := url.Parse(calls[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("query"); got != "dune" {
		t.Errorf("query = %q, want %q", got, "dune")
	}
	if got := u.Query().Get("page"); got != "1" {
		t.Errorf("page = %q, want %q", got, "1")
	}
}

// Paging is by page number, so the number asked for has to be the number sent
// — and a nonsensical one has to become the first page rather than being
// passed through to the instance.
func TestSearchPaging(t *testing.T) {
	tests := []struct {
		name string
		page int
		want string
	}{
		{"the first page", 1, "1"},
		{"a later page", 3, "3"},
		{"zero", 0, "1"},
		{"negative", -7, "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := themetest.New(t, map[string]themetest.Route{
				"GET /api/metadata/search": {File: "metadata-search-dune.json"},
			})
			if _, err := shelfmark.New(f).Search(context.Background(), source(), "dune", tc.page); err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(f.Calls()[0].URL)
			if err != nil {
				t.Fatal(err)
			}
			if got := u.Query().Get("page"); got != tc.want {
				t.Errorf("page = %q, want %q", got, tc.want)
			}
		})
	}
}

// A book with no provider or no provider_id cannot be given an id, so there is
// nothing a later call could do with it. Dropping it at the search is the only
// place the user can see that something is missing; carrying it forward would
// produce a row that fails when tapped, out of sight of what produced it.
//
// Nothing in the capture is malformed this way — which is exactly why the case
// is written out rather than waited for.
func TestSearchDropsBooksItCannotAddress(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/metadata/search": {Body: `{
			"has_more": false, "page": 1, "total_found": 0,
			"books": [
				{"title": "Addressable", "provider": "openlibrary", "provider_id": "OL1W"},
				{"title": "No provider id", "provider": "openlibrary", "provider_id": ""},
				{"title": "No provider", "provider": "", "provider_id": "OL2W"},
				{"title": "", "provider": "openlibrary", "provider_id": "OL3W"}
			]}`},
	})
	got, err := shelfmark.New(f).Search(context.Background(), source(), "x", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d books, want only the addressable one: %+v", len(got), got)
	}
	if got[0].Title != "Addressable" {
		t.Errorf("kept %q, want %q", got[0].Title, "Addressable")
	}
}

// --- Series ----------------------------------------------------------------

func TestSeries(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		// metadata-book-dune.json is the `book` member of the /api/releases
		// capture, which is the same object this endpoint returns.
		"GET /api/metadata/book/openlibrary/OL893414W": {File: "metadata-book-dune.json"},
	})
	got, err := shelfmark.New(f).Series(context.Background(), source(), duneBookID)
	if err != nil {
		t.Fatalf("Series() = %v", err)
	}
	if got.ID != duneBookID {
		t.Errorf("ID = %q, want %q", got.ID, duneBookID)
	}
	if got.Title != "Dune" {
		t.Errorf("Title = %q, want %q", got.Title, "Dune")
	}
	if len(got.Authors) != 1 || got.Authors[0] != "Frank Herbert" {
		t.Errorf("Authors = %v, want [Frank Herbert]", got.Authors)
	}
	if !strings.Contains(got.Description, "Arrakis") {
		t.Errorf("Description = %.60q…, want the book's blurb", got.Description)
	}
	if len(got.Genres) == 0 {
		t.Error("Genres is empty; the capture carries five")
	}
	// A book is not serialised, so no status is the only truthful status.
	if got.Status != theme.StatusUnknown {
		t.Errorf("Status = %q, want unknown", got.Status)
	}
}

func TestSeriesRejectsAReleaseID(t *testing.T) {
	// No routes: reaching the network at all would be the bug.
	f := themetest.New(t, nil)
	if _, err := shelfmark.New(f).Series(context.Background(), source(), duneRelease); err == nil {
		t.Fatal("Series() accepted a release id")
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("made %d requests for an id it should have rejected outright", n)
	}
}

// --- Releases as chapters --------------------------------------------------

// The filter, which is the decision this theme most needs to get right: the
// reMarkable opens epub and pdf, and offering a user an azw3 they cannot read
// is worse than telling them it exists elsewhere.
func TestReleaseListKeepsOnlyWhatTheDeviceCanOpen(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases": {File: "releases-dune.json"},
	})
	list, err := shelfmark.New(f).ReleaseList(context.Background(), source(), duneBookID)
	if err != nil {
		t.Fatalf("ReleaseList() = %v", err)
	}

	// The capture holds 50 releases: 34 epub, 10 mobi, 5 azw3, 1 fb2.
	if got := len(list.Chapters); got != 34 {
		t.Fatalf("kept %d releases, want 34", got)
	}
	if list.Dropped != 16 {
		t.Errorf("Dropped = %d, want 16", list.Dropped)
	}
	// The count is not enough on its own: the UI has to be able to say *what*
	// was hidden, or "16 hidden" reads as a malfunction.
	want := []string{"azw3", "fb2", "mobi"}
	if strings.Join(list.DroppedFormats, ",") != strings.Join(want, ",") {
		t.Errorf("DroppedFormats = %v, want %v", list.DroppedFormats, want)
	}
	if len(list.SourcesSearched) != 1 || list.SourcesSearched[0] != "direct_download" {
		t.Errorf("SourcesSearched = %v, want [direct_download]", list.SourcesSearched)
	}
	if list.Book.Title != "Dune" {
		t.Errorf("Book.Title = %q, want Dune", list.Book.Title)
	}

	// Everything kept must be addressable and openable.
	for _, ch := range list.Chapters {
		if !strings.HasPrefix(ch.ID, "/release/openlibrary/OL893414W/") {
			t.Fatalf("chapter id %q is not a release of the book asked for", ch.ID)
		}
		if !strings.HasPrefix(ch.Title, "EPUB") && !strings.HasPrefix(ch.Title, "PDF") {
			t.Errorf("chapter title %q does not lead with a format the device can open", ch.Title)
		}
		if ch.Number != -1 {
			t.Errorf("chapter %q carries number %v; a release has no chapter number", ch.Title, ch.Number)
		}
	}

	// The release list is a set of alternatives, so there is no reading order
	// and the theme must say so rather than implying one.
	if theme.OrderIsKnown(list.Chapters) {
		t.Error("the release list claims a reading order; releases are alternatives, not a sequence")
	}
}

// A release title is what the user picks a *format and source* from, so all
// three have to be in it — and the release's own name too, because the sources
// match loosely: the first result for "Dune" in the capture is a book about
// Dune, not Dune.
func TestReleaseTitlesAreChoosable(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases": {File: "releases-dune.json"},
	})
	chs, err := shelfmark.New(f).Chapters(context.Background(), source(), duneBookID)
	if err != nil {
		t.Fatal(err)
	}

	var found string
	for _, ch := range chs {
		if strings.Contains(ch.Title, relTitleText) {
			found = ch.Title
		}
		// content_type arrives with an emoji prefix and this device draws a
		// glyph it has no font for as a tofu box, so no title may carry one.
		for _, r := range ch.Title {
			if r >= 0x1f000 {
				t.Fatalf("title %q carries U+%04X; the reMarkable renders it as tofu", ch.Title, r)
			}
		}
	}
	if found == "" {
		t.Fatalf("no chapter mentions %q; the user cannot see what they are choosing", relTitleText)
	}
	for _, want := range []string{"EPUB", "0.4MB", "Direct Download", relTitleText} {
		if !strings.Contains(found, want) {
			t.Errorf("title %q does not mention %q", found, want)
		}
	}
}

// The order means nothing, but it must not *change*: a list that shuffles
// between two calls is a list the user cannot point at.
func TestReleaseOrderIsDeterministic(t *testing.T) {
	run := func() []string {
		f := themetest.New(t, map[string]themetest.Route{
			"GET /api/releases": {File: "releases-dune.json"},
		})
		chs, err := shelfmark.New(f).Chapters(context.Background(), source(), duneBookID)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, ch := range chs {
			ids = append(ids, ch.ID)
		}
		return ids
	}
	first, second := run(), run()
	if strings.Join(first, "\n") != strings.Join(second, "\n") {
		t.Error("two identical calls produced two different orders")
	}

	// Smallest first within a format — the device has little storage and a
	// slow link, so the smaller copy is the one to try.
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases": {File: "releases-dune.json"},
	})
	chs, err := shelfmark.New(f).Chapters(context.Background(), source(), duneBookID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chs[0].Title, "0.2MB") {
		t.Errorf("first chapter is %q, want the smallest (0.2MB) first", chs[0].Title)
	}
}

// The device has no emoji font, so a glyph it does not know is drawn as a tofu
// box. content_type arrives with one on the front of every value, and the
// three below are verbatim from the capture — non-breaking space included,
// which is the part that is easy to miss.
//
// This is tested directly rather than through a release title because the
// title only ever shows the parenthesised qualifier: an assertion there cannot
// tell "the emoji was stripped" from "the emoji was never looked at".
func TestCleanContentType(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"\U0001F4D5 book (fiction)", "book (fiction)"},
		{"\U0001F4D7 book (unknown)", "book (unknown)"},
		{"\U0001F4D8 book (non-fiction)", "book (non-fiction)"},
		{"book (fiction)", "book (fiction)"},
		{"", ""},
		{"\U0001F4D5", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := shelfmark.CleanContentType(tc.in); got != tc.want {
				t.Errorf("CleanContentType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// And the claim that matters, over the real values: nothing survives that
	// the device has no font for.
	for _, in := range []string{"\U0001F4D5 book (fiction)", "\U0001F4D7 book (unknown)"} {
		for _, r := range shelfmark.CleanContentType(in) {
			if r >= 0x1f000 || r == 0x00a0 {
				t.Errorf("CleanContentType(%q) left U+%04X", in, r)
			}
		}
	}
}

// --- Pages -----------------------------------------------------------------

// The one method this theme cannot implement, and the failure mode worth a
// test of its own: nil, nil would read as "this chapter has no pages", which
// is a different and false claim about a file that is sitting right there.
func TestPagesErrorsRatherThanReturningNothing(t *testing.T) {
	pages, err := shelfmark.New(nil).Pages(context.Background(), source(), duneRelease)
	if err == nil {
		t.Fatal("Pages() returned no error; an empty result reads as a broken chapter")
	}
	if !errors.Is(err, shelfmark.ErrNotPageBased) {
		t.Errorf("Pages() error = %v, want ErrNotPageBased so a caller can tell the two apart", err)
	}
	if pages != nil {
		t.Errorf("Pages() = %v, want nil", pages)
	}
}

// --- The ids ---------------------------------------------------------------

func TestIDsRoundTrip(t *testing.T) {
	// Round-tripping is asserted through the public surface, which is the only
	// place it matters: a stub's ID must be accepted by Series, and a
	// chapter's ID by Retrieve's parser.
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/metadata/search":                     {File: "metadata-search-dune.json"},
		"GET /api/metadata/book/openlibrary/OL893414W": {File: "metadata-book-dune.json"},
		"GET /api/releases":                            {File: "releases-dune.json"},
	})
	th := shelfmark.New(f)
	ctx := context.Background()

	stubs, err := th.Search(ctx, source(), "dune", 1)
	if err != nil {
		t.Fatal(err)
	}
	series, err := th.Series(ctx, source(), stubs[0].ID)
	if err != nil {
		t.Fatalf("Series(%q) = %v; a search result's id must be accepted as it stands", stubs[0].ID, err)
	}
	if series.ID != stubs[0].ID {
		t.Errorf("Series().ID = %q, want %q", series.ID, stubs[0].ID)
	}

	chs, err := th.Chapters(ctx, source(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if chs[0].ID != duneRelease && !strings.HasPrefix(chs[0].ID, "/release/openlibrary/OL893414W/direct_download/") {
		t.Errorf("chapter id %q is not a release of the series it came from", chs[0].ID)
	}
}

// The ids are composite and percent-escaped, so the parser has to refuse the
// shapes that would otherwise re-cut into something plausible.
func TestIDsRejectTheWrongShapes(t *testing.T) {
	f := themetest.New(t, nil)
	th := shelfmark.New(f)
	ctx := context.Background()

	bad := []string{
		"",
		"   ",
		"OL893414W",
		"/book/openlibrary",
		"/book/openlibrary/OL893414W/extra",
		"/book//OL893414W",
		"/release/openlibrary/OL893414W",
	}
	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			if _, err := th.Series(ctx, source(), id); err == nil {
				t.Errorf("Series(%q) was accepted", id)
			}
		})
	}

	// A provider id with a slash in it must survive the round trip rather than
	// being re-cut into two segments. Nothing in the capture has one; that is
	// exactly why it is worth pinning, because the day one appears is the day
	// the id silently addresses a different book.
	t.Run("an escaped slash survives", func(t *testing.T) {
		slashed := themetest.New(t, map[string]themetest.Route{
			"GET /api/metadata/book/openlibrary/OL%2F893414W": {File: "metadata-book-dune.json"},
		})
		if _, err := shelfmark.New(slashed).Series(ctx, source(), "/book/openlibrary/OL%2F893414W"); err != nil {
			t.Fatalf("Series() = %v; the escaped slash should have been unescaped once and re-escaped once", err)
		}
	})
}

// --- The 200-with-an-error body -------------------------------------------

// The trap this API sets: an unknown path answers {"error":"Resource not
// found"} with HTTP 200. A theme that trusted the status code would parse that
// as an empty-but-successful result and tell the user their search found
// nothing, for what is really a wrong base URL.
func TestResourceNotFoundIsAnErrorDespiteHTTP200(t *testing.T) {
	ctx := context.Background()
	notFound := themetest.Route{File: "not-found.json", Status: http.StatusOK}

	t.Run("search", func(t *testing.T) {
		f := themetest.New(t, map[string]themetest.Route{"GET /api/metadata/search": notFound})
		got, err := shelfmark.New(f).Search(ctx, source(), "dune", 1)
		if err == nil {
			t.Fatalf("Search() = %v, nil; want an error", got)
		}
		if !strings.Contains(err.Error(), "Resource not found") {
			t.Errorf("error = %v, want it to quote the instance's message", err)
		}
	})
	t.Run("book", func(t *testing.T) {
		f := themetest.New(t, map[string]themetest.Route{"GET /api/metadata/book/openlibrary/OL893414W": notFound})
		if _, err := shelfmark.New(f).Series(ctx, source(), duneBookID); err == nil {
			t.Error("Series() accepted an error body")
		}
	})
	t.Run("releases", func(t *testing.T) {
		f := themetest.New(t, map[string]themetest.Route{"GET /api/releases": notFound})
		if _, err := shelfmark.New(f).Chapters(ctx, source(), duneBookID); err == nil {
			t.Error("Chapters() accepted an error body")
		}
	})
}

// --- Retrieve --------------------------------------------------------------

// statusBodies builds the sequence /api/status answers during one download,
// out of the real capture: the same record, first in the `downloading` bucket
// and then in `complete`, keyed by the release's source_id.
//
// It is derived rather than hand-written so the shape stays the instance's.
// The capture's id was redacted to 32 zeros, so it is swapped for the id the
// theme will actually poll for.
func statusBodies(t *testing.T, id string) (pending, complete []byte) {
	t.Helper()
	raw := strings.ReplaceAll(string(readFixture(t, "status.json")), "00000000000000000000000000000000", id)

	var m map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	complete, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	entry := m["complete"][id]
	if entry == nil {
		t.Fatalf("the status capture has no complete entry for %s", id)
	}
	m["downloading"] = map[string]json.RawMessage{id: entry}
	m["complete"] = map[string]json.RawMessage{}
	pending, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return pending, complete
}

// pollingFetcher answers /api/status from a script and everything else from
// the fixtures, so the poll loop can be driven through more than one state
// without a second capture.
type pollingFetcher struct {
	*themetest.Fetcher
	bodies [][]byte
	polls  int
}

func (f *pollingFetcher) Get(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	if strings.Contains(rawurl, "/api/status") {
		i := f.polls
		if i >= len(f.bodies) {
			i = len(f.bodies) - 1
		}
		f.polls++
		u, _ := url.Parse(rawurl)
		return &fetch.Response{StatusCode: 200, Header: http.Header{}, Body: f.bodies[i], FinalURL: u, Attempts: 1}, nil
	}
	return f.Fetcher.Get(ctx, p, rawurl)
}

// fakeClock is a clock that only moves when the code under test sleeps, so a
// fifteen-minute timeout can be tested in microseconds. Nothing here waits.
type fakeClock struct {
	now    time.Time
	slept  []time.Duration
	onWake func()
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
	if c.onWake != nil {
		c.onWake()
	}
	return nil
}

func TestRetrievePollsAndReturnsTheFileURL(t *testing.T) {
	pending, complete := statusBodies(t, relSourceID)
	fixtures := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases": {File: "releases-dune.json"},
		// The acknowledgement was not captured, so it is served empty here on
		// purpose: that is the path the theme actually relies on, where the
		// download is polled for under the release's own source_id.
		"POST /api/releases/download": {Body: `{}`},
	})
	f := &pollingFetcher{Fetcher: fixtures, bodies: [][]byte{pending, pending, complete}}
	clock := &fakeClock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}

	var notes []string
	th := shelfmark.NewWithClock(f, clock.Now, clock.Sleep)
	fileURL, name, err := th.Retrieve(context.Background(), source(), duneRelease, func(n string) {
		notes = append(notes, n)
	})
	if err != nil {
		t.Fatalf("Retrieve() = %v", err)
	}

	wantURL := base + "/api/localdownload?id=" + relSourceID
	if fileURL != wantURL {
		t.Errorf("fileURL = %q, want %q", fileURL, wantURL)
	}
	// The name the document gets, taken from the instance's download_path —
	// its basename, never its directory.
	if want := "An Example Book - Example Author (1970)_1.epub"; name != want {
		t.Errorf("filename = %q, want %q", name, want)
	}

	if f.polls != 3 {
		t.Errorf("polled %d times, want 3 (two pending, then complete)", f.polls)
	}
	if len(clock.slept) != 2 {
		t.Fatalf("slept %d times, want 2 (once between each pair of polls)", len(clock.slept))
	}
	for _, d := range clock.slept {
		// Generous on purpose: hammering a box on the user's own network buys
		// nothing, and the fetch layer's floor is 2s anyway.
		if d < 2*time.Second {
			t.Errorf("poll interval %v is below the fetch layer's own politeness floor", d)
		}
	}
	if len(notes) == 0 {
		t.Error("Retrieve() reported no progress; the user is staring at this for minutes")
	}

	// The POST must carry the chosen release back verbatim: the instance needs
	// the `extra` block to do the fetch, and re-encoding our own struct would
	// drop it.
	var posted map[string]any
	for _, c := range f.Calls() {
		if c.Method == http.MethodPost && strings.HasSuffix(c.URL, "/api/releases/download") {
			if err := json.Unmarshal(c.JSON, &posted); err != nil {
				t.Fatalf("the posted body is not JSON: %v", err)
			}
		}
	}
	if posted == nil {
		t.Fatal("Retrieve() never posted to /api/releases/download")
	}
	if posted["source_id"] != relSourceID {
		t.Errorf("posted source_id = %v, want %s", posted["source_id"], relSourceID)
	}
	if _, ok := posted["extra"]; !ok {
		t.Error("the posted release lost its `extra` block; the instance needs it to fetch the file")
	}
}

// An acknowledgement that names its own id is believed over the source_id
// fallback, because it can only be more authoritative.
func TestRetrievePrefersTheIDTheInstanceReturns(t *testing.T) {
	const ackID = "1111222233334444aaaabbbbccccdddd"
	pending, complete := statusBodies(t, ackID)
	b := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases":           {File: "releases-dune.json"},
		"POST /api/releases/download": {Body: `{"id":"` + ackID + `"}`},
	})
	f := &pollingFetcher{Fetcher: b, bodies: [][]byte{pending, complete}}
	clock := &fakeClock{now: time.Now()}

	fileURL, _, err := shelfmark.NewWithClock(f, clock.Now, clock.Sleep).
		Retrieve(context.Background(), source(), duneRelease, nil)
	if err != nil {
		t.Fatalf("Retrieve() = %v", err)
	}
	if !strings.HasSuffix(fileURL, "id="+ackID) {
		t.Errorf("fileURL = %q, want it to use the id the instance acknowledged", fileURL)
	}
}

// A download that never finishes must give up eventually, and must say
// something the user can act on rather than just failing.
func TestRetrieveGivesUpEventually(t *testing.T) {
	pending, _ := statusBodies(t, relSourceID)
	b := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases":           {File: "releases-dune.json"},
		"POST /api/releases/download": {Body: `{}`},
	})
	f := &pollingFetcher{Fetcher: b, bodies: [][]byte{pending}}
	clock := &fakeClock{now: time.Now()}

	_, _, err := shelfmark.NewWithClock(f, clock.Now, clock.Sleep).
		Retrieve(context.Background(), source(), duneRelease, nil)
	if err == nil {
		t.Fatal("Retrieve() waited forever")
	}
	if !strings.Contains(err.Error(), "gave up waiting") {
		t.Errorf("error = %v, want it to say it gave up", err)
	}

	// The patience is the feature: the instance's own release_search_timeout
	// is 300s and it tries several sources in turn, so anything close to that
	// would abandon downloads that were working.
	var total time.Duration
	for _, d := range clock.slept {
		total += d
	}
	if total < 10*time.Minute {
		t.Errorf("gave up after %v; the instance's own search timeout alone is 300s", total)
	}
}

// A cancelled context stops the poll loop, and stops it *before* another
// request rather than after one.
func TestRetrieveHonoursCancellation(t *testing.T) {
	pending, _ := statusBodies(t, relSourceID)
	b := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases":           {File: "releases-dune.json"},
		"POST /api/releases/download": {Body: `{}`},
	})
	f := &pollingFetcher{Fetcher: b, bodies: [][]byte{pending}}
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Now(), onWake: cancel}

	_, _, err := shelfmark.NewWithClock(f, clock.Now, clock.Sleep).
		Retrieve(ctx, source(), duneRelease, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Retrieve() = %v, want context.Canceled", err)
	}
	// Exactly one poll: the cancel lands during the first sleep, and the check
	// at the top of the loop must stop the next request from being made at
	// all. Two would mean the loop noticed only when it tried to sleep again.
	if f.polls != 1 {
		t.Errorf("polled %d times, want 1; the context check belongs at the top of the loop", f.polls)
	}
}

// A release the instance no longer offers is an ordinary outcome — sources
// come and go between listing and tapping — and must be reported as that.
func TestRetrieveRejectsAReleaseThatIsGone(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases": {File: "releases-dune.json"},
	})
	gone := "/release/openlibrary/OL893414W/direct_download/ffffffffffffffffffffffffffffffff"
	_, _, err := shelfmark.New(f).Retrieve(context.Background(), source(), gone, nil)
	if err == nil {
		t.Fatal("Retrieve() accepted a release that is not in the list")
	}
	if !strings.Contains(err.Error(), "no longer among the releases") {
		t.Errorf("error = %v, want it to explain that the release is gone", err)
	}
	for _, c := range f.Calls() {
		if c.Method == http.MethodPost {
			t.Error("Retrieve() posted a download for a release it could not find")
		}
	}
}

// --- The theme's answers about itself --------------------------------------

func TestThemeDeclarations(t *testing.T) {
	th := shelfmark.New(nil)
	if th.ID() != "shelfmark" {
		t.Errorf("ID() = %q", th.ID())
	}
	if th.SuggestedName() != "Shelfmark" {
		t.Errorf("SuggestedName() = %q", th.SuggestedName())
	}
	// Everything is fetched from the instance itself — covers are proxied
	// through /api/covers and the file comes from /api/localdownload — so
	// there is no host to declare, and declaring one would widen PLAN §7.4's
	// boundary for a request that is never made.
	if got := th.AllowedHosts(); got != nil {
		t.Errorf("AllowedHosts() = %v, want nil", got)
	}
	// A watch check must never be able to reach Retrieve.
	if _, ok := any(th).(theme.DiscoveryClassifier); !ok {
		t.Error("the theme that starts downloads does not implement DiscoveryClassifier")
	}
	if _, ok := any(th).(theme.FileTheme); !ok {
		t.Error("the theme whose chapters are files does not implement FileTheme")
	}
}

// The real acknowledgement, captured from a live instance on 2026-09-20, names
// no id — so polling falls back to the release's source_id, and that fallback
// is the only path this build ever takes. Pinned here because the code keeps an
// ack.ID branch that nothing currently exercises, and a reader should be able
// to see why it is there without going to the server to find out.
func TestTheDownloadAcknowledgementCarriesNoID(t *testing.T) {
	raw := readFixture(t, "download-ack.json")

	var ack struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Priority int    `json:"priority"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("the captured acknowledgement is not JSON: %v", err)
	}
	if ack.ID != "" {
		t.Errorf("the acknowledgement now carries an id (%q) — the ack.ID branch in "+
			"Retrieve is live after all, and its comment is out of date", ack.ID)
	}
	if ack.Status != "queued" {
		t.Errorf("status = %q, want %q", ack.Status, "queued")
	}
}

// --- how long Quire is prepared to wait ------------------------------------

// The release search is the one call a Shelfmark instance spends minutes on:
// it asks each of its sources in turn. Measured at 36 seconds against an
// instance searching one source (2026-09-20), and its own
// `release_search_timeout` is 300 seconds.
//
// The client's ordinary transport gives up after 30 seconds *waiting for the
// first byte*, which is exactly the wait here — the server sends nothing while
// it is searching. So this call has to ask for the long-timeout path, and the
// fast ones must not: a source that is merely broken should fail in seconds,
// not in six minutes.
func TestOnlyTheReleaseSearchAsksForTheLongTimeout(t *testing.T) {
	pending, complete := statusBodies(t, relSourceID)
	fixtures := themetest.New(t, map[string]themetest.Route{
		"GET /api/metadata/search":                     {File: "metadata-search-dune.json"},
		"GET /api/metadata/book/openlibrary/OL893414W": {File: "metadata-book-dune.json"},
		"GET /api/releases":                            {File: "releases-dune.json"},
		"GET /api/config":                              {File: "config.json"},
		"POST /api/releases/download":                  {Body: `{}`},
	})
	f := &pollingFetcher{Fetcher: fixtures, bodies: [][]byte{pending, complete}}
	clock := &fakeClock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	th := shelfmark.NewWithClock(f, clock.Now, clock.Sleep)

	ctx := context.Background()
	if _, err := th.Search(ctx, source(), "dune", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Series(ctx, source(), duneBookID); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Chapters(ctx, source(), duneBookID); err != nil {
		t.Fatal(err)
	}
	if err := th.Confirm(ctx, source()); err != nil {
		t.Fatal(err)
	}

	slow := map[string]bool{}
	for _, c := range fixtures.Calls() {
		u, err := url.Parse(c.URL)
		if err != nil {
			t.Fatal(err)
		}
		if c.Slow {
			slow[u.EscapedPath()] = true
		} else if _, seen := slow[u.EscapedPath()]; !seen {
			slow[u.EscapedPath()] = false
		}
	}
	if !slow["/api/releases"] {
		t.Error("the release search used the ordinary timeout; on a real instance it dies at 30 seconds")
	}
	for path, isSlow := range slow {
		if path == "/api/releases" || !isSlow {
			continue
		}
		t.Errorf("%s asked for the six-minute bound; only the call the server is working on should",
			path)
	}
}

// The documented fallback. PLAN §12.2's watch check runs this theme through
// theme.DiscoveryFetcher, which exposes only theme.Fetcher's methods — so the
// long-timeout path is not available there. The release search must still work:
// refusing would break the watch check on a source that works perfectly, which
// is a real failure traded for a hypothetical one.
func TestTheReleaseSearchStillWorksWithoutTheSlowPath(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /api/releases": {File: "releases-dune.json"},
	})
	th := shelfmark.New(theme.DiscoveryFetcher(f))

	chs, err := th.Chapters(context.Background(), source(), duneBookID)
	if err != nil {
		t.Fatalf("Chapters() = %v", err)
	}
	if len(chs) != 34 {
		t.Errorf("got %d releases, want the 34 the capture keeps", len(chs))
	}
	for _, c := range f.Calls() {
		if c.Slow {
			t.Error("a fetcher with no slow path reported making a slow request")
		}
	}
}
