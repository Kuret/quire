package service_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
)

// seriesFixture is one series with n downloads, each with its own chapter in
// the page cache.
//
// Every case builds its own: a delete that works because the case before it
// left the store empty is not a delete anyone has tested.
func seriesFixture(t *testing.T, n int) (*service.Service, *library.Store, string, []string) {
	t.Helper()
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	seriesDir := ""
	var uuids []string
	for i := range n {
		chapter := "/manga/the-lantern-keeper/chapter-" + string(rune('1'+i)) + "/"
		seriesDir = pageCache(t, root, "example-reader", "the-lantern-keeper", chapter)
		uuid := "doc-" + string(rune('a'+i))
		if err := libStore.Put(library.Record{
			Key: library.Key{Source: "example-reader", Series: "the-lantern-keeper",
				Volume: string(rune('1' + i))},
			DocumentUUID: uuid,
			SeriesTitle:  "The Lantern Keeper",
			Chapters:     []string{chapter},
		}); err != nil {
			t.Fatal(err)
		}
		uuids = append(uuids, uuid)
	}
	return svc, libStore, seriesDir, uuids
}

// results builds the frontend's report: the first `deleted` documents went, the
// rest did not.
func results(uuids []string, deleted int, removed bool) string {
	parts := make([]string, 0, len(uuids))
	for i, uuid := range uuids {
		ok := i < deleted
		parts = append(parts, `{"documentUuid":"`+uuid+`","trashed":`+boolJSON(ok)+
			`,"removed":`+boolJSON(ok && removed)+`}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func waitForError(t *testing.T, rec *recorder) (string, string) {
	t.Helper()
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	return e.Code, e.Message
}

// Asking must change nothing, and the question has to carry both facts that
// decide whether this is the row the user meant: which series, and how many.
func TestAskingToDeleteASeriesReturnsTheQuestionAndTouchesNothing(t *testing.T) {
	svc, libStore, _, _ := seriesFixture(t, 3)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper"}`)

	var q struct {
		SeriesID      string   `json:"seriesId"`
		DocumentUUIDs []string `json:"documentUuids"`
		Message       string   `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageDeleteSeriesConfirm), &q); err != nil {
		t.Fatal(err)
	}
	if q.SeriesID != "the-lantern-keeper" {
		t.Errorf("the question is about %q", q.SeriesID)
	}
	if len(q.DocumentUUIDs) != 3 {
		t.Errorf("the question names %d documents, want 3", len(q.DocumentUUIDs))
	}
	if !strings.Contains(q.Message, "The Lantern Keeper") {
		t.Errorf("question %q does not name the series", q.Message)
	}
	if !strings.Contains(q.Message, "all 3 downloads") {
		t.Errorf("question %q does not say how many go", q.Message)
	}
	if !strings.Contains(q.Message, "for good") {
		t.Errorf("question %q does not say the delete is permanent", q.Message)
	}
	// The same promise the single delete makes, for the same reason: a user who
	// read the older warning will assume the whole Trash still goes.
	if !strings.Contains(q.Message, "left alone") {
		t.Errorf("question %q does not say the rest of the Trash is left alone", q.Message)
	}
	if n := len(libStore.List()); n != 3 {
		t.Errorf("%d records; asking must not delete anything", n)
	}
}

// One download reads as one download. "all 1 downloads" is the kind of sentence
// that makes a careful app look careless.
func TestTheQuestionForOneDownloadReadsAsOne(t *testing.T) {
	svc, _, _, _ := seriesFixture(t, 1)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper"}`)

	var q struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageDeleteSeriesConfirm), &q); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Message, "the one download") {
		t.Errorf("question %q does not read as one download", q.Message)
	}
}

// The feature: every download of the series goes, its records go, and the pages
// behind them are reclaimed.
func TestDeletingASeriesForgetsEveryRecordAndReclaimsThePages(t *testing.T) {
	svc, libStore, seriesDir, uuids := seriesFixture(t, 3)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":`+results(uuids, 3, true)+`}`)

	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records left; every download was deleted", n)
	}
	for i := range uuids {
		chapter := "/manga/the-lantern-keeper/chapter-" + string(rune('1'+i)) + "/"
		if dir := download.ChapterDir(seriesDir, chapter); exists(t, dir) {
			t.Errorf("the pages for %s survived the delete", chapter)
		}
	}

	// The list is pushed, because the rows on screen are now wrong and the
	// backend is the side that knows what they should say.
	var list struct {
		Series []struct {
			SeriesID string `json:"seriesId"`
		} `json:"series"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageDownloadedList), &list); err != nil {
		t.Fatal(err)
	}
	for _, row := range list.Series {
		if row.SeriesID == "the-lantern-keeper" {
			t.Error("the deleted series is still in the pushed list")
		}
	}
}

// Partial failure is the case this feature is most likely to meet, and the one
// it would be easiest to lie about. Five of seven must read as five of seven.
func TestAPartialDeleteSaysHowManySurvived(t *testing.T) {
	svc, libStore, _, uuids := seriesFixture(t, 3)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":`+results(uuids, 2, true)+`}`)

	// The two that went are forgotten; the one that did not is untouched, so
	// the user can try it again and the row still accounts for it.
	left := libStore.List()
	if len(left) != 1 {
		t.Fatalf("%d records left, want 1", len(left))
	}
	if left[0].DocumentUUID != uuids[2] {
		t.Errorf("the surviving record is %q, want %q", left[0].DocumentUUID, uuids[2])
	}

	rec.wait(t, appload.MessageDownloadedList)
	code, msg := waitForError(t, rec)
	if code != "not_all_deleted" {
		t.Errorf("code %q", code)
	}
	if !strings.Contains(msg, "2 of those downloads") {
		t.Errorf("note %q does not say how many went", msg)
	}
	if !strings.Contains(msg, "1 is still on your reMarkable") {
		t.Errorf("note %q does not say how many are left", msg)
	}
}

// Nothing went: nothing may be forgotten. A record dropped for a document still
// on the tablet is an orphan the user cannot get rid of through Quire.
func TestASeriesDeleteThatFailedEntirelyChangesNothing(t *testing.T) {
	svc, libStore, seriesDir, uuids := seriesFixture(t, 3)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":`+results(uuids, 0, false)+`}`)

	if n := len(libStore.List()); n != 3 {
		t.Errorf("%d records left, want 3; nothing was deleted", n)
	}
	if dir := download.ChapterDir(seriesDir, "/manga/the-lantern-keeper/chapter-1/"); !exists(t, dir) {
		t.Error("pages were reclaimed for a document that is still on the tablet")
	}

	rec.wait(t, appload.MessageDownloadedList)
	code, msg := waitForError(t, rec)
	if code != "not_deleted" {
		t.Errorf("code %q", code)
	}
	if !strings.Contains(msg, "left everything as it was") {
		t.Errorf("note %q does not say the downloads are untouched", msg)
	}
}

// Trashed but not removed is still a delete: the downloads are out of the
// library and the records go. What the note corrects is where they ended up.
func TestDownloadsLeftInTheTrashAreStillDeleted(t *testing.T) {
	svc, libStore, _, uuids := seriesFixture(t, 2)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":`+results(uuids, 2, false)+`}`)

	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records left; the downloads are out of the library", n)
	}

	rec.wait(t, appload.MessageDownloadedList)
	code, msg := waitForError(t, rec)
	if code != "left_in_trash" {
		t.Errorf("code %q", code)
	}
	if !strings.Contains(msg, "2 are still in your reMarkable’s Trash") {
		t.Errorf("note %q does not say how many are in the Trash", msg)
	}
	if strings.Contains(msg, "could not delete") {
		t.Errorf("note %q reads like a failure", msg)
	}
}

// A chapter another series' record still needs must survive. reclaimPages
// already asks that question; this is the series delete asking it once per
// document rather than once for the batch.
func TestDeletingASeriesKeepsPagesAnotherRecordStillNeeds(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	shared := "/manga/the-lantern-keeper/chapter-1/"
	seriesDir := pageCache(t, root, "example-reader", "the-lantern-keeper", shared)
	for i, uuid := range []string{"doc-a", "doc-b"} {
		if err := libStore.Put(library.Record{
			Key: library.Key{Source: "example-reader", Series: "the-lantern-keeper",
				Volume: string(rune('1' + i))},
			DocumentUUID: uuid,
			Chapters:     []string{shared},
		}); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":[{"documentUuid":"doc-a","trashed":true,"removed":true}]}`)

	if n := len(libStore.List()); n != 1 {
		t.Fatalf("%d records left, want 1", n)
	}
	if dir := download.ChapterDir(seriesDir, shared); !exists(t, dir) {
		t.Error("pages a remaining record still needs were reclaimed")
	}
}

// Asking about a series Quire no longer has downloads for is answered, not
// ignored: the row on screen can outlive the records behind it.
func TestAskingToDeleteASeriesWithNoDownloadsSaysSo(t *testing.T) {
	svc, _, _, _ := seriesFixture(t, 1)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"no-such-series"}`)

	code, _ := waitForError(t, rec)
	if code != "not_found" {
		t.Errorf("code %q", code)
	}
}

// Two series, one delete: the other series' records and pages are not collateral.
func TestDeletingOneSeriesLeavesTheOtherAlone(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	otherChapter := "/manga/the-tea-house/chapter-1/"
	otherDir := pageCache(t, root, "example-reader", "the-tea-house", otherChapter)
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-tea-house", Volume: "1"},
		DocumentUUID: "other-doc",
		Chapters:     []string{otherChapter},
	}); err != nil {
		t.Fatal(err)
	}

	chapter := "/manga/the-lantern-keeper/chapter-1/"
	pageCache(t, root, "example-reader", "the-lantern-keeper", chapter)
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "1"},
		DocumentUUID: "doc-a",
		Chapters:     []string{chapter},
	}); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":[{"documentUuid":"doc-a","trashed":true,"removed":true}]}`)

	left := libStore.List()
	if len(left) != 1 || left[0].DocumentUUID != "other-doc" {
		t.Fatalf("records left: %+v", left)
	}
	if dir := download.ChapterDir(otherDir, otherChapter); !exists(t, dir) {
		t.Error("another series' pages were reclaimed")
	}
}

// filedSeriesFixture is a series whose documents are recorded as filed into a
// folder inside Comics, plus the fake web interface that folder lives on.
//
// Its own fixture, and its own folder: a tidy-up that works because an earlier
// case left the library empty is not a tidy-up anyone has tested.
func filedSeriesFixture(t *testing.T, n int) (*service.Service, *library.Store, *fakeLibrary, []string) {
	t.Helper()
	svc, store, libStore, fake, _ := newDownloadServiceWith(t, downloadRoutes(t),
		func(o *service.Options) { o.DownloadDir = filepath.Join(t.TempDir(), "downloads") })
	addSource(t, store)

	fake.mu.Lock()
	fake.entries = append(fake.entries, library.Entry{
		ID: "lantern", Parent: "comics", Type: library.Collection, VisibleName: "The Lantern Keeper"})
	for i := range n {
		fake.entries = append(fake.entries, library.Entry{
			ID: "doc-" + string(rune('a'+i)), Parent: "lantern", Type: library.Document})
	}
	fake.mu.Unlock()

	var uuids []string
	for i := range n {
		uuid := "doc-" + string(rune('a'+i))
		if err := libStore.Put(library.Record{
			Key: library.Key{Source: "example-reader", Series: "the-lantern-keeper",
				Volume: string(rune('1' + i))},
			DocumentUUID: uuid,
			SeriesTitle:  "The Lantern Keeper",
			FolderUUID:   "lantern",
			FolderPath:   []string{"Comics", "The Lantern Keeper"},
		}); err != nil {
			t.Fatal(err)
		}
		uuids = append(uuids, uuid)
	}
	return svc, libStore, fake, uuids
}

// emptyFolder takes the series' documents out of the fake library, which is
// what the frontend's delete does on the tablet.
func emptyFolder(fake *fakeLibrary, parent string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	var left []library.Entry
	for _, e := range fake.entries {
		if e.Parent != parent {
			left = append(left, e)
		}
	}
	fake.entries = left
}

// The feature: the last download of a series goes, the folder is *listed*, it
// comes back empty, and the frontend is asked to remove it.
func TestAnEmptySeriesFolderIsOfferedForDeletion(t *testing.T) {
	svc, _, fake, uuids := filedSeriesFixture(t, 2)
	rec := &recorder{}
	emptyFolder(fake, "lantern")

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":`+results(uuids, 2, true)+`}`)

	var ask struct {
		FolderID   string `json:"folderId"`
		FolderName string `json:"folderName"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageDeleteFolder), &ask); err != nil {
		t.Fatal(err)
	}
	if ask.FolderID != "lantern" {
		t.Errorf("asked to delete %q, want the series folder", ask.FolderID)
	}
	if ask.FolderName != "The Lantern Keeper" {
		t.Errorf("the folder is named %q", ask.FolderName)
	}
}

// A folder the listing still shows something in is left alone -- here because
// the frontend's delete did not actually take the documents off the tablet,
// which is exactly the state a silent failure leaves behind.
func TestAFolderThatStillHasDocumentsIsNotOffered(t *testing.T) {
	svc, _, _, uuids := filedSeriesFixture(t, 2)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":`+results(uuids, 2, true)+`}`)

	rec.wait(t, appload.MessageDownloadedList)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("a folder that still lists its documents was offered for deletion")
	}
}

// A partial delete leaves the folder, and it is never even asked about: the
// documents that survived are still in it.
func TestAPartialSeriesDeleteNeverOffersTheFolder(t *testing.T) {
	svc, _, fake, uuids := filedSeriesFixture(t, 3)
	rec := &recorder{}
	// Even with the folder listing empty -- which it would not be in reality --
	// a run with a failure must not reach the question at all.
	emptyFolder(fake, "lantern")

	handle(t, svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"the-lantern-keeper","confirmed":true,`+
			`"results":`+results(uuids, 2, true)+`}`)

	rec.wait(t, appload.MessageError)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("a partial delete offered the series folder for deletion")
	}
}

// The report: a folder that went is accounted for, once, and in the backend's
// words.
func TestARemovedFolderIsReported(t *testing.T) {
	svc, _, _, _ := filedSeriesFixture(t, 1)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageFolderDeleted,
		`{"folderId":"lantern","folderName":"The Lantern Keeper","trashed":true,"removed":true}`)

	code, msg := waitForError(t, rec)
	if code != "folder_removed" {
		t.Errorf("code %q", code)
	}
	if !strings.Contains(msg, "The Lantern Keeper") {
		t.Errorf("note %q does not name the folder", msg)
	}
}

// A folder that did not go is not mentioned. Claiming a tidy-up that did not
// happen is worse than saying nothing about one nobody asked for.
func TestAFolderThatStayedIsNotMentioned(t *testing.T) {
	svc, _, _, _ := filedSeriesFixture(t, 1)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageFolderDeleted,
		`{"folderId":"lantern","folderName":"The Lantern Keeper","trashed":true,"removed":false}`)

	if hasFrame(rec, appload.MessageError) {
		t.Error("a folder that stayed was reported as removed")
	}
}

// The gap this closes: deleting the *last chapter* from a chapter row leaves
// the same empty folder as deleting the series from its row, and used to leave
// it behind. One path now, reached from both deletes.
func TestDeletingTheLastDownloadOffersItsFolderToo(t *testing.T) {
	svc, _, fake, uuids := filedSeriesFixture(t, 1)
	rec := &recorder{}
	emptyFolder(fake, "lantern")

	handle(t, svc, rec, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuids[0]+`","confirmed":true,"trashed":true,"removed":true}`)

	var ask struct {
		FolderID   string `json:"folderId"`
		FolderName string `json:"folderName"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageDeleteFolder), &ask); err != nil {
		t.Fatal(err)
	}
	if ask.FolderID != "lantern" {
		t.Errorf("asked to delete %q, want the series folder", ask.FolderID)
	}
	if ask.FolderName != "The Lantern Keeper" {
		t.Errorf("the folder is named %q", ask.FolderName)
	}
}

// One chapter of several: the series still has downloads, so its folder is not
// even asked about -- a delete that cannot leave the folder empty should not be
// making HTTP calls to discover that.
func TestDeletingOneOfSeveralLeavesTheFolderAlone(t *testing.T) {
	svc, _, fake, uuids := filedSeriesFixture(t, 3)
	rec := &recorder{}
	// Even with the listing empty, which it would not be in reality: a series
	// with records left must not reach the question at all.
	emptyFolder(fake, "lantern")

	handle(t, svc, rec, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuids[0]+`","confirmed":true,"trashed":true,"removed":true}`)

	rec.wait(t, appload.MessageDownloadDeleted)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("a series that still has downloads was asked about its folder")
	}
}

// A record from before series folders existed points straight at Comics. It is
// not a series folder, and the cheap check catches it before the listing does.
func TestADownloadWithNoSeriesFolderAsksNothing(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	chapter := "/manga/the-lantern-keeper/chapter-1/"
	pageCache(t, root, "example-reader", "the-lantern-keeper", chapter)
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "1"},
		DocumentUUID: "doc-a",
		FolderUUID:   "comics",
		FolderPath:   []string{"Comics"},
		Chapters:     []string{chapter},
	}); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageDeleteDownload,
		`{"documentUuid":"doc-a","confirmed":true,"trashed":true,"removed":true}`)

	rec.wait(t, appload.MessageDownloadDeleted)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("a document filed straight into Comics offered Comics for deletion")
	}
}

// A delete the frontend could not perform changes nothing, including the
// folder: the document is still in it.
func TestAFailedSingleDeleteAsksNothingAboutTheFolder(t *testing.T) {
	svc, _, fake, uuids := filedSeriesFixture(t, 1)
	rec := &recorder{}
	emptyFolder(fake, "lantern")

	handle(t, svc, rec, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuids[0]+`","confirmed":true,"trashed":false}`)

	rec.wait(t, appload.MessageError)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("a delete that did not happen offered the folder for deletion")
	}
}

// A document the user filed somewhere of their own: not inside Comics, so not
// a folder Quire made, so not a folder Quire deletes.
//
// The listing would probably refuse it too -- it is unlikely to be empty -- but
// "probably" is not a guard, and this is the check that gets there first
// without an HTTP call.
func TestAFolderOutsideComicsIsNeverOffered(t *testing.T) {
	svc, store, libStore, root := serviceWithDownloadRoot(t)
	addSource(t, store)

	chapter := "/manga/the-lantern-keeper/chapter-1/"
	pageCache(t, root, "example-reader", "the-lantern-keeper", chapter)
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: "the-lantern-keeper", Volume: "1"},
		DocumentUUID: "doc-a",
		FolderUUID:   "my-own-folder",
		// One level: a folder of the user's at the top, not Comics/<Series>.
		FolderPath: []string{"Reading now"},
		Chapters:   []string{chapter},
	}); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageDeleteDownload,
		`{"documentUuid":"doc-a","confirmed":true,"trashed":true,"removed":true}`)

	rec.wait(t, appload.MessageDownloadDeleted)
	if hasFrame(rec, appload.MessageDeleteFolder) {
		t.Error("a folder of the user's own was offered for deletion")
	}
}
