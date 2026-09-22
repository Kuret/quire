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

// folderUnresolved marks a top-level folder id that an attempt to resolve
// could not establish — as distinct from "", which means the resolve
// succeeded and found no such folder. It is never a real reMarkable folder
// id, so it can only ever compare unequal to one.
const folderUnresolved = "\x00 unresolved"

// resolveTopLevel is Comics or Books, resolved to an id, "" (confirmed not to
// exist), or folderUnresolved (the attempt failed and it is unknown).
//
// Both callers in tidySeriesFolder need exactly this three-way answer, so it
// lives once: a folder that simply hasn't been created yet must not read the
// same as one nobody could ask about.
func (s *Service) resolveTopLevel(ctx context.Context, name string) string {
	place, err := s.library.Resolve(ctx, name)
	if err != nil {
		return folderUnresolved
	}
	if len(place.Path) == 1 {
		return place.FolderID
	}
	return ""
}

// folderVerdict decides whether a folder may be deleted, and says why not.
//
// Pure, and separate from the plumbing, because every interesting case here is
// a refusal and a refusal is hard to assert through an asynchronous send.
//
// `listErr` is the listing's own error. `entries` is what came back. `comicsID`
// and `booksID` are the resolved ids of the user's Comics and Books folders:
// `""` when the folder was positively confirmed not to exist yet, and
// `folderUnresolved` when the attempt to find out failed and it is genuinely
// unknown. The two must not be conflated — a device with no Books folder at
// all (most of them, before a first book is ever downloaded) must not stop
// every comic's per-series folder from ever being tidied, but a resolve that
// merely failed must never be read as "so there is no Books to worry about".
//
// **Comics and Books are checked by resolved id, not inferred from anything
// about the recorded folder's shape.** `seriesFolderOf` and `recordedFolder`
// carry a heuristic — a book's path is one name, a comic's per-series path is
// two — that is meant to keep the top-level folders out of `folderID` in the
// first place, but that heuristic reads a label this package writes onto a
// record, not a fact re-checked against the device. A record from before that
// distinction existed, or one mislabelled some other way, can still hand this
// function a `folderID` that names Comics or Books, and this is the guard that
// must catch it regardless — the same reason `folderID == comicsID` was
// already checked here rather than trusted to never arrive.
func folderVerdict(folderID, comicsID, booksID string, entries []library.Entry, listErr error) (bool, string) {
	switch {
	case folderID == "":
		// Not a folder Quire recorded. The documents may well have been filed
		// by hand, and where they were filed is the user's business.
		return false, "no series folder was recorded for those downloads"
	case folderID == library.RootID:
		return false, "the recorded folder is the top level"
	case comicsID == folderUnresolved || booksID == folderUnresolved:
		// Not "no such folder" — genuinely unknown. Without knowing which
		// folders are Comics and Books, the guards below cannot be applied, so
		// nothing may be deleted at all.
		return false, "Comics or Books could not be resolved, so the guard cannot be applied"
	case folderID == comicsID:
		// **Never Comics.** A user who deletes their only series should still
		// have the folder every future download goes into, and an empty Comics
		// is an ordinary state rather than litter.
		return false, "the recorded folder is Comics itself"
	case folderID == booksID:
		// **Never Books**, for the same reason and checked the same way: an
		// empty Books is where the next downloaded book goes, not litter, and
		// this is positive against the resolved id rather than assumed from
		// how the folder was labelled.
		return false, "the recorded folder is Books itself"
	case listErr != nil:
		// A listing that failed is not an empty folder.
		return false, "the folder could not be listed: " + listErr.Error()
	case len(entries) > 0:
		// The folder has something in it that is not accounted for by what
		// Quire just deleted — the user's own document, filed there by hand,
		// most likely. Ownership of what is inside a folder Quire manages is
		// never inferred from the folder alone; only an empty listing, checked
		// here, says nothing of the user's is in it.
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
// Comics is never a series folder.
//
// A book's finished path is Books alone — one name, the same length as an
// unfiled comic's — so this returns "" for a book exactly as it does for an
// unsorted comic, and in the ordinary case considerEmptyFolder never runs on
// the Books folder at all.
//
// **That is a convention this function tries to keep, not a guarantee this
// function can enforce.** `FolderPath` is a label `rememberFolder` writes from
// what a sort reported, not a fact re-derived from the device each time it is
// read, so a record mislabelled once — by a bug in the sort path, or one
// carried over from before this file existed — can still have a two-element
// path pointing straight at Books' own id, and this function has no way to
// tell that apart from a genuine per-series folder. It was read that way once
// on a real device: the resolved Books id and a document the owner filed there
// by hand both survived a delete only because xochitl's own Trash guard
// happened to refuse them, not because anything here did. `folderVerdict` is
// where Comics and Books are actually kept safe — checked against their
// resolved ids, every time, regardless of what a record claims — and this
// function's job is only to name a candidate for that check to accept or
// refuse.
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
		comicsID := s.resolveTopLevel(bgCtx, library.ComicsFolder)
		booksID := s.resolveTopLevel(bgCtx, library.BooksFolder)

		entries, listErr := s.library.List(bgCtx, folderID)
		ok, why := folderVerdict(folderID, comicsID, booksID, entries, listErr)
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

	// Deleted means the folder is gone from Comics outright — folders and
	// documents are both entries to the same `deleteEntries` call.
	//
	// The wire key is still "trashed": that is the field name ui/Main.qml
	// sends, a holdover from when this went by way of the Trash, and this
	// struct decodes it rather than requiring a frontend change that has not
	// landed yet.
	// Trashed means the folder is out of Comics. Removed means it was taken
	// out of the Trash too.
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
			"folder", req.FolderID, "detail", req.Detail)
		return nil
	}
	s.log.Info("an empty series folder was removed", "folder", req.FolderID)
	return s.sendError(out, "folder_removed", SeriesFolderRemovedNote(req.FolderName))
}
