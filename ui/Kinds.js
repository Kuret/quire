// What a row stands for — a page-based series, or a book.
//
// Every source Quire had until now was page-based: a series of chapters, each
// of which is a run of images. A Shelfmark source is not. It publishes *books*,
// and what looks like a chapter list there is a list of **releases** — the
// files one book is available as, of which the reader picks one.
//
// # The field, and what its absence means
//
// The backend puts a plain `kind` string on search results, on the combined
// search's groups and on downloaded rows: "manga" or "book". **It may be
// absent, and absent means manga** — every source that existed before books did
// omits it, so a missing field is the old world rather than an unknown one.
// That rule is `of()` below and it is the only place it is written down: a
// second `row.kind === "book"` test somewhere else is how "absent" eventually
// comes to mean "neither".
//
// # Why the words are here
//
// PLAN §2 keeps *sentences* in the backend, and everything the reply carries as
// a sentence is still drawn verbatim. What is composed here is the same class
// of thing as "Select" or "3 selected": labels on a control the backend does
// not know exists, and a line describing the state of that control. The filter
// is transient and lives entirely in the frontend — there is no setting behind
// it — so there is nothing for the backend to have an opinion about.
//
// It is a .js library rather than functions inside a screen because this is the
// part with behaviour worth asserting on its own, and a library can be driven
// directly by build/qmlcheck/Harness.qml — which is where "absent means manga"
// is pinned.
.pragma library

var MANGA = "manga"
var BOOK = "book"

// The third value the filter can take, and the one it starts at. Not a kind: no
// row is ever "all", and it is only ever a filter.
var ALL = "all"

// Same separator as Grouping.js, and for the same reason: U+00B7 is in the
// fonts on the device, which most of the punctuation one would reach for is
// not (build/qml-check.sh audits it).
var SEPARATOR = " · "

// normalise folds anything that is not the book kind onto manga — an absent
// field, a kind from a newer backend, a settings file edited into nonsense.
// The same line Views.js takes on layouts, for the same reason: a row drawn the
// usual way is better than a row not drawn at all.
function normalise(kind) {
    return kind === BOOK ? BOOK : MANGA
}

// of is the kind of a row, a group or a reply object. **This is the one place
// "absent means manga" is decided.**
function of(row) {
    return row ? normalise(row.kind) : MANGA
}

function isBook(row) {
    return of(row) === BOOK
}

// matches is the filter itself. Anything that is not one of the two kinds —
// including ALL, and including a value nothing set — lets every row through,
// so a filter that somehow went wrong shows too much rather than nothing.
function matches(filter, kind) {
    if (filter !== MANGA && filter !== BOOK)
        return true
    return normalise(kind) === filter
}

// filters is the control's three segments, in the order they are drawn. A
// function rather than a var: a .pragma library shares one copy of its state
// across every importer, and a shared array is one splice away from being a
// different control on two screens.
function filters() {
    return [{"kind": ALL, "label": "All"},
            {"kind": MANGA, "label": "Manga"},
            {"kind": BOOK, "label": "Books"}]
}

// mark is the word that marks a row as a book, and nothing at all for the
// kind everything used to be.
//
// A word, not a colour and not a glyph: colour on this panel is a solid area
// only (ui/Style.js), and a mark that were only a fill would leave "is this a
// book?" answerable only by hue. A manga row wears none — a mark on every row
// is a mark nobody reads, which is the same rule the multi-source badge keeps.
function mark(row) {
    return isBook(row) ? "Book" : ""
}

// filterLine says, in words, that a filter is on.
//
// **It is not decoration.** A filtered screen and a search that found nothing
// look identical otherwise, and the second one is the one people report as
// broken. So the line is there whenever the filter is not ALL — because
// "Showing books only" with nothing under it is the answer to "why is this
// empty?", and a line that appeared only sometimes would be missing exactly
// then.
//
// There is no longer a count of results the filter is "holding back": the
// filter is part of the search itself (backend/service/searchall.go), not a
// hide applied to a page that was fetched unfiltered, so there is nothing on
// the page in hand for a hidden count to be worked out against.
function filterLine(filter) {
    if (filter !== MANGA && filter !== BOOK)
        return ""
    return filter === BOOK ? "Showing books only." : "Showing manga only."
}

// emptyLine is what an empty page of results says.
//
// The filter is now part of the search itself (backend/service/searchall.go):
// a Books search that came back empty found no books, full stop — there is no
// separate "the filter hid everything" case any more, because nothing outside
// the chosen kind was ever asked for. The wording still names the filter when
// one is on, because "No books in these results" answers the question a bare
// "Nothing came back for that" would leave open: whether trying All might find
// something.
function emptyLine(filter) {
    if (filter === BOOK)
        return "No books in these results."
    if (filter === MANGA)
        return "No manga in these results."
    return "Nothing came back for that."
}

// kindFor looks a row's kind up in a model by whichever fields identify it.
//
// The series screen is told what it is showing by the row that opened it, not
// by a reply of its own: MessageSeriesDetailResult carries no kind, and
// inventing one on the wire to save this lookup would be the frontend adding a
// field to the protocol. The models are already in hand and already keyed —
// a group by its key, a downloaded row by its (source, series) pair.
//
// A row that is not there at all is manga, which is the same default an absent
// field has: a series reached from the watched list, whose rows carry no kind
// role whatsoever, lands here.
function kindFor(model, fields) {
    for (var i = 0; model && i < model.count; ++i) {
        var row = model.get(i)
        var hit = true
        for (var key in fields) {
            if (row[key] !== fields[key]) {
                hit = false
                break
            }
        }
        if (hit)
            return of(row)
    }
    return MANGA
}
