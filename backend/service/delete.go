package service

import (
	"errors"
	"io/fs"
	"os"

	"github.com/rickl/quire/backend/appload"
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

	// Trashed is what the frontend observed, not what it intended. False means
	// the document is still on the tablet.
	Trashed bool `json:"trashed"`
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
		s.log.Info("a downloaded volume was moved to the reMarkable's Trash",
			"document", req.DocumentUUID, "name", rec.VisibleName)
		break
	}

	// Sent even when no record matched: the frontend asked for a row to stop
	// saying "Read", the document really is in the Trash, and a silent reply
	// would leave the row offering to open something that is not there.
	return send(out, appload.MessageDownloadDeleted, map[string]any{"documentUuid": req.DocumentUUID})
}
