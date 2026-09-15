// Series detail — PLAN §6 M3: "synopsis, chapter list, per-chapter download
// state."
//
// The download *state* is M4's to fill in: this screen sends EnqueueDownload
// and shows what the backend says about each chapter. Until DownloadProgress
// arrives (M4), a chapter shows its date and nothing else, which is honest
// rather than a progress bar that never moves.

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: screen

    property alias model: list.model
    property string seriesTitle: ""
    property string synopsis: ""
    property bool busy: false

    signal downloadRequested(string chapterId)

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

                Text {
                    width: parent.width
                    elide: Text.ElideRight
                    text: (model.published.length > 0 ? model.published : "Date unknown") +
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
                    text: "Download"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: downloadArea
                    anchors.fill: parent
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
