package service_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
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
