package service_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
)

// waitWatchList decodes the next MessageWatchList off rec — watchListMsg is
// watch_test.go's own decode shape, reused here because a watch row's
// continue target is asserted the same way a downloaded row's is.
func waitWatchList(t *testing.T, rec *recorder) watchListMsg {
	t.Helper()
	var msg watchListMsg
	if err := json.Unmarshal(rec.wait(t, appload.MessageWatchList), &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}

// A downloaded row's continue target names the one saved chapter, never
// read yet — the first (and only) one in reading order.
func TestDownloadedRowCarriesContinueForAnUnreadSavedChapter(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	rows := fetchDownloaded(t, h)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.SeriesID != seriesID {
		t.Fatalf("seriesId %q, want %q", row.SeriesID, seriesID)
	}
	if row.Continue.Kind != "saved" || row.Continue.ChapterID != chapterID {
		t.Errorf("continue = %+v, want saved/%s", row.Continue, chapterID)
	}
}

// Once the one saved chapter has been read and finished, the downloaded row's
// continue target moves on to nothing — there is no next chapter saved — but
// stays "saved" naming the chapter itself, since that is still the only
// saved thing to open.
func TestDownloadedRowContinuesTheSameChapterWhenFinishedWithNoNext(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	handle(t, h.svc, rec, appload.MessageOpenSaved,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+`"}`)
	var opened struct {
		Pages []string `json:"pages"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedOpened), &opened); err != nil {
		t.Fatal(err)
	}
	last := len(opened.Pages) - 1
	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	handle(t, h.svc, rec, appload.MessageSavePosition,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","chapterId":"`+chapterID+
			`","position":`+strconv.Itoa(last)+`}`)
	waitFor(t, func() bool {
		got, _ := h.shelfStore.Get(key)
		return got.Position == last
	})

	rows := fetchDownloaded(t, h)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	if rows[0].Continue.Kind != "saved" || rows[0].Continue.ChapterID != chapterID {
		t.Errorf("continue = %+v, want saved/%s (nothing else to open)", rows[0].Continue, chapterID)
	}
}

// A series with a library download and nothing saved continues at the
// library's newest document — what "Read latest" already opens.
func TestDownloadedRowContinuesALibraryOnlySeries(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")

	rows := fetchDownloaded(t, h)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	if rows[0].Continue.Kind != "library" || rows[0].Continue.DocumentUUID == "" {
		t.Errorf("continue = %+v, want a library target", rows[0].Continue)
	}
	if rows[0].Continue.DocumentUUID != rows[0].LatestUUID {
		t.Errorf("continue document %q != latestUuid %q", rows[0].Continue.DocumentUUID, rows[0].LatestUUID)
	}
}

// A watch row carries the same continue target, computed the same way, for a
// series that also has a saved chapter — the whole point being that a tap on
// either screen behaves the same.
func TestWatchRowCarriesContinueForASavedChapter(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	handle(t, h.svc, rec, appload.MessageWatchSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","title":"The Lantern Keeper"}`)
	msg := waitWatchList(t, rec)

	if len(msg.Watched) != 1 {
		t.Fatalf("%d watched rows, want 1: %+v", len(msg.Watched), msg.Watched)
	}
	got := msg.Watched[0].Continue
	if got.Kind != "saved" || got.ChapterID != chapterID {
		t.Errorf("continue = %+v, want saved/%s", got, chapterID)
	}
}

// A watched series with nothing downloaded and nothing saved has nothing to
// continue: the row falls back to browsing the series, same as before this
// existed.
func TestWatchRowContinueIsEmptyWithNothingDownloaded(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := firstChapter(t, h.svc, rec)

	handle(t, h.svc, rec, appload.MessageWatchSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","title":"The Lantern Keeper"}`)
	msg := waitWatchList(t, rec)

	if len(msg.Watched) != 1 {
		t.Fatalf("%d watched rows, want 1", len(msg.Watched))
	}
	if msg.Watched[0].Continue.Kind != "" {
		t.Errorf("continue = %+v, want none", msg.Watched[0].Continue)
	}
}
