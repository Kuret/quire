package service_test

// PLAN §12.12: opening a series answers offline-first. These tests exercise
// the whole path through the service, with a real seriescache.Store on disk,
// rather than the unit-level checks in seriesdetail_offline_internal_test.go.

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/seriescache"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/theme/themetest"
)

// waitNth is wait's counterpart for a message that may legitimately arrive
// more than once in a test: it returns the payload of the nth occurrence
// (1-based) of msgType, or fails after the same 2s deadline wait uses.
func waitNth(t *testing.T, r *recorder, msgType int32, n int) []byte {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		count := 0
		for _, f := range r.sent {
			if f.Type == msgType {
				count++
				if count == n {
					payload := f.Payload
					r.mu.Unlock()
					return payload
				}
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("occurrence %d of %s never arrived; got %d", n, appload.Name(msgType), countFrames(r, msgType))
	return nil
}

// countFrames is how many times msgType has been sent so far. Must be called
// with r.mu held, or accepted to race against a background sender — callers
// here only use it after a wait has already settled, or inside waitNth's own
// locked failure path.
func countFrames(r *recorder, msgType int32) int {
	n := 0
	for _, f := range r.sent {
		if f.Type == msgType {
			n++
		}
	}
	return n
}

type seriesDetailFrame struct {
	Cached    bool   `json:"cached"`
	FetchedAt string `json:"fetchedAt"`
	Note      string `json:"note"`
	Series    struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"series"`
	Chapters []struct {
		ID string `json:"id"`
	} `json:"chapters"`
}

func decodeSeriesDetail(t *testing.T, b []byte) seriesDetailFrame {
	t.Helper()
	var f seriesDetailFrame
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("decoding SeriesDetailResult: %v", err)
	}
	return f
}

func withSeriesCache(cache **seriescache.Store) func(*service.Options) {
	return func(o *service.Options) {
		c, err := seriescache.OpenStore(o.DownloadDir + "-seriescache")
		if err != nil {
			panic(err)
		}
		o.SeriesCache = c
		*cache = c
	}
}

// The first request for a series has nothing cached and nothing saved, so it
// behaves exactly as it always has: one live fetch, one reply.
func TestSeriesDetailFirstOpenIsUncached(t *testing.T) {
	var cache *seriescache.Store
	h := buildDownloadHarness(t, downloadRoutes(t), withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}

	seriesID, _ := firstChapter(t, h.svc, rec)

	frame := decodeSeriesDetail(t, waitNth(t, rec, appload.MessageSeriesDetailResult, 1))
	if frame.Cached {
		t.Error("the very first fetch should not be marked cached")
	}
	if frame.Note != "" {
		t.Errorf("note = %q, want none on an uncached fetch", frame.Note)
	}
	if _, ok := cache.Get("example-reader", seriesID); !ok {
		t.Error("a successful fetch should have populated the series cache")
	}
}

// Opening a series a second time answers immediately from the cache, then
// again with the fresh result once the live fetch lands.
func TestSeriesDetailServesCacheThenFreshReply(t *testing.T) {
	var cache *seriescache.Store
	h := buildDownloadHarness(t, downloadRoutes(t), withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := firstChapter(t, h.svc, rec)

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	cachedFrame := decodeSeriesDetail(t, waitNth(t, rec2, appload.MessageSeriesDetailResult, 1))
	if !cachedFrame.Cached {
		t.Error("the immediate reply should be marked cached")
	}
	if cachedFrame.FetchedAt == "" {
		t.Error("a cached reply should carry fetchedAt")
	}
	if cachedFrame.Note == "" {
		t.Error("a cached reply should carry an explanatory note")
	}
	if len(cachedFrame.Chapters) == 0 {
		t.Error("the cached reply should still list chapters")
	}

	freshFrame := decodeSeriesDetail(t, waitNth(t, rec2, appload.MessageSeriesDetailResult, 2))
	if freshFrame.Cached {
		t.Error("the follow-up live reply should not be marked cached")
	}
	if freshFrame.Note != "" {
		t.Errorf("note = %q, want none on the fresh reply", freshFrame.Note)
	}
}

// When the live fetch fails after a cached reply was already shown, the
// series screen gets a note update, never an error banner: something true is
// already on screen and must stay there.
func TestSeriesDetailOfflineKeepsCacheAndUpdatesNoteInsteadOfErroring(t *testing.T) {
	var cache *seriescache.Store
	routes := downloadRoutes(t)
	h := buildDownloadHarness(t, routes, withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := firstChapter(t, h.svc, rec)

	// Break the series page itself: th.Series is the first network call
	// runSeriesDetail makes, so this is enough to fail the live fetch
	// wholesale without touching the cached reply's own data.
	routes["GET /manga/the-lantern-keeper/"] = themetest.Route{Err: errors.New("connection refused")}

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	first := decodeSeriesDetail(t, waitNth(t, rec2, appload.MessageSeriesDetailResult, 1))
	if !first.Cached {
		t.Fatal("expected the immediate reply to come from the cache")
	}

	second := decodeSeriesDetail(t, waitNth(t, rec2, appload.MessageSeriesDetailResult, 2))
	if !second.Cached {
		t.Error("the failure update should still say cached:true — nothing newer arrived")
	}
	if second.Note == "" {
		t.Error("the failure update should explain that the fetch didn't land")
	}
	if len(second.Chapters) == 0 {
		t.Error("the failure update should still list the cached chapters")
	}

	if hasFrame(rec2, appload.MessageError) {
		t.Error("a network failure after a cached reply was shown must not raise an error banner")
	}
	if countFrames(rec2, appload.MessageSeriesDetailResult) != 2 {
		t.Errorf("expected exactly 2 SeriesDetailResult frames, got %d",
			countFrames(rec2, appload.MessageSeriesDetailResult))
	}
}

// A network failure must not trigger PLAN §6 M7's re-probe: that machinery
// exists for a source that legitimately started returning nothing, and a
// dropped connection is not evidence of that.
func TestSeriesDetailNetworkFailureDoesNotReprobe(t *testing.T) {
	var cache *seriescache.Store
	routes := downloadRoutes(t)
	h := buildDownloadHarness(t, routes, withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := firstChapter(t, h.svc, rec)

	routes["GET /manga/the-lantern-keeper/"] = themetest.Route{Err: errors.New("connection refused")}

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	waitNth(t, rec2, appload.MessageSeriesDetailResult, 2)

	if hasFrame(rec2, appload.MessageError) {
		t.Error("a network failure must not surface a re-probe's source_changed error")
	}
}

// With no cache entry yet but a chapter already saved in Quire, the series
// screen is synthesised from what is on the tablet rather than waiting on
// the network at all.
func TestSeriesDetailSynthesizesFromSavedChaptersWhenNoCacheEntry(t *testing.T) {
	var cache *seriescache.Store
	h := buildDownloadHarness(t, downloadRoutes(t), withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	// saveOne's own series-detail round trip already populated the cache;
	// drop it, so what is left standing is exactly "saved chapters, no
	// cache" — the case this test is about.
	if err := cache.Remove("example-reader", seriesID); err != nil {
		t.Fatal(err)
	}

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	synth := decodeSeriesDetail(t, waitNth(t, rec2, appload.MessageSeriesDetailResult, 1))
	if !synth.Cached {
		t.Error("a synthesised reply is still cached:true — it did not come from a live fetch")
	}
	if synth.Note == "" {
		t.Error("a synthesised reply should explain it is only what is on the tablet")
	}
	found := false
	for _, c := range synth.Chapters {
		if c.ID == chapterID {
			found = true
		}
	}
	if !found {
		t.Errorf("the synthesised chapters do not include the saved chapter %q: %+v", chapterID, synth.Chapters)
	}

	// The network is fine in this test, so a fresh reply should follow.
	fresh := decodeSeriesDetail(t, waitNth(t, rec2, appload.MessageSeriesDetailResult, 2))
	if fresh.Cached {
		t.Error("the follow-up live reply should not be marked cached")
	}
}

// With nothing cached and nothing on the tablet, an unreachable source still
// answers with today's plain error — the offline-first path has nothing to
// fall back to, so it must not pretend otherwise.
func TestSeriesDetailErrorsAsTodayWithNothingToFallBackOn(t *testing.T) {
	var cache *seriescache.Store
	routes := downloadRoutes(t)
	h := buildDownloadHarness(t, routes, withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}

	// A search discovers the series id without ever asking for its detail —
	// so nothing is cached and nothing is saved for it yet.
	handle(t, h.svc, rec, appload.MessageSearch, `{"sourceId":"example-reader","query":"lantern"}`)
	var results struct {
		Series []struct {
			ID string `json:"id"`
		} `json:"series"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &results); err != nil {
		t.Fatal(err)
	}
	if len(results.Series) == 0 {
		t.Fatal("search found nothing")
	}
	seriesID := results.Series[0].ID

	routes["GET /manga/the-lantern-keeper/"] = themetest.Route{Err: errors.New("connection refused")}

	rec2 := &recorder{}
	handle(t, h.svc, rec2, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)

	rec2.wait(t, appload.MessageError)
	if hasFrame(rec2, appload.MessageSeriesDetailResult) {
		t.Error("expected no SeriesDetailResult when there is nothing to fall back on")
	}
}

// Deleting a series drops its cached chapter list along with its saved
// chapters — a stale offline reply for a series with nothing left on the
// tablet is worth nothing.
func TestDeletingASeriesClearsItsCache(t *testing.T) {
	var cache *seriescache.Store
	h := buildDownloadHarness(t, downloadRoutes(t), withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := saveOne(t, h, rec)

	if _, ok := cache.Get("example-reader", seriesID); !ok {
		t.Fatal("expected the series to be cached before deleting it")
	}

	handle(t, h.svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	rec.wait(t, appload.MessageDeleteSeriesConfirm)
	handle(t, h.svc, rec, appload.MessageDeleteSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","confirmed":true}`)

	if _, ok := cache.Get("example-reader", seriesID); ok {
		t.Error("deleting the series should have dropped its cached chapter list")
	}
}

// Removing a source drops every cached series belonging to it, the same as
// its saved chapters.
func TestRemovingASourceClearsItsSeriesCache(t *testing.T) {
	var cache *seriescache.Store
	h := buildDownloadHarness(t, downloadRoutes(t), withSeriesCache(&cache))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, _ := firstChapter(t, h.svc, rec)

	if _, ok := cache.Get("example-reader", seriesID); !ok {
		t.Fatal("expected the series to be cached before removing its source")
	}

	handle(t, h.svc, rec, appload.MessageRemoveSource, `{"sourceId":"example-reader"}`)

	if _, ok := cache.Get("example-reader", seriesID); ok {
		t.Error("removing the source should have dropped its cached series")
	}
}
