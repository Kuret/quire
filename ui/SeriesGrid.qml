// The series grid — PLAN §6 M3: "Series grid with covers. Cache on disk,
// downscale hard, never hold more than a screenful in memory."
//
// Since PLAN §12.1 it does not scroll: it turns pages. The model holds exactly
// one display page, because the backend's pager (backend/service/paging.go)
// serves display-sized slices out of a cache of source pages — so a page turn
// is usually not an HTTP request at all, and a screenful is all that is ever
// resident, which is the memory rule kept by construction.
//
// The page size is computed from the viewport and sent to the backend with
// every request. It is never hardcoded: the panel is 1620×2160, but the AppLoad
// PC emulator is a window, and a page sized to the panel would leave that one
// with a half-visible row — the scrolling problem in miniature, and an
// invitation to the swipe PLAN §12.1 is removing.
//
// The images themselves are files on disk that the backend downscaled — nothing
// image-shaped ever crosses the socket (PLAN §7.1).
//
// Since PLAN §7.1 type 75 it has two layouts: the tiles above, and rows for the
// case a cover does not help — a search whose results are twelve near-identical
// covers of the same series, where the title is the thing being read. The row
// carries a thumbnail all the same: it is the same already-downscaled cache
// entry the tile draws, so it costs nothing to fetch, and a row that showed
// *no* picture made the two layouts differ in what they knew rather than in
// how much room they gave the title. Which one
// is showing is the backend's stored setting (Views.js), and both share the
// paging and the three signals, so Main.qml does not know which is on screen.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging
import "Screens.js" as Screens
import "Views.js" as Views

Item {
    id: screen

    property alias model: tiles.model
    property bool busy: false
    property string emptyMessage: ""
    property string query: ""
    property bool searching: false

    // Which layout is showing, from the store by way of Main.qml. Grid until a
    // status says otherwise: the screen is drawn long before the first Pong
    // lands and must not wait for it (Views.js).
    property string view: Views.GRID
    readonly property bool listing: screen.view === Views.LIST

    // The count comes from the model, not from the view: a view updates its own
    // count during a layout pass, so anything derived from it lags by a frame —
    // and a frame on this panel is a repaint.
    readonly property int rowCount: screen.model ? screen.model.count : 0

    // Whether the current page has any authors worth a tile subtitle — the
    // tile grid's captionHeight only grows when this is true (CoverGrid.qml),
    // so a manga listing (none of these) keeps today's density exactly and a
    // book listing (Shelfmark) costs one row per page, uniformly. Recomputed
    // whenever rowCount changes, which is every time a page is (re)filled.
    readonly property bool anyAuthors: {
        var n = screen.rowCount
        for (var i = 0; i < n; ++i) {
            var row = screen.model.get(i)
            if (row.authors !== undefined && row.authors !== null && String(row.authors).length > 0)
                return true
        }
        return false
    }

    // The source this listing came from, shown on each row of the list layout.
    // It is the same for every row today; when a search across every source
    // lands (Msg.SearchAll) it is the line the sources go on.
    property string sourceName: ""

    // Where in the listing we are. All three come from the backend: it owns the
    // cache, so it is the only thing that knows whether there is more, and
    // totalPages stays 0 until the source has actually run out rather than
    // being guessed at (PLAN §12.1).
    property int page: 1
    property int totalPages: 0
    property bool hasMore: false
    property int pendingPage: 0

    // ---- browse listings ---------------------------------------------------
    //
    // Which listing is showing besides a text search: theme.ListingLatest by
    // default (every visit starts there), or one the picker below chose. Both
    // the id and its label come from the backend (Msg.Listings) — never
    // invented here, per PLAN §2 — so currentListingLabel starts empty and the
    // browse button just says "Browse" until the first Msg.Listings answers
    // and fills in "Latest updates" for it.
    property string currentListingId: "latest"
    property string currentListingLabel: ""

    // The full menu Msg.Listings sent for this source, [{id, label, group}].
    property var listings: []

    // Whether the picker panel is open. It is this screen's own state — like
    // the long-press menu — because nothing outside the grid needs to know.
    property bool pickerOpen: false

    // listings, grouped and ordered sort/status/genre, for the picker's three
    // headings. A group nobody offered stays an empty array rather than being
    // left out of the list — the Repeater below hides an empty section by its
    // own entries.length, not by this shape changing.
    readonly property var listingGroups: {
        var sortG = [], statusG = [], genreG = []
        for (var i = 0; i < screen.listings.length; ++i) {
            var l = screen.listings[i]
            if (l.group === "sort") sortG.push(l)
            else if (l.group === "status") statusG.push(l)
            else if (l.group === "genre") genreG.push(l)
        }
        return [
            {"heading": "Sort", "entries": sortG},
            {"heading": "Status", "entries": statusG},
            {"heading": "Genres", "entries": genreG}
        ]
    }

    // applyListings stores a fresh Msg.Listings answer, and — only while
    // still showing the default listing nothing has changed away from yet —
    // fills in currentListingLabel from it, so the very first draw of the
    // button says "Browse" and the one right after Listings lands says
    // "Browse: Latest updates" in the backend's own words.
    function applyListings(list) {
        screen.listings = list ? list : []
        for (var i = 0; i < screen.listings.length; ++i) {
            if (screen.listings[i].id === screen.currentListingId) {
                screen.currentListingLabel = screen.listings[i].label
                break
            }
        }
    }

    // chooseListing is the picker's one action: close it, switch to the
    // listing picked, and browse its first page — through browseRequested,
    // the same signal the Clear button and the old "Latest" tap already used,
    // so Main.qml has exactly one place that composes the request.
    function chooseListing(id, label) {
        screen.pickerOpen = false
        screen.currentListingId = id
        screen.currentListingLabel = label
        screen.query = ""
        screen.searching = false
        screen.browseRequested()
    }

    // How many items fit, whole rows only, in whichever layout is showing. The
    // page size is sent to the backend with every request, so switching layout
    // changes what a page *is* — which is why onPageSizeChanged below refetches
    // rather than re-slicing: the backend owns the listing and the frontend
    // never has the rows a taller page would need.
    readonly property int columns: screen.listing ? 1 : tiles.columns
    readonly property int rowsPerPage: screen.listing
        ? Paging.rowsPerPage(viewport.height, Style.coverRowHeight) : tiles.rowsPerPage
    readonly property int pageSize: screen.listing
        ? Paging.itemsPerPage(viewport.height, Style.coverRowHeight, 1) : tiles.pageSize

    signal searchRequested(string query)
    signal browseRequested()
    // coversRequested carries the whole visible set at once, because the
    // backend treats a batch as superseding the last one: a cover still being
    // fetched for a tile that a page turn took away is work nobody will see,
    // and on a device whose politeness limiter serialises requests (PLAN §7.4)
    // it is work standing in front of the covers that *are* on screen.
    signal coversRequested(var covers)
    signal openRequested(string seriesId, string title)
    signal pageRequested(int page)

    // The long-press menu's two actions (PLAN §12.2's watch, reached from the
    // results rather than only from inside a series). Watching is a property
    // of a (source, series) pair and this screen shows one source at a time,
    // so the source is Main.qml's to add.
    signal watchRequested(string seriesId, string title)
    signal unwatchRequested(string seriesId)

    // ---- the long-press menu ------------------------------------------------
    //
    // The row is remembered by its ids rather than by its index, for the same
    // reason the watch list's strip is: an index points at a different series
    // the moment a page lands, and a page can land while the menu is open —
    // this screen's rows are refilled by the backend, not paged in hand.
    property string menuSeriesId: ""
    property string menuTitle: ""
    property bool menuWatched: false

    // itemsFor is what one row can do. Two lines: opening it, and the watch
    // that would otherwise mean opening it first (ChapterList's button).
    function itemsFor(row) {
        return [
            {"action": "open", "label": "Open", "enabled": true},
            row.watched === true
                ? {"action": "unwatch", "label": "Stop watching", "enabled": true}
                : {"action": "watch", "label": "Watch", "enabled": true}
        ]
    }

    // openMenu is the one way in, from either layout. Both pass a point in
    // this screen's coordinates and the menu clamps it.
    function openMenu(index, x, y) {
        if (index < 0 || index >= screen.rowCount)
            return
        var row = screen.model.get(index)
        screen.menuSeriesId = row.seriesId
        screen.menuTitle = row.title
        // A model that has never carried the role at all reads as not
        // watched, rather than throwing halfway through building the menu.
        screen.menuWatched = row.watched === true
        menu.show(screen.itemsFor(row), x, y)
    }

    function chose(action) {
        switch (action) {
        case "open":
            screen.openRequested(screen.menuSeriesId, screen.menuTitle)
            return
        case "watch":
            screen.watchRequested(screen.menuSeriesId, screen.menuTitle)
            return
        case "unwatch":
            // Straight through, as the series screen's own button is: the
            // confirm this menu respects on Watching is that screen's strip,
            // and there is no such question here to route into. Dropping a
            // watch loses no downloads and is undone by tapping Watch again.
            screen.unwatchRequested(screen.menuSeriesId)
            return
        }
    }

    // dismissInput puts the keyboard away (PLAN §11 Q5). Two steps, in this
    // order: drop the field's focus, then lower the keyboard. Quire's keyboard
    // is a plain element whose visibility is `searching`, so the second step is
    // an assignment rather than a request — but the order is kept, because it
    // is the order Screens.js records a real bug fix for.
    //
    // The placeholder comes back with it, which is right: it says "Search this
    // source" to a screen nobody is typing into.
    function dismissInput() {
        Screens.dismissKeyboard([queryBar.field])
        screen.searching = false
    }

    function reset() {
        screen.dismissInput()
        screen.query = ""
        screen.searching = false
        screen.emptyMessage = ""
        screen.page = 1
        screen.totalPages = 0
        screen.hasMore = false
        screen.pendingPage = 0
        // Default listing per visit: latest. The label is filled in again
        // once the fresh Msg.Listings this source's opening asks for answers
        // (Main.qml's openSource) — until then the button just says "Browse".
        screen.currentListingId = "latest"
        screen.currentListingLabel = ""
        screen.listings = []
        screen.pickerOpen = false
    }

    // The geometry changed — a resized emulator window, never the panel — so
    // the page in hand is the wrong size. Ask for a whole one rather than
    // leaving a gap or a clipped row.
    onPageSizeChanged: {
        if (screen.pageSize > 0 && screen.rowCount > 0 && screen.rowCount !== screen.pageSize)
            screen.turnTo(1)
    }

    // Switching layout changes what is drawn and therefore what is worth
    // fetching. The page itself is refetched by onPageSizeChanged above — a
    // list page holds many more results than a grid page — so this is only
    // about the covers.
    onViewChanged: screen.requestVisibleCovers()

    // seriesRowSubtitle composes the one line under a row's title: the row's
    // authors and the source name, in that order, joined by " · " when both
    // are there. Author strings arrive already comma-joined (Main.qml's
    // fillSeries does that, since a ListModel role cannot reliably hold a
    // list — see its comment) so this only has to compose the two halves.
    //
    // A ListModel fixes its roles on the first append, so a listing whose
    // rows never carried "authors" at all — every source but Shelfmark today
    // — reads back as an *absent* property rather than an empty string, and
    // reading .length off that throws (CoverGrid's roleOf records the same
    // trap). Guarded here the same defensive way, so a source with nothing to
    // say about authors renders exactly as it always has: the source name
    // alone, no stray separator, no empty parenthetical.
    function seriesRowSubtitle(row) {
        var authors = row.authors === undefined || row.authors === null ? "" : String(row.authors)
        if (authors.length > 0 && screen.sourceName.length > 0)
            return authors + " · " + screen.sourceName
        if (authors.length > 0)
            return authors
        return screen.sourceName
    }

    function turnTo(page) {
        screen.pendingPage = page
        screen.busy = true
        screen.pageRequested(page)
    }

    // Asks for every cover on the page in one go. Called when the page lands,
    // not per row: the set is what matters, and the backend cancels whatever
    // the previous set left in flight.
    //
    // **Both layouts ask.** The rows used to ask for nothing, which was right
    // while a row was two lines of text; since the row draws a thumbnail it
    // wants the same file the tile wants — the cache entry is already
    // downscaled to 300x450 (backend/covers/covers.go), so drawing it at row
    // height is a smaller decode of a picture that was going to be fetched
    // anyway, not a second request.
    //
    // The batch is sent even when it comes out empty, so there is one path and
    // it always reports what the page wants: an empty batch supersedes the last
    // one, which is how fetches still in flight for a page that has been turned
    // away from get dropped.
    function requestVisibleCovers() {
        var wanted = []
        for (var i = 0; i < screen.rowCount; ++i) {
            var row = screen.model.get(i)
            if (row.coverUrl && row.coverUrl.length > 0 && row.coverPath.length === 0)
                wanted.push({"seriesId": row.seriesId, "url": row.coverUrl})
        }
        screen.coversRequested(wanted)
    }

    // ---- search bar --------------------------------------------------------

    Item {
        id: searchBar
        objectName: "searchBar"
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: Style.rowHeight

        // The field, Clear and all, shared with the combined search across
        // every source (ui/SearchBar.qml). Only the placeholder differs, and
        // it differs in the one way that matters here: it names the scope.
        SearchBar {
            id: queryBar
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: browseButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            query: screen.query
            searching: screen.searching
            placeholder: "Search this source"
            onRaised: screen.searching = true
            onCleared: {
                screen.query = ""
                // Deliberately *not* dismissInput: the user is about to type.
                // `searching` is set rather than left alone, because Clear is
                // reachable from a bar that was only tapped once and the
                // keyboard may never have been up.
                screen.searching = true
                // Clearing steps back out of the search, to whichever listing
                // was chosen before it — Browse's own default or one picked
                // from the panel below — so, unlike a bare Clear on a filter,
                // this re-fetches rather than leaving the old search's rows on
                // screen under a keyboard with nothing left typed into it.
                screen.busy = true
                screen.pendingPage = 1
                screen.browseRequested()
            }
        }

        // "Browse: <label>" once Msg.Listings has named the current listing,
        // "Browse" alone before that or when the full wording will not fit —
        // measured against measureLabel's real font metrics rather than a
        // guessed character count, so it is exactly the same rule on any
        // label. A tap opens the picker; it no longer browses by itself,
        // since a single tap and a listing choice are now two different
        // things.
        Text {
            id: measureLabel
            visible: false
            text: screen.currentListingLabel.length > 0 ? "Browse: " + screen.currentListingLabel : "Browse"
            font.pointSize: Style.smallSize
        }

        Rectangle {
            id: browseButton
            objectName: "browseButton"
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            width: 180
            height: Style.buttonHeight
            color: browseArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                objectName: "browseButtonLabel"
                anchors.centerIn: parent
                text: measureLabel.contentWidth <= (browseButton.width - Style.gap) ? measureLabel.text : "Browse"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: browseArea
                objectName: "browseButtonArea"
                anchors.fill: parent
                onClicked: screen.pickerOpen = !screen.pickerOpen
            }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    // ---- the results -------------------------------------------------------
    //
    // The viewport is fixed between the search bar and the pager, and does not
    // move when the keyboard opens — the keyboard is anchored above the pager
    // and draws over the results instead. A control that jumps out from under a
    // thumb is worse than the scroll it replaced (PLAN §12.1).
    //
    // Both layouts are built and one is hidden, rather than a Loader swapping
    // them: the hidden one's geometry is what the *visible* one's page size is
    // compared against when the switch is thrown, and a layout that does not
    // exist yet has no geometry to ask.

    Item {
        id: viewport
        anchors {
            top: searchBar.bottom
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
            bottom: pagerBar.top
        }

        CoverGrid {
            id: tiles
            objectName: "seriesTiles"
            anchors.fill: parent
            visible: !screen.listing
            // The model holds this page and only this page, so the window
            // starts at the top of it.
            firstIndex: 0
            hasSubtitles: screen.anyAuthors
            onTapped: {
                var row = screen.model.get(index)
                screen.openRequested(row.seriesId, row.title)
            }
            onHeld: {
                var at = tiles.mapToItem(screen, x, y)
                screen.openMenu(index, at.x, at.y)
            }
        }

        ListView {
            id: rows
            objectName: "seriesRows"
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
                // The whole row, not the thumbnail on it: a tile is darkened
                // because the tile is what the finger is on, and a row's
                // thumbnail is a thumbnail's worth of a slow panel.
                Rectangle {
                    objectName: "rowPressFeedback"
                    anchors.fill: parent
                    color: rowArea.feedback ? Style.pressed : Style.paper
                }

                // The cover, small, at the leading edge. It is sized from the
                // row (Style.thumbWidth) and never sizes it: this screen works
                // out how many results fit from Style.coverRowHeight, so a
                // thumbnail that made the row taller would quietly be fewer
                // results per page than before the picture arrived.
                CoverArt {
                    id: thumb
                    objectName: "rowCover"
                    anchors {
                        left: parent.left
                        verticalCenter: parent.verticalCenter
                    }
                    width: Style.thumbWidth
                    height: Style.thumbHeight
                    // A ListModel fixes its roles on the first append, so
                    // "this model has no coverPath yet" is a real state — and
                    // an absent role is an absent property, not an empty
                    // string (CoverGrid's roleOf records the same trap).
                    coverPath: model.coverPath === undefined ? "" : String(model.coverPath)
                    caption: model.title
                    // Two lines at this width. The row's own title is beside
                    // it and is what is actually read; this only has to make
                    // an empty box look like the book it stands for.
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

                    // The authors and where the result came from. The line
                    // keeps its height when there is nothing to put on it, so
                    // that a listing without one does not hold a different
                    // number of rows per page than a listing with one.
                    Text {
                        objectName: "seriesRowSubtitle"
                        width: parent.width
                        elide: Text.ElideRight
                        text: screen.seriesRowSubtitle(model)
                        font.pointSize: Style.smallSize
                        color: Style.muted
                    }
                }

                // The same component the tiles use, so a row and a tile hold
                // for the same length of time and offer the same menu.
                HoldArea {
                    id: rowArea
                    objectName: "seriesRowArea"
                    anchors.fill: parent
                    onTapped: screen.openRequested(model.seriesId, model.title)
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

        // Discrete status text rather than a spinner (PLAN §6 M3: no animations).
        Text {
            anchors.centerIn: parent
            width: parent.width - Style.margin * 2
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
            text: screen.busy ? "Fetching…" : screen.emptyMessage
            font.pointSize: Style.bodySize
            color: Style.muted
            visible: (screen.busy || screen.emptyMessage.length > 0) && screen.rowCount === 0
        }
    }

    // ---- paging ------------------------------------------------------------

    PagerBar {
        id: pagerBar
        objectName: "seriesPager"
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.hasMore
        busy: screen.busy && screen.rowCount > 0
        pendingPage: screen.pendingPage
        onPreviousRequested: screen.turnTo(screen.page - 1)
        onNextRequested: screen.turnTo(screen.page + 1)
    }

    // ---- the listing picker -------------------------------------------------
    //
    // A full-width panel, not a small anchored menu like ContextMenu: it is
    // reached from a fixed button rather than a press point, and a source's
    // genres can run to dozens of entries the picker has to show *something*
    // for. It scrolls in a Flickable rather than paging, the one exception to
    // PLAN §12.1 the "Aa" reader-settings panel already set the precedent
    // for (PLAN §12.8 round 3): this is a menu of short lines, not a listing
    // of results, and capping it at a screenful would just hide genres near
    // the end of the site's own list.
    Item {
        id: listingPicker
        objectName: "listingPicker"
        anchors.fill: parent
        visible: screen.pickerOpen

        // Tap anywhere outside the panel to close it without choosing
        // anything — the same dismiss ContextMenu uses.
        MouseArea {
            objectName: "listingPickerScrim"
            anchors.fill: parent
            enabled: screen.pickerOpen
            onClicked: screen.pickerOpen = false
        }

        Rectangle {
            id: pickerPanel
            objectName: "listingPickerPanel"
            // Anchored to its own parent, offset by where the search bar and
            // pager sit: those two are listingPicker's siblings, not this
            // panel's, and Qt refuses an anchor to an item that is neither
            // parent nor sibling — the panel then had no geometry at all and
            // Browse opened nothing on the device. listingPicker fills the
            // screen, so the bars' own y is valid in its coordinates.
            anchors {
                top: parent.top; topMargin: searchBar.y + searchBar.height
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
                bottom: parent.bottom; bottomMargin: parent.height - pagerBar.y + Style.gap
            }
            color: Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Flickable {
                id: pickerFlick
                objectName: "listingPickerFlickable"
                anchors.fill: parent
                anchors.margins: Style.gap
                contentWidth: width
                contentHeight: pickerColumn.height
                clip: true
                boundsBehavior: Flickable.StopAtBounds

                Column {
                    id: pickerColumn
                    width: pickerFlick.width
                    spacing: Style.gap

                    Repeater {
                        model: screen.listingGroups
                        delegate: Column {
                            width: pickerColumn.width
                            spacing: 4
                            visible: modelData.entries.length > 0

                            SectionHeading {
                                width: parent.width
                                text: modelData.heading
                            }

                            Repeater {
                                model: modelData.entries
                                delegate: Rectangle {
                                    objectName: "listingPickerEntry"
                                    width: pickerColumn.width
                                    height: Style.rowHeight
                                    color: entryArea.pressed ? Style.pressed : Style.paper

                                    Text {
                                        anchors {
                                            left: parent.left; leftMargin: Style.gap
                                            right: parent.right; rightMargin: Style.gap
                                            verticalCenter: parent.verticalCenter
                                        }
                                        elide: Text.ElideRight
                                        text: modelData.label
                                        font.pointSize: Style.bodySize
                                        color: Style.ink
                                    }

                                    MouseArea {
                                        id: entryArea
                                        anchors.fill: parent
                                        onClicked: screen.chooseListing(modelData.id, modelData.label)
                                    }

                                    Rectangle {
                                        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                                        height: Style.hairline
                                        color: Style.rule
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // ---- search input ------------------------------------------------------
    //
    // Anchored to the pager rather than to the bottom of the screen, so the
    // pager stays exactly where it was while the keyboard is up.

    Keyboard {
        id: keyboard
        objectName: "searchKeyboard"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        visible: screen.searching
        layout: "text"
        onKeyTyped: screen.query += text
        onBackspace: screen.query = screen.query.substring(0, screen.query.length - 1)
        onClearAll: screen.query = ""
        onSubmit: {
            // The keyboard does not survive the thing it was raised for.
            screen.dismissInput()
            if (screen.query.length > 0) {
                screen.searchRequested(screen.query)
            }
        }
    }

    // Last, so it is over the results, the pager and the keyboard alike. A
    // menu that something else could draw on top of would be a menu whose
    // items are sometimes untappable.
    ContextMenu {
        id: menu
        objectName: "seriesMenu"
        anchors.fill: parent
        onChosen: screen.chose(action)
    }
}
