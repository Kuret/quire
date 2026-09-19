package service_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
)

// errorOf decodes the one error frame a refusal is made of.
func errorOf(t *testing.T, rec *recorder) (code, message string) {
	t.Helper()
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	return e.Code, e.Message
}

// searchCoverURL runs a search and returns the series and the cover URL the
// listing carried, so a test asserts against what the source actually said
// rather than a URL copied out of a fixture.
func searchCoverURL(t *testing.T, svc *service.Service, rec *recorder) (seriesID, coverURL string) {
	t.Helper()
	handle(t, svc, rec, appload.MessageSearch, `{"sourceId":"example-reader","query":"lantern"}`)
	var results struct {
		Series []struct {
			ID       string `json:"id"`
			CoverURL string `json:"coverUrl"`
		} `json:"series"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &results); err != nil {
		t.Fatal(err)
	}
	if len(results.Series) == 0 {
		t.Fatal("the search found nothing")
	}
	if results.Series[0].CoverURL == "" {
		t.Fatal("the fixture listing carries no cover URL, so this test proves nothing")
	}
	return results.Series[0].ID, results.Series[0].CoverURL
}

// The reason the cache exists: the Downloaded screen is built from local
// records and never sees a listing, so the URL a search went past weeks ago is
// the only cover it can draw.
func TestACoverURLFromASearchReachesTheDownloadedRow(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, coverURL := searchCoverURL(t, svc, rec)
	if err := libStore.Put(storedVolume("example-reader", seriesID,
		"The Lantern Keeper", "1", "doc-a")); err != nil {
		t.Fatal(err)
	}

	row := askDownloaded(t, svc).Series[0]
	if row.CoverURL != coverURL {
		t.Errorf("downloaded row cover = %q, want the URL the listing gave: %q", row.CoverURL, coverURL)
	}
	// And the newest download is openable in the stock reader without another
	// screen in between: MessageOpenInReader takes a uuid and nothing else.
	if row.LatestUUID != "doc-a" {
		t.Errorf("latestUuid = %q, want the newest downloaded document", row.LatestUUID)
	}
}

// The same cover on the other screen, learnt the other way: opening the series
// is the other moment a cover URL passes through.
func TestACoverURLReachesTheWatchRow(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()

	var detail struct {
		Series struct {
			CoverURL string `json:"coverUrl"`
		} `json:"series"`
	}
	if err := json.Unmarshal(h.rec.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Series.CoverURL == "" {
		t.Fatal("the fixture series has no cover URL, so this test proves nothing")
	}

	h.forget()
	h.watch()
	rows := h.list()
	if len(rows) != 1 {
		t.Fatalf("%d watched rows, want 1", len(rows))
	}
	if rows[0].CoverURL != detail.Series.CoverURL {
		t.Errorf("watch row cover = %q, want %q", rows[0].CoverURL, detail.Series.CoverURL)
	}
}

// Everything downloaded before the cache existed has no URL, and that is the
// ordinary case rather than a broken row: a blank tile, a full sentence, no
// error.
func TestASeriesWithNoStoredCoverURLIsAnEmptyString(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)
	if err := libStore.Put(storedVolume("example-reader", "/manga/the-lantern-keeper/",
		"The Lantern Keeper", "1", "doc-a")); err != nil {
		t.Fatal(err)
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 1 {
		t.Fatalf("%d rows, want 1", len(got.Series))
	}
	if got.Series[0].CoverURL != "" {
		t.Errorf("cover = %q, want none for a series nothing has listed", got.Series[0].CoverURL)
	}
	if got.Series[0].Detail != "1 download" {
		t.Errorf("detail %q; the row is complete without a cover", got.Series[0].Detail)
	}
}

// A volume split to fit the upload cap is several documents written seconds
// apart. "Read the newest" opens the first part of it, not the third.
func TestLatestUUIDOpensTheFirstPartOfASplitVolume(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)

	// The labels are the ones a real split writes — they are the library key,
	// so each part has its own — and the last part is the newest record.
	for _, part := range []struct {
		n     int
		label string
		uuid  string
	}{
		{1, "7 (part 1 of 2)", "doc-part-1"},
		{2, "7 (part 2 of 2)", "doc-part-2"},
	} {
		rec := storedVolume("example-reader", "/manga/the-lantern-keeper/", "The Lantern Keeper", part.label, part.uuid)
		rec.Part, rec.Parts = part.n, 2
		rec.StoredAt = fixedNow.Add(time.Duration(part.n) * time.Minute)
		if err := libStore.Put(rec); err != nil {
			t.Fatal(err)
		}
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 1 {
		t.Fatalf("%d rows, want 1", len(got.Series))
	}
	if got.Series[0].LatestUUID != "doc-part-1" {
		t.Errorf("latestUuid = %q, want the first part of the newest volume", got.Series[0].LatestUUID)
	}
}

// watchAndCheck watches the series with a baseline that leaves `unseen`
// chapters out, then runs a check against the unchanged site. The result is a
// badge over a stored list of exactly those chapters, without needing the
// fixture site to change under the test.
func watchAndCheck(t *testing.T, svc *service.Service, store *state.Store, rec *recorder,
	seriesID string, all []string, unseen int) []string {
	t.Helper()
	if _, err := store.Watch("example-reader", seriesID, "The Lantern Keeper",
		all[unseen:], fixedNow); err != nil {
		t.Fatal(err)
	}
	handle(t, svc, rec, appload.MessageCheckWatched, `{}`)
	rec.wait(t, appload.MessageWatchList)
	return all[:unseen]
}

// The badge and the button under it must name the same chapters, so the
// download queues exactly what the check stored — no more, and nothing the user
// has already read.
func TestDownloadingNewChaptersQueuesExactlyThose(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, ids := firstChapters(t, svc, rec, 4)
	want := watchAndCheck(t, svc, store, rec, seriesID, ids, 2)

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDownloadNewChapters,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	var r struct {
		Queued  int `json:"queued"`
		Skipped int `json:"skipped"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageQueueResult), &r); err != nil {
		t.Fatal(err)
	}
	if r.Queued != len(want) || r.Skipped != 0 {
		t.Fatalf("queued %d and skipped %d, want %d queued", r.Queued, r.Skipped, len(want))
	}

	queued := map[string]bool{}
	for _, p := range progressOf(t, fresh) {
		if phase, _ := p["phase"].(string); phase != "queued" {
			continue
		}
		id, _ := p["volumeId"].(string)
		queued[id] = true
	}
	if len(queued) != len(want) {
		t.Fatalf("%d rows queued, want %d: %v", len(queued), len(want), queued)
	}
	for _, id := range want {
		if !queued[id] {
			t.Errorf("%q was badged as new and was not queued", id)
		}
	}
}

// A button that does nothing is indistinguishable from one that failed. The
// row can honestly be tapped after something else has cleared it, so the
// answer is a sentence.
func TestDownloadingNewChaptersSaysSoWhenThereAreNone(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, ids := firstChapters(t, svc, rec, 3)
	// Watched with the whole list as the baseline: nothing is new.
	if _, err := store.Watch("example-reader", seriesID, "The Lantern Keeper", ids, fixedNow); err != nil {
		t.Fatal(err)
	}

	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageDownloadNewChapters,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	code, message := errorOf(t, fresh)
	if code != "nothing_new" {
		t.Errorf("code = %q, want nothing_new", code)
	}
	if message != service.NothingNewToDownload {
		t.Errorf("message = %q", message)
	}
	if hasFrame(fresh, appload.MessageQueueResult) {
		t.Error("nothing new was queued as though it were something")
	}
}

// "I read it elsewhere": the badge goes, the store agrees, and the radio stays
// off. A mark-seen that fetched a chapter list would be exactly the traffic
// PLAN §12.2 is restrained about.
func TestMarkingSeenClearsTheBadgeWithoutFetching(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	h.phase(chaptersGrown())
	h.checkNow()
	if row := h.settled(); row.NewChapters != 3 {
		t.Fatalf("the check found %d new chapters, want 3", row.NewChapters)
	}
	h.forget()

	before := h.fetch.countedCalls()
	handle(t, h.svc, h.rec, appload.MessageMarkSeen,
		`{"sourceId":"example-reader","seriesId":"`+seriesPath+`"}`)

	list := h.listMsg()
	if len(list.Watched) != 1 {
		t.Fatalf("%d watched rows, want 1", len(list.Watched))
	}
	row := list.Watched[0]
	if row.NewChapters != 0 || row.Badge != "" {
		t.Errorf("row still says %d new (%q)", row.NewChapters, row.Badge)
	}
	if list.Summary.SeriesWithNew != 0 || list.Summary.Short != "" {
		t.Errorf("the summary still lights the entry point: %+v", list.Summary)
	}
	if after := h.fetch.countedCalls(); after != before {
		t.Errorf("marking seen made %d requests; it needs none", after-before)
	}

	// The row redraws from the store, so it survives what the badge is really
	// for: the next check finds nothing new either.
	h.forget()
	h.checkNow()
	if row := h.settled(); row.NewChapters != 0 {
		t.Errorf("the next check found %d new chapters, want none", row.NewChapters)
	}
}

// Both new handlers are entry points from the socket, so both refuse a payload
// that names nothing rather than acting on an empty series id.
func TestTheNewWatchActionsRefuseABadRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		msgType int32
		payload string
	}{
		{"download with no series", appload.MessageDownloadNewChapters, `{"sourceId":"example-reader"}`},
		{"download with no payload at all", appload.MessageDownloadNewChapters, ``},
		{"mark seen with no series", appload.MessageMarkSeen, `{"sourceId":"example-reader"}`},
		{"mark seen with no payload at all", appload.MessageMarkSeen, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _, _, _ := newDownloadService(t)
			addSource(t, store)

			rec := &recorder{}
			handle(t, svc, rec, tc.msgType, tc.payload)
			code, message := errorOf(t, rec)
			if code != "bad_request" {
				t.Errorf("code = %q, want bad_request", code)
			}
			if message == "" {
				t.Error("a refusal with nothing to read is not an answer")
			}
		})
	}
}

// A series that is not watched has no stored "new", and neither action may
// invent one.
func TestTheNewWatchActionsRefuseAnUnwatchedSeries(t *testing.T) {
	svc, store, _, _, _ := newDownloadService(t)
	addSource(t, store)

	for _, msgType := range []int32{appload.MessageDownloadNewChapters, appload.MessageMarkSeen} {
		rec := &recorder{}
		handle(t, svc, rec, msgType, `{"sourceId":"example-reader","seriesId":"/manga/not-watched/"}`)
		if code, _ := errorOf(t, rec); code != "not_found" {
			t.Errorf("%s: code = %q, want not_found", appload.Name(msgType), code)
		}
	}
}
