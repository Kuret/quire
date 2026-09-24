// Books in Quire's own reader (books-contract.md §B): a theme.FileTheme
// source's chapters now render as page images via backend/bookrender rather
// than always ending up in the reMarkable library. This file is the service
// layer's half of the wire contract — the session-management twin of
// tryreader.go and saved.go, which is why it reuses their entry points
// (MessageOpenSaved, MessageTryChapter, MessageSavePosition) instead of
// adding parallel ones QML would have to branch on kind to reach.
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/bookreader"
	"github.com/rickl/quire/backend/bookrender"
	"github.com/rickl/quire/backend/shelf"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// bookKey identifies which chapter the current book session belongs to —
// tryKey's exact shape, kept separate because a book session and a Try
// session are two different things that can (in principle) each be "the one
// thing currently open" without colliding.
type bookKey struct {
	Source, Series, Chapter string
}

func (k bookKey) valid() bool { return k.Source != "" && k.Series != "" && k.Chapter != "" }

const (
	bookModeSaved = "saved"
	bookModeTry   = "try"
)

// bookPageRequest is MessageBookPageRequest's payload.
type bookPageRequest struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
	Index     int    `json:"index"`
}

// setReaderSettingsRequest is MessageSetReaderSettings's payload.
type setReaderSettingsRequest struct {
	SourceID  string               `json:"sourceId"`
	SeriesID  string               `json:"seriesId"`
	ChapterID string               `json:"chapterId"`
	Page      int                  `json:"page"`
	Settings  state.ReaderSettings `json:"settings"`
}

// closeBookRequest is MessageCloseBook's payload.
type closeBookRequest struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
	Page      int    `json:"page"`
}

func (r bookPageRequest) key() bookKey { return bookKey{r.SourceID, r.SeriesID, r.ChapterID} }
func (r setReaderSettingsRequest) key() bookKey {
	return bookKey{r.SourceID, r.SeriesID, r.ChapterID}
}
func (r closeBookRequest) key() bookKey { return bookKey{r.SourceID, r.SeriesID, r.ChapterID} }

// bookOpened is MessageBookOpened's payload.
type bookOpened struct {
	SourceID       string                     `json:"sourceId"`
	SeriesID       string                     `json:"seriesId"`
	ChapterID      string                     `json:"chapterId"`
	Title          string                     `json:"title"`
	Mode           string                     `json:"mode"`
	FixedLayout    bool                       `json:"fixedLayout"`
	PageCount      int                        `json:"pageCount"`
	Page           int                        `json:"page"`
	Toc            []bookrender.TocEntry      `json:"toc"`
	Settings       state.ReaderSettings       `json:"settings"`
	SettingsNote   string                     `json:"settingsNote"`
	SettingsFields []bookrender.SettingsField `json:"settingsFields"`
	Private        bool                       `json:"private"`
	InLibrary      bool                       `json:"inLibrary"`
}

// settingsPanelFields is the "Aa" panel's whole description for a session:
// empty for a fixed-layout book (PDF/XPS/CBZ), which has no layout controls
// for it to describe — the panel stays hidden, exactly as it always has.
func settingsPanelFields(fixedLayout bool) []bookrender.SettingsField {
	if fixedLayout {
		return nil
	}
	return bookrender.SettingsFields()
}

// bookStatus is MessageBookStatus's payload.
type bookStatus struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
	Message   string `json:"message"`
}

func (s *Service) sendBookStatus(out Sender, k bookKey, message string) {
	_ = send(out, appload.MessageBookStatus, bookStatus{
		SourceID: k.Source, SeriesID: k.Series, ChapterID: k.Chapter, Message: message,
	})
}

// currentBookSession returns the session open for key, if any is.
func (s *Service) currentBookSession(key bookKey) (*bookreader.Session, string, bool) {
	s.bookMu.Lock()
	defer s.bookMu.Unlock()
	if s.bookSession == nil || s.bookKey != key {
		return nil, "", false
	}
	return s.bookSession, s.bookMode, true
}

// bookSessionStillCurrent reports whether sess is still the session open —
// false once the user has closed it or opened another book, the cue to say
// nothing rather than report on a book nobody is looking at any more (the
// same rule tryreader.go's trySessionStillCurrent follows).
func (s *Service) bookSessionStillCurrent(sess *bookreader.Session) bool {
	s.bookMu.Lock()
	defer s.bookMu.Unlock()
	return s.bookSession == sess
}

// endCurrentBookSession ends whichever book session is open, if any,
// removing its cache and (in try mode) its fetched file. It is called both
// when a new session is about to replace the old one and when the reader
// sends MessageCloseBook.
func (s *Service) endCurrentBookSession() {
	s.bookMu.Lock()
	sess := s.bookSession
	tryPath := s.bookTryPath
	s.bookSession = nil
	s.bookKey = bookKey{}
	s.bookMode = ""
	s.bookTryPath = ""
	s.bookMu.Unlock()

	if sess == nil {
		return
	}
	if err := sess.Close(); err != nil {
		s.log.Warn("could not close a book session cleanly", "err", err)
	}
	if tryPath != "" {
		if err := os.Remove(tryPath); err != nil && !os.IsNotExist(err) {
			s.log.Warn("could not remove a tried book's fetched file", "path", tryPath, "err", err)
		}
	}
}

// openSavedBook is openSaved's branch for a shelf.Record of Kind book: it
// opens the saved file in a book session rather than handing back page
// paths, and answers MessageBookOpened instead of MessageSavedOpened.
func (s *Service) openSavedBook(ctx context.Context, out Sender, req savedRequest, rec shelf.Record) error {
	if s.bookCache == nil {
		return s.sendError(out, "unavailable", "This build of Quire cannot open its own book reader.")
	}
	root, err := filepath.Abs(filepath.Clean(s.savedDir))
	if err != nil {
		return s.sendError(out, "saved_missing", SavedMissingRemedy)
	}
	full := filepath.Join(root, rec.File)
	if _, err := os.Stat(full); err != nil {
		s.log.Warn("a saved book's file is missing on disk; forgetting it",
			"source", req.SourceID, "series", req.SeriesID, "chapter", req.ChapterID)
		_ = s.shelfStore.Remove(req.key())
		return s.sendError(out, "saved_missing", SavedMissingRemedy)
	}

	key := bookKey{req.SourceID, req.SeriesID, req.ChapterID}
	private := false
	if src, ok := s.store.Get(req.SourceID); ok {
		private = src.IsPrivate()
	}
	inLibrary := s.chapterInLibrary(req.SourceID, req.SeriesID, req.ChapterID)

	// LastReadAt marks this as a real read, exactly as openSaved does for a
	// chapter of pages — see that function's own comment.
	rec.LastReadAt = s.now()
	if err := s.shelfStore.Put(rec); err != nil {
		s.log.Warn("could not remember when a saved book was opened",
			"source", req.SourceID, "series", req.SeriesID, "chapter", req.ChapterID, "err", err)
	}

	s.sendBookStatus(out, key, "Opening the book…")
	s.goBackground(ctx, func(ctx context.Context) {
		s.endCurrentBookSession()

		settings := s.store.Settings().Reader()
		sess, err := s.bookCache.Open(ctx, full, settings)
		if err != nil {
			_ = s.sendError(out, "book_unreadable", fmt.Sprintf(
				"Quire could not open that book: %s", plain(err)))
			return
		}

		s.bookMu.Lock()
		s.bookSession = sess
		s.bookKey = key
		s.bookMode = bookModeSaved
		s.bookMu.Unlock()

		page := sess.InitialPage(ctx, rec.Position, rec.PositionFraction, rec.PositionSnippet, rec.PositionLayout)
		title := sess.Title()
		if strings.TrimSpace(title) == "" {
			title = rec.ChapterTitle
		}
		_ = send(out, appload.MessageBookOpened, bookOpened{
			SourceID: req.SourceID, SeriesID: req.SeriesID, ChapterID: req.ChapterID,
			Title: title, Mode: bookModeSaved, FixedLayout: sess.FixedLayout(),
			PageCount: sess.PageCount(), Page: page, Toc: sess.TOC(),
			Settings: settings, SettingsNote: bookrender.SettingsNote,
			SettingsFields: settingsPanelFields(sess.FixedLayout()),
			Private:        private, InLibrary: inLibrary,
		})
	})
	return nil
}

// startTryBook is startTry's branch for a theme.FileTheme source: fetch the
// file (the same guarded Retrieve+fetch runFileDownload uses), open it in a
// "try" book session, and delete the fetched file on close — there is no
// download queue involved and nothing is remembered afterwards, the same
// disposability rule an ordinary Try session follows.
func (s *Service) startTryBook(ctx context.Context, out Sender, req tryRequest, th theme.Theme, ft theme.FileTheme, src *theme.Source) {
	if s.bookCache == nil {
		_ = s.sendError(out, "try_unavailable",
			"This build of Quire cannot open its own book reader.")
		return
	}
	key := bookKey{req.SourceID, req.SeriesID, req.ChapterID}
	s.sendBookStatus(out, key, "Asking "+sourceLabel(src)+" for this book…")

	fileURL, name, err := ft.Retrieve(ctx, src, req.ChapterID, func(note string) {
		s.sendBookStatus(out, key, progressSentence(note))
	})
	if err != nil {
		_ = s.sendError(out, "try_failed", fmt.Sprintf("Quire could not get this book: %s", plain(err)))
		return
	}
	s.sendBookStatus(out, key, fmt.Sprintf("Downloading %s…", name))
	file, err := s.fetchFile(ctx, th, src, fileURL)
	if err != nil {
		_ = s.sendError(out, "try_failed", fmt.Sprintf("Quire could not download the file: %s", plain(err)))
		return
	}

	format := bookFormat(name)
	if !bookFormatReadable(format) {
		_ = s.sendError(out, "book_unreadable", fmt.Sprintf(
			"Quire can't display %s books, so there is nothing to preview here.", strings.ToUpper(format)))
		return
	}

	tryDir := filepath.Join(s.bookCache.Dir(), "try")
	if err := os.MkdirAll(tryDir, 0o755); err != nil {
		_ = s.sendError(out, "try_failed", "Quire could not prepare a place to open this book.")
		return
	}
	tmpPath := filepath.Join(tryDir, safeSegment(req.SourceID)+"-"+safeSegment(req.ChapterID)+"."+format)
	if err := os.WriteFile(tmpPath, file, 0o644); err != nil {
		_ = s.sendError(out, "try_failed", "Quire could not save this book to preview it.")
		return
	}

	s.sendBookStatus(out, key, "Opening the book…")
	s.endCurrentBookSession()

	settings := s.store.Settings().Reader()
	sess, err := s.bookCache.Open(ctx, tmpPath, settings)
	if err != nil {
		_ = os.Remove(tmpPath)
		_ = s.sendError(out, "book_unreadable", fmt.Sprintf("Quire could not open that book: %s", plain(err)))
		return
	}

	s.bookMu.Lock()
	s.bookSession = sess
	s.bookKey = key
	s.bookMode = bookModeTry
	s.bookTryPath = tmpPath
	s.bookMu.Unlock()

	title := sess.Title()
	_ = send(out, appload.MessageBookOpened, bookOpened{
		SourceID: req.SourceID, SeriesID: req.SeriesID, ChapterID: req.ChapterID,
		Title: title, Mode: bookModeTry, FixedLayout: sess.FixedLayout(),
		PageCount: sess.PageCount(), Page: 0, Toc: sess.TOC(),
		Settings: settings, SettingsNote: bookrender.SettingsNote,
		SettingsFields: settingsPanelFields(sess.FixedLayout()),
		Private:        src.IsPrivate(), InLibrary: s.chapterInLibrary(req.SourceID, req.SeriesID, req.ChapterID),
	})
}

// requestBookPage answers MessageBookPageRequest: serve (or render) one
// page of the book session currently open, and prefetch its neighbours.
func (s *Service) requestBookPage(ctx context.Context, out Sender, req bookPageRequest) error {
	sess, _, ok := s.currentBookSession(req.key())
	if !ok {
		return s.sendError(out, "book_render_failed", "That book isn’t open any more.")
	}
	s.goBackground(ctx, func(ctx context.Context) {
		path, err := sess.Page(ctx, req.Index)
		if !s.bookSessionStillCurrent(sess) {
			return
		}
		if err != nil {
			_ = s.sendError(out, "book_render_failed",
				fmt.Sprintf("Quire could not render page %d: %s", req.Index+1, plain(err)))
			return
		}
		_ = send(out, appload.MessageBookPage, map[string]any{
			"sourceId": req.SourceID, "seriesId": req.SeriesID, "chapterId": req.ChapterID,
			"index": req.Index, "path": path,
		})
		sess.Prefetch(ctx, req.Index+1)
		sess.Prefetch(ctx, req.Index+2)
		sess.Prefetch(ctx, req.Index-1)
	})
	return nil
}

// setReaderSettings answers MessageSetReaderSettings: store the new global
// settings, re-lay the open book out, and re-anchor its reading position.
func (s *Service) setReaderSettings(ctx context.Context, out Sender, req setReaderSettingsRequest) error {
	if !req.Settings.Valid() {
		return s.sendError(out, "bad_request", "Quire does not recognise that reader setting.")
	}
	sess, _, ok := s.currentBookSession(req.key())
	if !ok {
		return s.sendError(out, "book_render_failed", "That book isn’t open any more.")
	}
	if s.store != nil {
		if err := s.store.SetReader(req.Settings); err != nil {
			s.log.Warn("could not remember the reader settings", "err", err)
		}
	}

	s.sendBookStatus(out, req.key(), "Laying out the book…")
	s.goBackground(ctx, func(ctx context.Context) {
		pageCount, page, toc, err := sess.SetSettings(ctx, req.Settings, req.Page)
		if !s.bookSessionStillCurrent(sess) {
			return
		}
		if err != nil {
			_ = s.sendError(out, "book_render_failed",
				fmt.Sprintf("Quire could not lay the book out: %s", plain(err)))
			return
		}
		_ = send(out, appload.MessageBookRelaid, map[string]any{
			"sourceId": req.SourceID, "seriesId": req.SeriesID, "chapterId": req.ChapterID,
			"pageCount": pageCount, "page": page, "toc": toc, "settings": req.Settings,
			"settingsNote": bookrender.SettingsNote, "settingsFields": bookrender.SettingsFields(),
		})
	})
	return nil
}

// closeBook answers MessageCloseBook: save the reading position (saved
// mode), end the session and remove its cache (and, in try mode, the
// fetched file) — the entire cleanup a book needs, the same shape
// MessageEndTry and MessageOpenSaved's siblings already take.
func (s *Service) closeBook(ctx context.Context, req closeBookRequest) error {
	sess, mode, ok := s.currentBookSession(req.key())
	if !ok {
		return nil
	}
	if mode == bookModeSaved && s.shelfStore != nil {
		if rec, ok := s.shelfStore.Get(shelf.Key(req.key())); ok {
			fraction, snippet, hash, err := sess.PositionFor(ctx, req.Page)
			if err != nil {
				s.log.Warn("could not compute a book's reading position", "err", err)
			} else {
				rec.Position = req.Page
				rec.PositionFraction = fraction
				rec.PositionSnippet = snippet
				rec.PositionLayout = hash
				rec.LastReadAt = s.now()
				if err := s.shelfStore.Put(rec); err != nil {
					s.log.Warn("could not remember a book's reading position", "err", err)
				}
			}
		}
	}
	s.endCurrentBookSession()
	return nil
}
