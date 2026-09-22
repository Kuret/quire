package service

import "github.com/rickl/quire/backend/shelf"

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
	rec.Position = clampPosition(req.Position, len(rec.Pages))
	if err := s.shelfStore.Put(rec); err != nil {
		s.log.Warn("could not remember a saved chapter's reading position",
			"source", req.SourceID, "series", req.SeriesID, "chapter", req.ChapterID, "err", err)
	}
	return nil
}
