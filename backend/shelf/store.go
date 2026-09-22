// Package shelf is the index of chapters "Saved in Quire": read in Quire's
// own reader, never uploaded to the reMarkable library, never routed through
// xochitl's Trash.
//
// It is modelled on backend/library.Store — a JSON file in the state
// directory, written atomically, guarded by one mutex — because the shape of
// the problem is the same one: a small table of records that must survive a
// crash mid-write and must never be read half-updated. What differs is the
// key and the payload: a saved chapter is keyed by (source, series, chapter)
// rather than by (source, series, volume), because there is no PDF grouping
// chapters together here — every chapter saved in Quire is its own set of
// page files, on disk exactly where backend/download.Queue put them, in the
// saved root rather than the download cache.
package shelf

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// StoreFileName is where the store lives, beside library.json.
const StoreFileName = "saved.json"

const storeVersion = 1

// Key identifies one saved chapter.
type Key struct {
	Source  string `json:"source"`
	Series  string `json:"series"`
	Chapter string `json:"chapter"`
}

func (k Key) valid() bool {
	return k.Source != "" && k.Series != "" && k.Chapter != ""
}

// Record is one chapter saved in Quire's own storage.
type Record struct {
	Key

	// SeriesTitle and ChapterTitle are the source's own titles, for the UI:
	// the key holds ids, which are a source's path or slug and not something
	// to show anyone.
	SeriesTitle  string `json:"seriesTitle,omitempty"`
	ChapterTitle string `json:"chapterTitle,omitempty"`

	// Number is the chapter's number, when the source gave one, for sorting
	// and for display. It is a float rather than the printed string a page
	// number might be padded into, because nothing here formats a sentence —
	// PLAN §2 does that in the service layer.
	Number float64 `json:"number,omitempty"`

	// Pages are the chapter's page files, in reading order (the order
	// backend/download.Queue returned them — strip-split pages included) and
	// as paths *relative to the saved root*. Relative, and not absolute,
	// because the root itself is a runtime detail (backend/cmd/quired/main.go
	// decides where the data directory lives) and a record written on one
	// data directory must still resolve after that directory moves — the same
	// reason library.Record does not carry an absolute PDF path either... but
	// where library.Record does, actually see PDF's own comment; this is one
	// step more careful because the saved reader resolves every page, every
	// time it opens, and a record that silently pointed outside the current
	// root would be exactly the escape reclaim.go's own rule exists to forbid.
	Pages []string `json:"pages,omitempty"`

	// Bytes is the total size of the chapter's page files, for the same
	// reporting library.Record.Bytes exists for.
	Bytes int64 `json:"bytes,omitempty"`

	SavedAt time.Time `json:"savedAt"`

	// Position is the 0-based index of the page last shown, so the reader
	// reopens where the user left off. Try never stores this — there is no
	// Try record at all — this field exists only because a saved chapter is
	// the one kind of read Quire remembers a place in.
	Position int `json:"position,omitempty"`
}

type storeFile struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

// Store remembers every chapter saved in Quire's own storage.
type Store struct {
	path string

	mu   sync.RWMutex
	recs []Record
}

// OpenStore loads the store from dir, creating the directory if needed. A
// missing file is an empty store, not an error.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("shelf: %w", err)
	}
	s := &Store{path: filepath.Join(dir, StoreFileName)}

	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shelf: %w", err)
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("shelf: %s is not readable: %w", s.path, err)
	}
	s.recs = f.Records
	s.sort()
	return s, nil
}

// Path is the file the store is kept in.
func (s *Store) Path() string { return s.path }

// Get returns the record for a key.
func (s *Store) Get(k Key) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.recs {
		if r.Key == k {
			return r, true
		}
	}
	return Record{}, false
}

// List returns every record, newest first.
func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Record(nil), s.recs...)
}

// ForSeries returns every record for one (source, series) pair, in no
// particular order beyond List's own.
func (s *Store) ForSeries(source, series string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Record
	for _, r := range s.recs {
		if r.Source == source && r.Series == series {
			out = append(out, r)
		}
	}
	return out
}

// Put stores a record, replacing any earlier one for the same key, and writes
// the file.
func (s *Store) Put(r Record) error {
	if !r.Key.valid() {
		return fmt.Errorf("shelf: a saved chapter needs a source, a series and a chapter")
	}
	if r.SavedAt.IsZero() {
		r.SavedAt = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i := range s.recs {
		if s.recs[i].Key == r.Key {
			s.recs[i] = r
			replaced = true
			break
		}
	}
	if !replaced {
		s.recs = append(s.recs, r)
	}
	s.sort()
	return s.save()
}

// Remove drops a record. A key that is not there is not an error: a delete
// must be safe to run twice.
func (s *Store) Remove(k Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.recs[:0]
	for _, r := range s.recs {
		if r.Key != k {
			out = append(out, r)
		}
	}
	if len(out) == len(s.recs) {
		return nil
	}
	s.recs = out
	return s.save()
}

func (s *Store) sort() {
	sort.SliceStable(s.recs, func(i, j int) bool {
		return s.recs[i].SavedAt.After(s.recs[j].SavedAt)
	})
}

// save writes the whole file atomically, for the same reason library.Store
// does: a device that loses power mid-write must not come back with a
// truncated file, and there is no cloud copy of any of this.
func (s *Store) save() error {
	b, err := json.MarshalIndent(storeFile{Version: storeVersion, Records: s.recs}, "", "  ")
	if err != nil {
		return fmt.Errorf("shelf: %w", err)
	}
	b = append(b, '\n')

	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("shelf: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("shelf: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("shelf: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("shelf: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("shelf: %w", err)
	}
	return nil
}
