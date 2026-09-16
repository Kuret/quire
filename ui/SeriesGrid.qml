// The series grid — PLAN §6 M3: "Series grid with covers. Cache on disk,
// downscale hard, never hold more than a screenful in memory."
//
// Since PLAN §12.1 it does not scroll: it turns pages. The model holds exactly
// one display page, because the backend's pager (backend/service/paging.go)
// serves display-sized slices out of a cache of source pages — so a page turn
// is usually not an HTTP request at all, and a screenful is all that is ever
// resident, which is the memory rule kept by construction.
//
// The page size is computed from the viewport and sent to the backend with
// every request. It is never hardcoded: the panel is 1620×2160, but the AppLoad
// PC emulator is a window, and a page sized to the panel would leave that one
// with a half-visible row — the scrolling problem in miniature, and an
// invitation to the swipe PLAN §12.1 is removing.
//
// The images themselves are files on disk that the backend downscaled — nothing
// image-shaped ever crosses the socket (PLAN §7.1).

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: screen

    property alias model: grid.model
    property bool busy: false
    property string emptyMessage: ""
    property string query: ""
    property bool searching: false

    // Where in the listing we are. All three come from the backend: it owns the
    // cache, so it is the only thing that knows whether there is more, and
    // totalPages stays 0 until the source has actually run out rather than
    // being guessed at (PLAN §12.1).
    property int page: 1
    property int totalPages: 0
    property bool hasMore: false
    property int pendingPage: 0

    // How many tiles fit, whole rows only. Three columns on the panel's short
    // edge; the tile is 2:3, the shape covers are published at and the shape
    // the cache writes.
    readonly property int columns: 3
    readonly property int rowsPerPage: Paging.rowsPerPage(viewport.height, grid.cellHeight)
    readonly property int pageSize: Paging.itemsPerPage(viewport.height, grid.cellHeight, screen.columns)

    signal searchRequested(string query)
    signal browseRequested()
    // coversRequested carries the whole visible set at once, because the
    // backend treats a batch as superseding the last one: a cover still being
    // fetched for a tile that a page turn took away is work nobody will see,
    // and on a device whose politeness limiter serialises requests (PLAN §7.4)
    // it is work standing in front of the covers that *are* on screen.
    signal coversRequested(var covers)
    signal openRequested(string seriesId, string title)
    signal pageRequested(int page)

    function reset() {
        screen.query = ""
        screen.searching = false
        screen.emptyMessage = ""
        screen.page = 1
        screen.totalPages = 0
        screen.hasMore = false
        screen.pendingPage = 0
    }

    // The geometry changed — a resized emulator window, never the panel — so
    // the page in hand is the wrong size. Ask for a whole one rather than
    // leaving a gap or a clipped row.
    onPageSizeChanged: {
        if (screen.pageSize > 0 && grid.count > 0 && grid.count !== screen.pageSize)
            screen.turnTo(1)
    }

    function turnTo(page) {
        screen.pendingPage = page
        screen.busy = true
        screen.pageRequested(page)
    }

    // Asks for every cover on the page in one go. Called when the page lands,
    // not per tile: the set is what matters, and the backend cancels whatever
    // the previous set left in flight.
    function requestVisibleCovers() {
        var wanted = []
        for (var i = 0; i < grid.count; ++i) {
            var row = screen.model.get(i)
            if (row.coverUrl && row.coverUrl.length > 0 && row.coverPath.length === 0)
                wanted.push({"seriesId": row.seriesId, "url": row.coverUrl})
        }
        screen.coversRequested(wanted)
    }

    // ---- search bar --------------------------------------------------------

    Item {
        id: searchBar
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: Style.rowHeight

        Rectangle {
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: browseButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            height: Style.buttonHeight
            color: Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors {
                    left: parent.left; leftMargin: Style.gap
                    right: parent.right; rightMargin: Style.gap
                    verticalCenter: parent.verticalCenter
                }
                elide: Text.ElideRight
                text: screen.query.length > 0 ? screen.query : "Search this source"
                font.pointSize: Style.bodySize
                color: screen.query.length > 0 ? Style.ink : Style.rule
            }

            MouseArea {
                anchors.fill: parent
                onClicked: screen.searching = true
            }
        }

        Rectangle {
            id: browseButton
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            width: 180
            height: Style.buttonHeight
            color: browseArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Latest"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: browseArea
                anchors.fill: parent
                onClicked: {
                    screen.query = ""
                    screen.searching = false
                    screen.browseRequested()
                }
            }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    // ---- the grid ----------------------------------------------------------
    //
    // The viewport is fixed between the search bar and the pager, and does not
    // move when the keyboard opens — the keyboard is anchored above the pager
    // and draws over the tiles instead. A control that jumps out from under a
    // thumb is worse than the scroll it replaced (PLAN §12.1).

    Item {
        id: viewport
        anchors {
            top: searchBar.bottom
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
            bottom: pagerBar.top
        }

        GridView {
            id: grid
            anchors { top: parent.top; left: parent.left; right: parent.right }
            // Whole rows only. The remainder is left blank rather than showing
            // a half row, which is what would invite a swipe.
            height: screen.rowsPerPage * grid.cellHeight
            clip: true

            // Nothing flicks, nothing scrolls, nothing is thrown. The model
            // holds this page and only this page.
            interactive: false
            // No off-screen delegates: a tile that is not on this page must not
            // hold a decoded cover (PLAN §6 M3) or ask for one.
            cacheBuffer: 0

            cellWidth: Math.floor(width / screen.columns)
            cellHeight: Math.floor(cellWidth * 1.5) + 56

            delegate: Item {
                width: grid.cellWidth
                height: grid.cellHeight

                Rectangle {
                    id: tile
                    anchors {
                        left: parent.left; right: parent.right
                        top: parent.top
                        margins: Style.gap / 2
                    }
                    height: grid.cellHeight - 56
                    color: Style.panel
                    border.width: 1
                    border.color: Style.rule

                    Image {
                        id: cover
                        anchors.fill: parent
                        source: model.coverPath
                        fillMode: Image.PreserveAspectFit
                        // Decode at the size shown, not at the size stored.
                        sourceSize.width: tile.width
                        sourceSize.height: tile.height
                        asynchronous: true
                        // No fade-in: the panel would ghost the intermediate frames.
                        cache: true
                        visible: model.coverPath.length > 0 && cover.status !== Image.Error
                    }

                    // The placeholder covers both "no cover was offered" and "the
                    // file is there but will not decode". A tile that is blank in
                    // the second case is indistinguishable from one still loading,
                    // and the user cannot tell whether to wait.
                    Text {
                        anchors.centerIn: parent
                        text: "No cover"
                        font.pointSize: Style.smallSize
                        color: Style.muted
                        visible: model.coverPath.length === 0 || cover.status === Image.Error
                    }
                }

                Text {
                    anchors {
                        top: tile.bottom; topMargin: 6
                        left: parent.left; right: parent.right
                        leftMargin: Style.gap / 2; rightMargin: Style.gap / 2
                    }
                    text: model.title
                    font.pointSize: Style.smallSize
                    color: Style.ink
                    elide: Text.ElideRight
                    maximumLineCount: 2
                    wrapMode: Text.WordWrap
                }

                MouseArea {
                    anchors.fill: parent
                    onClicked: screen.openRequested(model.seriesId, model.title)
                }
            }
        }

        // Discrete status text rather than a spinner (PLAN §6 M3: no animations).
        Text {
            anchors.centerIn: parent
            width: parent.width - Style.margin * 2
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
            text: screen.busy ? "Fetching…" : screen.emptyMessage
            font.pointSize: Style.bodySize
            color: Style.muted
            visible: (screen.busy || screen.emptyMessage.length > 0) && grid.count === 0
        }
    }

    // ---- paging ------------------------------------------------------------

    PagerBar {
        id: pagerBar
        objectName: "seriesPager"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.hasMore
        busy: screen.busy && grid.count > 0
        pendingPage: screen.pendingPage
        onPreviousRequested: screen.turnTo(screen.page - 1)
        onNextRequested: screen.turnTo(screen.page + 1)
    }

    // ---- search input ------------------------------------------------------
    //
    // Anchored to the pager rather than to the bottom of the screen, so the
    // pager stays exactly where it was while the keyboard is up.

    Keyboard {
        id: keyboard
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        visible: screen.searching
        layout: "text"
        onKeyTyped: screen.query += text
        onBackspace: screen.query = screen.query.substring(0, screen.query.length - 1)
        onClearAll: screen.query = ""
        onSubmit: {
            screen.searching = false
            if (screen.query.length > 0) {
                screen.searchRequested(screen.query)
            }
        }
    }
}
