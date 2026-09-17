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
import "Reconcile.js" as Reconcile
import "Deleting.js" as Deleting

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

    // check reports which of these documents are still on the tablet.
    //
    // `Library.entryForId` is the question, and it is the *only* question: a
    // document the user has filed into a folder of their own still resolves and
    // is not missing. Folder membership is never consulted.
    //
    // The decisions are in Reconcile.js so the harness can drive them; what is
    // here is the one call, and the guarantee that a failure is reported as a
    // failure rather than as an empty list.
    function check(uuids) {
        return Reconcile.check({
            resolves: function (id) {
                return Library.entryForId(id) ? true : false
            }
        }, uuids)
    }

    // trash deletes a document: into xochitl's Trash, and then out of the Trash
    // for good. PLAN §12.4 proved every call here on hardware.
    //
    // **It no longer empties the Trash.** `removeAllTrashed()` destroyed
    // whatever else the user had in there, and the confirmation had to warn
    // about it. `LibraryController.deleteEntries` — which is in xochitl itself,
    // on 3.25 and 3.27 — removes one entry instead, and a control folder left
    // in the Trash beside two deletions survived both (2026-09-17). That
    // measurement is what let the warning go.
    //
    // The order, the reading-back and the four answers live in Deleting.js so
    // the offscreen harness can drive them. This function is the device: seven
    // callbacks and no judgement.
    //
    // **There are two selections and only one of them is safe to write.**
    // `explorer.selection` is what selectionMoveToTrash() acts on.
    // `Library.documentSelection` drives the navigator's own enabled-state
    // bindings, and writing it from outside wedged the side menu badly enough
    // to need a xochitl restart. It is never touched here. `add` also takes an
    // id string, not a Document (Navigator.qml:733).
    function trash(uuid) {
        return Deleting.deleteDocument(handoff.library(), uuid)
    }

    // library is the seven calls Deleting.js works against, in one place so
    // that the single delete and the row delete cannot drift apart. The
    // judgement is all in Deleting.js; this is the device.
    function library() {
        return {
            exists: function (id) {
                return Library.entryForId(id) ? true : false
            },
            parentOf: function (id) {
                return String(Library.parentIdForId(id))
            },
            // The id deleteEntries is given. Identical to the uuid on 3.25 and
            // deliberately not written as the uuid; see Deleting.js idFor.
            idFor: function (id) {
                var entry = Library.entryForId(id)
                return entry ? String(entry.id) : ""
            },
            select: function (id) {
                var ex = NavigationManager.treeExplorerForNavigation
                ex.selection.add(id)
                return ex.selection.size
            },
            selectionSize: function () {
                var ex = NavigationManager.treeExplorerForNavigation
                return ex && ex.selection ? ex.selection.size : 0
            },
            moveToTrash: function () {
                NavigationManager.treeExplorerForNavigation.selectionMoveToTrash()
            },
            clearSelection: function () {
                // Never Library.documentSelection: see the file comment above.
                NavigationManager.treeExplorerForNavigation.selection.clear()
            },
            deleteEntries: function (ids) {
                LibraryController.deleteEntries(ids)
            }
        }
    }

    // trashMany deletes several documents, one at a time, and reports each.
    //
    // The row delete on the downloaded overview (PLAN §12.5). It runs the same
    // two-step delete per document rather than batching the ids into one
    // `deleteEntries` call: the batch would be one result for several
    // documents, and the whole point of the reporting is that each of them
    // fails on its own.
    function trashMany(uuids) {
        return Deleting.deleteMany(handoff.library(), uuids)
    }
}
