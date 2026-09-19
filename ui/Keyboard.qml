// An on-screen keyboard.
//
// PLAN §11 Q5: the device offers an embedded app no system keyboard. AppLoad
// v0.4.2 — the version this device ran — had none; the keyboard arrived in
// v0.5.x, which does not run on OS 3.25.1.1. **Annex, the host Quire runs under
// now, has none either**, which is why this file came back after being deleted
// for v0.5.x's panel: without it the three text fields are untypeable and the
// app is unusable.
//
// It stays cheap to remove. Nothing else in ui/ knows this file exists beyond
// the three elements that instantiate it, and it talks to its owner through
// four signals and nothing else. If Quire ever runs on a host that provides a
// keyboard, delete this file, drop the instantiations, and let the fields take
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

            // The space bar, in the text layout only.
            //
            // It used to type "." in the URL layout, which put a second full
            // stop on a keyboard that already has one in its fourth row — so
            // an address had two keys that did the same thing and a space bar
            // that silently was not one. A URL has no spaces in it, so the
            // honest answer is that the URL layout has no space bar, and the
            // room goes to the two keys below that actually save typing.
            Rectangle {
                objectName: "keyboardSpace"
                width: 300
                height: 84
                color: spaceArea.pressed ? Style.pressed : Style.paper
                border.width: 1
                border.color: Style.rule
                radius: 4
                visible: keyboard.layout !== "url"

                Text {
                    anchors.centerIn: parent
                    text: "Space"
                    font.pointSize: Style.smallSize
                    color: Style.ink
                }

                MouseArea {
                    id: spaceArea
                    anchors.fill: parent
                    onClicked: keyboard.keyTyped(" ")
                }
            }

            // Whole suffixes, in the URL layout only. Eight taps each on a
            // panel that redraws between them, and there is no ambiguity about
            // what an address ends in.
            Repeater {
                model: keyboard.layout === "url" ? [".com", ".org"] : []

                delegate: Rectangle {
                    objectName: "keyboardSuffix"
                    property string suffix: modelData
                    width: 170
                    height: 84
                    color: suffixArea.pressed ? Style.pressed : Style.paper
                    border.width: 1
                    border.color: Style.rule
                    radius: 4

                    Text {
                        anchors.centerIn: parent
                        text: parent.suffix
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: suffixArea
                        anchors.fill: parent
                        onClicked: keyboard.keyTyped(parent.suffix)
                    }
                }
            }

            Rectangle {
                id: backKey
                objectName: "keyboardBackspace"
                width: 150
                height: 84
                color: backArea.pressed ? Style.pressed : Style.paper
                border.width: 1
                border.color: Style.rule
                radius: 4

                // Drawn, not typed.
                //
                // This key used to be U+232B ERASE TO THE LEFT and rendered as
                // a tofu box on the device. The reMarkable ships Noto Sans,
                // Noto Serif, NotoSansUI and Noto Mono and nothing else, and
                // U+232B is in Noto Sans *Symbols*, which is not installed —
                // verified against the cmaps of the fonts actually on the
                // device, not inferred. The same hole swallows U+21E7 ⇧,
                // U+23CE ⏎, U+2423 ␣ and U+2326 ⌦, so reaching for another
                // symbol would have been the same bug with a different
                // codepoint.
                //
                // A drawing has no font dependency to get wrong, and scales
                // with the key. It repaints only when the key resizes, which
                // on a fixed panel is never.
                Canvas {
                    id: backGlyph
                    anchors.centerIn: parent
                    width: 52
                    height: 34
                    onWidthChanged: backGlyph.requestPaint()
                    onHeightChanged: backGlyph.requestPaint()

                    onPaint: {
                        var ctx = backGlyph.getContext("2d")
                        ctx.reset()
                        ctx.strokeStyle = Style.ink
                        ctx.lineWidth = 3
                        ctx.lineJoin = "round"
                        ctx.lineCap = "round"

                        var w = backGlyph.width
                        var h = backGlyph.height
                        var mid = h / 2
                        var tip = h * 0.55   // where the arrowhead meets the body

                        // The outline: a rectangle with a point on its left.
                        ctx.beginPath()
                        ctx.moveTo(2, mid)
                        ctx.lineTo(tip, 2)
                        ctx.lineTo(w - 2, 2)
                        ctx.lineTo(w - 2, h - 2)
                        ctx.lineTo(tip, h - 2)
                        ctx.closePath()
                        ctx.stroke()

                        // The cross inside it.
                        var x0 = tip + 9
                        var x1 = w - 9
                        var y0 = mid - 6
                        var y1 = mid + 6
                        ctx.beginPath()
                        ctx.moveTo(x0, y0)
                        ctx.lineTo(x1, y1)
                        ctx.moveTo(x1, y0)
                        ctx.lineTo(x0, y1)
                        ctx.stroke()
                    }
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
