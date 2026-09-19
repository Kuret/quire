package state

// The cover URL each series was last listed with.
//
// # Why this is remembered at all
//
// The cover cache on disk is keyed by *URL* (see service's cover pipeline), and
// until this existed nothing wrote down which URL belonged to which series. A
// cover could therefore only be drawn on a screen that had just fetched the
// listing it came from — the browse grid. The Downloaded and Watching screens
// are built from local records, never from a listing, so they had no URL to ask
// for and their tiles were blank for ever.
//
// # Why its own file, not the sources envelope
//
// sources.json is what PLAN §7.2 lets a user export and import to move a setup
// between devices, and what the watch list and the settings ride in because
// they *are* that setup. A remembered cover URL is not: it is a cache of
// somebody else's CDN, it is re-learnt by opening the browse screen once, and a
// stale one costs a blank tile and nothing else. Putting it in the envelope
// would mean a few hundred KB of other people's URLs travelling with an export
// that is otherwise a dozen lines, and every search page rewriting the file
// that holds the user's actual sources.
//
// It is dropped on any read error rather than refusing to open. A cache that
// can stop the app from starting is a liability, not a cache.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"
)

// CoverFileName is the cache's file inside the data directory.
const CoverFileName = "covers.json"

const coverFileVersion = 1

// maxCoverURLs bounds the cache.
//
// The file is rewritten whole on every listing (see RememberCovers), so the cap
// is set by what is acceptable to rewrite rather than by what is useful to
// keep: at roughly 120 bytes an entry — a series id, a CDN URL and a timestamp,
// measured against real MangaDex rows — a thousand entries is a ~120 KB write,
// once per page of results. What the screens that need it actually hold is the
// downloaded and watched sets, which are tens of series, so this is two orders
// of magnitude of headroom over the working set.
//
// The oldest entries go first rather than the whole map being dropped: unlike
// the in-memory referrer memo, forgetting here is visible as a blank tile on a
// screen the user did not visit a listing from, and the oldest entry is the one
// least likely to be on it.
const maxCoverURLs = 1024

// CoverRef is one series' cover URL, as a listing just gave it.
type CoverRef struct {
	SeriesID string
	URL      string
}

// coverEntry is one remembered URL. SeenAt is what eviction sorts on.
type coverEntry struct {
	SourceID string    `json:"sourceId"`
	SeriesID string    `json:"seriesId"`
	URL      string    `json:"url"`
	SeenAt   time.Time `json:"seenAt"`
}

type coverFile struct {
	Version int          `json:"version"`
	Covers  []coverEntry `json:"covers"`
}

// CoverURL is the URL the series was last listed with, or "" if Quire has never
// seen one.
//
// "" is the whole of the unknown case: a series downloaded before this cache
// existed, or one whose entry has been evicted, is not an error. The caller
// draws no tile and the row is otherwise complete.
func (s *Store) CoverURL(sourceID, seriesID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.coverURLs[coverKey(sourceID, seriesID)].URL
}

// RememberCovers records the cover URL of every series in one listing.
//
// A batch rather than one call per row on purpose: a page of results is twenty
// rows, and twenty atomic rewrites of the same file — each an fsync on a
// battery-powered device's eMMC — to store twenty short strings is the kind of
// cost that turns a page turn into a stutter. One listing is one write.
//
// It writes nothing when the listing tells it nothing new, which is the common
// case: paging back and forth through results the user has already seen is free.
func (s *Store) RememberCovers(sourceID string, refs []CoverRef) error {
	if sourceID == "" || len(refs) == 0 {
		return nil
	}
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.coverURLs == nil {
		s.coverURLs = map[string]coverEntry{}
	}
	changed := false
	for _, ref := range refs {
		if ref.SeriesID == "" || ref.URL == "" {
			continue
		}
		k := coverKey(sourceID, ref.SeriesID)
		if prev, ok := s.coverURLs[k]; ok && prev.URL == ref.URL {
			// The timestamp is deliberately *not* refreshed for an unchanged
			// URL. Doing so would make every page turn a write, which is the
			// cost this batching exists to avoid, and eviction by "when we first
			// learnt it" is close enough: the cap is a thousand.
			continue
		}
		s.coverURLs[k] = coverEntry{SourceID: sourceID, SeriesID: ref.SeriesID, URL: ref.URL, SeenAt: now}
		changed = true
	}
	if !changed {
		return nil
	}
	s.evictCovers()
	return s.saveCovers()
}

// dropCoversFor forgets every cover URL belonging to one source. Must be called
// with the lock held.
//
// Removing a source should mean the app has forgotten it, and a cover URL is
// the last thing left pointing at a site the user deleted. The cap would have
// evicted these eventually, which is exactly the argument for doing it here
// instead: "eventually, once a thousand other series have been browsed" is not
// the same promise as "gone".
//
// It reports whether anything went, so the caller can skip an fsync in the
// ordinary case of removing a source that was never browsed.
func (s *Store) dropCoversFor(sourceID string) bool {
	dropped := false
	for k, e := range s.coverURLs {
		if e.SourceID == sourceID {
			delete(s.coverURLs, k)
			dropped = true
		}
	}
	return dropped
}

// evictCovers trims to the cap, oldest first. Must be called with the lock held.
func (s *Store) evictCovers() {
	if len(s.coverURLs) <= maxCoverURLs {
		return
	}
	all := make([]coverEntry, 0, len(s.coverURLs))
	for _, e := range s.coverURLs {
		all = append(all, e)
	}
	sortCovers(all)
	// Oldest first, and the key as the tie-break, so an eviction round is the
	// same round twice: a cache that forgets a different entry each run is one
	// nobody can reason about from the file.
	sort.SliceStable(all, func(i, j int) bool { return all[i].SeenAt.Before(all[j].SeenAt) })
	for _, e := range all[:len(all)-maxCoverURLs] {
		delete(s.coverURLs, coverKey(e.SourceID, e.SeriesID))
	}
}

// loadCovers reads the cache. It never fails the open: see the file comment.
func (s *Store) loadCovers(log *slog.Logger) {
	s.coverURLs = map[string]coverEntry{}
	b, err := os.ReadFile(s.coverPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Warn("could not read the cover URL cache", "path", s.coverPath, "err", err)
		return
	}
	var f coverFile
	if err := json.Unmarshal(b, &f); err != nil {
		log.Warn("the cover URL cache is not readable JSON, starting empty",
			"path", s.coverPath, "err", err)
		return
	}
	for _, e := range f.Covers {
		if e.SourceID == "" || e.SeriesID == "" || e.URL == "" {
			continue
		}
		s.coverURLs[coverKey(e.SourceID, e.SeriesID)] = e
	}
}

// saveCovers writes the cache atomically. Must be called with the lock held.
//
// Atomic for the same reason state.save is, even though this is only a cache: a
// truncated file left by a power cut would be unreadable JSON, and the recovery
// from that is losing the whole cache rather than the tail of it.
func (s *Store) saveCovers() error {
	all := make([]coverEntry, 0, len(s.coverURLs))
	for _, e := range s.coverURLs {
		all = append(all, e)
	}
	sortCovers(all)

	b, err := json.MarshalIndent(coverFile{Version: coverFileVersion, Covers: all}, "", "  ")
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	b = append(b, '\n')

	tmp := s.coverPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("state: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("state: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("state: %w", err)
	}
	if err := os.Rename(tmp, s.coverPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("state: %w", err)
	}
	return nil
}

// sortCovers puts the file in a stable order, so two runs that learnt the same
// URLs write the same bytes and a diff of the file says something.
func sortCovers(all []coverEntry) {
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].SourceID != all[j].SourceID {
			return all[i].SourceID < all[j].SourceID
		}
		return all[i].SeriesID < all[j].SeriesID
	})
}

// coverKey is per-source: two sources can carry the same series id, and one's
// cover is not the other's picture.
func coverKey(sourceID, seriesID string) string {
	return sourceID + "\x00" + seriesID
}
