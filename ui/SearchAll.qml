// One query across every enabled source — Msg.SearchAll.
//
// It is reached from the sources screen, which is where a search across *all*
// of them belongs: the per-source screen (ui/SeriesGrid.qml) is unchanged and
// this is an addition to it, not a replacement. Opening a source and searching
// inside it is still the way to search one.
//
// It shares the stored "search" layout with that screen (Views.js): a combined
// search is a search, the tiles and the rows are the same tiles and rows, and
// a user who set one screen to rows did not mean "rows, except when I search
// everything".
//
// # What is different from SeriesGrid, and why
//
//   - **A group is a series, not a result.** The backend merged the sources by
//     normalised title, so one row can stand for the same book on three sites.
//     The list layout puts their names on the subtitle line that screen
//     already keeps for one source's name; the grid marks the tile with
//     CoverGrid's badge, because a tile has no room for a line of names. That
//     reasoning is about *source names* specifically — an author is one short
//     name, not a list of them, so it gets CoverGrid's subtitle line instead
//     (the same one SeriesGrid's tiles use), and the badge is unchanged.
//   - **No long-press menu.** Tap opens; the series screen carries the
//     actions. "Watch" on a *group* would have to pick a source silently, and
//     watch records, downloads and the downloaded overview are all keyed by
//     (source, series) — so a menu here would be a menu that quietly decides
//     which pair the user is acting on. The series screen's switcher makes
//     that choice visible instead.
//   - **No "Latest".** An empty query is not a browse here: it would fan out a
//     request to every configured site for a box nobody has typed in
//     (Grouping.js searchable).
//   - **Sources that failed are not a failure.** They contributed no rows
//     while the rest filled the screen, so they are one subdued line under the
//     results and nothing else — never a blank page, never an error.
//   - **A filter on what kind of thing a result is.** Only here: a single
//     source is one kind, so the same control on ui/SeriesGrid.qml would be
//     three buttons that cannot change anything. See the strip below.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging
import "Screens.js" as Screens
import "Views.js" as Views
import "Grouping.js" as Grouping
import "Kinds.js" as Kinds

Item {
    id: screen

    property alias model: tiles.model
    property bool busy: false
    property string emptyMessage: ""
    property string query: ""
    property bool searching: false

    // The layout, from the store by way of Main.qml — the same setting the
    // per-source search screen draws in. Grid until a status says otherwise:
    // the screen is drawn long before the first Pong lands (Views.js).
    property string view: Views.GRID
    readonly property bool listing: screen.view === Views.LIST

    // The count comes from the model, not from the view: a view updates its
    // own count during a layout pass, so anything derived from it lags by a
    // frame — and a frame on this panel is a repaint.
    readonly property int rowCount: screen.model ? screen.model.count : 0

    // Whether the current page has any authors worth a tile subtitle —
    // CoverGrid's captionHeight only grows when this is true, so a page of
    // manga (none of these) keeps today's density exactly (ui/CoverGrid.qml,
    // ui/SeriesGrid.qml's own screen.anyAuthors does the same thing).
    // Recomputed whenever rowCount changes, which is every time a page is
    // (re)filled.
    readonly property bool anyAuthors: {
        var n = screen.rowCount
        for (var i = 0; i < n; ++i) {
            var row = screen.model.get(i)
            if (row.authors !== undefined && row.authors !== null && String(row.authors).length > 0)
                return true
        }
        return false
    }

    // Which sources did not answer, composed in Grouping.js from the reply's
    // sourceErrors. Empty when they all did, and empty is what is shown: a
    // line that is always there is a line nobody reads.
    property string failedSources: ""

    // ---- the kind filter ----------------------------------------------------
    //
    // Which kind of result is being shown: Kinds.ALL, MANGA or BOOK. A combined
    // search is the one screen where the two mix, which is the whole reason the
    // control is here and nowhere else.
    //
    // **It is transient on purpose, and that is a limit rather than a
    // decision.** Every other remembered choice in Quire — the layout switch,
    // the robots setting — goes to the backend's store and comes back on the
    // status, so the screen draws what was saved rather than what it hoped.
    // There is no stored setting for this one, so it starts at ALL every time
    // the app starts, and a filter that outlived a restart would need that
    // setting first.
    property string kindFilter: Kinds.ALL

    readonly property string filterNote: Kinds.filterLine(screen.kindFilter)

    // showKind is the one way the filter moves. Guarded, so tapping the filter
    // already on asks for nothing — a fan-out to every configured site is the
    // most expensive thing this screen can do for no change.
    //
    // **The filter is part of the search, not a view on one page of it.** The
    // kind travels with the request (Main.qml's requestSearchAllPage), so
    // choosing Books re-runs the same query from page 1 asking only book
    // sources — a Books search never touches a manga site, and page 1 is the
    // first N book groups rather than a page a client-side hide had hollowed
    // out. That is also why this only fires the request when there is a query
    // to run: tapping a filter on an empty screen must not fan out for a box
    // nobody has typed in (searchRequested already carries that guard, in
    // Main.qml's requestSearchAllPage).
    function showKind(kind) {
        if (screen.kindFilter === kind)
            return
        screen.kindFilter = kind
        if (!Grouping.searchable(screen.query))
            return
        screen.page = 1
        screen.totalPages = 0
        screen.hasMore = false
        screen.pendingPage = 1
        screen.busy = true
        screen.searchRequested(screen.query)
    }

    // Where in the listing we are. All of it comes from the backend: it owns
    // the merge and the cache, so it is the only thing that knows whether
    // there is more, and totalPages stays 0 until every source has run dry
    // rather than being guessed at (PLAN §12.1).
    property int page: 1
    property int totalPages: 0
    property bool hasMore: false
    property int pendingPage: 0

    // How many groups fit, whole rows only, in whichever layout is showing.
    // The page size is sent with every request, so switching layout changes
    // what a page *is* — which is why onPageSizeChanged refetches rather than
    // re-slicing: the backend owns the listing and this screen never has the
    // rows a taller page would need.
    readonly property int columns: screen.listing ? 1 : tiles.columns
    readonly property int rowsPerPage: screen.listing
        ? Paging.rowsPerPage(viewport.height, Style.coverRowHeight) : tiles.rowsPerPage
    readonly property int pageSize: screen.listing
        ? Paging.itemsPerPage(viewport.height, Style.coverRowHeight, 1) : tiles.pageSize

    signal searchRequested(string query)
    // coversRequested carries the whole visible set at once, because the
    // backend treats a batch as superseding the last one — and every entry
    // names its own source, because this screen draws series from several at
    // once and only the source knows how to fetch its covers (Main.qml's
    // requestCoversBySource records what splitting the batch cost).
    signal coversRequested(var covers)
    // The group, and the pair a tap opens: the first match, which the backend
    // put first because it is first in the user's source order. The key comes
    // with it because it is what the series screen's switcher is looked up by.
    signal openRequested(string key, string sourceId, string seriesId, string title)
    signal pageRequested(int page)

    // dismissInput puts the keyboard away (PLAN §11 Q5). Two steps, in this
    // order: drop the field's focus, then lower the keyboard. Quire's keyboard
    // is a plain element whose visibility is `searching`, so the second step is
    // an assignment rather than a request — but the order is kept, because it
    // is the order Screens.js records a real bug fix for.
    function dismissInput() {
        Screens.dismissKeyboard([queryBar.field])
        screen.searching = false
    }

    function reset() {
        screen.dismissInput()
        screen.query = ""
        screen.emptyMessage = ""
        screen.failedSources = ""
        // A fresh screen is a fresh question, and the filter is part of the
        // question. Coming *back* from a series is a different route and does
        // not pass through here, so the filter survives that — what the user
        // was looking at is still what they were looking at.
        screen.kindFilter = Kinds.ALL
        screen.page = 1
        screen.totalPages = 0
        screen.hasMore = false
        screen.pendingPage = 0
    }

    // The geometry changed — a resized emulator window, never the panel — so
    // the page in hand is the wrong size. Ask for a whole one rather than
    // leaving a gap or a clipped row. Only ever when there is a search to
    // re-run: an empty query must not fan out, whatever moved (Grouping.js).
    onPageSizeChanged: {
        if (Grouping.searchable(screen.query) && screen.pageSize > 0
                && screen.rowCount > 0 && screen.rowCount !== screen.pageSize)
            screen.turnTo(1)
    }

    // Switching layout changes what is drawn and therefore what is worth
    // fetching. The page itself is refetched by onPageSizeChanged above, so
    // this is only about the covers.
    onViewChanged: screen.requestVisibleCovers()

    function turnTo(page) {
        screen.pendingPage = page
        screen.busy = true
        screen.pageRequested(page)
    }

    // Asks for every cover on the page in one go. Called when the page lands,
    // not per tile: the set is what matters, and the backend cancels whatever
    // the previous set left in flight.
    //
    // **Both layouts ask.** The rows used to ask for nothing, which was right
    // while a row was two lines of text; since the row draws a thumbnail
    // (ui/CoverArt.qml) it wants the same already-downscaled file the tile
    // wants, so it is a smaller decode of a picture that would have been
    // fetched anyway rather than a second request.
    //
    // The batch is sent even when it comes out empty, so there is one path and
    // it always reports what the page wants — and an empty batch supersedes the
    // last one, which is what drops fetches for a page turned away from.
    //
    // **The cover is requested under its own source, not the row's.**
    // row.sourceId/seriesId name the match a tap opens (the group's primary),
    // which is not always where row.coverUrl came from — a group's cover is
    // the best-ranked match that *has* one (Grouping.js groupRow). Sending the
    // row's own source for a URL that belongs to a different one is exactly
    // the cross-domain request the backend's SSRF guard exists to refuse.
    function requestVisibleCovers() {
        var wanted = []
        for (var i = 0; i < screen.rowCount; ++i) {
            var row = screen.model.get(i)
            if (row.coverUrl && row.coverUrl.length > 0 && row.coverPath.length === 0)
                wanted.push({"sourceId": row.coverSourceId, "seriesId": row.coverSeriesId,
                             "url": row.coverUrl})
        }
        screen.coversRequested(wanted)
    }

    // open is the one way in, from a tile or from a row. Both pass a model
    // index and the row is looked up here, so neither layout carries a copy of
    // what a group means.
    function open(index) {
        if (index < 0 || index >= screen.rowCount)
            return
        var row = screen.model.get(index)
        screen.openRequested(row.key, row.sourceId, row.seriesId, row.title)
    }

    // submit sends the query, or nothing at all.
    //
    // The guard is here as well as in Main.qml because this is where the
    // keyboard's Search key lands: an empty box must cost no request, and the
    // screen must not go busy waiting for a reply that was never asked for.
    function submit() {
        screen.dismissInput()
        if (!Grouping.searchable(screen.query))
            return
        screen.page = 1
        screen.totalPages = 0
        screen.hasMore = false
        screen.pendingPage = 1
        screen.busy = true
        screen.searchRequested(screen.query)
    }

    // ---- search bar --------------------------------------------------------

    Item {
        id: searchBar
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: Style.rowHeight

        // The same field the per-source search uses, Clear and all. Only the
        // placeholder differs, and it differs in the one way that matters:
        // it names the scope.
        SearchBar {
            id: queryBar
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            query: screen.query
            searching: screen.searching
            placeholder: "Search every source"
            onRaised: screen.searching = true
            onCleared: {
                screen.query = ""
                // Not dismissInput: the user is about to type. The results on
                // screen stay until a new search runs, for the reason
                // SearchBar's `cleared` records.
                screen.searching = true
            }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    // ---- the kind filter ----------------------------------------------------
    //
    // Three words and the state of the filter spelled out beside them. The same
    // shape as the layout switch (ui/ViewToggle.qml), deliberately: the one you
    // are in is the filled one and it is dead to touch, so the control answers
    // "which am I in?" before it is touched and asking for it again costs
    // nothing.
    //
    // **The fill is the only colour, and it is a solid block.** Measured on the
    // device: this panel composes a coloured pixel through its filter array and
    // draws black at full resolution, so a coloured word or a coloured 2px
    // border reads muddy (ui/Style.js). Every label here is ink, on the filled
    // segment as well as the others.
    //
    // **And colour is never the only signal.** The filled segment is also the
    // inert one, it is visibly the dark one with the hue taken away, and the
    // line to its right says in words which filter is on and how much it is
    // holding back. A reader who cannot tell the hues apart, or a panel in
    // direct sun, gets the same answer.
    //
    // It is always on screen rather than appearing when a search has books in
    // it: a control that comes and goes is a control the user has to discover
    // twice, and its absence would be indistinguishable from a search that
    // happened to find no books.
    Item {
        id: kindStrip
        objectName: "kindStrip"
        anchors { top: searchBar.bottom; left: parent.left; right: parent.right }
        height: Style.buttonHeight + Style.margin

        Row {
            id: kindButtons
            anchors {
                left: parent.left; leftMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            spacing: Style.gap

            Repeater {
                // A Repeater over the three, rather than three near-identical
                // Rectangles: they differ in one word and one value, and a copy
                // is how the third ends up a few pixels shorter than the first.
                model: Kinds.filters()

                delegate: Rectangle {
                    id: segment
                    objectName: "kindFilter-" + modelData.kind

                    readonly property bool current: screen.kindFilter === modelData.kind

                    width: 180
                    height: Style.buttonHeight
                    color: segmentArea.pressed ? Style.pressed
                                               : (segment.current ? Style.accent : Style.paper)
                    border.width: 2
                    // Grey on both, as the layout switch has: a coloured 2px
                    // border is a thin coloured stroke, which is the shape the
                    // measurement in ui/Style.js rules out.
                    border.color: Style.rule
                    radius: 6

                    Text {
                        objectName: "kindFilterLabel-" + modelData.kind
                        anchors.centerIn: parent
                        text: modelData.label
                        font.pointSize: Style.smallSize
                        // Black on the accent fill: 5.90:1 (ui/Style.js). The
                        // segments that are not on stay grey on paper, so the
                        // three differ in type colour as well as in fill.
                        color: segment.current ? Style.ink : Style.muted
                    }

                    MouseArea {
                        id: segmentArea
                        objectName: "kindFilterArea-" + modelData.kind
                        anchors.fill: parent
                        // The filter already on is inert, for the reason
                        // showKind() guards as well: re-rendering the page of
                        // results is this screen's most expensive repaint and
                        // it would change nothing.
                        enabled: !segment.current
                        onClicked: screen.showKind(modelData.kind)
                    }
                }
            }
        }

        // What the filter is doing, in words. Empty — and therefore silent —
        // while everything is being shown; a line that is always there is a
        // line nobody reads.
        Text {
            objectName: "kindFilterNote"
            anchors {
                left: kindButtons.right; leftMargin: Style.gap
                right: parent.right; rightMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            horizontalAlignment: Text.AlignRight
            elide: Text.ElideRight
            text: screen.filterNote
            font.pointSize: Style.smallSize
            color: Style.ink
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    // ---- the results -------------------------------------------------------
    //
    // Both layouts are built and one is hidden, rather than a Loader swapping
    // them: the hidden one's geometry is what the *visible* one's page size is
    // compared against when the switch is thrown, and a layout that does not
    // exist yet has no geometry to ask.

    Item {
        id: viewport
        anchors {
            top: kindStrip.bottom
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
            bottom: failedBar.top
        }

        CoverGrid {
            id: tiles
            objectName: "searchAllTiles"
            anchors.fill: parent
            visible: !screen.listing
            // The model holds this page and only this page, so the window
            // starts at the top of it.
            firstIndex: 0
            // The badge corner is how a group found in more than one source is
            // marked here: a tile has no room for a line of *source names*,
            // and the slot is already drawn and already legible on this panel
            // (Grouping.js badgeFor). An author is one short name rather than
            // a list of them, so it gets the ordinary subtitle line instead —
            // see hasSubtitles below, and ui/SeriesGrid.qml's identical wiring.
            badges: true
            hasSubtitles: screen.anyAuthors
            onTapped: screen.open(index)
            // `held` is deliberately not connected. There is no menu on a
            // group: see the note at the top of this file.
        }

        ListView {
            id: rows
            objectName: "searchAllRows"
            anchors { top: parent.top; left: parent.left; right: parent.right }
            // Whole rows only; the remainder is blank rather than a half row.
            height: Paging.rowsPerPage(viewport.height, Style.coverRowHeight) * Style.coverRowHeight
            clip: true
            visible: screen.listing
            model: screen.model

            interactive: false
            cacheBuffer: 0

            delegate: Item {
                width: rows.width
                height: Style.coverRowHeight

                // The press acknowledgement, behind everything the row draws.
                // The whole row rather than the thumbnail on it: the tile is
                // darkened because the tile is what the finger is on, and a
                // row's thumbnail is too small to read as an answer to a press.
                Rectangle {
                    objectName: "rowPressFeedback"
                    anchors.fill: parent
                    color: rowArea.pressed ? Style.pressed : Style.paper
                }

                // The group's cover, at the leading edge and sized from the
                // row. It never sizes the row: the page size is worked out
                // from Style.coverRowHeight, so a taller row would be fewer groups
                // per page than before the picture arrived.
                CoverArt {
                    id: thumb
                    objectName: "rowCover"
                    anchors {
                        left: parent.left
                        verticalCenter: parent.verticalCenter
                    }
                    width: Style.thumbWidth
                    height: Style.thumbHeight
                    // An absent role is an absent property rather than an
                    // empty string, and a delegate that throws leaves the row
                    // half-built (CoverGrid's roleOf records the same trap).
                    coverPath: model.coverPath === undefined ? "" : String(model.coverPath)
                    caption: model.title
                    // Two lines at this width. The row's title is beside it
                    // and is what is read; this only keeps a group with no
                    // cover from being a blank box.
                    lines: 2
                    padding: Style.gap / 4
                }

                Column {
                    anchors {
                        left: thumb.right; leftMargin: Style.gap
                        right: parent.right
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

                    // The authors, what it is, and which sources have it, in
                    // that order — the line SeriesGrid keeps for the one
                    // source it is showing, here composed from all three
                    // (Grouping.subtitleLine). A book says so in a word before
                    // its sources, because "Shelfmark" is a source name like
                    // any other and nothing else on the row would tell a novel
                    // from a comic. The height is kept even when it is empty,
                    // so a page holds the same number of rows either way.
                    Text {
                        objectName: "searchAllRowSources"
                        width: parent.width
                        elide: Text.ElideRight
                        text: model.sources
                        font.pointSize: Style.smallSize
                        color: Style.muted
                    }
                }

                // A plain MouseArea rather than a HoldArea: a hold here has
                // nothing to open, and an area that darkens the row for half a
                // second and then does nothing is a control that looks broken.
                MouseArea {
                    id: rowArea
                    objectName: "searchAllRowArea"
                    anchors.fill: parent
                    onClicked: screen.open(index)
                }

                Rectangle {
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    height: Style.hairline
                    color: Style.rule
                }
            }
        }

        // Discrete status text rather than a spinner (PLAN §6 M3: no animations).
        Text {
            objectName: "searchAllStatus"
            anchors.centerIn: parent
            width: parent.width - Style.margin * 2
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
            text: screen.busy ? "Searching every source…" : screen.emptyMessage
            font.pointSize: Style.bodySize
            color: Style.muted
            visible: (screen.busy || screen.emptyMessage.length > 0) && screen.rowCount === 0
        }
    }

    // ---- the sources that did not answer ------------------------------------
    //
    // Under the results, above the pager, and quiet. A source that failed is a
    // partial answer: the others filled the screen and those rows are real, so
    // this may never blank the page or read as the search having failed. It
    // takes no room at all when every source answered.

    Item {
        id: failedBar
        objectName: "searchAllFailedBar"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        visible: screen.failedSources.length > 0
        height: visible ? Style.rowHeight : 0

        // A solid square at the head of the line, and the line itself black.
        // The accent is what stops the bar being read as part of the last
        // result row; it is not what says something went wrong, and it is not
        // a warning shape — a source that did not answer is a partial result,
        // and the whole point of this bar is that the rows above it are real.
        //
        // Beside the words rather than on them: the words used to carry the
        // accent and read muddy on the device (ui/Style.js). They are the
        // whole signal in any case — this line *is* the list of sources, not a
        // mark standing for one.
        AccentMark {
            objectName: "searchAllFailedMark"
            anchors {
                left: parent.left; leftMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
        }

        Text {
            objectName: "searchAllFailed"
            anchors {
                left: parent.left
                leftMargin: Style.margin + Style.accentMark + Style.gap / 2
                right: parent.right; rightMargin: Style.margin
                verticalCenter: parent.verticalCenter
            }
            elide: Text.ElideRight
            text: screen.failedSources
            font.pointSize: Style.smallSize
            // Still the smallest type on the screen, below the results and
            // above the pager.
            color: Style.ink
        }
    }

    // ---- paging ------------------------------------------------------------

    PagerBar {
        id: pagerBar
        objectName: "searchAllPager"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.hasMore
        busy: screen.busy && screen.rowCount > 0
        pendingPage: screen.pendingPage
        onPreviousRequested: screen.turnTo(screen.page - 1)
        onNextRequested: screen.turnTo(screen.page + 1)
    }

    // ---- search input ------------------------------------------------------
    //
    // Anchored to the pager rather than to the bottom of the screen, so the
    // pager stays exactly where it was while the keyboard is up.

    Keyboard {
        id: keyboard
        objectName: "searchAllKeyboard"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        visible: screen.searching
        layout: "text"
        onKeyTyped: screen.query += text
        onBackspace: screen.query = screen.query.substring(0, screen.query.length - 1)
        onClearAll: screen.query = ""
        onSubmit: screen.submit()
    }
}
