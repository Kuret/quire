// The search field, shared by the screens that have one.
//
// It was SeriesGrid's own until the combined search across every source
// wanted the same control (Msg.SearchAll). Copying it into the second screen
// was rejected for the reason CoverGrid was extracted rather than duplicated:
// the interesting behaviour here is not the box, it is **Clear** — a control
// whose whole design is about which of the two states around it survive a tap
// — and two copies is two places for that to drift apart. The next screen that
// searches gets the same one.
//
// What it deliberately does not own:
//
//   - **The query.** `query` is shown, never written. The screen holds the
//     string because the screen is what the keyboard types into and what sends
//     it to the backend; a field that edited its own copy would be a second
//     answer to "what is being searched for".
//   - **What Clear means beyond emptying the box.** It reports `cleared` and
//     the screen decides; the note on that signal says what every caller is
//     expected to do and why.
//   - **A button beside it.** SeriesGrid puts "Latest" to its right and the
//     combined search has nothing there, so the anchoring is the caller's.

import QtQuick 2.5
import "Style.js" as Style

Rectangle {
    id: bar

    // What is being searched for, shown as it arrived from the screen.
    property string query: ""

    // Whether the keyboard is up. It is the screen's state — ui/Keyboard.qml
    // belongs to the screen, not to this box — and is read here only to decide
    // whether the placeholder is worth showing.
    property bool searching: false

    // The grey line on an empty field. It names the *scope* of the search,
    // which is the one thing the two screens using this differ on.
    property string placeholder: ""

    // The field itself, for Screens.dismissKeyboard: focus is dropped on the
    // real item, and the caller is the only thing that knows which fields it
    // meant (Screens.js).
    property alias field: queryField

    // The bar was tapped: the screen raises its keyboard. `searching` is the
    // whole of "the keyboard is up", so this and the screen's dismissInput are
    // the only two things that move it.
    signal raised()

    // Clear was tapped. The screen empties the query **and keeps the keyboard
    // up**: Clear was asked for as a way to start a *new* search without
    // holding backspace down, so clearing is the beginning of typing, not the
    // end of it. Routing it through dismissInput would cost the user a tap to
    // get the keyboard back — more taps than the backspacing they asked to be
    // rid of. The results on screen stay until a new search runs, for the same
    // reason: blanking them on the way to typing takes away what the user may
    // be comparing against.
    signal cleared()

    height: Style.buttonHeight
    color: Style.paper
    border.width: 2
    border.color: Style.ink
    radius: 6

    // A display, not an input: the device has no system keyboard available to
    // an embedded app (PLAN §11 Q5) — Annex supplies none — so nothing would
    // raise one for a focused field. What this shows is what ui/Keyboard.qml
    // has typed into the screen's `query`.
    Text {
        id: queryField
        objectName: "searchField"
        anchors {
            left: parent.left; leftMargin: Style.gap
            // Stops where Clear starts, so the two never overlap and a long
            // query cannot run underneath the button.
            right: clearButton.visible ? clearButton.left : parent.right
            rightMargin: Style.gap
            verticalCenter: parent.verticalCenter
        }
        clip: true
        elide: Text.ElideRight
        text: bar.query
        font.pointSize: Style.bodySize
        color: Style.ink
    }

    MouseArea {
        objectName: "searchBarArea"
        anchors.fill: parent
        onClicked: bar.raised()
    }

    // The placeholder is its own item rather than a fallback string in
    // queryField, so that what the field *shows* is never anything but the
    // query.
    Text {
        objectName: "searchPlaceholder"
        anchors {
            left: parent.left; leftMargin: Style.gap
            right: parent.right; rightMargin: Style.gap
            verticalCenter: parent.verticalCenter
        }
        elide: Text.ElideRight
        text: bar.placeholder
        font.pointSize: Style.bodySize
        color: Style.rule
        visible: bar.query.length === 0 && !bar.searching
    }

    // A word, not a glyph: the rest of the app labels its controls in words
    // ("Latest", "Keep", "Save"), and the device's four fonts have already
    // cost this project one tofu box.
    Rectangle {
        id: clearButton
        objectName: "clearSearchButton"
        anchors {
            right: parent.right; rightMargin: Style.gap / 2
            verticalCenter: parent.verticalCenter
        }
        // Only when there is something to clear. A control that is always lit
        // on an empty field is one people learn to ignore.
        visible: bar.query.length > 0
        width: 150
        height: Style.buttonHeight - 8
        color: clearArea.pressed ? Style.pressed : Style.paper
        border.width: 2
        border.color: Style.rule
        radius: 6

        Text {
            anchors.centerIn: parent
            text: "Clear"
            font.pointSize: Style.smallSize
            color: Style.ink
        }

        MouseArea {
            id: clearArea
            objectName: "clearSearchArea"
            anchors.fill: parent
            onClicked: bar.cleared()
        }
    }
}
