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
    signal downloadConfirmed(string chapterId)
    signal readRequested(string documentUuid)

    // The chapter whose confirm strip is open. Only ever one: the strip asks a
    // question, and two open questions is two ways to tap the wrong answer.
    property string confirmingId: ""

    // The phases are backend/service's; the words each maps to are the view's,
    // and they are the only wording this file invents. Every sentence shown to
    // the user is composed in the backend (PLAN §2).
    function buttonLabel(state, documentUuid) {
        if (documentUuid)
            return "Read"
        switch (state) {
        case "confirm": return "Cancel"
        case "done":    return "In library"
        case "failed":  return "Retry"
        case "":        return "Download"
        default:        return "Working…"
        }
    }

    // canTap is true when the button does something. A download in flight is
    // not cancellable yet, so the button is inert rather than lying.
    function canTap(state, documentUuid) {
        if (documentUuid)
            return true
        return state === "" || state === "failed" || state === "confirm"
    }

    function tapped(chapterId, state, documentUuid) {
        if (documentUuid) {
            screen.readRequested(documentUuid)
            return
        }
        if (state === "confirm") {
            screen.confirmingId = ""
            return
        }
        screen.downloadRequested(chapterId)
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
            id: entry
            width: list.width

            readonly property bool confirming:
                screen.confirmingId === model.chapterId && model.downloadMessage.length > 0

            height: Style.rowHeight + (entry.confirming ? confirmStrip.height : 0)

            Item {
                id: row
                anchors { top: parent.top; left: parent.left; right: parent.right }
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
                        text: model.downloadMessage.length > 0 && model.downloadState !== "confirm"
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
                        text: screen.buttonLabel(model.downloadState, model.documentUuid)
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: downloadArea
                        anchors.fill: parent
                        enabled: screen.canTap(model.downloadState, model.documentUuid)
                        onClicked: screen.tapped(model.chapterId, model.downloadState, model.documentUuid)
                    }
                }
            }

            // The confirm strip. A tap on Download queues a whole volume — up
            // to ten chapters and a few hundred megabytes — so PLAN §6 M3's
            // "say what you are doing" means asking first. One step, and the
            // question itself is the backend's sentence, not this file's.
            Item {
                id: confirmStrip
                anchors { top: row.bottom; left: parent.left; right: parent.right }
                height: entry.confirming ? confirmText.height + Style.gap * 2 : 0
                visible: entry.confirming

                Text {
                    id: confirmText
                    anchors {
                        left: parent.left; leftMargin: Style.margin
                        right: confirmButton.left; rightMargin: Style.gap
                        top: parent.top; topMargin: Style.gap
                    }
                    wrapMode: Text.WordWrap
                    text: model.downloadMessage
                    font.pointSize: Style.smallSize
                    color: Style.muted
                }

                Rectangle {
                    id: confirmButton
                    anchors { right: parent.right; rightMargin: Style.margin; top: parent.top; topMargin: Style.gap }
                    width: 180
                    height: Style.buttonHeight - Style.gap
                    color: confirmArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: Style.ink
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Download all"
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: confirmArea
                        anchors.fill: parent
                        onClicked: {
                            screen.confirmingId = ""
                            screen.downloadConfirmed(model.chapterId)
                        }
                    }
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
