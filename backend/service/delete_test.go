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
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)
	pdf := recordFor(t, libStore, uuid).PDF

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuid+`","confirmed":true,"trashed":true,"removed":true}`)

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
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)
	pdf := recordFor(t, libStore, uuid).PDF

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuid+`","confirmed":true,"trashed":false}`)

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
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")

	before := libStore.List()
	if len(before) < 2 {
		t.Fatalf("%d records; this test needs a split volume", len(before))
	}
	victim := before[0]

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload,
		`{"documentUuid":"`+victim.DocumentUUID+`","confirmed":true,"trashed":true,"removed":true}`)
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

	handle(t, svc, rec, appload.MessageDeleteDownload, `{"documentUuid":"","confirmed":true,"trashed":true,"removed":true}`)

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

// The confirmation step. An accidental tap must not destroy a download, so the
// first message deletes nothing and answers with the question — named, so the
// user can see which file is going.
func TestDeletingAsksFirstAndNamesTheDocument(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)
	name := recordFor(t, libStore, uuid).VisibleName

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload, `{"documentUuid":"`+uuid+`"}`)

	var q struct {
		DocumentUUID string `json:"documentUuid"`
		Message      string `json:"message"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageDeleteConfirm), &q); err != nil {
		t.Fatal(err)
	}
	if q.DocumentUUID != uuid {
		t.Errorf("the question is about %q, want %q", q.DocumentUUID, uuid)
	}
	if !strings.Contains(q.Message, name) {
		t.Errorf("question %q does not name %q", q.Message, name)
	}
	if !strings.Contains(q.Message, "for good") {
		t.Errorf("question %q does not say the delete is permanent", q.Message)
	}
	// The promise turned round on 2026-09-17. Deleting used to empty the whole
	// Trash and the question had to warn about it; it now removes that one
	// document, measured against a control folder that sat in the Trash
	// through two deletions. So the question must say the rest of the Trash
	// survives -- and must not still be promising to destroy it.
	if !strings.Contains(q.Message, "Trash") || !strings.Contains(q.Message, "left alone") {
		t.Errorf("question %q does not say the rest of the Trash is left alone", q.Message)
	}
	if strings.Contains(q.Message, "empty") || strings.Contains(q.Message, "anything else already") {
		t.Errorf("question %q still promises to empty the Trash", q.Message)
	}
	if n := len(libStore.List()); n != 1 {
		t.Errorf("%d records; asking must not delete anything", n)
	}
}

// Asking about a document Quire no longer knows about is answered, not
// ignored: the row on screen can outlive the record behind it.
func TestAskingAboutAnUnknownDocumentSaysSo(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageDeleteDownload, `{"documentUuid":"no-such-document"}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "not_found" {
		t.Errorf("code %q", e.Code)
	}
}

// A document left in the Trash is still a delete. The download is out of the
// library and the record is gone, so reporting a failure would tell the user
// their download survived when it did not — but the confirmation promised it
// would be gone for good, and instead it is recoverable, so the note corrects
// that much and says who can finish the job.
func TestATrashThatWillNotEmptyIsStillADelete(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDeleteDownload,
		`{"documentUuid":"`+uuid+`","confirmed":true,"trashed":true,"removed":false}`)

	// The row is put right first: the delete happened.
	fresh.wait(t, appload.MessageDownloadDeleted)
	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records left; the document was deleted", n)
	}

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "left_in_trash" {
		t.Errorf("code %q", e.Code)
	}
	if !strings.Contains(e.Message, "deleted") {
		t.Errorf("note %q does not say the download went", e.Message)
	}
	if strings.Contains(e.Message, "still there") {
		t.Errorf("note %q reads like a failed delete", e.Message)
	}
}
