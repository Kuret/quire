package fetch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/fetch"
)

// The two deliberate widenings of 2026-09-20, and the fact that they are
// *narrow*: a book needs more time and more bytes than a page image, and
// nothing else does.
//
// Both were found by reading measurements rather than by a failure, which is
// why they are pinned here: the 8 MiB cap would have refused a scanned PDF at
// the end of a transfer the user waited for, and the 30-second header timeout
// would have killed shelfmark's release search at 30 seconds against an
// instance whose own budget for it is 300.

// deadlineSpy is a transport that records the deadline each request carried.
//
// http.Client.Timeout is applied as a deadline on the request context, so this
// can tell which of the two clients a call went through — which is the thing
// worth checking, and is invisible from the outside otherwise.
//
// The requests here are made one at a time, so there is no lock: a spy that
// needed one would be hiding concurrency this test does not have.
type deadlineSpy struct {
	inner   http.RoundTripper
	budgets []time.Duration
}

func (d *deadlineSpy) RoundTrip(req *http.Request) (*http.Response, error) {
	budget := time.Duration(0)
	if dl, ok := req.Context().Deadline(); ok {
		budget = time.Until(dl)
	}
	d.budgets = append(d.budgets, budget)
	return d.inner.RoundTrip(req)
}

func TestASlowCallGetsTheLongerBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"releases":[]}`))
	}))
	defer srv.Close()

	spy := &deadlineSpy{inner: http.DefaultTransport}
	c, p := testClient(t, srv, fetch.Options{Transport: spy})

	ctx := context.Background()
	if _, err := c.Get(ctx, p, srv.URL+"/fast"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetSlow(ctx, p, srv.URL+"/slow"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetFileRetrieval(ctx, p, srv.URL+"/book.epub", fetch.Referrer{}); err != nil {
		t.Fatal(err)
	}

	// The robots.txt fetch rides along on the ordinary client, so the three
	// calls above are taken from the end rather than the start.
	budgets := spy.budgets
	if len(budgets) < 3 {
		t.Fatalf("%d requests reached the transport, want at least 3", len(budgets))
	}
	slow := budgets[len(budgets)-2]
	file := budgets[len(budgets)-1]
	fast := budgets[len(budgets)-3]

	// Generous windows: what matters is which side of the instance's own 300s
	// release-search budget each call lands on, not the exact number.
	if fast <= 0 || fast > fetch.DefaultRequestTimeout {
		t.Errorf("an ordinary request carried %v, want about %v", fast, fetch.DefaultRequestTimeout)
	}
	if fast >= 300*time.Second {
		t.Errorf("an ordinary request carried %v; the short bound is the whole point of having two", fast)
	}
	for _, tc := range []struct {
		name string
		got  time.Duration
	}{{"GetSlow", slow}, {"GetFileRetrieval", file}} {
		if tc.got <= 300*time.Second {
			t.Errorf("%s carried %v, which fires while a Shelfmark instance is still "+
				"legitimately searching (its own release_search_timeout is 300s)", tc.name, tc.got)
		}
		if tc.got > fetch.SlowRequestTimeout {
			t.Errorf("%s carried %v, longer than the bound it asked for", tc.name, tc.got)
		}
	}
}

// The size cap: raised for a file retrieval, unchanged for everything else.
func TestOnlyAFileRetrievalMayExceedTheDefaultCap(t *testing.T) {
	// Comfortably over the 8 MiB default and far under the file cap.
	big := strings.Repeat("A", 9<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	ctx := context.Background()

	// A page image of this size is still refused. The default exists because
	// the device has little memory and no swap, and a book is the one thing
	// worth spending it on.
	if _, err := c.GetRetrieval(ctx, p, srv.URL+"/page.jpg"); !errors.Is(err, fetch.ErrTooLarge) {
		t.Errorf("an oversized page image returned %v, want ErrTooLarge", err)
	}
	if _, err := c.Get(ctx, p, srv.URL+"/page.html"); !errors.Is(err, fetch.ErrTooLarge) {
		t.Errorf("an oversized page returned %v, want ErrTooLarge", err)
	}

	resp, err := c.GetFileRetrieval(ctx, p, srv.URL+"/book.pdf", fetch.Referrer{})
	if err != nil {
		t.Fatalf("a %d-byte book was refused: %v", len(big), err)
	}
	if len(resp.Body) != len(big) {
		t.Errorf("got %d bytes, want %d", len(resp.Body), len(big))
	}
}

// And the file cap is a cap, not an absence of one. Past it the device would
// reset the connection mid-upload anyway (library.MaxUploadBytes), so the
// refusal belongs at the start of the download rather than the end.
func TestAFileRetrievalIsStillCapped(t *testing.T) {
	// Declared, not sent: the client refuses on Content-Length before reading a
	// byte, which is the behaviour worth having and makes the test cheap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/epub+zip")
		w.Header().Set("Content-Length", strconv.FormatInt(fetch.FileRetrievalMaxResponseBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, p := testClient(t, srv, fetch.Options{})
	_, err := c.GetFileRetrieval(context.Background(), p, srv.URL+"/huge.epub", fetch.Referrer{})
	if !errors.Is(err, fetch.ErrTooLarge) {
		t.Errorf("a book past the cap returned %v, want ErrTooLarge", err)
	}
}

// MaxResponseBytes keeps its downwards-only rule. Widening the ceiling is a
// named method, not a number in a config struct, so nothing a file can set
// raises the memory ceiling on a 2 GB device.
func TestMaxResponseBytesStillOnlyNarrows(t *testing.T) {
	body := strings.Repeat("A", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	// Asking for more than the default must not get more than the default.
	c, p := testClient(t, srv, fetch.Options{MaxResponseBytes: fetch.FileRetrievalMaxResponseBytes})
	if _, err := c.Get(context.Background(), p, srv.URL+"/small"); err != nil {
		t.Fatal(err)
	}
	// The observable half: a body over the default is still refused by an
	// ordinary request on a client that asked for a huge cap.
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("A", 9<<20)))
	}))
	defer huge.Close()
	c2, p2 := testClient(t, huge, fetch.Options{MaxResponseBytes: fetch.FileRetrievalMaxResponseBytes})
	if _, err := c2.Get(context.Background(), p2, huge.URL+"/big"); !errors.Is(err, fetch.ErrTooLarge) {
		t.Errorf("Options.MaxResponseBytes widened the cap: %v", err)
	}
}
