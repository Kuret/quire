package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/shelf"
)

// The phases MessageSavedDeleted answers with. Unlike the library delete
// (delete.go), there is no Trash to land in and nothing left half-done: a
// saved chapter is either still there or os.RemoveAll'd outright, so "done"
// is the only success there is.
const (
	savedDeletePhaseConfirm = "confirm"
	savedDeletePhaseDone    = "done"
	savedDeletePhaseFailed  = "failed"
)

// deleteSavedRequest is the MessageDeleteSaved payload.
type deleteSavedRequest struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`

	// Confirmed is false on the first message, which is the frontend asking
	// what to put in front of the user — the same two-step shape
	// MessageDeleteDownload uses, and for the same reason (PLAN §2: the
	// sentence is the backend's).
	Confirmed bool `json:"confirmed,omitempty"`
}

func (r deleteSavedRequest) key() shelf.Key {
	return shelf.Key{Source: r.SourceID, Series: r.SeriesID, Chapter: r.ChapterID}
}

func (r deleteSavedRequest) valid() bool {
	return r.SourceID != "" && r.SeriesID != "" && r.ChapterID != ""
}

// savedChapterLabel is a saved chapter's name for a sentence: its own title
// when the source gave one, else "Chapter <number>", else a plain "That
// chapter" for a record with neither — which should not happen for anything
// this package wrote itself, but a record is user-visible state and can
// outlive the code that wrote it.
func savedChapterLabel(rec shelf.Record) string {
	if t := strings.TrimSpace(rec.ChapterTitle); t != "" {
		return t
	}
	if rec.Number != 0 {
		return "Chapter " + strconv.FormatFloat(rec.Number, 'f', -1, 64)
	}
	return "That chapter"
}

// savedDeleteQuestion is the sentence the confirm strip asks.
//
// It says what deleting costs — a re-download — because that is the one fact
// that decides whether this is worth doing: unlike a library delete there is
// no Trash to recover from, so the honest cost is "fetch it again", not
// "gone for good" (which is also true, but says nothing about what comes
// next).
func savedDeleteQuestion(label string) string {
	return "Delete " + label + " from Quire? It will need downloading again to read."
}

// deleteSaved answers MessageDeleteSaved: ask, or act on the answer.
func (s *Service) deleteSaved(out Sender, req deleteSavedRequest) error {
	if s.shelfStore == nil || s.savedDir == "" {
		return s.sendError(out, "unavailable", "This build of Quire has nothing saved to delete.")
	}
	if !req.valid() {
		return s.sendError(out, "bad_request", "Quire needs a source, a series and a chapter to delete.")
	}

	rec, ok := s.shelfStore.Get(req.key())
	if !ok {
		return s.sendError(out, "not_found", "Quire has no saved chapter to delete.")
	}

	if !req.Confirmed {
		// Nothing is touched yet — an accidental tap gets no further.
		return send(out, appload.MessageSavedDeleted, map[string]any{
			"sourceId": req.SourceID, "seriesId": req.SeriesID, "chapterId": req.ChapterID,
			"phase":   savedDeletePhaseConfirm,
			"message": savedDeleteQuestion(savedChapterLabel(rec)),
		})
	}

	if err := s.removeSavedChapter(req.SourceID, req.SeriesID, req.ChapterID); err != nil {
		s.log.Error("could not delete a saved chapter",
			"source", req.SourceID, "series", req.SeriesID, "chapter", req.ChapterID, "err", err)
		return send(out, appload.MessageSavedDeleted, map[string]any{
			"sourceId": req.SourceID, "seriesId": req.SeriesID, "chapterId": req.ChapterID,
			"phase":   savedDeletePhaseFailed,
			"message": "Quire could not delete that chapter, so it has left it as it was.",
		})
	}

	s.log.Info("a saved chapter was deleted",
		"source", req.SourceID, "series", req.SeriesID, "chapter", req.ChapterID)
	return send(out, appload.MessageSavedDeleted, map[string]any{
		"sourceId": req.SourceID, "seriesId": req.SeriesID, "chapterId": req.ChapterID,
		"phase":   savedDeletePhaseDone,
		"message": savedChapterLabel(rec) + " is deleted.",
	})
}

// removeSavedChapter deletes one chapter's saved files outright — os.RemoveAll,
// never the Trash, since there is no xochitl document here to put there — and
// drops its shelf record.
//
// The directory is built only from the saved root, safeSegment'd ids and
// download.ChapterDir, never from anything stored on the record: the same
// rule reclaim.go applies to the download cache, and for the same reason —
// only a path built this way is provably inside the root this function is
// allowed to touch.
func (s *Service) removeSavedChapter(sourceID, seriesID, chapterID string) error {
	root, err := filepath.Abs(filepath.Clean(s.savedDir))
	if err != nil {
		return fmt.Errorf("resolving the saved directory: %w", err)
	}
	seriesDir := filepath.Join(root, safeSegment(sourceID), safeSegment(seriesID))
	chapterDir := download.ChapterDir(seriesDir, chapterID)

	if err := removeAllUnderRoot(root, chapterDir); err != nil {
		return err
	}

	key := shelf.Key{Source: sourceID, Series: seriesID, Chapter: chapterID}
	if err := s.shelfStore.Remove(key); err != nil {
		return fmt.Errorf("forgetting the saved chapter: %w", err)
	}

	pruneEmpty(seriesDir, root)
	pruneEmpty(filepath.Dir(seriesDir), root)
	return nil
}

// deleteSavedChaptersForSource removes every chapter saved in Quire for one
// source — used when the source itself is removed (see MessageRemoveSource):
// unlike a document already in the reMarkable library, which is the user's
// and is left alone, a saved chapter has no existence apart from Quire, so
// nothing is worth keeping once the source that named it is gone.
func (s *Service) deleteSavedChaptersForSource(sourceID string) {
	if s.shelfStore == nil {
		return
	}
	for _, rec := range s.shelfStore.List() {
		if rec.Source != sourceID {
			continue
		}
		if err := s.removeSavedChapter(rec.Source, rec.Series, rec.Chapter); err != nil {
			s.log.Error("could not delete a saved chapter for a removed source",
				"source", rec.Source, "series", rec.Series, "chapter", rec.Chapter, "err", err)
		}
	}
}

// removeAllUnderRoot is os.RemoveAll, refusing anything that does not resolve
// inside root — the same check backend/service/reclaim.go's removeUnderRoot
// makes, duplicated rather than shared because that one also measures and
// returns the bytes freed, which nothing here needs and would only be a
// reason to get the two confused.
func removeAllUnderRoot(root, path string) error {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("resolving %q: %w", path, err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove %q: it is outside %q", abs, root)
	}
	return os.RemoveAll(abs)
}
