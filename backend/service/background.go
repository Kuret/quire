package service

import (
	"context"
	"sync"
)

// background owns every goroutine the service starts on behalf of a message.
//
// # Why this exists
//
// Most of what the service does outlives the call that triggered it: a search
// answers on its own goroutine so the AppLoad message loop stays free to
// receive, the watched-series check (PLAN §12.2) streams results in as they
// land and deliberately does not block, and an automatic re-probe (PLAN §6 M7)
// is six more stages of requests started because a listing came back empty.
//
// Started with a bare `go`, none of that work has an owner. Nothing can wait
// for it, so nothing knows when the service has actually stopped — it only
// knows when the *message loop* has. The visible symptom was a test failing
// about one run in six with "TempDir RemoveAll cleanup: directory not empty":
// a re-probe was still writing its verdict into the store after the test that
// started it had finished. The same goroutine in production writes into the
// user's real state directory, during or after shutdown, on a 2 GB device the
// OOM killer has already visited (docs/DEVICE-NOTES.md §10.4).
//
// So background work is tracked. Close cancels it and waits, which gives the
// service the one property it was missing: when Close returns, nothing of the
// service's is still running.
type background struct {
	// ctx is cancelled by Close. Every tracked goroutine's context is a child
	// of it, so cancelling once stops the lot.
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func newBackground() *background {
	ctx, cancel := context.WithCancel(context.Background())
	return &background{ctx: ctx, cancel: cancel}
}

// start runs fn on its own goroutine and reports whether it was started.
//
// The context fn receives is cancelled when *either* the caller's context ends
// or the service closes. Both halves matter and neither subsumes the other:
// the caller's context is how a request that has gone away stops the work it
// asked for, and the service's is how shutdown stops work whose caller is still
// notionally alive.
//
// After Close, start refuses and returns false. That is not a lost message —
// by then the socket is gone and there is nobody to send the answer to — but it
// is what makes waiting safe: the closed flag and the WaitGroup are set under
// the same mutex, so no goroutine can be added after Close has begun to wait.
func (b *background) start(parent context.Context, fn func(context.Context)) bool {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return false
	}
	b.wg.Add(1)
	b.mu.Unlock()

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(b.ctx, cancel)

	go func() {
		defer b.wg.Done()
		defer stop()
		defer cancel()
		fn(ctx)
	}()
	return true
}

// close cancels everything tracked and waits for it to stop.
//
// It is idempotent: the second call waits for the same goroutines as the first,
// which matters because a test's cleanup and an explicit close in the body of
// the test both want to be allowed to say it.
func (b *background) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()

	b.cancel()
	b.wg.Wait()
}

// goBackground starts tracked background work. See background.start.
func (s *Service) goBackground(ctx context.Context, fn func(context.Context)) {
	s.bg.start(ctx, fn)
}

// Close stops the service's background work and waits for it to finish.
//
// Callers get a real guarantee out of it: once Close returns, nothing the
// service started is still touching the store, the download directory or the
// connection. quired calls it on the clean-exit path, before it clears the
// session marker — a marker cleared while a re-probe was still writing would
// be claiming an orderly end the process had not actually had yet.
//
// Close does not close the store or the connection; those belong to whoever
// opened them, and by design they outlive the service just long enough for the
// caller to shut them down in its own order.
func (s *Service) Close() {
	s.bg.close()
	// Whatever Try session was open leaves no trace even on a clean shutdown,
	// not only after a crash: End is cheap, and there is no reason to leave
	// it to the next startup's sweep when this one can do it itself.
	s.tryMu.Lock()
	session := s.trySession
	s.trySession = nil
	s.tryMu.Unlock()
	if session != nil {
		if err := session.End(); err != nil {
			s.log.Warn("could not remove the Try session's cache on shutdown", "err", err)
		}
	}
}
