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
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
)

// FileName is the store's file inside the data directory.
const FileName = "sources.json"

// fileVersion is what a new file is written with. The envelope version is
// bumped when the *envelope* changes or when stored data needs converting; a
// source entry itself is versioned by schema/source.schema.json. See
// migrate.go for the forward-migration chain.
const fileVersion = CurrentVersion

// ErrNotFound is returned for an ID no source has.
var ErrNotFound = errors.New("no such source")

// file is the on-disk envelope.
type file struct {
	Version int             `json:"version"`
	Sources []*theme.Source `json:"sources"`

	// Watched is PLAN §12.2's watched series. It rides in the same envelope as
	// the sources rather than in a file of its own because it is meaningless
	// without them — a watch names a source — and because the pair has to be
	// exported and imported together to be worth anything.
	Watched []*Watch `json:"watched,omitempty"`

	// Settings is the device-wide configuration of settings.go. Omitted when
	// empty, so a file written before the setting existed and a file whose
	// settings are all at their defaults are the same file.
	Settings *Settings `json:"settings,omitempty"`
}

// Store holds the configured sources. It is safe for concurrent use: the
// backend answers AppLoad messages from one loop today, but a probe running
// while the user toggles a source is exactly the kind of thing that happens.
type Store struct {
	path string
	reg  *theme.Registry

	// coverPath is the cover URL cache, which is deliberately *not* part of the
	// exported envelope. See covers.go.
	coverPath string

	mu       sync.RWMutex
	sources  []*theme.Source
	watched  []*Watch
	settings Settings

	// coverURLs is keyed by coverKey(sourceID, seriesID).
	coverURLs map[string]coverEntry
}

// Open loads the store from dir, creating the directory if it is missing. A
// store with no file yet is an empty store, not an error: PLAN §1.3 ships with
// an empty index and that is the normal first run.
func Open(dir string, reg *theme.Registry) (*Store, error) {
	return OpenWithLog(dir, reg, slog.Default())
}

// OpenWithLog is Open with somewhere to record what a migration did. A
// migration that quietly rewrites the user's configuration and says nothing is
// how a surprising change becomes an unexplainable one.
func OpenWithLog(dir string, reg *theme.Registry, log *slog.Logger) (*Store, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	s := &Store{
		path:      filepath.Join(dir, FileName),
		coverPath: filepath.Join(dir, CoverFileName),
		reg:       reg,
	}
	// Before the store's own file is read, because a first run has no
	// sources.json at all and returns early — and a first run after a
	// re-install is still a device with covers already on disk.
	s.loadCovers(log)

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

	// Migrate before validating. A migration's whole job is to make an old
	// file valid for today's code, so validating first would reject exactly the
	// files the migration exists to rescue.
	migrated, err := migrate(&f, reg, log)
	if err != nil {
		return nil, err
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
	s.watched = f.Watched
	if f.Settings != nil {
		s.settings = copySettings(*f.Settings)
	}
	s.sort()
	s.sortWatches()

	if migrated {
		// Written back immediately so the migration runs once, not on every
		// launch until something else happens to save.
		if err := s.save(); err != nil {
			return nil, err
		}
		log.Info("source store migrated and saved", "path", s.path, "version", CurrentVersion)
	}
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

// ErrBadSplitStrips means the requested strip-splitting mode is not one the
// schema offers.
var ErrBadSplitStrips = errors.New("state: splitStrips must be auto, never or always")

// SetSplitStrips changes a source's strip-splitting override (PLAN §12.3).
//
// The empty string is accepted and stored as empty, which is how a source that
// has never been touched stays on the schema default rather than being pinned
// to a value it never asked for.
func (s *Store) SetSplitStrips(id, mode string) error {
	if mode != "" && !slices.Contains(theme.SplitStripsValues, mode) {
		return ErrBadSplitStrips
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			if src.SplitStrips == mode {
				return nil
			}
			src.SplitStrips = mode
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// NameMaxLen is schema/source.schema.json's maxLength for a source name. It is
// mirrored here so a rename is refused with a sentence rather than by failing
// schema validation somewhere further down.
const NameMaxLen = 120

// ErrBadName means the requested name is empty or too long.
var ErrBadName = errors.New("state: a source needs a name of 1 to 120 characters")

// Rename changes a source's display name (PLAN §7.1 type 19).
//
// It exists because the name is guessed from the site's <title> at probe time
// and that guess will be wrong for some site forever — the first real use
// produced "MangaDex API documentation" from an API root. The schema has always
// described the name as "then user-editable"; this is what makes that true.
//
// The ID is deliberately *not* re-derived from the new name. It keys everything
// already downloaded (library.Key), so renaming a source must not orphan the
// user's books.
func (s *Store) Rename(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > NameMaxLen {
		return ErrBadName
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			if src.Name == name {
				return nil
			}
			src.Name = name
			s.sort()
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// ErrBadSelfHosted means the address offered with a confirmation is not one a
// source can be confirmed on.
var ErrBadSelfHosted = errors.New("state: a source can only be confirmed on a private or CGNAT address")

// ConfirmSelfHosted records that the user says this source is a service on
// their own network, and is the *only* way that ever gets recorded.
//
// It takes the address the source's host resolved to, because the record is of
// what the user agreed to and an agreement to reach "whatever this name points
// at" is not one worth keeping. The caller has to have resolved the host and
// have the answer in hand; there is deliberately no variant that resolves it
// here and confirms whatever comes back, since that would be Quire deciding
// rather than the user.
//
// Everything about why this exists is on theme.SelfHosted and fetch.Guard. The
// short version: the address rules exist because scraped content chooses URLs
// Quire then fetches unattended, and a host the user typed themselves is the
// one case that reasoning does not cover.
func (s *Store) ConfirmSelfHosted(id, addr string) error {
	parsed, err := netip.ParseAddr(strings.TrimSpace(addr))
	if err != nil || !fetch.SelfHostableAddr(parsed) {
		return fmt.Errorf("%w (got %q)", ErrBadSelfHosted, addr)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			was := src.SelfHosted
			src.SelfHosted = &theme.SelfHosted{ConfirmedAddr: parsed.String(), ConfirmedAt: time.Now().UTC()}
			// Validated after being set rather than before, so the check is on
			// the source as it would be stored — in particular the baseUrl/
			// address agreement, which this function cannot see on its own.
			if err := s.reg.Validate(src); err != nil {
				src.SelfHosted = was
				return fmt.Errorf("state: %w", err)
			}
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// ErrBadProxy means the proxy URL offered is not one Quire can use.
var ErrBadProxy = errors.New("state: a proxy must be an http://, https:// or socks5:// address")

// SetProxy records the proxy a source is reached through, or clears it when raw
// is empty. It is the only writer of theme.Source.Proxy, for the same reason
// ConfirmSelfHosted is the only writer of the confirmation beside it: both are
// assertions the *user* made about one source, and a single writer is what
// makes "nothing else can set this" checkable rather than hoped for.
//
// The URL is validated here — at the point it is set — rather than at the point
// it is used. A proxy that is only rejected when a chapter fails to download is
// one the user debugs from the wrong end of the app.
func (s *Store) SetProxy(id, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, err := fetch.ParseProxyURL(raw)
		if err != nil {
			return fmt.Errorf("%w (%v)", ErrBadProxy, err)
		}
		// Store what was parsed, so the scheme is normalised and the stored
		// value is the one the fetch layer will act on.
		raw = u.String()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			if src.Proxy == raw {
				return nil
			}
			was := src.Proxy
			src.Proxy = raw
			if err := s.reg.Validate(src); err != nil {
				src.Proxy = was
				return fmt.Errorf("state: %w", err)
			}
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// ErrNoProxyToConfirm means a source was offered as "confirmed by the proxy
// that reaches it" without having a proxy.
var ErrNoProxyToConfirm = errors.New("state: a source confirmed through a proxy has to have one")

// ConfirmSelfHostedViaProxy records the confirmation for the source whose host
// does not resolve on this device at all.
//
// It is a second method rather than ConfirmSelfHosted with an empty address,
// because the two record different evidence and each has a precondition worth
// stating: that one needs an address the caller resolved, and this one needs
// the source to already carry the proxy the user typed. A single function
// taking "" would have neither precondition and would be the easiest way to end
// up with a confirmation standing on nothing.
//
// The order matters and is the caller's to get right: SetProxy first, then
// this. It is checked rather than assumed — a source with no proxy is refused
// here, and validation refuses the same shape again on the way to disk.
//
// Why there is no address: measured 2026-09-20, the owner's source is a
// MagicDNS name on their mesh. The tablet resolves against public DNS, the
// kernel has no TUN device so the userspace VPN installs no resolver, and the
// name never resolves here — the proxy resolves it. Resolving through the proxy
// just to have a value to store was rejected; see theme.SelfHosted.ViaProxy.
func (s *Store) ConfirmSelfHostedViaProxy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			if strings.TrimSpace(src.Proxy) == "" {
				return fmt.Errorf("%w (%q)", ErrNoProxyToConfirm, id)
			}
			was := src.SelfHosted
			src.SelfHosted = &theme.SelfHosted{ViaProxy: true, ConfirmedAt: time.Now().UTC()}
			if err := s.reg.Validate(src); err != nil {
				src.SelfHosted = was
				return fmt.Errorf("state: %w", err)
			}
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// RevokeSelfHosted takes the confirmation back. A permission that cannot be
// withdrawn is not one the user is in charge of, and unlike ConfirmSelfHosted
// this direction needs no evidence: it only ever makes the guard stricter.
func (s *Store) RevokeSelfHosted(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			if src.SelfHosted == nil {
				return nil
			}
			src.SelfHosted = nil
			return s.save()
		}
	}
	return fmt.Errorf("state: %q: %w", id, ErrNotFound)
}

// ClearProxyAndRevoke clears a source's proxy and, if the source's self-hosted
// confirmation stands on that proxy (SelfHosted.ViaProxy), revokes the
// confirmation in the same locked operation.
//
// The two edits have to land together: a ViaProxy confirmation with no proxy
// beneath it is exactly what theme.Source.validate() refuses, so doing this as
// a clear then a separate revoke — two calls, each taking and releasing the
// lock — would let another goroutine observe, or the store persist, the
// in-between shape where the confirmation is orphaned. See SetProxy and
// ConfirmSelfHostedViaProxy for the two assertions this reconciles.
//
// A source confirmed by address (ConfirmedAddr set, ViaProxy false) is left
// alone: its evidence does not depend on the proxy being present.
func (s *Store) ClearProxyAndRevoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, src := range s.sources {
		if src.ID == id {
			wasProxy := src.Proxy
			wasSelfHosted := src.SelfHosted
			src.Proxy = ""
			if src.SelfHosted != nil && src.SelfHosted.ViaProxy {
				src.SelfHosted = nil
			}
			if err := s.reg.Validate(src); err != nil {
				src.Proxy = wasProxy
				src.SelfHosted = wasSelfHosted
				return fmt.Errorf("state: %w", err)
			}
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
			// PLAN §12.2: a watched series whose source is removed goes with
			// it. Leaving the watch behind would leave a row nothing can
			// check and nothing can un-watch.
			s.dropWatchesFor(id)
			// The remembered cover URLs go too; see dropCoversFor. They live in
			// their own file, so this is a second write, and only when there
			// was something to forget.
			//
			// **Its error is deliberately dropped.** By this point the source is
			// already out of s.sources, so returning here would save neither
			// file and leave memory disagreeing with disk about whether the
			// source exists — a far worse outcome than a stale cache. What
			// survives a failure is a covers file naming a source that is gone,
			// which nothing will ever look up (the lookups are by source id) and
			// which the next cover write rewrites anyway.
			if s.dropCoversFor(id) {
				_ = s.saveCovers()
			}
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
	env := file{
		Version: fileVersion,
		Sources: s.sources,
		Watched: s.watched,
	}
	// Only written once something has been set, so a store nobody has
	// configured keeps the file it had.
	if s.settings != (Settings{}) {
		set := copySettings(s.settings)
		env.Settings = &set
	}
	b, err := json.MarshalIndent(env, "", "  ")
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
	if src.SelfHosted != nil {
		v := *src.SelfHosted
		out.SelfHosted = &v
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
