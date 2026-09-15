// Package state is Quire's local store: the sources the user has added, and
// nothing else. PLAN §5 gives it backend/state/; PLAN §1.2 rules out any cloud
// sync of it, and PLAN §1.1 rule 5 rules out ever keeping a reading position
// here — that belongs to xochitl and is not ours to hold.
//
// The file it writes is the same shape a user can export and import to move a
// setup between devices (PLAN §7.2), so it is plain, sorted JSON rather than
// anything clever.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rickl/quire/backend/theme"
)

// FileName is the store's file inside the data directory.
const FileName = "sources.json"

// fileVersion is bumped only when the *envelope* changes. A source entry itself
// is versioned by schema/source.schema.json.
const fileVersion = 1

// ErrNotFound is returned for an ID no source has.
var ErrNotFound = errors.New("no such source")

// file is the on-disk envelope.
type file struct {
	Version int             `json:"version"`
	Sources []*theme.Source `json:"sources"`
}

// Store holds the configured sources. It is safe for concurrent use: the
// backend answers AppLoad messages from one loop today, but a probe running
// while the user toggles a source is exactly the kind of thing that happens.
type Store struct {
	path string
	reg  *theme.Registry

	mu      sync.RWMutex
	sources []*theme.Source
}

// Open loads the store from dir, creating the directory if it is missing. A
// store with no file yet is an empty store, not an error: PLAN §1.3 ships with
// an empty index and that is the normal first run.
func Open(dir string, reg *theme.Registry) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	s := &Store{path: filepath.Join(dir, FileName), reg: reg}

	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}

	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("state: %s is not readable JSON: %w", s.path, err)
	}
	// Every source is validated on the way in, even though it was validated on
	// the way out: a store can be hand-edited or imported from another device,
	// and a bad entry should surface here rather than three screens later.
	for _, src := range f.Sources {
		if err := reg.Validate(src); err != nil {
			return nil, fmt.Errorf("state: %s: source %q: %w", s.path, src.ID, err)
		}
	}
	s.sources = f.Sources
	s.sort()
	return s, nil
}

// Path is where the store writes, for diagnostics.
func (s *Store) Path() string { return s.path }

// List returns the sources, in display order. The entries are copies: a caller
// mutating what it was handed must not quietly change what is stored.
func (s *Store) List() []*theme.Source {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*theme.Source, 0, len(s.sources))
	for _, src := range s.sources {
		out = append(out, copySource(src))
	}
	return out
}

// Get returns one source by ID.
func (s *Store) Get(id string) (*theme.Source, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, src := range s.sources {
		if src.ID == id {
			return copySource(src), true
		}
	}
	return nil, false
}

// Add validates and stores a source, giving it a unique ID derived from its
// name, and returns the stored entry.
func (s *Store) Add(src *theme.Source) (*theme.Source, error) {
	if src == nil {
		return nil, errors.New("state: no source given")
	}
	stored := copySource(src)
	if stored.AddedAt.IsZero() {
		stored.AddedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored.ID = s.uniqueID(stored)
	if err := s.reg.Validate(stored); err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	// Adding the same site twice is a mistake with an unhelpful failure mode —
	// two identical rows, one of which the user will edit and the other of
	// which will keep answering — so it is refused by base URL.
	for _, existing := range s.sources {
		if strings.EqualFold(existing.BaseURL, stored.BaseURL) {
			return nil, fmt.Errorf("state: %s is already a source (%s)", stored.BaseURL, existing.Name)
		}
	}

	s.sources = append(s.sources, stored)
	s.sort()
	if err := s.save(); err != nil {
		return nil, err
	}
	return copySource(stored), nil
}

// SetEnabled is PLAN §6 M3's per-source toggle.
func (s *Store) SetEnabled(id string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			v := on
			src.Enabled = &v
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// SetProbe records a new probe result against a source (PLAN §6 M7 re-probes an
// existing source, and the UI shows the last result per source).
func (s *Store) SetProbe(id string, r *theme.ProbeResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			src.LastProbe = r
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// Remove deletes a source. Downloaded volumes are xochitl's and are left alone:
// they are ordinary documents in the user's library by then, and deleting a
// source is not a request to delete their books.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, src := range s.sources {
		if src.ID == id {
			s.sources = append(s.sources[:i], s.sources[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// sort orders by name, then ID, so the list does not reshuffle between runs.
func (s *Store) sort() {
	sort.SliceStable(s.sources, func(i, j int) bool {
		a, b := s.sources[i], s.sources[j]
		if !strings.EqualFold(a.Name, b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.ID < b.ID
	})
}

// save writes the whole file atomically. A half-written sources.json on a
// device that lost power mid-write would lose every source the user added, and
// there is no cloud copy to fall back on (PLAN §1.2).
func (s *Store) save() error {
	b, err := json.MarshalIndent(file{Version: fileVersion, Sources: s.sources}, "", "  ")
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	b = append(b, '\n')

	tmp := s.path + ".tmp"
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
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("state: %w", err)
	}
	return nil
}

var idUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// uniqueID derives a schema-legal ID from the source's name, with a numeric
// suffix if that is taken. The ID is never derived from the URL, so a source
// can be re-pointed at a mirror without orphaning what it downloaded.
func (s *Store) uniqueID(src *theme.Source) string {
	base := strings.Trim(idUnsafe.ReplaceAllString(strings.ToLower(src.Name), "-"), "-")
	if base == "" {
		base = "source"
	}
	if len(base) > 48 {
		base = strings.Trim(base[:48], "-")
	}
	candidate := base
	for n := 2; s.idTaken(candidate); n++ {
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
	return candidate
}

func (s *Store) idTaken(id string) bool {
	for _, src := range s.sources {
		if src.ID == id {
			return true
		}
	}
	return false
}

// copySource is a deep-enough copy: the maps and slices a caller could mutate
// are cloned, the scalars are not shared by definition.
func copySource(src *theme.Source) *theme.Source {
	out := *src
	if src.Overrides != nil {
		out.Overrides = make(map[string]any, len(src.Overrides))
		for k, v := range src.Overrides {
			out.Overrides[k] = v
		}
	}
	if src.Selectors != nil {
		out.Selectors = make(map[string]string, len(src.Selectors))
		for k, v := range src.Selectors {
			out.Selectors[k] = v
		}
	}
	if src.AllowedHosts != nil {
		out.AllowedHosts = append([]string(nil), src.AllowedHosts...)
	}
	if src.Enabled != nil {
		v := *src.Enabled
		out.Enabled = &v
	}
	if src.RateLimit != nil {
		v := *src.RateLimit
		out.RateLimit = &v
	}
	if src.LastProbe != nil {
		v := *src.LastProbe
		if src.LastProbe.ThemeScores != nil {
			v.ThemeScores = make(map[string]int, len(src.LastProbe.ThemeScores))
			for k, n := range src.LastProbe.ThemeScores {
				v.ThemeScores[k] = n
			}
		}
		out.LastProbe = &v
	}
	return &out
}
