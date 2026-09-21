// Putting a finished download into its series folder — PLAN §6 M5, the
// conclusion corrected 2026-09-17.
//
// The two calls this drives are xochitl's own. The create call has moved
// twice: `Library.createCollectionWrapper` was proven on hardware on both
// 3.25 and 3.27; on 3.28.0.172 it is gone outright (calling it throws
// "is not a function", despite the name still sitting in that class's
// meta-object table in the shipped binary — the binary misled twice, so a
// live-object probe against the running device is what settled it).
// `Library.createCollection` is what replaced it. Current as of 3.28:
//
//   Library.createCollection(parentIdString, name)   -> new folder id
//   LibraryController.moveEntries([idString], destIdString)
//
// **Everything here is an id string, and an object where one belongs is
// silently ignored.** An Entry in the create call's parent slot puts
// the folder at the root — no throw, no complaint, just the wrong answer —
// and `moveEntries([entry], entry)` moves nothing at all while returning
// normally. That is why every folder made here has its parent read back before
// anything is moved into it, and why the move is verified per document.
//
// **The move used to go through xochitl's tree explorer**, selecting the
// documents and moving the selection. On OS 3.27 that stopped working inside an
// AppLoad app: the explorer is still there, but `explorer.selection` is
// undefined, and the first thing the old code did with it was `clear()`.
// `moveEntries` is a library-level call — no navigator, no selection, no UI
// component in the middle of an operation that has nothing to do with the UI.
//
// The logic lives in a .js rather than in ReaderHandoff.qml because that file
// imports com.remarkable and so cannot be loaded anywhere but the device. Here
// it is plain functions over an object of callbacks, which the offscreen
// harness can drive through every path — including the ones that only happen
// when the device says no.
.pragma library

// ROOT is the parent id meaning "the top of My Files". The device spells it as
// the empty string, both when asked for a parent and when reporting one.
var ROOT = ""

// sortDocuments creates the folder if it has to, moves the documents into it,
// and reports what actually happened.
//
// `api` is the device, as three functions:
//
//   parentOf(id)            -> parent id string, "" for the root
//   createFolder(parent, n) -> new folder id string, or "" if it did not work
//   move(ids, destId)       -> nothing; throwing is allowed, and returning
//                              quietly having done nothing is expected
//
// `req` is MessageSortDocuments. The return value is what goes back on
// MessageDocumentsSorted.
//
// **Nothing here treats a failure as an error.** A document that could not be
// sorted stays in Comics, which is exactly where every download went before
// this existed. The caller reports it as a note, never as a failed download.
function sortDocuments(api, req) {
    var out = {
        documentUuids: req.documentUuids || [],
        moved: [],
        folderId: req.folderId || "",
        folderName: req.folderName || "",
        created: false,
        detail: ""
    }

    if (!out.documentUuids.length) {
        out.detail = "nothing to sort"
        return out
    }

    var under = req.createUnder || ROOT

    // Comics first, when there isn't one. It is the same call, and creating it
    // is now possible — the manual "make a folder called Comics" step only
    // existed because folder creation was thought impossible.
    if (req.createComics) {
        var comicsId = create(api, ROOT, req.comicsName || "Comics", out)
        if (!comicsId) {
            // create() says *why* when it knows — a folder at the wrong parent
            // is a different fact from a call that threw, and the second
            // sentence must not overwrite the first.
            if (!out.detail)
                out.detail = "could not create " + (req.comicsName || "Comics") + "; left where it is"
            return out
        }
        under = comicsId
    }

    if (!out.folderId) {
        out.folderId = create(api, under, out.folderName, out)
        if (!out.folderId) {
            if (!out.detail)
                out.detail = "could not create the folder " + out.folderName + "; left where it is"
            return out
        }
    }

    // The move, and then the only thing that counts as evidence of it: each
    // document's parent, read back. A call that returns without throwing and
    // changes nothing is the failure this is written around.
    //
    // One call, and no selection anywhere near it. This used to select the
    // documents in xochitl's tree explorer and move the selection, which broke
    // on OS 3.27: the explorer is still there, but `explorer.selection` is
    // *undefined* inside an AppLoad app, and `selection.clear()` threw. A move
    // between two folders has nothing to do with what a file list happens to
    // have highlighted, and now does not depend on one.
    var threw = ""
    try {
        api.move(out.documentUuids, out.folderId)
    } catch (e) {
        threw = String(e)
    }

    for (var i = 0; i < out.documentUuids.length; ++i) {
        if (parentOf(api, out.documentUuids[i]) === out.folderId)
            out.moved.push(out.documentUuids[i])
    }

    if (out.moved.length === out.documentUuids.length)
        out.detail = threw ? "moved, though the call complained: " + threw : "moved"
    else if (out.moved.length)
        out.detail = "moved " + out.moved.length + " of " + out.documentUuids.length +
                     (threw ? "; the call threw " + threw : "")
    else
        out.detail = threw ? "the move threw " + threw : "the move changed nothing"

    return out
}

// create makes one folder and refuses to hand it back unless it landed where it
// was asked for.
//
// The check is the whole point: the wrong-parent failure is silent on the
// device, so a folder at the wrong level would otherwise be reported as a
// success and documents would be moved into it.
function create(api, parent, name, out) {
    if (!name)
        return ""

    var id = ""
    try {
        id = api.createFolder(parent, name)
    } catch (e) {
        // Previously discarded: the caller only saw the generic "could not
        // create X; left where it is" fallback and never learned why. This is
        // the case that hit the owner's device on 3.28 — createFolder threw,
        // and the thrown reason is the one fact that was missing from the
        // backend log.
        out.detail = "creating " + name + " under " + describe(parent) +
                     " threw " + String(e)
        return ""
    }
    if (!id)
        return ""

    // parentOf can itself have recorded a thrown reason below; the "rather
    // than" sentence is more specific whenever it applies, but must not paper
    // over a read-back that threw with the same fallback wording it exists to
    // replace.
    var landed = parentOf(api, id, out)
    if (landed !== parent) {
        if (!out.detail)
            out.detail = name + " was created under " + describe(landed) +
                         " rather than " + describe(parent)
        return ""
    }
    out.created = true
    return id
}

// `out` is optional: the per-document read-back in sortDocuments' verification
// loop does not pass one, because that loop already assigns out.detail once,
// unconditionally, after every document has been checked — a reason recorded
// here would only be overwritten there, so it is not worth threading through
// a call that cannot use it. See the comment above that loop.
function parentOf(api, id, out) {
    try {
        var p = api.parentOf(id)
        return p === undefined || p === null ? "" : String(p)
    } catch (e) {
        if (out && !out.detail)
            out.detail = "reading " + id + "'s parent threw " + String(e)
        return ""
    }
}

function describe(id) {
    return id === ROOT ? "the top of My Files" : id
}

// extractId pulls a usable id out of whatever a device create call handed
// back. It is plain value-shape logic — no device types, so the offscreen
// harness can drive it directly — even though the only caller today is
// ReaderHandoff.qml's createFolder, translating createCollection's answer.
//
// The device's own return shape for a create call has moved before
// (createCollectionWrapper answered with a plain uuid string on 3.25/3.27)
// and createCollection's shape on 3.28 has never been observed on hardware:
// a wrapper object exposing `id` — the convention this file already relies
// on for entry ids — a bare string, or something else are all plausible.
//
// **An object with no usable `id` must not be stringified.** `String({})`
// is `"[object Object]"` — truthy, plausible-looking, and wrong — and
// create()'s only guard against a bad id is `if (!id) return ""`. So an
// object is only ever read through its `id` member; anything else,
// including an object without one, answers "" and lets that guard catch it.
function extractId(value) {
    if (value === undefined || value === null)
        return ""
    if (typeof value === "object") {
        if (!("id" in value) || value.id === undefined || value.id === null)
            return ""
        return String(value.id)
    }
    return String(value)
}

// folderName is the series title as it will read on the tablet.
//
// Trimmed, collapsed and capped — the user reads this name, so it is the
// series' own name and not a scheme. The cap is generous enough for a long
// title and short enough that a folder list stays legible; the characters a
// path cannot carry are dropped rather than substituted, because a title with
// a slash in it is not improved by inventing punctuation for it.
function folderName(title) {
    var s = String(title === undefined || title === null ? "" : title)
    s = s.replace(/[\/\\\n\r\t]+/g, " ").replace(/\s+/g, " ").trim()
    if (s.length > 60)
        s = s.substring(0, 60).trim()
    return s
}
