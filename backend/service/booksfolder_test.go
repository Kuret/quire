package service_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
)

// A book goes into Books, flat, and never into Comics and never into a
// per-title subfolder under Books. PLAN's "Same deal as the Comics directory
// ... but no subfoldering" design (2026-09-21).

// booksFolder is the Books folder, empty, exactly as comicsFolder is Comics.
func booksFolder() library.Entry {
	return library.Entry{ID: "books", Parent: "", Type: library.Collection, VisibleName: "Books"}
}

// A Books folder that already exists: the upload lands in it directly, and
// nothing is asked of the frontend — there is no per-series decision left to
// make once the one level Books needs is there.
func TestABookIsFiledFlatIntoBooks(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	env.fake.mu.Lock()
	env.fake.entries = append(env.fake.entries, booksFolder())
	env.fake.mu.Unlock()

	downloadTheBook(t, env.svc, env.rec)
	waitForPhase(t, env.rec, "done")

	env.rec.mu.Lock()
	for _, f := range env.rec.sent {
		if f.Type == appload.MessageSortDocuments {
			t.Errorf("asked the frontend to sort a book that already had a Books folder: %s", f.Payload)
		}
	}
	env.rec.mu.Unlock()

	recs := env.libStore.List()
	if len(recs) != 1 {
		t.Fatalf("%d records, want 1", len(recs))
	}
	got := recs[0]
	if got.FolderUUID != "books" {
		t.Errorf("record folder %q, want books", got.FolderUUID)
	}
	if len(got.FolderPath) != 1 || got.FolderPath[0] != library.BooksFolder {
		t.Errorf("record path %v, want exactly [Books] — never a two-element path", got.FolderPath)
	}

	env.fake.mu.Lock()
	defer env.fake.mu.Unlock()
	parent := ""
	for _, e := range env.fake.entries {
		if e.ID == got.DocumentUUID {
			parent = e.Parent
		}
	}
	if parent != "books" {
		t.Errorf("landed in %q, want the Books folder", parent)
	}
}

// No Books folder yet: the upload lands at the root and the frontend is asked
// to make Books, one level, under the root — never a per-series ask, and
// never inside Comics.
func TestABookWithoutABooksFolderAsksToCreateOneFlat(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	// The default fixture only has Comics; no Books.

	downloadTheBook(t, env.svc, env.rec)
	waitForPhase(t, env.rec, "done")

	var ask sortAsk
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageSortDocuments), &ask); err != nil {
		t.Fatal(err)
	}
	if ask.FolderName != library.BooksFolder {
		t.Errorf("folderName %q, want %q", ask.FolderName, library.BooksFolder)
	}
	if ask.CreateUnder != "" {
		t.Errorf("createUnder %q, want the root — Books is never nested", ask.CreateUnder)
	}
	if ask.Kind != "book" {
		t.Errorf("kind %q, want book", ask.Kind)
	}
	if ask.CreateComics {
		t.Error("asked to create Comics for a book download")
	}
	if ask.FolderID != "" {
		t.Errorf("folderId %q, want none yet", ask.FolderID)
	}

	recs := env.libStore.List()
	if len(recs) != 1 {
		t.Fatalf("%d records, want 1", len(recs))
	}
	if recs[0].FolderUUID == "comics" {
		t.Error("a book with no Books folder landed in Comics")
	}
}

// The reply to that ask is recorded as Books alone, one element — never
// Books/<title>, which would be exactly the per-title subfolder a book must
// never get.
func TestABookNeverGetsAPerTitleSubfolder(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)

	downloadTheBook(t, env.svc, env.rec)
	waitForPhase(t, env.rec, "done")

	var ask sortAsk
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageSortDocuments), &ask); err != nil {
		t.Fatal(err)
	}

	handle(t, env.svc, &recorder{}, appload.MessageDocumentsSorted, `{
		"sourceId":"example-books","seriesId":"`+ask.SeriesID+`",
		"documentUuids":["`+ask.DocumentUUIDs[0]+`"],
		"moved":["`+ask.DocumentUUIDs[0]+`"],
		"folderId":"made-1","folderName":"Books","created":true,"detail":"moved",
		"kind":"book"}`)

	for _, rec := range env.libStore.List() {
		if rec.DocumentUUID != ask.DocumentUUIDs[0] {
			continue
		}
		if rec.FolderUUID != "made-1" {
			t.Errorf("record folder %q, want made-1", rec.FolderUUID)
		}
		if len(rec.FolderPath) != 1 || rec.FolderPath[0] != library.BooksFolder {
			t.Errorf("record path %v, want exactly [Books], never a per-title subfolder", rec.FolderPath)
		}
		return
	}
	t.Fatal("no record for the book that moved")
}

// The other half of the pin: a comic must never be filed flat, into Books, or
// anywhere but Comics/<series>. If a comic ever started being filed flat this
// is the assertion that catches it.
func TestAComicIsNeverFiledIntoBooksOrFlat(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	addSource(t, env.store) // the manga source
	env.fake.mu.Lock()
	env.fake.entries = []library.Entry{comicsFolder(), booksFolder()}
	env.fake.mu.Unlock()

	seriesID, chapterID := firstChapter(t, env.svc, env.rec)
	handle(t, env.svc, env.rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, env.rec, "done")

	var ask sortAsk
	if err := json.Unmarshal(env.rec.wait(t, appload.MessageSortDocuments), &ask); err != nil {
		t.Fatal(err)
	}
	if ask.CreateUnder != "comics" {
		t.Errorf("createUnder %q, want comics — a comic must never be asked to go into Books", ask.CreateUnder)
	}
	if ask.Kind == "book" {
		t.Error("a comic download was asked for with kind book")
	}

	recs := env.libStore.List()
	if len(recs) != 1 {
		t.Fatalf("%d records, want 1", len(recs))
	}
	if len(recs[0].FolderPath) != 1 || recs[0].FolderPath[0] != library.ComicsFolder {
		t.Errorf("record path %v, want exactly [Comics] before it is sorted", recs[0].FolderPath)
	}
	if recs[0].FolderUUID == "books" {
		t.Error("a comic landed in the Books folder")
	}
}

// No Books folder and the frontend never answers (or fails to make one): the
// book still lands, at the top of My Files, and is told the Books-specific
// remedy — never Comics' wording.
func TestABookWithoutABooksFolderGetsTheBooksRemedy(t *testing.T) {
	th := &bookTheme{}
	env := newBookService(t, th)
	env.fake.mu.Lock()
	env.fake.entries = nil // no Comics, no Books
	env.fake.mu.Unlock()

	downloadTheBook(t, env.svc, env.rec)
	done := waitForPhase(t, env.rec, "done")

	note := ""
	if n, ok := done["note"]; ok {
		note = fmt.Sprint(n)
	}
	if !strings.Contains(note, "Books") {
		t.Errorf("note %q, want the Books remedy", note)
	}
	if strings.Contains(note, "“Comics”") {
		t.Errorf("note %q carries Comics' wording for a book", note)
	}
}
