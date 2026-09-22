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
