// The cover grid, shared by every screen that draws series as tiles.
//
// It was SeriesGrid's delegate until three screens wanted it — search results,
// Downloaded and Watching (PLAN §7.1 type 75). The alternative was to copy the
// tile into the two new screens, and it was rejected on a specific failure
// rather than on principle: the tile's interesting behaviour is what it does
// when there is *no* picture, and "many existing downloads will have no cover"
// means the placeholder is the common case on two of the three screens. Three
// copies is three places for a blank square to reappear.
//
// PLAN §6 M3's memory rule is kept here by construction, as it was in the grid
// this came from: nothing flicks, no delegate is cached off-page, and the
// images are files the backend already downscaled — nothing image-shaped
// crosses the socket (PLAN §7.1).
//
// What it deliberately does not own:
//
//   - **What a tap means, or a hold.** It reports the model index and the
//     screen looks the row up itself. The three models carry different ids — a
//     search result is a series on the open source, a Downloaded or Watching
//     row is a (source, series) pair — and a signal carrying "whatever the row
//     happens to have" would couple this file to all three of them at once.
//     The same index is what the long-press menu is built from, which is why
//     `held` carries one too.
//   - **Paging policy.** It draws the window it is given (`firstIndex`) and
//     reports the geometry it worked out. The search grid holds exactly one
//     page fetched from the backend; the other two page a list already in hand.
//     Those are genuinely different and neither belongs in a delegate.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: tiles

    property alias model: grid.model

    // Three columns on the panel's short edge; the tile is 2:3, the shape
    // covers are published at and the shape the cache writes.
    property int columns: 3

    // The model index of the first tile on the page now showing.
    property int firstIndex: 0

    // Whether the model carries a `badge` role — Watching's "3 new chapters",
    // composed in backend/service/watch.go and drawn verbatim. Only that one
    // model has them.
    property bool badges: false

    // roleOf reads a role that the model may not have at all.
    //
    // It is not defensive decoration. A ListModel fixes its roles on the first
    // append and silently drops keys added later, so "this model has no
    // coverPath yet" is a real state — and an absent role is *not* an empty
    // string but an absent property, which `.length` throws on. A delegate
    // that throws leaves the tile half-built, which on this panel is a tile
    // that stays half-built until something else repaints it.
    function roleOf(value) {
        return value === undefined || value === null ? "" : String(value)
    }

    // Whether the current listing has authors worth a subtitle line at all.
    // The screen that owns the model sets this from whether any row on the
    // page carries a non-empty `authors` — a search across manga sites has
    // none of them, and every tile on such a page must stay exactly the size
    // it always was, not grow a blank line nobody asked for. GridView cannot
    // vary one cell's height from another's, so this is page-wide rather than
    // per-tile: a book listing costs one row per page, uniformly.
    property bool hasSubtitles: false

    // The room under each tile for its title. Two short lines at
    // Style.smallSize, plus a third — this one at Style.smallSize too, for the
    // authors — only when hasSubtitles says the listing has any to show.
    readonly property int subtitleLineHeight: 20
    readonly property int captionHeight: tiles.hasSubtitles ? 56 + tiles.subtitleLineHeight : 56

    readonly property int cellWidth: grid.cellWidth
    readonly property int cellHeight: grid.cellHeight
    readonly property int rowsPerPage: Paging.rowsPerPage(tiles.height, grid.cellHeight)
    readonly property int pageSize: Paging.itemsPerPage(tiles.height, grid.cellHeight, tiles.columns)

    // tapped names the row, not its contents. See the note above.
    signal tapped(int index)

    // held is the long press, with the press point in *this* item's
    // coordinates so the screen can put its menu beside the finger. The screen
    // maps it the rest of the way, because only the screen knows what the menu
    // is anchored inside (ui/ContextMenu.qml).
    signal held(int index, real x, real y)

    GridView {
        id: grid
        objectName: "coverTiles"
        anchors { top: parent.top; left: parent.left; right: parent.right }
        // Whole rows only. The remainder is left blank rather than showing a
        // half row, which is what would invite a swipe (PLAN §12.1).
        height: tiles.rowsPerPage * grid.cellHeight
        clip: true

        // Nothing flicks, nothing scrolls, nothing is thrown.
        interactive: false
        // No off-screen delegates: a tile that is not on this page must not
        // hold a decoded cover (PLAN §6 M3) or ask for one.
        cacheBuffer: 0

        cellWidth: Math.floor(width / tiles.columns)
        cellHeight: Math.floor(cellWidth * Style.coverRatio) + tiles.captionHeight

        // Positioned, never scrolled: the window moves a whole number of rows
        // at a time, so a page turn can never leave a half row at the top.
        contentY: Math.floor(tiles.firstIndex / tiles.columns) * grid.cellHeight

        delegate: Item {
            id: cell
            width: grid.cellWidth
            height: grid.cellHeight

            readonly property string coverPath: tiles.roleOf(model.coverPath)
            readonly property string caption: tiles.roleOf(model.title)
            readonly property string badge: tiles.badges ? tiles.roleOf(model.badge) : ""
            // roleOf guards this the same way as the rest: most sources have
            // no authors to give at all, so the role may be entirely absent
            // from this model rather than present-and-empty (see roleOf).
            readonly property string subtitle: tiles.roleOf(model.authors)

            Rectangle {
                id: tile
                objectName: "coverTile"
                anchors {
                    left: parent.left; right: parent.right
                    top: parent.top
                    margins: Style.gap / 2
                }
                height: grid.cellHeight - tiles.captionHeight
                // Darkened once a press has lasted long enough to be worth
                // acknowledging (ui/HoldArea.qml). The tile rather than the
                // caption, because the tile is what the finger is on and what
                // is big enough to see change on a slow panel.
                color: tileArea.feedback ? Style.pressed : Style.panel
                // The border carries the same feedback as the fill, and it is
                // the half that does the work: a tile showing a cover is
                // covered by its image, so a darkened background under it
                // would be invisible on exactly the tiles that have one.
                border.width: tileArea.feedback ? 4 : 1
                border.color: tileArea.feedback ? Style.ink : Style.rule

                // The picture and its placeholder, shared with the list rows
                // since they grew a thumbnail (ui/CoverArt.qml). The
                // placeholder **carries the title**: a tile reading "No cover"
                // tells the user nothing about which book it is, and on
                // Downloaded that is most of the screen — the covers are only
                // fetched for series a source still publishes, and a library
                // full of older downloads has none. The caption below elides to
                // two lines; five here do not, so the long titles that elide
                // are exactly the ones this rescues.
                CoverArt {
                    anchors.fill: parent
                    coverPath: cell.coverPath
                    caption: cell.caption
                }

                // Watching's badge, in the corner rather than at the end of a
                // row. It survives a failed check on purpose: the count was
                // established by a check that did succeed and those chapters
                // really are unread (PLAN §12.2).
                Rectangle {
                    objectName: "coverBadge"
                    anchors {
                        right: parent.right; top: parent.top
                        margins: Style.gap / 2
                    }
                    width: visible ? badgeText.width + Style.gap : 0
                    height: visible ? badgeText.height + Style.gap : 0
                    // The same badge as the list row's (ui/WatchList.qml) and
                    // so the same solid accent fill: the two layouts are one
                    // badge drawn twice, and a tile whose badge were black
                    // would read as a different kind of thing from the row it
                    // replaces.
                    //
                    // A filled block is also what this corner needs most. It
                    // sits on a cover rather than on paper, so an outlined
                    // badge had to compete with whatever art was under it;
                    // solid colour cuts a hole in the picture.
                    color: Style.accent
                    border.width: 0
                    radius: 6
                    visible: cell.badge.length > 0

                    Text {
                        id: badgeText
                        objectName: "coverBadgeText"
                        anchors.centerIn: parent
                        text: cell.badge
                        font.pointSize: Style.smallSize
                        // Black on the accent: 5.90:1 (ui/Style.js).
                        color: Style.ink
                    }
                }
            }

            Text {
                id: caption
                objectName: "coverCaption"
                anchors {
                    top: tile.bottom; topMargin: 6
                    left: parent.left; right: parent.right
                    leftMargin: Style.gap / 2; rightMargin: Style.gap / 2
                }
                text: cell.caption
                font.pointSize: Style.smallSize
                color: Style.ink
                elide: Text.ElideRight
                // Never cut to one line to make room for the subtitle below —
                // a truncated title is a worse trade than a shorter page.
                maximumLineCount: 2
                wrapMode: Text.WordWrap
            }

            // The authors, when the listing has any and the tile has room
            // reserved for them (tiles.hasSubtitles). Invisible and
            // zero-height rather than just invisible when there is nothing to
            // show, so a manga listing's geometry is byte-for-byte what it
            // always was — see tiles.captionHeight.
            Text {
                objectName: "coverSubtitle"
                anchors {
                    top: caption.bottom
                    left: parent.left; right: parent.right
                    leftMargin: Style.gap / 2; rightMargin: Style.gap / 2
                }
                text: cell.subtitle
                font.pointSize: Style.smallSize
                color: Style.muted
                elide: Text.ElideRight
                maximumLineCount: 1
                visible: tiles.hasSubtitles
                height: visible ? implicitHeight : 0
            }

            // One area for both gestures, so a tile cannot end up opening the
            // series *and* offering a menu about it: HoldArea emits `tapped`
            // only for a press that was not held.
            HoldArea {
                id: tileArea
                objectName: "coverTileArea"
                anchors.fill: parent
                onTapped: tiles.tapped(index)
                onHeld: {
                    var at = tileArea.mapToItem(tiles, tileArea.pressX, tileArea.pressY)
                    tiles.held(index, at.x, at.y)
                }
            }
        }
    }
}
