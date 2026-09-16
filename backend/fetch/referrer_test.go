package fetch_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

// recordReferers returns a server that records the Referer of every request in
// order, so a test can assert on what was sent *and* on what was not sent on
// the next request.
func recordReferers(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Referer"))
		mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// The header is sent when, and only when, a caller names a page. PLAN §7.6
// permits a *truthful* Referer and forbids a fabricated one, so "never
// defaulted" is not a nicety — a header Quire invented would name a page it
// did not fetch, which is the lie the section exists to prevent.
func TestRefererIsSentOnlyWhenSupplied(t *testing.T) {
	t.Parallel()
	srv, seen := recordReferers(t)
	c, p := rawTestClient(t, srv, fetch.Options{})

	from, err := fetch.PageReferrer(srv.URL + "/chapter/1")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if _, err := c.GetFrom(ctx, p, srv.URL+"/img-1.png", from); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRetrievalFrom(ctx, p, srv.URL+"/img-2.png", from); err != nil {
		t.Fatal(err)
	}
	// The plain calls must send nothing. They are the same code path with a
	// zero Referrer, which is what makes "no default" structural rather than a
	// promise.
	if _, err := c.Get(ctx, p, srv.URL+"/img-3.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRetrieval(ctx, p, srv.URL+"/img-4.png"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		srv.URL + "/chapter/1",
		srv.URL + "/chapter/1",
		"",
		"",
	}
	got := seen()
	if len(got) != len(want) {
		t.Fatalf("got %d requests, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d Referer = %q, want %q", i, got[i], want[i])
		}
	}
}

// A Referrer travels with one request and must not become client state. A
// per-client Referer would end up pinned to a constant, and a constant naming
// a page we did not fetch is exactly what §7.6 forbids.
func TestRefererDoesNotLeakToLaterRequests(t *testing.T) {
	t.Parallel()
	srv, seen := recordReferers(t)
	c, p := rawTestClient(t, srv, fetch.Options{})

	from, err := fetch.PageReferrer(srv.URL + "/chapter/7")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.GetRetrievalFrom(ctx, p, srv.URL+"/a.png", from); err != nil {
		t.Fatal(err)
	}
	// Same client, same policy, same host, immediately after.
	if _, err := c.GetRetrieval(ctx, p, srv.URL+"/b.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, p, srv.URL+"/c.png"); err != nil {
		t.Fatal(err)
	}

	got := seen()
	if len(got) != 3 {
		t.Fatalf("got %d requests, want 3", len(got))
	}
	if got[0] == "" {
		t.Fatal("the first request sent no Referer; the rest of this test proves nothing")
	}
	for i, r := range got[1:] {
		if r != "" {
			t.Errorf("request %d carried a leaked Referer %q", i+1, r)
		}
	}
}

// The constructor that cannot lie: a Referrer taken from a response names the
// page this client actually fetched, redirects included.
func TestResponseReferrerNamesThePageThatWasFetched(t *testing.T) {
	t.Parallel()
	srv, seen := recordReferers(t)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old-chapter" {
			http.Redirect(w, r, "/new-chapter", http.StatusFound)
			return
		}
		fmt.Fprint(w, "<html></html>")
	}))
	defer redirector.Close()

	c, p := rawTestClient(t, redirector, fetch.Options{})
	ctx := context.Background()

	page, err := c.Get(ctx, p, redirector.URL+"/old-chapter")
	if err != nil {
		t.Fatal(err)
	}
	final := redirector.URL + "/new-chapter"
	if got := page.Referrer().String(); got != final {
		t.Fatalf("Referrer = %q, want the URL after the redirect, %q", got, final)
	}

	// And it is usable as one. The image is on the other server so that the
	// Referer crossing hosts — which is the whole point — is what is measured.
	c2, p2 := rawTestClient(t, srv, fetch.Options{})
	if _, err := c2.GetRetrievalFrom(ctx, p2, srv.URL+"/page.png", page.Referrer()); err != nil {
		t.Fatal(err)
	}
	if got := seen(); len(got) != 1 || got[0] != final {
		t.Errorf("Referer = %q, want %q", got, final)
	}
}

// A zero Referrer is the one a caller gets by not thinking about it, and it
// must be the safe answer.
func TestZeroReferrerSendsNothing(t *testing.T) {
	t.Parallel()
	var zero fetch.Referrer
	if !zero.IsZero() {
		t.Error("the zero Referrer does not report itself as zero")
	}
	if zero.String() != "" {
		t.Errorf("the zero Referrer names %q", zero.String())
	}
	// A response that never came back names nothing either.
	var nilResp *fetch.Response
	if !nilResp.Referrer().IsZero() {
		t.Error("a nil Response produced a Referrer")
	}
	if !(&fetch.Response{}).Referrer().IsZero() {
		t.Error("a Response with no FinalURL produced a Referrer")
	}
}

func TestPageReferrerRejectsWhatItCanCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, in, wantErr string
	}{
		{name: "relative", in: "/chapter/1", wantErr: "must be absolute"},
		{name: "not web", in: "ftp://example.invalid/x", wantErr: "scheme must be"},
		{name: "no host", in: "https:///chapter", wantErr: "no host"},
		{name: "empty", in: "", wantErr: "must be absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fetch.PageReferrer(tc.in)
			if err == nil {
				t.Fatalf("PageReferrer(%q) = nil error", tc.in)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// Credentials must never reach another server's access log, and a fragment is
// ours alone. PLAN §7.4 refuses a credentialled URL outright; this is the same
// rule one layer out, because a Referer is the classic way one leaks.
func TestPageReferrerStripsCredentialsAndFragment(t *testing.T) {
	t.Parallel()
	got, err := fetch.PageReferrer("https://user:hunter2@example.invalid/manga/x/chapter-3?lang=en#page-4")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.invalid/manga/x/chapter-3?lang=en"
	if got.String() != want {
		t.Fatalf("PageReferrer = %q, want %q", got.String(), want)
	}
	// The query survives on purpose: on more than one site the chapter is
	// identified by nothing else.
	if !strings.Contains(got.String(), "lang=en") {
		t.Error("the query string was dropped; it is part of the page's identity")
	}
}
