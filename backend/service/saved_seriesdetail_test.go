package service_test

import (
	"encoding/json"
	"testing"

	"github.com/rickl/quire/backend/appload"
)

// A chapter saved in Quire is marked as such on the series detail's chapter
// rows, so the chapter list can offer [Delete] [Read] rather than [Try]
// [Download] for it.
func TestSeriesDetailMarksSavedChapters(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	// A fresh recorder: rec already holds the SeriesDetailResult firstChapter
	// (inside saveOne) fetched before the chapter was saved, and wait would
	// happily return that stale frame instead of waiting for a new one.
	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	var detail struct {
		Private  bool `json:"private"`
		Chapters []struct {
			ID    string `json:"id"`
			Saved bool   `json:"saved"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(rec2.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Private {
		t.Error("private = true for a source that was never marked private")
	}
	found := false
	for _, c := range detail.Chapters {
		if c.ID == chapterID {
			found = true
			if !c.Saved {
				t.Error("the saved chapter's row does not say saved:true")
			}
		} else if c.Saved {
			t.Errorf("chapter %q says saved:true but was never saved", c.ID)
		}
	}
	if !found {
		t.Fatal("the saved chapter is missing from the chapter list")
	}
}

// SeriesDetailResult's top-level `private` flag lets the UI hide "Send to
// library" without a round trip.
func TestSeriesDetailReportsAPrivateSource(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := firstChapter(t, h.svc, rec)

	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	var detail struct {
		Private bool `json:"private"`
	}
	if err := json.Unmarshal(rec2.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	if !detail.Private {
		t.Error("private = false for a source marked private")
	}
}
