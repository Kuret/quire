// The native reader handoff — PLAN §6 M6.
//
// This is the only file in Quire that touches xochitl's own QML. It is a
// separate file, loaded through a Loader, precisely so that it is the only
// thing that breaks if those imports ever go away: a failed Loader leaves the
// rest of the app running and the "Read" button reporting that it cannot open
// the document, instead of taking the whole frontend down with it.
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
}
