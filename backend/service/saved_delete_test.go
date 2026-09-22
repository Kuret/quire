package service_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
)

func TestDeleteSavedAsksBeforeDeleting(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`"}`)

	var reply struct {
		Phase   string `json:"phase"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedDeleted), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Phase != "confirm" {
		t.Fatalf("phase %q, want confirm", reply.Phase)
	}
	if reply.Message == "" {
		t.Error("no question sentence")
	}

	// Nothing was touched.
	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	if _, ok := h.shelfStore.Get(key); !ok {
		t.Error("the record was removed before confirmation")
	}
}

// Deleting removes the chapter's files outright — no Trash, since there is no
// xochitl document — and drops the record.
func TestDeleteSavedRemovesFilesOutright(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	before, ok := h.shelfStore.Get(key)
	if !ok {
		t.Fatal("chapter was not saved")
	}
	if len(before.Pages) == 0 {
		t.Fatal("no pages to check")
	}
	firstPage := before.Pages[0]

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`","confirmed":true}`)

	var reply struct {
		Phase   string `json:"phase"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedDeleted), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Phase != "done" {
		t.Fatalf("phase %q, want done: %+v", reply.Phase, reply)
	}

	if _, ok := h.shelfStore.Get(key); ok {
		t.Error("the record survived the delete")
	}
	if _, err := os.Stat(h.savedDir + "/" + firstPage); err == nil {
		t.Error("the page file survived the delete")
	}
}

// A delete of a chapter that is not saved is refused rather than answering as
// though it worked.
func TestDeleteSavedOnAnUnsavedChapterIsRefused(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`","confirmed":true}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "not_found" {
		t.Errorf("code %q, want not_found", e.Code)
	}
}

// Deleting a series' downloads also deletes its chapters saved in Quire —
// they are a separate store, but the same delete from the user's point of
// view.
func TestDeleteSeriesAlsoDeletesItsSavedChapters(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	if _, ok := h.shelfStore.Get(key); !ok {
		t.Fatal("chapter was not saved")
	}

	handle(t, h.svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","confirmed":true,"results":[]}`)

	waitFor(t, func() bool {
		_, ok := h.shelfStore.Get(key)
		return !ok
	})
}

// A series that has only saved chapters — no library record at all — still
// answers the confirm step rather than "not found": the Downloaded row it
// came from exists precisely because of those chapters.
func TestDeleteSeriesAsksAboutASavedOnlySeries(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := saveOne(t, h, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	var q struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageDeleteSeriesConfirm), &q); err != nil {
		t.Fatal(err)
	}
	if q.Message == "" {
		t.Error("no question sentence for a saved-only series")
	}
}

// Removing a source deletes its saved chapters too: there is no library
// document to leave behind for them the way a removed source's reMarkable
// downloads are left alone.
func TestRemovingASourceDeletesItsSavedChapters(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	if _, ok := h.shelfStore.Get(key); !ok {
		t.Fatal("chapter was not saved")
	}

	handle(t, h.svc, rec, appload.MessageRemoveSource, `{"sourceId":"example-reader"}`)

	waitFor(t, func() bool {
		_, ok := h.shelfStore.Get(key)
		return !ok
	})
}

// removeQuestion (backend/service/service.go's buildSourceViews) is composed
// in the backend rather than in ui/SourceList.qml's confirm strip (PLAN §2)
// because removing a source cascades to delete its saved chapters too, and
// only the backend knows whether there are any.
func sourceRemoveQuestion(t *testing.T, rec *recorder) string {
	t.Helper()
	var sources struct {
		Sources []struct {
			RemoveQuestion string `json:"removeQuestion"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSources), &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources.Sources) != 1 {
		t.Fatalf("%d sources, want 1", len(sources.Sources))
	}
	return sources.Sources[0].RemoveQuestion
}

// A source with nothing saved in Quire gets the plain old sentence: removing
// it has always left downloaded volumes in the library, untouched.
func TestRemoveSourceQuestionWithNothingSaved(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}

	handle(t, h.svc, rec, appload.MessageListSources, `{}`)

	got := sourceRemoveQuestion(t, rec)
	want := "Remove Example Reader? Downloaded volumes stay in your library."
	if got != want {
		t.Errorf("removeQuestion = %q, want %q", got, want)
	}
}

// A source with chapters saved in Quire names how many, singular for one.
func TestRemoveSourceQuestionWithSavedChapters(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	saveOne(t, h, rec)

	handle(t, h.svc, rec, appload.MessageListSources, `{}`)

	got := sourceRemoveQuestion(t, rec)
	want := "Remove Example Reader? Its 1 chapter saved in Quire will be deleted; " +
		"anything in your library stays."
	if got != want {
		t.Errorf("removeQuestion = %q, want %q", got, want)
	}
}

// More than one saved chapter is plural, and counted across every series of
// the source — not just the one saveOne downloaded from.
func TestRemoveSourceQuestionWithSeveralSavedChapters(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	saveOne(t, h, rec)

	// A second, synthetic record in another series: removeQuestion counts
	// chapters saved *anywhere* under the source, not one series' worth.
	if err := h.shelfStore.Put(shelf.Record{
		Key: shelf.Key{Source: "example-reader", Series: "/manga/other/", Chapter: "/manga/other/c1/"},
	}); err != nil {
		t.Fatal(err)
	}

	handle(t, h.svc, rec, appload.MessageListSources, `{}`)

	got := sourceRemoveQuestion(t, rec)
	want := "Remove Example Reader? Its 2 chapters saved in Quire will be deleted; " +
		"anything in your library stays."
	if got != want {
		t.Errorf("removeQuestion = %q, want %q", got, want)
	}
}
