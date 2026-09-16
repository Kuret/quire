package service_test

// Reporting that strips were split — PLAN §12.3.
//
// Splitting silently is what makes "I'd hate for an actual manga to be
// recognised as a webtoon and get weirdly split" an unanswerable worry: the
// only way to find out would be to notice the reader looks wrong and guess at
// the cause. So the download's own result says what was cut up — and says
// nothing at all when nothing was, because a line that always appears is a
// line nobody reads (the same rule as PLAN §12.2's empty summary).

import (
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/theme"
)

// doneMessage returns the done phase's payload for a strip download.
func doneMessage(t *testing.T, mode string) map[string]any {
	t.Helper()
	svc, store, _, _, rec := newDownloadServiceWith(t, stripRoutes(t))
	// Grouped by volume, so one download covers the whole fixture and the
	// counts below are the whole fixture's. Per chapter — the default since
	// PLAN §6 M4 was reversed on 2026-09-16 — would report a quarter of it,
	// which is true but a weaker test of the sentence.
	addSourceWithSplitStrips(t, store, mode, theme.GroupingVolume)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	return waitForPhase(t, rec, "done")
}

// TestASplitDownloadSaysSo is the visibility half of PLAN §12.3. The worry that
// prompted the feature was an ordinary manga being mistaken for a webtoon, and
// splitting that happens silently makes that worry unanswerable: the only way
// to find out would be to notice the reader looks wrong and guess why.
func TestASplitDownloadSaysSo(t *testing.T) {
	done := doneMessage(t, "auto")

	msg, _ := done["message"].(string)
	if !strings.Contains(msg, "split") {
		t.Errorf("the download finished having cut 20 strips up and said nothing about it: %q", msg)
	}
	// The sentence has to carry both numbers, or it does not tell the user
	// enough to judge whether the split was right.
	for _, want := range []string{"20", "60"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not say %s: %q", want, msg)
		}
	}
	if v, _ := done["pagesSplit"].(float64); int(v) != 20 {
		t.Errorf("pagesSplit = %v, want 20", done["pagesSplit"])
	}
	if v, _ := done["pagesFromSplit"].(float64); int(v) != 60 {
		t.Errorf("pagesFromSplit = %v, want 60", done["pagesFromSplit"])
	}
	t.Logf("done message: %q", msg)
}

// TestAnUnsplitDownloadSaysNothingAboutSplitting is the other half, and the one
// that keeps the line worth reading. A note that appears every time is a note
// nobody reads (PLAN §12.2's empty-summary rule), so an ordinary manga volume
// must carry neither the sentence nor the counters.
func TestAnUnsplitDownloadSaysNothingAboutSplitting(t *testing.T) {
	// The default fixture is ordinary 200×300 manga pages, not strips.
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")

	msg, _ := done["message"].(string)
	if strings.Contains(msg, "split") {
		t.Errorf("an ordinary manga download talked about splitting: %q", msg)
	}
	if _, ok := done["pagesSplit"]; ok {
		t.Errorf("pagesSplit was sent for a download that split nothing: %v", done["pagesSplit"])
	}
	if _, ok := done["pagesFromSplit"]; ok {
		t.Errorf("pagesFromSplit was sent for a download that split nothing: %v", done["pagesFromSplit"])
	}
}

// TestNeverSaysNothingAboutSplittingEither — a user who turned splitting off
// should not then be told about it. Same input as the auto case, so this is the
// override's effect on the wording and not a different fixture.
func TestNeverSaysNothingAboutSplittingEither(t *testing.T) {
	done := doneMessage(t, "never")
	msg, _ := done["message"].(string)
	if strings.Contains(msg, "split") {
		t.Errorf("splitStrips=never still reported splitting: %q", msg)
	}
	if _, ok := done["pagesSplit"]; ok {
		t.Errorf("pagesSplit was sent with splitStrips=never: %v", done["pagesSplit"])
	}
}
