package state

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rickl/quire/backend/theme"
)

// ErrSourceIsPrivate means a watch was refused because its source is marked
// private (theme.Source.Private). Watching is a standing background job whose
// result is pushed to the ordinary Watching list, and a private source's
// series has no home there — see service.watchListView.
var ErrSourceIsPrivate = errors.New("state: that source is private and cannot be watched")

// Watch is one watched series and what Quire knows about it since the user last
// looked (PLAN §12.2).
//
// It lives in the same versioned file as the sources, because "new since you
// last looked" is only meaningful if it survives a restart — and the backend on
// this device is killed for memory often enough that treating it as in-memory
// state would make the indicator a lie within a day.
type Watch struct {
	// SourceID and SeriesID together identify the series. The pair is the key:
	// two sources can legitimately carry the same series ID, and a watch keyed
	// on the series alone would merge them.
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`

	// Title is the display name at the time it was watched, so the watch list
	// can be drawn without fetching anything at all.
	Title string `json:"title"`

	AddedAt time.Time `json:"addedAt"`

	// Seen is the digest of every chapter the series had when the user last
	// looked at it, sorted. "New" is the set difference against this — see
	// ChapterDigest for why digests rather than the IDs themselves, and
	// NewChapters for why a set rather than a count.
	Seen []string `json:"seen,omitempty"`

	// SeenAt is when that set was taken. Zero means the baseline has never
	// been established, which is *not* the same as "the series had no
	// chapters": the first check seeds it and reports nothing new, because a
	// newly watched series is one the user is looking at right now.
	SeenAt time.Time `json:"seenAt,omitempty"`

	// NewCount is what the last successful check found: how many of the
	// source's current chapters are absent from Seen. It is stored rather than
	// recomputed because the badge has to be right before any check has run in
	// this session.
	//
	// It is always len(NewIDs) — the two are written together from one
	// observation, never counted twice. A badge that says three over an action
	// that downloads two is the failure this pairing exists to rule out.
	NewCount int `json:"newCount,omitempty"`

	// NewIDs are those chapters themselves, as the source's own chapter ids, in
	// the order the source listed them.
	//
	// The **ids and not digests**, unlike Seen: a digest can answer "have I seen
	// this?" and nothing else, and what the actions under the badge need is
	// something to download. Seen is the set that grows without bound over a
	// series' life and is worth compressing; this one is what appeared since the
	// user last looked, which is a handful — and on the pathological source that
	// reissues every URL at once it is still bounded by one series' chapter
	// list, which the check has just held in memory in full anyway.
	NewIDs []string `json:"newIds,omitempty"`

	// CheckedAt is when a check last *completed*, successfully or not. It is
	// what the per-source cooldown is measured from, so it has to be persisted:
	// an in-memory cooldown would be reset by every restart, and on this device
	// a restart is roughly as frequent as opening the app.
	CheckedAt time.Time `json:"checkedAt,omitempty"`

	// Failure is the plain-language reason the last check did not complete, or
	// "" when it did. PLAN §12.2: failure is not "new" — a series whose check
	// failed says so, and keeps whatever NewCount it legitimately had rather
	// than dropping to a reassuring zero.
	Failure string `json:"failure,omitempty"`
}

// IsWatched reports whether the series is watched.
func (s *Store) IsWatched(sourceID, seriesID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findWatch(sourceID, seriesID) != nil
}

// Watches returns every watched series, in display order. The entries are
// copies for the same reason List's are.
func (s *Store) Watches() []*Watch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Watch, 0, len(s.watched))
	for _, w := range s.watched {
		out = append(out, copyWatch(w))
	}
	return out
}

// Watch marks a series as watched and returns the stored entry.
//
// seen is the chapter list the user is looking at as they ask, when the caller
// has one. Seeding from it is what stops a freshly watched series announcing
// its entire back catalogue as new. A caller with nothing to offer passes nil,
// and the first successful check establishes the baseline instead.
//
// Watching an already-watched series is not an error: it refreshes the title
// and, if a baseline is offered, the baseline.
func (s *Store) Watch(sourceID, seriesID, title string, seen []string, now time.Time) (*Watch, error) {
	sourceID, seriesID = strings.TrimSpace(sourceID), strings.TrimSpace(seriesID)
	if sourceID == "" || seriesID == "" {
		return nil, fmt.Errorf("state: a watch needs a source and a series")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// A watch on a source that is not here would be an orphan from the moment
	// it was written, and PLAN §12.2 is explicit that orphans are not allowed
	// to exist.
	src, ok := s.getSourceLocked(sourceID)
	if !ok {
		return nil, fmt.Errorf("state: %q: %w", sourceID, ErrNotFound)
	}
	// A private source is kept out of the ordinary Watching list (see
	// service.watchListView), and a watch that could never be shown there is
	// not a feature, it is a way for a badge or a title to leak somewhere the
	// owner did not ask for it to appear. Refused outright rather than
	// silently accepted-but-hidden, so the "Watch" action can say why.
	if src.IsPrivate() {
		return nil, ErrSourceIsPrivate
	}

	w := s.findWatch(sourceID, seriesID)
	if w == nil {
		w = &Watch{SourceID: sourceID, SeriesID: seriesID, AddedAt: now.UTC()}
		s.watched = append(s.watched, w)
	}
	if t := strings.TrimSpace(title); t != "" {
		w.Title = t
	}
	if seen != nil {
		w.Seen = digests(seen)
		w.SeenAt = now.UTC()
		w.NewCount, w.NewIDs = 0, nil
	}
	s.sortWatches()
	if err := s.save(); err != nil {
		return nil, err
	}
	return copyWatch(w), nil
}

// Unwatch drops a watch. Unwatching something that is not watched is a no-op:
// the user asked for it not to be watched, and it is not.
func (s *Store) Unwatch(sourceID, seriesID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.watched {
		if w.SourceID == sourceID && w.SeriesID == seriesID {
			s.watched = append(s.watched[:i], s.watched[i+1:]...)
			return s.save()
		}
	}
	return nil
}

// MarkSeen records that the user has looked at the series: the given chapters
// become the baseline and nothing is new any more.
//
// It replaces the stored set rather than merging into it, which is what makes a
// source that *removes* chapters behave. A merge would accumulate every ID the
// series ever had, and a chapter re-added under its old ID would then never be
// reported again.
//
// A series that is not watched is ignored — the user reads plenty of series
// they have not asked to be told about.
func (s *Store) MarkSeen(sourceID, seriesID string, chapterIDs []string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.findWatch(sourceID, seriesID)
	if w == nil {
		return nil
	}
	w.Seen = digests(chapterIDs)
	w.SeenAt = now.UTC()
	w.NewCount, w.NewIDs = 0, nil
	w.Failure = ""
	return s.save()
}

// MarkNewSeen is the same claim made with no chapter list to hand: the chapters
// the last check called new have been dealt with, and nothing is new any more.
//
// It **merges** into Seen, where MarkSeen replaces it. That is not an
// inconsistency: MarkSeen is given the series' current chapter list and can
// therefore state the whole baseline, including which chapters have gone away.
// This one is given nothing, so the only honest edit is to add what it does
// know about — replacing the baseline with just the new ids would announce the
// entire back catalogue as new on the very next check.
//
// Failure is deliberately left alone. The user saying they have read these
// chapters is not evidence that the last check completed, and clearing it would
// turn "Quire couldn't look" into "up to date" on no new information at all.
//
// It returns the updated watch so the caller can redraw the row from what was
// stored rather than from what it hoped was stored.
func (s *Store) MarkNewSeen(sourceID, seriesID string, now time.Time) (*Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.findWatch(sourceID, seriesID)
	if w == nil {
		return nil, fmt.Errorf("state: %q/%q is not watched: %w", sourceID, seriesID, ErrNotFound)
	}
	w.Seen = mergeDigests(w.Seen, w.NewIDs)
	w.SeenAt = now.UTC()
	w.NewCount, w.NewIDs = 0, nil
	if err := s.save(); err != nil {
		return nil, err
	}
	return copyWatch(w), nil
}

// RecordCheck stores the result of a successful check: how many of the current
// chapters the user has not seen.
//
// When no baseline exists yet this seeds one and reports nothing new, because
// the alternative — announcing a 400-chapter back catalogue as "400 new
// chapters" the first time a check runs — is noise, not information.
func (s *Store) RecordCheck(sourceID, seriesID string, chapterIDs []string, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.findWatch(sourceID, seriesID)
	if w == nil {
		return 0, fmt.Errorf("state: %q/%q is not watched: %w", sourceID, seriesID, ErrNotFound)
	}
	w.CheckedAt = now.UTC()
	w.Failure = ""

	if w.SeenAt.IsZero() {
		w.Seen = digests(chapterIDs)
		w.SeenAt = now.UTC()
		w.NewCount, w.NewIDs = 0, nil
		return 0, s.save()
	}
	// One observation, both fields. The count is the length of the list and is
	// never arrived at separately — see NewCount.
	w.NewIDs = newIDs(w.Seen, chapterIDs)
	w.NewCount = len(w.NewIDs)
	return w.NewCount, s.save()
}

// RecordCheckFailure stores that a check did not complete, and why.
//
// NewCount is deliberately left alone. A failed check knows nothing about the
// series, so writing zero would be claiming "nothing new" on no evidence, and
// writing a count would be inventing one — both of which PLAN §12.2 calls out
// by name.
func (s *Store) RecordCheckFailure(sourceID, seriesID, reason string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.findWatch(sourceID, seriesID)
	if w == nil {
		return nil
	}
	w.CheckedAt = now.UTC()
	w.Failure = strings.TrimSpace(reason)
	if w.Failure == "" {
		w.Failure = "The check didn’t finish."
	}
	return s.save()
}

// LastCheckedSource is the most recent completed check across a source's
// watches, or the zero time if it has none. It is what the per-source cooldown
// in the service is measured from.
func (s *Store) LastCheckedSource(sourceID string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var last time.Time
	for _, w := range s.watched {
		if w.SourceID == sourceID && w.CheckedAt.After(last) {
			last = w.CheckedAt
		}
	}
	return last
}

// NewChapters counts the chapters in the list that are not in the seen set.
//
// This is a **set difference, never a comparison of counts**. Sources rewrite
// their own history: chapters get deleted for a DMCA notice, re-uploaded by a
// different group, or renumbered wholesale when a translation is revised. A
// "the number went up" check reports nothing at all when three chapters are
// added and three removed, and reports a spurious badge when a source merges
// two chapters into one and then publishes nothing.
//
// The identity used is the theme's chapter ID, which is a site-relative path
// (PLAN §7.2) and therefore survives renumbering — a chapter relabelled from
// "12" to "11.5" keeps its URL. What it does not survive is a source reissuing
// URLs, and nothing could: that is genuinely a new resource by every signal
// the site gives us.
func NewChapters(seen []string, chapterIDs []string) int { return countNew(seen, chapterIDs) }

// ChapterDigest is the stored identity of one chapter ID.
//
// The digest rather than the ID because this file is rewritten whole, fsynced,
// and atomically renamed on every change (see Store.save), and the user on the
// other end of that is holding a battery-powered tablet. Forty watched series
// of a few hundred chapters is tens of thousands of IDs; at ~45 bytes each that
// is a megabyte-and-a-half rewrite every time a source is toggled, and at 11
// bytes each it is not.
//
// 64 bits is ample: a collision needs two chapter IDs inside one series to
// agree, and at a thousand chapters that is a probability around 3e-14. The
// cost of losing that bet is one chapter not being announced as new, not a
// corrupted file.
func ChapterDigest(chapterID string) string {
	sum := sha256.Sum256([]byte(chapterID))
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

// countNew is the size of that difference. It is defined as the length of the
// list rather than as a second traversal, so the badge and the action under it
// cannot disagree even in principle.
func countNew(seen []string, chapterIDs []string) int {
	return len(newIDs(seen, chapterIDs))
}

// newIDs is the chapters in the list that are not in the seen set, in the
// source's own order.
func newIDs(seen []string, chapterIDs []string) []string {
	if len(chapterIDs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(seen))
	for _, d := range seen {
		set[d] = struct{}{}
	}
	// Over *distinct* chapters: a source that lists the same chapter twice —
	// two scanlation groups under one entry, a duplicated row in a template —
	// must not make the badge say two, nor queue the same download twice.
	counted := make(map[string]struct{}, len(chapterIDs))
	var out []string
	for _, id := range chapterIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		d := ChapterDigest(id)
		if _, ok := set[d]; ok {
			continue
		}
		if _, ok := counted[d]; ok {
			continue
		}
		counted[d] = struct{}{}
		out = append(out, id)
	}
	return out
}

// mergeDigests adds chapter ids to an existing digest set, sorted and
// de-duplicated, in the shape digests() produces.
func mergeDigests(seen []string, chapterIDs []string) []string {
	if len(chapterIDs) == 0 {
		return seen
	}
	set := make(map[string]struct{}, len(seen)+len(chapterIDs))
	out := make([]string, 0, len(seen)+len(chapterIDs))
	add := func(d string) {
		if _, ok := set[d]; ok {
			return
		}
		set[d] = struct{}{}
		out = append(out, d)
	}
	for _, d := range seen {
		add(d)
	}
	for _, id := range chapterIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		add(ChapterDigest(id))
	}
	sort.Strings(out)
	return out
}

// digests turns a chapter list into the stored, sorted, de-duplicated set.
func digests(chapterIDs []string) []string {
	out := make([]string, 0, len(chapterIDs))
	seen := make(map[string]struct{}, len(chapterIDs))
	for _, id := range chapterIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		d := ChapterDigest(id)
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// findWatch must be called with the lock held.
func (s *Store) findWatch(sourceID, seriesID string) *Watch {
	for _, w := range s.watched {
		if w.SourceID == sourceID && w.SeriesID == seriesID {
			return w
		}
	}
	return nil
}

// getSourceLocked must be called with the lock held. It returns the stored
// entry itself, not a copy: callers here only read it before releasing the
// lock, and never hand it further.
func (s *Store) getSourceLocked(id string) (*theme.Source, bool) {
	for _, src := range s.sources {
		if src.ID == id {
			return src, true
		}
	}
	return nil, false
}

// hasSource must be called with the lock held.
func (s *Store) hasSource(id string) bool {
	for _, src := range s.sources {
		if src.ID == id {
			return true
		}
	}
	return false
}

// dropWatchesFor removes every watch belonging to a source. Must be called with
// the lock held.
//
// PLAN §12.2: "a watched series whose source is removed is dropped with it".
// An orphan would otherwise sit in the list for ever, unwatchable from a UI
// that has no row to put it on and uncheckable by a backend with no theme to
// ask.
func (s *Store) dropWatchesFor(sourceID string) bool {
	kept := s.watched[:0]
	dropped := false
	for _, w := range s.watched {
		if w.SourceID == sourceID {
			dropped = true
			continue
		}
		kept = append(kept, w)
	}
	s.watched = kept
	return dropped
}

// sortWatches orders by title, then source, then series, so the watch list does
// not reshuffle between runs. Must be called with the lock held.
func (s *Store) sortWatches() {
	sort.SliceStable(s.watched, func(i, j int) bool {
		a, b := s.watched[i], s.watched[j]
		if !strings.EqualFold(a.Title, b.Title) {
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		}
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		return a.SeriesID < b.SeriesID
	})
}

func copyWatch(w *Watch) *Watch {
	out := *w
	out.Seen = append([]string(nil), w.Seen...)
	out.NewIDs = append([]string(nil), w.NewIDs...)
	return &out
}
