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
    // in, not a moment.
    property string note: ""

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
    readonly property bool showOpeningNotice: screen.index === 0 && !screen.overlayVisible

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
    function begin(sourceId, seriesId, chapterId, title) {
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
        objectName: "tryLoadingLabel"
        anchors.centerIn: parent
        visible: !screen.pageIsReady
        text: "Fetching page " + (screen.index + 1) + "…"
        font.pointSize: Style.bodySize
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
            }
        }
    }
}
