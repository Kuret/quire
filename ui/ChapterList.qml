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

// # A book is the same screen with different words
//
// A Shelfmark source publishes books, and what fills this list for one is not
// chapters but **releases** — the files that book is available as, of which the
// reader picks one. The backend titles each of them for display ("EPUB · 0.4MB
// · Direct Download · …"), so none of that wording is here.
//
// What *is* here is everything around the list, and for a book most of it was
// a lie: a volume view, a watch button that promises to announce new chapters,
// "No chapters listed.", a date line for a thing with no date.
//
// **It is conditional wording rather than a second screen**, and deliberately:
// strip the words out and a book behaves identically to a series of one volume
// — one paged list, one button a row, the same download states, the same
// delete question, the same selection mode, the same pager. A ReleaseList.qml
// would be a copy of ~700 lines of paging and selection bookkeeping in order to
// change four strings and hide two controls, and the copy is where the next fix
// to the selection logic would fail to land. The cases where a book genuinely
// differs are all *absences*, which this screen already knows how to draw: it
// has collapsed the volume switch to nothing since volumes existed.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: screen

    property alias model: list.model
    property string seriesTitle: ""
    property string synopsis: ""
    property bool busy: false

    // What this series is: "manga" — every source that existed before books —
    // or "book". It is set from the row that opened the screen, because that is
    // where the backend says it (ui/Kinds.js); the detail reply carries no kind
    // of its own and inventing one on the wire would be the frontend adding a
    // field to the protocol.
    //
    // The default is what an absent field means, so a screen nobody told is the
    // world as it was.
    property string kind: "manga"
    readonly property bool isBook: screen.kind === "book"

    // Whether this series' source is private (SeriesDetailResult's top-level
    // `private`). Never offered "Send to library" from the reader — that
    // check is made here rather than trusted to the reader alone, because
    // this is where the flag arrives.
    property bool isPrivate: false

    // The volume view (PLAN §6 M4, revised 2026-09-16). Chapters are the
    // default and are always here; volumes are a second view of the same
    // series, offered only when the backend sent rows for one.
    //
    // Whether volumes exist is not decided here. The backend sends the rows or
    // sends none, because the rule is about the data — the source's own labels,
    // and whether the reading order is known at all — and an empty tab is a
    // worse answer than no tab.
    property alias volumeModel: volumeList.model

    // A book has exactly one series and no volumes, so the switch is never
    // offered for one — whatever happens to be in the model. Written as a
    // refusal here rather than trusted to arrive empty: "Volumes" on a book is
    // the screen claiming a reading order that does not exist, and the cost of
    // being sure is one term.
    readonly property bool hasVolumes: !screen.isBook
                                       && (screen.volumeModel ? screen.volumeModel.count > 0 : false)

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

    // Try (milestone 1): read a chapter without downloading it. Carries the
    // title along with the id, the same as sourceSwitchRequested does,
    // because the reader screen needs something to put in its own header and
    // the row already has it — asking the backend to say it again would be a
    // second answer to a question already answered.
    //
    // Never offered for a book (screen.isBook, below): a Shelfmark release
    // has no page images at all, so there is nothing here to preview.
    signal tryRequested(string chapterId, string title)

    // Saved in Quire: reading and deleting a chapter kept in Quire's own
    // storage rather than the library. A saved row shows [Delete][Read]
    // whether or not the same chapter is also in the library (see
    // buttonLabel/tapped) — Read opens Quire's own reader (OpenSaved), never
    // xochitl's, and Delete asks the same confirm-then-done question as a
    // library delete but of the saved copy alone.
    signal readSavedRequested(string chapterId)
    signal deleteSavedRequested(string chapterId)
    signal deleteSavedConfirmed(string chapterId)

    // Deleting a whole saved volume in one go (round 2): the same
    // confirm-then-done shape, naming every chapter of the volume rather than
    // one. volumeLabel is the short "Vol 2" form, for the backend's question
    // and done sentence.
    signal deleteVolumeRequested(string volumeLabel, var chapterIds)
    signal deleteVolumeConfirmed(string volumeLabel, var chapterIds)

    // Deleting a download (PLAN §12.4). Two signals because it is two steps and
    // the question in between is the backend's: deleteRequested asks for it,
    // deleteConfirmed is the answer. An accidental tap can only ever reach the
    // first one.
    signal deleteRequested(string documentUuid)
    signal deleteConfirmed(string documentUuid)

    // Queueing a selection (PLAN §12.1). One signal, because there is one step:
    // the footer queues what was picked, immediately. Picking the rows is
    // itself the deliberate act, and a question behind a deliberate act is a
    // tap people learn to make without reading it.
    signal queueConfirmed(var chapterIds, bool volumes)

    signal watchRequested()
    signal unwatchRequested()

    // ---- the sources this series was found in ------------------------------
    //
    // Only ever filled for a series opened from the combined search
    // (ui/SearchAll.qml), where a group can stand for the same book on three
    // sites. A series reached the ordinary way — browsing one source, a watch
    // row, a download — has no alternatives at all, and then this is empty and
    // the strip below is not drawn.
    //
    // **The chips do not merge anything.** Watch records, downloads and the
    // downloaded overview are all keyed by (sourceId, seriesId), and tapping
    // another chip switches *which pair this screen is acting on* — the
    // chapters are re-fetched for it, and everything the screen then offers
    // belongs to it. Showing the pair being acted on is the whole reason the
    // chips are here rather than a silent fallback to another source.
    //
    // Each entry is {sourceId, sourceName, seriesId}, in the reply's order,
    // which is the user's configured source order.
    property var sources: []
    property string currentSourceId: ""

    // One source is not a choice. A single chip would be a control that can
    // only tell you what you already know from the header.
    readonly property bool hasAlternatives: screen.sources !== undefined
                                            && screen.sources !== null
                                            && screen.sources.length > 1

    signal sourceSwitchRequested(string sourceId, string sourceName, string seriesId)

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

    // The whole-volume delete's own pair, alongside confirmingId: a volume
    // delete names a label and a list of chapter ids rather than one id, and
    // both have to survive from the request to the confirmed tap (see
    // askToDeleteVolume / the confirm button below).
    property string confirmingVolumeLabel: ""
    property var confirmingChapterIds: []

    // Which question the strip is asking: "download" is the volume-download
    // confirmation the strip was built for, "delete" is PLAN §12.4's. One
    // property rather than a strip each, because there is one place at the foot
    // of the list for a question and two strips fighting over it is two ways to
    // answer the one you were not looking at.
    //
    // A selection is not one of them any more: it queues without asking.
    property string confirmingKind: "download"

    // Both delete questions share the strip's Keep / Delete-for-good shape;
    // this is the one place that says so, rather than every visible binding
    // spelling out "delete or deleteSaved" for itself.
    readonly property bool confirmingDelete: screen.confirmingKind === "delete"
                                             || screen.confirmingKind === "deleteSaved"
                                             || screen.confirmingKind === "deleteSavedVolume"

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
        // A book's rows are **alternatives**: three releases of one book are
        // three copies of the same thing in three formats, and queueing them is
        // never what anyone meant. For a manga the mode is the whole point —
        // thirty chapters in one tap — so this is the one place the two
        // genuinely want different behaviour rather than different words.
        //
        // Refused here as well as at the door (enterSelection) because the two
        // stop different mistakes: that stops the mode being entered, this
        // means no row is pickable even if it somehow were.
        if (screen.isBook)
            return false
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
        // Never for a book — see canSelect. A bulk action over a set of
        // alternatives invites exactly the mistake it makes easy, and each
        // mistaken copy is minutes of a slow queue.
        if (screen.isBook)
            return
        screen.closeConfirm()
        screen.selecting = true
        screen.clearSelection()
    }

    function leaveSelection() {
        screen.selecting = false
        screen.clearSelection()
    }

    // queueSelection queues what was picked and leaves the mode.
    //
    // The ids are taken before the selection is cleared: leaveSelection assigns
    // a fresh empty array rather than emptying this one, so what was picked
    // travels on intact.
    //
    // What the queue could not take is the backend's to say, once, when it has
    // tried — see MessageQueueResult. Nothing is guessed at here.
    function queueSelection() {
        if (screen.selectedCount === 0)
            return
        var ids = screen.selectedIds
        var volumes = screen.showingVolumes
        screen.leaveSelection()
        screen.queueConfirmed(ids, volumes)
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

    // askToDeleteSaved is askToDelete's counterpart for a chapter saved in
    // Quire: the strip carries the chapter id, since a saved chapter has no
    // document uuid, and "deleteSaved" is its own confirmingKind so the
    // answer comes back as deleteSavedConfirmed rather than deleteConfirmed.
    function askToDeleteSaved(chapterId) {
        if (!chapterId)
            return
        screen.confirmingId = ""
        screen.confirmingMessage = ""
        screen.confirmingKind = "deleteSaved"
        screen.deleteSavedRequested(chapterId)
    }

    // askToDeleteVolume is askToDeleteSaved's whole-volume counterpart (round
    // 2): chapterIds names every chapter of the volume, and the backend
    // deletes whichever of them are actually saved.
    function askToDeleteVolume(volumeLabel, chapterIds) {
        if (!chapterIds || chapterIds.length === 0)
            return
        screen.confirmingId = ""
        screen.confirmingMessage = ""
        screen.confirmingKind = "deleteSavedVolume"
        screen.confirmingVolumeLabel = volumeLabel
        screen.confirmingChapterIds = chapterIds
        screen.deleteVolumeRequested(volumeLabel, chapterIds)
    }

    // closeConfirm puts the strip away without answering it.
    function closeConfirm() {
        screen.confirmingId = ""
        screen.confirmingMessage = ""
        screen.confirmingKind = "download"
        screen.confirmingVolumeLabel = ""
        screen.confirmingChapterIds = []
    }

    // The phases are backend/service's; the words each maps to are the view's,
    // and they are the only wording this file invents. Every sentence shown to
    // the user is composed in the backend (PLAN §2).
    function buttonLabel(state, documentUuid, saved) {
        // A saved chapter reads "Read" whether or not the same chapter also
        // has a documentUuid — see tapped() for why saved wins the row.
        if (saved)
            return "Read"
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

    // inFlight is true while a download is actually running for this row: the
    // states the backend leaves a row in between the tap and the file. It is
    // written as the same exclusion list buttonLabel() uses rather than a list
    // of live phases, so that a phase the backend adds later is in flight here
    // for the same reason its button already says "Stop" — the accent mark has
    // to sit on exactly the rows that offer the way out, and two lists would
    // drift.
    //
    // A finished or failed row is *not* in flight. "Failed" is a whole
    // plain-language sentence and it is not progress; marking it would make
    // the accent mean two different things on the same line.
    function inFlight(state, documentUuid) {
        if (documentUuid)
            return false
        switch (state) {
        case "":
        case "confirm":
        case "done":
        case "failed":
        case "cancelled":
            return false
        default:
            return true
        }
    }

    // canTap is true when the button does something. Every state now does.
    function canTap(state, documentUuid) {
        return true
    }

    function tapped(chapterId, state, documentUuid, saved) {
        // Saved wins the row even when the chapter is also in the library
        // (PLAN's saved-in-Quire design: independent copies, one row) —
        // Read opens Quire's own reader, never xochitl's.
        if (saved) {
            screen.readSavedRequested(chapterId)
            return
        }
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
    function volumeTapped(chapterId, state, documentUuid, saved) {
        if (saved) {
            screen.readSavedRequested(chapterId)
            return
        }
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
                right: selectButton.visible ? selectButton.left
                                            : (watchButton.visible ? watchButton.left : parent.right)
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
                // Takes the watch button's place when there is no watch button
                // — a book has none — rather than leaving a hole where it was.
                right: watchButton.visible ? watchButton.left : parent.right
                rightMargin: watchButton.visible ? Style.gap : Style.margin
                top: parent.top; topMargin: Style.margin
            }
            width: 220
            height: Style.buttonHeight
            // Absent for a book, not inert: the mode it opens has nothing to
            // offer a list of alternatives, and a control that is there and
            // does nothing is worse than one that never was.
            visible: !screen.selecting && screen.rowCount > 0 && !screen.isBook
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
            // Not offered for a book. Watching is the new-chapters machinery
            // (PLAN §12.2) from end to end: the backend seeds a watch from the
            // chapter list it last served and every check answers "how many new
            // chapters". A book's releases are the files one finished book
            // already exists as — nothing is ever added to them — so the button
            // would promise an announcement that cannot arrive, and the badge it
            // leads to would be counting chapters on a thing with none.
            visible: !screen.isBook
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
                objectName: "watchArea"
                anchors.fill: parent
                // Dead as well as invisible. An invisible item takes no touches
                // in any case; saying so here is what makes "a book cannot be
                // watched" a property of the control rather than of where it
                // happens to be drawn.
                enabled: watchButton.visible
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

    // ---- the source switcher -----------------------------------------------
    //
    // A row of chips, one per source the combined search found this series in,
    // with the one being read marked. It collapses to zero height when there
    // is nothing to choose between, so a series reached by browsing gets
    // exactly the screen it had before — including the same number of rows to
    // a page, since the viewport is what decides that. The same shape as the
    // Chapters/Volumes switch below, and for the same reason.
    //
    // Filled rather than outlined for the current one: on e-ink a border alone
    // is too quiet to answer "which am I reading?" at a glance, which is the
    // one question this strip exists to keep answered.
    Item {
        id: sourceStrip
        objectName: "sourceStrip"
        anchors { top: synopsisBand.bottom; left: parent.left; right: parent.right }
        visible: screen.hasAlternatives
        height: visible ? Style.buttonHeight + Style.margin : 0

        Row {
            anchors {
                left: parent.left; leftMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            spacing: Style.gap

            Repeater {
                // The list itself, not a count: each chip names its own source
                // and carries its own seriesId, because the series id differs
                // per source and is half of the pair every message on this
                // screen is about.
                model: screen.sources

                Rectangle {
                    objectName: "sourceChip-" + modelData.sourceId
                    // Sized to the name it holds, with a floor: a source
                    // called "Kai" must still be a finger-sized target.
                    width: Math.max(180, chipLabel.width + Style.margin)
                    height: Style.buttonHeight
                    readonly property bool current: modelData.sourceId === screen.currentSourceId
                    // The fill is still what marks the current chip; it is the
                    // accent now rather than the grey. That was the wrong way
                    // round before — the accent was on the 2px border, on the
                    // grounds that ink on a deep orange read worse than ink on
                    // grey. It did, at the *old* accent: black on #C2410C is
                    // 4.06:1. The accent moved to a value black clears
                    // comfortably on (5.90:1, ui/Style.js), which is what lets
                    // the colour sit where it belongs — on an area rather than
                    // on a hairline the panel renders badly.
                    color: chipArea.pressed ? Style.pressed
                                            : (current ? Style.accent : Style.paper)
                    border.width: 2
                    border.color: Style.ink
                    radius: 6

                    Text {
                        id: chipLabel
                        objectName: "sourceChipLabel-" + modelData.sourceId
                        anchors.centerIn: parent
                        text: modelData.sourceName
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: chipArea
                        objectName: "sourceChipArea-" + modelData.sourceId
                        anchors.fill: parent
                        // The source already showing is inert. Re-requesting
                        // it would throw away a list that is already right and
                        // put a fetch in front of the user for nothing —
                        // and on a slow panel the blank while it lands reads
                        // as the tap having broken something.
                        enabled: !parent.current
                        onClicked: screen.sourceSwitchRequested(
                            modelData.sourceId, modelData.sourceName, modelData.seriesId)
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

    // ---- what the list holds, for a book -----------------------------------
    //
    // A book's rows are not chapters and not episodes: they are the files this
    // one book is available as, and the reader is picking a format and a place
    // to get it from rather than working through a list in order. Nothing else
    // on the screen says so — the rows are titled by the backend, and a list of
    // rows with Download beside each reads as a list of parts.
    //
    // One line, in the strip the view switch would occupy, so a book's list
    // starts exactly where a manga's does and a page holds the same number of
    // rows. It is a label on the list, the same class of wording as "Select" or
    // "3 selected"; the sentences the backend composes are untouched (PLAN §2).
    Item {
        id: releaseStrip
        objectName: "releaseStrip"
        anchors { top: sourceStrip.bottom; left: parent.left; right: parent.right }
        visible: screen.isBook
        height: visible ? Style.buttonHeight + Style.margin : 0

        Text {
            objectName: "releaseNote"
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            elide: Text.ElideRight
            text: "Releases · pick the file to download."
            font.pointSize: Style.smallSize
            color: Style.ink
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
        anchors { top: releaseStrip.bottom; left: parent.left; right: parent.right }
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
                            right: deleteButton.visible ? deleteButton.left
                                   : (tryButton.visible ? tryButton.left : downloadButton.left)
                            rightMargin: Style.gap
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: 4

                        Text {
                            objectName: "chapterTitle"
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
                        // A download in flight is the one thing on this screen
                        // the user is waiting on, so it is the one line that
                        // is marked. The mark sits *beside* the sentence and
                        // the sentence stays grey: a solid square on paper
                        // next to black-and-grey words, neither of them a
                        // coloured letterform (ui/Style.js, ui/AccentMark.qml).
                        // The line used to be drawn in the accent itself and
                        // that is what read muddy on the device.
                        //
                        // The sentence is still the backend's and still says
                        // what is happening in words, and the button beside it
                        // still says "Stop": the mark is what finds the row on
                        // a page of thirty, not what explains it.
                        Row {
                            id: subtitleLine
                            width: parent.width
                            spacing: Style.gap / 2

                            AccentMark {
                                objectName: "chapterMark"
                                // On the first line of the sentence rather
                                // than centred on both: a two-line failure
                                // would otherwise drag the mark into the gap
                                // between the rows.
                                y: 2
                                visible: screen.inFlight(model.downloadState,
                                                         model.documentUuid)
                            }

                            Text {
                                objectName: "chapterSubtitle"
                                width: subtitleLine.width
                                       - (screen.inFlight(model.downloadState, model.documentUuid)
                                          ? Style.accentMark + subtitleLine.spacing : 0)
                                wrapMode: Text.WordWrap
                                maximumLineCount: 2
                                elide: Text.ElideRight
                                // A release has no publication date and no
                                // scanlator: the whole of what it is — format,
                                // size, where it comes from — is in the title
                                // the backend composed. "Date unknown" under it
                                // would be the screen inventing a fact about a
                                // thing that has none, so a book's row says
                                // nothing here until a download does.
                                text: model.downloadMessage.length > 0 && model.downloadState !== "confirm"
                                      ? model.downloadMessage
                                      : (screen.isBook
                                         ? ""
                                         : (model.published.length > 0 ? model.published : "Date unknown") +
                                           (model.scanlator.length > 0 ? " · " + model.scanlator : ""))
                                font.pointSize: Style.smallSize
                                color: Style.muted
                            }
                        }
                    }

                    // Try (milestone 1): read this chapter without
                    // downloading it. Never for a book — a release has no
                    // page images, so there is nothing here to preview
                    // (theme.FileTheme) — and never once the chapter is
                    // already on the tablet, where "Read" opens the real
                    // thing rather than a preview of it.
                    Rectangle {
                        id: tryButton
                        // Suffixed with the chapter id — see downloadButton's
                        // comment above for why a static name is not enough
                        // to find *this* row's control.
                        objectName: "tryButton-" + model.chapterId
                        anchors { right: downloadButton.left; rightMargin: Style.gap; verticalCenter: parent.verticalCenter }
                        width: 140
                        height: Style.buttonHeight
                        // A saved chapter offers [Delete][Read] instead —
                        // see the delete/download buttons below.
                        visible: !screen.isBook && !model.documentUuid && !model.saved
                                 && !screen.selecting
                        color: tryArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.rule
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: "Try"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: tryArea
                            objectName: "tryArea-" + model.chapterId
                            anchors.fill: parent
                            enabled: tryButton.visible
                            onClicked: screen.tryRequested(model.chapterId, model.title)
                        }
                    }

                    // Only on a row that has something to delete, and never
                    // instead of Read: the download is the thing the user came
                    // for, and a delete that sits where they expect to tap to
                    // read is a delete they will hit by accident.
                    //
                    // A saved chapter offers it too — saved or in the library,
                    // "has something to delete" is true either way — but it
                    // asks a different question: deleteSaved of the chapter,
                    // never the library document, when both are true (see
                    // tapped()).
                    Rectangle {
                        id: deleteButton
                        objectName: "deleteButton"
                        anchors { right: tryButton.left; rightMargin: Style.gap; verticalCenter: parent.verticalCenter }
                        width: 140
                        height: Style.buttonHeight
                        visible: (model.saved || model.documentUuid) && !screen.selecting ? true : false
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
                            onClicked: {
                                if (model.saved)
                                    screen.askToDeleteSaved(model.chapterId)
                                else
                                    screen.askToDelete(model.documentUuid)
                            }
                        }
                    }

                    // In selection mode the row is the target, not the button
                    // on it: the point of the mode is to pick several rows
                    // without waiting for each one's button to settle. The
                    // button stays on screen so the row still says what state
                    // it is in; it just stops being tappable.
                    Rectangle {
                        id: downloadButton
                        // Suffixed with the chapter id, the same convention
                        // as the source chips (sourceChip-<id>): a ListView
                        // recycles and reorders its delegates, so a test that
                        // wants *this row's* button has to name it rather
                        // than guess at a position.
                        objectName: "downloadButton-" + model.chapterId
                        anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
                        width: 180
                        height: Style.buttonHeight
                        color: downloadArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: screen.buttonLabel(model.downloadState, model.documentUuid, model.saved)
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: downloadArea
                            objectName: "downloadArea-" + model.chapterId
                            anchors.fill: parent
                            enabled: !screen.selecting
                                     && screen.canTap(model.downloadState, model.documentUuid)
                            onClicked: screen.tapped(model.chapterId, model.downloadState,
                                                     model.documentUuid, model.saved)
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
                        // Exactly as a chapter row, through the same predicate
                        // and the same shape: a volume is the longer wait of
                        // the two, so if only one of these were marked it
                        // would be the wrong one.
                        Row {
                            id: volumeSubtitleLine
                            width: parent.width
                            spacing: Style.gap / 2

                            AccentMark {
                                objectName: "volumeMark"
                                y: 2
                                visible: screen.inFlight(model.downloadState,
                                                         model.documentUuid)
                            }

                            Text {
                                objectName: "volumeSubtitle"
                                width: volumeSubtitleLine.width
                                       - (screen.inFlight(model.downloadState, model.documentUuid)
                                          ? Style.accentMark + volumeSubtitleLine.spacing : 0)
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
                    }

                    // Delete for the volume's chapters saved in Quire —
                    // whether the volume is fully or only partly saved. This
                    // is offered *instead of* the library-document delete
                    // below whenever there is anything saved to delete: the
                    // library document (if any) stays reachable through the
                    // chapter rows, and once nothing is saved any more this
                    // row falls back to offering that delete instead (round
                    // 2's whole-volume delete).
                    Rectangle {
                        id: volumeDeleteSavedButton
                        objectName: "volumeDeleteSavedButton"
                        anchors { right: volumeButton.left; rightMargin: Style.gap; verticalCenter: parent.verticalCenter }
                        width: 140
                        height: Style.buttonHeight
                        // savedCount is undefined for a row appended without
                        // the role at all (an older backend, or a harness
                        // fixture) — treated as 0, same as everywhere else
                        // that reads it.
                        visible: (model.savedCount ? model.savedCount : 0) > 0 && !screen.selecting
                        color: volumeDeleteSavedArea.pressed ? Style.pressed : Style.paper
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
                            id: volumeDeleteSavedArea
                            objectName: "volumeDeleteSavedArea"
                            anchors.fill: parent
                            enabled: volumeDeleteSavedButton.visible
                            // "Vol " + the source's own bare label, matching
                            // the same short form backend/service/download.go
                            // already names this volume's files with — never
                            // a sentence built here (PLAN §2), just the label
                            // the backend's own question wraps around.
                            onClicked: screen.askToDeleteVolume(
                                model.label && model.label.length > 0 ? "Vol " + model.label : "",
                                model.chapterIdsJson ? JSON.parse(model.chapterIdsJson) : [])
                        }
                    }

                    // The same affordance as a chapter row, for the same
                    // reason: this row offers Read on a document, so it
                    // offers the way to get rid of it. Offered only when
                    // there is nothing saved to delete instead (above) — a
                    // volume that is both saved and a library document
                    // offers the saved delete, and this one is reachable
                    // again from here once that is no longer true.
                    Rectangle {
                        id: volumeDeleteButton
                        objectName: "volumeDeleteButton"
                        anchors { right: volumeButton.left; rightMargin: Style.gap; verticalCenter: parent.verticalCenter }
                        width: 140
                        height: Style.buttonHeight
                        visible: (model.savedCount ? model.savedCount : 0) === 0
                                 && model.documentUuid && !screen.selecting ? true : false
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
                        objectName: "volumeButton"
                        anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
                        width: 180
                        height: Style.buttonHeight
                        color: volumeArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: screen.buttonLabel(model.downloadState, model.documentUuid, model.saved)
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: volumeArea
                            objectName: "volumeArea"
                            anchors.fill: parent
                            enabled: !screen.selecting
                                     && screen.canTap(model.downloadState, model.documentUuid)
                            onClicked: screen.volumeTapped(model.chapterId, model.downloadState,
                                                           model.documentUuid, model.saved)
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
            objectName: "chapterEmpty"
            anchors.centerIn: parent
            text: screen.busy ? "Fetching…"
                              : (screen.isBook ? "No releases listed." : "No chapters listed.")
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
                    onClicked: screen.queueSelection()
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
    // A *selection* of rows does not ask, here or anywhere: the footer queues
    // what was picked. That leaves the asymmetry of one volume selected queuing
    // straight away while that same volume's own Download button still asks —
    // deliberate, and the user's own call.
    Rectangle {
        id: confirmStrip
        objectName: "confirmStrip"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        height: Style.rowHeight
        color: Style.paper
        // A whole-volume delete carries no single confirmingId — it names a
        // list of chapter ids instead (confirmingChapterIds) — so the strip
        // is also shown for that kind once the backend's question arrives.
        visible: (screen.confirmingId.length > 0 || screen.confirmingKind === "deleteSavedVolume")
                 && screen.confirmingMessage.length > 0

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
            visible: !screen.confirmingDelete
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
                    var volume = screen.showingVolumes
                    screen.closeConfirm()
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
            visible: screen.confirmingDelete

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
                        var id = screen.confirmingId
                        var kind = screen.confirmingKind
                        var volumeLabel = screen.confirmingVolumeLabel
                        var chapterIds = screen.confirmingChapterIds
                        screen.closeConfirm()
                        if (kind === "deleteSavedVolume")
                            screen.deleteVolumeConfirmed(volumeLabel, chapterIds)
                        else if (kind === "deleteSaved")
                            screen.deleteSavedConfirmed(id)
                        else
                            screen.deleteConfirmed(id)
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
