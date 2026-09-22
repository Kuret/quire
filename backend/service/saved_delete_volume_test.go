// Deleting a whole saved volume in one round trip — the volume delete half of
// round 2's contract. It rides on MessageDeleteSaved's existing confirm/done
// shape, guarded through the same removeSavedChapter a single-chapter delete
// uses (backend/service/saved_delete.go).
package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
)

// saveWholeVolume saves every chapter of the fixture's one volume in Quire
// and returns the series id and the volume's chapter ids in order.
func saveWholeVolume(t *testing.T, h *downloadHarness, rec *recorder) (seriesID string, chapterIDs []string) {
	t.Helper()
	seriesID, chapterID := firstChapter(t, h.svc, rec)
	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "confirm")
	handle(t, h.svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	waitForPhase(t, rec, "done")

	for _, r := range h.shelfStore.ForSeries("example-reader", seriesID) {
		chapterIDs = append(chapterIDs, r.Chapter)
	}
	if len(chapterIDs) < 2 {
		t.Fatalf("only %d chapters saved, want the fixture's whole volume", len(chapterIDs))
	}
	return seriesID, chapterIDs
}

func deleteVolumePayload(sourceID, seriesID, label string, chapterIDs []string, confirmed bool) string {
	b, err := json.Marshal(map[string]any{
		"sourceId": sourceID, "seriesId": seriesID, "volumeLabel": label,
		"chapterIds": chapterIDs, "confirmed": confirmed,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// A whole-volume delete asks before touching anything, naming how many saved
// chapters would go.
func TestDeleteSavedVolumeAsksBeforeDeleting(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterIDs := saveWholeVolume(t, h, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "Vol 2", chapterIDs, false))

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
	want := "Delete Vol 2 from Quire? Its 4 saved chapters will need downloading again to read."
	if reply.Message != want {
		t.Errorf("message %q, want %q", reply.Message, want)
	}

	// Nothing was touched.
	for _, id := range chapterIDs {
		if _, ok := h.shelfStore.Get(shelf.Key{Source: "example-reader", Series: seriesID, Chapter: id}); !ok {
			t.Errorf("chapter %q was removed before confirmation", id)
		}
	}
}

// The singular form, for a volume with exactly one saved chapter.
func TestDeleteSavedVolumeQuestionIsSingularForOneChapter(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterIDs := saveWholeVolume(t, h, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "Vol 2", chapterIDs[:1], false))

	var reply struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedDeleted), &reply); err != nil {
		t.Fatal(err)
	}
	want := "Delete Vol 2 from Quire? Its 1 saved chapter will need downloading again to read."
	if reply.Message != want {
		t.Errorf("message %q, want %q", reply.Message, want)
	}
}

// Confirmed, a whole-volume delete removes every one of its saved chapters
// through the same guarded removeSavedChapter a single delete uses, and the
// reply names exactly the ones it deleted.
func TestDeleteSavedVolumeRemovesEveryChapter(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterIDs := saveWholeVolume(t, h, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "Vol 2", chapterIDs, true))

	var reply struct {
		Phase      string   `json:"phase"`
		Message    string   `json:"message"`
		ChapterIDs []string `json:"chapterIds"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedDeleted), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Phase != "done" {
		t.Fatalf("phase %q, want done: %+v", reply.Phase, reply)
	}
	if reply.Message != "Vol 2 is deleted." {
		t.Errorf("message %q, want the plain done sentence", reply.Message)
	}
	if len(reply.ChapterIDs) != len(chapterIDs) {
		t.Fatalf("chapterIds = %v, want all %d deleted", reply.ChapterIDs, len(chapterIDs))
	}

	for _, id := range chapterIDs {
		if _, ok := h.shelfStore.Get(shelf.Key{Source: "example-reader", Series: seriesID, Chapter: id}); ok {
			t.Errorf("chapter %q survived the volume delete", id)
		}
	}
}

// A chapterIds list naming a chapter Quire never saved is not an error: the
// volume row only knows what the volume contains, and the chapters that are
// not saved are silently skipped rather than deleted or refused wholesale.
func TestDeleteSavedVolumeIgnoresChaptersNotActuallySaved(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterIDs := saveWholeVolume(t, h, rec)

	// Delete all but the last chapter, then ask again with the full list —
	// the already-gone chapter must not turn the second call into an error.
	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "Vol 2", chapterIDs[:len(chapterIDs)-1], true))
	rec.wait(t, appload.MessageSavedDeleted)

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "Vol 2", chapterIDs, true))
	var reply struct {
		Phase      string   `json:"phase"`
		ChapterIDs []string `json:"chapterIds"`
	}
	if err := json.Unmarshal(rec2.wait(t, appload.MessageSavedDeleted), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Phase != "done" {
		t.Fatalf("phase %q, want done", reply.Phase)
	}
	if len(reply.ChapterIDs) != 1 || reply.ChapterIDs[0] != chapterIDs[len(chapterIDs)-1] {
		t.Errorf("chapterIds = %v, want only the last, still-saved chapter", reply.ChapterIDs)
	}
}

// Mutation check: a volume delete only ever removes the chapters it was
// named — never chapters of a different series or a different volume that
// happen to be saved at the same time.
func TestDeleteSavedVolumeLeavesOtherSeriesAlone(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterIDs := saveWholeVolume(t, h, rec)

	// A second, unrelated saved record that must survive the delete.
	other := shelf.Record{Key: shelf.Key{Source: "example-reader", Series: "/manga/other/", Chapter: "/manga/other/c1/"}}
	if err := h.shelfStore.Put(other); err != nil {
		t.Fatal(err)
	}

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "Vol 2", chapterIDs, true))
	rec.wait(t, appload.MessageSavedDeleted)

	if _, ok := h.shelfStore.Get(other.Key); !ok {
		t.Error("an unrelated series' saved chapter was removed by a volume delete for another series")
	}
}

// Deleting a whole-volume request that names no chapterIds Quire actually has
// saved is refused rather than silently answered as done.
func TestDeleteSavedVolumeWithNothingSavedIsRefused(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := firstChapter(t, h.svc, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "Vol 2", []string{chapterID}, true))

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

// A confirm question for a volume with no label falls back to a plain
// sentence that still reads as a sentence.
func TestDeleteSavedVolumeWithNoLabelFallsBackToAPlainSentence(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addVolumeSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterIDs := saveWholeVolume(t, h, rec)

	handle(t, h.svc, rec, appload.MessageDeleteSaved,
		deleteVolumePayload("example-reader", seriesID, "", chapterIDs, false))

	var reply struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSavedDeleted), &reply); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reply.Message, "Delete This volume from Quire?") {
		t.Errorf("message %q, want the plain fallback label", reply.Message)
	}
}
