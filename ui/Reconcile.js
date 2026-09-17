// Answering "which of these documents are still on the tablet?" — PLAN §12.4.
//
// The user can delete a download in xochitl, and then Quire holds a record for a
// document that is not there and a page cache nothing can reach. Only the
// frontend can check: `Library.entryForId(uuid)` is QML.
//
// # The rule this file exists to enforce
//
// **A frontend that cannot check reports that it could not check.** Not an
// empty list of missing documents — an explicit `checked: false`. The two are
// indistinguishable downstream if this gets it wrong, and they mean opposite
// things:
//
//	checked: true,  missing: []   -> every document is still there
//	checked: false, missing: []   -> I have no idea, do nothing
//
// The cost of confusing them is not a stale button. It is every record dropped
// and the whole page cache deleted, on the user's device, because a QML import
// stopped resolving. So any failure at all — no bridge, a call that throws, one
// lookup that misbehaves — abandons the whole answer rather than returning a
// partial one that looks complete.
//
// # What counts as missing
//
// Only a document `entryForId` cannot resolve. **Not** a document that has moved:
// the user filing a volume into a folder of their own is not the user deleting
// it, and folder membership is never the question asked.
//
// The logic lives here rather than in ReaderHandoff.qml because that file
// imports com.remarkable and cannot be loaded off the device; here the offscreen
// harness can drive every path, including the ones that only happen when the
// device says no.
.pragma library

// check asks `api.resolves(uuid)` for each id and reports what it found.
//
// `api.resolves` returns true when the document is still on the tablet. A throw
// from it is not a missing document — it is a failed check.
function check(api, uuids) {
    var out = {checked: false, documentUuids: uuids || [], missing: []}
    if (!out.documentUuids.length) {
        // Nothing to ask about is a complete answer, and a true one.
        out.checked = true
        return out
    }
    if (!api || typeof api.resolves !== "function") {
        return out
    }

    var missing = []
    for (var i = 0; i < out.documentUuids.length; ++i) {
        var id = out.documentUuids[i]
        var present
        try {
            present = api.resolves(id)
        } catch (e) {
            // One lookup failing makes the whole answer untrustworthy: the ids
            // after it are unexamined, and an answer that is partly guesswork
            // is not one the backend can act on.
            out.missing = []
            out.checked = false
            return out
        }
        if (!present)
            missing.push(id)
    }

    out.missing = missing
    out.checked = true
    return out
}
