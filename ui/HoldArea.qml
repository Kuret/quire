// A tap target that can also be held — the one place the long press is timed.
//
// Every row and every tile that offers the context menu uses this, because the
// interesting part is not opening a menu: it is the three things that have to
// happen in the right order on a panel that repaints slowly.
//
//   1. **Feedback before the menu.** `feedback` goes true at
//      Style.pressFeedbackDelay and the caller darkens itself with it. Without
//      it a hold looks identical to a tap that did not register, and the user
//      lifts at ~300ms — before the menu — and concludes the app is broken.
//      The darkening is drawn by the caller rather than here, because a tile
//      and a row darken different rectangles.
//   2. **The hold at Style.holdDelay**, from this component's own Timer rather
//      than MouseArea.pressAndHold. Qt's default is 800ms; 5.9 added
//      pressAndHoldInterval, but the feedback step needs a second timer here
//      anyway and one mechanism reading one pair of tokens is easier to
//      reason about than a built-in that is configured in one place and
//      shadowed in another.
//   3. **A held press is not also a tap.** MouseArea emits clicked() on
//      release whether or not pressAndHold already fired, so a long press
//      would otherwise open the menu *and* open the series behind it. `tapped`
//      is therefore this component's own signal, emitted from onClicked only
//      when the hold did not fire — callers connect to `tapped` and never to
//      `clicked`.
//
// `clicked` itself is deliberately left alone rather than suppressed: a signal
// cannot be un-emitted, and a caller that wires onClicked by habit should get
// the plain MouseArea behaviour rather than a subtly filtered one.

import QtQuick 2.5
import "Style.js" as Style

MouseArea {
    id: area

    // The timings, from the tokens. Properties rather than constants so the
    // offscreen harness can drive the machinery without waiting out a real
    // hold, and so a screen could lengthen its own if one ever needs to.
    property int feedbackDelay: Style.pressFeedbackDelay
    property int holdDelay: Style.holdDelay

    // True once the press has lasted long enough to be worth acknowledging.
    property bool feedback: false

    // True once this press has become a hold. Read by onClicked below; it
    // survives the release on purpose, because clicked() arrives after it.
    property bool holdFired: false

    // Where the finger went down, in this area's coordinates. The menu is
    // anchored near the press rather than to the item, so a hold at the bottom
    // of a tall tile does not put the menu half a screen away from the thumb.
    property real pressX: 0
    property real pressY: 0

    signal tapped()
    signal held()

    // beginPress and endPress are the whole state machine, as functions rather
    // than inline handlers, so that the harness can start a real press and let
    // the real timers decide what happens. A test that called held() directly
    // would prove nothing about when it fires.
    function beginPress() {
        area.holdFired = false
        area.feedback = false
        feedbackTimer.restart()
        holdTimer.restart()
    }

    function endPress() {
        feedbackTimer.stop()
        holdTimer.stop()
        area.feedback = false
    }

    onPressed: (mouse) => {
        area.pressX = mouse.x
        area.pressY = mouse.y
        area.beginPress()
    }
    onReleased: area.endPress()
    // A press the scene took away — the screen changed under the finger, or a
    // parent grabbed the grab. Nothing has been held and nothing was tapped.
    onCanceled: {
        area.endPress()
        area.holdFired = false
    }

    onClicked: {
        if (!area.holdFired)
            area.tapped()
    }

    Timer {
        id: feedbackTimer
        interval: area.feedbackDelay
        onTriggered: area.feedback = true
    }

    Timer {
        id: holdTimer
        interval: area.holdDelay
        onTriggered: {
            area.holdFired = true
            // Already true by now — the feedback timer is the shorter of the
            // two — and set again rather than assumed, because a caller may
            // have shortened one of the delays past the other.
            area.feedback = true
            area.held()
            // The darkening ends with the press, not with the menu: an item
            // left dark under an open menu would still be dark behind the
            // *next* one if a dismissal were ever missed, and a stuck press
            // state on e-ink reads as a frozen screen.
        }
    }
}
