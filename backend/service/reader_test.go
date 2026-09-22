package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
)

// Once a volume is downloaded, its chapters carry the document UUID, which is
// what turns the row's button into "Read" after a restart.
func TestSeriesDetailCarriesTheStoredUUID(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)

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
	found := 0
	for _, c := range detail.Chapters {
		if c.DocumentUUID == uuid {
			found++
		}
	}
	if found == 0 {
		t.Fatalf("no chapter row carries %q", uuid)
	}
	if found < 2 {
		t.Errorf("only %d rows carry the UUID; every chapter of the volume should", found)
	}
}

// A "Read" on a document the user has since deleted must not leave the record
// behind: PLAN §6 M7 wants it detected and offered as a re-download.
func TestOpeningAMissingDocumentForgetsIt(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageOpenInReader,
		`{"documentUuid":"`+uuid+`","missing":true}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "document_gone" {
		t.Errorf("code %q", e.Code)
	}
	if !strings.Contains(e.Message, "download it again") {
		t.Errorf("message %q does not offer a re-download", e.Message)
	}
	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records left; the dead UUID must be forgotten", n)
	}
}

// A successful open is recorded and answers nothing: the frontend has already
// opened the document by then, and an error frame would be a lie.
func TestOpeningAPresentDocumentSaysNothing(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")
	uuid, _ := done["documentUuid"].(string)

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageOpenInReader, `{"documentUuid":"`+uuid+`"}`)

	fresh.mu.Lock()
	defer fresh.mu.Unlock()
	if len(fresh.sent) != 0 {
		t.Errorf("sent %d frames, want none", len(fresh.sent))
	}
	if n := len(libStore.List()); n != 1 {
		t.Errorf("%d records, want the volume still remembered", n)
	}
}
