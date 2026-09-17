// The handoffs to xochitl's own QML: opening a document (PLAN §6 M6), moving
// one to the Trash (PLAN §12.4), and sorting a finished download into its
// series folder (PLAN §6 M5, the conclusion corrected 2026-09-17).
//
// This is the only file in Quire that touches xochitl's own QML, and both live
// here rather than in a file each so that it stays the only one. It is a
// separate file, loaded through a Loader, precisely so that it is the only
// thing that breaks if those imports ever go away: a failed Loader leaves the
// rest of the app running, the "Read" button reporting that it cannot open the
// document and "Delete" reporting that it changed nothing, instead of taking
// the whole frontend down with it.
//
// It needs no QMLDiff patch. An AppLoad application's QML runs inside
// xochitl's own QML engine, so these singletons are simply importable — proven
// on hardware 2026-09-15 against OS 3.25.1.1 (see PLAN §6 M6 and
// docs/QMD-NOTES.md). If a future OS closes that door, this file fails to load
// and nothing else does.
//
// **The page-offset trap.** MainView.qml:88 honours a `page` argument only when
// a search-highlight object is also present, so passing {documentId, page}
// silently drops the page and lands on lastOpenedPage instead. Calling
// LibraryController.setLastOpenedPage(id, page) first is how xochitl works
// around its own bug (Navigator.qml:857-860), and it is why open() below is in
// that order and must stay in it.

import QtQuick 2.5
import device.global
import com.remarkable
import "Sorting.js" as Sorting

QtObject {
    id: handoff

    // open asks the stock reader to show a document.
    //
    // It returns false when the UUID no longer resolves — the user deleted the
    // document on the tablet — so the caller can say so and offer it again.
    // Nothing is created or repaired here: a missing document stays missing.
    function open(uuid, page) {
        if (!uuid)
            return false

        var entry = Library.entryForId(uuid)
        if (!entry)
            return false

        // Must come first. See the trap in the file comment.
        if (page !== undefined && page !== null && page >= 0)
            LibraryController.setLastOpenedPage(uuid, page)

        var loader = Global.documentViewLoader
        if (!loader || !loader.item)
            return false

        loader.item.openDocument(entry)
        return true
    }

    // sort puts a finished download in its series folder, and answers with what
    // actually happened.
    //
    // The two calls are xochitl's, and both were proven on hardware after five
    // probe rounds (PLAN §6 M5):
    //
    //   Library.createCollectionWrapper(parentIdString, name)
    //   explorer.selectionMove(folderIdString)
    //
    // **Both want id strings and the parent comes first.** An Entry object in
    // the parent slot is accepted and then ignored — the folder lands at the
    // root with no error at all — which is why Sorting.js reads every new
    // folder's parent back before it moves anything into it.
    //
    // The decisions live in Sorting.js so the offscreen harness can drive every
    // path, including the ones that only happen when the device says no. This
    // function is just the device: five callbacks and no judgement.
    function sort(req) {
        return Sorting.sortDocuments({
            parentOf: function (id) {
                return Library.parentIdForId(id)
            },
            createFolder: function (parent, name) {
                return String(Library.createCollectionWrapper(parent, name))
            },
            select: function (ids) {
                var ex = NavigationManager.treeExplorerForNavigation
                ex.selection.clear()
                for (var i = 0; i < ids.length; ++i)
                    ex.selection.add(ids[i])
                return ex.selection.size
            },
            move: function (folderId) {
                NavigationManager.treeExplorerForNavigation.selectionMove(folderId)
            },
            clearSelection: function () {
                // Never Library.documentSelection: it drives the navigator's own
                // enabled-state bindings and writing it from outside wedges the
                // side menu (PLAN §12.4).
                NavigationManager.treeExplorerForNavigation.selection.clear()
            }
        }, req)
    }

    // trash deletes a document: into xochitl's Trash, and then the Trash is
    // emptied. PLAN §12.4 proved both calls on hardware.
    //
    // **Emptying destroys.** removeAllTrashed() does not hide the document, it
    // removes it: the probe watched a document's .metadata and content replaced
    // by a tombstone stamped with the second of the call, and nothing else in
    // the xochitl directory touched. It is what the user asked for — "just
    // empty the trash after a deletion, i don't really mind if my whole trash
    // is emptied" — and it is why the confirmation says so before it happens.
    //
    // It answers with a word rather than a flag, because the outcomes want
    // different things from the caller: "ok" is deleted and the Trash emptied,
    // "kept" is deleted but the Trash still holding it, "gone" means the
    // document was already not there and the M6 missing-document path already
    // has the right answer for that, and "failed" means change nothing at all.
    //
    // **There are two selections and only one of them is safe to write.**
    // `explorer.selection` is what selectionMoveToTrash() acts on.
    // `Library.documentSelection` drives the navigator's own enabled-state
    // bindings, and writing it from outside wedged the side menu badly enough
    // to need a xochitl restart. It is never touched here. `add` also takes an
    // id string, not a Document (Navigator.qml:733).
    function trash(uuid) {
        if (!uuid)
            return "failed"

        try {
            if (!Library.entryForId(uuid))
                return "gone"

            var ex = NavigationManager.treeExplorerForNavigation
            if (!ex || !ex.selection)
                return "failed"

            // One document, chosen explicitly. Clearing first means an earlier
            // selection left behind by the navigator cannot be swept into this
            // delete — the user asked for one row.
            ex.selection.clear()
            ex.selection.add(uuid)
            if (ex.selection.size !== 1) {
                // The id did not take. Leaving a half-made selection behind is
                // how the navigator ends up disagreeing with itself.
                ex.selection.clear()
                return "failed"
            }

            ex.selectionMoveToTrash()
            var left = ex.selection.size

            // Always, whatever happened: nothing should stay selected under the
            // user (PLAN §12.4's implementation notes).
            ex.selection.clear()

            if (left !== 0) {
                // The move did not take. The probe hit exactly this by trashing
                // a UUID that no longer existed, so it is a real state and not
                // a defensive flourish.
                return "failed"
            }

            // The Trash is emptied second, and only ever after a move that
            // worked. It lives on the explorer and nowhere else: the probe
            // found emptyTrash undefined and no removeAllTrashed on Library or
            // LibraryController (OS 3.25.1.1, 2026-09-16).
            if (typeof ex.removeAllTrashed !== "function")
                return "kept"
            try {
                ex.removeAllTrashed()
            } catch (emptyFailed) {
                // The document is in the Trash, which is still a delete. Only
                // the emptying did not happen, and saying otherwise would tell
                // the user their download survived when it did not.
                console.log("[quire] the Trash could not be emptied: " + emptyFailed)
                return "kept"
            }
            return "ok"
        } catch (e) {
            // Every early return above clears the selection before it leaves,
            // and a throw must not be the one path that does not: a half-made
            // selection left behind is exactly what made the navigator
            // disagree with itself the first time (PLAN §12.4).
            try {
                var ex2 = NavigationManager.treeExplorerForNavigation
                if (ex2 && ex2.selection)
                    ex2.selection.clear()
            } catch (ignored) {}
            console.log("[quire] trash failed: " + e)
            return "failed"
        }
    }
}
