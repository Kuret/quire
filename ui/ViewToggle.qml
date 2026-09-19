// The grid/list switch in the header — PLAN §7.1 type 75.
//
// **Two halves, not one button that flips.** A single control labelled with the
// layout it would switch *to* reads as the layout you are already in about half
// the time, and this panel has no hover state and no tooltip to settle the
// argument. Both words are on screen always and the one you are in is the
// filled one, so the control answers "which am I in?" before it is touched.
//
// Words rather than glyphs, like every other control in Quire. The obvious
// icons here (U+229E, U+2630) are in neither Noto Sans nor NotoSansUI, which is
// the same hole that put a tofu box where the backspace key should have been —
// build/qml-check.sh audits for it now.
//
// It flips nothing itself. Tapping asks for a *named* layout, the backend
// stores it, and the value comes back on the Pong status (Views.js). So a write
// that failed cannot leave the switch showing a layout nothing was saved for,
// and a stale switch cannot ask for the wrong thing — "list" means list, where
// "the other one" would have meant whatever this half of the app last guessed.

import QtQuick 2.5
import "Style.js" as Style
import "Views.js" as Views

Item {
    id: toggle

    // The layout now stored for the screen in the header. Grid until a status
    // says otherwise, so the control draws before the first Pong arrives.
    property string view: Views.GRID

    signal viewRequested(string view)

    width: halves.width
    height: Style.buttonHeight

    Row {
        id: halves
        anchors.verticalCenter: parent.verticalCenter
        spacing: 0

        // A Repeater over the two layouts rather than two near-identical
        // Rectangles: the halves differ in one word and one value, and a copy
        // is how the second one ends up a few pixels shorter than the first.
        Repeater {
            model: [{"view": Views.GRID, "label": "Grid"},
                    {"view": Views.LIST, "label": "List"}]

            delegate: Rectangle {
                id: half
                objectName: "viewToggle" + modelData.label

                // True when this half names the layout already showing.
                readonly property bool current: toggle.view === modelData.view

                width: 140
                height: Style.buttonHeight
                // The half you are in is the filled one, as it always was —
                // the fill is simply the accent now instead of the grey panel.
                // That keeps the signal where it was and puts the colour on
                // the one shape this panel draws cleanly, a solid block
                // (ui/Style.js).
                //
                // Strip the colour out and this control still answers "which
                // am I in?" exactly as well as it did: the accent's luminance
                // is a quarter of paper's, so the active half is still
                // visibly the dark one, and it is still the inert one.
                color: halfArea.pressed ? Style.pressed
                                        : (half.current ? Style.accent : Style.paper)
                border.width: 2
                // Both halves keep the same grey outline. A coloured 2px
                // border is a thin coloured stroke, which is the shape the
                // measurement in ui/Style.js rules out.
                border.color: Style.rule
                radius: 6

                Text {
                    anchors.centerIn: parent
                    objectName: "viewToggleLabel" + modelData.label
                    text: modelData.label
                    font.pointSize: Style.smallSize
                    // Black on the accent fill: 5.90:1, better than the grey
                    // it replaces and better than the accent-on-panel text it
                    // replaces. The inactive half stays grey on paper, so the
                    // two halves still differ in type colour as well as fill.
                    color: half.current ? Style.ink : Style.muted
                }

                MouseArea {
                    id: halfArea
                    objectName: "viewToggle" + modelData.label + "Area"
                    anchors.fill: parent
                    // The half you are already in is inert. Asking for the
                    // layout already stored would write a setting and ping for
                    // it, and the panel would redraw the screen underneath for
                    // a change that did not happen.
                    enabled: !half.current
                    onClicked: toggle.viewRequested(modelData.view)
                }
            }
        }
    }
}
