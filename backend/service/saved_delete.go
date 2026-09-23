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

	// VolumeLabel and ChapterIDs are the whole-volume delete: when ChapterIDs
	// is non-empty, ChapterID is ignored and every chapter it names is
	// deleted instead (the ones that are not actually saved are skipped
	// rather than treated as an error — the row asking for this only knows
	// what the volume contains, not which of its chapters Quire has).
	// VolumeLabel is only for the question and the "done" sentence; it names
	// nothing on its own.
	VolumeLabel string   `json:"volumeLabel,omitempty"`
	ChapterIDs  []string `json:"chapterIds,omitempty"`
}

func (r deleteSavedRequest) key() shelf.Key {
	return shelf.Key{Source: r.SourceID, Series: r.SeriesID, Chapter: r.ChapterID}
}

func (r deleteSavedRequest) valid() bool {
	if r.SourceID == "" || r.SeriesID == "" {
		return false
	}
	return r.ChapterID != "" || len(r.ChapterIDs) > 0
}

// isVolume reports whether this is a whole-volume delete rather than a
// single chapter.
func (r deleteSavedRequest) isVolume() bool { return len(r.ChapterIDs) > 0 }

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
	if req.isVolume() {
		return s.deleteSavedVolume(out, req)
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

// savedVolumeLabel is what a whole-volume delete's sentences call the
// volume: the label the row sent, or a plain "This volume" for a row that
// somehow had none — which should not happen, but the sentence still has to
// read as a sentence.
func savedVolumeLabel(label string) string {
	if l := strings.TrimSpace(label); l != "" {
		return l
	}
	return "This volume"
}

// savedVolumeDeleteQuestion is the confirm sentence for a whole-volume
// delete, singular-aware like savedDeleteQuestion.
func savedVolumeDeleteQuestion(label string, savedCount int) string {
	chapters := fmt.Sprintf("%d saved chapters", savedCount)
	if savedCount == 1 {
		chapters = "1 saved chapter"
	}
	return fmt.Sprintf("Delete %s from Quire? Its %s will need downloading again to read.",
		label, chapters)
}

// deleteSavedVolume answers a MessageDeleteSaved naming a whole volume's
// chapters: it deletes every one of them that is actually saved, through the
// same removeSavedChapter every single-chapter delete uses — there is no
// second removal path, only a loop over the one that already exists.
func (s *Service) deleteSavedVolume(out Sender, req deleteSavedRequest) error {
	label := savedVolumeLabel(req.VolumeLabel)

	// Which of the named chapters are actually saved — the volume row may
	// list chapters Quire never saved, and those are silently skipped rather
	// than treated as an error: the row only knows what the volume contains.
	var toDelete []string
	for _, id := range req.ChapterIDs {
		key := shelf.Key{Source: req.SourceID, Series: req.SeriesID, Chapter: id}
		if _, ok := s.shelfStore.Get(key); ok {
			toDelete = append(toDelete, id)
		}
	}
	if len(toDelete) == 0 {
		return s.sendError(out, "not_found", "Quire has none of that volume saved to delete.")
	}

	if !req.Confirmed {
		return send(out, appload.MessageSavedDeleted, map[string]any{
			"sourceId": req.SourceID, "seriesId": req.SeriesID,
			"phase":   savedDeletePhaseConfirm,
			"message": savedVolumeDeleteQuestion(label, len(toDelete)),
		})
	}

	var deleted []string
	var failed bool
	for _, id := range toDelete {
		if err := s.removeSavedChapter(req.SourceID, req.SeriesID, id); err != nil {
			s.log.Error("could not delete a saved chapter as part of a volume delete",
				"source", req.SourceID, "series", req.SeriesID, "chapter", id, "err", err)
			failed = true
			continue
		}
		deleted = append(deleted, id)
	}

	if failed && len(deleted) == 0 {
		return send(out, appload.MessageSavedDeleted, map[string]any{
			"sourceId": req.SourceID, "seriesId": req.SeriesID,
			"phase":   savedDeletePhaseFailed,
			"message": "Quire could not delete " + label + ", so it has left it as it was.",
		})
	}

	s.log.Info("a saved volume was deleted",
		"source", req.SourceID, "series", req.SeriesID, "chapters", len(deleted), "failed", failed)
	message := label + " is deleted."
	if failed {
		// A partial failure is still reported as done — most of the volume
		// really is gone — but the sentence says the rest did not go, rather
		// than claiming a clean sweep it did not make.
		message = label + " is mostly deleted, but some of its chapters could not be removed."
	}
	return send(out, appload.MessageSavedDeleted, map[string]any{
		"sourceId": req.SourceID, "seriesId": req.SeriesID,
		"phase":      savedDeletePhaseDone,
		"message":    message,
		"chapterIds": deleted,
	})
}

// removeSavedChapter deletes one saved chapter or book outright —
// os.RemoveAll, never the Trash, since there is no xochitl document here to
// put there — and drops its shelf record.
//
// A book (shelf.KindBook) is one file, saved at Record.File, and only that
// file is removed — not download.ChapterDir's whole per-page directory,
// which a book never has. Everything else is a chapter of page images, in
// download.ChapterDir(seriesDir, chapterID) exactly as before. Either way the
// path removed is built only from the saved root, safeSegment'd ids and (for
// a book) the record's own File — never trusted beyond being joined under
// root and checked — the same rule reclaim.go applies to the download cache,
// and for the same reason: only a path built and verified this way is
// provably inside the root this function is allowed to touch.
func (s *Service) removeSavedChapter(sourceID, seriesID, chapterID string) error {
	root, err := filepath.Abs(filepath.Clean(s.savedDir))
	if err != nil {
		return fmt.Errorf("resolving the saved directory: %w", err)
	}
	key := shelf.Key{Source: sourceID, Series: seriesID, Chapter: chapterID}

	rec, ok := s.shelfStore.Get(key)
	var prunable string // the directory to sweep upward from once the file/dir is gone
	if ok && rec.IsBook() && rec.File != "" {
		bookFile := filepath.Join(root, rec.File)
		if err := removeAllUnderRoot(root, bookFile); err != nil {
			return err
		}
		prunable = filepath.Dir(bookFile)
	} else {
		seriesDir := filepath.Join(root, safeSegment(sourceID), safeSegment(seriesID))
		chapterDir := download.ChapterDir(seriesDir, chapterID)
		if err := removeAllUnderRoot(root, chapterDir); err != nil {
			return err
		}
		prunable = seriesDir
	}

	if err := s.shelfStore.Remove(key); err != nil {
		return fmt.Errorf("forgetting the saved chapter: %w", err)
	}

	pruneEmpty(prunable, root)
	pruneEmpty(filepath.Dir(prunable), root)
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
