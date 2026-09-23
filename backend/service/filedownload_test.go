package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/shelf"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Downloading from a theme.FileTheme, through the whole service (2026-09-20).
//
// The claim being tested is a *negative* one as much as a positive: a book must
// not go near the image pipeline. Measured on the device, an epub POSTed to
// /upload answers 201 and lands with fileType "epub" for the stock reader to
// paginate — so fetching page images, resizing them and building a PDF would
// not merely be wasted work, it would produce the wrong file. Every assertion
// about what was *not* done is there on purpose.

const (
	bookThemeID = "bookish"

	// The release the tests download, and the name the source gives its file.
	// The name is the shape a real instance produced (author and year, and an
	// extension that is not .pdf), because the extension surviving the upload
	// is the whole point of the second test.
	bookReleaseID = "/release/openlibrary/OL1W/direct_download/abc123"
	bookFilename  = "An Example Book - Example Author (1970).epub"
)

// bookEPUB is what the source hands over: a zip, which is what an epub is.
// Not a PDF, and not an image — if either of those would have done, the test
// would not be about anything.
var bookEPUB = []byte("PK\x03\x04epub bytes that are nobody's business but the reader's")

// bookTheme is a theme.FileTheme: it searches books, lists releases, has no
// pages at all, and retrieves a finished file.
//
// It is a stub rather than the real shelfmark theme because what is under test
// is the *service's* branch — which theme produced the URL is exactly the thing
// this path is supposed to not care about. shelfmark's own behaviour is pinned
// offline in its package.
type bookTheme struct {
	mu sync.Mutex

	// fileURL is what Retrieve hands back for the caller to fetch.
	fileURL  string
	filename string

	// title overrides the one book this source has. A test sets it to the
	// manga fixture's title to force the two sources into one group.
	title string

	// authors overrides the one book's author list, nil by default so most
	// tests here exercise the "no authors" shape most themes actually have.
	authors []string

	// notes are the progress notes Retrieve reports, in order.
	notes []string

	// hold, when non-nil, makes Retrieve block until it is closed or the
	// context ends — which is how a test cancels *during* the retrieval, the
	// minutes-long window where a user actually reaches for Stop.
	hold    chan struct{}
	started chan struct{}

	// pagesAsked counts calls to Pages. It must stay at zero: a file theme has
	// no page images, and asking for them is the bug this file exists to catch.
	pagesAsked int

	// chaptersAsked counts calls to Chapters — the release search, which on a
	// real instance costs 36 seconds. Once per download is one too many; the
	// download path gets its releases from Retrieve, which reads them fresh.
	chaptersAsked int

	// retrieved is every chapter id Retrieve was asked for.
	retrieved []string

	// headers, when non-nil, makes bookTheme implement theme.SourceHeaders —
	// standing in for a theme like globalcomix whose source needs a static
	// header attached to its own requests, including the file fetch
	// fetchFile performs.
	headers http.Header

	// useCookies, when true, makes bookTheme implement theme.CookieUser.
	useCookies bool
}

func (b *bookTheme) SourceHeaders(*theme.Source) http.Header { return b.headers }
func (b *bookTheme) UsesCookies(*theme.Source) bool          { return b.useCookies }

func (b *bookTheme) ID() string                             { return bookThemeID }
func (b *bookTheme) Fingerprint(*probe.Page) int            { return 0 }
func (b *bookTheme) AllowedHosts() []string                 { return nil }
func (b *bookTheme) SuggestedName() string                  { return "Example Books" }
func (b *bookTheme) ValidateOverrides(map[string]any) error { return nil }
func (b *bookTheme) OverrideKeys() []theme.OverrideDoc      { return nil }

// bookTitle is the one book this source has. searchAll's grouping is on the
// title, so the mixed-group test leans on it being distinct from the manga
// fixture's.
const bookTitle = "An Example Book"

func (b *bookTheme) bookTitle() string {
	if b.title != "" {
		return b.title
	}
	return bookTitle
}

func (b *bookTheme) Search(context.Context, *theme.Source, string, int) ([]theme.SeriesStub, error) {
	return []theme.SeriesStub{{ID: "/book/openlibrary/OL1W", Title: b.bookTitle(), Authors: b.authors}}, nil
}

func (b *bookTheme) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return &theme.Series{ID: "/book/openlibrary/OL1W", Title: b.bookTitle()}, nil
}

// Chapters are releases: alternatives to each other, so the list is marked
// order-unknown exactly as the real one is.
func (b *bookTheme) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	b.mu.Lock()
	b.chaptersAsked++
	b.mu.Unlock()
	return theme.SortAndMark([]theme.Chapter{
		{ID: bookReleaseID, Title: "EPUB · 1.7MB · Direct Download — An Example Book", Number: -1},
	}), nil
}

// Pages errors, as theme.FileTheme requires: an empty slice would read as "this
// chapter has no pages", which is a different and false claim.
func (b *bookTheme) Pages(context.Context, *theme.Source, string) ([]string, error) {
	b.mu.Lock()
	b.pagesAsked++
	b.mu.Unlock()
	return nil, fmt.Errorf("bookish: %w", errNoPages)
}

var errNoPages = fmt.Errorf("this theme has no page images")

func (b *bookTheme) Retrieve(ctx context.Context, s *theme.Source, chapterID string,
	progress func(string)) (string, string, error) {

	b.mu.Lock()
	b.retrieved = append(b.retrieved, chapterID)
	notes, hold, started := b.notes, b.hold, b.started
	b.mu.Unlock()

	for _, n := range notes {
		if progress != nil {
			progress(n)
		}
	}
	if hold != nil {
		if started != nil {
			close(started)
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-hold:
		}
	}
	return b.fileURL, b.filename, nil
}

func (b *bookTheme) asked() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pagesAsked
}

func (b *bookTheme) listedReleases() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chaptersAsked
}

// bookEnv is one wired-up service with a book source in it.
type bookEnv struct {
	svc      *service.Service
	store    *state.Store
	libStore *library.Store
	fake     *fakeLibrary
	rec      *recorder

	// downloadDir is where the page pipeline would have written, had it run.
	// Kept so a test can assert that it did not.
	downloadDir string

	// fetcher is the guarded client's stand-in. It records every request, so a
	// test can check that the file went through it — which is the whole reason
	// theme.FileTheme hands back a URL rather than bytes.
	fetcher *themetest.Fetcher

	// savedDir and shelfStore let a test check a book that landed in Quire's
	// own storage rather than the library.
	savedDir   string
	shelfStore *shelf.Store
}

// newBookService is newDownloadService with the book source added, so every
// test here can compare the two paths in one process.
func newBookService(t *testing.T, th *bookTheme) bookEnv {
	t.Helper()

	if th.fileURL == "" {
		th.fileURL = "https://books.example.invalid/api/localdownload?id=abc123"
	}
	if th.filename == "" {
		th.filename = bookFilename
	}

	routes := downloadRoutes(t)
	// The file, served from the source's own host and fetched through the
	// guarded client like any page image.
	routes["GET /api/localdownload"] = themetest.Route{Body: string(bookEPUB)}

	dir := ""
	saved := ""
	var fetcher *themetest.Fetcher
	var shelfStore *shelf.Store
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, routes, func(o *service.Options) {
		o.Registry.MustRegister(th)
		dir = o.DownloadDir
		saved = o.SavedDir
		fetcher = o.Fetcher.(*themetest.Fetcher)
		shelfStore = o.ShelfStore
	})
	if _, err := store.Add(&theme.Source{
		Name: "Example Books", Lang: "en", Theme: bookThemeID,
		BaseURL: "https://books.example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	return bookEnv{svc: svc, store: store, libStore: libStore, fake: fake, rec: rec,
		downloadDir: dir, fetcher: fetcher, savedDir: saved, shelfStore: shelfStore}
}

// downloadTheBook enqueues the one release and waits for the given phase.
func downloadTheBook(t *testing.T, svc *service.Service, rec *recorder) {
	t.Helper()
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`","confirmed":true,"destination":"library"}`)
}

// The path, end to end: the file the source produced is what lands in the
// library, and nothing was built on the way.
func TestABookIsUploadedWithoutTheImagePipeline(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)

	// Add a Books folder to the fake library for this test.
	env.fake.mu.Lock()
	env.fake.entries = append(env.fake.entries, library.Entry{
		ID: "books", Parent: "", Type: library.Collection, VisibleName: "Books",
	})
	env.fake.mu.Unlock()

	downloadTheBook(t, env.svc, env.rec)
	done := waitForPhase(t, env.rec, "done")

	env.fake.mu.Lock()
	uploads := append([][]byte(nil), env.fake.uploaded...)
	names := append([]string(nil), env.fake.names...)
	entries := append([]library.Entry(nil), env.fake.entries...)
	env.fake.mu.Unlock()

	if len(uploads) != 1 {
		t.Fatalf("%d uploads, want 1", len(uploads))
	}
	// Byte for byte. Anything else means something re-encoded a finished
	// document — the one thing this path exists to avoid.
	if !bytes.Equal(uploads[0], bookEPUB) {
		t.Errorf("uploaded %q, want the file the source handed over", uploads[0])
	}
	if names[0] != bookFilename {
		t.Errorf("uploaded as %q, want %q", names[0], bookFilename)
	}

	// The image pipeline was not merely unused, it was never asked.
	if th.asked() != 0 {
		t.Errorf("the download asked a file theme for page images %d time(s)", th.asked())
	}
	// The bytes came through the guarded client, classified as retrieval — one
	// file the user named. This is what keeps the SSRF guard, the rate limit
	// and the size cap on the transfer (theme.FileTheme's "why a URL").
	fetched := false
	for _, c := range env.fetcher.Calls() {
		if !strings.Contains(c.URL, "/api/localdownload") {
			continue
		}
		fetched = true
		if c.Kind != fetch.KindRetrieval {
			t.Errorf("the file was fetched as %v, want a retrieval", c.Kind)
		}
		// Through GetFileRetrieval, not the page-image path. That is what
		// raises the response cap from 8 MiB to the upload budget and gives the
		// transfer minutes rather than 60 seconds; an ordinary retrieval would
		// refuse a scanned PDF at the end of the download.
		if !c.Slow {
			t.Error("the book was fetched on the page-image path, which caps it at 8 MiB")
		}
	}
	if !fetched {
		t.Error("the file was never fetched through the guarded client")
	}
	// And it wrote nothing: no page files, no assembled PDF. The download
	// directory for this source should not exist at all.
	dir := filepath.Join(env.downloadDir, "example-books")
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("%s exists; a book download wrote page files or a PDF", dir)
	}

	// The library record, which is what the "Read" button and the downloaded
	// overview are built from.
	recs := env.libStore.List()
	if len(recs) != 1 {
		t.Fatalf("%d records, want 1", len(recs))
	}
	got := recs[0]
	if got.DocumentUUID == "" || got.DocumentUUID != done["documentUuid"] {
		t.Errorf("record uuid %q, done message said %v", got.DocumentUUID, done["documentUuid"])
	}
	if len(got.Chapters) != 1 || got.Chapters[0] != bookReleaseID {
		t.Errorf("record chapters = %v, want the release that was downloaded", got.Chapters)
	}
	if got.PDF != "" {
		t.Errorf("record names a PDF at %q; nothing of Quire's is on disk", got.PDF)
	}
	if got.Bytes != int64(len(bookEPUB)) {
		t.Errorf("record says %d bytes, want %d", got.Bytes, len(bookEPUB))
	}
	if got.SeriesTitle != bookTitle {
		t.Errorf("record series title %q, want %q", got.SeriesTitle, bookTitle)
	}

	// A book is filed flat into Books, not into Comics and not into a per-title subfolder.
	parent := ""
	for _, e := range entries {
		if e.ID == got.DocumentUUID {
			parent = e.Parent
		}
	}
	if parent != "books" {
		t.Errorf("landed in %q, want the Books folder", parent)
	}
}

// The extension is the difference between a book the device paginates and a
// document it treats as a PDF. xochitl appends ".pdf" only when a name has no
// extension at all (library.Upload's comment, measured 2026-09-20), so the one
// thing Quire must not do is help.
func TestABooksExtensionSurvivesTheUpload(t *testing.T) {
	for _, name := range []string{
		"An Example Book - Example Author (1970).epub",
		"Another Example.pdf",
	} {
		t.Run(name, func(t *testing.T) {
			th := &bookTheme{filename: name}
			env := newBookService(t, th)

			downloadTheBook(t, env.svc, env.rec)
			waitForPhase(t, env.rec, "done")

			env.fake.mu.Lock()
			defer env.fake.mu.Unlock()
			if len(env.fake.names) != 1 {
				t.Fatalf("%d uploads, want 1", len(env.fake.names))
			}
			if env.fake.names[0] != name {
				t.Errorf("uploaded as %q, want %q — the name was rewritten on the way",
					env.fake.names[0], name)
			}
			// And the device's own view of it: an epub must arrive as an epub.
			want := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
			for _, e := range env.fake.entries {
				if e.VisibleName == name && e.FileType != want {
					t.Errorf("the reMarkable filed it as %q, want %q", e.FileType, want)
				}
			}
		})
	}
}

// A release search takes minutes — the instance's own timeout is 300s per
// source and it tries several. The notes are the only thing between the user
// and a bar that looks frozen, so they have to arrive as they happen.
func TestABooksProgressNotesReachTheUser(t *testing.T) {
	th := &bookTheme{notes: []string{
		"asking Shelfmark which sources have this book",
		"looking for a source",
		"downloading — 31%",
	}}
	env := newBookService(t, th)

	downloadTheBook(t, env.svc, env.rec)
	waitForPhase(t, env.rec, "done")

	var messages []string
	for _, m := range progressOf(t, env.rec) {
		messages = append(messages, fmt.Sprint(m["message"]))
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{
		// Finished sentences, not the theme's fragments (PLAN §2).
		"Looking for a source…",
		"Downloading — 31%…",
		"Asking Shelfmark which sources have this book…",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no progress line said %q; got:\n%s", want, joined)
		}
	}
	// They must arrive in the order the theme reported them, or the user is
	// watching a shuffled account of what is happening.
	first := strings.Index(joined, "Asking Shelfmark")
	second := strings.Index(joined, "Looking for a source")
	third := strings.Index(joined, "Downloading — 31%")
	if !(first < second && second < third) {
		t.Errorf("progress notes arrived out of order:\n%s", joined)
	}
}

// Stop, while the source is still searching. This is the whole window in which
// a user cancels a book download, so it is the one that has to work.
func TestStoppingABookDuringRetrievalIsHonoured(t *testing.T) {
	th := &bookTheme{hold: make(chan struct{}), started: make(chan struct{})}
	env := newBookService(t, th)
	defer close(th.hold)

	downloadTheBook(t, env.svc, env.rec)

	select {
	case <-th.started:
	case <-time.After(20 * time.Second):
		t.Fatal("the retrieval never started")
	}

	handle(t, env.svc, env.rec, appload.MessageCancelDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`"}`)

	stopped := waitForPhase(t, env.rec, "cancelled")
	if msg := fmt.Sprint(stopped["message"]); !strings.Contains(msg, "Stopped") {
		t.Errorf("the cancelled message reads %q", msg)
	}
	// A book has no partial file, so the message must not promise a resume
	// that cannot happen.
	if strings.Contains(fmt.Sprint(stopped["message"]), "carry on from here") {
		t.Errorf("the cancelled message offers to resume a book download: %v", stopped["message"])
	}

	env.fake.mu.Lock()
	uploads := len(env.fake.uploaded)
	env.fake.mu.Unlock()
	if uploads != 0 {
		t.Errorf("%d uploads after a cancel during retrieval, want 0", uploads)
	}
	if recs := env.libStore.List(); len(recs) != 0 {
		t.Errorf("%d library records after a cancel, want 0", len(recs))
	}
	for _, m := range progressOf(t, env.rec) {
		if m["phase"] == "done" {
			t.Fatalf("a cancelled book download reported done: %v", m["message"])
		}
	}
}

// PLAN §2: the frontend renders, it does not classify. A search result says
// what kind of thing it is, and the answer comes from the theme.
func TestSearchResultsSayWhatKindOfSourceTheyCameFrom(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	addSource(t, env.store) // the manga source

	for _, tc := range []struct{ sourceID, query, want string }{
		{"example-books", "example", "book"},
		{"example-reader", "lantern", "manga"},
	} {
		rec := &recorder{}
		handle(t, env.svc, rec, appload.MessageSearch,
			`{"sourceId":"`+tc.sourceID+`","query":"`+tc.query+`"}`)
		var reply struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Kind != tc.want {
			t.Errorf("%s search said kind %q, want %q", tc.sourceID, reply.Kind, tc.want)
		}
	}
}

// The combined search merges on the title, so a group can hold rows from a book
// service and a comic site at once. It says what it is when its sources agree
// and says nothing when they do not — which is the only honest thing one field
// can say about a mixed group.
func TestCombinedSearchGroupsSayWhatKindTheyAre(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	addSource(t, env.store)

	handle(t, env.svc, env.rec, appload.MessageSearchAll, `{"query":"lantern","page":1,"pageSize":20}`)

	var reply struct {
		Groups []struct {
			Title   string `json:"title"`
			Kind    string `json:"kind"`
			Matches []struct {
				SourceID string `json:"sourceId"`
			} `json:"matches"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageSearchAllResults), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Groups) < 2 {
		t.Fatalf("%d groups, want one per source", len(reply.Groups))
	}
	seen := map[string]string{}
	for _, g := range reply.Groups {
		seen[g.Title] = g.Kind
	}
	if got := seen[bookTitle]; got != "book" {
		t.Errorf("the book group says kind %q, want %q", got, "book")
	}
	if got := seen["The Lantern Keeper"]; got != "manga" {
		t.Errorf("the manga group says kind %q, want %q", got, "manga")
	}
}

// The mixed group. "Dune" is a novel and a manga, and the merge is on the
// title, so one group can hold both. Naming it either is a claim about rows
// that are not that, so it names neither.
func TestAMixedCombinedSearchGroupClaimsNoKind(t *testing.T) {
	th := &bookTheme{title: "The Lantern Keeper"}
	env := newBookService(t, th)
	addSource(t, env.store)

	handle(t, env.svc, env.rec, appload.MessageSearchAll, `{"query":"lantern","page":1,"pageSize":20}`)

	var reply struct {
		Groups []struct {
			Title   string `json:"title"`
			Kind    string `json:"kind"`
			Matches []struct {
				SourceID string `json:"sourceId"`
			} `json:"matches"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageSearchAllResults), &reply); err != nil {
		t.Fatal(err)
	}
	mixed := 0
	for _, g := range reply.Groups {
		if len(g.Matches) < 2 {
			continue
		}
		mixed++
		if g.Kind != "" {
			t.Errorf("a group holding %d sources of different kinds calls itself %q",
				len(g.Matches), g.Kind)
		}
	}
	if mixed != 1 {
		t.Fatalf("%d groups merged the two sources, want 1 — the test is not testing anything",
			mixed)
	}
}

// The downloaded overview is where a user goes back to what they have. A book
// and a manga are different things to open, so each row says which it is — and
// a row whose source is gone says nothing rather than guessing.
func TestTheDownloadedOverviewSaysWhatKindEachRowIs(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	addSource(t, env.store)

	for _, rec := range []library.Record{
		{Key: library.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Volume: "r1"},
			DocumentUUID: "doc-book", SeriesTitle: bookTitle, StoredAt: fixedNow},
		{Key: library.Key{Source: "example-reader", Series: "/manga/the-lantern-keeper/", Volume: "1"},
			DocumentUUID: "doc-manga", SeriesTitle: "The Lantern Keeper", StoredAt: fixedNow},
		{Key: library.Key{Source: "gone", Series: "/whatever/", Volume: "1"},
			DocumentUUID: "doc-orphan", SeriesTitle: "Orphaned", StoredAt: fixedNow},
	} {
		if err := env.libStore.Put(rec); err != nil {
			t.Fatal(err)
		}
	}

	handle(t, env.svc, env.rec, appload.MessageListDownloaded, `{}`)
	var reply struct {
		Series []struct {
			SourceID string `json:"sourceId"`
			Kind     string `json:"kind"`
		} `json:"series"`
	}
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageDownloadedList), &reply); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"example-books": "book", "example-reader": "manga", "gone": ""}
	if len(reply.Series) != len(want) {
		t.Fatalf("%d rows, want %d", len(reply.Series), len(want))
	}
	for _, row := range reply.Series {
		if got := row.Kind; got != want[row.SourceID] {
			t.Errorf("row %s says kind %q, want %q", row.SourceID, got, want[row.SourceID])
		}
	}
}

// The other half of every claim above: a page-based source is untouched by any
// of this. It still fetches images, still builds a PDF, and still uploads one.
func TestAPageBasedDownloadIsUnchangedByTheBookPath(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	addSource(t, env.store)

	seriesID, chapterID := firstChapter(t, env.svc, env.rec)
	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, env.rec, "done")

	env.fake.mu.Lock()
	defer env.fake.mu.Unlock()
	if len(env.fake.uploaded) != 1 {
		t.Fatalf("%d uploads, want 1", len(env.fake.uploaded))
	}
	if !bytes.HasPrefix(env.fake.uploaded[0], []byte("%PDF")) {
		t.Errorf("a manga chapter was uploaded as %q, want a PDF", env.fake.uploaded[0][:8])
	}
	if !strings.HasSuffix(env.fake.names[0], ".pdf") {
		t.Errorf("uploaded as %q, want a .pdf", env.fake.names[0])
	}
	// It went through the assembling phase, which a book never does.
	assembling := false
	for _, m := range progressOf(t, env.rec) {
		if m["phase"] == "assembling" {
			assembling = true
		}
	}
	if !assembling {
		t.Error("a manga download never reported assembling; the page pipeline did not run")
	}
}

// The first tap on a book downloads it, and does not ask.
//
// There is nothing to confirm: one release is one document, so the "this volume
// is ten chapters, download all of them?" question has no meaning here. The
// check is skipped rather than answered, because answering it means listing the
// book's releases — the slow source search — and Retrieve then does it again.
// A book with no explicit "destination" now saves in Quire by default, the
// same as any other download (books-contract.md §B) — reversed from the old
// always-upload-to-the-library default this test used to pin.
func TestTheFirstTapDownloadsABookWithoutAskingOrListingTwice(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)

	// No "confirmed":true. This is the first tap.
	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`"}`)
	waitForPhase(t, env.rec, "done")

	for _, m := range progressOf(t, env.rec) {
		if m["phase"] == "confirm" {
			t.Errorf("a book download asked first: %v", m["message"])
		}
	}
	// The release search is the expensive call. The download path must not make
	// it at all: Retrieve reads the releases itself, fresh, because the object
	// it posts back cannot be reconstructed from a cached copy.
	if n := th.listedReleases(); n != 0 {
		t.Errorf("the download listed the book's releases %d time(s); Retrieve does that itself", n)
	}
	// An epub is a format Quire's own reader can open, and nothing asked for
	// the library, so it lands in Quire's own storage rather than being
	// uploaded.
	env.fake.mu.Lock()
	uploaded := len(env.fake.uploaded)
	env.fake.mu.Unlock()
	if uploaded != 0 {
		t.Errorf("%d documents uploaded, want 0 — a readable book with no explicit destination stays in Quire", uploaded)
	}
}

// A readable book with no explicit destination lands in Quire's own
// storage, as a shelf.Record of Kind book.
func TestAReadableBookSavesInQuire(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)

	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`"}`)
	waitForPhase(t, env.rec, "done")

	rec, ok := env.shelfStore.Get(shelf.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Chapter: bookReleaseID})
	if !ok {
		t.Fatal("the book was not remembered in the shelf store")
	}
	if !rec.IsBook() {
		t.Errorf("Kind = %q, want a book", rec.Kind)
	}
	if rec.Format != "epub" {
		t.Errorf("Format = %q, want epub", rec.Format)
	}
	full := filepath.Join(env.savedDir, rec.File)
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("the saved file is not readable: %v", err)
	}
	if !bytes.Equal(got, bookEPUB) {
		t.Errorf("the saved file's bytes do not match what the source served")
	}
}

// TestAnUnreadableBookFallsBackToLibrary is the mutation check for "unreadable
// format falls back to library": a format backend/bookrender cannot open must
// still reach the reMarkable library — the one place it can be read — with a
// note saying so, and must not be remembered as a saved-in-Quire book.
func TestAnUnreadableBookFallsBackToLibrary(t *testing.T) {
	th := &bookTheme{filename: "An Example Book - Example Author (1970).azw3"}
	env := newBookService(t, th)

	env.fake.mu.Lock()
	env.fake.entries = append(env.fake.entries, library.Entry{
		ID: "books", Parent: "", Type: library.Collection, VisibleName: "Books",
	})
	env.fake.mu.Unlock()

	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`"}`)
	waitForPhase(t, env.rec, "done")

	env.fake.mu.Lock()
	uploaded := len(env.fake.uploaded)
	env.fake.mu.Unlock()
	if uploaded != 1 {
		t.Fatalf("%d documents uploaded, want 1 — an unreadable format must fall back to the library", uploaded)
	}
	if _, ok := env.shelfStore.Get(shelf.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Chapter: bookReleaseID}); ok {
		t.Error("an unreadable book was remembered as saved in Quire; it went to the library instead")
	}

	found := false
	for _, m := range progressOf(t, env.rec) {
		note, _ := m["note"].(string)
		msg, _ := m["message"].(string)
		if (strings.Contains(note, "AZW3") || strings.Contains(msg, "AZW3")) &&
			(strings.Contains(note, "library") || strings.Contains(msg, "library")) {
			found = true
		}
	}
	if !found {
		t.Error("no progress note explained the fallback to the library")
	}
}

// TestAPrivateSourceRefusesAnUnreadableBook is the mutation check for "a
// private source refuses" instead of falling back to the library: the whole
// point of a private source is that nothing it names reaches xochitl's
// library, and an unreadable format must not be the one silent exception.
func TestAPrivateSourceRefusesAnUnreadableBook(t *testing.T) {
	th := &bookTheme{filename: "An Example Book - Example Author (1970).azw3"}
	env := newBookService(t, th)
	if err := env.store.SetPrivate("example-books", true); err != nil {
		t.Fatal(err)
	}

	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`"}`)
	waitForPhase(t, env.rec, "failed")

	env.fake.mu.Lock()
	uploaded := len(env.fake.uploaded)
	env.fake.mu.Unlock()
	if uploaded != 0 {
		t.Error("a private source's unreadable book was uploaded to the library anyway")
	}
	if _, ok := env.shelfStore.Get(shelf.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Chapter: bookReleaseID}); ok {
		t.Error("a refused book should not have been saved anywhere")
	}
}

// TestAPrivateSourceCanSaveAReadableBookInQuire: private sources may now
// download books to Quire's own storage, even though the library stays
// refused for them (books-contract.md §B, Storage).
func TestAPrivateSourceCanSaveAReadableBookInQuire(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	if err := env.store.SetPrivate("example-books", true); err != nil {
		t.Fatal(err)
	}

	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`"}`)
	waitForPhase(t, env.rec, "done")

	rec, ok := env.shelfStore.Get(shelf.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Chapter: bookReleaseID})
	if !ok {
		t.Fatal("a private source's readable book should still save in Quire")
	}
	if !rec.IsBook() {
		t.Errorf("Kind = %q, want a book", rec.Kind)
	}
}

// A private source explicitly asking for the library is still refused, book
// or not — the destination override in the previous tests must not have
// weakened that existing rule.
func TestAPrivateSourceStillRefusesTheLibraryForABook(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	if err := env.store.SetPrivate("example-books", true); err != nil {
		t.Fatal(err)
	}

	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`","destination":"library"}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "private_library" {
		t.Errorf("code %q, want private_library", e.Code)
	}
}

// TestDeletingASavedBookRemovesOnlyItsFile is the mutation check for "delete
// of a book removes only its file under the saved root": a book is one file,
// not a per-page directory, and deleting it must not reach for
// download.ChapterDir's directory shape (which would remove nothing, since a
// book never has one) nor take a sibling book down with it.
func TestDeletingASavedBookRemovesOnlyItsFile(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)

	const otherRelease = "/release/openlibrary/OL1W/direct_download/other456"

	// Two releases of the same series, both saved in Quire. Each download
	// gets its own recorder: waitForPhase matches against everything a
	// recorder has ever seen, and the two downloads' "done" phases would
	// otherwise be indistinguishable.
	rec1 := &recorder{}
	handle(t, env.svc, rec1, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			bookReleaseID+`"}`)
	waitForPhase(t, rec1, "done")
	rec2 := &recorder{}
	handle(t, env.svc, rec2, appload.MessageEnqueueDownload,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","volumeId":"`+
			otherRelease+`"}`)
	waitForPhase(t, rec2, "done")

	keyA := shelf.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Chapter: bookReleaseID}
	keyB := shelf.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Chapter: otherRelease}
	recA, ok := env.shelfStore.Get(keyA)
	if !ok {
		t.Fatal("the first book was not saved")
	}
	recB, ok := env.shelfStore.Get(keyB)
	if !ok {
		t.Fatal("the second book was not saved")
	}
	fileA := filepath.Join(env.savedDir, recA.File)
	fileB := filepath.Join(env.savedDir, recB.File)
	if _, err := os.Stat(fileA); err != nil {
		t.Fatalf("the first book's file is missing before deleting anything: %v", err)
	}
	if _, err := os.Stat(fileB); err != nil {
		t.Fatalf("the second book's file is missing before deleting anything: %v", err)
	}

	handle(t, env.svc, env.rec, appload.MessageDeleteSaved,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","chapterId":"`+bookReleaseID+`"}`)
	env.rec.wait(t, appload.MessageSavedDeleted) // the confirm question
	handle(t, env.svc, env.rec, appload.MessageDeleteSaved,
		`{"sourceId":"example-books","seriesId":"/book/openlibrary/OL1W","chapterId":"`+bookReleaseID+`","confirmed":true}`)
	env.rec.wait(t, appload.MessageSavedDeleted) // "done"

	if _, err := os.Stat(fileA); !os.IsNotExist(err) {
		t.Errorf("the deleted book's file still exists: %v", err)
	}
	if _, ok := env.shelfStore.Get(keyA); ok {
		t.Error("the deleted book's record is still there")
	}

	// The sibling book — same series, same source — must be untouched.
	if _, err := os.Stat(fileB); err != nil {
		t.Errorf("deleting one book removed its sibling's file too: %v", err)
	}
	if _, ok := env.shelfStore.Get(keyB); !ok {
		t.Error("deleting one book removed its sibling's record too")
	}
}

// The number two packages have to agree on.
//
// fetch cannot import library — it imports nothing of Quire's, and the
// dependency would be backwards — so the file-retrieval cap is written out
// there and pinned here, in the one package that sees both. If they ever
// diverge, either books are refused before the device would have refused them
// or Quire downloads megabytes that xochitl will reset mid-upload.
func TestTheFileRetrievalCapIsTheUploadBudget(t *testing.T) {
	if fetch.FileRetrievalMaxResponseBytes != library.UploadBudgetBytes {
		t.Errorf("fetch caps a book at %d and the library aims one at %d; they are the same number "+
			"for a reason — see fetch.FileRetrievalMaxResponseBytes",
			fetch.FileRetrievalMaxResponseBytes, library.UploadBudgetBytes)
	}
	// And the budget is what the device will actually take. This is
	// library's own invariant; it is restated here because the cap above is
	// only correct while it holds.
	if library.UploadBudgetBytes >= library.MaxUploadBytes {
		t.Errorf("the upload budget %d is not below the device's %d hard limit",
			library.UploadBudgetBytes, library.MaxUploadBytes)
	}
}

// TestBookFileFetchCarriesSourceHeaders is the mutation (a) test for
// filedownload.go's fetchFile: reverting its policy construction from
// theme.PolicyFor(th, src) back to the bare src.Policy() it used to call
// makes this fail, because a bare Source.Policy() never carries a theme's
// SourceHeaders — theme.PolicyFor is the only place that side interface is
// consulted.
func TestBookFileFetchCarriesSourceHeaders(t *testing.T) {
	th := &bookTheme{headers: http.Header{"X-Reading-Grant": []string{"granted-token"}}}
	env := newBookService(t, th)

	downloadTheBook(t, env.svc, env.rec)
	waitForPhase(t, env.rec, "done")

	var found bool
	for _, c := range env.fetcher.Calls() {
		if !strings.Contains(c.URL, "/api/localdownload") {
			continue
		}
		found = true
		if c.Policy == nil || c.Policy.Headers.Get("X-Reading-Grant") != "granted-token" {
			t.Errorf("the file was fetched with policy %+v, want X-Reading-Grant: granted-token", c.Policy)
		}
	}
	if !found {
		t.Fatal("the file was never fetched")
	}
}

// TestBookFileFetchCarriesCookieJar is the same mutation (a) proof for
// theme.CookieUser: without going through theme.PolicyFor, a theme that opts
// into cookies never gets a jar on the policy the file fetch is made with.
func TestBookFileFetchCarriesCookieJar(t *testing.T) {
	th := &bookTheme{useCookies: true}
	env := newBookService(t, th)

	downloadTheBook(t, env.svc, env.rec)
	waitForPhase(t, env.rec, "done")

	var found bool
	for _, c := range env.fetcher.Calls() {
		if !strings.Contains(c.URL, "/api/localdownload") {
			continue
		}
		found = true
		if c.Policy == nil || c.Policy.Cookies == nil {
			t.Errorf("the file was fetched with policy %+v, want a cookie jar", c.Policy)
		}
	}
	if !found {
		t.Fatal("the file was never fetched")
	}
}
