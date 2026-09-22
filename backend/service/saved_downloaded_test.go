package service_test

import (
	"encoding/json"
	"testing"

	"github.com/rickl/quire/backend/appload"
)

type savedDownloadedRow struct {
	SourceID             string `json:"sourceId"`
	SeriesID             string `json:"seriesId"`
	Title                string `json:"title"`
	Detail               string `json:"detail"`
	SavedCount           int    `json:"savedCount"`
	LatestSavedChapterID string `json:"latestSavedChapterId"`
	LatestUUID           string `json:"latestUuid"`
}

func fetchDownloaded(t *testing.T, h *downloadHarness) []savedDownloadedRow {
	t.Helper()
	rec := &recorder{}
	handle(t, h.svc, rec, appload.MessageListDownloaded, `{}`)
	var got struct {
		Series []savedDownloadedRow `json:"series"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageDownloadedList), &got); err != nil {
		t.Fatal(err)
	}
	return got.Series
}

// A series with only chapters saved in Quire — nothing in the library at all
// — still gets a row on the downloaded overview.
func TestDownloadedOverviewListsASavedOnlySeries(t *testing.T) {
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
		t.Errorf("seriesId %q, want %q", row.SeriesID, seriesID)
	}
	if row.SavedCount != 1 {
		t.Errorf("savedCount %d, want 1", row.SavedCount)
	}
	if row.LatestSavedChapterID != chapterID {
		t.Errorf("latestSavedChapterId %q, want %q", row.LatestSavedChapterID, chapterID)
	}
	if row.LatestUUID != "" {
		t.Errorf("latestUuid %q, want empty for a series with no library record", row.LatestUUID)
	}
	if row.Detail == "" {
		t.Error("no detail sentence")
	}
}

// A series with both a library download and a saved chapter reports both
// counts in one sentence.
func TestDownloadedOverviewCombinesLibraryAndSavedCounts(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	// Saved, then also sent to the library — independent copies of the same
	// chapter.
	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "done")
	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec2, "done")

	rows := fetchDownloaded(t, h)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.SavedCount != 1 {
		t.Errorf("savedCount %d, want 1", row.SavedCount)
	}
	if row.LatestUUID == "" {
		t.Error("latestUuid is empty even though a library record exists")
	}
	want := "1 chapter saved in Quire · 1 in your library"
	if row.Detail != want {
		t.Errorf("detail %q, want %q", row.Detail, want)
	}
}

// Private sources are excluded from the downloaded overview entirely, saved
// chapters included: private content is reached only via the private source
// list.
func TestDownloadedOverviewExcludesPrivateSources(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)
	_ = chapterID

	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}

	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "done")

	rows := fetchDownloaded(t, h)
	if len(rows) != 0 {
		t.Fatalf("%d rows, want none for a private source: %+v", len(rows), rows)
	}
}
