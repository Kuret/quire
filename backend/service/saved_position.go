package service

import (
	"context"

	"github.com/rickl/quire/backend/shelf"
)

// savePositionRequest is MessageSavePosition's payload: no reply, the same
// shape as tryreader.go's page traffic in what it does not send back.
type savePositionRequest struct {
	SourceID  string `json:"sourceId"`
	SeriesID  string `json:"seriesId"`
	ChapterID string `json:"chapterId"`
	Position  int    `json:"position"`
}

func (r savePositionRequest) key() shelf.Key {
	return shelf.Key{Source: r.SourceID, Series: r.SeriesID, Chapter: r.ChapterID}
}

// savePosition answers MessageSavePosition: remember where the reader is in a
// saved chapter, clamped to the page range exactly as OpenSaved's own answer
// is.
//
// It has no reply. The reader already knows what page it is showing — it is
// the one who sent this — so echoing the position back would only be telling
// it something it just said. Try never sends this at all: there is no Try
// record to remember a place in (see backend/service/tryreader.go).
func (s *Service) savePosition(req savePositionRequest) error {
	if s.shelfStore == nil {
		return nil
	}
	rec, ok := s.shelfStore.Get(req.key())
	if !ok {
		// A stale send from a reader that has since been told the chapter is
		// gone (see openSaved). Nothing to save a position in any more.
		return nil
	}

	if rec.IsBook() {
		// A book's position is a page number too, but what it means depends
		// on the layout it was recorded under (books-contract.md §B); the
		// fraction and snippet only mean anything while the book session
		// that page number came from is still the one open, so this is a
		// no-op — not an error — for a stale send naming a book that is no
		// longer the open one. See closeBook for where a book's position is
		// actually computed and saved; MessageSavePosition on a book both
		// exist so the reader's own debounced page-turn save keeps working
		// unchanged (it does not know or care what kind it is reading).
		if sess, mode, ok := s.currentBookSession(bookKey(req.key())); ok && mode == bookModeSaved {
			fraction, snippet, hash, err := sess.PositionFor(context.Background(), req.Position)
			if err != nil {
				s.log.Warn("could not compute a book's reading position", "err", err)
				return nil
			}
			rec.Position = req.Position
			rec.PositionFraction = fraction
			rec.PositionSnippet = snippet
			rec.PositionLayout = hash
			if err := s.shelfStore.Put(rec); err != nil {
				s.log.Warn("could not remember a book's reading position", "err", err)
			}
		}
		return nil
	}

	rec.Position = clampPosition(req.Position, len(rec.Pages))
	if err := s.shelfStore.Put(rec); err != nil {
		s.log.Warn("could not remember a saved chapter's reading position",
			"source", req.SourceID, "series", req.SeriesID, "chapter", req.ChapterID, "err", err)
	}
	return nil
}
