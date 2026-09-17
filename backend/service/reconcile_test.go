package service_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
)

// reconcileFixture is a library of n recorded documents with a page cache each.
//
// Every case builds its own: a guard that holds because the case before it left
// the store empty is not a guard anyone has tested.
func reconcileFixture(t *testing.T, n int) (*service.Service, *library.Store, string, []string) {
	t.Helper()
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	var uuids []string
	for i := range n {
		chapter := "/manga/the-lantern-keeper/chapter-" + string(rune('1'+i)) + "/"
		pageCache(t, root, "example-reader", "the-lantern-keeper", chapter)
		uuid := "doc-" + string(rune('a'+i))
		if err := libStore.Put(library.Record{
			Key: library.Key{Source: "example-reader", Series: "the-lantern-keeper",
				Volume: string(rune('1' + i))},
			DocumentUUID: uuid,
			Chapters:     []string{chapter},
		}); err != nil {
			t.Fatal(err)
		}
		uuids = append(uuids, uuid)
	}
	seriesDir := filepath.Join(root, "example-reader", "the-lantern-keeper")
	return svc, libStore, seriesDir, uuids
}

func chapterOf(i int) string {
	return "/manga/the-lantern-keeper/chapter-" + string(rune('1'+i)) + "/"
}

// The feature: a document the user deleted on the tablet is forgotten, and its
// pages go with it.
func TestReconcileForgetsADocumentTheTabletNoLongerHas(t *testing.T) {
	svc, libStore, seriesDir, uuids := reconcileFixture(t, 3)

	handle(t, svc, &recorder{}, appload.MessageDocumentsChecked, `{
		"checked": true,
		"documentUuids": `+jsonList(uuids)+`,
		"missing": ["`+uuids[1]+`"]}`)

	if n := len(libStore.List()); n != 2 {
		t.Errorf("%d records left, want 2", n)
	}
	for _, rec := range libStore.List() {
		if rec.DocumentUUID == uuids[1] {
			t.Error("the deleted document is still recorded")
		}
	}
	if exists(t, download.ChapterDir(seriesDir, chapterOf(1))) {
		t.Error("the deleted document's pages survived")
	}
	// And the others are untouched.
	for _, i := range []int{0, 2} {
		if !exists(t, download.ChapterDir(seriesDir, chapterOf(i))) {
			t.Errorf("the pages of document %d were reclaimed too", i)
		}
	}
}

// **The guard that matters.** The backend acts only on an explicit "I checked".
//
// The dangerous reply is not the empty one — an empty `missing` deletes nothing
// whatever the flag says — it is a reply that **names documents while admitting
// it could not check them**: a frontend that failed part way, a malformed
// message, a future frontend that does not abandon its partial answer the way
// Reconcile.js does. Read as an answer, that deletes records and their pages on
// the strength of a check that never happened.
//
// An earlier version of this test used `missing: []` and passed with the guard
// removed, which means it was testing nothing.
func TestReconcileActsOnNothingWhenTheFrontendCouldNotCheck(t *testing.T) {
	svc, libStore, seriesDir, uuids := reconcileFixture(t, 3)

	handle(t, svc, &recorder{}, appload.MessageDocumentsChecked, `{
		"checked": false,
		"documentUuids": `+jsonList(uuids)+`,
		"missing": ["`+uuids[0]+`", "`+uuids[1]+`"]}`)

	if n := len(libStore.List()); n != 3 {
		t.Fatalf("%d records left; an unchecked answer deleted %d of them", n, 3-n)
	}
	for i := range 3 {
		if !exists(t, download.ChapterDir(seriesDir, chapterOf(i))) {
			t.Errorf("the pages of document %d were reclaimed on an unchecked answer", i)
		}
	}
}

// And the quiet form of the same thing: a frontend with no bridge sends an
// empty list, which must not read as "none of them exist".
func TestReconcileTreatsAnEmptyUncheckedAnswerAsIgnorance(t *testing.T) {
	svc, libStore, _, uuids := reconcileFixture(t, 3)

	handle(t, svc, &recorder{}, appload.MessageDocumentsChecked, `{
		"checked": false,
		"documentUuids": `+jsonList(uuids)+`,
		"missing": []}`)

	if n := len(libStore.List()); n != 3 {
		t.Errorf("%d records left, want all 3", n)
	}
}

// A reply naming a document that was never asked about is not an answer to the
// question asked.
func TestReconcileIgnoresDocumentsItDidNotAskAbout(t *testing.T) {
	svc, libStore, _, uuids := reconcileFixture(t, 3)

	handle(t, svc, &recorder{}, appload.MessageDocumentsChecked, `{
		"checked": true,
		"documentUuids": ["`+uuids[0]+`"],
		"missing": ["`+uuids[0]+`", "`+uuids[1]+`", "someone-elses-document"]}`)

	left := map[string]bool{}
	for _, rec := range libStore.List() {
		left[rec.DocumentUUID] = true
	}
	if left[uuids[0]] {
		t.Error("the document that was asked about and is missing was kept")
	}
	if !left[uuids[1]] {
		t.Error("a document that was never asked about was dropped")
	}
}

// Everything missing at once is likelier to be a bug than a user who emptied
// their library between two launches, and the two are indistinguishable from
// here. Above a few records, it is refused.
func TestReconcileRefusesToWipeTheWholeLibrary(t *testing.T) {
	svc, libStore, seriesDir, uuids := reconcileFixture(t, 6)

	handle(t, svc, &recorder{}, appload.MessageDocumentsChecked, `{
		"checked": true,
		"documentUuids": `+jsonList(uuids)+`,
		"missing": `+jsonList(uuids)+`}`)

	if n := len(libStore.List()); n != 6 {
		t.Fatalf("%d records left; a total wipe was obeyed", n)
	}
	for i := range 6 {
		if !exists(t, download.ChapterDir(seriesDir, chapterOf(i))) {
			t.Errorf("the pages of document %d were reclaimed by a total wipe", i)
		}
	}
}

// A small library really can be emptied by hand, and that is an ordinary
// afternoon rather than a bug.
func TestReconcileAcceptsASmallLibraryGoingAway(t *testing.T) {
	svc, libStore, _, uuids := reconcileFixture(t, 2)

	handle(t, svc, &recorder{}, appload.MessageDocumentsChecked, `{
		"checked": true,
		"documentUuids": `+jsonList(uuids)+`,
		"missing": `+jsonList(uuids)+`}`)

	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records left; two documents the user deleted should go", n)
	}
}

// A document that merely moved still resolves, so it is never in `missing` — and
// nothing about it is touched. The test drives the same answer the frontend
// would give for a volume filed into a folder of the user's own.
func TestReconcileLeavesADocumentThatOnlyMoved(t *testing.T) {
	svc, libStore, seriesDir, uuids := reconcileFixture(t, 3)

	handle(t, svc, &recorder{}, appload.MessageDocumentsChecked, `{
		"checked": true,
		"documentUuids": `+jsonList(uuids)+`,
		"missing": []}`)

	if n := len(libStore.List()); n != 3 {
		t.Errorf("%d records left; documents that still resolve must be kept", n)
	}
	for i := range 3 {
		if !exists(t, download.ChapterDir(seriesDir, chapterOf(i))) {
			t.Errorf("the pages of document %d went", i)
		}
	}
}

// Attach asks, so the user does not have to tap Read to find out.
func TestAttachAsksWhichDocumentsAreStillThere(t *testing.T) {
	svc, store, libStore, _ := serviceWithDownloadRoot(t)
	addSource(t, store)
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "1"},
		DocumentUUID: "doc-a",
		Chapters:     []string{chapterOf(0)},
	}); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	if err := svc.FrontendAttached(rec); err != nil {
		t.Fatal(err)
	}

	var ask struct {
		DocumentUUIDs []string `json:"documentUuids"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageCheckDocuments), &ask); err != nil {
		t.Fatal(err)
	}
	if len(ask.DocumentUUIDs) != 1 || ask.DocumentUUIDs[0] != "doc-a" {
		t.Errorf("asked about %v, want [doc-a]", ask.DocumentUUIDs)
	}
}
