// The Try reader — milestone 1: read a chapter without downloading it and
// without a library entry, in a reader of Quire's own.
//
// PLAN §12.1's page-turn rule applies here exactly as it does everywhere
// else: nothing scrolls, flicks or animates, and PagerBar is the same control
// every other paged screen uses. What is different about this pager is what
// "how many pages" means — see totalPages and hasMore below.
//
// # Streaming, not a wall of "please wait"
//
// The backend opens on page 1 as soon as that one page exists, and the rest
// arrive as they are fetched (backend/service/tryreader.go). This screen
// reflects that directly: `pagePaths` is a sparse map of whatever has
// arrived so far, keyed by index, and turning to a page that is not in it
// yet shows a plain "Fetching…" line instead of a blank rectangle or a
// frozen control — never silently standing in for content (the owner's
// requirement). Nothing here waits for the whole chapter, or even asks the
// backend for more than the one page beyond wherever the reader is —
// Main.qml's onIndexChanged handler is the only thing that ever asks for a
// page, and it asks for exactly one.
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
// # Honesty
//
// PLAN's Try milestone is explicit that this must never look like a save:
// the notice below says so in words, permanently, not as a one-time toast —
// a reader who glances up mid-chapter should see it as plainly as at the
// start.
import QtQuick 2.5
import "Style.js" as Style

Item {
    id: screen

    property string sourceId: ""
    property string seriesId: ""
    property string chapterId: ""
    property string chapterTitle: ""

    property int index: 0
    // 0 means "not known to be the whole chapter yet" — PagerBar's own
    // convention for "the source has not said how much there is" (see
    // ui/PagerBar.qml), which is exactly what an incomplete Try session is.
    property int pageCount: 0
    property bool complete: false

    // A theme's full page list failed to resolve behind the fast first page
    // (theme.FirstPageProber); plain language from the backend (PLAN §2),
    // shown until the session ends rather than as a toast that could be
    // missed — a partial chapter is a state the reader stays in, not a
    // moment.
    property string note: ""

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

    readonly property string currentPath: screen.pagePaths[screen.index] !== undefined
                                          ? screen.pagePaths[screen.index] : ""
    readonly property bool pageIsReady: screen.currentPath.length > 0

    // begin starts a fresh look, before the backend has answered at all —
    // called the moment the user taps Try, so the screen already shows
    // "Fetching page 1…" instead of whatever the previous session left on
    // screen.
    function begin(sourceId, seriesId, chapterId, title) {
        screen.sourceId = sourceId
        screen.seriesId = seriesId
        screen.chapterId = chapterId
        screen.chapterTitle = title
        screen.index = 0
        screen.pageCount = 0
        screen.complete = false
        screen.note = ""
        screen.pagePaths = ({})
        screen.requested = ({})
        // Called explicitly rather than left to onIndexChanged: index is
        // already 0 for the reader's very first chapter, so assigning it 0
        // above emits no change signal at all, and page 1 would never be
        // asked for.
        screen.ensureRequested(0)
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

    onIndexChanged: screen.ensureRequested(screen.index)
    onVisibleChanged: if (screen.visible) screen.ensureRequested(screen.index)

    Rectangle {
        anchors.fill: parent
        color: Style.paper
    }

    // The reader's own way out, alongside the shared Back button in Main.qml's
    // chrome — both end the same way (Main.qml's endTry), so which one a
    // reader reaches for makes no difference to what happens.
    Rectangle {
        id: closeButton
        objectName: "tryCloseButton"
        anchors {
            top: parent.top; topMargin: Style.gap
            right: parent.right; rightMargin: Style.margin
        }
        width: 160
        height: Style.buttonHeight
        color: closeArea.pressed ? Style.pressed : Style.paper
        border.width: 2
        border.color: Style.ink
        radius: 6

        Text {
            anchors.centerIn: parent
            text: "Close"
            font.pointSize: Style.smallSize
            color: Style.ink
        }

        MouseArea {
            id: closeArea
            objectName: "tryCloseArea"
            anchors.fill: parent
            onClicked: screen.closeRequested()
        }
    }

    // The one thing Try must never let anyone believe: that tapping it saved
    // anything. Permanent, not a toast — a reader glancing up mid-chapter
    // sees it as plainly as at the start.
    Text {
        id: notice
        objectName: "tryNotice"
        anchors {
            top: parent.top; topMargin: Style.gap
            left: parent.left; leftMargin: Style.margin
            right: closeButton.left; rightMargin: Style.gap
        }
        wrapMode: Text.WordWrap
        text: "Preview only — nothing is saved here. Download the chapter to keep it."
        font.pointSize: Style.smallSize
        color: Style.muted
    }

    Text {
        id: partialNote
        objectName: "tryPartialNote"
        anchors {
            top: notice.bottom; topMargin: 4
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
        }
        visible: screen.note.length > 0
        wrapMode: Text.WordWrap
        text: screen.note
        font.pointSize: Style.smallSize
        color: Style.muted
    }

    Item {
        id: viewport
        anchors {
            top: partialNote.visible ? partialNote.bottom : notice.bottom
            topMargin: Style.gap
            left: parent.left; right: parent.right
            bottom: pager.top
        }

        // Exactly one Image, so exactly one page is ever decoded at a time
        // (see the file comment). Its source simply changes on a page turn —
        // no ListView, no cacheBuffer, nothing that would hold a second page
        // resident.
        Image {
            id: pageImage
            objectName: "tryPageImage"
            anchors.fill: parent
            anchors.margins: Style.margin
            fillMode: Image.PreserveAspectFit
            visible: screen.pageIsReady
            source: screen.pageIsReady ? "file://" + screen.currentPath : ""
            asynchronous: false
            cache: false
        }

        // The "turning faster than fetching" case: never a blank page
        // standing in for content, and never a freeze — the reader stays
        // fully interactive (Previous/Close both still work) while this
        // shows.
        Text {
            objectName: "tryLoadingLabel"
            anchors.centerIn: parent
            visible: !screen.pageIsReady
            text: "Fetching page " + (screen.index + 1) + "…"
            font.pointSize: Style.bodySize
            color: Style.muted
        }
    }

    PagerBar {
        id: pager
        objectName: "tryPager"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.index + 1
        // 0 while the full list is still unknown (see screen.pageCount);
        // PagerBar then shows "Page N" rather than inventing a denominator.
        totalPages: screen.complete ? screen.pageCount : 0
        // Known to exist, whether or not it has been fetched yet — the
        // difference between "off the end of what Quire knows about" (Next
        // goes dead, same as any other pager) and "known to exist but still
        // downloading" (the loading label above, not a dead button).
        hasMore: screen.index + 1 < screen.pageCount
        onPreviousRequested: if (screen.index > 0) screen.index -= 1
        onNextRequested: if (screen.index + 1 < screen.pageCount) screen.index += 1
    }
}
