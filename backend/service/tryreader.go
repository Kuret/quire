// Try: reading a chapter in Quire's own reader without downloading it and
// without leaving a library entry behind (milestone 1).
//
// It reuses exactly the pipeline the cover cache and the download queue
// already prove out — theme.Fetcher through the source's own guarded policy,
// backend/imageproc to decode and downscale, a path handed to QML rather than
// bytes over the socket (PLAN §7.1) — over backend/tryreader's own,
// deliberately uncached-across-restarts directory instead of the download
// directory or the library.
//
// # Streaming, not batching
//
// The point of Try is to glance at a chapter before committing to a
// download, so the reader opens on page 1 the moment that one page is ready
// — it never waits for the rest of the chapter, and for most themes it never
// waits for the rest of the *page list* either. A theme that implements
// theme.FirstPageProber (doujinreader is the one that does today, because its
// family has no bulk page listing and Pages() costs one request per page) is
// asked for just the first page, and the full list is resolved in the
// background and merged in with Session.Extend once it lands — see
// startTry. Every other theme's Pages() is already one cheap request, so
// there is nothing to stream there: the whole list is known before the
// session opens, exactly as the download queue already treats it.
//
// # How far ahead this fetches
//
// One page: whatever is on screen, and the one behind it (requestTryPage's
// Prefetch call). That is deliberate, not a placeholder — see
// tryreader.Session.Prefetch's comment for why more would cost the device
// something Try's whole premise says it should not.
package service

import (
	"context"
	"fmt"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/tryreader"
)

// tryRequest is MessageTryChapter's and MessageEndTry's shared payload: which
// chapter a Try session is about.
type tryRequest struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
}

func (r tryRequest) key() tryKey {
	return tryKey{Source: r.SourceID, Series: r.SeriesID, Chapter: r.ChapterID}
}

// tryPageRequest is MessageTryPageRequest's payload.
type tryPageRequest struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
	Index     int    `json:"index"`
}

// tryReady is MessageTryReady's payload: the session has opened and its
// first page is on disk.
type tryReady struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`

	Index int    `json:"index"`
	Path  string `json:"path"`

	// PageCount is what is known so far — the full chapter for an ordinary
	// theme, or just the first page(s) for one that streams its list in
	// (theme.FirstPageProber). Complete says which.
	PageCount int  `json:"pageCount"`
	Complete  bool `json:"complete"`
}

// tryPage is MessageTryPage's payload: one page, ready.
type tryPage struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
	Index     int    `json:"index"`
	Path      string `json:"path"`
}

// tryPageCount is MessageTryPageCount's payload: the page count changed,
// because a theme's full list has resolved (or failed to) after the session
// opened on a fast first page alone. See startTry.
type tryPageCount struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`

	PageCount int  `json:"pageCount"`
	Complete  bool `json:"complete"`

	// Note explains an incomplete chapter — the full list failed to resolve
	// after the fast first page was already shown — in plain language
	// (PLAN §2). Empty on an ordinary completion.
	Note string `json:"note,omitempty"`
}

// startTry answers MessageTryChapter: it opens a Try session on one chapter,
// ending whichever was open before, and streams back the first page as soon
// as it exists rather than waiting on the rest of the chapter.
//
// It is honest about what it is not from the very first message: nothing
// here touches s.library or s.libStore, and end of story — MessageTryReady
// carries no documentUuid, because there is no document.
func (s *Service) startTry(ctx context.Context, out Sender, req tryRequest) error {
	if s.tryCache == nil {
		return s.sendError(out, "unavailable", "This build of Quire cannot open the Try reader.")
	}
	if req.SourceID == "" || req.SeriesID == "" || req.ChapterID == "" {
		return s.sendError(out, "bad_request", "Quire needs a source, a series and a chapter to try.")
	}

	th, src, err := s.themeFor(req.SourceID)
	if err != nil {
		return s.sendError(out, "not_found", plain(err))
	}

	// A book has no page images at all — see theme.FileTheme's own comment —
	// so there is nothing here to preview. The chapter list is expected to
	// have hidden the Try action already (kindBook, ChapterList.qml); this is
	// the backend saying no on its own account regardless, the same rule
	// askToConfirm and runDownload apply to a book's grouping questions.
	if _, ok := th.(theme.FileTheme); ok {
		return s.sendError(out, "try_unavailable",
			"This source's chapters are already finished files, so there is nothing to preview here — download it instead.")
	}

	s.goBackground(ctx, func(ctx context.Context) { s.runTry(ctx, out, req, th, src) })
	return nil
}

func (s *Service) runTry(ctx context.Context, out Sender, req tryRequest, th theme.Theme, src *theme.Source) {
	ref, err := theme.PageRefererFor(th, src, req.ChapterID)
	if err != nil {
		s.log.Warn("theme named an unusable page referer for Try",
			"source", src.ID, "theme", src.Theme, "chapter", req.ChapterID, "err", err)
	}

	// See the package comment: a theme that can name a first page cheaply is
	// asked for exactly that, and its full list is resolved afterwards rather
	// than up front.
	var (
		initial  []string
		complete bool
	)
	if fp, ok := th.(theme.FirstPageProber); ok {
		first, err := fp.FirstPage(ctx, src, req.ChapterID)
		if err != nil {
			_ = s.sendError(out, "try_failed", fmt.Sprintf("Quire could not open that chapter: %s", plain(err)))
			return
		}
		if len(first) == 0 {
			_ = s.sendError(out, "try_failed", "That chapter has no pages Quire can read.")
			return
		}
		initial, complete = first, false
	} else {
		pages, err := th.Pages(ctx, src, req.ChapterID)
		if err != nil {
			_ = s.sendError(out, "try_failed", fmt.Sprintf("Quire could not open that chapter: %s", plain(err)))
			return
		}
		if len(pages) == 0 {
			_ = s.sendError(out, "try_failed", "That chapter has no pages Quire can read.")
			return
		}
		initial, complete = pages, true
	}

	session := s.tryCache.NewSession(th, src, initial, ref)

	// Only one session at a time — the reader is one screen. Ending the
	// previous one here, rather than leaving it for the frontend's own
	// MessageEndTry, is what keeps a session that never got a clean close (a
	// crash, a killed connection) from surviving past the next chapter
	// someone opens: this app never accumulates more than one Try directory
	// while it is running, and Cache.New's sweep covers the case where it
	// does not run at all.
	s.tryMu.Lock()
	previous := s.trySession
	s.trySession = session
	s.tryKey = req.key()
	s.tryMu.Unlock()
	if previous != nil {
		if err := previous.End(); err != nil {
			s.log.Warn("could not remove the previous Try session's cache", "err", err)
		}
	}

	path, err := session.Page(ctx, 0)
	if err != nil {
		_ = s.sendError(out, "try_failed", fmt.Sprintf("Quire could not load the first page: %s", plain(err)))
		s.dropTrySession(session)
		return
	}
	_ = send(out, appload.MessageTryReady, tryReady{
		SourceID: req.SourceID, SeriesID: req.SeriesID, ChapterID: req.ChapterID,
		Index: 0, Path: path,
		PageCount: session.PageCount(), Complete: complete,
	})
	session.Prefetch(ctx, 1)

	if complete {
		return
	}

	// The full list, resolved behind the fast first page. Nothing about the
	// reader waits on this: it only ever changes what MessageTryPageCount
	// reports next.
	pages, err := th.Pages(ctx, src, req.ChapterID)
	if !s.trySessionStillCurrent(session) {
		// The user has since moved on to another chapter, or closed the
		// reader; this list is no longer about anything on screen.
		return
	}
	if err != nil {
		_ = send(out, appload.MessageTryPageCount, tryPageCount{
			SourceID: req.SourceID, SeriesID: req.SeriesID, ChapterID: req.ChapterID,
			PageCount: session.PageCount(), Complete: false,
			Note: fmt.Sprintf("Quire could only open the first page of this chapter: %s. "+
				"The rest may be missing.", plain(err)),
		})
		return
	}
	if !session.Extend(pages) {
		s.log.Warn("a theme's FirstPage and Pages disagreed", "source", src.ID, "chapter", req.ChapterID)
		return
	}
	_ = send(out, appload.MessageTryPageCount, tryPageCount{
		SourceID: req.SourceID, SeriesID: req.SeriesID, ChapterID: req.ChapterID,
		PageCount: session.PageCount(), Complete: true,
	})
}

// requestTryPage answers MessageTryPageRequest: fetch (or serve from cache)
// one page of the session currently open, and prefetch the one page beyond
// it.
//
// A page not ready yet is not an error: the frontend is expected to say so
// on screen and wait, exactly as it would for a download in progress, rather
// than freezing or showing a blank page as though it were content (see
// ui/TryReader.qml).
func (s *Service) requestTryPage(ctx context.Context, out Sender, req tryPageRequest) error {
	session, ok := s.currentTrySession(tryKey{Source: req.SourceID, Series: req.SeriesID, Chapter: req.ChapterID})
	if !ok {
		return s.sendError(out, "try_unavailable", "That preview isn’t open any more.")
	}

	s.goBackground(ctx, func(ctx context.Context) {
		path, err := session.Page(ctx, req.Index)
		if !s.trySessionStillCurrent(session) {
			return
		}
		if err != nil {
			_ = s.sendError(out, "try_failed", fmt.Sprintf("Quire could not load page %d: %s", req.Index+1, plain(err)))
			return
		}
		_ = send(out, appload.MessageTryPage, tryPage{
			SourceID: req.SourceID, SeriesID: req.SeriesID, ChapterID: req.ChapterID,
			Index: req.Index, Path: path,
		})
		session.Prefetch(ctx, req.Index+1)
	})
	return nil
}

// endTry answers MessageEndTry: it discards the session's cache directory,
// which is the entire cleanup Try needs — there is no reading position, no
// record and no library entry to also undo (PLAN's Try milestone).
//
// It only ends the session actually named. A stale MessageEndTry arriving
// after the reader already moved on to a different chapter — the close
// signal and the next chapter's open crossing on the wire — must not delete
// the session the user is now looking at.
func (s *Service) endTry(req tryRequest) error {
	s.tryMu.Lock()
	var ended *tryreader.Session
	if s.trySession != nil && s.tryKey == req.key() {
		ended = s.trySession
		s.trySession = nil
		s.tryKey = tryKey{}
	}
	s.tryMu.Unlock()
	if ended == nil {
		return nil
	}
	if err := ended.End(); err != nil {
		s.log.Warn("could not remove a Try session's cache", "err", err)
	}
	return nil
}

// currentTrySession returns the session open for key, if any is.
func (s *Service) currentTrySession(key tryKey) (*tryreader.Session, bool) {
	s.tryMu.Lock()
	defer s.tryMu.Unlock()
	if s.trySession == nil || s.tryKey != key {
		return nil, false
	}
	return s.trySession, true
}

// trySessionStillCurrent reports whether session is still the one Try has
// open — false once the user has closed it or moved on to another chapter,
// which is the caller's cue to say nothing rather than report on a chapter
// nobody is looking at any more.
func (s *Service) trySessionStillCurrent(session *tryreader.Session) bool {
	s.tryMu.Lock()
	defer s.tryMu.Unlock()
	return s.trySession == session
}

// dropTrySession clears the current session if it is still session, without
// removing its files — used when opening the very first page failed, so
// there is nothing on disk this needs to clean up beyond what never got
// written.
func (s *Service) dropTrySession(session *tryreader.Session) {
	s.tryMu.Lock()
	defer s.tryMu.Unlock()
	if s.trySession == session {
		s.trySession = nil
		s.tryKey = tryKey{}
	}
}
