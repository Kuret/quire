package service_test

import (
	"encoding/json"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
)

// sortAsk is the instruction the backend sends the frontend after a download.
type sortAsk struct {
	DocumentUUIDs []string `json:"documentUuids"`
	FolderID      string   `json:"folderId"`
	CreateUnder   string   `json:"createUnder"`
	FolderName    string   `json:"folderName"`
	CreateComics  bool     `json:"createComics"`
	ComicsName    string   `json:"comicsName"`
	SourceID      string   `json:"sourceId"`
	SeriesID      string   `json:"seriesId"`
	Kind          string   `json:"kind"`
}

// downloadOneWith runs one chapter download against a library whose folders the
// test chose, and returns the sort instruction that followed it.
//
// Each case builds its own library and its own store: a folder decision that
// comes out right because the case before it left a record behind is not a
// test of the decision.
func downloadOneWith(t *testing.T, entries ...library.Entry) (*service.Service, *library.Store, sortAsk, *recorder) {
	t.Helper()
	svc, store, libStore, fake, rec := newDownloadService(t)
	fake.mu.Lock()
	fake.entries = append([]library.Entry(nil), entries...)
	fake.mu.Unlock()
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	waitForPhase(t, rec, "done")

	var ask sortAsk
	if err := json.Unmarshal(rec.wait(t, appload.MessageSortDocuments), &ask); err != nil {
		t.Fatal(err)
	}
	return svc, libStore, ask, rec
}

func comicsFolder() library.Entry {
	return library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"}
}

// With no series folder anywhere, the frontend is asked to make one — under
// Comics, named after the series.
func TestADownloadWithNoFolderAsksForOne(t *testing.T) {
	_, _, ask, _ := downloadOneWith(t, comicsFolder())

	if len(ask.DocumentUUIDs) == 0 {
		t.Fatal("nothing was asked about")
	}
	if ask.FolderID != "" {
		t.Errorf("folderId %q; there is no folder to use yet", ask.FolderID)
	}
	if ask.CreateUnder != "comics" {
		t.Errorf("createUnder %q, want comics", ask.CreateUnder)
	}
	if ask.FolderName != "The Lantern Keeper" {
		t.Errorf("folderName %q, want the series title", ask.FolderName)
	}
	if ask.CreateComics {
		t.Error("asked to create Comics, which is already there")
	}
}

// A series folder that already exists needs no move at all: the upload goes
// straight into it, because Place has preferred Comics/<series> since M5 for
// exactly the case where the user had made one by hand.
//
// So the assertion is that nothing is asked of the frontend *and* the record
// says where the volume is — the quiet path, which is the one a series gets
// every time after its first download.
func TestADownloadIntoAnExistingFolderAsksNothing(t *testing.T) {
	svc, store, libStore, fake, rec := newDownloadService(t)
	fake.mu.Lock()
	fake.entries = []library.Entry{
		comicsFolder(),
		{ID: "lantern", Parent: "comics", Type: library.Collection, VisibleName: "The Lantern Keeper"},
	}
	fake.mu.Unlock()
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	waitForPhase(t, rec, "done")

	rec.mu.Lock()
	for _, f := range rec.sent {
		if f.Type == appload.MessageSortDocuments {
			t.Errorf("asked the frontend to sort a volume that is already in its folder: %s", f.Payload)
		}
	}
	rec.mu.Unlock()

	recs := libStore.List()
	if len(recs) == 0 {
		t.Fatal("nothing was recorded")
	}
	if recs[0].FolderUUID != "lantern" {
		t.Errorf("record says folder %q, want lantern", recs[0].FolderUUID)
	}
	if len(recs[0].FolderPath) != 2 || recs[0].FolderPath[1] != "The Lantern Keeper" {
		t.Errorf("record path %v, want [Comics The Lantern Keeper]", recs[0].FolderPath)
	}
}

// A folder of that name inside Comics, but the upload did not land in it — the
// case where the folder appeared between the upload and the sort, or where the
// name differs only in case. It is used rather than a second one being made
// beside it.
func TestTheBackendPrefersAFolderThatAlreadyExists(t *testing.T) {
	svc, store, _, fake, rec := newDownloadService(t)
	fake.mu.Lock()
	fake.entries = []library.Entry{comicsFolder()}
	fake.mu.Unlock()
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)

	// The folder turns up after the upload has already chosen Comics.
	fake.onUpload = func() {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.entries = append(fake.entries, library.Entry{
			ID: "lantern", Parent: "comics", Type: library.Collection,
			VisibleName: "The Lantern Keeper"})
	}

	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	waitForPhase(t, rec, "done")

	var ask sortAsk
	if err := json.Unmarshal(rec.wait(t, appload.MessageSortDocuments), &ask); err != nil {
		t.Fatal(err)
	}
	if ask.FolderID != "lantern" {
		t.Errorf("folderId %q, want the folder that exists", ask.FolderID)
	}
	if ask.CreateUnder != "comics" {
		t.Errorf("createUnder %q, want comics", ask.CreateUnder)
	}
}

// No Comics folder at all: Quire makes it. The manual setup step only existed
// because creating a folder was thought impossible.
func TestADownloadWithNoComicsFolderAsksForThatToo(t *testing.T) {
	_, _, ask, _ := downloadOneWith(t)

	if !ask.CreateComics {
		t.Error("did not ask for the Comics folder")
	}
	if ask.ComicsName != library.ComicsFolder {
		t.Errorf("comicsName %q, want %q", ask.ComicsName, library.ComicsFolder)
	}
	if ask.FolderName != "The Lantern Keeper" {
		t.Errorf("folderName %q, want the series title", ask.FolderName)
	}
}

// The answer is recorded on every record it names, so the next download of the
// same series goes straight to the folder by id.
func TestASortedDownloadIsRemembered(t *testing.T) {
	svc, libStore, ask, _ := downloadOneWith(t, comicsFolder())

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDocumentsSorted, `{
		"sourceId":"example-reader","seriesId":"`+ask.SeriesID+`",
		"documentUuids":["`+ask.DocumentUUIDs[0]+`"],
		"moved":["`+ask.DocumentUUIDs[0]+`"],
		"folderId":"made-1","folderName":"The Lantern Keeper","created":true,"detail":"moved"}`)

	for _, rec := range libStore.List() {
		if rec.DocumentUUID != ask.DocumentUUIDs[0] {
			continue
		}
		if rec.FolderUUID != "made-1" {
			t.Errorf("record says folder %q, want made-1", rec.FolderUUID)
		}
		if len(rec.FolderPath) != 2 || rec.FolderPath[1] != "The Lantern Keeper" {
			t.Errorf("record path %v, want [Comics The Lantern Keeper]", rec.FolderPath)
		}
		return
	}
	t.Fatal("no record for the document that moved")
}

// A move that did not happen must not be recorded as one: the next download
// would believe it and upload into a folder that is not there.
func TestAnUnsortedDownloadRecordsNothing(t *testing.T) {
	svc, libStore, ask, _ := downloadOneWith(t, comicsFolder())

	before := libStore.List()
	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDocumentsSorted, `{
		"sourceId":"example-reader","seriesId":"`+ask.SeriesID+`",
		"documentUuids":["`+ask.DocumentUUIDs[0]+`"],
		"moved":[],
		"folderId":"made-1","folderName":"The Lantern Keeper","created":true,
		"detail":"the move changed nothing"}`)

	for _, rec := range libStore.List() {
		if rec.DocumentUUID == ask.DocumentUUIDs[0] && rec.FolderUUID == "made-1" {
			t.Fatal("a move that did not happen was recorded")
		}
	}
	if len(libStore.List()) != len(before) {
		t.Errorf("%d records, want the %d there were", len(libStore.List()), len(before))
	}

	// And it is not an error to the user: the download stands.
	fresh.mu.Lock()
	defer fresh.mu.Unlock()
	for _, f := range fresh.sent {
		if f.Type == appload.MessageError {
			t.Errorf("an unsorted download was reported as an error: %s", f.Payload)
		}
	}
}

// A folder the user deleted must send the next download back to looking it up
// by name, not at an id that no longer resolves.
func TestARecordedFolderThatHasGoneIsNotUsed(t *testing.T) {
	svc, _, ask, rec := downloadOneWith(t, comicsFolder())

	// Record a folder that the library does not have.
	handle(t, svc, &recorder{}, appload.MessageDocumentsSorted, `{
		"sourceId":"example-reader","seriesId":"`+ask.SeriesID+`",
		"documentUuids":["`+ask.DocumentUUIDs[0]+`"],
		"moved":["`+ask.DocumentUUIDs[0]+`"],
		"folderId":"deleted-by-the-user","folderName":"The Lantern Keeper","created":true,
		"detail":"moved"}`)

	// A second download of the same series.
	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+ask.SeriesID+`","volumeId":"`+
			ask.SeriesID+`chapter-4/","confirmed":true}`)
	_ = rec

	var second sortAsk
	if err := json.Unmarshal(fresh.wait(t, appload.MessageSortDocuments), &second); err != nil {
		t.Fatal(err)
	}
	if second.FolderID == "deleted-by-the-user" {
		t.Error("the second download aimed at a folder that is not there any more")
	}
	if second.CreateUnder != "comics" {
		t.Errorf("createUnder %q, want comics so a new one can be made", second.CreateUnder)
	}
}
