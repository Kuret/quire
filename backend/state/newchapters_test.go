package state_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rickl/quire/backend/state"
)

// watchOf is the stored watch, read back the way the service reads it.
func watchOf(t *testing.T, s *state.Store, seriesID string) *state.Watch {
	t.Helper()
	for _, w := range s.Watches() {
		if w.SeriesID == seriesID {
			return w
		}
	}
	t.Fatalf("%q is not watched", seriesID)
	return nil
}

const lanternWatched = "/manga/the-lantern-keeper/"

// A check that finds three chapters has to be able to say *which* three. The
// count and the list come from one observation, so the badge and the action
// under it cannot disagree.
func TestRecordCheckStoresTheNewChaptersItCounted(t *testing.T) {
	s, _ := newWatchStore(t)
	// The baseline is everything but the two the "site" is about to have, so
	// the check has something genuinely new to find.
	seen := chapterIDs[:5]
	if _, err := s.Watch("example-reader", lanternWatched, "The Lantern Keeper", seen, watchNow); err != nil {
		t.Fatal(err)
	}

	// The list the site now serves: reordered, with a duplicate row, because
	// real ones are.
	now := []string{
		chapterIDs[6], chapterIDs[0], chapterIDs[5], chapterIDs[5],
		chapterIDs[2], chapterIDs[1], chapterIDs[4], chapterIDs[3],
	}
	n, err := s.RecordCheck("example-reader", lanternWatched, now, watchNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("the check found %d new chapters, want 2", n)
	}

	w := watchOf(t, s, lanternWatched)
	if w.NewCount != len(w.NewIDs) {
		t.Errorf("badge says %d over a list of %d; they are one observation",
			w.NewCount, len(w.NewIDs))
	}
	want := map[string]bool{chapterIDs[5]: true, chapterIDs[6]: true}
	if len(w.NewIDs) != len(want) {
		t.Fatalf("new ids = %q, want the two unseen chapters", w.NewIDs)
	}
	for _, id := range w.NewIDs {
		if !want[id] {
			t.Errorf("new ids include %q, which the user has already seen", id)
		}
	}
}

// A first check has no baseline to compare against, so it seeds one and reports
// nothing — and must not offer a back catalogue to download either.
func TestAFirstCheckHasNoNewChaptersToOffer(t *testing.T) {
	s, _ := newWatchStore(t)
	if _, err := s.Watch("example-reader", lanternWatched, "The Lantern Keeper", nil, watchNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordCheck("example-reader", lanternWatched, chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}
	w := watchOf(t, s, lanternWatched)
	if w.NewCount != 0 || len(w.NewIDs) != 0 {
		t.Errorf("a seeding check left %d new (%q)", w.NewCount, w.NewIDs)
	}
}

// "I read it elsewhere": the stored new ids become part of the baseline, the
// badge goes, and the very next check agrees that there is nothing new.
func TestMarkNewSeenMovesTheNewIDsIntoSeen(t *testing.T) {
	s, dir := newWatchStore(t)
	if _, err := s.Watch("example-reader", lanternWatched, "The Lantern Keeper", chapterIDs[:5], watchNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordCheck("example-reader", lanternWatched, chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}

	later := watchNow.Add(time.Hour)
	w, err := s.MarkNewSeen("example-reader", lanternWatched, later)
	if err != nil {
		t.Fatal(err)
	}
	if w.NewCount != 0 || len(w.NewIDs) != 0 {
		t.Errorf("after marking seen the badge says %d (%q)", w.NewCount, w.NewIDs)
	}

	// The baseline really moved: the same list checked again is not new.
	n, err := s.RecordCheck("example-reader", lanternWatched, chapterIDs, later)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d new chapters after they were marked seen", n)
	}
	// The earlier baseline is still part of it — a merge, not a replacement —
	// so a chapter the user saw before is not announced again either.
	if got := state.NewChapters(watchOf(t, s, lanternWatched).Seen, chapterIDs[:2]); got != 0 {
		t.Errorf("%d of the old chapters came back as new; the baseline was replaced, not merged", got)
	}

	// And it survives a restart, like every other thing the badge is drawn from.
	reopened, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := watchOf(t, reopened, lanternWatched); got.NewCount != 0 || len(got.NewIDs) != 0 {
		t.Errorf("after a restart the badge says %d (%q)", got.NewCount, got.NewIDs)
	}
}

// A series nobody is watching is not something to mark as seen, and saying so
// is what stops the service answering a stale row with a cheerful nothing.
func TestMarkNewSeenRefusesASeriesThatIsNotWatched(t *testing.T) {
	s, _ := newWatchStore(t)
	if _, err := s.MarkNewSeen("example-reader", "/manga/not-watched/", watchNow); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A check that could not complete says nothing about the chapters, so it must
// not quietly empty the list the badge is standing on.
func TestAFailedCheckLeavesTheNewChaptersAlone(t *testing.T) {
	s, _ := newWatchStore(t)
	if _, err := s.Watch("example-reader", lanternWatched, "The Lantern Keeper", chapterIDs[:5], watchNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordCheck("example-reader", lanternWatched, chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}
	before := watchOf(t, s, lanternWatched)

	if err := s.RecordCheckFailure("example-reader", lanternWatched, "The site did not answer.", watchNow); err != nil {
		t.Fatal(err)
	}
	after := watchOf(t, s, lanternWatched)
	if after.NewCount != before.NewCount || len(after.NewIDs) != len(before.NewIDs) {
		t.Errorf("a failed check changed the new chapters: %d (%q) -> %d (%q)",
			before.NewCount, before.NewIDs, after.NewCount, after.NewIDs)
	}
}
