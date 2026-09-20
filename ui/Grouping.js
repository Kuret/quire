// The rows of the combined search — Msg.SearchAll / SearchAllResults.
//
// Named for what it holds rather than for the screen that uses it
// (ui/SearchAll.qml), so the file and the QML type it serves cannot be
// mistaken for each other in an import line.
//
// The backend does the searching, the merging and the paging: a **group** is
// one series as the user sees it, already merged across sources by normalised
// title, and `matches` is already in the user's configured source order. So
// this file does not decide what belongs together. It decides only what a row
// made of a group *shows*, which is the one part of the reply that has no
// composed string in it.
//
// It is a .js library rather than functions inside Main.qml because this is
// the part with behaviour worth asserting on its own, and a library can be
// driven against a bare ListModel by build/qmlcheck/Harness.qml. (Main.qml is
// itself loaded and driven, by build/qmlcheck/MainHarness.qml. It was long
// said here that it could not be, "because it imports the Backend plugin" —
// that was wrong: Backend is a plain QML file in Annex's lib/, and the only
// obstacle was the relative path that import resolves through.)
//
// # The two composed strings, and why they are composed here
//
// PLAN §2 keeps sentences in the backend, and everything the reply carries
// *as* a sentence is drawn verbatim. These two are not sentences the backend
// has: they are descriptions of the merge itself — which sources a group came
// from, and which sources did not answer — and both are lists of names whose
// order and length come from the reply. Asking the backend to render them
// would mean it also deciding how many fit on a subtitle line, which is a
// question only the view can answer.
.pragma library
// Whether a group is a book, and the word that says so, are Kinds.js's — one
// place decides that an absent `kind` means manga, and it is not this file.
.import "Kinds.js" as Kinds

// SEPARATOR is what joins names on one line. U+00B7, as SourceList's
// "Watching · 3 new" already uses — it is in the device's fonts, which is not
// true of most of the punctuation one would reach for here (the backspace key
// was a tofu box for exactly this reason).
var SEPARATOR = " · "

// matchesOf is a group's matches, never undefined.
//
// A group with no matches at all cannot happen — the backend builds a group
// out of matches — but a reply that arrived half-parsed, or from a backend
// older than this screen, must produce an empty row rather than throw halfway
// through filling the model.
function matchesOf(group) {
    return group && group.matches ? group.matches : []
}

// sourceLine names the sources a group was found in, in the order the reply
// gave them, which is the user's configured source order. It is the list
// layout's subtitle — the line SeriesGrid already keeps for one source's name.
function sourceLine(group) {
    var matches = matchesOf(group)
    var names = []
    for (var i = 0; i < matches.length; ++i) {
        var name = matches[i].sourceName ? matches[i].sourceName : ""
        if (name.length > 0)
            names.push(name)
    }
    return names.join(SEPARATOR)
}

// authorsLine names a group's authors, comma-joined — the same shape
// SeriesGrid's row carries (Main.qml's fillSeries), and for the same reason:
// a ListModel role cannot reliably hold a list, so the join happens once,
// here, rather than in the delegate.
function authorsLine(group) {
    var authors = group && group.authors ? group.authors : []
    return authors.join(", ")
}

// subtitleLine is the whole of a row's second line: the authors, if any, then
// what the group *is*, then where it was found.
//
// A book says so in a word before its sources, because "Shelfmark" is a source
// name like any other and nothing else on the row distinguishes a novel from a
// comic — the title and the cover arrive through the same fields. Manga says
// nothing extra: every row wearing a mark is a mark nobody reads, which is the
// rule the multi-source badge already keeps.
function subtitleLine(group) {
    var mark = Kinds.mark(group)
    var sources = sourceLine(group)
    var rest
    if (mark.length === 0)
        rest = sources
    else if (sources.length === 0)
        rest = mark
    else
        rest = mark + SEPARATOR + sources

    var authors = authorsLine(group)
    if (authors.length === 0)
        return rest
    if (rest.length === 0)
        return authors
    return authors + SEPARATOR + rest
}

// badgeFor marks a group that was found in more than one source, for the grid
// layout — where there is no subtitle line to put the names on and a tile is
// too small for them anyway.
//
// A book is marked here too, and in the same slot: a tile has no room for a
// second mark, and the two facts read perfectly well on one line.
//
// It reuses CoverGrid's badge corner, which exists for Watching's "3 new
// chapters". That is the whole reason the mark is a count rather than a row of
// small source names: the slot is already drawn, already legible on this
// panel, and already tested. A group found in one source wears none — a mark
// on every tile is a mark nobody reads.
function badgeFor(group) {
    var parts = []
    var mark = Kinds.mark(group)
    if (mark.length > 0)
        parts.push(mark)
    var n = matchesOf(group).length
    if (n > 1)
        parts.push(n + " sources")
    return parts.join(SEPARATOR)
}

// groupRow is one row of the model, with every role it will ever need.
//
// Every key is written on every row, including the empty ones: a ListModel
// fixes its roles on the first append and silently drops keys added later, so
// a row built from a single-source group is what would otherwise leave the
// whole model without a badge role.
//
// `sourceId` and `seriesId` are **matches[0]'s** — the source the reply put
// first, and so the one a tap opens. The rest of the matches are not in the
// row: they are what the series screen's switcher is built from, and a nested
// list inside a ListModel role is a second model to keep in step for no gain.
function groupRow(group) {
    var matches = matchesOf(group)
    var first = matches.length > 0 ? matches[0] : {}
    return {
        "key": group && group.key ? group.key : "",
        "title": group && group.title ? group.title : "",
        // The group's own cover, falling back to the first match's: the
        // backend picks the group cover from the matches, but a reply that
        // carried none still has one per match.
        "coverUrl": group && group.coverUrl ? group.coverUrl
                                            : (first.coverUrl ? first.coverUrl : ""),
        // Filled in later, when MessageCoverReady lands. The role has to exist
        // from the first append or that write is dropped.
        "coverPath": "",
        "sourceId": first.sourceId ? first.sourceId : "",
        "seriesId": first.seriesId ? first.seriesId : "",
        "sources": subtitleLine(group),
        "sourceCount": matches.length,
        // Authors alone, for the tile view's subtitle (ui/CoverGrid.qml),
        // which has no room for the source names the row's "sources" line
        // above carries — those stay on the badge (badgeFor).
        "authors": authorsLine(group),
        // What this group is. Carried on the row rather than looked up again
        // later: the series screen is told what it is showing by the row that
        // opened it, and the filter reads it off the model it is filtering.
        "kind": Kinds.of(group),
        "badge": badgeFor(group)
    }
}

// fill replaces the model with one page of groups and returns the matches,
// keyed by group, for the series screen's switcher.
//
// The two come out of one pass because they must not disagree: the switcher is
// looked up by the key on the row the user tapped, and a map built from a
// different reply than the rows is a switcher offering sources for a series
// nobody is looking at.
//
// `kind` is the screen's filter, and an absent one is "all" — every caller that
// does not filter gets exactly the page it sent. **Filtering happens here, over
// the reply already in hand, rather than as a request.** The backend merged and
// paged this page; asking it again for a subset would spend a fan-out to every
// configured site to hide rows that are already on the screen, and would turn a
// filter into something that can fail (PLAN §7.4).
//
// The matches are indexed for *every* group, filtered out or not. They are a
// lookup by the key of the row that was tapped, and a hidden row cannot be
// tapped; keeping the index whole means switching the filter never has to
// rebuild it.
function fill(model, msg, kind) {
    model.clear()
    var groups = msg && msg.groups ? msg.groups : []
    var matches = {}
    for (var i = 0; i < groups.length; ++i) {
        var row = groupRow(groups[i])
        if (Kinds.matches(kind, row.kind))
            model.append(row)
        matches[row.key] = matchesOf(groups[i])
    }
    return matches
}

// countOf is how many groups a reply held, before any filtering — the number
// the hidden count is worked out against.
function countOf(msg) {
    return msg && msg.groups ? msg.groups.length : 0
}

// matchesFor reads the switcher's list back out, or an empty list for a series
// that was not reached through a combined search at all. That case is the
// common one — browsing a single source — and it is what decides that the
// switcher is absent rather than showing one inert chip.
function matchesFor(index, key) {
    if (!index || !key || !index[key])
        return []
    return index[key]
}

// failedLine names the sources that did not answer.
//
// **This is not an error state.** A source that failed contributed no rows
// while the others filled the screen, so the results stand and this goes
// quietly underneath them. The per-source `message` is deliberately left out:
// it explains *why* one source failed, which is a paragraph on a line that has
// to hold several names — and the user's question here is only which of their
// sources this page is missing.
function failedLine(sourceErrors) {
    var errors = sourceErrors ? sourceErrors : []
    var names = []
    for (var i = 0; i < errors.length; ++i) {
        var name = errors[i].sourceName ? errors[i].sourceName : ""
        if (name.length > 0)
            names.push(name)
    }
    if (names.length === 0)
        return ""
    return "No answer from " + names.join(SEPARATOR)
}

// searchable says whether a query is worth sending.
//
// An empty query is **not** a browse here. One source's empty query means its
// catalogue, which is what SeriesGrid's "Latest" button asks for; the same
// thing across every source would be a fan-out of requests to every site the
// user has configured for a box they have not typed in yet (PLAN §7.4's
// politeness limiter would then serialise the lot in front of whatever they
// type next). Whitespace counts as empty for the same reason.
function searchable(query) {
    return !!query && String(query).trim().length > 0
}
