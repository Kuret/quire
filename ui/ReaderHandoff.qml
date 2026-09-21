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
// It needs no QMLDiff patch. An Annex app's QML runs inside xochitl's own QML
// engine, so these singletons are simply importable — proven on hardware
// 2026-09-15 against OS 3.25.1.1, and again under Annex on 3.28.0.172 (see
// PLAN §6 M6 and docs/QMD-NOTES.md). If a future OS closes that door, this
// file fails to load and nothing else does.
//
// **The page-offset trap.** MainView.qml:88 honours a `page` argument only when
// a search-highlight object is also present, so passing {documentId, page}
// silently drops the page and lands on lastOpenedPage instead. Calling
// LibraryController.setLastOpenedPage(id, page) first is how xochitl works
// around its own bug (Navigator.qml:857-860), and it is why open() below is in
// that order and must stay in it.
//
// **Two things moved on 3.28.0.172 and both are load-bearing here.**
//
// 1. `com.remarkable` is gone; `Library` now lives in `xofm.libs.library`.
//    Measured, not guessed — the old import is what made this file fail to
//    load on 3.28 at all.
//
// 2. `Global.documentViewLoader` no longer exists, so there is no named path
//    to the reader. The loader is found by walking the object tree for
//    `objectName === "DocumentView"` instead. That is deliberately structural:
//    the objectName is what `MainView.qml` sets on the Loader
//    (docs/QMD-NOTES.md), and a name survives the kind of reshuffle that has
//    now broken a property reference twice.

import QtQuick 2.5
// `device.global` is deliberately not imported any more. It was here only for
// `Global.documentViewLoader`, which does not exist on 3.28 — so keeping it
// would be one more module that has to resolve for this file to load, in
// exchange for nothing. Every import in this file is a way for the reader
// handoff to go missing, and the Loader around it is what turns that into a
// degraded button instead of a dead app.
import xofm.libs.library
import "Sorting.js" as Sorting
import "Reconcile.js" as Reconcile
import "Deleting.js" as Deleting

QtObject {
    id: handoff

    // anchor is any Item in the live scene, set by Main.qml after this loads.
    //
    // It exists because this is a QtObject and therefore has no parent of its
    // own: finding the reader means walking the object tree, and a QtObject
    // is not in it. Main.qml hands over its root, which is.
    property Item anchor: null

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
        //
        // Guarded rather than called outright: LibraryController's module on
        // 3.28 is not something this project has measured, and a page offset
        // is worth strictly less than the handoff itself. If it is missing,
        // the document opens on its last-opened page — which is where a plain
        // openDocument lands anyway — instead of the file failing to load and
        // "Read" reporting that it cannot open anything at all.
        if (page !== undefined && page !== null && page >= 0) {
            if (typeof LibraryController !== "undefined" && LibraryController.setLastOpenedPage)
                LibraryController.setLastOpenedPage(uuid, page)
            else
                console.log("[quire] no LibraryController; opening on the last page instead of " + page)
        }

        var loader = handoff.documentViewLoader()
        if (!loader || !loader.item)
            return false

        loader.item.openDocument(entry)
        return true
    }

    // documentViewLoader finds the Loader that holds the stock reader.
    //
    // `Global.documentViewLoader` was the named route and is gone on 3.28, so
    // this walks the object tree for the Loader that MainView.qml marks with
    // `objectName: "DocumentView"` — the same name docs/QMD-NOTES.md records
    // and the same one the Annex host uses.
    //
    // The walk starts at the app's own root and climbs to the window, because
    // an Annex app is parented *inside* the navigator and the reader is a
    // sibling branch further up. It is breadth-first and depth-capped: the
    // tree is large, and an unbounded recursive walk over a QML object graph
    // is a good way to turn a tap into a visible pause on a 1.8 GHz device.
    //
    // The result is cached, because the Loader is part of MainView and lives
    // as long as xochitl does — so the walk happens once per session rather
    // than once per Read. The cache is validated before use rather than
    // trusted: a destroyed QML object comes back as null through a `var`
    // property, so a stale one costs one re-walk and not a crash.
    property var _cachedLoader: null

    function documentViewLoader() {
        if (_cachedLoader && _cachedLoader.objectName === "DocumentView")
            return _cachedLoader
        _cachedLoader = handoff.findDocumentView()
        return _cachedLoader
    }

    function findDocumentView() {
        var root = handoff.anchor
        if (!root) {
            console.log("[quire] the reader handoff has no anchor into the scene")
            return null
        }
        while (root.parent)
            root = root.parent

        var queue = [root]
        var depth = 0
        while (queue.length > 0 && depth < 32) {
            var next = []
            for (var i = 0; i < queue.length; ++i) {
                var node = queue[i]
                if (!node)
                    continue
                if (node.objectName === "DocumentView")
                    return node
                var kids = node.children
                if (!kids)
                    continue
                for (var k = 0; k < kids.length; ++k)
                    next.push(kids[k])
            }
            queue = next
            depth += 1
        }
        console.log("[quire] no DocumentView in the object tree; the reader cannot be reached")
        return null
    }

    // sort puts a finished download in its series folder, and answers with what
    // actually happened.
    //
    // The create call has moved twice now. `Library.createCollectionWrapper`
    // was proven on hardware on 3.25 and again on 3.27. On 3.28.0.172 it is
    // gone outright — calling it throws `TypeError: ... is not a function`,
    // even though the name still sits in that class's meta-object table in
    // the shipped binary. A live-object probe run against the device (not
    // the binary, which misled twice already) found `createCollection` in
    // its place. That is a third distinct way this API has moved: first the
    // module path (3.28, see the file comment), then an id's object-vs-
    // string shape (below), and now the method's name disappearing under a
    // name that still reads as present in static analysis. The two calls,
    // current as of 3.28:
    //
    //   Library.createCollection(parentIdString, name)
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
                // The same object-vs-string trap as entryIds/entryId below,
                // confirmed against xochitl's own 3.28 QML: create-notebook-
                // window.qml passes `currentFolderId` (the wrapper object)
                // straight through to createDocument, and .toString()s it
                // only where a string is wanted elsewhere in the same file.
                // Sorting.js's parent is a plain uuid string — ROOT's own
                // sentinel, "" — so it has to be resolved here the same way
                // entryId() resolves a move destination, and the root
                // sentinel must pass through unresolved: it does not name an
                // entry, and there is nothing for entryId() to look up.
                //
                // Whether a root-level create (parent "") works at all on
                // 3.28 is NOT verified. Every call site in xochitl's own QML
                // requires a real parentFolderId and refuses to open without
                // one (create-collection-window.qml:19-23), so there is no
                // proof either way from the device's own code — only that
                // xochitl itself never exercises this case. The exception
                // recorded in Sorting.js's create() is what will say, from
                // the next device log, whether this fails too.
                //
                // `createCollection` replaces `createCollectionWrapper`
                // (gone outright on 3.28.0.172; see the comment on sort()
                // above). Its return shape has never been observed on
                // hardware — a Collection*, a wrapper object, an id object,
                // or a plain string are all plausible — so it is not
                // stringified directly. Sorting.extractId takes an object's
                // `id` member when there is one and the value itself
                // otherwise, and answers "" for anything it cannot make
                // sense of, so an unrecognised shape degrades to the id
                // create() already rejects rather than to a plausible-
                // looking wrong string.
                var resolvedParent = parent === "" ? parent : handoff.entryId(parent)

                // `createCollection`'s return value alone has never yielded a
                // usable id on hardware (see the log this scaffolding
                // produced: "with no id member"), even though the folder is
                // genuinely created. `Library.collectionCreated` is the
                // second route: a signal that looks like it is meant to
                // carry the new collection. Qt signal connections made from
                // QML/JS are direct connections when emitter and receiver
                // share a thread, so a signal raised synchronously inside
                // createCollection's own call stack fires this handler
                // before createCollection returns — captured has already
                // been set by the time the `finally` below disconnects it.
                // Verified against Qt's own semantics rather than assumed;
                // if a future OS ever made this emission queued/async
                // instead, `captured` would simply stay unset here and this
                // falls straight through to the existing (already-handled)
                // failure path below — nothing here blocks or waits for it.
                var captured
                var haveCaptured = false
                var onCollectionCreated = function (created) {
                    haveCaptured = true
                    captured = created
                }
                Library.collectionCreated.connect(onCollectionCreated)
                var raw
                try {
                    raw = Library.createCollection(resolvedParent, name)
                } finally {
                    Library.collectionCreated.disconnect(onCollectionCreated)
                }

                var id = Sorting.extractId(raw)
                if (!id && haveCaptured)
                    id = Sorting.extractId(captured)

                // TODO(scaffolding): remove once folder creation is confirmed
                // working on 3.28. Two device round-trips have already gone
                // into this call; these lines are what will say, from the
                // next one, whether extractId guessed the return shape (and
                // now the signal's shape) right, instead of a third blind
                // guess. The member enumeration is the same technique that
                // settled what Library itself exposes (docs/QMD-NOTES.md):
                // if `raw`/`captured` is an object, list its own member
                // names and their typeof rather than only noting "id" is
                // missing, so the next log says what shape it actually is.
                console.log("[quire] createCollection returned " + typeof raw +
                            (raw !== null && typeof raw === "object"
                                ? (("id" in raw) ? " with an id member" : " with no id member")
                                : "") +
                            "; extracted id=\"" + id + "\"")
                console.log("[quire] createCollection return value members: " +
                            handoff.describeMembers(raw))
                console.log("[quire] collectionCreated " +
                            (haveCaptured
                                ? "delivered " + typeof captured + "; members: " +
                                  handoff.describeMembers(captured)
                                : "did not fire during the call"))
                return id
            },
            // Both sides resolved, for the same reason: the destination is an
            // id the controller has to recognise too, and a folder uuid string
            // is exactly as useless to it as a document uuid string.
            move: function (uuids, folderId) {
                LibraryController.moveEntries(handoff.entryIds(uuids),
                                              handoff.entryId(folderId))
            }
        }, req)
    }

    // describeMembers lists an object's own member names and their typeof,
    // the same technique the earlier `Library` probe used (`for (var k in
    // value)`) to find what the shipped binary's meta-object table got
    // wrong twice already. Guarded because enumerating an arbitrary device
    // object is exactly the kind of thing that has surprised this project
    // before, and a probe log line must never itself be what crashes the
    // handoff.
    //
    // TODO(scaffolding): remove alongside the createFolder logging above
    // once createCollection's and collectionCreated's shapes are confirmed
    // on 3.28.
    function describeMembers(value) {
        if (value === undefined || value === null || typeof value !== "object")
            return "(" + typeof value + ", not an object)"
        try {
            var out = []
            for (var k in value)
                out.push(k + ":" + typeof value[k])
            return out.length ? out.join(", ") : "(no enumerable own members)"
        } catch (e) {
            return "(could not enumerate: " + String(e) + ")"
        }
    }

    // entryIds maps document uuids to the ids the controller acts on.
    //
    // **`entry.id` is an object, and stringifying it is the bug.** Measured on
    // 3.28.0.172 (2026-09-18):
    //
    //     [quire]   .id = 11fd53c9-ecbf-4fa4-bd51-75a09a2bb31f  (object)
    //
    // It is a wrapper whose toString() is the uuid. So `String(entry.id)`
    // yields exactly the right-looking text, is truthy, passes every guard this
    // code has — and is then worthless to the controller, which takes the
    // string, finds no entry for it and does nothing. xochitl says so in its
    // own log and nowhere the app can see:
    //
    //     rm.library.controller  moving "" to trash (moveEntryToTrash ...)
    //
    // That is what broke delete on 3.28, and the same line broke the move into
    // series folders — one `String()` in one helper, two features.
    //
    // On 3.25 and 3.27 the id and the uuid really were the same string, which
    // is why the stringify was invisible for as long as it was.
    //
    // A uuid that no longer resolves is dropped rather than passed through: an
    // id the controller does not recognise is ignored silently, and a silently
    // ignored id in a list of five is four moves and a mystery.
    function entryIds(uuids) {
        var out = []
        for (var i = 0; i < uuids.length; ++i) {
            var entry = Library.entryForId(uuids[i])
            if (entry)
                out.push(entry.id)
        }
        return out
    }

    // entryId is entryIds for a single uuid, for the destination of a move.
    //
    // It falls back to the uuid rather than to nothing: a destination that does
    // not resolve is a move that fails, and a move that fails is reported by
    // Sorting.js reading the parent back. Substituting an empty id here would
    // turn that into a move to the top of My Files.
    function entryId(uuid) {
        var ids = handoff.entryIds([uuid])
        return ids.length ? ids[0] : uuid
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
            // Measured on 3.28.0.172: a trashed entry's parent reads back as
            // the literal "trash", which is what Deleting.js compares against.
            // Worth having been checked rather than assumed — every delete on
            // this OS was failing further upstream, so nothing had ever reached
            // this comparison to prove it.
            parentOf: function (id) {
                return String(Library.parentIdForId(id))
            },
            // **A check, not the id.** This answers "does this uuid still name
            // something?" and deliberately answers in a plain string: it is
            // compared, logged and carried around by Deleting.js, and the one
            // thing it must never be is an id on its way back to the
            // controller. The two calls below do that resolution themselves,
            // at the moment of the call, so no wrapper object ever leaves this
            // file — and no amount of string handling upstream can break one.
            idFor: function (id) {
                var entry = Library.entryForId(id)
                return entry ? String(entry.id) : ""
            },
            // Both take document uuids and resolve them here. See entryIds.
            moveToTrash: function (uuids) {
                LibraryController.moveEntriesToTrash(handoff.entryIds(uuids))
            },
            deleteEntries: function (uuids) {
                LibraryController.deleteEntries(handoff.entryIds(uuids))
            }
        }
    }

    // The probes that found the object-id bug and confirmed the "trash" parent
    // have done their job and are gone. What they measured is in
    // docs/QMD-NOTES.md and in the comments on entryIds and parentOf above.

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
