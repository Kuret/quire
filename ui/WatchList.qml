// Watched series — PLAN §12.2's "new since you last looked".
//
// Every word on this screen was composed in backend/service/watch.go: the badge
// ("3 new chapters"), the status line ("Up to date", "Couldn’t check", "Checking…",
// "Not checked yet") and the failure detail all arrive as strings and are drawn
// verbatim. This file pluralises nothing, formats no dates and maps no state to
// words — PLAN §2, and the backend has already done the work.
//
// What it does decide is layout, and one thing that is genuinely a layout
// decision: a check round streams its results in, one series at a time, while
// the user is standing on a page. Rows are therefore updated *in place* and
// never reordered (see Main.qml's reconcileWatched), so a result landing for a
// series on another page costs nothing on this one — no reflow, no repaint, and
// above all no silent change of which series the current page holds.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: screen

    property alias model: list.model

    property int page: 1
    // The count comes from the model, not from the view. A ListView updates its
    // own count during a layout pass, so a page count derived from it lags the
    // model by a frame — and a frame on this panel is a repaint.
    readonly property int rowCount: screen.model ? screen.model.count : 0
    readonly property int pageSize: Paging.rowsPerPage(viewport.height, Style.rowHeight)
    readonly property int totalPages: Paging.pageCount(screen.rowCount, screen.pageSize)

    // Only a watch being dropped, or a source being removed, shortens the list.
    // An update landing mid-round does not, so this does not fire during a
    // check round and the page the user is on stays put.
    onTotalPagesChanged: screen.page = Paging.clampPage(screen.page, screen.totalPages)

    signal checkRequested()
    signal unwatchRequested(string sourceId, string seriesId)
    signal openRequested(string sourceId, string sourceName, string seriesId, string title)

    // The row whose strip is open, held by its two ids rather than an index:
    // an index would point at a different series the moment a page turns or a
    // watch is dropped. The strip is where a failure's detail sentence lives,
    // because it is too long for a row of fixed height and the row still says
    // "Couldn’t check" without it.
    property string stripSourceId: ""
    property string stripSeriesId: ""
    property string stripText: ""

    function closeStrip() {
        screen.stripSourceId = ""
        screen.stripSeriesId = ""
        screen.stripText = ""
    }

    function isStripped(sourceId, seriesId) {
        return screen.stripSourceId === sourceId && screen.stripSeriesId === seriesId
    }

    // ---- check now ---------------------------------------------------------

    Item {
        id: checkBar
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: Style.rowHeight

        Text {
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: checkButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            elide: Text.ElideRight
            wrapMode: Text.WordWrap
            maximumLineCount: 2
            // PLAN §12.2: never on a timer, never in the background. Saying so
            // is the difference between "Quire is not watching" and "Quire
            // looks when you open it", which is the actual behaviour.
            text: "Checked when you open Quire, and whenever you ask."
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        Rectangle {
            id: checkButton
            objectName: "checkNowButton"
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            width: 220
            height: Style.buttonHeight
            color: checkArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: screen.rowCount > 0 ? Style.ink : Style.rule
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Check now"
                font.pointSize: Style.smallSize
                color: screen.rowCount > 0 ? Style.ink : Style.rule
            }

            MouseArea {
                id: checkArea
                anchors.fill: parent
                enabled: screen.rowCount > 0
                onClicked: screen.checkRequested()
            }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    // ---- the list ----------------------------------------------------------

    Item {
        id: viewport
        anchors {
            top: checkBar.bottom
            left: parent.left; right: parent.right
            bottom: pagerBar.top
        }

        ListView {
            id: list
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
                        right: badge.left; rightMargin: Style.gap
                        verticalCenter: parent.verticalCenter
                    }
                    spacing: 4

                    Text {
                        width: parent.width
                        elide: Text.ElideRight
                        text: model.title
                        font.pointSize: Style.bodySize
                        color: Style.ink
                    }

                    // The backend's sentence, then which site it came from.
                    // Someone watching the same series on two sources needs to
                    // be able to tell the rows apart.
                    Text {
                        width: parent.width
                        elide: Text.ElideRight
                        text: model.status +
                              (model.sourceName.length > 0 ? " · " + model.sourceName : "")
                        font.pointSize: Style.smallSize
                        color: model.state === "failed" ? Style.ink : Style.muted
                    }
                }

                // The badge survives a failed check on purpose: the count was
                // established by a check that did succeed and those chapters
                // really are unread, so a row can carry a badge and a warning
                // at once (PLAN §12.2).
                Rectangle {
                    id: badge
                    objectName: "watchBadge"
                    anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
                    width: visible ? badgeText.width + Style.gap * 2 : 0
                    height: Style.buttonHeight - Style.gap
                    color: Style.panel
                    border.width: 2
                    border.color: Style.ink
                    radius: 6
                    visible: model.badge.length > 0

                    Text {
                        id: badgeText
                        anchors.centerIn: parent
                        text: model.badge
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }
                }

                MouseArea {
                    anchors.fill: parent
                    // Opening the series is what clears the badge, and the
                    // backend is what clears it: this only navigates.
                    onClicked: screen.openRequested(model.sourceId, model.sourceName,
                                                    model.seriesId, model.title)
                    onPressAndHold: {
                        if (screen.isStripped(model.sourceId, model.seriesId)) {
                            screen.closeStrip()
                            return
                        }
                        screen.stripSourceId = model.sourceId
                        screen.stripSeriesId = model.seriesId
                        screen.stripText = model.detail.length > 0 ? model.detail : model.status
                    }
                }

                Rectangle {
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    height: Style.hairline
                    color: Style.rule
                }
            }
        }

        Text {
            anchors.centerIn: parent
            width: parent.width - Style.margin * 2
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
            text: "Nothing watched yet. Open a series and tap Watch to be told " +
                  "when it gains a chapter."
            font.pointSize: Style.bodySize
            color: Style.muted
            visible: screen.rowCount === 0
        }
    }

    // ---- the strip ---------------------------------------------------------
    //
    // Over the foot of the list rather than inside the row, for the same reason
    // as everywhere else: a row that expands pushes the rows below it off a
    // page of fixed size (PLAN §12.1).
    Rectangle {
        id: strip
        objectName: "watchStrip"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        height: Style.rowHeight
        color: Style.paper
        visible: screen.stripSeriesId.length > 0

        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: Style.hairline
            color: Style.rule
        }

        Text {
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: unwatchButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            wrapMode: Text.WordWrap
            maximumLineCount: 2
            elide: Text.ElideRight
            text: screen.stripText
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        Rectangle {
            id: unwatchButton
            objectName: "unwatchButton"
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            width: 260
            height: Style.buttonHeight
            color: unwatchArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Stop watching"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: unwatchArea
                anchors.fill: parent
                onClicked: {
                    screen.unwatchRequested(screen.stripSourceId, screen.stripSeriesId)
                    screen.closeStrip()
                }
            }
        }
    }

    // ---- paging ------------------------------------------------------------

    PagerBar {
        id: pagerBar
        objectName: "watchPager"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.page < screen.totalPages
        onPreviousRequested: screen.page = Paging.clampPage(screen.page - 1, screen.totalPages)
        onNextRequested: screen.page = Paging.clampPage(screen.page + 1, screen.totalPages)
    }
}
