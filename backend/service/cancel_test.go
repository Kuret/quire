package service_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// stallingFetcher holds page retrievals open until it is released.
//
// It exists because PLAN §7.1 singles out one case: cancel has to be prompt
// during a *slow fetch*. That is exactly when the user reaches for Stop, and
// exactly when checking a flag between pages would leave them waiting. A test
// that only cancels between pages would pass against the implementation the
// plan forbids.
type stallingFetcher struct {
	inner theme.Fetcher

	// stallAfter is how many page images are served normally before the rest
	// hang. It lets a test prove that pages already on disk are *kept* as well
	// as that an in-flight fetch is interruptible.
	stallAfter int

	release chan struct{}

	mu      sync.Mutex
	served  int
	stalled int
}

func newStallingFetcher(inner theme.Fetcher, stallAfter int) *stallingFetcher {
	return &stallingFetcher{inner: inner, stallAfter: stallAfter, release: make(chan struct{})}
}

func (f *stallingFetcher) Get(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	return f.inner.Get(ctx, p, rawurl)
}

func (f *stallingFetcher) GetFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error) {
	return f.inner.GetFrom(ctx, p, rawurl, from)
}

func (f *stallingFetcher) PostForm(ctx context.Context, p *fetch.Policy, rawurl string, form url.Values) (*fetch.Response, error) {
	return f.inner.PostForm(ctx, p, rawurl, form)
}

func (f *stallingFetcher) GetRetrieval(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	return f.GetRetrievalFrom(ctx, p, rawurl, fetch.Referrer{})
}

// GetRetrievalFrom is the page-image path, and the only one that stalls: the
// chapter list has to come back or there is no download to cancel.
func (f *stallingFetcher) GetRetrievalFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error) {
	f.mu.Lock()
	f.served++
	pass := f.served <= f.stallAfter
	if !pass {
		f.stalled++
	}
	f.mu.Unlock()

	if pass {
		return f.inner.GetRetrievalFrom(ctx, p, rawurl, from)
	}
	select {
	case <-f.release:
		return f.inner.GetRetrievalFrom(ctx, p, rawurl, from)
	case <-ctx.Done():
		// The whole point: a cancelled context unblocks a fetch already in
		// flight, rather than the cancel waiting for the fetch.
		return nil, ctx.Err()
	}
}

func (f *stallingFetcher) stallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stalled
}

// waitForStall blocks until at least one page fetch is in flight and stuck.
func (f *stallingFetcher) waitForStall(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if f.stallCount() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no page fetch ever started")
}

// stallingService is newDownloadServiceWith plus a fetcher that hangs on page
// images, and a download directory the test can inspect.
func stallingService(t *testing.T, stallAfter int) (*service.Service, *stallingFetcher, string,
	*fakeLibrary, *library.Store, *state.Store, *recorder) {
	t.Helper()

	var stall *stallingFetcher
	dir := t.TempDir()
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, downloadRoutes(t),
		func(o *service.Options) {
			stall = newStallingFetcher(o.Fetcher, stallAfter)
			o.Fetcher = stall
			o.DownloadDir = dir
		})
	t.Cleanup(func() { close(stall.release) })
	return svc, stall, dir, fake, libStore, store, rec
}

func TestCancelStopsADownloadMidFetch(t *testing.T) {
	svc, stall, _, fake, libStore, store, rec := stallingService(t, 0)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	body := `{"sourceId":"example-reader","seriesId":"` + seriesID + `","volumeId":"` + chapterID + `"`
	handle(t, svc, rec, appload.MessageEnqueueDownload, body+`,"confirmed":true}`)

	stall.waitForStall(t)
	handle(t, svc, rec, appload.MessageCancelDownload, body+`}`)

	stop := waitForPhase(t, rec, "cancelled")
	if message, _ := stop["message"].(string); !strings.Contains(message, "Stopped") {
		t.Errorf("message %q", message)
	}

	fake.mu.Lock()
	uploads := len(fake.uploaded)
	fake.mu.Unlock()
	if uploads != 0 {
		t.Errorf("%d uploads from a cancelled download", uploads)
	}
	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records stored by a cancelled download", n)
	}
}

// Cancel means stop, not discard. Resume skips pages that already exist, so
// throwing them away would make cancelling at page 300 of 325 cost 300
// refetches.
func TestCancelKeepsFetchedPagesAndLeavesNoPDF(t *testing.T) {
	// The first three pages land; everything after that hangs.
	svc, stall, dir, _, _, store, rec := stallingService(t, 3)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	body := `{"sourceId":"example-reader","seriesId":"` + seriesID + `","volumeId":"` + chapterID + `"`
	handle(t, svc, rec, appload.MessageEnqueueDownload, body+`,"confirmed":true}`)

	stall.waitForStall(t)
	handle(t, svc, rec, appload.MessageCancelDownload, body+`}`)
	waitForPhase(t, rec, "cancelled")

	var pages, pdfs int
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg":
			pages++
		case ".pdf":
			pdfs++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pages == 0 {
		t.Error("the pages already fetched were discarded; a restart would refetch them")
	}
	// assemble builds into a temp file under .partial and renames only when the
	// file is whole, so a cancelled run must leave nothing that looks finished.
	if pdfs != 0 {
		t.Errorf("%d PDFs left behind by a cancelled download", pdfs)
	}
}

// Cancelling something that is not running is not an error: the user may tap
// Stop as the last page lands, and a dialogue for that is noise.
func TestCancellingNothingIsQuiet(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageCancelDownload,
		`{"sourceId":"example-reader","seriesId":"s","volumeId":"v"}`)
	waitForPhase(t, rec, "cancelled")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, f := range rec.sent {
		if f.Type == appload.MessageError {
			t.Fatalf("cancelling an idle download raised an error: %s", f.Payload)
		}
	}
}

// What cancel does about a split volume: parts already uploaded stay.
//
// Each is a complete, correctly indexed document with its own thumbnail, and
// xochitl's web interface has no delete route to take one back with — the whole
// surface is /documents/, /download/ and /upload (M5). Dropping the record
// while leaving the document on the tablet would be strictly worse: an orphan
// in the user's library that Quire cannot account for. So uploaded parts and
// their records stay, and only the unstarted remainder is dropped.
func TestCancelKeepsPartsAlreadyUploaded(t *testing.T) {
	var stall *stallingFetcher
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, downloadRoutes(t),
		func(o *service.Options) {
			// Small enough to split the fixture volume into several parts.
			o.UploadBudgetBytes = 4096
			stall = newStallingFetcher(o.Fetcher, 1000) // never stall the fetch
			o.Fetcher = stall
		})
	t.Cleanup(func() { close(stall.release) })
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	body := `{"sourceId":"example-reader","seriesId":"` + seriesID + `","volumeId":"` + chapterID + `"`

	// Stop as soon as the first part is in the library. The upload of a part is
	// not itself interruptible — it is a single HTTP request — so the cancel
	// lands between parts, which is the seam that matters.
	fake.onUpload = func() {
		handle(t, svc, rec, appload.MessageCancelDownload, body+`}`)
	}
	handle(t, svc, rec, appload.MessageEnqueueDownload, body+`,"confirmed":true}`)

	stop := waitForPhase(t, rec, "cancelled")
	message, _ := stop["message"].(string)
	if !strings.Contains(message, "already saved") {
		t.Errorf("message %q does not tell the user the finished part was kept", message)
	}

	fake.mu.Lock()
	uploaded := len(fake.uploaded)
	fake.mu.Unlock()

	if uploaded == 0 {
		t.Fatal("nothing was uploaded, so there is nothing to keep")
	}
	// The remainder must not have been uploaded: cancel has to actually stop.
	if uploaded >= 4 {
		t.Errorf("%d parts uploaded; the cancel did not stop the remainder", uploaded)
	}
	// And every part that did land is remembered, or it becomes an orphan the
	// user cannot open from Quire and cannot delete from the web interface.
	if n := len(libStore.List()); n != uploaded {
		t.Errorf("%d records for %d uploaded documents", n, uploaded)
	}
}

// PLAN §6 M7: downloads only while foregrounded. A volume is 7-10 minutes of
// pegged CPU and a busy radio; carrying on after the user has closed Quire
// drains their battery for work nobody is waiting for, and is the work most
// likely to get the backend OOM-killed with nobody there to see why.
func TestDetachingPausesADownload(t *testing.T) {
	svc, stall, dir, fake, _, store, rec := stallingService(t, 3)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	stall.waitForStall(t)

	svc.FrontendDetached(nil)
	waitForPhase(t, rec, "cancelled")

	fake.mu.Lock()
	uploads := len(fake.uploaded)
	fake.mu.Unlock()
	if uploads != 0 {
		t.Errorf("%d uploads continued after the frontend went away", uploads)
	}

	// Pausing is not discarding: the pages already fetched stay, so reopening
	// Quire resumes rather than starting over.
	pages := 0
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".jpg") {
			pages++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if pages == 0 {
		t.Error("pausing threw the fetched pages away")
	}
}

// A queued download must not start once the frontend is gone either: starting
// new background work at that moment is exactly what the plan forbids.
func TestDetachingDrainsTheQueue(t *testing.T) {
	svc, stall, _, fake, _, store, rec := stallingService(t, 3)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	stall.waitForStall(t)

	queued := &recorder{}
	handle(t, svc, queued, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"queued-one","confirmed":true}`)

	svc.FrontendDetached(nil)
	waitForPhase(t, rec, "cancelled")
	waitForPhase(t, queued, "cancelled")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.uploaded) != 0 {
		t.Errorf("%d uploads after detach", len(fake.uploaded))
	}
}
