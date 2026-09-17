// Which library implementation is live, and what it can promise.
//
// Quire drives xochitl's document library two ways:
//
//   **legacy** — xochitl's own private QML (`com.remarkable`), which is what
//   runs on 3.25 and on 3.27, measured on both. Folders come from
//   `Library.createCollectionWrapper`, moves from `explorer.selectionMove`, and
//   deleting a download means trashing it and then emptying the Trash, because
//   that is the only tool this API has.
//
//   **librarian** — the rm-librarian xovi extension, reached through
//   `XoviMessageBroker.sendSimpleSignal`. Folders, moves and *per-document*
//   deletes, with the version-specific part living in an upstream `.so` that is
//   swapped rather than in private API names we re-derive ourselves.
//
// # Both are first-class, and that is not a formality
//
// Legacy is not a fallback for a hypothetical future: it is what the user's
// device runs today, and what 3.27 runs tomorrow. librarian is an extension
// that can be absent. Neither may lose the app a feature.
//
// # The part that must not be papered over
//
// **The two do not have the same semantics.** `deleteEntry` removes one
// document; the legacy path empties the whole Trash, including anything of the
// user's that was already in it. Every sentence shown to the user is composed
// in the backend (PLAN §2), so the backend has to *know which path is live* to
// compose an honest confirmation. Hiding both behind one `Delete()` is exactly
// the drift this arrangement exists to prevent, so the capability is reported
// rather than assumed.
.pragma library

// Capability names, as reported to the backend.
var LEGACY = "legacy"
var LIBRARIAN = "librarian"

// describe reports what is available and what it promises, for the backend to
// compose sentences from.
//
// `api.broker()` returns the broker object or null. A broker that exists is not
// yet proof: `sendSimpleSignal` returns the extension's reply **only when
// exactly one extension answers**, so an empty string means librarian is not
// loaded. That is checked with a read-only signal rather than assumed.
function describe(api) {
    var out = {
        path: LEGACY,
        // deletesOneDocument is the fact the confirmation copy turns on: with
        // librarian a delete removes the document, without it a delete empties
        // the reMarkable's Trash as well.
        deletesOneDocument: false,
        detail: ""
    }

    var reply = probe(api)
    if (!reply.ok) {
        out.detail = reply.text
        return out
    }
    out.path = LIBRARIAN
    out.deletesOneDocument = true
    out.detail = "librarian answered"
    return out
}

// probe asks librarian something read-only and reports whether it answered.
//
// `lookupEntry` on a name that does not exist is the safest question there is:
// it reads metadata, writes nothing, and a well-formed refusal ("ERROR: not
// found") is as good an answer as a UUID — it proves the extension is there.
function probe(api) {
    if (!api || typeof api.send !== "function")
        return {ok: false, text: "no broker on this build"}

    var reply
    try {
        reply = api.send("lookupEntry", "quire-probe-no-such-entry")
    } catch (e) {
        return {ok: false, text: "the broker threw: " + String(e).substring(0, 80)}
    }
    var text = reply === undefined || reply === null ? "" : String(reply)
    if (text === "") {
        // hitCount != 1 inside the broker: nothing answered.
        return {ok: false, text: "no extension answered"}
    }
    return {ok: true, text: text.substring(0, 80)}
}

// reply reads one librarian answer into a result the caller can act on.
//
// Three outcomes, and they are deliberately not two: a UUID or `ok` is success,
// `ERROR: …` is a refusal the extension understood, and an empty string is
// nobody answering at all. The third is the one that must never be mistaken for
// either of the others.
function reply(raw) {
    var text = raw === undefined || raw === null ? "" : String(raw)
    if (text === "")
        return {ok: false, answered: false, text: "no extension answered"}
    if (text.indexOf("ERROR") === 0)
        return {ok: false, answered: true, text: text}
    return {ok: true, answered: true, text: text}
}

// argsFor joins parameters for the broker's text protocol.
//
// **There is no escaping in librarian's parser** (`findArgSeparator`): it splits
// at offset 36 when the first argument is a UUID, and otherwise at the *last*
// comma. So the rule, and it is the whole of the rule:
//
//   - the first argument is always a UUID where one exists, which makes the
//     split exact whatever the second argument contains;
//   - a second argument is always passed when the first could contain a comma,
//     so the last comma is ours rather than the name's;
//   - names never contain `/` (a path separator to librarian) or a newline (the
//     broker's frame terminator). folderName already strips both, which makes
//     that stripping load-bearing rather than cosmetic.
//
// A colon is safe: the broker splits the signal from the parameters at the
// *first* colon, which is its own.
function argsFor(parts) {
    return parts.join(",")
}

// ---- deleting one document, on the librarian path --------------------------
//
// **`ok` means the call was accepted, not that anything happened.** Measured
// twice on hardware, 2026-09-17: `deleteEntry` on a live folder returned `ok`
// and changed nothing — the `.metadata` was untouched and survived a restart —
// and the same on a live document, with all three files still on disk. So every
// result here is read back from state, never taken from the reply. That is the
// same rule the legacy QML path already follows, and it now has two independent
// reasons behind it.
//
// **The precondition is the finding.** `deleteEntry` only removes an entry that
// is *already in the Trash*, so deleting is two calls:
//
//	trashEntry:<uuid>    → parent becomes "trash"     (verify)
//	deleteEntry:<uuid>   → only <uuid>.tombstone left (verify)
//
// Measured with a second entry sitting in the Trash beside it: that one
// survived. **That is what makes the new confirmation sentence true** — this
// removes one document and leaves the user's Trash alone, where the legacy path
// has to empty the whole Trash to remove anything at all.
//
// `api.parentOf(uuid)` is the second opinion, read through whatever the caller
// has: on 3.25 and 3.27 that is `Library.parentIdForId`.
function deleteOne(api, uuid) {
    var out = {ok: false, trashed: false, detail: ""}
    if (!uuid) {
        out.detail = "no document to delete"
        return out
    }
    if (!api || typeof api.send !== "function") {
        out.detail = "librarian is not available"
        return out
    }

    // Step one: into the Trash, and read the parent back rather than believing
    // the reply.
    var trashed = reply(send(api, "trashEntry", uuid))
    if (!trashed.ok) {
        out.detail = "could not trash it: " + trashed.text
        return out
    }
    if (parentOf(api, uuid) !== "trash") {
        out.detail = "the call said ok and the document is not in the Trash"
        return out
    }
    out.trashed = true

    // Step two: remove that one entry. A bare deleteEntry without the trash
    // step is the silent no-op above, which is why these are one operation.
    var removed = reply(send(api, "deleteEntry", uuid))
    if (!removed.ok) {
        out.detail = "could not delete it: " + removed.text + "; it is in the Trash"
        return out
    }
    if (exists(api, uuid)) {
        out.detail = "the call said ok and the document is still there; it is in the Trash"
        return out
    }

    out.ok = true
    out.detail = "deleted"
    return out
}

function send(api, signal, params) {
    try {
        return api.send(signal, params)
    } catch (e) {
        return ""
    }
}

function parentOf(api, uuid) {
    if (!api || typeof api.parentOf !== "function")
        return ""
    try {
        var p = api.parentOf(uuid)
        return p === undefined || p === null ? "" : String(p)
    } catch (e) {
        return ""
    }
}

function exists(api, uuid) {
    if (!api || typeof api.exists !== "function")
        return false
    try {
        return api.exists(uuid) ? true : false
    } catch (e) {
        // Unable to check is not the same as gone, and this function is asked
        // "is it still there?" in a context where a wrong "no" would report a
        // delete that did not happen.
        return true
    }
}
