package service

import (
	"errors"
	"io/fs"
	"os"

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

	// Emptied says the Trash was emptied afterwards, which is what makes the
	// delete permanent (PLAN §12.4). It is reported separately from Trashed
	// because the two fail separately: a document in the Trash is deleted as
	// far as the user's library is concerned, whether or not the emptying that
	// should have followed it worked.
	Emptied bool `json:"emptied,omitempty"`
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
// document that is now in the Trash, and a user who knows why can put it right
// by restoring it or by leaving Quire to notice on the next tap (§6 M6 already
// handles a UUID that no longer resolves).
const DeleteNotForgottenRemedy = "That document is in your reMarkable's Trash, but Quire could not update " +
	"its own record of it. Tapping Read will sort itself out."

// DeleteUnknownRemedy answers a delete for a document Quire has no record of.
//
// It can happen honestly — a record dropped by the reader handoff a moment ago,
// a list on screen older than the store — so it is phrased as the state of the
// world rather than as the user having done something wrong.
const DeleteUnknownRemedy = "Quire has no record of that download any more, so there is nothing for it " +
	"to delete."

// DeleteNotEmptiedNote is said when the document went to the Trash but the
// Trash would not empty.
//
// It is not phrased as a failure, because the delete was not one: the download
// is out of the library and the record is gone. What it corrects is the promise
// the confirmation made — the user agreed to their Trash being emptied and it
// was not, so they are told rather than left to find a Trash they thought was
// empty.
const DeleteNotEmptiedNote = "That download is deleted and now sits in your reMarkable’s Trash, but Quire " +
	"could not empty the Trash afterwards, so it is still holding what was in it."

// deleteQuestion is the sentence the confirm strip asks.
//
// It names the document exactly as the tablet does, which is the whole point:
// for a volume split across the upload cap the row's title is the chapter range
// and the *document* is "… (part 2 of 3)". Naming the file is what makes it
// legible that one part is going and the others are staying.
//
// **It says "for good", and it says the whole Trash goes.** Deleting empties
// the Trash straight afterwards (PLAN §12.4), and the probe watched that call
// replace a document's metadata and content with a tombstone — so this is
// destruction, not the recoverable Trash the earlier wording promised. The
// user asked for the emptying and accepted losing the rest of the Trash with
// it; they are still owed the sentence at the moment they confirm, because the
// thing being destroyed may be something of theirs that Quire never put there.
func deleteQuestion(name string) string {
	if name == "" {
		// A record from before names were kept. "That download" is vague, but
		// it is not wrong, and inventing a name would be.
		return "Delete that download from your reMarkable for good, and empty the Trash — including anything " +
			"else already in it — at the same time?"
	}
	return "Delete “" + name + "” from your reMarkable for good, and empty the Trash — including anything " +
		"else already in it — at the same time?"
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

		// The assembled PDF is reclaimable space PLAN §6 M7 already wants back,
		// and it is of no use once the document it produced is in the Trash. A
		// PDF that is already gone is not a failure: the UUID outlives the file
		// by design (library.Record.PDF).
		if rec.PDF != "" {
			if err := os.Remove(rec.PDF); err != nil && !errors.Is(err, fs.ErrNotExist) {
				s.log.Warn("could not remove the assembled PDF", "path", rec.PDF, "err", err)
			}
		}

		if err := s.libStore.Remove(rec.Key); err != nil {
			s.log.Error("could not forget a trashed volume", "uuid", req.DocumentUUID, "err", err)
			return s.sendError(out, "not_forgotten", DeleteNotForgottenRemedy)
		}
		s.log.Info("a downloaded volume was deleted from the reMarkable",
			"document", req.DocumentUUID, "name", rec.VisibleName, "trashEmptied", req.Emptied)
		break
	}

	// Sent even when no record matched: the frontend asked for a row to stop
	// saying "Read", the document really is gone, and a silent reply would
	// leave the row offering to open something that is not there.
	if err := send(out, appload.MessageDownloadDeleted,
		map[string]any{"documentUuid": req.DocumentUUID}); err != nil {
		return err
	}

	if !req.Emptied {
		// The delete stands; only the promise about the Trash did not. Said
		// after the row has already been put right, so the screen shows a
		// finished delete with a note about it rather than a failure.
		s.log.Warn("the Trash was not emptied after a delete", "document", req.DocumentUUID)
		return s.sendError(out, "trash_not_emptied", DeleteNotEmptiedNote)
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
