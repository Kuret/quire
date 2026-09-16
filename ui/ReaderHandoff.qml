// The two handoffs to xochitl's own QML: opening a document (PLAN §6 M6) and
// moving one to the Trash (PLAN §12.4).
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

    // trash moves a document to xochitl's own Trash — recoverable by the user,
    // and what the stock UI's own delete does. PLAN §12.4 proved the route on
    // hardware.
    //
    // It answers with a word rather than a flag, because the three outcomes
    // want three different things from the caller: "ok" means tell the backend
    // to forget the record, "gone" means the document was already not there and
    // the M6 missing-document path already has the right answer for that, and
    // "failed" means change nothing at all.
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

            return left === 0 ? "ok" : "failed"
        } catch (e) {
            console.log("[quire] trash failed: " + e)
            return "failed"
        }
    }
}
