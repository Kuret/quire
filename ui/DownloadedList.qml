// Downloaded series — PLAN §12.5.
//
// Asked for because downloads and the watched list are different sets: "since
// we can download stuff without watching them, the watch list on its own is not
// enough". Every series with at least one volume on the tablet is here, whether
// or not anyone is watching it.
//
// Every word comes from backend/service/downloaded.go — the count ("3
// downloads"), the note on a row whose source has been removed, the empty
// state. This file pluralises nothing and formats nothing (PLAN §2).
//
// **A row is one (source, series) pair and always names its source.** That is
// the navigational half of the request — "otherwise we dont know which one to
// link to" — and it is applied always rather than only when two rows collide,
// because a label that appears conditionally is one the user cannot rely on.
//
// Paged like every other list here (PLAN §12.1): a fixed number of whole rows
// and a pager, never a scrolling view.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: screen

    property alias model: list.model

    // The backend's line for a library with nothing in it. Empty means there is
    // something to show.
    property string emptyNote: ""

    property int page: 1
    // From the model rather than the view: a ListView updates its own count
    // during a layout pass, so a page count taken from it lags by a frame.
    readonly property int rowCount: screen.model ? screen.model.count : 0
    readonly property int pageSize: Paging.rowsPerPage(viewport.height, Style.rowHeight)
    readonly property int totalPages: Paging.pageCount(screen.rowCount, screen.pageSize)

    onTotalPagesChanged: screen.page = Paging.clampPage(screen.page, screen.totalPages)

    // The pair, and the title, because opening a series needs all three: the
    // source to browse, the series to fetch, and the name to put in the header
    // before the detail arrives.
    signal openRequested(string sourceId, string sourceName, string seriesId, string title)

    // ---- the list ----------------------------------------------------------

    Item {
        id: viewport
        anchors {
            top: parent.top
            left: parent.left; right: parent.right
            bottom: pagerBar.top
        }

        ListView {
            id: list
            objectName: "downloadedRows"
            anchors { top: parent.top; left: parent.left; right: parent.right }
            // Whole rows only; the remainder is blank rather than a half row.
            height: Paging.rowsPerPage(viewport.height, Style.rowHeight) * Style.rowHeight
            clip: true

            interactive: false
            cacheBuffer: 0
            contentY: Paging.firstIndex(screen.page, screen.pageSize) * Style.rowHeight

            delegate: Item {
                width: list.width
                height: Style.rowHeight

                Column {
                    anchors {
                        left: parent.left; leftMargin: Style.margin
                        right: parent.right; rightMargin: Style.margin
                        verticalCenter: parent.verticalCenter
                    }
                    spacing: 4

                    Text {
                        width: parent.width
                        elide: Text.ElideRight
                        text: model.title
                        font.pointSize: Style.bodySize
                        // A row that cannot be opened says so by looking quiet
                        // as well as by saying it: the note below is the
                        // sentence, this is the glance.
                        color: model.openable ? Style.ink : Style.muted
                    }

                    // The source, on every row, and what is downloaded. Two
                    // facts the user needs before tapping: which of their
                    // sources this came from, and how much is already here.
                    Text {
                        width: parent.width
                        wrapMode: Text.WordWrap
                        maximumLineCount: 2
                        elide: Text.ElideRight
                        text: model.openable
                              ? model.sourceName + " · " + model.detail
                              : model.sourceName + " · " + model.detail + " — " + model.note
                        font.pointSize: Style.smallSize
                        color: Style.muted
                    }
                }

                MouseArea {
                    objectName: "downloadedRowArea"
                    anchors.fill: parent
                    // Inert rather than failing: there is no source left to
                    // browse, and a tap that goes nowhere quietly is worse than
                    // a row that never offered.
                    enabled: model.openable
                    onClicked: screen.openRequested(model.sourceId, model.sourceName,
                                                    model.seriesId, model.title)
                }

                Rectangle {
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    height: Style.hairline
                    color: Style.rule
                }
            }
        }

        // The empty state, in the backend's words.
        Text {
            objectName: "downloadedEmpty"
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            wrapMode: Text.WordWrap
            horizontalAlignment: Text.AlignHCenter
            text: screen.emptyNote
            font.pointSize: Style.bodySize
            color: Style.muted
            visible: screen.rowCount === 0 && screen.emptyNote.length > 0
        }
    }

    // ---- paging ------------------------------------------------------------

    PagerBar {
        id: pagerBar
        objectName: "downloadedPager"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.page < screen.totalPages
        onPreviousRequested: screen.page = Paging.clampPage(screen.page - 1, screen.totalPages)
        onNextRequested: screen.page = Paging.clampPage(screen.page + 1, screen.totalPages)
    }
}
