// Package bookreader is one open book's session in Quire's own reader,
// wrapping backend/bookrender's mutool process the way backend/tryreader
// wraps a page fetch: one page at a time, on disk, handed to QML as a path
// rather than bytes (PLAN §7.1).
//
// # One book at a time
//
// Exactly like Try, there is one book session open at a time (see the
// service layer's bookMu/bookSession) — the reader is one screen, and a
// second session starting ends whichever was open, so its cache directory
// is never left behind uncollected. Sweep, called once at startup, is
// bookreader's answer to a session that never ended cleanly (a crash): it
// removes the whole cache directory, which is safe because nothing under it
// is the only copy of anything — a page image is regenerated from the saved
// or fetched book file at any time.
//
// # Reading position across a re-layout
//
// A page index only means the same thing across two Layout calls when
// nothing about the layout changed. Changing the font, size, margins or
// spacing re-lays the whole book out and very likely changes what is on any
// given page number — so a reading position is kept three ways (fraction
// through the book, a text snippet, and the settings hash the position was
// recorded under): reopening under the *same* settings uses the page
// directly, and reopening under different settings (or a font-size change
// mid-read) estimates the page from the fraction and searches nearby pages
// for the snippet (see findPage) rather than trusting a stale page number.
package bookreader

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rickl/quire/backend/bookrender"
	"github.com/rickl/quire/backend/state"
)

// PageWidth and PageHeight are the reflow target, in points, for the
// device's 1620x2160px panel at 229dpi (docs/DEVICE-NOTES.md, measured):
// 1620*72/229 and 2160*72/229.
const (
	PageWidth  = 509.3
	PageHeight = 679.2
)

// positionSearchRadius is how many pages either side of the fraction
// estimate findPage searches for a position's text snippet
// (books-contract.md §B, Reader sessions: "search ±6 pages").
const positionSearchRadius = 6

// snippetCompareLen is how much of a saved snippet findPage actually
// compares. The full snippet stored is up to ~200 characters (render.js's
// own cap) starting wherever the page happened to start; after a re-layout
// that exact run of text may straddle a page boundary that has since moved,
// but a shorter prefix — one that is more likely to still land inside a
// single page — usually still matches somewhere nearby.
const snippetCompareLen = 60

// Renderer is the slice of *bookrender.Renderer a Session needs. It exists
// so tests can supply a fake implementation instead of driving a real
// mutool process — bookrender.Renderer satisfies this interface as-is.
type Renderer interface {
	Open(ctx context.Context, path string) (bookrender.OpenResult, error)
	Layout(ctx context.Context, p bookrender.LayoutParams) (int, error)
	Render(ctx context.Context, page int, out string, scale float64) error
	Text(ctx context.Context, page int) (string, error)
	Outline(ctx context.Context) ([]bookrender.TocEntry, error)
	Close() error
}

// RendererFactory builds a fresh Renderer for one session.
type RendererFactory func() Renderer

// Cache is the on-disk root every book session is written under.
type Cache struct {
	dir     string
	newRend RendererFactory
}

// New builds a Cache writing under dir, sweeping it first — see the package
// comment's Sweep section.
func New(dir string, newRend RendererFactory) *Cache {
	_ = os.RemoveAll(dir)
	return &Cache{dir: dir, newRend: newRend}
}

// Dir is the cache's root directory.
func (c *Cache) Dir() string { return c.dir }

// Open starts a new session on path, laying it out at settings.
func (c *Cache) Open(ctx context.Context, path string, settings state.ReaderSettings) (*Session, error) {
	r := c.newRend()
	res, err := r.Open(ctx, path)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	s := &Session{
		cache:       c,
		id:          randomID(),
		r:           r,
		path:        path,
		fixedLayout: res.FixedLayout,
		title:       res.Title,
		settings:    settings,
	}
	if err := s.relayoutLocked(ctx); err != nil {
		_ = r.Close()
		return nil, err
	}
	return s, nil
}

// Session is one book open in the reader.
type Session struct {
	cache *Cache
	id    string
	path  string

	title       string
	fixedLayout bool

	// rmu serialises every call into r: a Renderer is documented as unsafe
	// for concurrent use, and this is the one place in the service that
	// enforces it.
	rmu sync.Mutex
	r   Renderer

	// mu guards everything else: settings and the layout's own results,
	// read by Page (for the cache key) far more often than they change.
	mu         sync.Mutex
	settings   state.ReaderSettings
	layoutHash string
	pageCount  int
	toc        []bookrender.TocEntry
}

// Title is the book's own title, from Open.
func (s *Session) Title() string { return s.title }

// FixedLayout reports whether this book paginates on its own (PDF/XPS/CBZ)
// rather than reflowing — the reader hides the settings panel's layout
// controls for one of these.
func (s *Session) FixedLayout() bool { return s.fixedLayout }

// PageCount, TOC and Settings are the session's current layout — changed
// only by SetSettings.
func (s *Session) PageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pageCount
}

func (s *Session) TOC() []bookrender.TocEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bookrender.TocEntry(nil), s.toc...)
}

func (s *Session) Settings() state.ReaderSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// relayoutLocked composes CSS for the session's current settings, lays the
// book out, and refreshes the outline (whose page numbers depend on the
// current layout too — render.js resolves them through the document's own
// link resolver at whatever layout is current). The caller must hold rmu.
func (s *Session) relayoutLocked(ctx context.Context) error {
	s.mu.Lock()
	settings := s.settings
	s.mu.Unlock()

	css, _ := bookrender.ComposeCSS(settings, nil)
	pages, err := s.r.Layout(ctx, bookrender.LayoutParams{
		W: PageWidth, H: PageHeight, Em: bookrender.EmForSize(settings.Size), CSS: css,
	})
	if err != nil {
		return err
	}
	toc, err := s.r.Outline(ctx)
	if err != nil {
		// An outline that fails to load is not fatal to the session — most
		// books have one, but "none found" and "the call broke" read the
		// same failure to a caller that only wants pages either way, and a
		// missing table of contents costs the Contents button, not the book.
		toc = nil
	}

	s.mu.Lock()
	s.pageCount = pages
	s.toc = toc
	s.layoutHash = layoutHash(settings, s.fixedLayout)
	s.mu.Unlock()
	return nil
}

// dir is this session's own cache directory, created lazily by Page.
func (s *Session) dir() string { return filepath.Join(s.cache.dir, s.id) }

func (s *Session) pathFor(hash string, index int) string {
	return filepath.Join(s.dir(), fmt.Sprintf("%s-%04d.png", hash, index))
}

// Page renders (or serves from cache) one page, keyed by the current
// layout's hash — a page cached under a previous settings hash is simply a
// different file, never confused with the current one, and is swept away by
// SetSettings dropping the whole directory... actually it is left on disk
// until Close, at deliberately low cost: a stale PNG under an old hash is
// never served again (Page always asks for the *current* hash) and Close
// removes the lot regardless of how many settings changes wrote into it.
func (s *Session) Page(ctx context.Context, index int) (string, error) {
	s.mu.Lock()
	hash := s.layoutHash
	pageCount := s.pageCount
	s.mu.Unlock()
	if index < 0 || index >= pageCount {
		return "", fmt.Errorf("bookreader: page %d is out of range (%d pages)", index, pageCount)
	}

	path := s.pathFor(hash, index)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return "", fmt.Errorf("bookreader: %w", err)
	}

	s.rmu.Lock()
	defer s.rmu.Unlock()
	// Re-check under rmu: a concurrent Page/Prefetch for the same index may
	// have finished while this call waited for the lock.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	tmp := path + ".tmp"
	if err := s.r.Render(ctx, index, tmp, 0); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("bookreader: %w", err)
	}
	return path, nil
}

// Prefetch starts rendering index in the background if it is not cached
// already, dropping any error — Page will simply try again, synchronously,
// the moment the reader actually turns there. See tryreader.Session.Prefetch
// for the same reasoning about how far ahead this is allowed to look: one
// page, never a whole book, on a device sharing its RAM with xochitl.
func (s *Session) Prefetch(ctx context.Context, index int) {
	s.mu.Lock()
	pageCount := s.pageCount
	s.mu.Unlock()
	if index < 0 || index >= pageCount {
		return
	}
	go func() {
		_, _ = s.Page(context.WithoutCancel(ctx), index)
	}()
}

// SetSettings changes the session's global reader settings, re-laying the
// book out, and re-anchors currentPage across the change: it returns the
// new page count, the page to show, and the refreshed table of contents.
func (s *Session) SetSettings(ctx context.Context, settings state.ReaderSettings, currentPage int) (
	pageCount, page int, toc []bookrender.TocEntry, err error) {

	s.mu.Lock()
	oldPageCount := s.pageCount
	fixedLayout := s.fixedLayout
	s.mu.Unlock()

	var fraction float64
	var snippet string
	if !fixedLayout && oldPageCount > 0 {
		s.rmu.Lock()
		snippet, _ = s.r.Text(ctx, clampPage(currentPage, oldPageCount))
		s.rmu.Unlock()
		fraction = float64(clampPage(currentPage, oldPageCount)) / float64(oldPageCount)
	}

	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()

	s.rmu.Lock()
	err = s.relayoutLocked(ctx)
	s.rmu.Unlock()
	if err != nil {
		return 0, 0, nil, err
	}

	s.mu.Lock()
	pageCount = s.pageCount
	toc = append([]bookrender.TocEntry(nil), s.toc...)
	s.mu.Unlock()

	if fixedLayout {
		page = clampPage(currentPage, pageCount)
	} else {
		page = s.findPage(ctx, fraction, snippet, pageCount)
	}
	return pageCount, page, toc, nil
}

// PositionFor computes what SavePosition/CloseBook remember for page: the
// fraction through the book, a text snippet (fixed-layout books skip this —
// their page numbers never move), and the layout hash it was recorded
// under.
func (s *Session) PositionFor(ctx context.Context, page int) (fraction float64, snippet string, hash string, err error) {
	s.mu.Lock()
	pageCount := s.pageCount
	hash = s.layoutHash
	fixedLayout := s.fixedLayout
	s.mu.Unlock()
	if pageCount <= 0 {
		return 0, "", hash, nil
	}
	page = clampPage(page, pageCount)
	fraction = float64(page) / float64(pageCount)
	if fixedLayout {
		return fraction, "", hash, nil
	}
	s.rmu.Lock()
	snippet, _ = s.r.Text(ctx, page)
	s.rmu.Unlock()
	return fraction, snippet, hash, nil
}

// InitialPage is where a reopened session should start: position directly
// if it was recorded under this exact layout, otherwise re-anchored from
// the fraction and snippet (see findPage).
func (s *Session) InitialPage(ctx context.Context, position int, fraction float64, snippet, recordedHash string) int {
	s.mu.Lock()
	pageCount := s.pageCount
	curHash := s.layoutHash
	fixedLayout := s.fixedLayout
	s.mu.Unlock()
	if pageCount <= 0 {
		return 0
	}
	if fixedLayout || (recordedHash != "" && recordedHash == curHash) {
		return clampPage(position, pageCount)
	}
	return s.findPage(ctx, fraction, snippet, pageCount)
}

// findPage estimates a page from fraction and, if snippet is non-empty,
// searches ±positionSearchRadius pages around the estimate for it — the
// wire contract's own words for this — falling back to the estimate when
// nothing matches (or there is nothing to search for). The caller must not
// hold rmu; findPage takes it itself, once, for the whole search.
func (s *Session) findPage(ctx context.Context, fraction float64, snippet string, pageCount int) int {
	if pageCount <= 0 {
		return 0
	}
	estimate := clampPage(int(math.Round(fraction*float64(pageCount))), pageCount)
	needle := snippetKey(snippet)
	if needle == "" {
		return estimate
	}

	s.rmu.Lock()
	defer s.rmu.Unlock()
	for d := 0; d <= positionSearchRadius; d++ {
		candidates := []int{estimate + d}
		if d > 0 {
			candidates = append(candidates, estimate-d)
		}
		for _, p := range candidates {
			if p < 0 || p >= pageCount {
				continue
			}
			text, err := s.r.Text(ctx, p)
			if err != nil {
				continue
			}
			if strings.Contains(snippetKey(text), needle) {
				return p
			}
		}
	}
	return estimate
}

// Close ends the session: the renderer process and the whole cache
// directory, in that order so a renderer still mid-write never races the
// removal of the file it is writing.
func (s *Session) Close() error {
	s.rmu.Lock()
	rErr := s.r.Close()
	s.rmu.Unlock()
	dErr := os.RemoveAll(s.dir())
	if rErr != nil {
		return rErr
	}
	return dErr
}

// clampPage keeps page inside [0, pageCount).
func clampPage(page, pageCount int) int {
	if pageCount <= 0 {
		return 0
	}
	if page < 0 {
		return 0
	}
	if page >= pageCount {
		return pageCount - 1
	}
	return page
}

// snippetKey normalises a stored or freshly-read snippet for comparison:
// lower-cased, and cut to snippetCompareLen so a run of text that would
// otherwise straddle a moved page boundary is more likely to still be found
// whole on one side of it.
func snippetKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := []rune(s)
	if len(r) > snippetCompareLen {
		r = r[:snippetCompareLen]
	}
	return string(r)
}

// layoutHash identifies a settings-plus-geometry combination, for the page
// cache's file names and for Record.PositionLayout. It only needs to be
// stable and collision-resistant, not secret.
func layoutHash(s state.ReaderSettings, fixedLayout bool) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%v|%t|%.2fx%.2f", s, fixedLayout, PageWidth, PageHeight)))
	return hex.EncodeToString(h[:8])
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "book-fallback"
	}
	return hex.EncodeToString(b[:])
}
