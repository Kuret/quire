// Package fixtures serves Quire's committed synthetic fixtures over a real
// HTTP socket, and simulates on demand the responses that are awkward to
// produce any other way.
//
// # Why this exists
//
// Every test up to M2 stubs the transport. themetest.Fetcher hands a theme a
// fetch.Response it constructed in memory, which is exactly right for asking
// "does this theme parse this shape" and proves nothing whatever about the
// layer underneath it. Nothing so far has exercised the parts of PLAN §7.4
// that only exist once there is a socket: connection handling, a redirect the
// Go client actually follows, robots.txt being fetched rather than injected,
// the limiter's delays against real elapsed time, Retry-After, backoff, the
// response-size cap meeting a real Content-Length, byte accounting, and the
// SSRF guard being consulted on a hop it did not choose.
//
// This package closes that gap without going online. It is a local server on
// loopback that serves the same committed fixtures (PLAN §1.3: synthetic, at
// example.invalid, never a recording) and that can be told to misbehave.
//
// # What it can simulate
//
// The interesting cases are the ones a cooperative server never produces:
//
//   - 429 with a Retry-After header, then success
//   - 5xx for a bounded number of attempts, then success
//   - a slow response, to exercise timeouts and the "don't look hung" path
//   - a body larger than the response cap, with and without a Content-Length
//   - a redirect chain that leaves the registrable domain
//   - a robots.txt that disallows a path
//
// # It is a test and development tool
//
// The binary in backend/cmd/fixtureserver is for running it by hand. Nothing
// in the shipped app imports this package, and it serves only files under a
// directory it is given.
package fixtures

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Route is one canned response.
//
// The FailTimes/FailStatus pair is what makes the retry paths testable: a
// route can refuse the first two attempts and then answer, which is the shape
// of a real rate limit and cannot be expressed by a static file.
type Route struct {
	// File is a path relative to the server's directory. It wins over Body.
	File string

	// Body is the response body when File is empty.
	Body string

	// Status is the response status. Zero means 200.
	Status int

	// Header is merged into the response headers.
	Header http.Header

	// ContentType overrides the guessed type.
	ContentType string

	// Delay is how long to wait before answering, for the slow-response case.
	Delay time.Duration

	// FailTimes is how many of the first attempts answer with FailStatus
	// instead of the real response. Counted per route, across connections.
	FailTimes int

	// FailStatus is the status those attempts get. Zero means 503.
	FailStatus int

	// RetryAfter, when set, is sent with each failing attempt. Both RFC 9110
	// forms work; the client accepts delta-seconds and an HTTP-date.
	RetryAfter string
}

// Options configures a Server.
type Options struct {
	// Dir is the directory Route.File is resolved against. A request can never
	// escape it: the path is cleaned and rejected if it still climbs out.
	Dir string

	// Routes maps "METHOD /path" — or just "/path", which matches any method —
	// to a response. The lookup tries the request URI first and then the bare
	// path, so a route only spells out a query when it discriminates on one.
	Routes map[string]Route

	// Robots is the body served at /robots.txt. Empty means an empty file,
	// which allows everything; set it to disallow a path.
	Robots string

	// RobotsStatus overrides the status of /robots.txt, for the 404-allows and
	// 5xx-is-unknown cases PLAN §7.4 distinguishes.
	RobotsStatus int
}

// Server is the running fixture server.
type Server struct {
	opts Options

	mu       sync.Mutex
	hits     map[string]int
	requests []Request

	http *httptest.Server
}

// Request is one request the server saw.
type Request struct {
	Method    string
	URI       string
	UserAgent string
	At        time.Time
}

// New builds a Server without starting it. Handler is the http.Handler; Start
// wraps it in a real listener.
func New(opts Options) *Server {
	if opts.Routes == nil {
		opts.Routes = map[string]Route{}
	}
	return &Server{opts: opts, hits: map[string]int{}}
}

// Start begins listening on loopback and returns the server. Close it with
// Close, or let testing.T do it via Cleanup in the caller.
func Start(opts Options) *Server {
	s := New(opts)
	s.http = httptest.NewServer(s)
	return s
}

// URL is the base URL of a started server.
func (s *Server) URL() string {
	if s.http == nil {
		return ""
	}
	return s.http.URL
}

// Close stops a started server.
func (s *Server) Close() {
	if s.http != nil {
		s.http.Close()
	}
}

// Hits is how many times a path was requested. It is the cheapest way for a
// test to assert that robots.txt was fetched once and cached, or that a
// refused request never reached the server at all.
func (s *Server) Hits(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// Requests returns everything the server saw, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.hits[r.URL.Path]++
	s.requests = append(s.requests, Request{
		Method:    r.Method,
		URI:       r.URL.RequestURI(),
		UserAgent: r.Header.Get("User-Agent"),
		At:        time.Now(),
	})
	s.mu.Unlock()

	if r.URL.Path == "/robots.txt" {
		s.serveRobots(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/sim/") {
		s.serveSim(w, r)
		return
	}
	s.serveRoute(w, r)
}

func (s *Server) serveRobots(w http.ResponseWriter) {
	status := s.opts.RobotsStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if status < 400 {
		fmt.Fprint(w, s.opts.Robots)
	}
}

func (s *Server) serveRoute(w http.ResponseWriter, r *http.Request) {
	route, key, ok := s.match(r)
	if !ok {
		http.Error(w, "fixtures: no route for "+r.Method+" "+r.URL.RequestURI(), http.StatusNotFound)
		return
	}

	if route.Delay > 0 {
		time.Sleep(route.Delay)
	}

	// The retry path. The count is per route and shared across connections, so
	// "fail twice then answer" means exactly that however the client retries.
	if route.FailTimes > 0 {
		s.mu.Lock()
		n := s.hits["fail:"+key]
		if n < route.FailTimes {
			s.hits["fail:"+key] = n + 1
		}
		s.mu.Unlock()
		if n < route.FailTimes {
			status := route.FailStatus
			if status == 0 {
				status = http.StatusServiceUnavailable
			}
			if route.RetryAfter != "" {
				w.Header().Set("Retry-After", route.RetryAfter)
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
	}

	body := route.Body
	if route.File != "" {
		b, err := s.readFixture(route.File)
		if err != nil {
			http.Error(w, "fixtures: "+err.Error(), http.StatusInternalServerError)
			return
		}
		body = string(b)
	}

	for k, vs := range route.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", contentType(route, r.URL.Path))
	}
	status := route.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// match finds the route for a request.
//
// Exact keys win, then partial-query keys. The partial form — "/a/b?offset=4"
// — matches when the path agrees and every parameter the key names is present
// with that value, ignoring the rest; the most specific such key wins.
//
// That middle ground is what makes a paginated endpoint testable without
// transcribing a twelve-parameter query string into a route table, where it
// would be wrong the moment the caller added a parameter and would fail as an
// unrouted request rather than as the assertion someone meant to write.
func (s *Server) match(r *http.Request) (Route, string, bool) {
	for _, k := range []string{
		r.Method + " " + r.URL.RequestURI(),
		r.Method + " " + r.URL.EscapedPath(),
		r.URL.RequestURI(),
		r.URL.EscapedPath(),
	} {
		if rt, ok := s.opts.Routes[k]; ok {
			return rt, k, true
		}
	}

	got := r.URL.Query()
	best, bestKey, bestN := Route{}, "", -1
	for key, rt := range s.opts.Routes {
		spec := key
		if m, rest, found := strings.Cut(key, " "); found {
			if !strings.EqualFold(m, r.Method) {
				continue
			}
			spec = rest
		}
		path, rawQuery, found := strings.Cut(spec, "?")
		if !found || path != r.URL.EscapedPath() {
			continue
		}
		want, err := url.ParseQuery(rawQuery)
		if err != nil {
			continue
		}
		n := 0
		ok := true
		for k, vs := range want {
			for _, v := range vs {
				if !containsValue(got[k], v) {
					ok = false
					break
				}
				n++
			}
			if !ok {
				break
			}
		}
		if ok && n > bestN {
			best, bestKey, bestN = rt, key, n
		}
	}
	return best, bestKey, bestN >= 0
}

func containsValue(have []string, want string) bool {
	for _, v := range have {
		if v == want {
			return true
		}
	}
	return false
}

// readFixture reads a file under Dir. The path is cleaned and re-checked
// against the root: this server exists to be pointed at a directory of test
// data, and a path that climbs out of it is the one interesting way it could
// be abused.
func (s *Server) readFixture(name string) ([]byte, error) {
	if s.opts.Dir == "" {
		return nil, fmt.Errorf("no fixture directory configured")
	}
	root, err := filepath.Abs(s.opts.Dir)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(root, filepath.Clean("/"+name))
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("path %q escapes the fixture directory", name)
	}
	return os.ReadFile(full)
}

func contentType(r Route, path string) string {
	if r.ContentType != "" {
		return r.ContentType
	}
	switch {
	case strings.HasSuffix(r.File, ".json"), strings.HasSuffix(path, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(r.File, ".html"), strings.HasSuffix(path, ".html"):
		return "text/html; charset=utf-8"
	default:
		return "text/html; charset=utf-8"
	}
}

// The simulation endpoints. They are under one prefix so that a fixture path
// can never collide with one, and they take their parameters from the query
// string so a test can describe the case it wants inline rather than by
// configuring the server for it.
//
//	/sim/status/{code}            answer with that status
//	/sim/slow?ms=250              answer 200 after a delay
//	/sim/large?bytes=N            a body of N bytes, Content-Length declared
//	/sim/large?bytes=N&chunked=1  the same body with no Content-Length, so the
//	                              cap has to catch it while reading
//	/sim/redirect?n=3             a chain of n hops ending at /sim/ok
//	/sim/redirect?to=URL          one hop to an arbitrary URL — used with an
//	                              off-domain target to check the guard runs on
//	                              a hop the client chose to follow
//	/sim/ok                       a small 200
func (s *Server) serveSim(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rest := strings.TrimPrefix(r.URL.Path, "/sim/")

	switch {
	case rest == "ok":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "ok")

	case strings.HasPrefix(rest, "status/"):
		code, err := strconv.Atoi(strings.TrimPrefix(rest, "status/"))
		if err != nil || code < 100 || code > 599 {
			http.Error(w, "fixtures: bad status", http.StatusBadRequest)
			return
		}
		if ra := q.Get("retryAfter"); ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		http.Error(w, http.StatusText(code), code)

	case rest == "slow":
		ms, _ := strconv.Atoi(q.Get("ms"))
		if ms <= 0 {
			ms = 100
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "slow but fine")

	case rest == "large":
		n, _ := strconv.Atoi(q.Get("bytes"))
		if n <= 0 {
			n = 1 << 20
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if q.Get("chunked") == "" {
			// Declared. The client should refuse this before transferring it.
			w.Header().Set("Content-Length", strconv.Itoa(n))
		}
		w.WriteHeader(http.StatusOK)
		// Written in blocks so a refused request does not cost a megabyte of
		// allocation on the way out.
		block := strings.Repeat("x", 4096)
		for written := 0; written < n; {
			chunk := block
			if n-written < len(chunk) {
				chunk = chunk[:n-written]
			}
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
			written += len(chunk)
		}

	case rest == "redirect":
		if to := q.Get("to"); to != "" {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 1 {
			http.Redirect(w, r, "/sim/ok", http.StatusFound)
			return
		}
		next := url.Values{"n": []string{strconv.Itoa(n - 1)}}
		http.Redirect(w, r, "/sim/redirect?"+next.Encode(), http.StatusFound)

	default:
		http.Error(w, "fixtures: unknown simulation "+rest, http.StatusNotFound)
	}
}
