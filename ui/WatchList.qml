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

// Since PLAN §7.1 type 75 the same rows can be drawn as covers. **The badge
// comes with them**: it is the reason this screen exists, so a layout that lost
// it would be a layout that answers the screen's own question with "open every
// one and see". It sits in the corner of the tile rather than at the end of the
// row (ui/CoverGrid.qml).
//
// The rows carry a thumbnail of that same cover at their leading edge, and the
// badge keeps its end of the row: the picture is the same already-downscaled
// cache entry the tile draws, so it costs no second fetch, and a watch row with
// no cover yet shows the titled placeholder rather than a gap.
//
// Stop watching used to be the list layout's alone, behind a long press on a
// row. The long press now opens the menu (ui/ContextMenu.qml) in both layouts,
// and the menu opens that same strip — so the question is unchanged and the
// grid stopped being a worse version of the list.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging
import "Views.js" as Views
import "Watch.js" as WatchJs

Item {
    id: screen

    property alias model: list.model

    // Which layout is showing, from the store by way of Main.qml. Grid until a
    // status says otherwise, so the screen draws before the first Pong lands
    // (Views.js).
    property string view: Views.GRID
    readonly property bool listing: screen.view === Views.LIST

    property int page: 1
    // The count comes from the model, not from the view. A view updates its
    // own count during a layout pass, so a page count derived from it lags the
    // model by a frame — and a frame on this panel is a repaint.
    readonly property int rowCount: screen.model ? screen.model.count : 0
    // A tile is much taller than a row, so the layouts hold different numbers
    // of series. Both page the same model, already in hand.
    readonly property int pageSize: screen.listing
        ? Paging.rowsPerPage(viewport.height, Style.coverRowHeight) : tiles.pageSize
    readonly property int totalPages: Paging.pageCount(screen.rowCount, screen.pageSize)

    // The covers for the page on screen and no further — PLAN §6 M3's memory
    // rule, and PLAN §7.4's serialised fetches, which a cover for a page nobody
    // is looking at would stand in front of. Each entry names its own source:
    // watched series come from every source at once. It is still **one
    // message** — Main.qml's requestCoversBySource records what splitting it
    // per source cost.
    signal coversRequested(var covers)

    // Both layouts ask. The rows used to ask for nothing, which was right while
    // a row was a title and a status line; the row draws a thumbnail now, and
    // it is the same already-downscaled cache entry the tile draws, so a row
    // costs a smaller decode rather than a second fetch. The window below is
    // already layout-aware, so a list page asks for the rows a list page holds.
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

    // Only a watch being dropped, or a source being removed, shortens the list.
    // An update landing mid-round does not, so this does not fire during a
    // check round and the page the user is on stays put.
    onTotalPagesChanged: screen.page = Paging.clampPage(screen.page, screen.totalPages)

    // PLAN §12.2's "phrase" summary, composed in the backend and shown as it
    // arrived. Empty means nothing to report and nothing is shown.
    property string phrase: ""

    signal checkRequested()
    signal unwatchRequested(string sourceId, string seriesId)
    signal openRequested(string sourceId, string sourceName, string seriesId, string title)

    // Continuing to read a watched series (PLAN §12.9) rather than opening
    // its results list — the same two signals ui/DownloadedList.qml raises
    // for the same reason, wired through Main.qml the same way.
    signal readRequested(string documentUuid)
    signal readSavedRequested(string sourceId, string seriesId, string chapterId)

    // The two things the badge is for, reachable without opening the series.
    //
    // Both are answered by the backend rather than here: DownloadNewChapters
    // comes back as the same MessageQueueResult a multi-select download does,
    // and MarkSeen comes back as a watch update and then the whole list, so
    // the row redraws itself from the store. Nothing on this screen guesses at
    // either outcome — a row that cleared its own badge and then failed to
    // save would be a badge the user cannot get back.
    signal downloadNewRequested(string sourceId, string seriesId)
    signal markSeenRequested(string sourceId, string seriesId)

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

    // continueKindOf and tapRow are ui/DownloadedList.qml's own, unchanged:
    // "saved" and "library" continue reading, and "" (nothing to continue
    // yet) falls back to opening the series — what a tap always did, and
    // what the menu's Browse still does on purpose.
    function continueKindOf(row) {
        return row.continueKind === undefined || row.continueKind === null
               ? "" : String(row.continueKind)
    }

    function tapRow(row) {
        switch (screen.continueKindOf(row)) {
        case "saved":
            screen.readSavedRequested(row.sourceId, row.seriesId,
                row.continueChapterId ? String(row.continueChapterId) : "")
            return
        case "library":
            screen.readRequested(row.continueDocumentUuid ? String(row.continueDocumentUuid) : "")
            return
        default:
            screen.openRequested(row.sourceId, row.sourceName, row.seriesId, row.title)
        }
    }

    // openStrip is the existing question about dropping a watch, and the only
    // place a failure's detail sentence is shown. It used to be what the long
    // press did; the long press now opens the menu, and the menu's "Stop
    // watching" opens this. One step more than before, deliberately: the strip
    // is the confirm, and it is unchanged.
    function openStrip(row) {
        if (!row)
            return
        // Already asked about this row: leave it exactly as it is. Reopening
        // would rewrite the same three strings, and on this panel a rewrite
        // that changes nothing is still a repaint.
        if (screen.isStripped(row.sourceId, row.seriesId))
            return
        screen.stripSourceId = row.sourceId
        screen.stripSeriesId = row.seriesId
        screen.stripText = row.detail && row.detail.length > 0 ? row.detail : row.status
    }

    // ---- the long-press menu ------------------------------------------------
    //
    // Held by ids, like the strip and for the same reason: a check round
    // streams rows in while the menu is open.
    property string menuSourceId: ""
    property string menuSourceName: ""
    property string menuSeriesId: ""
    property string menuTitle: ""

    function itemsFor(row) {
        // **Offered and disabled at zero, not hidden.** The two are what this
        // screen is *for*, and a menu whose shape changes with the badge would
        // teach that they only exist sometimes. The backend would refuse both
        // on a series with nothing new; that refusal is a message, and a
        // message is not an answer to "why is this greyed out".
        var hasNew = row.newChapters > 0
        return [
            // "Browse" — opening the series' own results list, what a tap
            // on this row used to do before tapRow started continuing
            // reading instead. Relabelled, not duplicated: it is still the
            // only way to reach the series' full chapter list from here.
            {"action": "open", "label": "Browse", "enabled": true},
            {"action": "download", "label": "Download new chapters", "enabled": hasNew},
            {"action": "seen", "label": "Mark as seen", "enabled": hasNew},
            {"action": "unwatch", "label": "Stop watching", "enabled": true}
        ]
    }

    function openMenu(index, x, y) {
        if (index < 0 || index >= screen.rowCount)
            return
        var row = screen.model.get(index)
        screen.menuSourceId = row.sourceId
        screen.menuSourceName = row.sourceName
        screen.menuSeriesId = row.seriesId
        screen.menuTitle = row.title
        menu.show(screen.itemsFor(row), x, y)
    }

    function chose(action) {
        switch (action) {
        case "open":
            screen.openRequested(screen.menuSourceId, screen.menuSourceName,
                                 screen.menuSeriesId, screen.menuTitle)
            return
        case "download":
            screen.downloadNewRequested(screen.menuSourceId, screen.menuSeriesId)
            return
        case "seen":
            screen.markSeenRequested(screen.menuSourceId, screen.menuSeriesId)
            return
        case "unwatch":
            // Into the strip, which is this screen's existing question, and
            // not into unwatchRequested: the menu opens the confirm, it does
            // not answer it.
            var at = WatchJs.indexOf(screen.model, screen.menuSourceId, screen.menuSeriesId)
            if (at >= 0)
                screen.openStrip(screen.model.get(at))
            return
        }
    }

    // ---- check now ---------------------------------------------------------

    Item {
        id: checkBar
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: Style.rowHeight

        Column {
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: checkButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            spacing: 4

            // The backend's sentence, drawn as it arrived. When there is
            // nothing to report the line is not there at all, rather than
            // saying so at length.
            Text {
                objectName: "watchPhrase"
                width: parent.width
                elide: Text.ElideRight
                text: screen.phrase
                font.pointSize: Style.bodySize
                color: Style.ink
                visible: screen.phrase.length > 0
                height: visible ? implicitHeight : 0
            }

            // PLAN §12.2: never on a timer, never in the background. Saying so
            // is the difference between "Quire is not watching" and "Quire
            // looks when you open it", which is the actual behaviour.
            Text {
                width: parent.width
                elide: Text.ElideRight
                wrapMode: Text.WordWrap
                maximumLineCount: 2
                text: "Checked when you open Quire, and whenever you ask."
                font.pointSize: Style.smallSize
                color: Style.muted
            }
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

        CoverGrid {
            id: tiles
            objectName: "watchTiles"
            anchors {
                fill: parent
                leftMargin: Style.margin; rightMargin: Style.margin
            }
            visible: !screen.listing
            model: screen.model
            firstIndex: Paging.firstIndex(screen.page, screen.pageSize)
            // The watch rows are the one model here that carries a badge.
            badges: true
            onTapped: {
                var row = screen.model.get(index)
                // Continues reading now (tapRow, PLAN §12.9) rather than
                // opening the series first. The backend only clears a
                // watch's badge once the series' own chapter list is served
                // (seriesSeen, backend/service/watch.go) — a tap that goes
                // straight into a saved chapter or a library document does
                // not serve one, so it no longer clears the badge the way an
                // "Open" tap used to. Browse (the menu) still does.
                screen.tapRow(row)
            }
            onHeld: {
                var at = tiles.mapToItem(screen, x, y)
                screen.openMenu(index, at.x, at.y)
            }
        }

        ListView {
            id: list
            objectName: "watchRows"
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

                // The press acknowledgement, behind the row's own text.
                Rectangle {
                    objectName: "rowPressFeedback"
                    anchors.fill: parent
                    color: rowArea.feedback ? Style.pressed : Style.paper
                }

                // The cover, at the leading edge and sized from the row rather
                // than sizing it: this screen works out how many watches fit
                // from Style.coverRowHeight, and a taller row would be fewer series
                // per page than before the picture arrived. The badge keeps
                // its end of the row — it is why this screen exists.
                CoverArt {
                    id: thumb
                    objectName: "rowCover"
                    anchors {
                        left: parent.left; leftMargin: Style.margin
                        verticalCenter: parent.verticalCenter
                    }
                    width: Style.thumbWidth
                    height: Style.thumbHeight
                    // No watch row ever arrives with a coverPath — it is the
                    // frontend's own field, filled when a fetch lands
                    // (Watch.js) — so the placeholder is what is drawn until
                    // one does, and an absent role reads as none rather than
                    // throwing halfway through the row.
                    coverPath: model.coverPath === undefined ? "" : String(model.coverPath)
                    caption: model.title
                    lines: 2
                    padding: Style.gap / 4
                }

                Column {
                    anchors {
                        left: thumb.right; leftMargin: Style.gap
                        right: badge.left; rightMargin: Style.gap
                        verticalCenter: parent.verticalCenter
                    }
                    spacing: 4

                    Text {
                        objectName: "watchRowTitle"
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
                    // The badge is the one piece of live state on this screen
                    // and the reason the screen exists, so it gets the accent
                    // — as a solid fill, which is the only shape the accent
                    // takes (ui/Style.js). It used to be an accent border
                    // around an accent count on a grey fill; on the device the
                    // count read muddy and the 2px border was a thin coloured
                    // stroke, which is the same failure drawn twice.
                    //
                    // It does not *depend* on the colour: the badge is only
                    // drawn when there is something to report (below), it is a
                    // filled box at the end of the row either way, and the
                    // count is spelled out inside it.
                    color: Style.accent
                    border.width: 0
                    radius: 6
                    visible: model.badge.length > 0

                    Text {
                        id: badgeText
                        objectName: "watchBadgeText"
                        anchors.centerIn: parent
                        text: model.badge
                        font.pointSize: Style.smallSize
                        // Black on the accent: 5.90:1, which is why the accent
                        // is the value it is (ui/Style.js). Paper-coloured
                        // text here would put letterforms back inside the
                        // colour region, which is what was just measured as
                        // unreadable.
                        color: Style.ink
                    }
                }

                HoldArea {
                    id: rowArea
                    objectName: "watchRowArea"
                    anchors.fill: parent
                    // Continues reading (tapRow), same as the tile — see its
                    // own comment above for what this changes about the
                    // badge.
                    onTapped: screen.tapRow(model)
                    // The hold used to open the strip directly. It opens the
                    // menu now — the same menu the tiles get — and "Stop
                    // watching" is what opens the strip, so the row and the
                    // tile offer the same four things.
                    onHeld: {
                        var at = rowArea.mapToItem(screen, rowArea.pressX, rowArea.pressY)
                        screen.openMenu(index, at.x, at.y)
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

    // Last, so it is over the rows, the tiles, the strip and the pager.
    ContextMenu {
        id: menu
        objectName: "watchMenu"
        anchors.fill: parent
        onChosen: screen.chose(action)
    }
}
