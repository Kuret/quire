package service

import (
	"testing"
	"time"
)

// clockedService is a service whose clock the test moves.
//
// Its own fixture: the age window is the one rule here that cannot be observed
// without time passing, and passing it by waiting would make the suite slow and
// flaky at once.
func clockedService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	s := New(Options{DownloadDir: t.TempDir(), Now: func() time.Time { return now }})
	t.Cleanup(s.Close)
	return s, &now
}

// A position that cannot be checked against a source is not restored, even
// well inside the age window. It is the same rule as everywhere else on this
// project: unable to check is never "fine".
//
// (The positive case — a recent position on a source that still exists —
// belongs with the real store and lives in resume_test.go.)
func TestAPositionIsNotRestoredWithoutASourceToCheck(t *testing.T) {
	s, now := clockedService(t)
	s.rememberPosition(resumePosition{SourceID: "src", SeriesID: "ser", Page: 2})

	*now = now.Add(resumeWindow - time.Minute)
	if got := s.resumeFor(); got != nil {
		t.Errorf("a position was restored with no source store to check it against: %+v", got)
	}
}

// The age rule itself. Two hours covers reading a long chapter and coming
// back; beyond that the user is starting something new.
//
// Asserted on freshAt rather than through resumeFor, because inside resumeFor
// it sits in front of other checks that also answer nil — removing it there
// changed no result, which is a guard nobody had tested.
func TestThePositionAgeWindow(t *testing.T) {
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	p := resumePosition{At: at}

	if !p.freshAt(at) {
		t.Error("a position is stale the moment it is made")
	}
	if !p.freshAt(at.Add(resumeWindow - time.Second)) {
		t.Error("a position just inside the window was dropped")
	}
	if !p.freshAt(at.Add(resumeWindow)) {
		t.Error("a position exactly at the window was dropped")
	}
	if p.freshAt(at.Add(resumeWindow + time.Second)) {
		t.Error("a position past the window was kept")
	}
	// A clock that went backwards — a reboot, or the tablet syncing its time.
	// Negative age is not old.
	if !p.freshAt(at.Add(-time.Hour)) {
		t.Error("a clock that went backwards made a position stale")
	}
}

// And the same rule where it is actually applied.
func TestAnOldPositionIsDropped(t *testing.T) {
	s, now := clockedService(t)
	s.rememberPosition(resumePosition{SourceID: "src", SeriesID: "ser", Page: 2})

	*now = now.Add(resumeWindow + time.Minute)
	if got := s.resumeFor(); got != nil {
		t.Errorf("a stale position was restored: %+v", got)
	}
}

// The age check happens before anything else looks at it, so an expired
// position is gone rather than kept for a later attach that would be older
// still.
func TestAnExpiredPositionIsNotKeptForNextTime(t *testing.T) {
	s, now := clockedService(t)
	s.rememberPosition(resumePosition{SourceID: "src", SeriesID: "ser"})

	*now = now.Add(resumeWindow + time.Minute)
	s.resumeFor()

	*now = now.Add(-resumeWindow) // even if the clock went backwards
	if got := s.resumeFor(); got != nil {
		t.Errorf("an expired position came back: %+v", got)
	}
}

// A position with nothing to identify it is not a position.
func TestAnIncompletePositionIsNotRemembered(t *testing.T) {
	s, _ := clockedService(t)

	s.rememberPosition(resumePosition{SourceID: "src"})
	if s.resume != nil {
		t.Error("a position with no series was remembered")
	}
	s.rememberPosition(resumePosition{SeriesID: "ser"})
	if s.resume != nil {
		t.Error("a position with no source was remembered")
	}
}

// Page 0 is what an older frontend, or a missing field, sends. The list starts
// at 1 and a page of 0 would be a blank screen.
func TestAMissingPageBecomesTheFirstOne(t *testing.T) {
	s, _ := clockedService(t)
	s.rememberPosition(resumePosition{SourceID: "src", SeriesID: "ser"})

	if s.resume == nil {
		t.Fatal("nothing was remembered")
	}
	if s.resume.Page != 1 {
		t.Errorf("page %d, want 1", s.resume.Page)
	}
}

// The origins a restored Back button may lead to, stated once and checked here
// rather than trusted from the frontend.
func TestOnlyListScreensMayBeReturnedTo(t *testing.T) {
	for _, screen := range []string{"sources", "watching", "downloaded"} {
		if !restorableOrigin(screen) {
			t.Errorf("%q should be a place to go back to", screen)
		}
	}
	for _, screen := range []string{"settings", "add", "browse", "series", "", "something-new"} {
		if restorableOrigin(screen) {
			t.Errorf("%q should not be a place to go back to", screen)
		}
	}
}
