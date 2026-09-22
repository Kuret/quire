// Saved in Quire: reading, remembering a place in, and deleting a chapter
// that lives in Quire's own storage rather than the reMarkable library — see
// backend/service/download.go's runDownload for how a chapter gets there in
// the first place, and backend/shelf for the index this file reads and
// writes.
//
// It is written the way backend/service/tryreader.go is, and the difference
// between them is the whole reason "saved" is not simply "Try, but kept": a
// saved chapter's pages are all on disk before the reader opens it, so there
// is no MessageTryPageRequest traffic here and nothing to stream — every page
// path is handed over in the one MessageSavedOpened reply — and a saved
// chapter remembers a reading position, which Try never does because Try
// leaves nothing behind to remember it in.
package service

import (
	"os"
	"path/filepath"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
)

// savedRequest is shared by MessageOpenSaved's payload and the key half of
// MessageSavePosition and MessageDeleteSaved.
type savedRequest struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
}

func (r savedRequest) key() shelf.Key {
	return shelf.Key{Source: r.SourceID, Series: r.SeriesID, Chapter: r.ChapterID}
}

func (r savedRequest) valid() bool {
	return r.SourceID != "" && r.SeriesID != "" && r.ChapterID != ""
}

// SavedMissingRemedy is what OpenSaved answers when the chapter is not saved,
// or its files are gone from disk.
//
// Both cases read the same to the user: whatever they expected to be able to
// read from Quire's own storage is not there, and the only way back is to
// download it again. Distinguishing "never saved" from "saved and then lost"
// would be truthful about an implementation detail nobody asked about; what
// they need is the next step.
const SavedMissingRemedy = "Quire could not find that chapter's saved pages. " +
	"It will need downloading again to read."

// openSaved answers MessageOpenSaved: hand back every page path of a saved
// chapter, all at once, because unlike Try every one of them is already on
// disk.
//
// A chapter whose files are gone — the saved directory tampered with by hand,
// or a page removed outside Quire — is treated exactly like one that was
// never saved: the record is dropped, so the next look does not repeat the
// same failed promise, and the same plain sentence is sent either way.
func (s *Service) openSaved(out Sender, req savedRequest) error {
	if s.shelfStore == nil || s.savedDir == "" {
		return s.sendError(out, "unavailable", "This build of Quire has nothing saved to open.")
	}
	if !req.valid() {
		return s.sendError(out, "bad_request", "Quire needs a source, a series and a chapter to open.")
	}

	rec, ok := s.shelfStore.Get(req.key())
	if !ok {
		return s.sendError(out, "saved_missing", SavedMissingRemedy)
	}

	root, err := filepath.Abs(filepath.Clean(s.savedDir))
	if err != nil {
		s.log.Warn("could not resolve the saved directory", "dir", s.savedDir, "err", err)
		return s.sendError(out, "saved_missing", SavedMissingRemedy)
	}

	paths := make([]string, 0, len(rec.Pages))
	for _, p := range rec.Pages {
		full := filepath.Join(root, p)
		if _, err := os.Stat(full); err != nil {
			s.log.Warn("a saved chapter's pages are missing on disk; forgetting it",
				"source", req.SourceID, "series", req.SeriesID, "chapter", req.ChapterID, "page", p)
			if err := s.shelfStore.Remove(req.key()); err != nil {
				s.log.Error("could not drop a saved chapter with missing files", "err", err)
			}
			return s.sendError(out, "saved_missing", SavedMissingRemedy)
		}
		paths = append(paths, full)
	}
	if len(paths) == 0 {
		_ = s.shelfStore.Remove(req.key())
		return s.sendError(out, "saved_missing", SavedMissingRemedy)
	}

	position := clampPosition(rec.Position, len(paths))

	return send(out, appload.MessageSavedOpened, map[string]any{
		"sourceId":     req.SourceID,
		"seriesId":     req.SeriesID,
		"chapterId":    req.ChapterID,
		"seriesTitle":  rec.SeriesTitle,
		"chapterTitle": rec.ChapterTitle,
		"pages":        paths,
		"position":     position,
	})
}

// clampPosition keeps a page index inside [0, pageCount) — never negative,
// never past the last page, so a record whose page count shrank (or a
// position saved by a build with a bug) cannot ask the reader to open past
// the end of the chapter.
func clampPosition(position, pageCount int) int {
	if pageCount <= 0 {
		return 0
	}
	if position < 0 {
		return 0
	}
	if position >= pageCount {
		return pageCount - 1
	}
	return position
}

