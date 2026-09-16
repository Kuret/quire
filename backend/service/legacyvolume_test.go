package service_test

// Already-downloaded volumes must keep opening — PLAN §6 M4's reversal,
// consequence 3, and the part most likely to break silently.
//
// The default changed from one PDF per volume to one PDF per chapter on
// 2026-09-16. The user had already downloaded volumes when it changed. A
// volume sitting on the tablet that Quire no longer offers "Read" for is not a
// visible failure: the row simply says "Download" again, and the only symptom
// is a second copy appearing in Comics. So both shapes of stored record are
// pinned here.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
)

// chapterIDs asks the backend for a series' chapter list, the way the frontend
// does, and returns the IDs in the order it sends them.
func chapterIDs(t *testing.T, svc *service.Service, rec *recorder, seriesID string) []string {
	t.Helper()
	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	var detail struct {
		Chapters []struct {
			ID string `json:"id"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(detail.Chapters))
	for _, c := range detail.Chapters {
		ids = append(ids, c.ID)
	}
	return ids
}

// uuidsByChapter is the chapter list as the UI sees it: which rows offer Read.
func uuidsByChapter(t *testing.T, svc *service.Service, seriesID string) map[string]string {
	t.Helper()
	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	var detail struct {
		Chapters []struct {
			ID           string `json:"id"`
			DocumentUUID string `json:"documentUuid"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, c := range detail.Chapters {
		if c.DocumentUUID != "" {
			out[c.ID] = c.DocumentUUID
		}
	}
	return out
}

// A volume downloaded before the default changed, with its chapter IDs
// recorded, still puts "Read" on every one of its chapters — even though the
// source is now grouped per chapter and would never produce that volume again.
func TestAPreChangeVolumeRecordStillResolvesForRead(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store) // the new default: one PDF per chapter

	seriesID, _ := firstChapter(t, svc, rec)
	ids := chapterIDs(t, svc, rec, seriesID)
	if len(ids) < 2 {
		t.Fatalf("the fixture has %d chapters; this test needs a volume's worth", len(ids))
	}

	if err := libStore.Put(library.Record{
		Key: library.Key{Source: "example-reader", Series: seriesID, Volume: "1"},
		// Keyed on (source, series, volume) and recording its chapters: this
		// is exactly what a download from before 2026-09-16 left behind.
		DocumentUUID: "old-volume-doc",
		VisibleName:  "The Lantern Keeper — Vol 1.pdf",
		Chapters:     ids,
		StoredAt:     time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	got := uuidsByChapter(t, svc, seriesID)
	for _, id := range ids {
		if got[id] != "old-volume-doc" {
			t.Errorf("chapter %s lost its document when the default changed (uuid %q)", id, got[id])
		}
	}
}

// The older shape: a record with no chapter IDs at all, from before they were
// kept. Resolving it means redoing the grouping — and it has to be redone the
// way it was done *then*, by the source's volume labels, not with today's
// default. Get that wrong and the record matches nothing, silently.
func TestALegacyVolumeRecordWithNoChapterIDsStillResolves(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store) // the new default: one PDF per chapter

	seriesID, _ := firstChapter(t, svc, rec)
	ids := chapterIDs(t, svc, rec, seriesID)
	if len(ids) < 2 {
		t.Fatalf("the fixture has %d chapters; this test needs a volume's worth", len(ids))
	}

	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: "example-reader", Series: seriesID, Volume: "1"},
		DocumentUUID: "legacy-volume-doc",
		VisibleName:  "The Lantern Keeper — Vol 1.pdf",
		StoredAt:     time.Now(),
		// No Chapters: this is the shape that has to be re-derived.
	}); err != nil {
		t.Fatal(err)
	}

	got := uuidsByChapter(t, svc, seriesID)
	if len(got) == 0 {
		t.Fatal("a downloaded volume stopped offering Read entirely — the default change orphaned it")
	}
	for id, uuid := range got {
		if uuid != "legacy-volume-doc" {
			t.Errorf("chapter %s points at %q", id, uuid)
		}
	}
	if len(got) < 2 {
		t.Errorf("only %d chapters resolved to the old volume; it held more", len(got))
	}
}
