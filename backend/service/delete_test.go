package service_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
)

// recordFor finds the stored record for a document UUID.
func recordFor(t *testing.T, libStore *library.Store, uuid string) library.Record {
	t.Helper()
	for _, rec := range libStore.List() {
		if rec.DocumentUUID == uuid {
			return rec
		}
	}
	t.Fatalf("no record for %q", uuid)
	return library.Record{}
}

// The happy path: the frontend moved the document to xochitl's Trash, so the
// record goes and the assembled PDF goes with it — PLAN §6 M7 wants that space
// back, and the file is of no use once the document it produced is trashed.
func TestTrashingADocumentForgetsItAndReclaimsThePDF(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)
	pdf := recordFor(t, libStore, uuid).PDF

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuid+`","trashed":true}`)

	var reply struct {
		DocumentUUID string `json:"documentUuid"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageDownloadDeleted), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.DocumentUUID != uuid {
		t.Errorf("the reply names %q, want %q", reply.DocumentUUID, uuid)
	}
	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records left; a trashed document must be forgotten", n)
	}
	if pdf == "" {
		t.Fatal("the record carried no PDF path, so this test proves nothing")
	}
	if _, err := os.Stat(pdf); !os.IsNotExist(err) {
		t.Errorf("%s is still on disk; the assembled PDF should have gone with the document", pdf)
	}
}

// The failure that matters most. A record dropped for a document still sitting
// on the tablet is an orphan: Quire stops offering to open it and stops being
// able to delete it, and the user is left to find it by hand.
func TestAFailedTrashKeepsTheRecord(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)
	pdf := recordFor(t, libStore, uuid).PDF

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuid+`","trashed":false}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "not_deleted" {
		t.Errorf("code %q", e.Code)
	}
	if !strings.Contains(e.Message, "still there") {
		t.Errorf("message %q does not say the download is untouched", e.Message)
	}
	if n := len(libStore.List()); n != 1 {
		t.Errorf("%d records; a document that was not trashed must stay remembered", n)
	}
	if _, err := os.Stat(pdf); err != nil {
		t.Errorf("the PDF was removed anyway: %v", err)
	}
}

// Deleting one part of a split volume deletes that part only (PLAN §12.4's
// scope). The other parts are separate documents on the tablet, each still
// complete and still named "part n of m", so nothing is orphaned by leaving
// them — whereas removing records for documents that are still there would
// orphan every one of them at once.
func TestDeletingOnePartLeavesTheOtherParts(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadServiceWith(t, downloadRoutes(t),
		func(o *service.Options) { o.UploadBudgetBytes = 4096 })
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	waitForPhase(t, rec, "done")

	before := libStore.List()
	if len(before) < 2 {
		t.Fatalf("%d records; this test needs a split volume", len(before))
	}
	victim := before[0]

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload,
		`{"documentUuid":"`+victim.DocumentUUID+`","trashed":true}`)
	fresh.wait(t, appload.MessageDownloadDeleted)

	after := libStore.List()
	if len(after) != len(before)-1 {
		t.Fatalf("%d records left, want %d — exactly the one part goes", len(after), len(before)-1)
	}
	for _, r := range after {
		if r.DocumentUUID == victim.DocumentUUID {
			t.Errorf("%q is still remembered", victim.DocumentUUID)
		}
	}
}

// A delete with nothing to delete is a bad request, not a silent no-op: the
// frontend sending one has a bug, and swallowing it hides it.
func TestDeletingNothingIsRefused(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageDeleteDownload, `{"documentUuid":"","trashed":true}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "bad_request" {
		t.Errorf("code %q", e.Code)
	}
}

// The confirmation names the document it is about to remove, so the name has to
// reach the row. It is the stored VisibleName rather than the chapter title:
// for a split volume those differ, and "(part 2 of 3)" is the part the user
// needs to see before they tap.
func TestChapterRowsCarryTheDocumentName(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)
	want := recordFor(t, libStore, uuid).VisibleName
	if want == "" {
		t.Fatal("the record has no VisibleName, so this test proves nothing")
	}

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	var detail struct {
		Chapters []struct {
			DocumentUUID string `json:"documentUuid"`
			DocumentName string `json:"documentName"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	named := 0
	for _, c := range detail.Chapters {
		if c.DocumentUUID != uuid {
			continue
		}
		named++
		if c.DocumentName != want {
			t.Errorf("row names the document %q, want %q", c.DocumentName, want)
		}
	}
	if named == 0 {
		t.Fatalf("no row carries %q", uuid)
	}
}
