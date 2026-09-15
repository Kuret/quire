package service

import (
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/theme"
)

// openRequest is the MessageOpenInReader payload.
//
// The opening itself happens in QML, not here — an AppLoad app's QML runs
// inside xochitl's own engine, so it can call xochitl's singletons directly and
// the backend has no way to (PLAN §6 M6). This message exists so the backend
// learns the outcome, which is the half it *can* act on: a UUID that no longer
// resolves is a document the user deleted, and the record of it is now a lie.
type openRequest struct {
	DocumentUUID string `json:"documentUuid"`

	// Missing is set by the frontend when Library.entryForId returned null.
	Missing bool `json:"missing,omitempty"`
}

// ReaderGoneRemedy is what the user is told when the document behind a "Read"
// button is not on the tablet any more.
//
// It offers the one thing that fixes it rather than reporting a failure: PLAN
// §6 M7 wants a deleted volume detected and offered as a re-download, and this
// is where that detection lands.
const ReaderGoneRemedy = "That volume isn’t on your reMarkable any more — it looks like it was " +
	"deleted from the tablet. Quire has forgotten it, so you can download it again."

// openInReader records the outcome of a handoff.
func (s *Service) openInReader(out Sender, req openRequest) error {
	if req.DocumentUUID == "" {
		return s.sendError(out, "bad_request", "Quire was asked to open nothing.")
	}
	if !req.Missing {
		s.log.Info("opened in the stock reader", "document", req.DocumentUUID)
		return nil
	}

	// The document is gone. Drop the record so the row goes back to offering a
	// download, and so nothing else keeps pointing at a UUID that opens
	// nothing.
	if s.libStore != nil {
		for _, rec := range s.libStore.List() {
			if rec.DocumentUUID != req.DocumentUUID {
				continue
			}
			if err := s.libStore.Remove(rec.Key); err != nil {
				s.log.Error("could not forget a deleted volume", "uuid", req.DocumentUUID, "err", err)
			}
			break
		}
	}
	s.log.Info("a stored volume is no longer on the tablet", "document", req.DocumentUUID)
	return s.sendError(out, "document_gone", ReaderGoneRemedy)
}

// storedVolumes maps every chapter of a series to the volume record that
// contains it, so the chapter list can offer "Read" on rows whose volume is
// already downloaded.
//
// It groups the chapters exactly as runDownload does, because a record is keyed
// by volume label and the only way back from a label to a chapter is to redo
// the grouping. Two different groupings would put the Read button on the wrong
// rows, which is why volumeContaining and this share one function.
func (s *Service) storedVolumes(sourceID, seriesID, seriesTitle string,
	chapters []theme.Chapter) map[string]library.Record {

	out := map[string]library.Record{}
	if s.libStore == nil {
		return out
	}
	for _, vol := range groupVolumes(seriesTitle, chapters) {
		rec, ok := s.libStore.Get(library.Key{Source: sourceID, Series: seriesID, Volume: vol.Label})
		if !ok {
			continue
		}
		for _, c := range vol.Chapters {
			out[c.ID] = rec
		}
	}
	return out
}
