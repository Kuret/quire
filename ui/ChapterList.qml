// Series detail — PLAN §6 M3: "synopsis, chapter list, per-chapter download
// state."
//
// This screen sends EnqueueDownload and shows what the backend says about each
// chapter. Since M5 that is a real DownloadProgress stream, so a chapter being
// fetched shows the backend's own sentence in place of its date, and the button
// says where the download has got to instead of offering to start it again.

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: screen

    property alias model: list.model
    property string seriesTitle: ""
    property string synopsis: ""
    property bool busy: false

    signal downloadRequested(string chapterId)

    // The phases are backend/service's; the two words each maps to are the
    // view's, and they are the only wording this file invents.
    function buttonLabel(state) {
        switch (state) {
        case "done":   return "In library"
        case "failed": return "Retry"
        case "":       return "Download"
        default:       return "Working…"
        }
    }

    function canDownload(state) {
        return state === "" || state === "failed"
    }

    ListView {
        id: list
        anchors.fill: parent
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        flickDeceleration: 10000
        maximumFlickVelocity: 1600

        header: Item {
            width: list.width
            height: synopsisText.height + Style.margin * 2

            Text {
                id: synopsisText
                anchors {
                    top: parent.top; topMargin: Style.margin
                    left: parent.left; leftMargin: Style.margin
                    right: parent.right; rightMargin: Style.margin
                }
                wrapMode: Text.WordWrap
                text: screen.synopsis.length > 0 ? screen.synopsis
                                                 : (screen.busy ? "Fetching…" : "No description.")
                font.pointSize: Style.bodySize
                color: Style.muted
            }

            Rectangle {
                anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                height: Style.hairline
                color: Style.rule
            }
        }

        delegate: Item {
            width: list.width
            height: Style.rowHeight

            Column {
                anchors {
                    left: parent.left; leftMargin: Style.margin
                    right: downloadButton.left; rightMargin: Style.gap
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

                // A download in progress replaces the date line rather than
                // adding a row: the backend already sends a finished sentence,
                // and the state the user is waiting on should be the line they
                // read first.
                Text {
                    width: parent.width
                    elide: Text.ElideRight
                    text: model.downloadMessage.length > 0
                          ? model.downloadMessage
                          : (model.published.length > 0 ? model.published : "Date unknown") +
                            (model.scanlator.length > 0 ? " · " + model.scanlator : "")
                    font.pointSize: Style.smallSize
                    color: Style.muted
                }
            }

            Rectangle {
                id: downloadButton
                anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
                width: 180
                height: Style.buttonHeight
                color: downloadArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    anchors.centerIn: parent
                    text: screen.buttonLabel(model.downloadState)
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: downloadArea
                    anchors.fill: parent
                    enabled: screen.canDownload(model.downloadState)
                    onClicked: screen.downloadRequested(model.chapterId)
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
        text: screen.busy ? "Fetching…" : "No chapters listed."
        font.pointSize: Style.bodySize
        color: Style.muted
        visible: list.count === 0
    }
}
