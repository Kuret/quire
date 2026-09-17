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

    // Deleting every download of a series (PLAN §12.5). Two signals because it
    // is two steps and the question in between is the backend's, exactly as on
    // the chapter list: deleteRequested asks for the sentence, deleteConfirmed
    // is the answer. An accidental tap can only ever reach the first one.
    //
    // **It asks, where the multi-select queue does not.** Queueing is
    // reversible and costs time; this destroys every download of a series and
    // the only way back is to fetch them all again.
    signal deleteRequested(string sourceId, string seriesId)
    signal deleteConfirmed(string sourceId, string seriesId)

    // The series whose confirm strip is open, and the backend's question about
    // it. Only ever one: two open questions is two ways to answer the one you
    // were not looking at.
    property string confirmingSourceId: ""
    property string confirmingSeriesId: ""
    property string confirmingMessage: ""

    // askToDelete opens the question for a row. The sentence comes back from
    // the backend, which is the only side that knows how many downloads the
    // series has and what it is called.
    function askToDelete(sourceId, seriesId) {
        if (!seriesId)
            return
        screen.confirmingSourceId = ""
        screen.confirmingSeriesId = ""
        screen.confirmingMessage = ""
        screen.deleteRequested(sourceId, seriesId)
    }

    // closeConfirm puts the strip away without answering it.
    function closeConfirm() {
        screen.confirmingSourceId = ""
        screen.confirmingSeriesId = ""
        screen.confirmingMessage = ""
    }

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

                // The row body still opens the series — asked for explicitly,
                // "clicking on the rest of the row still should link to that
                // manga to delete individual chapters" — so the tap target
                // stops where the delete button starts rather than covering it.
                MouseArea {
                    objectName: "downloadedRowArea"
                    anchors {
                        top: parent.top; bottom: parent.bottom
                        left: parent.left; right: deleteButton.left
                    }
                    // Inert rather than failing: there is no source left to
                    // browse, and a tap that goes nowhere quietly is worse than
                    // a row that never offered.
                    enabled: model.openable
                    onClicked: screen.openRequested(model.sourceId, model.sourceName,
                                                    model.seriesId, model.title)
                }

                // Delete, on every row, including the rows whose source has
                // been removed: those downloads are still on the tablet and
                // still the user's to get rid of, and this screen is now the
                // only way to reach them.
                Rectangle {
                    id: deleteButton
                    objectName: "deleteSeriesButton"
                    anchors {
                        right: parent.right; rightMargin: Style.margin
                        verticalCenter: parent.verticalCenter
                    }
                    width: 150
                    height: Style.buttonHeight
                    color: deleteArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: Style.rule
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Delete"
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: deleteArea
                        objectName: "deleteSeriesArea"
                        anchors.fill: parent
                        onClicked: screen.askToDelete(model.sourceId, model.seriesId)
                    }
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

    // ---- the question -------------------------------------------------------
    //
    // It takes the pager's place while it is open rather than pushing the list
    // around: a question that moves the rows under the user's finger is a
    // question answered by accident. The pager is not needed to answer it.
    Item {
        id: confirmStrip
        objectName: "downloadedConfirmStrip"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        height: Style.rowHeight
        visible: screen.confirmingSeriesId.length > 0

        Text {
            objectName: "downloadedConfirmMessage"
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: answers.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            // The backend's sentence, rendered and not edited (PLAN §2).
            text: screen.confirmingMessage
            wrapMode: Text.WordWrap
            maximumLineCount: 2
            elide: Text.ElideRight
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        // Two buttons, because this is the destructive question on the screen
        // and "not this one" must be as easy to tap as the thing it protects.
        Row {
            id: answers
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            spacing: Style.gap

            Rectangle {
                objectName: "keepSeriesButton"
                width: 160
                height: Style.buttonHeight
                color: keepSeriesArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    anchors.centerIn: parent
                    text: "Keep"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: keepSeriesArea
                    objectName: "keepSeriesArea"
                    anchors.fill: parent
                    onClicked: screen.closeConfirm()
                }
            }

            Rectangle {
                objectName: "confirmDeleteSeriesButton"
                width: 230
                height: Style.buttonHeight
                color: confirmDeleteSeriesArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.rule
                radius: 6

                Text {
                    anchors.centerIn: parent
                    // What the button does, in the words of the question beside
                    // it. Nothing here promises somewhere to recover them from.
                    text: "Delete all for good"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: confirmDeleteSeriesArea
                    objectName: "confirmDeleteSeriesArea"
                    anchors.fill: parent
                    onClicked: {
                        var sourceId = screen.confirmingSourceId
                        var seriesId = screen.confirmingSeriesId
                        screen.closeConfirm()
                        screen.deleteConfirmed(sourceId, seriesId)
                    }
                }
            }
        }
    }

    PagerBar {
        id: pagerBar
        objectName: "downloadedPager"
        visible: !confirmStrip.visible
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.page < screen.totalPages
        onPreviousRequested: screen.page = Paging.clampPage(screen.page - 1, screen.totalPages)
        onNextRequested: screen.page = Paging.clampPage(screen.page + 1, screen.totalPages)
    }
}
