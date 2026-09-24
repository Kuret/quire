package service_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/shelf"
)

type savedDownloadedRow struct {
	SourceID             string         `json:"sourceId"`
	SeriesID             string         `json:"seriesId"`
	Title                string         `json:"title"`
	Detail               string         `json:"detail"`
	SavedCount           int            `json:"savedCount"`
	LatestSavedChapterID string         `json:"latestSavedChapterId"`
	LatestUUID           string         `json:"latestUuid"`
	Continue             continueTarget `json:"continue"`
}

// continueTarget mirrors service.continueTarget's JSON shape for a
// service_test-package test to decode into.
type continueTarget struct {
	Kind         string `json:"kind"`
	ChapterID    string `json:"chapterId"`
	DocumentUUID string `json:"documentUuid"`
}

func fetchDownloaded(t *testing.T, h *downloadHarness) []savedDownloadedRow {
	t.Helper()
	return fetchDownloadedMsg(t, h, false).Series
}

type downloadedListMsg struct {
	Series  []savedDownloadedRow `json:"series"`
	Private bool                 `json:"private"`
	Storage string               `json:"storage"`
	Empty   string               `json:"empty"`
}

func fetchDownloadedMsg(t *testing.T, h *downloadHarness, private bool) downloadedListMsg {
	t.Helper()
	rec := &recorder{}
	payload := `{}`
	if private {
		payload = `{"private":true}`
	}
	handle(t, h.svc, rec, appload.MessageListDownloaded, payload)
	var got downloadedListMsg
	if err := json.Unmarshal(rec.wait(t, appload.MessageDownloadedList), &got); err != nil {
		t.Fatal(err)
	}
	return got
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

// TestPrivateDownloadedOverviewListsOnlyPrivateSources is the mirror of
// TestDownloadedOverviewExcludesPrivateSources: asking with private:true
// gets exactly the private source's series, and the reply echoes private.
func TestPrivateDownloadedOverviewListsOnlyPrivateSources(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}

	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "done")

	msg := fetchDownloadedMsg(t, h, true)
	if !msg.Private {
		t.Error("reply does not echo private:true")
	}
	if len(msg.Series) != 1 || msg.Series[0].SeriesID != seriesID {
		t.Fatalf("private series = %+v, want the one private-source series", msg.Series)
	}

	// And the ordinary list, asked the same way as before, still excludes it.
	ordinary := fetchDownloadedMsg(t, h, false)
	if len(ordinary.Series) != 0 {
		t.Fatalf("ordinary series = %+v, want the private source excluded", ordinary.Series)
	}
}

// TestDownloadedListCarriesAStorageSentence covers PLAN §12.6's storage
// summary riding on MessageDownloadedList: a build with something downloaded
// gets a non-empty, backend-composed sentence, on both the ordinary and the
// private overview — the figure covers everything, so it does not depend on
// which list was asked for.
func TestDownloadedListCarriesAStorageSentence(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	saveOne(t, h, rec)

	ordinary := fetchDownloadedMsg(t, h, false)
	if ordinary.Storage == "" {
		t.Error("no storage sentence on the ordinary overview")
	}

	private := fetchDownloadedMsg(t, h, true)
	if private.Storage != ordinary.Storage {
		t.Errorf("storage sentence differs by mode: ordinary %q, private %q",
			ordinary.Storage, private.Storage)
	}
}

// The overview is ordered by each series' latest activity across both stores.
// It used to list every series with a library record first and every
// saved-only series after them, so a book just downloaded into Quire — or just
// read — sank to the last page behind older library downloads.
func TestDownloadedOverviewPutsTheMostRecentActivityFirst(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	now := time.Now()

	// An old library download, and one from yesterday.
	for _, r := range []library.Record{
		{Key: library.Key{Source: "example-reader", Series: "old-lib", Volume: "1"},
			DocumentUUID: "u-old", SeriesTitle: "Old Library", StoredAt: now.Add(-72 * time.Hour)},
		{Key: library.Key{Source: "example-reader", Series: "yesterday-lib", Volume: "1"},
			DocumentUUID: "u-yday", SeriesTitle: "Yesterday Library", StoredAt: now.Add(-24 * time.Hour)},
	} {
		if err := h.libStore.Put(r); err != nil {
			t.Fatal(err)
		}
	}
	// A book saved in Quire an hour ago, and a chapter saved last week but
	// read a minute ago.
	for _, r := range []shelf.Record{
		{Key: shelf.Key{Source: "example-reader", Series: "fresh-book", Chapter: "c1"},
			SeriesTitle: "Fresh Book", Kind: "book", SavedAt: now.Add(-time.Hour)},
		{Key: shelf.Key{Source: "example-reader", Series: "just-read", Chapter: "c1"},
			SeriesTitle: "Just Read", SavedAt: now.Add(-7 * 24 * time.Hour), LastReadAt: now.Add(-time.Minute)},
	} {
		if err := h.shelfStore.Put(r); err != nil {
			t.Fatal(err)
		}
	}

	var got []string
	for _, row := range fetchDownloaded(t, h) {
		got = append(got, row.SeriesID)
	}
	want := []string{"just-read", "fresh-book", "yesterday-lib", "old-lib"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v, want %v", got, want)
	}
}
