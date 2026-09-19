// What a cover request is made of — the one place a batch is built.
//
// **One message, however many sources.** Each entry names its own source, and
// the backend honours that per tile. Splitting a batch into one message per
// source was a real bug rather than a style choice: the backend cancels the
// batch before it, deliberately, because a page turn makes the covers for the
// tiles that went work nobody will see. That reasoning holds only while a batch
// is the *whole* visible set, so split across three messages each one cancelled
// the last — a three-source Downloaded screen fetched one source's covers and
// quietly abandoned the other two, and because it turned on which fetch won the
// race it looked flaky rather than broken. There is a regression test on the
// backend's half of it (backend/service/covers_batch_test.go).
//
// It lives here rather than in Main.qml because there were two implementations
// of it: the screens that span sources went through Main's
// requestCoversBySource, and the per-source search grid built its own message
// inline with the source at the top level instead of on each entry. The second
// worked — the backend falls back to the message-level source — but "the one
// place a batch is built" is only true if there is one, and the list rows
// asking for covers would otherwise have made a third.
//
// The empty case is the sharp one: **an empty batch is not sent at all.** The
// backend cancels the batch in flight before it looks at whether the new one is
// empty, which is right for a screen that genuinely has no tiles and wrong for
// a screen that has not been filled yet — and the screens say "nothing" by
// reporting an empty set rather than by staying quiet, so without this the
// empty set a screen reports at startup would cancel a page of covers.
.pragma library

// request is the whole message a screen's report becomes, or **null** when
// there is nothing to ask for. Null rather than an empty message on purpose:
// the caller sends what this returns and nothing else, so the decision not to
// send lives here, next to the reason for it, rather than as a length test in
// the caller that the next screen would have to remember to copy.
function request(covers, fallbackSourceId) {
    var want = batch(covers, fallbackSourceId)
    return want.length > 0 ? {"covers": want} : null
}

// batch turns what a screen reported into the entries of one RequestCover, or
// an empty array when there is nothing worth asking for.
//
// `fallbackSourceId` is for the screens that show one source at a time: their
// rows carry no source of their own because every row has the same one, and
// which source that is, is Main.qml's to know. An entry that names its own
// source keeps it.
function batch(covers, fallbackSourceId) {
    var want = []
    var fallback = fallbackSourceId ? String(fallbackSourceId) : ""
    for (var i = 0; i < (covers ? covers.length : 0); ++i) {
        var c = covers[i]
        if (!c || !c.url)
            continue
        var sourceId = c.sourceId ? String(c.sourceId) : fallback
        // No source, no request: the backend would have nothing to fetch it
        // with, and a cover nobody can fetch is a row the placeholder covers.
        if (sourceId.length === 0)
            continue
        want.push({"sourceId": sourceId, "seriesId": c.seriesId, "url": c.url})
    }
    return want
}
