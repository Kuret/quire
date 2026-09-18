package service

import (
	"time"

	"github.com/rickl/quire/backend/appload"
)

// Where the user was when the frontend closed itself.
//
// PLAN §12.5. Handing a document to the stock reader closes the frontend —
// AppLoad v0.5.3 renders its windows above the document view, cannot be
// lowered, and cannot be minimised without becoming unreachable, so closing is
// the only way the reader is visible at all. That leaves the user coming back
// from a comic to the sources screen, several taps from the chapter list they
// were reading.
//
// This is the smallest thing that fixes it: remember the one screen the handoff
// can be reached from, and put them back on it.
//
// # Where it lives, and why not in the state file
//
// **In the backend's memory, for as long as the backend runs.** Not in QML,
// which is unloaded — that is the whole problem. Not in `state.Store` either,
// and that is the more interesting half:
//
//   - The state file is what a user exports and imports to move a setup
//     between devices (see state.Settings). A reading position is not
//     configuration and has no business travelling.
//   - A backend that has restarted means the app was fully stopped or the
//     device rebooted. Landing someone in a chapter list from a previous
//     session is exactly the stale case this is trying to avoid, so losing the
//     position with the process is *correct*, not a limitation.
//
// The backend outliving its frontend is not an assumption: it is the same
// property in-flight downloads depend on, and AppLoad's README is explicit
// about it.
//
// # What makes it stale
//
// Three things, and each of them lands the user on the sources screen rather
// than on an error:
//
//   - **Age.** resumeWindow below.
//   - **The source is gone.** A removed source cannot be browsed, so a chapter
//     list on it cannot be shown.
//   - **The backend restarted**, which drops it by construction.
//
// # What it never restores
//
// The chapter/volume view mode. A series that published volumes last week may
// not today, and restoring a Volumes view for a series with no volumes is the
// same bug `volumesAvailable` was written to stop. The chapter list opens the
// way the fresh series detail says it should, every time.

// resumeWindow is how long a remembered position stays useful.
//
// Two hours, chosen for the case this exists for: read a chapter — which can
// genuinely take an hour — and come back. Beyond that the user is starting
// something new rather than continuing, and the sources screen is the honest
// place to start it.
const resumeWindow = 2 * time.Hour

// resumePosition is one remembered place in the app.
type resumePosition struct {
	SourceID   string
	SourceName string
	SeriesID   string
	Title      string

	// Page of the chapter list (PLAN §12.1), 1-based. A long series is many
	// pages, and landing on page 1 of forty is barely better than landing on
	// the sources screen.
	Page int

	// CameFrom is the screen Back should return to — one of the list screens
	// the series was opened from. Validated on the way in, because it becomes
	// the destination of a button the user will press without looking.
	CameFrom string

	At time.Time
}

// freshAt says whether a position is still worth restoring.
//
// Its own function so the age rule can be asserted on its own: inside
// resumeFor it sits in front of checks that also return nil, and a guard whose
// removal changes no test result is a guard nobody has tested.
func (p resumePosition) freshAt(now time.Time) bool {
	return now.Sub(p.At) <= resumeWindow
}

// rememberPosition records where the frontend was when it closed.
//
// It rides on the reader handoff rather than on a message of its own, because
// the handoff *is* the moment: it is the only thing that closes the frontend,
// and it already reports to the backend.
func (s *Service) rememberPosition(p resumePosition) {
	if p.SeriesID == "" || p.SourceID == "" {
		return
	}
	if p.Page < 1 {
		p.Page = 1
	}
	if !restorableOrigin(p.CameFrom) {
		// An origin this backend does not know is not passed on to a Back
		// button. "sources" is reachable from everywhere.
		p.CameFrom = "sources"
	}
	p.At = s.now()

	s.resumeMu.Lock()
	s.resume = &p
	s.resumeMu.Unlock()

	s.log.Info("remembering where the frontend was",
		"source", p.SourceID, "series", p.SeriesID, "page", p.Page)
}

// restorableOrigin says whether a screen may be the destination of the restored
// Back button.
//
// A closed list — the three the series screen can be opened from. Not "add" or
// "settings", which are not places to be returned to, and not "browse", whose
// paged grid is state this deliberately does not model.
func restorableOrigin(screen string) bool {
	return screen == "sources" || screen == "watching" || screen == "downloaded"
}

// resumeFor is the position to send a freshly-attached frontend, or nil.
//
// Every refusal drops the position rather than keeping it for later: the user
// has arrived at the sources screen, and a position that resurfaces on the next
// attach would be older still.
func (s *Service) resumeFor() *resumePosition {
	s.resumeMu.Lock()
	p := s.resume
	s.resume = nil
	s.resumeMu.Unlock()

	if p == nil {
		return nil
	}
	if !p.freshAt(s.now()) {
		s.log.Info("a remembered position was too old to use",
			"series", p.SeriesID, "age", s.now().Sub(p.At).Round(time.Second))
		return nil
	}
	if s.store == nil {
		return nil
	}
	src, ok := s.store.Get(p.SourceID)
	if !ok {
		// The source was removed while the user was reading. There is nothing
		// to browse, so there is nowhere to go back to.
		s.log.Info("a remembered position names a source that is gone", "source", p.SourceID)
		return nil
	}
	// The name is refreshed rather than replayed: it may have been renamed
	// since, and the header would otherwise show the old one.
	p.SourceName = src.Name
	return p
}

// sendResume pushes the position, if there is one worth pushing.
func (s *Service) sendResume(out Sender) error {
	p := s.resumeFor()
	if p == nil {
		return nil
	}
	s.log.Info("restoring the screen the frontend closed on",
		"source", p.SourceID, "series", p.SeriesID, "page", p.Page)
	return send(out, appload.MessageResume, map[string]any{
		"screen":     "series",
		"sourceId":   p.SourceID,
		"sourceName": p.SourceName,
		"seriesId":   p.SeriesID,
		"title":      p.Title,
		"page":       p.Page,
		"cameFrom":   p.CameFrom,
	})
}
