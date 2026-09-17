package service

import (
	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
)

// deleteRequest is the MessageDeleteDownload payload.
//
// The deletion itself happens in QML (PLAN §12.4): only an AppLoad app's QML can
// reach xochitl's tree explorer, and the backend has no route to it — §6 M5
// established that the web interface has no delete at all. So the frontend acts
// and then reports, exactly as it does for the reader handoff, and the backend
// does the half it owns.
type deleteRequest struct {
	DocumentUUID string `json:"documentUuid"`

	// Confirmed is false on the first message, which is the frontend asking
	// *what to ask the user*. It mirrors MessageEnqueueDownload: the question
	// is a sentence, and PLAN §2 keeps every sentence in the backend — this is
	// also the only side that knows what the document is called on the tablet.
	Confirmed bool `json:"confirmed,omitempty"`

	// Trashed is what the frontend observed, not what it intended. False means
	// the document is still on the tablet.
	Trashed bool `json:"trashed,omitempty"`

	// Removed says the document was then deleted out of the Trash, which is
	// what makes the delete permanent (PLAN §12.4). It is reported separately
	// from Trashed because the two fail separately: a document in the Trash is
	// deleted as far as the user's library is concerned, whether or not the
	// second step that should have followed it worked.
	//
	// It used to be `emptied`, when the second step was emptying the whole
	// Trash. It is now `LibraryController.deleteEntries` on that one entry —
	// measured 2026-09-17, with a control folder sitting in the Trash beside
	// two that were deleted and surviving both.
	Removed bool `json:"removed,omitempty"`
}

// DeleteFailedRemedy is what the user is told when the trash call did not take.
//
// It says the download is untouched, because that is the state they are in and
// it is the one that decides what to do next: nothing has been lost, and the
// row will still say "Read".
const DeleteFailedRemedy = "Quire could not remove that document from your reMarkable, so it has left " +
	"everything as it was. The download is still there."

// DeleteNotForgottenRemedy covers the narrow case where the document went but
// Quire could not update its own record of it.
//
// Saying so matters more than it looks: the row will still offer "Read" on a
// document that is not there any more, and a user who knows why can put it
// right by leaving Quire to notice on the next tap (§6 M6 already handles a
// UUID that no longer resolves).
//
// It does not mention the Trash. On this path the delete itself worked, so the
// document is gone rather than sitting somewhere it could be fetched back from.
// Only DeleteLeftInTrashNote may talk about the Trash, because that is the one
// path where the document is in it.
const DeleteNotForgottenRemedy = "That download is deleted, but Quire could not update its own record of " +
	"it. Tapping Read will sort itself out."

// DeleteUnknownRemedy answers a delete for a document Quire has no record of.
//
// It can happen honestly — a record dropped by the reader handoff a moment ago,
// a list on screen older than the store — so it is phrased as the state of the
// world rather than as the user having done something wrong.
const DeleteUnknownRemedy = "Quire has no record of that download any more, so there is nothing for it " +
	"to delete."

// DeleteLeftInTrashNote is said when the document reached the Trash but the
// second step, which removes it from there, did not take.
//
// It is not phrased as a failure, because the delete was not one: the download
// is out of the library and the record is gone. What it corrects is the state
// of the Trash — the confirmation said the download would be gone for good, and
// instead it is recoverable, which is a difference the user can act on. So it
// says where the document is and who can finish the job, rather than implying
// something went wrong with their download.
//
// It is the one sentence in this file allowed to mention the Trash.
const DeleteLeftInTrashNote = "That download is deleted and out of your library, but Quire could not " +
	"remove it from your reMarkable’s Trash, so it is sitting in there. Emptying the Trash on the tablet " +
	"will finish it off."

// deleteQuestion is the sentence the confirm strip asks.
//
// It names the document exactly as the tablet does, which is the whole point:
// for a volume split across the upload cap the row's title is the chapter range
// and the *document* is "… (part 2 of 3)". Naming the file is what makes it
// legible that one part is going and the others are staying.
//
// **It says "for good", and it says nothing else is touched.** Deleting is now
// two steps on that one document — into the Trash, then out of it with
// `LibraryController.deleteEntries` — and neither touches anything else. The
// old wording had to warn that the whole Trash went with it; that warning is
// gone because the behaviour it described is gone, measured on hardware
// 2026-09-17 with a control folder left sitting in the Trash beside two
// deletions and surviving both.
//
// The second half of the sentence is not padding. A user who has read the old
// warning once will assume it still applies, and the Trash is exactly where
// people keep things they have not decided about yet.
func deleteQuestion(name string) string {
	if name == "" {
		// A record from before names were kept. "That download" is vague, but
		// it is not wrong, and inventing a name would be.
		return "Delete that download from your reMarkable for good? Nothing else is touched, and the rest " +
			"of your Trash is left alone."
	}
	return "Delete “" + name + "” from your reMarkable for good? Nothing else is touched, and the rest " +
		"of your Trash is left alone."
}

// deleteDownload forgets a document the frontend has moved to xochitl's Trash,
// and reclaims the PDF that was assembled to make it.
//
// The order is deliberate. The record is dropped only on a *reported success*:
// a record dropped for a document still sitting on the tablet is an orphan the
// user cannot get rid of through Quire and Quire can no longer account for,
// which is a worse failure than the delete not happening at all.
func (s *Service) deleteDownload(out Sender, req deleteRequest) error {
	if req.DocumentUUID == "" {
		return s.sendError(out, "bad_request", "Quire was asked to delete nothing.")
	}

	if !req.Confirmed {
		// Step one: hand back the question. Nothing is touched, and an
		// accidental tap can get no further than this.
		rec, ok := s.recordFor(req.DocumentUUID)
		if !ok {
			return s.sendError(out, "not_found", DeleteUnknownRemedy)
		}
		return send(out, appload.MessageDeleteConfirm, map[string]any{
			"documentUuid": req.DocumentUUID,
			"message":      deleteQuestion(rec.VisibleName),
		})
	}

	if !req.Trashed {
		// The frontend tried and could not. Nothing here changes; the row keeps
		// saying "Read", which is true.
		s.log.Warn("the frontend could not trash a document", "document", req.DocumentUUID)
		return s.sendError(out, "not_deleted", DeleteFailedRemedy)
	}

	if s.libStore == nil {
		// No store to forget it from. The document is gone all the same, so
		// this is not an error to put in front of anyone.
		s.log.Info("a document was trashed with no library store to update", "document", req.DocumentUUID)
		return send(out, appload.MessageDownloadDeleted, map[string]any{"documentUuid": req.DocumentUUID})
	}

	for _, rec := range s.libStore.List() {
		if rec.DocumentUUID != req.DocumentUUID {
			continue
		}

		if err := s.libStore.Remove(rec.Key); err != nil {
			s.log.Error("could not forget a trashed volume", "uuid", req.DocumentUUID, "err", err)
			return s.sendError(out, "not_forgotten", DeleteNotForgottenRemedy)
		}

		// The PDF, its manifest and the cached pages behind them all go (PLAN
		// §12.4). Read *after* the record is removed, so "is any other record
		// still using this chapter?" is asked of the records that are staying —
		// asking before would find this one and keep everything it named.
		freed := s.reclaimPages(rec, s.libStore.List())

		s.log.Info("a downloaded volume was deleted from the reMarkable",
			"document", req.DocumentUUID, "name", rec.VisibleName, "removedFromTrash", req.Removed,
			"chapters", len(rec.Chapters), "pagesFreedBytes", freed,
			"pagesFreedMiB", freed>>20)
		break
	}

	// Sent even when no record matched: the frontend asked for a row to stop
	// saying "Read", the document really is gone, and a silent reply would
	// leave the row offering to open something that is not there.
	if err := send(out, appload.MessageDownloadDeleted,
		map[string]any{"documentUuid": req.DocumentUUID}); err != nil {
		return err
	}

	if !req.Removed {
		// The delete stands; only the promise that it was permanent did not.
		// Said after the row has already been put right, so the screen shows a
		// finished delete with a note about it rather than a failure.
		s.log.Warn("a trashed document was not removed from the Trash", "document", req.DocumentUUID)
		return s.sendError(out, "left_in_trash", DeleteLeftInTrashNote)
	}
	return nil
}

// recordFor finds the stored record for a document UUID.
func (s *Service) recordFor(uuid string) (library.Record, bool) {
	if s.libStore == nil {
		return library.Record{}, false
	}
	for _, rec := range s.libStore.List() {
		if rec.DocumentUUID == uuid {
			return rec, true
		}
	}
	return library.Record{}, false
}
