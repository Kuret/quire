package service_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
)

// attachWith builds a service whose library holds exactly these entries, puts
// these records in the store, and attaches a frontend.
//
// Every case builds its own: a pass that files the right thing because the case
// before it left a record behind proves nothing about the pass.
func attachWith(t *testing.T, entries []library.Entry, recs ...library.Record) (*library.Store, *recorder) {
	t.Helper()
	svc, store, libStore, fake, _ := newDownloadService(t)
	fake.mu.Lock()
	fake.entries = append([]library.Entry(nil), entries...)
	fake.mu.Unlock()
	addSource(t, store)

	for _, rec := range recs {
		if err := libStore.Put(rec); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recorder{}
	if err := svc.FrontendAttached(rec); err != nil {
		t.Fatal(err)
	}
	return libStore, rec
}

// comicsWith is the Comics folder holding these documents.
func comicsWith(docs ...library.Entry) []library.Entry {
	out := []library.Entry{comicsFolder()}
	return append(out, docs...)
}

func doc(id, name, parent string) library.Entry {
	return library.Entry{ID: id, Parent: parent, Type: library.Document, VisibleName: name}
}

// unsortedRecord is a download that landed in Comics and was never filed — a
// record written before sorting existed, or one whose frontend was away.
func unsortedRecord(uuid, volume, name string) library.Record {
	return library.Record{
		Key:          library.Key{Source: "example-reader", Series: "/manga/the-lantern-keeper/", Volume: volume},
		DocumentUUID: uuid,
		FolderUUID:   "comics",
		FolderPath:   []string{"Comics"},
		VisibleName:  name,
	}
}

// waitForSort returns the first sort instruction of the attach, or fails.
func waitForSort(t *testing.T, rec *recorder) sortAsk {
	t.Helper()
	var ask sortAsk
	if err := json.Unmarshal(rec.wait(t, appload.MessageSortDocuments), &ask); err != nil {
		t.Fatal(err)
	}
	return ask
}

// noSortSent reports that nothing was asked for, after giving the background
// pass a moment to have asked.
func noSortSent(t *testing.T, rec *recorder) {
	t.Helper()
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		for _, f := range rec.sent {
			if f.Type == appload.MessageSortDocuments {
				rec.mu.Unlock()
				t.Fatalf("a sort was asked for: %s", f.Payload)
			}
		}
		rec.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

// The gap this closes: a download that finished while the frontend was away is
// filed when it comes back.
func TestAttachFilesADownloadThatWasNeverSorted(t *testing.T) {
	_, rec := attachWith(t,
		comicsWith(doc("d1", "The Lantern Keeper — Ch 0001.pdf", "comics")),
		unsortedRecord("d1", "1", "The Lantern Keeper — Ch 0001.pdf"))

	ask := waitForSort(t, rec)
	if len(ask.DocumentUUIDs) != 1 || ask.DocumentUUIDs[0] != "d1" {
		t.Errorf("asked about %v, want [d1]", ask.DocumentUUIDs)
	}
	if ask.FolderName != "The Lantern Keeper" {
		t.Errorf("folderName %q, want the series title", ask.FolderName)
	}
	if ask.CreateUnder != "comics" {
		t.Errorf("createUnder %q, want comics", ask.CreateUnder)
	}
}

// A record Quire has already filed is off limits for good. A document of ours
// sitting in Comics now is one the *user* put back, and filing it again on
// every attach would be Quire overruling them.
func TestAttachLeavesADocumentTheUserMovedBack(t *testing.T) {
	filed := unsortedRecord("d1", "1", "The Lantern Keeper — Ch 0001.pdf")
	filed.FolderUUID = "lantern"
	filed.FolderPath = []string{"Comics", "The Lantern Keeper"}

	_, rec := attachWith(t,
		comicsWith(doc("d1", "The Lantern Keeper — Ch 0001.pdf", "comics")),
		filed)

	noSortSent(t, rec)
}

// Where the record says the volume landed is not where it is. A document the
// user has filed somewhere of their own is left alone.
func TestAttachLeavesADocumentThatIsNoLongerInComics(t *testing.T) {
	_, rec := attachWith(t,
		// The document exists, but under a folder of the user's rather than
		// directly in Comics.
		[]library.Entry{
			comicsFolder(),
			{ID: "mine", Parent: "comics", Type: library.Collection, VisibleName: "Reading now"},
			doc("d1", "The Lantern Keeper — Ch 0001.pdf", "mine"),
		},
		unsortedRecord("d1", "1", "The Lantern Keeper — Ch 0001.pdf"))

	noSortSent(t, rec)
}

// The subtle one. A frontend whose bridge did not load answers "moved nothing",
// and that must record no folder — otherwise the record is disqualified for
// good on the strength of a failure, and the volume sits in Comics forever with
// Quire believing it was filed.
func TestABridgeThatDidNotLoadLeavesTheRecordEligible(t *testing.T) {
	svc, store, libStore, fake, _ := newDownloadService(t)
	fake.mu.Lock()
	fake.entries = comicsWith(doc("d1", "The Lantern Keeper — Ch 0001.pdf", "comics"))
	fake.mu.Unlock()
	addSource(t, store)
	if err := libStore.Put(unsortedRecord("d1", "1", "The Lantern Keeper — Ch 0001.pdf")); err != nil {
		t.Fatal(err)
	}

	first := &recorder{}
	if err := svc.FrontendAttached(first); err != nil {
		t.Fatal(err)
	}
	ask := waitForSort(t, first)

	// The answer a frontend with no bridge gives: it moved nothing.
	handle(t, svc, &recorder{}, appload.MessageDocumentsSorted, `{
		"sourceId":"example-reader","seriesId":"`+ask.SeriesID+`",
		"documentUuids":["d1"],"moved":[],"folderId":"","folderName":"The Lantern Keeper",
		"created":false,"detail":"this build cannot reach the library's folders"}`)

	for _, rec := range libStore.List() {
		if rec.DocumentUUID == "d1" && len(rec.FolderPath) == 2 {
			t.Fatal("a failed sort was recorded as a folder; the record can never be filed again")
		}
	}

	// And the next attach asks again, which is the point.
	second := &recorder{}
	if err := svc.FrontendAttached(second); err != nil {
		t.Fatal(err)
	}
	if got := waitForSort(t, second); len(got.DocumentUUIDs) != 1 {
		t.Errorf("the second attach asked about %v, want [d1]", got.DocumentUUIDs)
	}
}

// Two series in one attach are two folders, one group each — and the second is
// only asked for once the first has been answered.
func TestAttachFilesEachSeriesSeparately(t *testing.T) {
	other := library.Record{
		Key:          library.Key{Source: "example-reader", Series: "/manga/snotgirl/", Volume: "1"},
		DocumentUUID: "d2",
		FolderUUID:   "comics",
		FolderPath:   []string{"Comics"},
		VisibleName:  "Snotgirl — Ch 0001.pdf",
	}

	svc, store, libStore, fake, _ := newDownloadService(t)
	fake.mu.Lock()
	fake.entries = comicsWith(
		doc("d1", "The Lantern Keeper — Ch 0001.pdf", "comics"),
		doc("d2", "Snotgirl — Ch 0001.pdf", "comics"))
	fake.mu.Unlock()
	addSource(t, store)
	for _, rec := range []library.Record{
		unsortedRecord("d1", "1", "The Lantern Keeper — Ch 0001.pdf"), other} {
		if err := libStore.Put(rec); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recorder{}
	if err := svc.FrontendAttached(rec); err != nil {
		t.Fatal(err)
	}

	first := waitForSort(t, rec)
	// One group at a time: the second is not asked for until the first is
	// answered, so a library of forty series does not open with forty folder
	// creations at once.
	rec.mu.Lock()
	asks := 0
	for _, f := range rec.sent {
		if f.Type == appload.MessageSortDocuments {
			asks++
		}
	}
	rec.mu.Unlock()
	if asks != 1 {
		t.Fatalf("%d sorts asked for at once, want one at a time", asks)
	}

	handle(t, svc, &recorder{}, appload.MessageDocumentsSorted, `{
		"sourceId":"example-reader","seriesId":"`+first.SeriesID+`",
		"documentUuids":`+jsonList(first.DocumentUUIDs)+`,
		"moved":`+jsonList(first.DocumentUUIDs)+`,
		"folderId":"made-1","folderName":"`+first.FolderName+`","created":true,"detail":"moved"}`)

	// The second group follows, with its own folder name.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		var names []string
		for _, f := range rec.sent {
			if f.Type != appload.MessageSortDocuments {
				continue
			}
			var a sortAsk
			if err := json.Unmarshal(f.Payload, &a); err == nil {
				names = append(names, a.FolderName)
			}
		}
		rec.mu.Unlock()
		if len(names) == 2 {
			if names[0] == names[1] {
				t.Errorf("both groups asked for %q", names[0])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the second series was never asked about")
}

// Nothing to do is silence. An attach with everything already filed sends no
// sort at all.
func TestAttachWithNothingToFileSaysNothing(t *testing.T) {
	filed := unsortedRecord("d1", "1", "The Lantern Keeper — Ch 0001.pdf")
	filed.FolderUUID = "lantern"
	filed.FolderPath = []string{"Comics", "The Lantern Keeper"}

	_, rec := attachWith(t,
		[]library.Entry{
			comicsFolder(),
			{ID: "lantern", Parent: "comics", Type: library.Collection, VisibleName: "The Lantern Keeper"},
			doc("d1", "The Lantern Keeper — Ch 0001.pdf", "lantern"),
		},
		filed)

	noSortSent(t, rec)
}

// A document whose name carries no series is not worth inventing a folder for:
// a folder called "something.pdf" on the tablet is worse than a volume in
// Comics.
func TestAttachDoesNotInventAFolderNameFromADocument(t *testing.T) {
	odd := unsortedRecord("d1", "1", "no-series-here.pdf")

	_, rec := attachWith(t, comicsWith(doc("d1", "no-series-here.pdf", "comics")), odd)

	noSortSent(t, rec)
}

// The bug this field prevents: a fresh download and a later re-sort naming the
// same series differently, and so making two folders for it.
//
// The download path passes the source's series title; the attach path reads it
// off the record. They must agree, and the only way to be sure is to run both
// against the same series and compare what they asked for.
func TestBothPathsNameTheSeriesFolderTheSameWay(t *testing.T) {
	// The fresh download's name, taken from a real download.
	svc, store, libStore, fake, rec := newDownloadService(t)
	fake.mu.Lock()
	fake.entries = []library.Entry{comicsFolder()}
	fake.mu.Unlock()
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")
	fresh := waitForSort(t, rec)

	// The record that download wrote, carried into a second service which has
	// never seen the series and can only go on what was recorded.
	var stored library.Record
	for _, r := range libStore.List() {
		if r.DocumentUUID == fresh.DocumentUUIDs[0] {
			stored = r
		}
	}
	if stored.DocumentUUID == "" {
		t.Fatal("the download recorded nothing")
	}
	if stored.SeriesTitle == "" {
		t.Fatal("the download did not record the series title, so the paths can still diverge")
	}

	_, second := attachWith(t,
		comicsWith(doc(stored.DocumentUUID, stored.VisibleName, "comics")),
		stored)
	resort := waitForSort(t, second)

	if resort.FolderName != fresh.FolderName {
		t.Errorf("the re-sort would make %q while a download makes %q — two folders for one series",
			resort.FolderName, fresh.FolderName)
	}
}

// A title with an em dash of its own is exactly what the filename split gets
// wrong, and exactly what the recorded title gets right.
func TestATitleWithAnEmDashSurvivesTheRecordedTitle(t *testing.T) {
	withTitle := unsortedRecord("d1", "1", "Wandance \u2014 Vol 1 \u2014 Ch 0001.pdf")
	withTitle.SeriesTitle = "Wandance \u2014 After the Dance"

	_, rec := attachWith(t,
		comicsWith(doc("d1", withTitle.VisibleName, "comics")),
		withTitle)

	ask := waitForSort(t, rec)
	if ask.FolderName != "Wandance \u2014 After the Dance" {
		t.Errorf("folderName %q, want the recorded title in full", ask.FolderName)
	}
}

// A record written before the field exists still gets filed, by the old split.
func TestARecordWithoutATitleStillUsesTheFilename(t *testing.T) {
	old := unsortedRecord("d1", "1", "The Lantern Keeper \u2014 Ch 0001.pdf")
	old.SeriesTitle = ""

	_, rec := attachWith(t, comicsWith(doc("d1", old.VisibleName, "comics")), old)

	if ask := waitForSort(t, rec); ask.FolderName != "The Lantern Keeper" {
		t.Errorf("folderName %q, want the name split off the filename", ask.FolderName)
	}
}

// unsortedBookRecord is a book download that landed before Books existed as a
// concept: like every other download of that era it went into Comics flat,
// which is exactly what makes it eligible for this pass \u2014 a record whose
// FolderPath is Comics alone is "never sorted" regardless of what kind of
// document it names (see sorted()).
func unsortedBookRecord(uuid, name string) library.Record {
	return library.Record{
		Key:          library.Key{Source: "example-books", Series: "/book/openlibrary/OL1W", Volume: "abc123"},
		DocumentUUID: uuid,
		FolderUUID:   "comics",
		FolderPath:   []string{"Comics"},
		VisibleName:  name,
	}
}

// attachWithBook is attachWith, but with a book source registered instead of
// the manga one, since kindForSource has to find a real theme to answer
// "book" rather than falling through to the manga default. It returns the
// whole environment, not just the store, because a follow-up assertion needs
// the service to answer the ask.
func attachWithBook(t *testing.T, entries []library.Entry, recs ...library.Record) bookEnv {
	t.Helper()
	env := newBookService(t, &bookTheme{})
	env.fake.mu.Lock()
	env.fake.entries = append([]library.Entry(nil), entries...)
	env.fake.mu.Unlock()

	for _, rec := range recs {
		if err := env.libStore.Put(rec); err != nil {
			t.Fatal(err)
		}
	}

	if err := env.svc.FrontendAttached(env.rec); err != nil {
		t.Fatal(err)
	}
	return env
}

// This is the bug from the field report: a book downloaded before books had
// their own folder must be filed flat into Books on this pass, never asked
// for a per-title subfolder the way a comic is.
func TestAttachFilesABookThatWasNeverSortedIntoBooksFlat(t *testing.T) {
	env := attachWithBook(t,
		comicsWith(doc("d1", "An Example Book.epub", "comics")),
		unsortedBookRecord("d1", "An Example Book.epub"))

	ask := waitForSort(t, env.rec)
	if ask.Kind != "book" {
		t.Fatalf("kind %q, want book", ask.Kind)
	}
	if ask.FolderName != library.BooksFolder {
		t.Errorf("folderName %q, want %q \u2014 a book gets no per-title subfolder",
			ask.FolderName, library.BooksFolder)
	}
	if len(ask.DocumentUUIDs) != 1 || ask.DocumentUUIDs[0] != "d1" {
		t.Errorf("asked about %v, want [d1]", ask.DocumentUUIDs)
	}

	// Answering as the frontend would for a book records the flat path, not a
	// per-title one \u2014 this is documentsSorted's kindBook branch, exercised
	// through the same pass that found the bug.
	handle(t, env.svc, &recorder{}, appload.MessageDocumentsSorted, `{
		"sourceId":"`+ask.SourceID+`","seriesId":"`+ask.SeriesID+`",
		"documentUuids":["d1"],"moved":["d1"],"folderId":"books","folderName":"Books",
		"created":true,"detail":"moved","kind":"book"}`)

	for _, r := range env.libStore.List() {
		if r.DocumentUUID != "d1" {
			continue
		}
		if len(r.FolderPath) != 1 || r.FolderPath[0] != library.BooksFolder {
			t.Errorf("recorded folder path %v, want [%s]", r.FolderPath, library.BooksFolder)
		}
	}
}

// The comic path on this exact pass must not have changed at all: a comic
// still gets asked for Comics/<series>, never Books.
func TestAttachStillFilesAComicIntoItsSeriesFolderOnThisPass(t *testing.T) {
	_, rec := attachWith(t,
		comicsWith(doc("d1", "The Lantern Keeper \u2014 Ch 0001.pdf", "comics")),
		unsortedRecord("d1", "1", "The Lantern Keeper \u2014 Ch 0001.pdf"))

	ask := waitForSort(t, rec)
	if ask.Kind == "book" {
		t.Fatalf("a comic was asked for as kind %q", ask.Kind)
	}
	if ask.FolderName != "The Lantern Keeper" {
		t.Errorf("folderName %q, want the series title", ask.FolderName)
	}
	if ask.CreateUnder != "comics" {
		t.Errorf("createUnder %q, want comics", ask.CreateUnder)
	}
}

// A mixed batch \u2014 one book, one comic, both never sorted \u2014 must file each
// through its own path in the same pass. The pass does one group at a time
// (TestAttachFilesEachSeriesSeparately pins that), so the first group is
// answered before the second is ever sent.
func TestAttachFilesAMixedBatchEachThroughItsOwnPath(t *testing.T) {
	env := newBookService(t, &bookTheme{})
	env.fake.mu.Lock()
	env.fake.entries = comicsWith(
		doc("d1", "An Example Book.epub", "comics"),
		doc("d2", "The Lantern Keeper \u2014 Ch 0001.pdf", "comics"))
	env.fake.mu.Unlock()
	addSource(t, env.store) // the manga source, alongside the book one newBookService added

	comic := unsortedRecord("d2", "1", "The Lantern Keeper \u2014 Ch 0001.pdf")
	for _, r := range []library.Record{unsortedBookRecord("d1", "An Example Book.epub"), comic} {
		if err := env.libStore.Put(r); err != nil {
			t.Fatal(err)
		}
	}

	if err := env.svc.FrontendAttached(env.rec); err != nil {
		t.Fatal(err)
	}

	seen := map[string]sortAsk{}

	first := waitForSort(t, env.rec)
	seen[first.DocumentUUIDs[0]] = first
	answerAsk(t, env.svc, first)

	// The second group only appears once the first has been answered.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(seen) < 2 {
		env.rec.mu.Lock()
		for _, f := range env.rec.sent {
			if f.Type != appload.MessageSortDocuments {
				continue
			}
			var a sortAsk
			if err := json.Unmarshal(f.Payload, &a); err == nil && len(a.DocumentUUIDs) == 1 {
				seen[a.DocumentUUIDs[0]] = a
			}
		}
		env.rec.mu.Unlock()
		if len(seen) < 2 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("saw asks for %d document(s), want 2", len(seen))
	}
	if got := seen["d1"]; got.Kind != "book" || got.FolderName != library.BooksFolder {
		t.Errorf("book asked for kind=%q folder=%q, want book/%s", got.Kind, got.FolderName, library.BooksFolder)
	}
	if got := seen["d2"]; got.Kind == "book" || got.FolderName != "The Lantern Keeper" {
		t.Errorf("comic asked for kind=%q folder=%q, want comic/The Lantern Keeper", got.Kind, got.FolderName)
	}
}

// answerAsk sends the documentsSorted reply an obedient frontend would give
// for this ask, book or comic alike, so a test can move a resort pass on to
// its next group without caring which shape this one was.
func answerAsk(t *testing.T, svc *service.Service, ask sortAsk) {
	t.Helper()
	kind := ""
	if ask.Kind == "book" {
		kind = `,"kind":"book"`
	}
	handle(t, svc, &recorder{}, appload.MessageDocumentsSorted, `{
		"sourceId":"`+ask.SourceID+`","seriesId":"`+ask.SeriesID+`",
		"documentUuids":`+jsonList(ask.DocumentUUIDs)+`,
		"moved":`+jsonList(ask.DocumentUUIDs)+`,
		"folderId":"made-1","folderName":"`+ask.FolderName+`","created":true,"detail":"moved"`+kind+`}`)
}

// The reported bug, pinned directly: Books already exists, so the re-filing
// pass must never ask the frontend to create it. This is the regression test
// for the duplicate "Books" folder created on every app open — the pass used
// to fabricate a Placement saying Books was missing without ever checking.
func TestAttachNeverAsksToCreateBooksWhenItAlreadyExists(t *testing.T) {
	env := attachWithBook(t,
		append(comicsWith(doc("d1", "An Example Book.epub", "comics")), booksFolder()),
		unsortedBookRecord("d1", "An Example Book.epub"))

	ask := waitForSort(t, env.rec)
	if ask.CreateComics {
		t.Error("asked to create Comics for a book")
	}
	if ask.CreateUnder != "" {
		t.Errorf("createUnder %q, want empty — Books already exists, nothing is being created", ask.CreateUnder)
	}
	if ask.FolderID != "books" {
		t.Errorf("folderId %q, want books — the document should be moved into the existing folder", ask.FolderID)
	}
	if ask.FolderName != library.BooksFolder {
		t.Errorf("folderName %q, want %q", ask.FolderName, library.BooksFolder)
	}
}

// The other half: Books genuinely does not exist yet, and the re-filing pass
// must still ask for it to be created — the fix for the duplicate-folder bug
// must not go so far as to silently skip a library with no Books at all.
func TestAttachAsksToCreateBooksWhenItGenuinelyDoesNotExist(t *testing.T) {
	env := attachWithBook(t,
		comicsWith(doc("d1", "An Example Book.epub", "comics")),
		unsortedBookRecord("d1", "An Example Book.epub"))

	ask := waitForSort(t, env.rec)
	if ask.FolderID != "" {
		t.Errorf("folderId %q, want empty — there is nothing to move into yet", ask.FolderID)
	}
	if ask.CreateUnder != "" {
		t.Errorf("createUnder %q, want empty — Books is never nested", ask.CreateUnder)
	}
	if ask.FolderName != library.BooksFolder {
		t.Errorf("folderName %q, want %q", ask.FolderName, library.BooksFolder)
	}
}

// A book whose document is sitting in Comics while Books already exists must
// be asked to move, not skipped forever. Before this fix, askToFileBook's
// place.Complete() early return conflated "the upload already landed there"
// (true on the download path) with "Books exists" (true here too, but the
// document is not in it) and silently dropped every such book.
func TestAttachMovesABookIntoAnExistingBooksFolderRatherThanSkippingIt(t *testing.T) {
	env := attachWithBook(t,
		append(comicsWith(doc("d1", "An Example Book.epub", "comics")), booksFolder()),
		unsortedBookRecord("d1", "An Example Book.epub"))

	ask := waitForSort(t, env.rec)
	if len(ask.DocumentUUIDs) != 1 || ask.DocumentUUIDs[0] != "d1" {
		t.Fatalf("asked about %v, want [d1] — the book must not be skipped", ask.DocumentUUIDs)
	}
	if ask.Kind != "book" {
		t.Errorf("kind %q, want book", ask.Kind)
	}
	if ask.FolderID != "books" {
		t.Errorf("folderId %q, want books — the ask must name the folder that already exists", ask.FolderID)
	}

	// Answering as a real move would confirms the record ends up filed.
	handle(t, env.svc, &recorder{}, appload.MessageDocumentsSorted, `{
		"sourceId":"`+ask.SourceID+`","seriesId":"`+ask.SeriesID+`",
		"documentUuids":["d1"],"moved":["d1"],"folderId":"books","folderName":"Books",
		"created":false,"detail":"moved","kind":"book"}`)

	for _, r := range env.libStore.List() {
		if r.DocumentUUID != "d1" {
			continue
		}
		if len(r.FolderPath) != 1 || r.FolderPath[0] != library.BooksFolder {
			t.Errorf("recorded folder path %v, want [%s]", r.FolderPath, library.BooksFolder)
		}
		return
	}
	t.Fatal("no record for the book that moved")
}

// A record whose source has been removed has no kind left to ask about
// (kindForSource returns "" \u2014 see its own comment). This pass's decision:
// leave the document exactly where it is rather than guess. Guessing comic
// reproduces the field bug for a book; guessing book would hide an
// unfinished multi-volume comic in a folder with no per-series grouping.
// Leaving it alone cannot make an already-orphaned record worse.
func TestAttachLeavesARecordAloneWhenItsSourceIsGone(t *testing.T) {
	vanished := unsortedRecord("d1", "1", "Some Vanished Source \u2014 Ch 0001.pdf")
	vanished.Source = "no-such-source-anymore"

	_, rec := attachWith(t, comicsWith(doc("d1", vanished.VisibleName, "comics")), vanished)

	noSortSent(t, rec)
}
