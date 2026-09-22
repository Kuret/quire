// Package tryreader is the on-disk page cache behind milestone 1's Try
// reader: read a chapter in Quire's own reader without downloading it and
// without leaving anything in the reMarkable library.
//
// It follows backend/covers exactly, for the same reason: the device's Qt
// decodes only GIF, ICO, JPEG and SVG, several sources serve WebP, and images
// are never sent over the AppLoad socket (PLAN §7.1). So a page is fetched
// through the source's own guarded policy, decoded and downscaled in Go with
// backend/imageproc, written to disk as a JPEG under Quire's own directory —
// not /tmp, which is a 981 MB tmpfs, i.e. RAM — and QML is handed the path.
//
// # Ephemeral by construction
//
// Every session gets its own directory, named for the session rather than
// for the chapter, so two Try sessions (or a Try and a stale one from a crash)
// never collide. Session.End removes that directory outright: the whole
// point of Try is that leaving the reader discards it, and there is no
// reading position or anything else worth keeping around after that.
//
// Sweep — called once, at startup — answers the other half: "the app died
// mid-session, so nothing ended cleanly." It removes the *entire* cache
// directory. That is safe because nothing under it is ever the only copy of
// anything: it holds transcoded pages of chapters whose real, authoritative
// copies are still on the source's own server. Unlike backend/covers, which
// is worth keeping warm across a restart, a Try session is not a cache in
// that sense at all, so it gets no such courtesy.
//
// # Memory
//
// A page is fetched, decoded, downscaled and written to disk one at a time;
// nothing here holds a decoded image once Page returns. A caller may ask for
// one page ahead of what is on screen with Prefetch, so that turning the page
// does not wait on the network — never more than one, which is what keeps a
// large decoded page from ever being resident twice at once (see Prefetch).
package tryreader

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/imageproc"
	"github.com/rickl/quire/backend/theme"
)

// Page size and quality. PageWidth and PageHeight are the same panel grid the
// download pipeline normalises pages to (imageproc.PanelWidth/PanelHeight) —
// there is only one screen to show a page on. The quality sits below the
// archived download's (85): a Try page is looked at once and then discarded,
// never kept, so there is nothing to gain from spending the extra bytes an
// archival copy is worth.
const (
	PageWidth   = imageproc.PanelWidth
	PageHeight  = imageproc.PanelHeight
	PageQuality = 78
)

// MaxSourcePageBytes is how much of a source page image Try will read. A page
// that wants more than this is not a page a 1.8 GHz device should be decoding
// for a reader nobody has committed to keeping.
const MaxSourcePageBytes = 8 << 20

// Cache is the on-disk root every Try session is written under.
type Cache struct {
	dir string
	f   theme.Fetcher
}

// New builds a Cache writing under dir, and sweeps it first.
//
// The sweep is Try's answer to "the previous session did not end cleanly"
// (PLAN's crash-cleanup requirement): New runs once, at process start, which
// is the one moment "nothing is using this directory yet" is actually true.
// Removing it outright is safe for the reason the package comment gives: nothing
// under here is the only copy of anything.
func New(dir string, f theme.Fetcher) *Cache {
	_ = os.RemoveAll(dir)
	return &Cache{dir: dir, f: f}
}

// Dir is the cache's root directory.
func (c *Cache) Dir() string { return c.dir }

// NewSession opens a Try session over one chapter's page URLs, fetched
// through th/src's own policy with referer attached to every page fetch —
// the same truthful, theme-supplied Referer the download queue uses
// (theme.PageRefererFor).
func (c *Cache) NewSession(th theme.Theme, src *theme.Source, pageURLs []string, referer fetch.Referrer) *Session {
	return &Session{
		cache:    c,
		id:       randomID(),
		th:       th,
		src:      src,
		pageURLs: append([]string(nil), pageURLs...),
		referer:  referer,
		inflight: map[int]chan struct{}{},
	}
}

// Session is one chapter open in the Try reader.
type Session struct {
	cache *Cache
	id    string

	th      theme.Theme
	src     *theme.Source
	referer fetch.Referrer

	// pageURLs grows over the session's life for a theme that streams its
	// list in behind a fast first page (see Extend and
	// theme.FirstPageProber), so it is guarded by mu along with inflight.
	pageURLs []string

	// inflight collapses concurrent requests for the same page — a prefetch
	// racing the reader turning there itself — exactly as covers.Cache does
	// for the same reason: without it, one page would be fetched twice.
	mu       sync.Mutex
	inflight map[int]chan struct{}
}

// PageCount is how many pages this chapter is known to have so far. It can
// grow: see Extend.
func (s *Session) PageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pageURLs)
}

// urlAt returns the page URL at index, and whether it exists.
func (s *Session) urlAt(index int) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.pageURLs) {
		return "", false
	}
	return s.pageURLs[index], true
}

// Extend widens the session's known page list to full, once a theme's
// complete Pages() resolves after FirstPage() started the session with only
// a prefix of it (theme.FirstPageProber). It reports whether it did.
//
// It is a no-op — and leaves the session exactly as it was — unless full is
// at least as long as what is already known and agrees with it on every page
// already in hand. FirstPageProber's contract is that FirstPage returns a
// genuine prefix of Pages()'s result, so a mismatch means the theme broke
// that contract; the session then keeps showing what it already fetched
// rather than trusting a list that disagrees with itself.
func (s *Session) Extend(full []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(full) < len(s.pageURLs) {
		return false
	}
	for i := range s.pageURLs {
		if full[i] != s.pageURLs[i] {
			return false
		}
	}
	s.pageURLs = append([]string(nil), full...)
	return true
}

// dir is this session's own directory, created lazily by Page.
func (s *Session) dir() string {
	return filepath.Join(s.cache.dir, s.id)
}

func (s *Session) pathFor(index int) string {
	return filepath.Join(s.dir(), fmt.Sprintf("%04d.jpg", index))
}

// Page fetches, decodes and downscales page index if it is not already
// cached, and returns its local path.
func (s *Session) Page(ctx context.Context, index int) (string, error) {
	pageURL, ok := s.urlAt(index)
	if !ok {
		return "", fmt.Errorf("tryreader: page %d is out of range (%d pages known)", index, s.PageCount())
	}
	path := s.pathFor(index)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	s.mu.Lock()
	wait, busy := s.inflight[index]
	if busy {
		s.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("tryreader: page %d could not be fetched", index)
	}
	done := make(chan struct{})
	s.inflight[index] = done
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.inflight, index)
		s.mu.Unlock()
		close(done)
	}()

	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return "", fmt.Errorf("tryreader: %w", err)
	}

	pol, err := theme.PolicyFor(s.th, s.src)
	if err != nil {
		return "", fmt.Errorf("tryreader: %w", err)
	}
	// GetRetrievalFrom, not Get: a page the user is reading right now is a
	// retrieval, one named thing they asked for (see theme.Fetcher and the
	// download queue's sourceFetcher.Get, which reaches for the same call).
	resp, err := s.cache.f.GetRetrievalFrom(ctx, pol, pageURL, s.referer)
	if err != nil {
		return "", fmt.Errorf("tryreader: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("tryreader: %s answered %d", pageURL, resp.StatusCode)
	}
	if len(resp.Body) > MaxSourcePageBytes {
		return "", fmt.Errorf("tryreader: %s is %d bytes, more than a page should be",
			pageURL, len(resp.Body))
	}

	var buf bytes.Buffer
	opts := imageproc.DefaultOptions()
	opts.MaxWidth, opts.MaxHeight = PageWidth, PageHeight
	opts.Quality = PageQuality
	if _, err := imageproc.Normalise(&buf, bytes.NewReader(resp.Body), opts); err != nil {
		return "", fmt.Errorf("tryreader: %w", err)
	}

	// Temp file then rename, exactly as covers.Cache writes a thumbnail: a
	// reader that raced in while this was half-written must never find a
	// truncated JPEG.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("tryreader: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("tryreader: %w", err)
	}
	return path, nil
}

// Prefetch starts fetching index in the background if it is not cached
// already, and drops any error: a failed prefetch costs nothing but itself,
// because Page will simply try again — synchronously, in front of the reader
// — the moment the page turn actually lands there.
//
// A caller should only ever ask for the *one* page beyond what is on screen.
// That is the whole memory rule this package keeps: it bounds how many pages
// are being decoded at once to at most two (the one showing, the one behind
// it), on a device whose RAM is shared with xochitl. Asking further ahead
// would not corrupt anything, but it would spend the device's memory and
// battery on pages the reader may never turn to — Try's whole premise is
// that it is disposable, and prefetching a whole chapter is the opposite of
// that.
func (s *Session) Prefetch(ctx context.Context, index int) {
	if _, ok := s.urlAt(index); !ok {
		return
	}
	path := s.pathFor(index)
	if _, err := os.Stat(path); err == nil {
		return
	}
	// Detached from the caller's context on purpose: the request that
	// triggered this prefetch (serving the current page) is free to return
	// before the prefetch finishes, and a page turn a moment later should
	// find work already under way rather than a fetch that was cancelled the
	// instant the handler that started it returned.
	go func() {
		_, _ = s.Page(context.WithoutCancel(ctx), index)
	}()
}

// End discards this session's cache directory. It is what makes leaving the
// Try reader leave no trace: nothing about a session survives it, and there
// is nothing else — no reading position, no record, no library entry — for
// it to also have to undo.
func (s *Session) End() error {
	return os.RemoveAll(s.dir())
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is not a survivable condition on any platform
		// this ships on; a fixed fallback at least keeps sessions from
		// colliding within one process, which is the property callers rely
		// on it for.
		return "try-fallback"
	}
	return hex.EncodeToString(b[:])
}
