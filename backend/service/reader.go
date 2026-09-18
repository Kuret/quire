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

	// Detail is why it did not open, when the cause was not simply a UUID that
	// no longer resolves — a bridge that is not there, or a call that threw.
	// It changes nothing here; it is how a device-side failure reaches this
	// log at all (see ui/Answers.js).
	Detail string `json:"detail,omitempty"`
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

	if req.Detail != "" {
		s.log.Warn("the reader handoff did not happen",
			"document", req.DocumentUUID, "detail", req.Detail)
	}

	// The document is gone. Drop the record so the row goes back to offering a
	// download, and so nothing else keeps pointing at a UUID that opens
	// nothing — and take the cached pages with it, exactly as deleting from
	// inside Quire does (PLAN §12.4).
	//
	// **Reclaiming here is not a tidiness nicety.** Once the record is gone,
	// no later delete can ever name those chapters again: they are orphaned
	// for good, and only the cache control can reach them. This is the same
	// orphan class, created by the user deleting the document on the tablet
	// instead of through Quire.
	if s.libStore != nil {
		for _, rec := range s.libStore.List() {
			if rec.DocumentUUID != req.DocumentUUID {
				continue
			}
			if err := s.libStore.Remove(rec.Key); err != nil {
				s.log.Error("could not forget a deleted volume", "uuid", req.DocumentUUID, "err", err)
				break
			}
			// After the removal and against the records that remain, so a
			// chapter another document still holds survives. Same ordering and
			// same reasoning as deleteDownload.
			freed := s.reclaimPages(rec, s.libStore.List())
			s.log.Info("reclaimed the pages of a volume deleted on the tablet",
				"document", req.DocumentUUID, "freedBytes", freed, "freedMiB", freed>>20)
			break
		}
	}
	s.log.Info("a stored volume is no longer on the tablet", "document", req.DocumentUUID)
	return s.sendError(out, "document_gone", ReaderGoneRemedy)
}

// storedVolumes maps every chapter of a series to the document that holds it,
// so the chapter list can offer "Read" on rows whose volume is already
// downloaded.
//
// It reads the chapter IDs recorded with each document rather than re-deriving
// the grouping. That matters since volumes split to fit xochitl's upload cap:
// the split depends on the byte sizes of page images that may since have been
// deleted, so the decision is not reproducible and must be remembered instead.
//
// Records written before chapter IDs were kept fall back to redoing the
// grouping, which is right for them because nothing was ever split back then.
//
// **That fallback redoes it the way it was done then, not the way it is done
// now** — see legacyGrouping. PLAN §6 M4 was reversed on 2026-09-16 and one PDF
// per chapter became the default, and revised again the same day so that
// volumes are a choice made at download time; but a record already in the
// user's library was written when the source's volume labels decided the
// grouping on their own, so re-deriving it with today's rules would look for
// "Vol 3" among volumes labelled "12", "12.5", "13", find nothing, and quietly
// stop offering Read on a volume that is sitting on the tablet. Changing how
// grouping is chosen must not orphan what is already downloaded, and this is
// the one place where it silently could.
func (s *Service) storedVolumes(sourceID, seriesID, seriesTitle string,
	chapters []theme.Chapter) map[string]library.Record {

	out := map[string]library.Record{}
	if s.libStore == nil {
		return out
	}

	var legacy []library.Record
	for _, rec := range s.libStore.List() {
		if rec.Source != sourceID || rec.Series != seriesID {
			continue
		}
		if len(rec.Chapters) == 0 {
			legacy = append(legacy, rec)
			continue
		}
		for _, id := range rec.Chapters {
			// First part wins. A chapter cut across two documents starts in
			// the earlier one, which is where a reader opening it wants to be.
			if prev, ok := out[id]; ok && prev.Part != 0 && prev.Part <= rec.Part {
				continue
			}
			out[id] = rec
		}
	}

	if len(legacy) > 0 {
		byLabel := map[string]library.Record{}
		for _, rec := range legacy {
			byLabel[rec.Volume] = rec
		}
		for _, vol := range legacyGrouping(seriesTitle, chapters) {
			rec, ok := byLabel[vol.Label]
			if !ok {
				continue
			}
			for _, c := range vol.Chapters {
				if _, taken := out[c.ID]; !taken {
					out[c.ID] = rec
				}
			}
		}
	}
	return out
}
