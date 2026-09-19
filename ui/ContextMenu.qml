// The long-press menu, shared by search, Downloaded and Watching.
//
// One component for all three screens and for **both layouts of each**. That
// equivalence is the whole point of it: the grid/list switch (PLAN §7.1 type
// 75) is a layout choice, and a layout choice must never be a choice about
// what the user can do. Before this, deleting a series and dropping a watch
// existed only in the list layout, so anyone who preferred covers had quietly
// given them up.
//
// What it is not:
//
//   - **Not a popup window.** It is an Item filling its screen, with the
//     dismiss target underneath and the panel on top. A QtQuick Popup needs
//     QtQuick.Controls, which is not imported anywhere in this app and would
//     arrive with its own styling on a device we match by hand.
//   - **Not scrollable.** PLAN §12.1: nothing here scrolls. The panel is as
//     tall as its items, the longest set is four, and four fit. A menu that
//     could scroll would be a menu that could hide the item you are looking
//     for behind a gesture this app does not have.
//   - **Not a second confirmation.** Destructive items hand back to the
//     confirm each screen already has. The deliberate hold is what the menu
//     contributes; it does not make deleting safe.
//
// The items are plain JS objects — {action, label, enabled} — built by the
// screen, because what a row can do is the screen's knowledge: which ids it
// carries, whether it has a document to open, whether it has new chapters.
// This file decides only where the panel goes and what a tap on a line means.

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: menu

    // Whether anything is showing. Nothing is drawn when it is not: the
    // dismiss target is disabled with it, so a closed menu cannot swallow the
    // taps meant for the page underneath.
    property bool opened: false

    // The lines, in order. Empty while closed so no delegate survives a
    // dismissal holding the previous row's ids.
    property var items: []

    // Where the finger was, in this item's coordinates. The panel is anchored
    // near it and then clamped — see below.
    property real wantX: 0
    property real wantY: 0

    // Wide enough for the longest label the three screens use ("Delete
    // everything from this series") at Style.smallSize, which is what a menu
    // is allowed to be as wide as: a panel sized to its content would change
    // width between rows of the same list.
    readonly property int panelWidth: 560
    readonly property int itemHeight: Style.rowHeight

    signal chosen(string action)
    signal dismissed()

    // show opens the menu for one row. An empty set opens nothing rather than
    // an empty box — a screen that finds a row can do nothing at all says so
    // by offering no menu.
    function show(list, x, y) {
        menu.items = list ? list : []
        menu.wantX = x
        menu.wantY = y
        menu.opened = menu.items.length > 0
    }

    function close() {
        menu.opened = false
        menu.items = []
    }

    visible: menu.opened

    // Tap anywhere else to dismiss. It covers the whole screen, including the
    // pager and the header's own controls, which is deliberate: the first tap
    // after a menu opens puts it away, whatever it lands on. Having it also
    // reach the control underneath would mean the menu could be dismissed
    // *into* a page turn.
    MouseArea {
        id: scrim
        objectName: "contextMenuScrim"
        anchors.fill: parent
        enabled: menu.opened
        onClicked: {
            menu.close()
            menu.dismissed()
        }
    }

    Rectangle {
        id: panel
        objectName: "contextMenuPanel"
        width: menu.panelWidth
        // Counted rather than measured from the Column. A positioner settles
        // its height on the next layout pass, and the clamping below has to be
        // right on the *first* frame: the panel is drawn where it is put, once,
        // and a second position a frame later is a ghost on this panel.
        height: menu.items.length * menu.itemHeight + Style.gap

        // Anchored near the press, then clamped inside the screen. Math.max
        // last, so a menu too tall or too wide for the space left lands at the
        // margin rather than off the top: on a screen with no scrolling, an
        // item that is off the edge is an item that does not exist.
        x: Math.max(Style.margin,
                    Math.min(menu.wantX, menu.width - panel.width - Style.margin))
        y: Math.max(Style.margin,
                    Math.min(menu.wantY, menu.height - panel.height - Style.margin))

        color: Style.paper
        border.width: 2
        border.color: Style.ink
        radius: 6

        Column {
            id: column
            objectName: "contextMenuItems"
            anchors {
                top: parent.top; topMargin: Style.gap / 2
                left: parent.left; right: parent.right
            }

            Repeater {
                model: menu.items

                delegate: Item {
                    objectName: "contextMenuItem"
                    width: column.width
                    height: menu.itemHeight

                    // Read by the offscreen harness, and by nothing else: the
                    // screens address items by the action they carry.
                    readonly property string action: modelData.action
                    readonly property bool live: modelData.enabled !== false

                    Rectangle {
                        anchors.fill: parent
                        anchors.margins: 2
                        color: itemArea.feedbackColor
                    }

                    Text {
                        objectName: "contextMenuLabel"
                        anchors {
                            left: parent.left; leftMargin: Style.margin
                            right: parent.right; rightMargin: Style.margin
                            verticalCenter: parent.verticalCenter
                        }
                        elide: Text.ElideRight
                        text: modelData.label
                        font.pointSize: Style.bodySize
                        // An item that cannot be used is drawn quietly rather
                        // than left out, so that the set of things a row can
                        // do does not change shape under the finger: "Download
                        // new chapters" greyed out on a series with none says
                        // more than its absence would.
                        color: parent.live ? Style.ink : Style.rule
                    }

                    MouseArea {
                        id: itemArea
                        objectName: "contextMenuItemArea"
                        anchors.fill: parent
                        enabled: parent.live
                        readonly property color feedbackColor:
                            itemArea.pressed ? Style.pressed : Style.paper
                        onClicked: {
                            // Read first and closed **last**: close() empties
                            // the model, which takes this delegate — and the
                            // scope these statements are running in — with it.
                            var action = modelData.action
                            menu.chosen(action)
                            menu.close()
                        }
                    }

                    // A hairline between lines, not around them: the panel has
                    // the border.
                    Rectangle {
                        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                        height: Style.hairline
                        color: Style.rule
                        visible: index < menu.items.length - 1
                    }
                }
            }
        }
    }
}
