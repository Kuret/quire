// Series detail — PLAN §6 M3: "synopsis, chapter list, per-chapter download
// state."
//
// This screen sends EnqueueDownload and shows what the backend says about each
// chapter. Since M5 that is a real DownloadProgress stream, so a chapter being
// fetched shows the backend's own sentence in place of its date, and the button
// says where the download has got to instead of offering to start it again.
//
// PLAN §12.1: it turns pages rather than scrolling. The whole chapter list
// arrives in one message, so the window into it is a view concern and stays
// here; what does *not* stay here is anything the backend could decide, which
// is why every sentence below is still the backend's.
//
// Two things moved out of the rows to make a page a fixed number of whole rows:
// the synopsis, which is now a band above the list rather than a list header of
// unpredictable height, and the confirm strip, which is now drawn over the foot
// of the list. Both used to change how much of the list fitted; on a paged
// screen that would push rows off the page or leave a half row at the bottom,
// which is the scrolling problem in miniature.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: screen

    property alias model: list.model
    property string seriesTitle: ""
    property string synopsis: ""
    property bool busy: false

    // Whether this series is watched (PLAN §12.2). It is set from the watched
    // list the backend pushes, never toggled locally: a tap sends the message
    // and the answer comes back, so the button cannot end up disagreeing with
    // the store after a failed round trip.
    property bool watched: false

    signal downloadRequested(string chapterId)
    signal downloadConfirmed(string chapterId)
    signal downloadCancelled(string chapterId)
    signal readRequested(string documentUuid)
    signal watchRequested()
    signal unwatchRequested()

    // Where in the list we are. Everything is in hand, so the total is always
    // known and the label can always say "of".
    property int page: 1
    // From the model, not the view: a ListView updates its own count during a
    // layout pass, so a page count taken from it lags by a frame.
    readonly property int rowCount: screen.model ? screen.model.count : 0
    readonly property int pageSize: Paging.rowsPerPage(viewport.height, Style.rowHeight)
    readonly property int totalPages: Paging.pageCount(screen.rowCount, screen.pageSize)

    onTotalPagesChanged: screen.page = Paging.clampPage(screen.page, screen.totalPages)

    // The chapter whose confirm strip is open, and the backend's question about
    // it. Only ever one: the strip asks a question, and two open questions is
    // two ways to tap the wrong answer. The message is carried rather than read
    // off the row because the strip is no longer inside the row.
    property string confirmingId: ""
    property string confirmingMessage: ""

    // The phases are backend/service's; the words each maps to are the view's,
    // and they are the only wording this file invents. Every sentence shown to
    // the user is composed in the backend (PLAN §2).
    function buttonLabel(state, documentUuid) {
        if (documentUuid)
            return "Read"
        switch (state) {
        case "confirm":   return "Cancel"
        case "done":      return "In library"
        case "failed":    return "Retry"
        case "cancelled": return "Download"
        case "":          return "Download"
        // A download in flight offers the way out. A volume is minutes of work
        // and up to 90 MB on a battery, so an inert button here is the thing
        // that makes the app feel broken (PLAN §7.1).
        default:          return "Stop"
        }
    }

    // canTap is true when the button does something. Every state now does.
    function canTap(state, documentUuid) {
        return true
    }

    function tapped(chapterId, state, documentUuid) {
        if (documentUuid) {
            screen.readRequested(documentUuid)
            return
        }
        switch (state) {
        case "confirm":
            screen.confirmingId = ""
            screen.confirmingMessage = ""
            return
        case "":
        case "failed":
        case "cancelled":
        case "done":
            screen.downloadRequested(chapterId)
            return
        default:
            screen.downloadCancelled(chapterId)
        }
    }

    // ---- synopsis ----------------------------------------------------------
    //
    // A band of fixed height, so that every page of chapters holds the same
    // number of rows. Three lines is what a description is worth next to the
    // thing the screen is actually for.
    Item {
        id: synopsisBand
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: Style.margin * 2 + lineProbe.height * 3

        // One line of body text, measured rather than guessed, so the band is
        // exactly three lines tall whatever the platform's font metrics say.
        Text {
            id: lineProbe
            visible: false
            text: "Ag"
            font.pointSize: Style.bodySize
        }

        Text {
            id: synopsisText
            anchors {
                top: parent.top; topMargin: Style.margin
                left: parent.left; leftMargin: Style.margin
                right: watchButton.left; rightMargin: Style.gap
            }
            wrapMode: Text.WordWrap
            maximumLineCount: 3
            elide: Text.ElideRight
            text: screen.synopsis.length > 0 ? screen.synopsis
                                             : (screen.busy ? "Fetching…" : "No description.")
            font.pointSize: Style.bodySize
            color: Style.muted
        }

        // Watching is offered here because this is where the baseline comes
        // from: the backend seeds a new watch from the chapter list it last
        // served, which is the one on screen. Watching from anywhere else
        // would make the first check announce the whole back catalogue as new
        // (PLAN §12.2).
        Rectangle {
            id: watchButton
            objectName: "watchButton"
            anchors {
                right: parent.right; rightMargin: Style.margin
                top: parent.top; topMargin: Style.margin
            }
            width: 220
            height: Style.buttonHeight
            color: watchArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: screen.watched ? "Unwatch" : "Watch"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: watchArea
                anchors.fill: parent
                onClicked: {
                    if (screen.watched)
                        screen.unwatchRequested()
                    else
                        screen.watchRequested()
                }
            }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    // ---- the chapters ------------------------------------------------------

    Item {
        id: viewport
        anchors {
            top: synopsisBand.bottom
            left: parent.left; right: parent.right
            bottom: pagerBar.top
        }

        ListView {
            id: list
            anchors { top: parent.top; left: parent.left; right: parent.right }
            // Whole rows only; the remainder is blank rather than a half row.
            height: Paging.rowsPerPage(viewport.height, Style.rowHeight) * Style.rowHeight
            clip: true

            // Nothing flicks. The page is set outright, which is one settled
            // full refresh instead of a stream of partial ones.
            interactive: false
            cacheBuffer: 0
            contentY: Paging.firstIndex(screen.page, screen.pageSize) * Style.rowHeight

            delegate: Item {
                id: entry
                width: list.width
                height: Style.rowHeight

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
                        //
                        // It wraps. A progress line is short, but a failure is a whole
                        // plain-language sentence (PLAN §6 M3) and eliding it at one
                        // line cut the answer off mid-word — the reason the download
                        // stopped was the part that went missing. The row affords two
                        // small lines under the title, which is the rest of the
                        // sentence.
                        Text {
                            width: parent.width
                            wrapMode: Text.WordWrap
                            maximumLineCount: 2
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
            visible: screen.rowCount === 0
        }
    }

    // ---- the confirm strip -------------------------------------------------
    //
    // A tap on Download queues a whole volume — up to ten chapters and a few
    // hundred megabytes — so PLAN §6 M3's "say what you are doing" means asking
    // first. One step, and the question itself is the backend's sentence, not
    // this file's.
    Rectangle {
        id: confirmStrip
        objectName: "confirmStrip"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        height: Style.rowHeight
        color: Style.paper
        visible: screen.confirmingId.length > 0 && screen.confirmingMessage.length > 0

        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: Style.hairline
            color: Style.rule
        }

        Text {
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: confirmButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            elide: Text.ElideRight
            maximumLineCount: 2
            wrapMode: Text.WordWrap
            text: screen.confirmingMessage
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        Rectangle {
            id: confirmButton
            objectName: "confirmDownloadButton"
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            width: 220
            height: Style.buttonHeight
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
                    var id = screen.confirmingId
                    screen.confirmingId = ""
                    screen.confirmingMessage = ""
                    screen.downloadConfirmed(id)
                }
            }
        }
    }

    // ---- paging ------------------------------------------------------------

    PagerBar {
        id: pagerBar
        objectName: "chapterPager"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.page < screen.totalPages
        onPreviousRequested: screen.page = Paging.clampPage(screen.page - 1, screen.totalPages)
        onNextRequested: screen.page = Paging.clampPage(screen.page + 1, screen.totalPages)
    }
}
