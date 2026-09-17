// Putting a finished download into its series folder — PLAN §6 M5, the
// conclusion corrected 2026-09-17.
//
// The two calls this drives are xochitl's own, proven on hardware:
//
//   Library.createCollectionWrapper(parentIdString, name)   -> new folder id
//   explorer.selectionMove(folderIdString)                  -> moves the selection
//
// **Both take id strings, and the parent slot is first.** An Entry object in
// the parent slot is *silently ignored* and the folder lands at root — no
// throw, no complaint, just the wrong answer — which is why every folder made
// here has its parent read back before anything is moved into it. Five probe
// rounds went into learning that; see PLAN §6 M5.
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
// `api` is the device, as five functions:
//
//   parentOf(id)            -> parent id string, "" for the root
//   createFolder(parent, n) -> new folder id string, or "" if it did not work
//   select(ids)             -> number of ids the selection actually took
//   move(folderId)          -> nothing; throwing is allowed
//   clearSelection()
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
    var took = 0
    try {
        took = api.select(out.documentUuids)
    } catch (e) {
        took = 0
    }
    if (took !== out.documentUuids.length) {
        api.clearSelection()
        out.detail = "the selection took " + took + " of " + out.documentUuids.length +
                     " documents; left where they are"
        return out
    }

    var threw = ""
    try {
        api.move(out.folderId)
    } catch (e) {
        threw = String(e)
    }
    api.clearSelection()

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
        return ""
    }
    if (!id)
        return ""

    var landed = parentOf(api, id)
    if (landed !== parent) {
        out.detail = name + " was created under " + describe(landed) +
                     " rather than " + describe(parent)
        return ""
    }
    out.created = true
    return id
}

function parentOf(api, id) {
    try {
        var p = api.parentOf(id)
        return p === undefined || p === null ? "" : String(p)
    } catch (e) {
        return ""
    }
}

function describe(id) {
    return id === ROOT ? "the top of My Files" : id
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
