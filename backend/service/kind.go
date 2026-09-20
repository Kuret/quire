package service

import "github.com/rickl/quire/backend/theme"

// What kind of thing a source deals in, as sent to the frontend.
//
// The distinction is the one the user can see: a manga is chapters of page
// images that Quire assembles into a PDF, and a book is a finished epub the
// source already has. They look different on screen — a book has no chapter
// numbers, no volumes and no page count — and the frontend has no other way to
// know which it is holding, because both arrive as a series with chapters.
//
// It is derived from theme.FileTheme rather than stored on the source, for the
// same reason PLAN §7.2 keeps the ordering contract in the theme: the theme is
// the only thing that knows what its chapters are. A field on the source entry
// would be a second copy of that fact, editable by a user and able to disagree.
const (
	kindBook  = "book"
	kindManga = "manga"
)

// kindOf says what a theme's results are.
//
// Every theme that is not a file theme is a manga source. That is a statement
// about this codebase rather than about the world — the eight page-based themes
// all drive comic sites — and it is worth making rather than leaving "" as the
// default, because a row with no kind at all is one the frontend cannot filter
// either way.
func kindOf(th theme.Theme) string {
	if _, ok := th.(theme.FileTheme); ok {
		return kindBook
	}
	return kindManga
}

// kindForSource is kindOf for a source id, or "" when there is no source or no
// theme left to ask.
//
// Empty rather than a guess: the downloaded overview lists rows whose source has
// been removed (see RemovedSourceNote), and those rows genuinely have no kind —
// the files are on the tablet and nothing is left that could say what they were.
// Calling them manga because most sources are would be inventing the answer.
func (s *Service) kindForSource(sourceID string) string {
	th, _, err := s.themeFor(sourceID)
	if err != nil {
		return ""
	}
	return kindOf(th)
}
