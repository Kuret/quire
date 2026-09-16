package service_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// blockingFetcher holds the first request open until the caller's context ends,
// which is what makes "did Close wait?" an observable question rather than a
// timing guess.
type blockingFetcher struct {
	*themetest.Fetcher

	once     sync.Once
	started  chan struct{}
	finished atomic.Bool
}

func newBlockingFetcher(inner *themetest.Fetcher) *blockingFetcher {
	return &blockingFetcher{Fetcher: inner, started: make(chan struct{})}
}

func (f *blockingFetcher) Get(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	f.once.Do(func() { close(f.started) })
	<-ctx.Done()
	f.finished.Store(true)
	return nil, ctx.Err()
}

func (f *blockingFetcher) GetFrom(ctx context.Context, p *fetch.Policy, rawurl string, ref fetch.Referrer) (*fetch.Response, error) {
	return f.Get(ctx, p, rawurl)
}

// Close is the property the whole of background.go exists for: when it returns,
// nothing the service started is still running.
//
// Without it, a re-probe or a watched-series check could still be writing into
// the store after the thing that owned it had gone — which showed up as a test
// failing about one run in six with "TempDir RemoveAll cleanup: directory not
// empty", and in production as writes into the user's state directory during
// shutdown.
func TestCloseWaitsForBackgroundWork(t *testing.T) {
	f := newBlockingFetcher(themetest.New(t, routes()))
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, func() time.Time { return fixedNow }))

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store:      store,
		Registry:   reg,
		Fetcher:    f,
		Covers:     covers.New(dir+"/covers", f),
		Now:        func() time.Time { return fixedNow },
		ProbeGuard: allowGuard{},
	})
	addExampleSource(t, store)

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageBrowse, `{"sourceId":"example-reader","page":1,"pageSize":10}`)

	select {
	case <-f.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the browse never reached the fetcher")
	}

	svc.Close()

	if !f.finished.Load() {
		t.Error("Close returned while background work was still running")
	}
}

// Close is called from a test's cleanup and may also be called in the body of
// the test that is checking it; the second call has to be a wait, not a panic.
func TestCloseIsIdempotent(t *testing.T) {
	svc, _, _ := newService(t, routes())
	svc.Close()
	svc.Close()
}

// After Close there is nobody to answer to: the socket is gone. Starting work
// anyway would be a goroutine with no owner, which is the bug this file is
// about — so Handle must still answer for itself rather than block or panic.
func TestHandleAfterCloseStartsNothing(t *testing.T) {
	svc, store, rec := newService(t, routes())
	addExampleSource(t, store)
	svc.Close()

	handle(t, svc, rec, appload.MessageBrowse, `{"sourceId":"example-reader","page":1,"pageSize":10}`)
}
