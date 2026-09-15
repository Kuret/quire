// The series grid — PLAN §6 M3: "Series grid with covers. Cache on disk,
// downscale hard, never hold more than a screenful in memory."
//
// The memory rule is kept by construction: a cover is requested when its tile
// is created and released when the tile is destroyed, so what is resident is
// what GridView has instantiated, which is a screenful plus its cache buffer.
// The images themselves are files on disk that the backend downscaled — nothing
// image-shaped ever crosses the socket (PLAN §7.1).

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: screen

    property alias model: grid.model
    property bool busy: false
    property string emptyMessage: ""
    property string query: ""
    property bool searching: false

    signal searchRequested(string query)
    signal browseRequested()
    signal coverRequested(string seriesId, string url)
    signal openRequested(string seriesId, string title)

    function reset() {
        screen.query = ""
        screen.searching = false
        screen.emptyMessage = ""
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

    GridView {
        id: grid
        anchors {
            top: searchBar.bottom
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
            bottom: screen.searching ? keyboard.top : parent.bottom
        }
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        flickDeceleration: 10000
        maximumFlickVelocity: 1600

        // Three columns on the panel's short edge; the tile is 2:3, the shape
        // covers are published at and the shape the cache writes.
        cellWidth: Math.floor(width / 3)
        cellHeight: Math.floor(cellWidth * 1.5) + 56

        // One screenful either side. Larger would hold more covers in memory
        // than PLAN §6 M3 allows; smaller would refetch on every flick.
        cacheBuffer: Math.round(cellHeight * 3)

        delegate: Item {
            width: grid.cellWidth
            height: grid.cellHeight

            // The cover is asked for when the tile appears and never again:
            // the backend answers from its disk cache if it has one.
            Component.onCompleted: {
                if (model.coverUrl && model.coverUrl.length > 0 && model.coverPath.length === 0) {
                    screen.coverRequested(model.seriesId, model.coverUrl)
                }
            }

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
        anchors.centerIn: grid
        width: grid.width - Style.margin * 2
        horizontalAlignment: Text.AlignHCenter
        wrapMode: Text.WordWrap
        text: screen.busy ? "Fetching…" : screen.emptyMessage
        font.pointSize: Style.bodySize
        color: Style.muted
        visible: (screen.busy || screen.emptyMessage.length > 0) && grid.count === 0
    }

    // ---- search input ------------------------------------------------------

    Keyboard {
        id: keyboard
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        visible: screen.searching
        layout: "text"
        onKeyTyped: screen.query += character
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
