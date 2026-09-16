package service_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
)

// firstChapters returns the series id and the first n chapter ids of the
// example series, which is what a selection is built out of.
func firstChapters(t *testing.T, svc *service.Service, rec *recorder, n int) (string, []string) {
	t.Helper()
	handle(t, svc, rec, appload.MessageSearch, `{"sourceId":"example-reader","query":"lantern"}`)
	var results struct {
		Series []struct {
			ID string `json:"id"`
		} `json:"series"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &results); err != nil {
		t.Fatal(err)
	}
	if len(results.Series) == 0 {
		t.Fatal("search found nothing to download")
	}
	seriesID := results.Series[0].ID

	handle(t, svc, rec, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	var detail struct {
		Chapters []struct {
			ID string `json:"id"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Chapters) < n {
		t.Fatalf("the fixture series has %d chapters; this test needs %d", len(detail.Chapters), n)
	}
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, detail.Chapters[i].ID)
	}
	return seriesID, ids
}

// jsonList renders ids as a JSON array for a handwritten payload.
func jsonList(ids []string) string {
	b, err := json.Marshal(ids)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// A selection asks once, and the question says how many rows it is about — the
// count is the thing the user can no longer see once the strip is over the
// list.
func TestQueueingASelectionAsksOnceWithTheCount(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, ids := firstChapters(t, svc, rec, 3)

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageEnqueueDownloads, fmt.Sprintf(
		`{"sourceId":"example-reader","seriesId":%q,"chapterIds":%s}`, seriesID, jsonList(ids)))

	var q struct {
		Count   int    `json:"count"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageQueueConfirm), &q); err != nil {
		t.Fatal(err)
	}
	if q.Count != len(ids) {
		t.Errorf("the question is about %d rows, want %d", q.Count, len(ids))
	}
	if !strings.Contains(q.Message, fmt.Sprintf("%d chapters", len(ids))) {
		t.Errorf("question %q does not say how many chapters", q.Message)
	}

	// Asking must not start anything.
	fresh.mu.Lock()
	defer fresh.mu.Unlock()
	for _, f := range fresh.sent {
		if f.Type == appload.MessageDownloadProgress {
			t.Fatalf("asking queued something: %s", f.Payload)
		}
	}
}

// The confirmed send queues every row and answers once. Nothing was skipped, so
// the answer carries no sentence: each row says "Queued." for itself.
func TestQueueingASelectionQueuesEveryRow(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, ids := firstChapters(t, svc, rec, 3)

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageEnqueueDownloads, fmt.Sprintf(
		`{"sourceId":"example-reader","seriesId":%q,"chapterIds":%s,"confirmed":true}`,
		seriesID, jsonList(ids)))

	var r struct {
		Queued  int    `json:"queued"`
		Skipped int    `json:"skipped"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageQueueResult), &r); err != nil {
		t.Fatal(err)
	}
	if r.Queued != len(ids) {
		t.Errorf("queued %d of %d", r.Queued, len(ids))
	}
	if r.Skipped != 0 {
		t.Errorf("skipped %d, want none", r.Skipped)
	}
	if r.Message != "" {
		t.Errorf("message %q; a selection that fitted needs no sentence", r.Message)
	}
}

// The same row twice is one download. A selection is built by tapping, and a
// tap that lands twice must not queue the same chapter twice.
func TestASelectionQueuesADuplicatedRowOnce(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, ids := firstChapters(t, svc, rec, 2)
	doubled := []string{ids[0], ids[1], ids[0]}

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageEnqueueDownloads, fmt.Sprintf(
		`{"sourceId":"example-reader","seriesId":%q,"chapterIds":%s,"confirmed":true}`,
		seriesID, jsonList(doubled)))

	var r struct {
		Queued int `json:"queued"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageQueueResult), &r); err != nil {
		t.Fatal(err)
	}
	if r.Queued != 2 {
		t.Errorf("queued %d, want the two distinct rows", r.Queued)
	}
}

// An empty selection is a frontend bug, and a silent no-op would hide it.
func TestQueueingNothingIsRefused(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageEnqueueDownloads,
		`{"sourceId":"example-reader","seriesId":"s","chapterIds":[]}`)

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

// A selection longer than the queue is answered once, with a sentence about the
// shortfall — not with one refusal per row that did not fit. Fabricated ids are
// enough here: what is under test is what the queue took and what it said, not
// what the downloads do afterwards.
func TestASelectionBeyondTheQueueIsAnsweredOnce(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, _ := firstChapters(t, svc, rec, 1)
	ids := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		ids = append(ids, fmt.Sprintf("/manga/the-lantern-keeper/made-up-%d/", i))
	}

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageEnqueueDownloads, fmt.Sprintf(
		`{"sourceId":"example-reader","seriesId":%q,"chapterIds":%s,"confirmed":true}`,
		seriesID, jsonList(ids)))

	var r struct {
		Queued  int    `json:"queued"`
		Skipped int    `json:"skipped"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageQueueResult), &r); err != nil {
		t.Fatal(err)
	}
	if r.Queued+r.Skipped != len(ids) {
		t.Errorf("queued %d + skipped %d, want %d rows accounted for", r.Queued, r.Skipped, len(ids))
	}
	if r.Skipped == 0 {
		t.Fatalf("a selection of %d against a queue of %d skipped nothing", len(ids), 16)
	}
	if !strings.Contains(r.Message, "didn’t fit") && !strings.Contains(r.Message, "none of those") {
		t.Errorf("message %q does not say what was left out", r.Message)
	}

	// The whole point: one sentence, not one per row that did not fit.
	fresh.mu.Lock()
	defer fresh.mu.Unlock()
	errs := 0
	for _, f := range fresh.sent {
		if f.Type == appload.MessageError {
			errs++
		}
	}
	if errs != 0 {
		t.Errorf("%d error frames; a full queue is answered once, in the result", errs)
	}
}
