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
    // The two calls are xochitl's, proven on hardware on 3.25 and again on 3.27:
    //
    //   Library.createCollectionWrapper(parentIdString, name)
    //   LibraryController.moveEntries([idString], destinationIdString)
    //
    // **Everything is an id string, and an object where one belongs is accepted
    // and ignored.** An Entry in the parent slot puts the folder at the root
    // with no error at all; `moveEntries([entry], entry)` moves nothing and
    // says nothing. That is why Sorting.js reads every new folder's parent back
    // before moving anything into it, and every document's parent back after.
    //
    // **The move no longer goes through the tree explorer.** On 3.27 inside an
    // AppLoad app `explorer.selection` is undefined — the explorer is there,
    // its selection is not — and the old path's first act was to clear it.
    //
    // The decisions live in Sorting.js so the offscreen harness can drive every
    // path, including the ones that only happen when the device says no. This
    // function is just the device: three callbacks and no judgement.
    function sort(req) {
        return Sorting.sortDocuments({
            parentOf: function (id) {
                return Library.parentIdForId(id)
            },
            createFolder: function (parent, name) {
                return String(Library.createCollectionWrapper(parent, name))
            },
            move: function (uuids, folderId) {
                LibraryController.moveEntries(handoff.entryIds(uuids), folderId)
            }
        }, req)
    }

    // entryIds maps document uuids to the ids the controller acts on.
    //
    // On 3.25 and 3.27 these are the same string, so the mapping looks like
    // superstition. It is the form rm-librarian had to adopt on 3.28, when the
    // controller stopped accepting raw uuids, and it costs one call that is
    // already being made. A uuid that no longer resolves is dropped rather than
    // passed through: an id the controller does not recognise is ignored
    // silently, and a silently ignored id in a list of five is four moves and a
    // mystery.
    function entryIds(uuids) {
        var out = []
        for (var i = 0; i < uuids.length; ++i) {
            var entry = Library.entryForId(uuids[i])
            if (entry)
                out.push(String(entry.id))
        }
        return out
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
    // **It no longer touches a selection either.** The trash step was the tree
    // explorer's `selectionMoveToTrash()`, which needs `explorer.selection` —
    // undefined on 3.27 inside an AppLoad app. `moveEntriesToTrash([id])` is
    // the library-level call and needs no file list at all. That also retires
    // the trap that came with the old path: `Library.documentSelection` drives
    // the navigator's own enabled-state bindings and wedged the side menu when
    // written from outside. Nothing here has a selection to confuse it with.
    //
    // The order, the reading-back and the four answers live in Deleting.js so
    // the offscreen harness can drive them. This function is the device: five
    // callbacks and no judgement.
    function trash(uuid) {
        return Deleting.deleteDocument(handoff.library(), uuid)
    }

    // library is the five calls Deleting.js works against, in one place so
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
            moveToTrash: function (ids) {
                LibraryController.moveEntriesToTrash(ids)
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
