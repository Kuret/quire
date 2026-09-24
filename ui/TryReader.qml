// The Try reader — milestone 1: read a chapter without downloading it and
// without a library entry, in a reader of Quire's own.
//
// # Full screen, standard e-reader shape (the owner's follow-up)
//
// The permanent header, pager bar and caption this screen used to draw are
// gone. The page image gets the whole panel — every pixel of chrome was a
// pixel of manga nobody could see — and Main.qml draws this over its own
// header rather than inside the ordinary `body` area (see the comment there)
// so there is truly nothing above it.
//
// Navigation is three tap zones the width of the screen (tapZones below):
// left turns back, right turns forward, the middle toggles a sparse overlay
// carrying the page number, the title and a way back. Nothing here is new
// gesture handling — it is three plain MouseAreas, the same control every
// other screen in ui/ uses, just arranged side by side instead of stacked.
// See zoneFraction for why the middle is the width it is.
//
// PLAN §12.1 still applies exactly as it always has: nothing scrolls, flicks
// or animates. The overlay does not fade in — it is there or it is not,
// which on e-ink is one repaint either way rather than a run of them.
//
// # Streaming, not a wall of "please wait"
//
// The backend opens on page 1 as soon as that one page exists, and the rest
// arrive as they are fetched (backend/service/tryreader.go). This screen
// reflects that directly: `pagePaths` is a sparse map of whatever has
// arrived so far, keyed by index, and turning to a page that is not in it
// yet shows a plain "Fetching…" line instead of a blank rectangle or a
// frozen control — never silently standing in for content. Nothing here
// waits for the whole chapter, or even asks the backend for more than the
// one page beyond wherever the reader is — Main.qml's onPageWanted handler
// is the only thing that ever asks for a page, and it asks for exactly one.
//
// # What is held decoded
//
// Exactly one Image element, bound to whatever page is on screen; its
// `cache: false` and the fact that its `source` simply changes on a page
// turn is what keeps the previous page's decoded texture from lingering —
// Qt drops it the moment nothing references it any more. The pages behind
// that, cached ahead by the backend, are JPEGs on disk (tryreader.Session's
// own bound: the one behind current), never image data held in this process.
//
// # Honesty, without a permanent line to carry it
//
// Full screen took away the one place the "nothing is saved" notice used to
// live. It now appears twice, deliberately: once as its own band the moment
// the reader opens — visible on page 1 before a single tap, so nobody can
// read the whole chapter without having seen it — and again inside the
// overlay every time it is opened, since that is the one control someone who
// skipped past page 1 (or came back to it later) is likely to reach for.
// Tying it to "index 0 and the overlay is shut" rather than a timer keeps it
// a discrete state rather than something that fades — nothing here animates
// — and it costs nothing extra: the reader cannot help but be on page 0 at
// the start.
import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: screen

    property string sourceId: ""
    property string seriesId: ""
    property string chapterId: ""
    property string chapterTitle: ""

    // "try" (the default, streaming preview) or "saved" (a chapter saved in
    // Quire's own storage, opened with OpenSaved/SavedOpened). Both modes
    // share every tap zone, the overlay and the escape handling below — the
    // differences are what is already known up front (every saved page, so
    // no TryPageRequest traffic) and what closing sends (Main.qml's
    // leaveReader).
    property string mode: "try"

    // Set by begin() when Try is starting on a book row (ChapterList's own
    // isBook, passed through by Main.qml's openTry). While true and mode is
    // still "try" — the window between the tap and BookOpened arriving — the
    // placeholder shows the backend's BookStatus sentence (screen.note)
    // rather than Try's own "Fetching page N…" wording, which is a comic's
    // page-fetching sentence and wrong for a book being opened or laid out.
    // Cleared once BookOpened lands and mode becomes "book".
    property bool pendingBook: false

    // Whether the reader offers "Send to library" (never when the source is
    // private, never when the chapter is already there) and, in Try mode
    // only, "Save in Quire" — see the overlay below. Set by whoever opened
    // the reader (Main.qml), which is where both facts are already known.
    property bool chapterInLibrary: false
    property bool sourcePrivate: false

    property int index: 0
    // 0 means "not known to be the whole chapter yet" — the same convention
    // ui/PagerBar.qml uses for "the source has not said how much there is",
    // which is exactly what an incomplete Try session is.
    property int pageCount: 0
    property bool complete: false

    // A theme's full page list failed to resolve behind the fast first page
    // (theme.FirstPageProber); plain language from the backend (PLAN §2),
    // carried in the overlay until the session ends rather than as a toast
    // that could be missed — a partial chapter is a state the reader stays
    // in, not a moment. Doubles as a book's BookStatus line (see the book
    // mode section below): both are a plain sentence from the backend, shown
    // in the same two places, for the same reason.
    property string note: ""

    // ---- book mode (payload BookOpened / BookRelaid) ----------------------
    //
    // A third mode, alongside try/saved: a MuPDF-rendered book, opened either
    // as a Try session or as a chapter saved in Quire — bookMode says which,
    // straight off BookOpened's own `mode` field. Every tap zone, the overlay
    // shell and the escape/swipe handling above (Main.qml) are shared with
    // try/saved untouched; only what the overlay additionally offers
    // (Contents, Aa) and what closing sends (CloseBook, Main.qml's
    // leaveReader) differ. Pages arrive exactly like Try's — one at a time,
    // through the same pagePaths/requested/ensureRequested machinery above —
    // since a book page is rendered on demand the same way a Try page is
    // fetched on demand.
    property string bookMode: "try"

    // Fixed-layout books (PDF/XPS/CBZ) have no reflow, so the Aa panel hides
    // its layout controls — margins, spacing, font, alignment mean nothing on
    // a page that is already a bitmap — and offers only Contents.
    property bool fixedLayout: false

    // [{title, page, level}], the book's outline. Empty for a book with none.
    property var toc: []

    // {font, size, margins, spacing, align} — the settings currently in
    // effect, straight off BookOpened/BookRelaid, never invented here.
    property var settings: ({})

    // The "Aa" panel's own sentence and rows, straight off BookOpened /
    // BookRelaid (PLAN §2: every user-facing sentence, and every field's
    // label and help text, is composed there — this file only lays them
    // out). settingsFields is
    // [{key,label,help,choices?:[{id,label}],steps?:[{id,label}]}], in
    // display order; empty for a fixed-layout book, which has nothing here
    // to describe.
    property string settingsNote: ""
    property var settingsFields: []

    // fieldByKey returns the settingsFields entry named key, or null if none
    // arrived under that name (a fixed-layout book, or a stale reader that
    // has not yet received one) — callers treat null the same as "nothing to
    // draw here" rather than crashing on a missing field.
    function fieldByKey(key) {
        var fields = screen.settingsFields
        for (var i = 0; i < fields.length; ++i)
            if (fields[i].key === key)
                return fields[i]
        return null
    }

    // capitalizedKey turns a field's wire key ("font", "margins", "spacing",
    // "align") into the prefix its choice buttons' objectNames already used
    // ("Font", "Margins", ...), so a harness written against those names
    // keeps working unchanged.
    function capitalizedKey(key) {
        return key.length > 0 ? key.charAt(0).toUpperCase() + key.slice(1) : key
    }

    // The size field's steps, and the label for whichever one is current —
    // looked up rather than composed here, per PLAN §2.
    readonly property var sizeSteps: {
        var f = screen.fieldByKey("size")
        return f && f.steps ? f.steps : []
    }
    readonly property int sizeMin: screen.sizeSteps.length > 0 ? screen.sizeSteps[0].id : 1
    readonly property int sizeMax: screen.sizeSteps.length > 0 ? screen.sizeSteps[screen.sizeSteps.length - 1].id : 1
    readonly property string currentSizeLabel: {
        var steps = screen.sizeSteps
        var size = screen.settings.size
        for (var i = 0; i < steps.length; ++i)
            if (steps[i].id === size)
                return steps[i].label
        return ""
    }

    // Which of the overlay's two book-only panels is open, if either. Both
    // close the reader's ordinary overlay dismiss area from doing anything
    // useful underneath them, so at most one is ever true.
    property bool contentsVisible: false
    property bool settingsVisible: false

    // The chapter title for wherever the reader currently is: the last toc
    // entry whose page is at or before the current one, same convention a
    // running head uses. Empty for a book with no outline, or before the
    // current page's chapter has been reached.
    readonly property string currentChapterTitle: screen.tocTitleFor(screen.index)

    function tocTitleFor(page) {
        var best = ""
        for (var i = 0; i < screen.toc.length; ++i)
            if (screen.toc[i].page <= page)
                best = screen.toc[i].title
        return best
    }

    // The overlay is shut by default: full screen means the page, not the
    // chrome, until the reader asks for it.
    property bool overlayVisible: false

    // Sparse: only the pages actually fetched so far have an entry. A plain
    // object rather than an array — pages can in principle land out of
    // order, and there is no reason to pre-fill placeholders for pages
    // nobody has asked for.
    property var pagePaths: ({})

    // Indices a page request has already gone out for, so turning back and
    // forth across a page still in flight does not resend the request every
    // time.
    property var requested: ({})

    signal closeRequested()
    // Asks the host to send MessageTryPageRequest for one page. Only ever
    // one at a time — see ensureRequested.
    signal pageWanted(int index)
    // Saved mode only: asks the host to send MessageSavePosition. Emitted
    // debounced on a page turn (savePositionTimer) and once more, straight
    // away, when the reader is left (flushSavePosition) — see Main.qml's
    // leaveReader/closeSaved.
    signal savePositionWanted(int index)

    // The overlay's two library actions (see the overlay below): both are
    // EnqueueDownload, so both just report the tap and leave composing the
    // message to Main.qml, the same division every other control here keeps.
    signal sendToLibraryRequested()
    signal saveInQuireRequested()

    // Book mode only: a tap in the Aa panel changed a setting. Main.qml
    // composes MessageSetReaderSettings from it, naming the book currently
    // open and the page on screen (for anchoring), and shows the BookStatus
    // sentence that answers it (screen.note) until BookRelaid applies.
    signal settingsChangeRequested(var settings)

    readonly property string currentPath: screen.pagePaths[screen.index] !== undefined
                                          ? screen.pagePaths[screen.index] : ""
    readonly property bool pageIsReady: screen.currentPath.length > 0

    // A hard stop at each end, never a wrap — the same rule every paged
    // screen in ui/ follows (PLAN §12.1), just driven by a tap zone instead
    // of a Previous/Next button.
    readonly property bool canGoBack: screen.index > 0
    // Known to exist, whether or not it has been fetched yet — the
    // difference between "off the end of what Quire knows about" (dead, same
    // as any other pager) and "known to exist but still downloading" (the
    // loading label, not a dead zone).
    readonly property bool canGoOn: screen.index + 1 < screen.pageCount

    // The opening honesty band: on by construction the moment the reader is
    // on its first page with the overlay shut, off the instant either stops
    // being true. See the file comment for why this is enough on its own —
    // it needs no timer and no dismissal to track.
    // Saved mode has no such line to show: everything on a saved chapter's
    // disk is exactly what SavedOpened said it was, so there is nothing to
    // be honest about.
    readonly property bool showOpeningNotice: screen.mode === "try"
                                              && screen.index === 0 && !screen.overlayVisible

    function goToPreviousPage() {
        if (screen.canGoBack)
            screen.index -= 1
    }

    function goToNextPage() {
        if (screen.canGoOn)
            screen.index += 1
    }

    function toggleOverlay() {
        screen.overlayVisible = !screen.overlayVisible
    }

    // begin starts a fresh look, before the backend has answered at all —
    // called the moment the user taps Try, so the screen already shows
    // "Fetching page 1…" instead of whatever the previous session left on
    // screen.
    function begin(sourceId, seriesId, chapterId, title, isBook) {
        screen.mode = "try"
        screen.pendingBook = !!isBook
        screen.sourceId = sourceId
        screen.seriesId = seriesId
        screen.chapterId = chapterId
        screen.chapterTitle = title
        screen.index = 0
        screen.pageCount = 0
        screen.complete = false
        screen.note = ""
        screen.overlayVisible = false
        screen.pagePaths = ({})
        screen.requested = ({})
        screen.chapterInLibrary = false
        screen.sourcePrivate = false
        // Called explicitly rather than left to onIndexChanged: index is
        // already 0 for the reader's very first chapter, so assigning it 0
        // above emits no change signal at all, and page 1 would never be
        // asked for.
        screen.ensureRequested(0)
    }

    // openSaved is MessageSavedOpened: unlike begin/ready, every page is
    // already known and local, so there is nothing to stream in and no
    // "Fetching…" placeholder to show — the reader opens complete, straight
    // at the position the backend last stored for it.
    function openSaved(payload) {
        // Stops a still-pending debounce from the chapter being left behind
        // firing after this one has already replaced it.
        savePositionTimer.stop()
        screen.mode = "saved"
        screen.sourceId = payload.sourceId
        screen.seriesId = payload.seriesId
        screen.chapterId = payload.chapterId
        screen.chapterTitle = payload.chapterTitle
        screen.note = ""
        screen.overlayVisible = false
        screen.requested = ({})
        // Carried on the payload itself (backend/service/saved.go), not
        // borrowed from whatever ChapterList screen happened to be open —
        // a chapter opened from the Downloaded overview never had one.
        screen.chapterInLibrary = !!payload.inLibrary
        screen.sourcePrivate = !!payload.private

        var pages = payload.pages ? payload.pages : []
        var paths = {}
        for (var i = 0; i < pages.length; ++i)
            paths[i] = pages[i]
        screen.pagePaths = paths
        screen.pageCount = pages.length
        screen.complete = true

        // Clamped defensively — the backend already clamps position to the
        // page range, but a reader that trusted the wire over its own page
        // count could still be told to open past the last page.
        var pos = payload.position ? payload.position : 0
        if (pos < 0)
            pos = 0
        if (pages.length > 0 && pos >= pages.length)
            pos = pages.length - 1
        screen.index = pos
    }

    // ready is MessageTryReady: the session is open and its first page is on
    // disk.
    function ready(payload) {
        screen.sourceId = payload.sourceId
        screen.seriesId = payload.seriesId
        screen.chapterId = payload.chapterId
        screen.index = payload.index
        screen.pageCount = payload.pageCount
        screen.complete = payload.complete
        screen.note = ""
        var paths = {}
        paths[payload.index] = payload.path
        screen.pagePaths = paths
        screen.requested = ({})
    }

    // openBook is MessageBookOpened, whichever question it answered
    // (OpenSaved or TryChapter — see Main.qml's dispatch). Unlike a saved
    // comic, the payload carries no page paths at all: a book page is
    // rendered on demand, so the current page is asked for exactly the way
    // begin() asks for Try's page 0, through ensureRequested below.
    function openBook(payload) {
        savePositionTimer.stop()
        screen.mode = "book"
        screen.pendingBook = false
        screen.bookMode = payload.mode ? payload.mode : "try"
        screen.sourceId = payload.sourceId
        screen.seriesId = payload.seriesId
        screen.chapterId = payload.chapterId
        screen.chapterTitle = payload.title ? payload.title : ""
        screen.fixedLayout = !!payload.fixedLayout
        screen.pageCount = payload.pageCount ? payload.pageCount : 0
        // Always the whole book: unlike Try's fast-first-page streaming, a
        // book's page count is known outright on BookOpened.
        screen.complete = true
        screen.note = ""
        screen.toc = payload.toc ? payload.toc : []
        screen.settings = payload.settings ? payload.settings : {}
        screen.settingsNote = payload.settingsNote ? payload.settingsNote : ""
        screen.settingsFields = payload.settingsFields ? payload.settingsFields : []
        screen.chapterInLibrary = !!payload.inLibrary
        screen.sourcePrivate = !!payload.private
        screen.overlayVisible = false
        screen.contentsVisible = false
        screen.settingsVisible = false
        screen.pagePaths = ({})
        screen.requested = ({})
        var pos = payload.page ? payload.page : 0
        if (pos < 0)
            pos = 0
        screen.index = pos
        // Explicit, not left to onIndexChanged: index can already be this
        // value (a fresh reader's very first book), which emits no change
        // signal at all — the same reasoning begin() explains.
        screen.ensureRequested(screen.index)
    }

    // relaid is MessageBookRelaid, the answer to a settings change: a new
    // layout means every cached page path is stale, so it is dropped and the
    // page on screen is asked for again under the new settings.
    function relaid(payload) {
        if (!screen.matches(payload))
            return
        screen.pageCount = payload.pageCount ? payload.pageCount : 0
        screen.toc = payload.toc ? payload.toc : []
        screen.settings = payload.settings ? payload.settings : {}
        if (payload.settingsNote)
            screen.settingsNote = payload.settingsNote
        if (payload.settingsFields)
            screen.settingsFields = payload.settingsFields
        screen.note = ""
        screen.pagePaths = ({})
        screen.requested = ({})
        screen.index = payload.page ? payload.page : 0
        screen.ensureRequested(screen.index)
    }

    // bookStatus is MessageBookStatus: a progress sentence while a book is
    // fetched (Try), opened or laid out — shown on the placeholder/opening
    // screen and in the overlay, the same as Try's partial-chapter note.
    function bookStatus(payload) {
        if (!screen.matches(payload))
            return
        screen.note = payload.message ? payload.message : ""
    }

    // changeSetting merges one field into the settings currently in effect
    // and asks for it — never a whole new object invented here, so a field
    // this reader does not draw a control for still survives the round trip.
    function changeSetting(key, value) {
        var next = {}
        for (var k in screen.settings)
            next[k] = screen.settings[k]
        next[key] = value
        screen.settingsChangeRequested(next)
    }

    // jumpToPage is the Contents panel's tap: closes both it and the overlay
    // and turns straight to the page, the same "just go there" a page number
    // would be if this reader had one.
    function jumpToPage(page) {
        screen.contentsVisible = false
        screen.overlayVisible = false
        screen.index = page
    }

    // matches is whether a reply is about the session actually open, rather
    // than one the reader has since closed or moved on from — the same
    // discipline the backend applies to a page request naming a stale
    // chapter (Service.currentTrySession).
    function matches(payload) {
        return payload.sourceId === screen.sourceId
            && payload.seriesId === screen.seriesId
            && payload.chapterId === screen.chapterId
    }

    // pageArrived is MessageTryPage: one more page, ready.
    function pageArrived(payload) {
        if (!screen.matches(payload))
            return
        var paths = {}
        for (var k in screen.pagePaths)
            paths[k] = screen.pagePaths[k]
        paths[payload.index] = payload.path
        screen.pagePaths = paths

        var req = {}
        for (var k2 in screen.requested)
            req[k2] = screen.requested[k2]
        delete req[payload.index]
        screen.requested = req
    }

    // countUpdated is MessageTryPageCount: the full page list resolved (or
    // failed to) behind the fast first page.
    function countUpdated(payload) {
        if (!screen.matches(payload))
            return
        screen.pageCount = payload.pageCount
        screen.complete = payload.complete
        screen.note = payload.note ? payload.note : ""
    }

    // ensureRequested asks for a page exactly once — not on every redraw,
    // and not again while it is still in flight.
    function ensureRequested(i) {
        if (screen.pagePaths[i] !== undefined)
            return
        if (screen.requested[i])
            return
        var req = {}
        for (var k in screen.requested)
            req[k] = screen.requested[k]
        req[i] = true
        screen.requested = req
        screen.pageWanted(i)
    }

    onIndexChanged: {
        screen.ensureRequested(screen.index)
        screen.scheduleSavePosition()
    }
    onVisibleChanged: if (screen.visible) screen.ensureRequested(screen.index)

    // A reading position is worth keeping for a saved chapter — Try has none
    // (PLAN's Try milestone: leaving discards the whole session) — but not
    // worth one round trip a page turn: flicking through a chapter would
    // otherwise fire a message every tap. The debounce holds the position at
    // rest and sends the one that stuck; flushSavePosition sends
    // immediately, for when the reader is about to close and there is no
    // "later" left to wait for.
    Timer {
        id: savePositionTimer
        interval: 1500
        repeat: false
        onTriggered: screen.savePositionWanted(screen.index)
    }

    // A position is worth keeping for a saved chapter and for a saved book
    // (SavePosition (93) works for both — the wire contract's own words) —
    // never for a Try session of either kind, comic or book, which has none
    // to keep.
    function positionTracked() {
        return screen.mode === "saved"
            || (screen.mode === "book" && screen.bookMode === "saved")
    }

    function scheduleSavePosition() {
        if (!screen.positionTracked())
            return
        savePositionTimer.restart()
    }

    function flushSavePosition() {
        if (!screen.positionTracked())
            return
        savePositionTimer.stop()
        screen.savePositionWanted(screen.index)
    }

    Rectangle {
        anchors.fill: parent
        color: Style.paper
    }

    // ---- the page — the whole panel, nothing else drawn over it by
    // default -------------------------------------------------------------

    // Exactly one Image, so exactly one page is ever decoded at a time (see
    // the file comment). Its source simply changes on a page turn — no
    // ListView, no cacheBuffer, nothing that would hold a second page
    // resident.
    Image {
        id: pageImage
        objectName: "tryPageImage"
        anchors.fill: parent
        fillMode: Image.PreserveAspectFit
        visible: screen.pageIsReady
        source: screen.pageIsReady ? "file://" + screen.currentPath : ""
        asynchronous: false
        cache: false
    }

    // The "turning faster than fetching" case: never a blank page standing
    // in for content, and never a freeze — the tap zones stay fully
    // interactive while this shows, so a reader who turned ahead of the
    // fetch can still turn back or open the overlay.
    Text {
        id: tryLoadingLabel
        objectName: "tryLoadingLabel"
        anchors.centerIn: parent
        // A book row's Try never shows this label's own page-fetching
        // wording — it is a comic's sentence, wrong for a book being
        // fetched, opened or laid out. While pendingBook (the window before
        // BookOpened arrives), this shows the backend's own BookStatus
        // sentence instead, empty until the first one arrives.
        visible: !screen.pageIsReady && (!screen.pendingBook || screen.note.length > 0)
        text: screen.pendingBook ? screen.note : "Fetching page " + (screen.index + 1) + "…"
        font.pointSize: Style.bodySize
        color: Style.muted
    }

    // Book mode's own BookStatus sentence — fetching (Try), opening or
    // laying out — shown on this placeholder/opening screen as well as in
    // the overlay (tryOverlayPartialNote), so it reaches whoever is looking
    // whether or not they have opened the overlay yet.
    Text {
        objectName: "tryBookStatusLabel"
        anchors { top: tryLoadingLabel.bottom; topMargin: Style.gap
                  horizontalCenter: parent.horizontalCenter }
        visible: screen.mode === "book" && !screen.pageIsReady && screen.note.length > 0
        text: screen.note
        font.pointSize: Style.smallSize
        color: Style.muted
    }

    // The opening honesty band (see the file comment). It carries no
    // MouseArea of its own, so a tap through it still reaches whichever tap
    // zone is under it — it is a label, not a control.
    Rectangle {
        id: openingNotice
        objectName: "tryOpeningNotice"
        visible: screen.showOpeningNotice
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        height: visible ? noticeText.implicitHeight + Style.gap * 2 : 0
        color: Style.paper

        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: Style.hairline
            color: Style.rule
        }

        Text {
            id: noticeText
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            wrapMode: Text.WordWrap
            text: "Preview only — nothing is saved here. Download the chapter to keep it."
            font.pointSize: Style.smallSize
            color: Style.muted
        }
    }

    // ---- the three tap zones ------------------------------------------
    //
    // zoneFraction is how much of the width each side takes. 30% each way
    // leaves 40% in the middle: wide enough that reaching for the overlay
    // does not need aiming — the whole point of a menu you summon with a
    // tap rather than a button you must land on exactly — while still
    // leaving each side zone a comfortable thumb's width for the gesture
    // that will be used on every page of a chapter, which the overlay is
    // not. Symmetric left and right because Quire does not know a reader's
    // hand or the site's own page direction; nothing here assumes either.
    readonly property real zoneFraction: 0.3

    Item {
        id: tapZones
        objectName: "tryTapZones"
        anchors.fill: parent
        // Page-turning stands down while the overlay owns the screen: the
        // area between its top and bottom bars is still the page, and a tap
        // there closes the overlay (overlayDismissArea) rather than also
        // turning a page underneath it. Reopening the tap zones is just
        // closing the overlay again.
        enabled: !screen.overlayVisible

        MouseArea {
            id: leftZone
            objectName: "tryLeftZone"
            x: 0; y: 0
            width: parent.width * screen.zoneFraction
            height: parent.height
            onClicked: screen.goToPreviousPage()
        }

        MouseArea {
            id: middleZone
            objectName: "tryMiddleZone"
            x: leftZone.width; y: 0
            width: parent.width - 2 * leftZone.width
            height: parent.height
            onClicked: screen.toggleOverlay()
        }

        MouseArea {
            id: rightZone
            objectName: "tryRightZone"
            x: leftZone.width + middleZone.width; y: 0
            width: parent.width - x
            height: parent.height
            onClicked: screen.goToNextPage()
        }
    }

    // ---- the overlay -----------------------------------------------------
    //
    // Sparse on purpose (the owner's word for it): a title, a page number, a
    // way back, and the honesty line — nothing this screen does not already
    // know, and nothing drawn only for decoration. Every appearance is a
    // full e-ink repaint, so there is nothing here that is not worth one.
    Item {
        id: overlay
        objectName: "tryOverlay"
        anchors.fill: parent
        visible: screen.overlayVisible

        // Tapping the page itself (anywhere not on one of the bars below)
        // closes the overlay — the same "tap the middle" gesture that opened
        // it, just answered by whatever is currently under the finger rather
        // than only the exact original zone. Declared first so the bars'
        // own controls, declared after, sit on top of it and take a tap
        // before it can fall through to this.
        MouseArea {
            id: overlayDismissArea
            objectName: "tryOverlayDismissArea"
            anchors.fill: parent
            onClicked: screen.toggleOverlay()
        }

        Rectangle {
            id: overlayTop
            objectName: "tryOverlayTop"
            anchors { top: parent.top; left: parent.left; right: parent.right }
            height: Style.rowHeight
            color: Style.paper

            Rectangle {
                anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                height: Style.hairline
                color: Style.rule
            }

            // The same words and the same shape as the shared header's own
            // Back (Main.qml) — a reader who has learned that control
            // anywhere else in Quire recognises this one too.
            Text {
                id: backLabel
                objectName: "tryBackLabel"
                anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
                text: "‹ Back"
                font.pointSize: Style.bodySize
                color: backArea.pressed ? Style.muted : Style.ink
            }

            MouseArea {
                id: backArea
                objectName: "tryBackArea"
                anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
                width: Style.margin * 2 + backLabel.width
                onClicked: screen.closeRequested()
            }

            Text {
                objectName: "tryOverlayTitle"
                anchors {
                    left: backLabel.right; leftMargin: Style.gap
                    right: parent.right; rightMargin: Style.margin
                    verticalCenter: parent.verticalCenter
                }
                horizontalAlignment: Text.AlignHCenter
                elide: Text.ElideRight
                text: screen.chapterTitle
                font.pointSize: Style.headingSize
                color: Style.ink
            }
        }

        Rectangle {
            id: overlayBottom
            objectName: "tryOverlayBottom"
            anchors { bottom: parent.bottom; left: parent.left; right: parent.right }
            height: overlayBottomColumn.implicitHeight + Style.gap * 2
            color: Style.paper

            Rectangle {
                anchors { left: parent.left; right: parent.right; top: parent.top }
                height: Style.hairline
                color: Style.rule
            }

            Column {
                id: overlayBottomColumn
                anchors {
                    left: parent.left; leftMargin: Style.margin
                    right: parent.right; rightMargin: Style.margin
                    verticalCenter: parent.verticalCenter
                }
                spacing: 4

                Text {
                    objectName: "tryOverlayPageLabel"
                    width: parent.width
                    text: Paging.label(screen.index + 1, screen.complete ? screen.pageCount : 0)
                    font.pointSize: Style.bodySize
                    color: Style.ink
                }

                // The honesty line, reaching everyone who opens the overlay
                // at any page — see the file comment for why the opening
                // band above is not treated as sufficient on its own.
                Text {
                    objectName: "tryOverlayNotice"
                    width: parent.width
                    // Saved mode has nothing to be honest about — see
                    // showOpeningNotice.
                    visible: screen.mode === "try"
                    wrapMode: Text.WordWrap
                    text: "Preview only — nothing is saved here. Download the chapter to keep it."
                    font.pointSize: Style.smallSize
                    color: Style.muted
                }

                Text {
                    objectName: "tryOverlayPartialNote"
                    width: parent.width
                    visible: screen.note.length > 0
                    wrapMode: Text.WordWrap
                    text: screen.note
                    font.pointSize: Style.smallSize
                    color: Style.muted
                }

                // A book's own chapter, alongside the page number — the last
                // toc entry at or before the current page (currentChapterTitle
                // above). Empty for a book with no outline.
                Text {
                    objectName: "tryOverlayChapterTitle"
                    width: parent.width
                    visible: screen.mode === "book" && screen.currentChapterTitle.length > 0
                    elide: Text.ElideRight
                    text: screen.currentChapterTitle
                    font.pointSize: Style.smallSize
                    color: Style.muted
                }

                // Book mode's own two ways into the outline and the settings
                // panel — QML owns only these two short labels (PLAN §2); the
                // toc entries and every settings sentence are the backend's.
                Row {
                    id: bookActions
                    width: parent.width
                    spacing: Style.gap
                    visible: screen.mode === "book"

                    Rectangle {
                        id: contentsButton
                        objectName: "tryContentsButton"
                        width: 160
                        height: Style.buttonHeight
                        color: contentsArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: "Contents"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: contentsArea
                            objectName: "tryContentsArea"
                            anchors.fill: parent
                            enabled: bookActions.visible
                            onClicked: {
                                screen.settingsVisible = false
                                screen.contentsVisible = true
                            }
                        }
                    }

                    Rectangle {
                        id: aaButton
                        objectName: "tryAaButton"
                        // Fixed-layout books (PDF/XPS/CBZ) have nothing this
                        // panel controls — no reflow, so no margins, spacing,
                        // font or alignment to change — so the button that
                        // opens an otherwise-empty panel is not offered at
                        // all: Contents is what a fixed-layout book keeps.
                        visible: !screen.fixedLayout
                        width: 160
                        height: Style.buttonHeight
                        color: aaArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: "Aa"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: aaArea
                            objectName: "tryAaArea"
                            anchors.fill: parent
                            enabled: aaButton.visible
                            onClicked: {
                                screen.contentsVisible = false
                                screen.settingsVisible = true
                            }
                        }
                    }
                }

                // The reader's two library actions. Neither is a question —
                // tapping one sends EnqueueDownload and the outcome comes
                // back as whatever DownloadProgress sentence the row it came
                // from would otherwise show (Main.qml, ChapterList.qml).
                // No visible binding of its own: a Row's own effective
                // visibility and its children's both come from the same
                // underlying QQuickItem state, so deriving one from the
                // other here would be a binding loop that Qt Quick breaks by
                // reading as false. An empty Row costs nothing to draw when
                // both buttons below are hidden.
                Row {
                    id: overlayActions
                    width: parent.width
                    spacing: Style.gap

                    Rectangle {
                        id: sendToLibraryButton
                        objectName: "trySendToLibraryButton"
                        // Never for a private source (PLAN's own rule: never
                        // put private content in the reMarkable library) and
                        // never once the chapter is already there — a second
                        // copy is not what the button offers.
                        visible: !screen.sourcePrivate && !screen.chapterInLibrary
                        width: 220
                        height: Style.buttonHeight
                        color: sendToLibraryArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: "Send to library"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: sendToLibraryArea
                            objectName: "trySendToLibraryArea"
                            anchors.fill: parent
                            enabled: sendToLibraryButton.visible
                            onClicked: screen.sendToLibraryRequested()
                        }
                    }

                    Rectangle {
                        id: saveInQuireButton
                        objectName: "trySaveInQuireButton"
                        // Try mode only — a saved chapter (or saved book) is
                        // already saved.
                        visible: screen.mode === "try"
                                 || (screen.mode === "book" && screen.bookMode === "try")
                        width: 220
                        height: Style.buttonHeight
                        color: saveInQuireArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: "Save in Quire"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: saveInQuireArea
                            objectName: "trySaveInQuireArea"
                            anchors.fill: parent
                            enabled: saveInQuireButton.visible
                            onClicked: screen.saveInQuireRequested()
                        }
                    }
                }
            }
        }
    }

    // ---- book mode: the outline ------------------------------------------
    //
    // Full screen, over everything else — the same weight as the overlay it
    // is opened from. Not paged: an outline is read down, not turned through,
    // and is nowhere near a chapter's worth of rows.
    Item {
        id: contentsPanel
        objectName: "tryContentsPanel"
        anchors.fill: parent
        visible: screen.contentsVisible
        z: 10

        Rectangle { anchors.fill: parent; color: Style.paper }

        Rectangle {
            id: contentsPanelTop
            anchors { top: parent.top; left: parent.left; right: parent.right }
            height: Style.rowHeight
            color: Style.paper

            Rectangle {
                anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                height: Style.hairline
                color: Style.rule
            }

            Text {
                id: contentsBackLabel
                anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
                text: "Back"
                font.pointSize: Style.bodySize
                color: contentsBackArea.pressed ? Style.muted : Style.ink
            }

            MouseArea {
                id: contentsBackArea
                objectName: "tryContentsBackArea"
                anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
                width: Style.margin * 2 + contentsBackLabel.width
                onClicked: screen.contentsVisible = false
            }

            Text {
                anchors {
                    left: contentsBackLabel.right; leftMargin: Style.gap
                    right: parent.right; rightMargin: Style.margin
                    verticalCenter: parent.verticalCenter
                }
                horizontalAlignment: Text.AlignHCenter
                text: "Contents"
                font.pointSize: Style.headingSize
                color: Style.ink
            }
        }

        Column {
            id: tocColumn
            objectName: "tryContentsList"
            anchors {
                top: contentsPanelTop.bottom; topMargin: Style.gap
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
            }

            Repeater {
                model: screen.toc

                Rectangle {
                    objectName: "tryContentsEntry-" + index
                    width: tocColumn.width
                    height: Style.rowHeight
                    color: tocEntryArea.pressed ? Style.pressed : Style.paper

                    Text {
                        anchors {
                            left: parent.left; leftMargin: Style.gap * (1 + modelData.level)
                            right: parent.right; rightMargin: Style.margin
                            verticalCenter: parent.verticalCenter
                        }
                        elide: Text.ElideRight
                        text: modelData.title
                        font.pointSize: Style.bodySize
                        color: Style.ink
                    }

                    MouseArea {
                        id: tocEntryArea
                        objectName: "tryContentsEntryArea-" + index
                        anchors.fill: parent
                        onClicked: screen.jumpToPage(modelData.page)
                    }
                }
            }
        }
    }

    // ---- book mode: the Aa settings panel ----------------------------------
    //
    // Reader settings are global (PLAN's own decision, not per-book), so
    // every control here fires the moment it is tapped — there is no save
    // step, the same as every other switch in ui/. Fixed-layout books never
    // show this panel at all (see the Aa button above).
    //
    // Every field's label, help sentence and choices come straight off
    // BookOpened/BookRelaid's settingsFields (PLAN §2) — this file lays out
    // a generic Repeater over them rather than composing any wording of its
    // own, so a field added on the backend needs no matching change here.
    Item {
        id: settingsPanel
        objectName: "trySettingsPanel"
        anchors.fill: parent
        visible: screen.settingsVisible
        z: 10

        Rectangle { anchors.fill: parent; color: Style.paper }

        Rectangle {
            id: settingsPanelTop
            anchors { top: parent.top; left: parent.left; right: parent.right }
            height: Style.rowHeight
            color: Style.paper

            Rectangle {
                anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                height: Style.hairline
                color: Style.rule
            }

            Text {
                id: settingsBackLabel
                anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
                text: "Back"
                font.pointSize: Style.bodySize
                color: settingsBackArea.pressed ? Style.muted : Style.ink
            }

            MouseArea {
                id: settingsBackArea
                objectName: "trySettingsBackArea"
                anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
                width: Style.margin * 2 + settingsBackLabel.width
                onClicked: screen.settingsVisible = false
            }

            Text {
                anchors {
                    left: settingsBackLabel.right; leftMargin: Style.gap
                    right: parent.right; rightMargin: Style.margin
                    verticalCenter: parent.verticalCenter
                }
                horizontalAlignment: Text.AlignHCenter
                text: "Text settings"
                font.pointSize: Style.headingSize
                color: Style.ink
            }
        }

        // The backend's own sentence about what this panel is for
        // (books-contract.md §B: the settings are global, not per-book).
        Text {
            id: settingsNoteLabel
            objectName: "trySettingsNote"
            anchors {
                top: settingsPanelTop.bottom; topMargin: Style.gap
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
            }
            text: screen.settingsNote
            font.pointSize: Style.smallSize
            color: Style.muted
            wrapMode: Text.WordWrap
        }

        // Scrollable, unlike every other panel in ui/ (PLAN §12.1 is about
        // paging and animation, not about a panel that can simply be taller
        // than the screen) — five fields plus their help sentences do not
        // reliably fit a small viewport.
        Flickable {
            id: settingsScroll
            objectName: "trySettingsScroll"
            anchors {
                top: settingsNoteLabel.bottom; topMargin: Style.gap
                left: parent.left; right: parent.right; bottom: parent.bottom
            }
            clip: true
            contentWidth: width
            contentHeight: settingsColumn.height + Style.gap
            boundsBehavior: Flickable.StopAtBounds

            Column {
                id: settingsColumn
                x: Style.margin
                y: 0
                width: parent.width - Style.margin * 2
                spacing: Style.gap * 1.5

                Repeater {
                    model: screen.settingsFields

                    delegate: Column {
                        id: fieldColumn
                        property var field: modelData
                        width: settingsColumn.width
                        spacing: Style.gap / 2

                        Text {
                            objectName: "trySettingsFieldLabel-" + fieldColumn.field.key
                            text: fieldColumn.field.label
                            font.pointSize: Style.bodySize
                            color: Style.ink
                        }

                        Text {
                            objectName: "trySettingsFieldHelp-" + fieldColumn.field.key
                            text: fieldColumn.field.help
                            font.pointSize: Style.smallSize
                            color: Style.muted
                            width: fieldColumn.width
                            wrapMode: Text.WordWrap
                        }

                        // The size field's stepper: "-", the current step's
                        // own label (never composed here), "+".
                        Row {
                            objectName: "trySettingsSizeRow"
                            visible: !!(fieldColumn.field.steps && fieldColumn.field.steps.length > 0)
                            spacing: Style.gap

                            Rectangle {
                                id: sizeDownButton
                                objectName: "trySettingsSizeDown"
                                // Disabled at the bound, not merely inert —
                                // the same dead-as-well-as-hidden rule every
                                // other control in ui/ follows
                                // (ChapterList.qml's watchButton, for one),
                                // so a check of this element alone says
                                // whether the control really works.
                                enabled: (screen.settings.size ? screen.settings.size : screen.sizeMin) > screen.sizeMin
                                width: 80
                                height: Style.buttonHeight
                                color: sizeDownArea.pressed ? Style.pressed : Style.paper
                                border.width: 2
                                border.color: enabled ? Style.ink : Style.rule
                                radius: 6
                                Text { anchors.centerIn: parent; text: "-"; font.pointSize: Style.bodySize; color: Style.ink }
                                MouseArea {
                                    id: sizeDownArea
                                    objectName: "trySettingsSizeDownArea"
                                    anchors.fill: parent
                                    enabled: sizeDownButton.enabled
                                    onClicked: screen.changeSetting("size", screen.settings.size - 1)
                                }
                            }

                            Text {
                                objectName: "trySettingsSizeLabel"
                                text: screen.currentSizeLabel
                                font.pointSize: Style.bodySize
                                color: Style.ink
                                width: 100
                                height: Style.buttonHeight
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }

                            Rectangle {
                                id: sizeUpButton
                                objectName: "trySettingsSizeUp"
                                enabled: (screen.settings.size ? screen.settings.size : screen.sizeMin) < screen.sizeMax
                                width: 80
                                height: Style.buttonHeight
                                color: sizeUpArea.pressed ? Style.pressed : Style.paper
                                border.width: 2
                                border.color: enabled ? Style.ink : Style.rule
                                radius: 6
                                Text { anchors.centerIn: parent; text: "+"; font.pointSize: Style.bodySize; color: Style.ink }
                                MouseArea {
                                    id: sizeUpArea
                                    objectName: "trySettingsSizeUpArea"
                                    anchors.fill: parent
                                    enabled: sizeUpButton.enabled
                                    onClicked: screen.changeSetting("size", screen.settings.size + 1)
                                }
                            }
                        }

                        // Every other field: a wrapping row of the backend's
                        // own choices (font, margins, spacing, align) —
                        // sized to their own label, with a floor so a short
                        // one ("Wide") still reads as a real button.
                        Flow {
                            objectName: "trySettingsChoices-" + fieldColumn.field.key
                            visible: !(fieldColumn.field.steps && fieldColumn.field.steps.length > 0)
                            width: fieldColumn.width
                            spacing: Style.gap

                            Repeater {
                                model: fieldColumn.field.choices ? fieldColumn.field.choices : []

                                Rectangle {
                                    objectName: "trySettings" + screen.capitalizedKey(fieldColumn.field.key) + "-" + modelData.id
                                    width: Math.max(140, choiceLabel.implicitWidth + Style.gap * 2)
                                    height: Style.buttonHeight
                                    color: choiceArea.pressed ? Style.pressed
                                           : (screen.settings[fieldColumn.field.key] === modelData.id ? Style.rule : Style.paper)
                                    border.width: 2
                                    border.color: Style.ink
                                    radius: 6

                                    Text {
                                        id: choiceLabel
                                        anchors.centerIn: parent
                                        text: modelData.label
                                        font.pointSize: Style.smallSize
                                        color: Style.ink
                                    }

                                    MouseArea {
                                        id: choiceArea
                                        anchors.fill: parent
                                        onClicked: screen.changeSetting(fieldColumn.field.key, modelData.id)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
