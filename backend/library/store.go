package library

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

// StoreFileName is where the store lives, beside sources.json.
const StoreFileName = "library.json"

const storeVersion = 1

// Key identifies a volume the way the rest of Quire thinks about one.
//
// It is (source, series, volume) and not the PDF's path, because the path is
// an implementation detail of backend/assemble and the UUID has to survive the
// downloaded PDF being deleted to reclaim space (PLAN §6 M7).
type Key struct {
	Source string `json:"source"`
	Series string `json:"series"`
	Volume string `json:"volume"`
}

func (k Key) valid() bool {
	return k.Source != "" && k.Series != "" && k.Volume != ""
}

// Record is one stored volume: the (source, series, volume) key and, above
// all, the document UUID.
//
// That UUID is the handle M6 opens the stock reader with, so it is modelled as
// the result of a download rather than as a log line about one. Everything
// else here is context for the UI and for diagnosing a library that has
// drifted from what Quire believes about it.
type Record struct {
	Key

	// DocumentUUID is xochitl's identifier for the document.
	DocumentUUID string `json:"documentUuid"`

	// FolderUUID is the folder it was put in; RootID for the top level.
	FolderUUID string `json:"folderUuid,omitempty"`

	// FolderPath is that folder in words, for the UI.
	FolderPath []string `json:"folderPath,omitempty"`

	// VisibleName is what the document is called on the tablet.
	VisibleName string `json:"visibleName,omitempty"`

	// PDF is the assembled file, so a re-download can be skipped and so M7 has
	// something to delete. It may be gone; the UUID outlives it.
	PDF string `json:"pdf,omitempty"`

	Pages int   `json:"pages,omitempty"`
	Bytes int64 `json:"bytes,omitempty"`

	// Chapters are the source-side chapter IDs this document holds, in reading
	// order.
	//
	// It is recorded rather than re-derived because a volume can be *split* to
	// fit xochitl's upload cap, and the split depends on the byte size of page
	// images that may since have been deleted to reclaim space. Without this,
	// working out which document holds a chapter would mean reproducing a
	// decision whose inputs are gone — and getting it wrong puts M6's "Read"
	// button on the wrong rows.
	Chapters []string `json:"chapters,omitempty"`

	// Part and Parts are 1-based, and both 0 when the volume was not split.
	Part  int `json:"part,omitempty"`
	Parts int `json:"parts,omitempty"`

	StoredAt time.Time `json:"storedAt"`
}

type storeFile struct {
	Version int      `json:"version"`
	Volumes []Record `json:"volumes"`
}

// Store remembers the document UUID of every volume Quire has put on the
// tablet.
type Store struct {
	path string

	mu   sync.RWMutex
	recs []Record
}

// OpenStore loads the store from dir, creating the directory if needed. A
// missing file is an empty store, not an error.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	s := &Store{path: filepath.Join(dir, StoreFileName)}

	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("library: %s is not readable: %w", s.path, err)
	}
	s.recs = f.Volumes
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

// Put stores a record, replacing any earlier one for the same key, and writes
// the file.
//
// Re-downloading a volume gives it a *new* document UUID — xochitl allocates
// one per upload and has no concept of replacing a document — so overwriting
// the old record is the correct behaviour: the old UUID no longer opens
// anything the user will recognise.
func (s *Store) Put(r Record) error {
	if !r.Key.valid() {
		return fmt.Errorf("library: a stored volume needs a source, a series and a volume")
	}
	if r.DocumentUUID == "" {
		return fmt.Errorf("library: a stored volume needs a document UUID")
	}
	if r.StoredAt.IsZero() {
		r.StoredAt = time.Now()
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

// Remove drops a record. A key that is not there is not an error: M7's
// "reclaim space" must be safe to run twice.
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
		return s.recs[i].StoredAt.After(s.recs[j].StoredAt)
	})
}

// save writes the whole file atomically, for the same reason state.Store does:
// a device that loses power mid-write must not come back with a truncated file,
// and there is no cloud copy of any of this.
func (s *Store) save() error {
	b, err := json.MarshalIndent(storeFile{Version: storeVersion, Volumes: s.recs}, "", "  ")
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	b = append(b, '\n')

	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("library: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("library: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("library: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("library: %w", err)
	}
	return nil
}
