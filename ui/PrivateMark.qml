// The private-sources button's own mark: a small eye-like glyph, drawn rather
// than typed for the reason every glyph in this app is (see the Canvas in
// ui/Keyboard.qml's backspace key) — the device's four Noto fonts do not cover
// the symbol blocks, and a drawing has no font to be missing from.
//
// It is deliberately not any browser's private-window icon. The brief asks for
// "an eye-like mark, the convention browsers use for private windows", but
// copying one would be copying a specific product's mark, not the convention —
// so this is its own drawing: an almond outline with a round pupil, the plainest
// possible eye and nothing more. It has to be unremarkable on the sources
// screen, not a signpost, so it carries no colour of its own and no label ever
// rides beside it.
import QtQuick 2.5
import "Style.js" as Style

Canvas {
    id: mark
    width: 40
    height: 24

    // Muted, like the rest of the chrome that is not the one accented line on
    // a screen (ui/Style.js) — this button is meant to be found by someone
    // looking for it, not by everyone glancing at the screen.
    property color inkColor: Style.muted

    onWidthChanged: mark.requestPaint()
    onHeightChanged: mark.requestPaint()
    onInkColorChanged: mark.requestPaint()

    onPaint: {
        var ctx = mark.getContext("2d")
        ctx.reset()
        ctx.strokeStyle = mark.inkColor
        ctx.fillStyle = mark.inkColor
        ctx.lineWidth = 2
        ctx.lineJoin = "round"
        ctx.lineCap = "round"

        var w = mark.width
        var h = mark.height
        var cx = w / 2
        var cy = h / 2
        var left = 2
        var right = w - 2

        // The almond: two arcs from corner to corner, one bowing up and one
        // bowing down, met at the outer corners — the plainest possible eye
        // outline.
        ctx.beginPath()
        ctx.moveTo(left, cy)
        ctx.quadraticCurveTo(cx, 2, right, cy)
        ctx.quadraticCurveTo(cx, h - 2, left, cy)
        ctx.closePath()
        ctx.stroke()

        // The pupil, filled and small enough to leave the almond doing the
        // work of reading as an eye rather than as a filled-in shape.
        var r = h * 0.16
        ctx.beginPath()
        ctx.arc(cx, cy, r, 0, Math.PI * 2)
        ctx.fill()
    }
}
