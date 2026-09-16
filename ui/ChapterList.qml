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

    // The volume view (PLAN §6 M4, revised 2026-09-16). Chapters are the
    // default and are always here; volumes are a second view of the same
    // series, offered only when the backend sent rows for one.
    //
    // Whether volumes exist is not decided here. The backend sends the rows or
    // sends none, because the rule is about the data — the source's own labels,
    // and whether the reading order is known at all — and an empty tab is a
    // worse answer than no tab.
    property alias volumeModel: volumeList.model

    readonly property bool hasVolumes: screen.volumeModel ? screen.volumeModel.count > 0 : false

    // Which view is on screen. It is never "volumes" without rows to show: a
    // series that loses its volumes on a refresh must not leave the screen
    // looking at nothing.
    property string view: "chapters"
    readonly property bool showingVolumes: screen.view === "volumes" && screen.hasVolumes

    onHasVolumesChanged: {
        if (!screen.hasVolumes && screen.view !== "chapters")
            screen.view = "chapters"
    }

    // A view holds a different number of rows, so the page starts again. Guarded
    // so setting the view to what it already is repaints nothing.
    onViewChanged: {
        screen.page = 1
        screen.closeConfirm()
        // Chapters and Volumes are two lists of different things. A selection
        // made in one of them means nothing in the other, and carrying it
        // across would queue rows the user can no longer see.
        screen.leaveSelection()
    }

    // A page turn hides the rows that were selected. The selection goes with
    // them for the same reason it goes when the mode is left: what is not on
    // screen cannot be checked before it is acted on.
    onPageChanged: screen.clearSelection()

    function showView(which) {
        if (screen.view !== which)
            screen.view = which
    }

    // Whether this series is watched (PLAN §12.2). It is set from the watched
    // list the backend pushes, never toggled locally: a tap sends the message
    // and the answer comes back, so the button cannot end up disagreeing with
    // the store after a failed round trip.
    property bool watched: false

    signal downloadRequested(string chapterId)
    signal downloadConfirmed(string chapterId)
    signal downloadCancelled(string chapterId)

    // The same three for a row in the volume view. Separate signals rather than
    // a grouping argument, so the one place that composes the message cannot
    // forget to say which it meant.
    signal volumeDownloadRequested(string chapterId)
    signal volumeDownloadConfirmed(string chapterId)
    signal volumeDownloadCancelled(string chapterId)

    signal readRequested(string documentUuid)

    // Deleting a download (PLAN §12.4). Two signals because it is two steps and
    // the question in between is the backend's: deleteRequested asks for it,
    // deleteConfirmed is the answer. An accidental tap can only ever reach the
    // first one.
    signal deleteRequested(string documentUuid)
    signal deleteConfirmed(string documentUuid)

    // Queueing a selection (PLAN §12.1). Two signals for the same two steps as
    // a delete: the first asks the backend for the question, the second is the
    // answer. The ids travel with both, because the selection is the view's and
    // the sentence about it is the backend's.
    signal queueRequested(var chapterIds, bool volumes)
    signal queueConfirmed(var chapterIds, bool volumes)

    signal watchRequested()
    signal unwatchRequested()

    // Where in the list we are. Everything is in hand, so the total is always
    // known and the label can always say "of".
    property int page: 1
    // From the model, not the view: a ListView updates its own count during a
    // layout pass, so a page count taken from it lags by a frame. It follows
    // whichever view is showing, so the pager counts the rows on screen.
    readonly property int rowCount: screen.showingVolumes
                                    ? (screen.volumeModel ? screen.volumeModel.count : 0)
                                    : (screen.model ? screen.model.count : 0)
    readonly property int pageSize: Paging.rowsPerPage(viewport.height, Style.rowHeight)
    readonly property int totalPages: Paging.pageCount(screen.rowCount, screen.pageSize)

    onTotalPagesChanged: screen.page = Paging.clampPage(screen.page, screen.totalPages)

    // The chapter whose confirm strip is open, and the backend's question about
    // it. Only ever one: the strip asks a question, and two open questions is
    // two ways to tap the wrong answer. The message is carried rather than read
    // off the row because the strip is no longer inside the row.
    property string confirmingId: ""
    property string confirmingMessage: ""

    // Which question the strip is asking. "download" is the volume-download
    // confirmation the strip was built for, "delete" is PLAN §12.4's, and
    // "queue" is a selection of rows. One property rather than a third strip:
    // there is one place at the foot of the list for a question, and two
    // strips fighting over it is two ways to answer the one you were not
    // looking at.
    property string confirmingKind: "download"

    // ---- selecting several rows --------------------------------------------
    //
    // An explicit mode, entered from a button, rather than a long press. A long
    // press is undiscoverable, and the press-and-hold feedback that makes it
    // legible elsewhere does not exist on e-ink: the screen simply does not
    // move until it does, by which time the user has lifted their finger.
    property bool selecting: false

    // The selected row ids, in list order. An array rather than a set, because
    // the order is the order the queue will work through and the order the user
    // is reading in.
    property var selectedIds: []

    readonly property int selectedCount: screen.selectedIds.length

    // canSelect is the whole of "only rows that can be downloaded are
    // selectable": a row already in the library, already queued or already
    // downloading has nothing to queue. Letting it be selected would let the
    // user build a selection of ten and watch four of them do nothing.
    //
    // It is deliberately the same set of states that make the row's button say
    // Download or Retry, so what is selectable is what the row already offers.
    function canSelect(state, documentUuid) {
        if (documentUuid)
            return false
        return state === "" || state === "failed" || state === "cancelled"
    }

    function isSelected(chapterId) {
        return screen.selectedIds.indexOf(chapterId) >= 0
    }

    function toggleSelected(chapterId, state, documentUuid) {
        if (!screen.selecting || !screen.canSelect(state, documentUuid))
            return
        var next = screen.selectedIds.slice()
        var at = next.indexOf(chapterId)
        if (at >= 0)
            next.splice(at, 1)
        else
            next.push(chapterId)
        // Assigned rather than mutated: a QML property holding an array emits
        // no change signal when its contents are edited in place, so a delegate
        // bound to it would keep drawing the selection it had a moment ago.
        screen.selectedIds = next
    }

    function clearSelection() {
        if (screen.selectedIds.length > 0)
            screen.selectedIds = []
    }

    // pruneSelection drops ids the list no longer offers, and is called
    // whenever the model is refilled.
    //
    // A refresh of the same series refills the rows, and a selection that
    // survives it does so only because the ids happen to match again. Two
    // things break that: a refresh that no longer lists a row, and a row that
    // comes back already downloaded. Either leaves an id selected with nothing
    // on screen carrying it — the count says 5, the user can see 4, and the
    // one they cannot see is the one they cannot take back off.
    //
    // It re-reads canSelect rather than only checking presence, so a row that
    // finished downloading while the mode was open leaves the selection the
    // same way it loses its box.
    function pruneSelection() {
        if (screen.selectedIds.length === 0)
            return
        var live = screen.showingVolumes ? screen.volumeModel : screen.model
        var kept = []
        // Built by walking the model, so what survives keeps list order — the
        // order the queue will work through.
        for (var i = 0; live && i < live.count; ++i) {
            var row = live.get(i)
            if (screen.selectedIds.indexOf(row.chapterId) >= 0
                    && screen.canSelect(row.downloadState, row.documentUuid))
                kept.push(row.chapterId)
        }
        if (kept.length !== screen.selectedIds.length)
            screen.selectedIds = kept
    }

    // enterSelection and leaveSelection are the only two doors. Leaving always
    // clears: a selection the user cannot see is a selection they will act on
    // by accident, which is the same rule the confirm strip follows.
    function enterSelection() {
        screen.closeConfirm()
        screen.selecting = true
        screen.clearSelection()
    }

    function leaveSelection() {
        screen.selecting = false
        screen.clearSelection()
    }

    // askToQueue opens the question for the selection. The sentence is the
    // backend's: it is the one that knows what a queue of this size costs, and
    // PLAN §2 keeps every sentence there.
    function askToQueue() {
        if (screen.selectedCount === 0)
            return
        screen.confirmingId = ""
        screen.confirmingMessage = ""
        screen.confirmingKind = "queue"
        screen.queueRequested(screen.selectedIds, screen.showingVolumes)
    }

    // askToDelete opens the delete question for a row. The sentence itself
    // comes back from the backend, which is the only thing that knows what the
    // document is called on the tablet.
    function askToDelete(documentUuid) {
        if (!documentUuid)
            return
        screen.confirmingId = ""
        screen.confirmingMessage = ""
        screen.confirmingKind = "delete"
        screen.deleteRequested(documentUuid)
    }

    // closeConfirm puts the strip away without answering it.
    function closeConfirm() {
        screen.confirmingId = ""
        screen.confirmingMessage = ""
        screen.confirmingKind = "download"
    }

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
            screen.closeConfirm()
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

    // The volume view's tap. Identical shape, different message: this one asks
    // for the whole volume as one file.
    function volumeTapped(chapterId, state, documentUuid) {
        if (documentUuid) {
            screen.readRequested(documentUuid)
            return
        }
        switch (state) {
        case "confirm":
            screen.closeConfirm()
            return
        case "":
        case "failed":
        case "cancelled":
        case "done":
            screen.volumeDownloadRequested(chapterId)
            return
        default:
            screen.volumeDownloadCancelled(chapterId)
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
                right: selectButton.visible ? selectButton.left : watchButton.left
                rightMargin: Style.gap
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
        // The way into selection mode. It lives in the header because that is
        // where the screen's verbs are, and it disappears while the mode is on:
        // the footer owns the mode once it is entered, and two controls that
        // both mean "stop selecting" is one more than the screen needs.
        Rectangle {
            id: selectButton
            objectName: "selectButton"
            anchors {
                right: watchButton.left; rightMargin: Style.gap
                top: parent.top; topMargin: Style.margin
            }
            width: 220
            height: Style.buttonHeight
            visible: !screen.selecting && screen.rowCount > 0
            color: selectArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Select"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: selectArea
                objectName: "selectArea"
                anchors.fill: parent
                enabled: selectButton.visible
                onClicked: screen.enterSelection()
            }
        }

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

    // ---- the view switch ---------------------------------------------------
    //
    // Two words, not a tab bar, and nothing at all when there is one view. It
    // collapses to zero height in that case so a source with no volumes gets
    // exactly the screen it had before — including the same number of rows to a
    // page, since the viewport is what decides that.
    Item {
        id: viewSwitch
        objectName: "viewSwitch"
        anchors { top: synopsisBand.bottom; left: parent.left; right: parent.right }
        visible: screen.hasVolumes
        height: visible ? Style.buttonHeight + Style.margin : 0

        Row {
            anchors {
                left: parent.left; leftMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            spacing: Style.gap

            Repeater {
                model: [{"key": "chapters", "label": "Chapters"},
                        {"key": "volumes", "label": "Volumes"}]

                Rectangle {
                    objectName: "viewButton-" + modelData.key
                    width: 220
                    height: Style.buttonHeight
                    // The view you are looking at is filled, the other is not:
                    // on e-ink a border alone is too quiet to answer "which am
                    // I on?" at a glance.
                    color: switchArea.pressed ? Style.pressed
                                              : (screen.view === modelData.key ? Style.rule : Style.paper)
                    border.width: 2
                    border.color: Style.ink
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: modelData.label
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: switchArea
                        anchors.fill: parent
                        onClicked: screen.showView(modelData.key)
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

    // ---- the chapters ------------------------------------------------------

    Item {
        id: viewport
        anchors {
            top: viewSwitch.bottom
            left: parent.left; right: parent.right
            bottom: pagerBar.top
        }

        ListView {
            id: list
            objectName: "chapterRows"
            anchors { top: parent.top; left: parent.left; right: parent.right }
            // Whole rows only; the remainder is blank rather than a half row.
            height: Paging.rowsPerPage(viewport.height, Style.rowHeight) * Style.rowHeight
            clip: true
            visible: !screen.showingVolumes

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

                    // The box is the affordance and the answer at once: it is
                    // there only on rows that can be queued, and filled only on
                    // the ones that are. A row with no box in selection mode is
                    // a row with nothing to queue — already downloaded, already
                    // waiting, or already on its way.
                    Rectangle {
                        id: selectBox
                        objectName: "selectBox"
                        anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
                        width: 40
                        height: 40
                        radius: 4
                        visible: screen.selecting && screen.canSelect(model.downloadState, model.documentUuid)
                        border.width: 2
                        border.color: Style.ink
                        color: screen.isSelected(model.chapterId) ? Style.ink : Style.paper
                    }

                    Column {
                        anchors {
                            left: selectBox.visible ? selectBox.right : parent.left
                            leftMargin: Style.margin
                            right: deleteButton.visible ? deleteButton.left : downloadButton.left
                            rightMargin: Style.gap
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: 4

                        Text {
                            width: parent.width
                            elide: Text.ElideRight
                            text: model.title
                            font.pointSize: Style.bodySize
                            // A row that cannot be queued says so quietly while
                            // the mode is on, rather than looking tappable and
                            // doing nothing.
                            color: screen.selecting
                                   && !screen.canSelect(model.downloadState, model.documentUuid)
                                   ? Style.muted : Style.ink
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

                    // Only on a row that has something to delete, and never
                    // instead of Read: the download is the thing the user came
                    // for, and a delete that sits where they expect to tap to
                    // read is a delete they will hit by accident.
                    Rectangle {
                        id: deleteButton
                        objectName: "deleteButton"
                        anchors { right: downloadButton.left; rightMargin: Style.gap; verticalCenter: parent.verticalCenter }
                        width: 140
                        height: Style.buttonHeight
                        visible: model.documentUuid && !screen.selecting ? true : false
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
                            objectName: "deleteArea"
                            anchors.fill: parent
                            enabled: deleteButton.visible
                            onClicked: screen.askToDelete(model.documentUuid)
                        }
                    }

                    // In selection mode the row is the target, not the button
                    // on it: the point of the mode is to pick several rows
                    // without waiting for each one's button to settle. The
                    // button stays on screen so the row still says what state
                    // it is in; it just stops being tappable.
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
                            enabled: !screen.selecting
                                     && screen.canTap(model.downloadState, model.documentUuid)
                            onClicked: screen.tapped(model.chapterId, model.downloadState, model.documentUuid)
                        }
                    }

                    // Declared last so it sits over the row's buttons while the
                    // mode is on, and disabled the rest of the time so it is
                    // not in the way of them.
                    MouseArea {
                        id: rowSelectArea
                        objectName: "rowSelectArea"
                        anchors.fill: parent
                        enabled: screen.selecting
                        onClicked: screen.toggleSelected(model.chapterId, model.downloadState,
                                                          model.documentUuid)
                    }
                }

                Rectangle {
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    height: Style.hairline
                    color: Style.rule
                }
            }
        }

        // ---- the volumes ---------------------------------------------------
        //
        // The same row shape, paged the same way, over the same viewport — so
        // switching views changes what is listed and nothing else about the
        // screen.
        ListView {
            id: volumeList
            objectName: "volumeRows"
            anchors { top: parent.top; left: parent.left; right: parent.right }
            height: Paging.rowsPerPage(viewport.height, Style.rowHeight) * Style.rowHeight
            clip: true
            visible: screen.showingVolumes

            interactive: false
            cacheBuffer: 0
            contentY: Paging.firstIndex(screen.page, screen.pageSize) * Style.rowHeight

            delegate: Item {
                width: volumeList.width
                height: Style.rowHeight

                Item {
                    anchors { top: parent.top; left: parent.left; right: parent.right }
                    height: Style.rowHeight

                    Rectangle {
                        id: volumeSelectBox
                        objectName: "volumeSelectBox"
                        anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
                        width: 40
                        height: 40
                        radius: 4
                        visible: screen.selecting && screen.canSelect(model.downloadState, model.documentUuid)
                        border.width: 2
                        border.color: Style.ink
                        color: screen.isSelected(model.chapterId) ? Style.ink : Style.paper
                    }

                    Column {
                        anchors {
                            left: volumeSelectBox.visible ? volumeSelectBox.right : parent.left
                            leftMargin: Style.margin
                            right: volumeDeleteButton.visible ? volumeDeleteButton.left : volumeButton.left
                            rightMargin: Style.gap
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: 4

                        Text {
                            width: parent.width
                            elide: Text.ElideRight
                            text: model.title
                            font.pointSize: Style.bodySize
                            color: screen.selecting
                                   && !screen.canSelect(model.downloadState, model.documentUuid)
                                   ? Style.muted : Style.ink
                        }

                        // What the volume holds, which is the whole reason this
                        // view is worth having: a volume that is seven chapters
                        // says so, because that is what tells you whether it is
                        // worth taking before a journey. The sentence is the
                        // backend's (PLAN §2); a download in flight replaces it
                        // with the backend's own progress line, exactly as a
                        // chapter row does.
                        Text {
                            width: parent.width
                            wrapMode: Text.WordWrap
                            maximumLineCount: 2
                            elide: Text.ElideRight
                            text: model.downloadMessage.length > 0 && model.downloadState !== "confirm"
                                  ? model.downloadMessage
                                  : model.detail
                            font.pointSize: Style.smallSize
                            color: Style.muted
                        }
                    }

                    // The same affordance as a chapter row, for the same
                    // reason: this row offers Read on a document, so it offers
                    // the way to get rid of it. One document, whichever view
                    // the user happens to be looking at it from.
                    Rectangle {
                        id: volumeDeleteButton
                        objectName: "volumeDeleteButton"
                        anchors { right: volumeButton.left; rightMargin: Style.gap; verticalCenter: parent.verticalCenter }
                        width: 140
                        height: Style.buttonHeight
                        visible: model.documentUuid && !screen.selecting ? true : false
                        color: volumeDeleteArea.pressed ? Style.pressed : Style.paper
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
                            id: volumeDeleteArea
                            objectName: "volumeDeleteArea"
                            anchors.fill: parent
                            enabled: volumeDeleteButton.visible
                            onClicked: screen.askToDelete(model.documentUuid)
                        }
                    }

                    Rectangle {
                        id: volumeButton
                        anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
                        width: 180
                        height: Style.buttonHeight
                        color: volumeArea.pressed ? Style.pressed : Style.paper
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
                            id: volumeArea
                            anchors.fill: parent
                            enabled: !screen.selecting
                                     && screen.canTap(model.downloadState, model.documentUuid)
                            onClicked: screen.volumeTapped(model.chapterId, model.downloadState,
                                                           model.documentUuid)
                        }
                    }

                    MouseArea {
                        id: volumeRowSelectArea
                        objectName: "volumeRowSelectArea"
                        anchors.fill: parent
                        enabled: screen.selecting
                        onClicked: screen.toggleSelected(model.chapterId, model.downloadState,
                                                          model.documentUuid)
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

    // ---- the selection bar -------------------------------------------------
    //
    // It stands in the same place as the confirm strip and gives way to it: one
    // place at the foot of the list for the screen to speak from, and a bar
    // under an open question is a second thing to tap while a question is
    // waiting.
    Rectangle {
        id: selectionBar
        objectName: "selectionBar"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        height: Style.rowHeight
        color: Style.paper
        visible: screen.selecting && !confirmStrip.visible

        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: Style.hairline
            color: Style.rule
        }

        // The count, because it is the thing the user is keeping in their head
        // while they tap down a list. The wording of a *question* is the
        // backend's (PLAN §2); this is a label on a control, like the buttons.
        Text {
            id: selectionCount
            objectName: "selectionCount"
            anchors {
                left: parent.left; leftMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            text: screen.selectedCount === 1 ? "1 selected" : screen.selectedCount + " selected"
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        Row {
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            spacing: Style.gap

            // The way out, and it selects nothing on the way.
            Rectangle {
                objectName: "cancelSelectionButton"
                width: 160
                height: Style.buttonHeight
                color: cancelSelectionArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    anchors.centerIn: parent
                    text: "Cancel"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: cancelSelectionArea
                    objectName: "cancelSelectionArea"
                    anchors.fill: parent
                    onClicked: screen.leaveSelection()
                }
            }

            // Inert until something is selected, rather than absent: a button
            // that appears when you have picked a row is a button you have to
            // discover twice.
            Rectangle {
                objectName: "queueSelectionButton"
                width: 240
                height: Style.buttonHeight
                color: queueSelectionArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: screen.selectedCount > 0 ? Style.ink : Style.rule
                radius: 6

                Text {
                    anchors.centerIn: parent
                    text: "Download selected"
                    font.pointSize: Style.smallSize
                    color: screen.selectedCount > 0 ? Style.ink : Style.muted
                }

                MouseArea {
                    id: queueSelectionArea
                    objectName: "queueSelectionArea"
                    anchors.fill: parent
                    enabled: screen.selectedCount > 0
                    onClicked: screen.askToQueue()
                }
            }
        }
    }

    // ---- the confirm strip -------------------------------------------------
    //
    // A tap on Download in the volume view queues up to ten chapters and a few
    // hundred megabytes, so PLAN §6 M3's "say what you are doing" means asking
    // first. One step, and the question itself is the backend's sentence, not
    // this file's. A single chapter is never asked about — the backend does not
    // send the question — so this strip belongs to the volume view in practice,
    // and confirming from it confirms a volume.
    //
    // A selection of rows asks here too, for the same reason and through the
    // same strip: "queue" is the third thing confirmingKind can be.
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

        // The way back out of the queue question. It closes the question and
        // leaves the selection exactly as it was, because "not yet" after
        // reading how many rows it is usually means "let me take one off".
        Rectangle {
            id: cancelQueueButton
            objectName: "cancelQueueButton"
            visible: screen.confirmingKind === "queue"
            anchors { right: confirmButton.left; rightMargin: Style.gap; verticalCenter: parent.verticalCenter }
            width: 160
            height: Style.buttonHeight
            color: cancelQueueArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Not yet"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: cancelQueueArea
                objectName: "cancelQueueArea"
                anchors.fill: parent
                enabled: cancelQueueButton.visible
                onClicked: screen.closeConfirm()
            }
        }

        Rectangle {
            id: confirmButton
            objectName: "confirmDownloadButton"
            visible: screen.confirmingKind !== "delete"
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            width: 220
            height: Style.buttonHeight
            color: confirmArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: screen.confirmingKind === "queue" ? "Download them" : "Download all"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: confirmArea
                anchors.fill: parent
                onClicked: {
                    var id = screen.confirmingId
                    var volume = screen.showingVolumes
                    var queueing = screen.confirmingKind === "queue"
                    var ids = screen.selectedIds

                    // Both doors close before anything is sent: the question is
                    // answered, and the selection it was about has been spent.
                    screen.closeConfirm()
                    if (queueing) {
                        screen.leaveSelection()
                        screen.queueConfirmed(ids, volume)
                        return
                    }
                    if (volume)
                        screen.volumeDownloadConfirmed(id)
                    else
                        screen.downloadConfirmed(id)
                }
            }
        }

        // The delete question's answers. Two buttons rather than one, because
        // this is the destructive question on the screen and "not this one"
        // must be as easy to tap as the thing it is protecting. The way out is
        // the wider of the two and sits under the thumb that just tapped
        // Delete.
        Row {
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            spacing: Style.gap
            visible: screen.confirmingKind === "delete"

            Rectangle {
                id: keepButton
                objectName: "keepDownloadButton"
                width: 160
                height: Style.buttonHeight
                color: keepArea.pressed ? Style.pressed : Style.paper
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
                    id: keepArea
                    objectName: "keepArea"
                    anchors.fill: parent
                    onClicked: screen.closeConfirm()
                }
            }

            Rectangle {
                id: confirmDeleteButton
                objectName: "confirmDeleteButton"
                width: 200
                height: Style.buttonHeight
                color: confirmDeleteArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.rule
                radius: 6

                Text {
                    anchors.centerIn: parent
                    // What the button does, in the words the question next to
                    // it uses. It stopped being a move to the Trash when the
                    // Trash started being emptied behind it (PLAN §12.4): the
                    // document is destroyed, and a label promising somewhere to
                    // recover it from would be the one lie on the screen.
                    text: "Delete for good"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: confirmDeleteArea
                    objectName: "confirmDeleteArea"
                    anchors.fill: parent
                    onClicked: {
                        var uuid = screen.confirmingId
                        screen.closeConfirm()
                        screen.deleteConfirmed(uuid)
                    }
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
