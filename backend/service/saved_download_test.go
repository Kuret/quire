package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
	"github.com/rickl/quire/backend/theme/themetest"
)

// countImageFetches is how many of calls were for a page image, identified by
// extension the way the download fixtures name them (see downloadRoutes).
func countImageFetches(calls []themetest.Request) int {
	n := 0
	for _, c := range calls {
		if strings.HasSuffix(c.URL, ".jpg") {
			n++
		}
	}
	return n
}

// The design's central default: with no `destination` on the request at all,
// a comic chapter lands in Quire's own storage — not the reMarkable library —
// and is remembered in the shelf store rather than library.Store.
func TestDefaultDownloadSavesInQuire(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}

	seriesID, chapterID := firstChapter(t, h.svc, rec)
	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)

	done := waitForPhase(t, rec, "done")
	if saved, _ := done["saved"].(bool); !saved {
		t.Fatalf("done = %+v, want saved:true", done)
	}
	if dest, _ := done["destination"].(string); dest != "quire" {
		t.Errorf("destination = %q, want quire", dest)
	}
	if msg, _ := done["message"].(string); !strings.Contains(msg, "saved in Quire") {
		t.Errorf("message %q does not say it was saved in Quire", msg)
	}

	if len(h.fake.uploaded) != 0 {
		t.Errorf("%d documents uploaded; a Quire-saved download must never touch the library", len(h.fake.uploaded))
	}
	if n := len(h.libStore.List()); n != 0 {
		t.Errorf("%d library records, want none", n)
	}

	recs := h.shelfStore.ForSeries("example-reader", seriesID)
	if len(recs) != 1 {
		t.Fatalf("%d shelf records, want 1: %+v", len(recs), recs)
	}
	if recs[0].Chapter != chapterID {
		t.Errorf("chapter %q, want %q", recs[0].Chapter, chapterID)
	}
	if len(recs[0].Pages) == 0 {
		t.Fatal("no pages were recorded")
	}
	for _, p := range recs[0].Pages {
		full := filepath.Join(h.savedDir, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("page %q missing on disk: %v", p, err)
		}
	}
}

// A volume-grouped "quire" download saves each chapter as its own record —
// there is no PDF here to give the volume a single identity.
func TestQuireVolumeDownloadSavesEachChapterSeparately(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}

	seriesID, chapterID := firstChapter(t, h.svc, rec)
	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "confirm")

	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")
	if saved, _ := done["saved"].(bool); !saved {
		t.Fatalf("done = %+v, want saved:true", done)
	}

	recs := h.shelfStore.ForSeries("example-reader", seriesID)
	if len(recs) < 2 {
		t.Fatalf("%d shelf records, want one per chapter of the volume: %+v", len(recs), recs)
	}
	if len(h.fake.uploaded) != 0 {
		t.Errorf("%d documents uploaded; a volume grouping must not assemble or upload a PDF for Quire", len(h.fake.uploaded))
	}
}

// Private sources are never put in the library — refused outright, not merely
// unoffered, so a replayed message or an older frontend cannot reach it
// either.
func TestPrivateSourceRefusesLibraryDestination(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}

	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "private_library" {
		t.Errorf("code %q, want private_library", e.Code)
	}
	if !strings.Contains(e.Message, "Private") {
		t.Errorf("message %q does not explain why", e.Message)
	}
	if len(h.fake.uploaded) != 0 {
		t.Error("the private source's chapter was uploaded anyway")
	}
}

// "Send to library" on a chapter already saved in Quire hard-links its saved
// files into the download cache first, so the queue's skip-existing resume
// fetches nothing over the network.
func TestSendToLibrarySeedsCacheAndFetchesNothing(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	// First, an ordinary Quire save.
	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "done")

	imageCallsBefore := countImageFetches(h.fetcherCalls())

	// Now send the same chapter to the library.
	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec2, "done")
	if dest, _ := done["destination"].(string); dest != "library" {
		t.Fatalf("destination = %q, want library", dest)
	}

	// The theme still has to be asked *where* the pages are — Series,
	// Chapters and Pages are cheap metadata lookups every download makes
	// regardless of destination. What must not happen a second time is
	// fetching the page *images themselves*: those are what seedCacheFromSaved
	// exists to make unnecessary.
	imageCallsAfter := countImageFetches(h.fetcherCalls())
	if imageCallsAfter != imageCallsBefore {
		t.Errorf("%d page image(s) were fetched again; sending a saved chapter to the library "+
			"must not refetch its pages", imageCallsAfter-imageCallsBefore)
	}

	if len(h.fake.uploaded) != 1 {
		t.Fatalf("%d documents uploaded, want 1", len(h.fake.uploaded))
	}
	if n := len(h.libStore.List()); n != 1 {
		t.Errorf("%d library records, want 1", n)
	}
	// The saved copy stays: both are independent.
	key := shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID}
	if _, ok := h.shelfStore.Get(key); !ok {
		t.Error("the saved chapter was removed by sending it to the library")
	}
}
