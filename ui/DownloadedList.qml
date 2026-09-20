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
//
// Since PLAN §7.1 type 75 it also has a grid of covers, which is what the
// screen is stored in by default: recognising a book you already own is what a
// cover is for, and this screen is entirely books you already own. The rows
// carry the same cover, small, at their leading edge — the cache entry is
// already downscaled, so the thumbnail costs a smaller decode rather than a
// second fetch, and the layout switch stops deciding whether there is a
// picture at all.
//
// Delete has a full-size button on the row, where there is room for one beside
// the title, and nothing on the tile, where it would sit on top of a picture.
// It is not missing from the grid for all that: a long press opens the same
// menu in both layouts (ui/ContextMenu.qml), and Delete is on it. That is the
// rule the menu exists for — the layout switch decides how the screen looks
// and never what it can do.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging
import "Views.js" as Views
// Whether a row is a book — and that an absent kind is not one — is decided in
// exactly one place, and it is not this file.
import "Kinds.js" as Kinds

Item {
    id: screen

    property alias model: list.model

    // The backend's line for a library with nothing in it. Empty means there is
    // something to show.
    property string emptyNote: ""

    // Which layout is showing, from the store by way of Main.qml. Grid until a
    // status says otherwise, so the screen draws on the way in rather than
    // waiting for the first Pong (Views.js).
    property string view: Views.GRID
    readonly property bool listing: screen.view === Views.LIST

    property int page: 1
    // From the model rather than the view: a view updates its own count during
    // a layout pass, so a page count taken from it lags by a frame.
    readonly property int rowCount: screen.model ? screen.model.count : 0
    // A tile is much taller than a row, so the two layouts hold different
    // numbers of series. The whole list is already in hand — this screen pages
    // a model, not a source — so switching is a re-slice and never a fetch.
    readonly property int pageSize: screen.listing
        ? Paging.rowsPerPage(viewport.height, Style.coverRowHeight) : tiles.pageSize
    readonly property int totalPages: Paging.pageCount(screen.rowCount, screen.pageSize)

    onTotalPagesChanged: screen.page = Paging.clampPage(screen.page, screen.totalPages)

    // Covers are fetched for the page on screen and no further, which is the
    // same rule the search grid keeps and the same reason: PLAN §6 M3 says
    // never hold more than a screenful, and the politeness limiter serialises
    // requests (PLAN §7.4), so a cover fetched for page four stands in front of
    // one the user is looking at now.
    //
    // Each entry names its own source: unlike a search, this screen draws
    // series from every source at once, and only the source knows how to fetch
    // its covers. It stays **one message** all the same — Main.qml's
    // requestCoversBySource records what splitting it per source cost.
    signal coversRequested(var covers)

    // Both layouts ask. The rows used to ask for nothing, which was right while
    // a row was two lines of text; the row draws a thumbnail now, and it is the
    // same cache entry the tile draws — already downscaled to 300x450
    // (backend/covers/covers.go) — so a row costs a smaller decode rather than
    // a second fetch. The page arithmetic below is already layout-aware, so
    // what a list page asks for is the rows a list page holds.
    function requestVisibleCovers() {
        var wanted = []
        var from = Paging.firstIndex(screen.page, screen.pageSize)
        for (var i = from; i < from + screen.pageSize && i < screen.rowCount; ++i) {
            var row = screen.model.get(i)
            if (row.coverUrl && row.coverUrl.length > 0 && row.coverPath.length === 0)
                wanted.push({"sourceId": row.sourceId, "seriesId": row.seriesId,
                             "url": row.coverUrl})
        }
        if (wanted.length > 0)
            screen.coversRequested(wanted)
    }

    onPageChanged: screen.requestVisibleCovers()
    onViewChanged: screen.requestVisibleCovers()

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

    // Reading the newest download without opening the series first, and the
    // watch that was otherwise only reachable from inside it.
    signal readRequested(string documentUuid)
    signal watchRequested(string sourceId, string seriesId, string title)
    signal unwatchRequested(string sourceId, string seriesId)

    // ---- the long-press menu ------------------------------------------------
    //
    // The menu is what brings Delete to the grid: before it, the layout switch
    // silently decided whether a series could be deleted at all.
    //
    // The row is held by its ids, never by its index — this screen re-slices
    // its model on a page turn and refills it whenever the screen is opened.
    property string menuSourceId: ""
    property string menuSourceName: ""
    property string menuSeriesId: ""
    property string menuTitle: ""
    property string menuLatestUuid: ""

    // subtitleOf is the row's second line: what it is, which source it came
    // from, and what is already on the tablet.
    //
    // **The mark is here because this screen spans every source.** A single
    // source is one kind and names itself, so ui/SeriesGrid.qml needs nothing;
    // here a book and a comic sit in the same list with a cover, a title and an
    // author line that arrive through identical fields. It is the same argument
    // the combined search's rows are marked under, and the same word — in
    // words, never in colour alone (ui/Kinds.js).
    //
    // It reads the `badge` role rather than re-deriving the mark from the kind:
    // that role is what the tile already wears in its badge corner, composed
    // once in Main.qml, and a second derivation is a second place for "absent
    // means manga" to be decided. An absent role is an absent property rather
    // than an empty string (CoverGrid's roleOf records the same trap).
    function subtitleOf(row) {
        var mark = row.badge === undefined || row.badge === null ? "" : String(row.badge)
        var line = row.sourceName + " · " + row.detail
        if (mark.length > 0)
            line = mark + " · " + line
        // The backend's note about a source that has been removed, kept where
        // it was: at the end, after everything that is still true.
        return row.openable ? line : line + " — " + row.note
    }

    // readable says whether the row names a document to open. A row carries the
    // newest download of its series; a library that predates the record, or a
    // series whose last download has gone, names none.
    function readable(row) {
        return row.latestUuid !== undefined && String(row.latestUuid).length > 0
    }

    function itemsFor(row) {
        var items = []

        // A row whose source has been removed gets a **shorter** menu, not no
        // menu. Everything here that needs the source is left out, because
        // offering something that cannot work is worse than not offering it:
        // there is nowhere to browse, nothing to watch for new chapters, and
        // no site to fetch them from.
        //
        // What survives is what never needed the source. Deleting is the
        // reason these rows are still listed at all — the downloads are on the
        // tablet and are the user's to be rid of — and it has to be here
        // rather than only on the row, because in the grid layout there *is*
        // no row and no button on it: without this, an orphaned series could
        // be deleted in one layout and not at all in the other, which is the
        // exact gap the menu exists to close. Reading is the same argument:
        // OpenInReader takes a document uuid and asks the source nothing, so a
        // file already on the tablet stays readable whatever became of the
        // site it came from.
        if (row.openable !== true) {
            if (screen.readable(row))
                items.push({"action": "read", "label": "Read latest", "enabled": true})
            items.push({"action": "delete",
                        "label": "Delete everything from this series", "enabled": true})
            return items
        }

        items.push({"action": "open", "label": "Open", "enabled": true})
        // **Absent, not greyed out, when there is nothing to open.** An item
        // that cannot ever be used on this row is not an item.
        if (screen.readable(row))
            items.push({"action": "read", "label": "Read latest", "enabled": true})
        // **Never offered on a book**, in either layout, and absent rather than
        // greyed out — the same treatment the series screen's Watch button
        // gets, and it has to be the same or the two screens disagree about
        // what a book is. A user who found a book watchable here and not there
        // would reasonably conclude one of the two is broken.
        //
        // The reason is not tidiness. Watching is the new-chapters machinery
        // from end to end (PLAN §12.2): every check is a real round trip per
        // watched series, and a book's releases are the files one finished book
        // already exists as. Pointing a check at one buys a request to a slow
        // Shelfmark instance that can only ever answer "nothing new", forever.
        //
        // A book that was already watched before this — from an older Quire —
        // is still unwatchable, from the watched screen itself (ui/WatchList.qml),
        // which is where a watch record is managed.
        if (!Kinds.isBook(row))
            items.push(row.watched === true
                       ? {"action": "unwatch", "label": "Stop watching", "enabled": true}
                       : {"action": "watch", "label": "Watch", "enabled": true})
        items.push({"action": "delete",
                    "label": "Delete everything from this series", "enabled": true})
        return items
    }

    function openMenu(index, x, y) {
        if (index < 0 || index >= screen.rowCount)
            return
        var row = screen.model.get(index)
        screen.menuSourceId = row.sourceId
        screen.menuSourceName = row.sourceName
        screen.menuSeriesId = row.seriesId
        screen.menuTitle = row.title
        screen.menuLatestUuid = row.latestUuid ? String(row.latestUuid) : ""
        menu.show(screen.itemsFor(row), x, y)
    }

    function chose(action) {
        switch (action) {
        case "open":
            screen.openRequested(screen.menuSourceId, screen.menuSourceName,
                                 screen.menuSeriesId, screen.menuTitle)
            return
        case "read":
            screen.readRequested(screen.menuLatestUuid)
            return
        case "watch":
            screen.watchRequested(screen.menuSourceId, screen.menuSeriesId, screen.menuTitle)
            return
        case "unwatch":
            screen.unwatchRequested(screen.menuSourceId, screen.menuSeriesId)
            return
        case "delete":
            // Into the question the row's own button asks, not into the
            // delete: the hold made the choice deliberate, it did not make it
            // safe, and a second confirmation on top of the backend's would
            // teach people to tap through both.
            screen.askToDelete(screen.menuSourceId, screen.menuSeriesId)
            return
        }
    }

    // ---- the list ----------------------------------------------------------

    Item {
        id: viewport
        anchors {
            top: parent.top
            left: parent.left; right: parent.right
            bottom: pagerBar.top
        }

        CoverGrid {
            id: tiles
            objectName: "downloadedTiles"
            anchors {
                fill: parent
                leftMargin: Style.margin; rightMargin: Style.margin
            }
            visible: !screen.listing
            model: screen.model
            // A tile has no subtitle line, so the mark a row carries in words
            // goes in the badge corner — the slot Watching's "3 new chapters"
            // already uses, already drawn and already legible on this panel. A
            // comic's row carries no badge at all: a mark on every tile is a
            // mark nobody reads.
            badges: true
            firstIndex: Paging.firstIndex(screen.page, screen.pageSize)
            onTapped: {
                var row = screen.model.get(index)
                // Inert for the same reason the row is: there is no source left
                // to browse. The guard is here rather than in the tile because
                // "can this be opened?" is the backend's answer about this
                // screen's rows, not something a grid of pictures should know.
                if (!row.openable)
                    return
                screen.openRequested(row.sourceId, row.sourceName, row.seriesId, row.title)
            }
            onHeld: {
                var at = tiles.mapToItem(screen, x, y)
                screen.openMenu(index, at.x, at.y)
            }
        }

        ListView {
            id: list
            objectName: "downloadedRows"
            anchors { top: parent.top; left: parent.left; right: parent.right }
            // Whole rows only; the remainder is blank rather than a half row.
            height: Paging.rowsPerPage(viewport.height, Style.coverRowHeight) * Style.coverRowHeight
            clip: true
            visible: screen.listing

            interactive: false
            cacheBuffer: 0
            contentY: Paging.firstIndex(screen.page, screen.pageSize) * Style.coverRowHeight

            delegate: Item {
                width: list.width
                height: Style.coverRowHeight

                // The press acknowledgement, behind the row's own text. Either
                // area can be the one being held; only ever one of them is
                // live, so this reads whichever it is.
                Rectangle {
                    objectName: "rowPressFeedback"
                    anchors.fill: parent
                    color: rowArea.feedback || orphanArea.feedback
                           ? Style.pressed : Style.paper
                }

                // The hold on a row whose source has been removed.
                //
                // A second area rather than one that is always enabled,
                // because the tap target must stay *disabled* on these rows —
                // there is nothing to open, and a tap that quietly goes
                // nowhere is worse than a row that never offered. `enabled` is
                // what makes that true for real input, so exactly one of the
                // two areas is live on any row and the other is not in the
                // way. Both end in the same openMenu; what differs is only
                // which lines the menu comes back with (itemsFor).
                HoldArea {
                    id: orphanArea
                    objectName: "downloadedOrphanArea"
                    anchors.fill: parent
                    enabled: !model.openable
                    onHeld: {
                        var at = orphanArea.mapToItem(screen,
                                                      orphanArea.pressX, orphanArea.pressY)
                        screen.openMenu(index, at.x, at.y)
                    }
                }

                // The cover, at the leading edge and sized from the row. It is
                // smaller than the row on purpose: this screen works out how
                // many series fit from Style.coverRowHeight, so a thumbnail that
                // set the height would be fewer downloads per page than
                // before. It takes no input of its own, so the row's two hold
                // areas still cover it and a tap on the picture is a tap on
                // the row.
                CoverArt {
                    id: thumb
                    objectName: "rowCover"
                    anchors {
                        left: parent.left; leftMargin: Style.margin
                        verticalCenter: parent.verticalCenter
                    }
                    width: Style.thumbWidth
                    height: Style.thumbHeight
                    // Most of a library predates covers being kept at all, so
                    // the placeholder is the common case here rather than the
                    // exception — and an absent role is an absent property,
                    // not an empty string (CoverGrid's roleOf).
                    coverPath: model.coverPath === undefined ? "" : String(model.coverPath)
                    caption: model.title
                    // Two lines at this width. The title is beside it in full;
                    // this is what stops a coverless series being a gap.
                    lines: 2
                    padding: Style.gap / 4
                }

                Column {
                    anchors {
                        left: thumb.right; leftMargin: Style.gap
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
                        objectName: "downloadedRowDetail"
                        width: parent.width
                        wrapMode: Text.WordWrap
                        maximumLineCount: 2
                        elide: Text.ElideRight
                        text: screen.subtitleOf(model)
                        font.pointSize: Style.smallSize
                        color: Style.muted
                    }
                }

                // The row body still opens the series — asked for explicitly,
                // "clicking on the rest of the row still should link to that
                // manga to delete individual chapters" — so the tap target
                // stops where the delete button starts rather than covering it.
                HoldArea {
                    id: rowArea
                    objectName: "downloadedRowArea"
                    anchors {
                        top: parent.top; bottom: parent.bottom
                        left: parent.left; right: deleteButton.left
                    }
                    // Inert rather than failing: there is no source left to
                    // browse, and a tap that goes nowhere quietly is worse than
                    // a row that never offered. The hold goes with it — see
                    // openMenu, where the grid declines the same rows, so that
                    // the two layouts offer the same nothing.
                    enabled: model.openable
                    onTapped: screen.openRequested(model.sourceId, model.sourceName,
                                                   model.seriesId, model.title)
                    onHeld: {
                        var at = rowArea.mapToItem(screen, rowArea.pressX, rowArea.pressY)
                        screen.openMenu(index, at.x, at.y)
                    }
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

    // Last, so it is over the rows, the tiles, the confirm strip and the pager.
    ContextMenu {
        id: menu
        objectName: "downloadedMenu"
        anchors.fill: parent
        onChosen: screen.chose(action)
    }
}
