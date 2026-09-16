// Applying PLAN §12.2's watch rows to a model without disturbing the page the
// user is standing on.
//
// This is a library rather than functions inside Main.qml for one reason: it is
// the only part of the watched-series UI with behaviour worth asserting, and a
// .js file can be driven by build/qmlcheck/Harness.qml against a real
// ListModel. It decides nothing about wording — every string it moves was
// composed in backend/service/watch.go and is copied across untouched (PLAN §2).
//
// The behaviour it exists to get right:
//
//   - **A check round streams in.** PLAN §12.2 deliberately does not wait for a
//     complete set, so updates land one at a time while the user is reading a
//     page. A row is therefore written *in place* and never moved; a result for
//     a series on another page costs the current page nothing.
//   - **WatchList is not a replacement.** It arrives on attach, after every
//     watch and unwatch, and again at the end of a round. Clearing and
//     refilling would blank and redraw the whole list several times a round, on
//     a panel where every redraw is a ghost. The backend sends rows in stored
//     order, so matching by id and writing only what differs leaves every row
//     where it was.
.pragma library

// summaryOf pulls PLAN §12.2's at-a-glance strings off a WatchList message.
//
// The counts beside them (seriesWithNew, newChapters, failed) are deliberately
// not read: they exist so a caller can decide *whether* to show something, and
// "short" and "phrase" being empty already says that. Nothing here does
// arithmetic on them, because the moment it did it would be composing, and the
// backend composes (PLAN §2).
function summaryOf(msg) {
    var s = msg && msg.summary ? msg.summary : null
    return {
        "short": s && s.short ? s.short : "",
        "phrase": s && s.phrase ? s.phrase : ""
    }
}

// applySummary writes the two strings onto target, and only where they differ.
// It reports whether it wrote anything.
//
// The guard is the point, not an optimisation. WatchList arrives on attach,
// after every watch and unwatch, and again at the end of every check round — so
// a round that ends with the same summary it started with must produce no
// visible change at all. Without this, finishing a round would flash the
// "Watching" button on the source-list screen, which is usually not even the
// screen the user is looking at the list on, and on e-ink a flash for no news
// is worse than no indicator.
function applySummary(target, msg) {
    var next = summaryOf(msg)
    var wrote = false
    if (target.watchShort !== next.short) {
        target.watchShort = next.short
        wrote = true
    }
    if (target.watchPhrase !== next.phrase) {
        target.watchPhrase = next.phrase
        wrote = true
    }
    return wrote
}

// key identifies a watch. A series can be watched on two sources at once, so it
// takes both halves.
function key(sourceId, seriesId) {
    return sourceId + "\u0000" + seriesId
}

// row normalises one backend watch row into model properties. Absent fields
// become empty rather than undefined, because a ListModel role has to exist
// from the first append or later writes to it are dropped.
function row(w) {
    return {
        "sourceId": w && w.sourceId ? w.sourceId : "",
        "seriesId": w && w.seriesId ? w.seriesId : "",
        "sourceName": w && w.sourceName ? w.sourceName : "",
        "title": w && w.title ? w.title : "",
        "newChapters": w && w.newChapters ? w.newChapters : 0,
        // Present when there is a count, absent when there is none — and
        // deliberately still present on a failed row, whose count came from a
        // check that did succeed (PLAN §12.2).
        "badge": w && w.badge ? w.badge : "",
        "state": w && w.state ? w.state : "",
        "status": w && w.status ? w.status : "",
        "detail": w && w.detail ? w.detail : "",
        "checkedAt": w && w.checkedAt ? w.checkedAt : ""
    }
}

// indexOf finds a watch in the model, or -1.
function indexOf(model, sourceId, seriesId) {
    for (var i = 0; i < model.count; ++i) {
        var have = model.get(i)
        if (have.sourceId === sourceId && have.seriesId === seriesId)
            return i
    }
    return -1
}

// writeRow sets only what changed, so a series whose check came back identical
// does not repaint at all.
function writeRow(model, at, next) {
    var have = model.get(at)
    for (var k in next) {
        if (have[k] !== next[k])
            model.setProperty(at, k, next[k])
    }
}

// applyUpdate is one series landing mid-round.
function applyUpdate(model, w) {
    if (!w || !w.seriesId)
        return
    var next = row(w)
    var at = indexOf(model, next.sourceId, next.seriesId)
    if (at < 0)
        model.append(next)
    else
        writeRow(model, at, next)
}

// reconcile applies a whole list in place: existing rows are written where they
// stand, genuinely new ones are appended, and anything the backend no longer
// lists is dropped — unwatched, or removed along with its source.
function reconcile(model, list) {
    var incoming = list ? list : []
    var seen = {}
    for (var i = 0; i < incoming.length; ++i) {
        var next = row(incoming[i])
        seen[key(next.sourceId, next.seriesId)] = true
        var at = indexOf(model, next.sourceId, next.seriesId)
        if (at < 0)
            model.append(next)
        else
            writeRow(model, at, next)
    }
    for (var j = model.count - 1; j >= 0; --j) {
        var have = model.get(j)
        if (!seen[key(have.sourceId, have.seriesId)])
            model.remove(j)
    }
}
