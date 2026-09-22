// Which layout a screen is in — PLAN §7.1 type 75, "grid" or "list".
//
// The values live in the backend's store and arrive on the **Pong status**,
// never in a reply of their own (backend/service/views.go). That is deliberate
// and it is the whole design: the screen draws what the store says rather than
// what it hoped, so a write that did not save cannot leave the switch showing a
// layout nothing is stored for. The robots.txt switch works exactly this way
// and for exactly this reason (ui/Settings.qml).
//
// It is a .js library rather than functions inside Main.qml because this is
// the part with behaviour worth asserting on its own, and a library can be
// driven against a plain object by build/qmlcheck/Harness.qml. (Main.qml is
// itself loaded and driven, by build/qmlcheck/MainHarness.qml. It was long
// said here that it could not be, "because it imports the Backend plugin" —
// that was wrong: Backend is a plain QML file in Annex's lib/, and the only
// obstacle was the relative path that import resolves through.)
.pragma library

var GRID = "grid"
var LIST = "list"

// wireName maps a screen name in ui/ onto the name in MessageSetView's
// {screen, view}, or "" for the screens that have nothing to remember.
//
// "browse" is "search" on the wire: that screen is one source's catalogue *and*
// its search results, which are the same tiles in the same layout, so they
// share one setting. The mapping is spelled out rather than derived for the
// reason backend/state/settings.go gives about the same three strings — they
// are wire values, and a screen renamed in the frontend must not silently start
// reading a different setting.
// "searchall" — one query across every source (ui/SearchAll.qml) — shares it,
// because a combined search is a search: the same tiles, the same rows, the
// same switch in the same place. A user who set results to rows did not mean
// "rows, except when I search everything".
function wireName(screen) {
    if (screen === "browse" || screen === "searchall")
        return "search"
    // The private Downloaded / Watching screens (round 2) share their
    // ordinary counterpart's setting: one layout preference per kind of
    // screen, not a second one for private mode nobody asked to set
    // separately.
    if (screen === "downloaded" || screen === "downloadedPrivate")
        return "downloaded"
    if (screen === "watching" || screen === "watchingPrivate")
        return "watching"
    return ""
}

// remembers says whether a screen has a layout to offer at all, which is what
// decides whether the header draws the switch.
function remembers(screen) {
    return wireName(screen) !== ""
}

// normalise applies the default of grid.
//
// Anything that is not "list" is grid: an absent value, a value from a settings
// file hand-edited into nonsense, and a value from a newer Quire that knows a
// third layout. backend/state/settings.go's View() takes exactly this line on
// exactly this data, and for the same reason — a screen drawn the usual way is
// better than a screen not drawn at all.
function normalise(view) {
    return view === LIST ? LIST : GRID
}

// fromStatus reads one screen's layout off a Pong status.
//
// A status that has not arrived yet is grid, which is what makes a screen shown
// before the first Pong draw rather than flicker: the frontend pings on
// startup, but the tiles are on screen before the answer is.
function fromStatus(status, screen) {
    var name = wireName(screen)
    if (!name || !status || !status.views)
        return GRID
    return normalise(status.views[name])
}
