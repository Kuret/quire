.import "Deleting.js" as Deleting

// Answering the backend, including when the device throws.
//
// Every one of these is a question the backend asked and is waiting on. The
// backend's timeouts are backstops — "no answer about a filing" after thirty
// seconds — and a backstop that is reached routinely is a backstop being used
// as a mechanism.
//
// # The bug this file exists to stop
//
// On OS 3.27 a download's filing pass sent `MessageSortDocuments` and **nothing
// came back at all**: not a failure, nothing, until the ceiling. The frontend
// handler called into `ReaderHandoff.qml` with no protection, the call threw,
// the exception unwound past the `send` that would have reported it, and the
// backend was left waiting on a question the frontend had already given up on.
// The error text went to the QML console, which nobody is reading on a tablet.
//
// So: **a handler that was asked a question always answers.** The negative
// replies below are the ones each caller already knew how to send — they are
// what a missing bridge produces — and the exception text rides along in
// `detail`, where the backend logs it. A throw becomes a *report* of a throw.
//
// # Why these are here and not in Main.qml
//
// Main.qml cannot be instantiated off-device: it imports AppLoad. These can,
// so the harness can hand each one a bridge that throws and assert that an
// answer still comes out — which is the whole behaviour.

// noBridge is the detail for a frontend that cannot reach xochitl's QML at all.
// It is a different situation from a call that threw, and the log should be
// able to tell them apart.
var NO_BRIDGE = "this build cannot reach the library"

// threw wraps an exception into something a log line can carry.
//
// Truncated, because this ends up in a message payload and an enormous QML
// stack trace in a protocol frame helps nobody; the first line is the part that
// names the call.
function threw(e) {
    return "the device threw: " + String(e).substring(0, 200)
}

// sorted answers MessageSortDocuments.
//
// Every failure ends the same way: the documents stay in Comics, which is where
// every download went before series folders existed. The backend is told so it
// can stop recording a folder that is not there — never so it can call the
// download a failure.
function sorted(bridge, msg) {
    var request = msg ? msg : {}
    if (bridge) {
        try {
            return bridge.sort(request)
        } catch (e) {
            return sortFailure(request, threw(e))
        }
    }
    return sortFailure(request, NO_BRIDGE)
}

function sortFailure(msg, detail) {
    return {
        "documentUuids": msg.documentUuids ? msg.documentUuids : [],
        "moved": [],
        "folderId": msg.folderId ? msg.folderId : "",
        "folderName": msg.folderName ? msg.folderName : "",
        "created": false,
        "detail": detail
    }
}

// checked answers MessageCheckDocuments.
//
// **The failure answer is explicit.** `checked: false` with an empty `missing`
// is the only safe shape: an empty `missing` on its own would be read as "none
// of them are missing", and the backend acts on that by deleting every record
// and the whole page cache (PLAN §12.4).
function checked(bridge, uuids) {
    var ids = uuids ? uuids : []
    if (bridge) {
        try {
            var answer = bridge.check(ids)
            // A bridge that answered with nothing has not answered. Its own
            // answer, whatever it says, is passed through untouched: deciding
            // here that a `checked: false` reply "really means" something else
            // would be a second opinion about a question only it can ask.
            if (answer)
                return answer
            return checkFailure(ids, "the library did not answer")
        } catch (e) {
            return checkFailure(ids, threw(e))
        }
    }
    return checkFailure(ids, NO_BRIDGE)
}

function checkFailure(uuids, detail) {
    return {"checked": false, "documentUuids": uuids, "missing": [], "detail": detail}
}

// trashed answers for one document: the word, and why if it is bad news.
//
// "failed" means nothing moved and the backend must change nothing — which is
// exactly what a throw means, so a throw is reported as "failed" with the text
// attached rather than as silence.
function trashed(bridge, uuid) {
    if (bridge) {
        try {
            return {"result": bridge.trash(uuid), "detail": ""}
        } catch (e) {
            return {"result": "failed", "detail": threw(e)}
        }
    }
    return {"result": "failed", "detail": NO_BRIDGE}
}

// trashedMany answers for a series' documents.
//
// A throw is one failure per document rather than one for the batch: the
// backend drops a record only for a document reported as trashed, so a vague
// batch failure and a per-document failure lead to the same records surviving —
// but only the per-document form keeps the counting honest in the sentence the
// user reads.
function trashedMany(bridge, uuids) {
    var ids = uuids ? uuids : []
    if (bridge) {
        try {
            var outcome = bridge.trashMany(ids)
            outcome.detail = ""
            return outcome
        } catch (e) {
            return manyFailure(ids, threw(e))
        }
    }
    return manyFailure(ids, NO_BRIDGE)
}

// manyFailure is the same shape a run against no library produces, which is
// already written once in Deleting.js: one result per document, every one of
// them a failure. Composing it here by hand would be a second answer to "what
// does a failed batch look like".
function manyFailure(uuids, detail) {
    var outcome = Deleting.deleteMany(null, uuids)
    outcome.detail = detail
    return outcome
}

// handoff answers the reader handoff, and says what the shell does next.
//
// Three outcomes in one object because they are one decision:
//
//   reply   what the backend is told. A document that did not open is reported
//           `missing`, and the backend's wording for that is the right answer
//           whether the cause was a UUID that no longer resolves or a call that
//           threw.
//   forget  clear the dead UUID off the rows, so the button goes back to
//           offering a download.
//   close   **unload the frontend, so the reader is not opened behind it.**
//           AppLoad v0.5.3 draws its windows above the document view, so on OS
//           3.27 the stock reader comes up *behind* Quire and the user has to
//           quit the app to read what they just tapped Read on.
//
// `close` is true only on a successful open. A failed open leaves the app up,
// because the sentence explaining it is on that screen and closing would take
// it away with the app.
//
// It is here rather than in Main.qml so the harness can drive it: Main.qml
// imports AppLoad and cannot be instantiated off a device.
function handoff(bridge, uuid, page) {
    var opened = false
    var detail = NO_BRIDGE
    if (bridge) {
        detail = ""
        try {
            opened = bridge.open(uuid, page) ? true : false
        } catch (e) {
            opened = false
            detail = threw(e)
        }
    }

    if (opened)
        return {"reply": {"documentUuid": uuid}, "forget": false, "close": true}
    return {
        "reply": {"documentUuid": uuid, "missing": true, "detail": detail},
        "forget": true,
        "close": false
    }
}
