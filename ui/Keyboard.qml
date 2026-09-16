// An on-screen keyboard.
//
// **This file exists to be deleted.** PLAN §11 Q5 is answered: AppLoad v0.4.2 —
// the version this device runs — has no virtual keyboard; the string does not
// appear in the binary, and the keyboard arrived in v0.5.x, which does not run
// on OS 3.25.1.1. Nothing else in ui/ knows this file exists beyond the one
// element that instantiates it, and it talks to its owner through four signals
// and nothing else. If Quire ever runs on an OS/AppLoad pair that provides a
// keyboard, delete this file, drop the instantiation, and let the field take
// focus.
//
// Deliberately not implemented: autocomplete, autocorrect, key repeat, popups
// on long-press, an animated pressed state. E-ink ghosts, so a key that redraws
// while held is a smear; and every one of those features is a place for logic
// to accumulate in QML, which PLAN §2 says is the layer that breaks on an OS
// update.

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: keyboard

    // "url" gives the layout the punctuation an address needs and no space bar
    // worth the name; "text" is for a search box.
    property string layout: "text"

    // Shift applies to the next character only, which is all a URL or a search
    // query ever needs.
    property bool shifted: false

    // The argument is what to insert, which since the .com and .org keys is not
    // always one character.
    signal keyTyped(string text)
    signal backspace()
    signal clearAll()
    signal submit()

    height: rows.height + Style.gap * 2

    Rectangle {
        anchors.fill: parent
        color: Style.panel

        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: Style.hairline
            color: Style.rule
        }
    }

    // Each row is a string; one character per key. The layouts are plain
    // QWERTY, because a keyboard that rearranges itself to be clever is a
    // keyboard nobody can touch-type on.
    function rowsFor(which) {
        if (which === "url") {
            return ["1234567890", "qwertyuiop", "asdfghjkl", "zxcvbnm.-/", ":_~?=&%"]
        }
        return ["1234567890", "qwertyuiop", "asdfghjkl", "zxcvbnm", ",.'-!?"]
    }

    Column {
        id: rows
        anchors {
            left: parent.left; leftMargin: Style.gap
            right: parent.right; rightMargin: Style.gap
            top: parent.top; topMargin: Style.gap
        }
        spacing: 6

        Repeater {
            model: keyboard.rowsFor(keyboard.layout)

            delegate: Row {
                property string characters: modelData
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: 6

                Repeater {
                    model: characters.length

                    delegate: Rectangle {
                        property string character: characters.charAt(index)
                        width: Math.min(96, (rows.width - 6 * 11) / 10)
                        height: 84
                        color: keyArea.pressed ? Style.pressed : Style.paper
                        border.width: 1
                        border.color: Style.rule
                        radius: 4

                        Text {
                            anchors.centerIn: parent
                            text: keyboard.shifted ? character.toUpperCase() : character
                            font.pointSize: Style.bodySize
                            color: Style.ink
                        }

                        MouseArea {
                            id: keyArea
                            anchors.fill: parent
                            onClicked: {
                                keyboard.keyTyped(keyboard.shifted ? character.toUpperCase() : character)
                                keyboard.shifted = false
                            }
                        }
                    }
                }
            }
        }

        // The control row: shift, space, backspace, clear, done.
        Row {
            anchors.horizontalCenter: parent.horizontalCenter
            spacing: 6

            Rectangle {
                width: 150
                height: 84
                color: shiftArea.pressed || keyboard.shifted ? Style.pressed : Style.paper
                border.width: 1
                border.color: Style.rule
                radius: 4

                Text {
                    anchors.centerIn: parent
                    text: "Shift"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: shiftArea
                    anchors.fill: parent
                    onClicked: keyboard.shifted = !keyboard.shifted
                }
            }

            Rectangle {
                width: keyboard.layout === "url" ? 150 : 300
                height: 84
                color: spaceArea.pressed ? Style.pressed : Style.paper
                border.width: 1
                border.color: Style.rule
                radius: 4

                Text {
                    anchors.centerIn: parent
                    // A URL has no spaces in it, so that key types the one
                    // character a typed address actually needs.
                    text: keyboard.layout === "url" ? "." : "Space"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: spaceArea
                    anchors.fill: parent
                    onClicked: keyboard.keyTyped(keyboard.layout === "url" ? "." : " ")
                }
            }

            Rectangle {
                width: 150
                height: 84
                color: backArea.pressed ? Style.pressed : Style.paper
                border.width: 1
                border.color: Style.rule
                radius: 4

                Text {
                    anchors.centerIn: parent
                    text: "⌫"
                    font.pointSize: Style.bodySize
                    color: Style.ink
                }

                MouseArea {
                    id: backArea
                    anchors.fill: parent
                    // No key repeat: holding a key on e-ink would repaint the
                    // field faster than the panel can settle.
                    onClicked: keyboard.backspace()
                    onPressAndHold: keyboard.clearAll()
                }
            }

            Rectangle {
                objectName: "keyboardDone"
                width: 180
                height: 84
                color: doneArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 4

                Text {
                    anchors.centerIn: parent
                    text: "Done"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: doneArea
                    anchors.fill: parent
                    onClicked: keyboard.submit()
                }
            }
        }
    }
}
