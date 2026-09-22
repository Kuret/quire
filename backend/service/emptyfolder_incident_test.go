package service_test

import (
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
)

// booksIncidentFixture reproduces the shape of the incident found in
// xochitl's own log: a record whose FolderPath was mislabelled to look like an
// ordinary two-element per-series path, but whose FolderUUID actually names
// the resolved *Books* folder -- not a folder nested under Comics at all.
//
// `withUnrecordedDoc` adds a second entry to that same folder: a document
// Quire never downloaded and has no record of, standing in for "Dark
// Disciple", which the owner added to their reMarkable outside Quire.
func booksIncidentFixture(t *testing.T, withUnrecordedDoc bool) (
	svc *service.Service, libStore *library.Store, fake *fakeLibrary, seriesDocUUID string) {
	t.Helper()
	svc, store, libStore, fake, _ := newDownloadServiceWith(t, downloadRoutes(t))
	addSource(t, store)

	fake.mu.Lock()
	fake.entries = append(fake.entries,
		library.Entry{ID: "books", Parent: "", Type: library.Collection, VisibleName: "Books"})
	if withUnrecordedDoc {
		fake.entries = append(fake.entries, library.Entry{
			ID: "dark-disciple", Parent: "books", Type: library.Document, VisibleName: "Dark Disciple"})
	}
	fake.mu.Unlock()

	seriesDocUUID = "doc-a"
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "1"},
		DocumentUUID: seriesDocUUID,
		SeriesTitle:  "The Lantern Keeper",
		// Mislabelled exactly the way seriesFolderOf's comment warns about: two
		// elements, the shape recordedFolder reads as "a per-series folder
		// under Comics", but the id it actually names is Books' own.
		FolderUUID: "books",
		FolderPath: []string{"Comics", "Books"},
	}); err != nil {
		t.Fatal(err)
	}
	return svc, libStore, fake, seriesDocUUID
}

// Proof (c): the resolved Books id must never be offered for deletion, even
// when a mislabelled record makes it look like an ordinary, empty per-series
// folder. This is the guard the incident needed: without it, Quire itself
// would ask the frontend to remove the reMarkable's top-level Books folder.
func TestBooksIsNeverOfferedEvenWhenARecordMislabelsItAsASeriesFolder(t *testing.T) {
	svc, _, _, uuid := booksIncidentFixture(t, false)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":[{"documentUuid":"`+uuid+`","trashed":true,"removed":true}]}`)

	rec.wait(t, appload.MessageDownloadedList)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("Quire asked to delete the Books folder")
	}
}

// Proof (a), written to the exact shape of the incident: a Books folder that
// still holds one document Quire never downloaded, while the user asks Quire
// to delete an unrelated series. Quire must not ask for Books to be removed --
// which, on the device, is the only way "Dark Disciple" was ever put in
// danger, since Quire never names a document to delete except by its own
// record.
func TestAnUndownloadedDocumentInBooksSurvivesAnUnrelatedSeriesDelete(t *testing.T) {
	svc, libStore, fake, uuid := booksIncidentFixture(t, true)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":[{"documentUuid":"`+uuid+`","trashed":true,"removed":true}]}`)

	rec.wait(t, appload.MessageDownloadedList)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("Quire asked to delete the Books folder, which still held a document it never downloaded")
	}

	// The series' own record is gone -- that part of the delete the user asked
	// for did happen -- but the unrelated document is untouched and unnamed.
	for _, r := range libStore.List() {
		if r.DocumentUUID == uuid {
			t.Errorf("the deleted series' own record is still there: %+v", r)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	found := false
	for _, e := range fake.entries {
		if e.ID == "dark-disciple" {
			found = true
		}
	}
	if !found {
		t.Fatal("the fixture's own unrecorded document went missing; this test proves nothing")
	}
}
