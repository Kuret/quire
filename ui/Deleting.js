// Deleting one document off the tablet, and what may be claimed about it.
//
// PLAN §12.4. The calls belong to xochitl and live in ReaderHandoff.qml; the
// order they go in, and the reading-back between them, live here so that the
// offscreen harness can drive every path — including the ones that only happen
// when the device says no, which are the ones worth having.
//
// # Two steps, because one is silently not enough
//
//   explorer.selection.add(uuid); explorer.selectionMoveToTrash()
//   LibraryController.deleteEntries([entry.id])
//
// A live entry cannot be deleted. Measured on hardware 2026-09-17: the delete
// on an entry still in the library is *accepted and ignored* — no throw, no
// complaint, nothing removed. Only an entry already in the Trash goes, and then
// only that one. So the trash step is not a nicety before the delete, it is the
// delete's precondition, and the parent is read back between them rather than
// assumed.
//
// **And only that one.** A control folder sat in the Trash while two siblings
// were deleted beside it and was untouched. That measurement is what the
// confirmation sentence now asserts — the rest of the user's Trash is theirs —
// so it is the reason this no longer calls removeAllTrashed().
//
// # Why the id goes through the entry
//
// See idFor below. Short version: on 3.25 `entry.id` and the uuid are the same
// string, and the indirection is still right.
//
// # The answers
//
// A word, not a flag, because the outcomes want different things from the
// caller:
//
//   "ok"     deleted, and gone from the Trash too
//   "kept"   in the Trash and staying there: out of the library, not destroyed
//   "gone"   it was already not on the tablet — §6 M6 has the wording for that
//   "failed" nothing moved; the caller must change nothing

// deleteDocument performs the delete and reports what it observed.
//
// `api` is the device: exists, select, moveToTrash, clearSelection, parentOf,
// idFor and deleteEntries. Every one of them may throw, and a throw is the
// caller's "failed" rather than an exception that escapes into the UI.
function deleteDocument(api, uuid) {
    if (!api || !uuid)
        return "failed"

    try {
        if (!api.exists(uuid))
            return "gone"

        // One document, chosen explicitly. Clearing first means an earlier
        // selection left behind by the navigator cannot be swept into this
        // delete — the user asked for one row.
        api.clearSelection()
        if (api.select(uuid) !== 1) {
            // The id did not take. Leaving a half-made selection behind is how
            // the navigator ends up disagreeing with itself.
            api.clearSelection()
            return "failed"
        }

        api.moveToTrash()
        var left = api.selectionSize()

        // Always, whatever happened: nothing should stay selected under the
        // user (PLAN §12.4's implementation notes).
        api.clearSelection()

        if (left !== 0) {
            // The move did not take. The probe hit exactly this by trashing a
            // UUID that no longer existed, so it is a real state and not a
            // defensive flourish.
            return "failed"
        }

        // The precondition, read off the library rather than inferred from a
        // call that returned nothing. If the document is not in the Trash, the
        // delete below would be the silent no-op, and reporting it as a delete
        // would tell the user their download is gone when it is sitting in
        // their library.
        if (api.parentOf(uuid) !== "trash")
            return api.exists(uuid) ? "failed" : "ok"

        return remove(api, uuid)
    } catch (e) {
        // Every early return above clears the selection before it leaves, and a
        // throw must not be the one path that does not: a half-made selection
        // left behind is exactly what made the navigator disagree with itself
        // the first time (PLAN §12.4).
        try {
            api.clearSelection()
        } catch (ignored) {}
        console.log("[quire] deleting failed: " + e)
        return "failed"
    }
}

// remove is the second step: out of the Trash for good.
//
// Everything it can go wrong with ends as "kept", never as "failed". The
// document is out of the user's library either way, the record has been dropped
// on the strength of that, and calling it a failure would tell them their
// download survived when it did not. What is lost is only the permanence, and
// the backend has a sentence for exactly that.
function remove(api, uuid) {
    var id = idFor(api, uuid)
    if (!id)
        return "kept"

    try {
        api.deleteEntries([id])
    } catch (e) {
        console.log("[quire] a trashed document could not be removed: " + e)
        return "kept"
    }

    // deleteEntries returns nothing and does not complain about an argument it
    // will not act on — passing the entry *object* instead of its id is
    // accepted and ignored, measured. So the only honest report is the state
    // afterwards.
    return api.exists(uuid) ? "kept" : "ok"
}

// idFor is the entry id to hand to deleteEntries.
//
// **On 3.25 this is the same string as the uuid**, so the indirection looks
// like superstition. It is not: rm-librarian's 3.28 work had to introduce this
// exact mapping because the controller stopped accepting raw UUIDs there. It
// costs one already-working call today on 3.25 and 3.27, and it is already the
// 3.28 form. Compatibility is rarely this cheap; it is taken while it is.
//
// An entry that cannot be resolved returns "" and the caller reports "kept".
// Guessing the uuid would work on 3.25 and be the silent no-op on 3.28, which
// is the failure this whole file is written around.
function idFor(api, uuid) {
    var id
    try {
        id = api.idFor(uuid)
    } catch (e) {
        return ""
    }
    return id ? String(id) : ""
}

// deleteMany deletes several documents and reports each one separately.
//
// The downloaded overview's row delete (PLAN §12.5): one action for the user,
// several documents underneath. **Each document is reported on its own**,
// because each fails on its own — a single flag for the batch would have to lie
// about one end of a partial run or the other, and the backend composes the
// "five of seven" sentence from exactly these entries.
//
// It does not stop at the first failure. The documents are independent, and a
// run that abandons four deletable downloads because the first one would not
// move leaves the user worse off than one that carries on and says so.
//
// "gone" — a document already off the tablet — counts as trashed and removed.
// It is not on the reMarkable, which is the state the user asked for, and
// reporting it as a failure would keep a record for a document that does not
// exist.
function deleteMany(api, uuids) {
    var out = {results: [], deleted: 0, kept: 0, failed: 0}
    if (!uuids)
        return out

    for (var i = 0; i < uuids.length; ++i) {
        var uuid = uuids[i]
        if (!uuid)
            continue
        var word = deleteDocument(api, uuid)
        var trashed = word === "ok" || word === "kept" || word === "gone"
        var removed = word === "ok" || word === "gone"
        out.results.push({documentUuid: uuid, trashed: trashed, removed: removed})
        if (!trashed)
            out.failed += 1
        else if (!removed)
            out.kept += 1
        else
            out.deleted += 1
    }
    return out
}
