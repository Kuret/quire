// Package seriescache remembers the last successful fetch of a series' chapter
// list, so opening a series can answer instantly from disk while a live fetch
// runs behind it (PLAN §12.12) — and, if the tablet has no connection at all,
// can still say something true rather than nothing.
//
// One file per (source, series) rather than one big file the way
// backend/state's covers cache is: a series' chapter list can run to hundreds
// of entries, and rewriting every cached series whole every time one of them
// is opened would turn "open a series" into an ever-growing disk write. Each
// entry is its own small atomic file instead, the same shape backend/shelf and
// backend/library use for their own records — just keyed by content hash
// rather than by a segment-safe id, because a cache file's name is never shown
// to anyone and never has to round-trip back into a URL the way a saved
// chapter's directory does.
package seriescache

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rickl/quire/backend/theme"
)

const fileVersion = 1

// MaxSeries bounds the cache: past this many entries, Put evicts the least
// recently *opened* ones (Touch and Put both count as an open) to make room —
// except any entry Protect says to keep. See Store.Put.
//
// 300 is comfortably above what one person's Watching and Downloaded screens
// hold at once (PLAN §12.2, §12.6), which is the working set this actually
// needs to keep warm; it exists to bound a browsing session that opens a great
// many series once each, not to promise permanence for all of them.
const MaxSeries = 300

// Entry is one series' last successful fetch.
type Entry struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`

	Series   theme.Series    `json:"series"`
	Chapters []theme.Chapter `json:"chapters"`

	// FetchedAt is when this was fetched live from the source — what the
	// "showing the chapter list from N ago" note is measured against.
	FetchedAt time.Time `json:"fetchedAt"`

	// OpenedAt is the last time this entry answered a series-detail request,
	// whether from a fresh fetch or a cache hit. Eviction is ordered by this,
	// not by FetchedAt, so a series read daily but changing rarely is not
	// mistaken for one nobody has looked at.
	OpenedAt time.Time `json:"openedAt"`
}

type entryFile struct {
	Version int   `json:"version"`
	Entry   Entry `json:"entry"`
}

// Protect reports whether a (source, series) pair must survive eviction
// regardless of the cap — because it has saved chapters, a library record, or
// a watch, in which case forgetting the chapter list is forgetting something
// the rest of Quire still points at.
type Protect func(sourceID, seriesID string) bool

// Store is the on-disk cache. All methods are safe for concurrent use.
type Store struct {
	dir string
	mu  sync.Mutex
}

// OpenStore opens (creating if needed) the cache directory.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("seriescache: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir is where the cache's files live.
func (s *Store) Dir() string { return s.dir }

// Get returns the cached entry for a series, if there is one.
func (s *Store) Get(sourceID, seriesID string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(sourceID, seriesID)
}

// Touch records that a cached entry answered a request, without changing
// anything it holds — see Entry.OpenedAt. A series id with no cached entry is
// a no-op: there is nothing to bump.
func (s *Store) Touch(sourceID, seriesID string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.readLocked(sourceID, seriesID)
	if !ok {
		return
	}
	e.OpenedAt = now
	_ = s.writeLocked(e)
}

// Put stores a fresh entry from a successful live fetch, then evicts down to
// MaxSeries if that pushed the cache over — skipping any entry protect says to
// keep, which can leave the cache over the cap when everything in it is
// protected. That is intentional: the cap exists to bound casual browsing, not
// to override the promise that a saved chapter's series is never forgotten.
//
// protect may be nil, meaning nothing is protected.
func (s *Store) Put(e Entry, protect Protect) error {
	if e.SourceID == "" || e.SeriesID == "" {
		return fmt.Errorf("seriescache: an entry needs a source and a series")
	}
	if e.OpenedAt.IsZero() {
		e.OpenedAt = e.FetchedAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeLocked(e); err != nil {
		return err
	}
	s.evictLocked(protect)
	return nil
}

// Remove drops one series' cached entry. Removing an entry that is not there
// is not an error — the same shape backend/shelf.Store.Remove takes, for the
// same reason: a delete must be safe to run twice.
func (s *Store) Remove(sourceID, seriesID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(sourceID, seriesID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("seriescache: %w", err)
	}
	return nil
}

// RemoveSource drops every cached entry belonging to one source — used
// alongside the source's saved chapters when the source itself is removed
// (PLAN §12.12): a removed source's cached chapter list points at a theme
// that is no longer registered, and is worth exactly as little as its saved
// chapters are in that case.
func (s *Store) RemoveSource(sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, e := range s.listLocked() {
		if e.SourceID != sourceID {
			continue
		}
		if err := os.Remove(s.path(e.SourceID, e.SeriesID)); err != nil &&
			!errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// List returns every cached entry, in no particular order. Exported for tests
// and for anything that needs to reason about the cache as a whole.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store) readLocked(sourceID, seriesID string) (Entry, bool) {
	b, err := os.ReadFile(s.path(sourceID, seriesID))
	if err != nil {
		return Entry{}, false
	}
	var f entryFile
	if err := json.Unmarshal(b, &f); err != nil {
		return Entry{}, false
	}
	if f.Entry.SourceID != sourceID || f.Entry.SeriesID != seriesID {
		// A hash collision, or a file damaged badly enough to lose its own
		// keys — either way, this is not the entry that was asked for.
		return Entry{}, false
	}
	return f.Entry, true
}

// writeLocked writes one entry's file atomically — the same fsync-then-rename
// shape backend/shelf.Store.save uses, applied to a single small file rather
// than the whole store, since a cache entry here is one series rather than the
// whole table.
func (s *Store) writeLocked(e Entry) error {
	b, err := json.MarshalIndent(entryFile{Version: fileVersion, Entry: e}, "", "  ")
	if err != nil {
		return fmt.Errorf("seriescache: %w", err)
	}
	b = append(b, '\n')

	p := s.path(e.SourceID, e.SeriesID)
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("seriescache: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("seriescache: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("seriescache: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("seriescache: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("seriescache: %w", err)
	}
	return nil
}

// listLocked reads every entry file in the directory. Must be called with the
// lock held.
//
// A damaged or foreign file is skipped rather than failing the whole listing:
// this is a cache, and the eviction pass it feeds must still be able to make
// room even if one file on disk is unreadable.
func (s *Store) listLocked() []Entry {
	files, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	out := make([]Entry, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, f.Name()))
		if err != nil {
			continue
		}
		var ef entryFile
		if err := json.Unmarshal(b, &ef); err != nil {
			continue
		}
		if ef.Entry.SourceID == "" || ef.Entry.SeriesID == "" {
			continue
		}
		out = append(out, ef.Entry)
	}
	return out
}

// evictLocked trims to MaxSeries, oldest-opened first, skipping anything
// protect approves of keeping. Must be called with the lock held.
func (s *Store) evictLocked(protect Protect) {
	all := s.listLocked()
	if len(all) <= MaxSeries {
		return
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].OpenedAt.Before(all[j].OpenedAt) })
	excess := len(all) - MaxSeries
	for _, e := range all {
		if excess <= 0 {
			break
		}
		if protect != nil && protect(e.SourceID, e.SeriesID) {
			continue
		}
		if err := os.Remove(s.path(e.SourceID, e.SeriesID)); err == nil {
			excess--
		}
	}
}

// path is the file one (source, series) entry lives in.
func (s *Store) path(sourceID, seriesID string) string {
	return filepath.Join(s.dir, cacheKey(sourceID, seriesID)+".json")
}

// cacheKey hashes the pair into a filename. A hash rather than a
// safeSegment-style sanitised id: this file is never named back to a URL or
// shown to anyone the way a saved chapter's directory is (backend/shelf), so
// there is nothing to gain from keeping it readable and a fixed-length name
// sidesteps two different long-but-distinct ids colliding after truncation.
func cacheKey(sourceID, seriesID string) string {
	sum := sha256.Sum256([]byte(sourceID + "\x00" + seriesID))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
