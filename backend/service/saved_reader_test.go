package service_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
)

// waitFor polls cond, the way recorder.wait polls for a frame, for state that
// SavePosition changes without ever answering on the socket.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was never met")
}

// saveOne downloads a chapter into Quire's own storage and returns its ids.
func saveOne(t *testing.T, h *downloadHarness, rec *recorder) (seriesID, chapterID string) {
	t.Helper()
	seriesID, chapterID = firstChapter(t, h.svc, rec)
	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "done")
	return seriesID, chapterID
}

func TestOpenSavedReturnsEveryPagePathUpFront(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	handle(t, h.svc, rec, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`"}`)

	var opened struct {
		SourceID     string   `json:"sourceId"`
		SeriesID     string   `json:"seriesId"`
		ChapterID    string   `json:"chapterId"`
		SeriesTitle  string   `json:"seriesTitle"`
		ChapterTitle string   `json:"chapterTitle"`
		Pages        []string `json:"pages"`
		Position     int      `json:"position"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedOpened), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.ChapterID != chapterID {
		t.Errorf("chapterId %q, want %q", opened.ChapterID, chapterID)
	}
	if len(opened.Pages) == 0 {
		t.Fatal("no pages returned")
	}
	for _, p := range opened.Pages {
		if !filepathIsAbs(p) {
			t.Errorf("page %q is not an absolute path", p)
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("page %q does not exist: %v", p, err)
		}
	}
	if opened.Position != 0 {
		t.Errorf("position %d, want 0 on first open", opened.Position)
	}
	if opened.SeriesTitle == "" {
		t.Error("seriesTitle is empty")
	}
}

func filepathIsAbs(p string) bool {
	return len(p) > 0 && p[0] == '/'
}

// SavedOpened's private carries the source's own privacy, not whatever a
// ChapterList screen happened to have in memory — this is what lets a
// chapter opened from the Downloaded overview (which never held a
// ChapterList of its own) still hide "Send to library" correctly for a
// private source.
func TestOpenSavedReportsPrivateSource(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	handle(t, h.svc, rec, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`"}`)

	var opened struct {
		Private   bool `json:"private"`
		InLibrary bool `json:"inLibrary"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedOpened), &opened); err != nil {
		t.Fatal(err)
	}
	if !opened.Private {
		t.Error("private is false for a private source")
	}
	if opened.InLibrary {
		t.Error("inLibrary is true for a chapter never sent to the library")
	}
}

// SavedOpened's inLibrary is what lets "Send to library" disappear on a
// chapter that is both saved and already in the library — again regardless
// of where the reader was opened from.
func TestOpenSavedReportsInLibrary(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec2, "done")

	handle(t, h.svc, rec, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`"}`)

	var opened struct {
		Private   bool `json:"private"`
		InLibrary bool `json:"inLibrary"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedOpened), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.Private {
		t.Error("private is true for a source added without privacy")
	}
	if !opened.InLibrary {
		t.Error("inLibrary is false for a chapter that was sent to the library")
	}
}

// A chapter never saved, or whose record was dropped, answers saved_missing
// rather than a reader payload with nothing in it.
func TestOpenSavedOnAnUnsavedChapterIsRefused(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	handle(t, h.svc, rec, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`"}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "saved_missing" {
		t.Errorf("code %q, want saved_missing", e.Code)
	}
}

// A saved record whose files have gone from disk is treated the same as
// never saved — and it is dropped, so a repeat OpenSaved does not fail the
// same way forever without explanation.
func TestOpenSavedWithMissingFilesDropsTheRecord(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	if err := os.RemoveAll(h.savedDir); err != nil {
		t.Fatal(err)
	}

	handle(t, h.svc, rec, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`"}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "saved_missing" {
		t.Errorf("code %q, want saved_missing", e.Code)
	}

	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	if _, ok := h.shelfStore.Get(key); ok {
		t.Error("the record was not dropped after its files went missing")
	}
}
