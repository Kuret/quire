// Package themetest provides the fixture fetcher the theme tests run against.
//
// PLAN §6 M2 requires theme behaviour to be pinned by committed fixtures and
// tested offline forever. This package is how: a theme.Fetcher that answers
// from a table of routes and files, and fails the test on any request nobody
// asked for.
//
// Read docs/THEME-NOTES.md, "What the fixtures do and do not prove", before
// trusting a green run here for more than it is worth. In short: these
// fixtures pin *our parser's* behaviour against markup we wrote to match a
// published software family's documented shape. They do not prove any live
// site is parsed correctly. What proves that is the runtime capability check
// of PLAN §7.5 stage 5, run against whatever site the user chooses to add.
package themetest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

// Request is one call the fetcher saw, so a test can assert on *which*
// endpoint a theme chose — which for madara is the whole point of the
// current/legacy split.
type Request struct {
	Method string
	URL    string
	Form   url.Values
}

// Route answers one request. Body is either inline or loaded from a file under
// the package's testdata directory.
type Route struct {
	// File is a path relative to the fetcher's testdata dir.
	File string
	// Body is used instead of File when File is empty.
	Body string
	// Status defaults to 200.
	Status int
}

// Fetcher is an offline theme.Fetcher backed by committed fixtures.
//
// Routes are keyed by "METHOD path" with the query string included when the
// route needs to distinguish on it, e.g. "GET /?post_type=wp-manga&s=lantern".
// A request with no route is a test failure, not an empty response: a theme
// quietly asking for the wrong URL and getting a blank page back is precisely
// the bug these tests exist to catch.
type Fetcher struct {
	t       *testing.T
	dir     string
	routes  map[string]Route
	mu      sync.Mutex
	calls   []Request
	missing []string
}

// New builds a Fetcher reading files from ./testdata.
func New(t *testing.T, routes map[string]Route) *Fetcher {
	t.Helper()
	f := &Fetcher{t: t, dir: "testdata", routes: routes}
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.missing) > 0 {
			sort.Strings(f.missing)
			t.Errorf("theme asked for %d unrouted URL(s):\n  %s\nknown routes:\n  %s",
				len(f.missing), strings.Join(f.missing, "\n  "), strings.Join(f.routeKeys(), "\n  "))
		}
	})
	return f
}

func (f *Fetcher) routeKeys() []string {
	keys := make([]string, 0, len(f.routes))
	for k := range f.routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Get implements theme.Fetcher.
func (f *Fetcher) Get(ctx context.Context, p *fetch.Policy, rawurl string) (*fetch.Response, error) {
	return f.answer(ctx, http.MethodGet, rawurl, nil)
}

// PostForm implements theme.Fetcher.
func (f *Fetcher) PostForm(ctx context.Context, p *fetch.Policy, rawurl string, form url.Values) (*fetch.Response, error) {
	return f.answer(ctx, http.MethodPost, rawurl, form)
}

func (f *Fetcher) answer(ctx context.Context, method, rawurl string, form url.Values) (*fetch.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.calls = append(f.calls, Request{Method: method, URL: rawurl, Form: form})
	f.mu.Unlock()

	// Try the path+query key first, then the bare path, so a route only has to
	// spell out a query string when it actually discriminates on one.
	keys := []string{method + " " + u.RequestURI()}
	if u.RawQuery != "" {
		keys = append(keys, method+" "+u.EscapedPath())
	}
	for _, key := range keys {
		route, ok := f.routes[key]
		if !ok {
			continue
		}
		body := route.Body
		if route.File != "" {
			b, err := os.ReadFile(filepath.Join(f.dir, route.File))
			if err != nil {
				f.t.Fatalf("themetest: route %q: %v", key, err)
			}
			body = string(b)
		}
		status := route.Status
		if status == 0 {
			status = http.StatusOK
		}
		return &fetch.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       []byte(body),
			FinalURL:   u,
			Attempts:   1,
		}, nil
	}

	f.mu.Lock()
	f.missing = append(f.missing, method+" "+u.RequestURI())
	f.mu.Unlock()
	return nil, fmt.Errorf("themetest: no fixture for %s %s", method, u.RequestURI())
}

// Calls returns every request the theme made, in order.
func (f *Fetcher) Calls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.calls...)
}

// Requested reports whether the theme asked for "METHOD path".
func (f *Fetcher) Requested(method, path string) bool {
	for _, c := range f.Calls() {
		u, err := url.Parse(c.URL)
		if err != nil {
			continue
		}
		if c.Method == method && (u.EscapedPath() == path || u.RequestURI() == path) {
			return true
		}
	}
	return false
}
