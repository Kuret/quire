package service

import (
	"context"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
)

// The series folder left behind when a series' last download is deleted.
//
// PLAN §12.5. Removing it is the natural completion of the row delete — but the
// folder is the user's once it exists and may hold something of theirs, so it
// goes **only when it has been measured empty**, never when it is merely
// believed to be.
//
// # Emptiness is measurable, and only on this side
//
// The frontend cannot enumerate a folder's children: ten QML candidates were
// tried on hardware and none of them listed one. The backend can, and already
// does on every download — `library.List` over xochitl's web interface is what
// the sorting pass uses to find an existing series folder. So the question is
// asked here, and the *deleting* is asked of the frontend, which is the only
// side that can delete anything.
//
// # Silence is not emptiness
//
// Every way of not knowing leaves the folder alone: the web interface
// unreachable, the listing erroring, Comics itself not resolving, a folder id
// Quire never recorded. It is the same rule as `checked` on the reconcile pass
// (PLAN §12.4) and for the same reason — an answer that could not be obtained
// must never be read as "nothing there".

// folderVerdict decides whether a folder may be deleted, and says why not.
//
// Pure, and separate from the plumbing, because every interesting case here is
// a refusal and a refusal is hard to assert through an asynchronous send.
//
// `listErr` is the listing's own error. `entries` is what came back. `comicsID`
// is the id of the user's Comics folder, or "" if it could not be established.
func folderVerdict(folderID, comicsID string, entries []library.Entry, listErr error) (bool, string) {
	switch {
	case folderID == "":
		// Not a folder Quire recorded. The documents may well have been filed
		// by hand, and where they were filed is the user's business.
		return false, "no series folder was recorded for those downloads"
	case folderID == library.RootID:
		return false, "the recorded folder is the top level"
	case comicsID == "":
		// Without knowing which folder is Comics, the guard below cannot be
		// applied, so nothing may be deleted at all.
		return false, "Comics could not be resolved, so the guard cannot be applied"
	case folderID == comicsID:
		// **Never Comics.** A user who deletes their only series should still
		// have the folder every future download goes into, and an empty Comics
		// is an ordinary state rather than litter.
		return false, "the recorded folder is Comics itself"
	case listErr != nil:
		// A listing that failed is not an empty folder.
		return false, "the folder could not be listed: " + listErr.Error()
	case len(entries) > 0:
		return false, "the folder still has something in it"
	}
	return true, ""
}

// SeriesFolderRemovedNote is said when the empty folder went too.
//
// It is the one sentence that may claim the tidy-up, and it is sent only after
// the frontend has reported the folder gone — not when Quire asked for it.
func SeriesFolderRemovedNote(name string) string {
	if name == "" {
		return "The series folder was empty afterwards, so Quire removed it from Comics too."
	}
	return "The “" + name + "” folder was empty afterwards, so Quire removed it from Comics too."
}

// considerEmptyFolder is the one way a series folder is ever tidied up, and it
// is reached from both deletes.
//
// It was wired into the series delete only, which meant deleting a series from
// the Downloaded row tidied up and deleting its last chapter from the chapter
// row did not — the same end state with two outcomes, found on 3.26 with a
// Kingdom folder left behind. Two implementations of one rule is the shape that
// has bitten this project repeatedly; there is now one.
//
// **The order matters and is the mirror of the reclaimPages bug.** "Are there
// records left for this series?" has to be asked *after* the record being
// deleted is dropped. Asked before, it finds the record on its way out, decides
// the series still has downloads, and the cleanup never fires — which is
// exactly how reclaimPages once concluded that every chapter was still in use.
//
// `folderID` and `name` must be read from the record *before* it is dropped,
// for the same reason: they live on the record, and it is about to go.
func (s *Service) considerEmptyFolder(ctx context.Context, out Sender, sourceID, seriesID, folderID, name string) {
	if folderID == "" {
		return
	}
	if left := s.recordsFor(sourceID, seriesID); len(left) > 0 {
		// Still downloads in it, so it is not empty and nothing is asked. The
		// listing would say the same, but a delete that cannot leave the
		// folder empty should not be making HTTP calls to find that out.
		s.log.Info("a series still has downloads, so its folder stays",
			"series", seriesID, "left", len(left))
		return
	}
	s.tidySeriesFolder(ctx, out, folderID, name)
}

// seriesFolderOf is the series folder a record was filed into, or "".
//
// Only a folder *inside* Comics counts, by the same rule recordedFolder uses: a
// record from before series folders existed points straight at Comics, and
// Comics is never a series folder. folderVerdict guards that again by id; this
// is the cheaper check that gets there first.
//
// A book's finished path is Books alone — one name, the same length as an
// unfiled comic's — so this returns "" for a book exactly as it does for an
// unsorted comic, and considerEmptyFolder never runs on the Books folder at
// all. That is the guard PLAN's Books design relies on: there is no per-title
// folder to ever be found empty, so the sweep never has an id to check
// folderVerdict's "never Comics"-shaped guard against.
func seriesFolderOf(rec library.Record) string {
	if rec.FolderUUID != "" && len(rec.FolderPath) == 2 {
		return rec.FolderUUID
	}
	return ""
}

// tidySeriesFolder asks the frontend to delete the folder, if it may.
//
// It runs in the background because `library.List` is an HTTP call to the
// tablet with a two-minute ceiling, and the message loop that called this is
// the one every other message is waiting on.
func (s *Service) tidySeriesFolder(ctx context.Context, out Sender, folderID, name string) {
	if s.library == nil || folderID == "" {
		return
	}
	// WithoutCancel: the delete is finished and its records are already gone.
	// Tying this to the message's context would abandon the tidy-up halfway
	// through for no reason the user could see.
	s.bg.start(context.WithoutCancel(ctx), func(bgCtx context.Context) {
		comicsID := ""
		if place, err := s.library.Resolve(bgCtx, library.ComicsFolder); err == nil && len(place.Path) == 1 {
			comicsID = place.FolderID
		}

		entries, listErr := s.library.List(bgCtx, folderID)
		ok, why := folderVerdict(folderID, comicsID, entries, listErr)
		if !ok {
			// Left alone, and said out loud in the log rather than nowhere: a
			// folder quietly surviving is exactly the sort of thing that looks
			// like a bug a year later.
			s.log.Info("an empty series folder was left alone", "folder", folderID, "why", why)
			return
		}

		s.log.Info("a series folder is empty and will be removed", "folder", folderID, "name", name)
		if err := send(out, appload.MessageDeleteFolder, map[string]any{
			"folderId":   folderID,
			"folderName": name,
		}); err != nil {
			s.log.Warn("could not ask for an empty series folder to be removed", "err", err)
		}
	})
}

// folderDeletedRequest is the MessageFolderDeleted payload: what the frontend
// observed, not what it was asked to do.
type folderDeletedRequest struct {
	FolderID   string `json:"folderId"`
	FolderName string `json:"folderName"`

	// Trashed means the folder is out of Comics. Removed means it was taken
	// out of the Trash as well, which is the same two-step delete a document
	// gets — folders and documents are both entries to that API.
	Trashed bool `json:"trashed,omitempty"`
	Removed bool `json:"removed,omitempty"`

	// Detail is why it did not go, when it did not.
	Detail string `json:"detail,omitempty"`
}

// folderDeleted reports the tidy-up, and only the one that happened.
//
// A folder that did not go is not mentioned at all. It is a tidy-up, not the
// thing the user asked for — the downloads are already deleted and already
// reported — and a sentence about a folder they never thought about would be
// noise. Claiming one that did not happen would be worse than noise.
func (s *Service) folderDeleted(out Sender, req folderDeletedRequest) error {
	if !req.Removed {
		s.log.Info("an empty series folder was not removed",
			"folder", req.FolderID, "trashed", req.Trashed, "detail", req.Detail)
		return nil
	}
	s.log.Info("an empty series folder was removed", "folder", req.FolderID)
	return s.sendError(out, "folder_removed", SeriesFolderRemovedNote(req.FolderName))
}
