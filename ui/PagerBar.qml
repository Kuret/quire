// The page-turn control — PLAN §12.1.
//
// Two buttons and a sentence. Deliberately not here:
//
//   - **Any animation on the turn.** PLAN §6 M3 forbids animation generally and
//     a page turn is exactly where one would be reached for. The whole point of
//     paging on e-ink is that the screen settles and refreshes once.
//   - **Wrapping.** Next at the end is dead, not a jump back to page 1. A
//     control that wraps lies about where the end is, and the end is the one
//     thing a pager exists to make visible.
//   - **A page-number field.** Typing a number would want the keyboard, and the
//     keyboard is the thing that must not move this bar.
//
// It sits at the very bottom of every screen that has one and never moves:
// ui/Keyboard.qml is anchored above it rather than over it, so opening the
// keyboard cannot shift the control out from under a thumb.

import QtQuick 2.5
import "Style.js" as Style
import "Paging.js" as Paging

Item {
    id: bar

    // The page now shown, 1-based.
    property int page: 1

    // 0 means the source has not said how much there is. The label then reads
    // "Page 3" rather than inventing a denominator (PLAN §12.1).
    property int totalPages: 0

    // Whether a next page exists. For a list already in hand this is just
    // "not the last one"; for a lazily fetched listing the backend says.
    property bool hasMore: false

    // A fetch is in flight for pendingPage. Discrete text, not a spinner
    // (PLAN §6 M3), and both buttons go dead until it lands so a second tap
    // cannot queue a second request.
    property bool busy: false
    property int pendingPage: 0

    signal previousRequested()
    signal nextRequested()

    height: Style.rowHeight

    // One page and nothing beyond it needs no control at all. The space is
    // still reserved by the screen above, so nothing moves when it appears.
    visible: bar.totalPages !== 1 || bar.hasMore || bar.busy

    readonly property bool canGoBack: !bar.busy && bar.page > 1
    readonly property bool canGoOn: !bar.busy && bar.hasMore

    Rectangle {
        anchors { left: parent.left; right: parent.right; top: parent.top }
        height: Style.hairline
        color: Style.rule
    }

    Rectangle {
        id: previousButton
        objectName: "pagerPrevious"
        anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
        width: 200
        height: Style.buttonHeight
        color: previousArea.pressed ? Style.pressed : Style.paper
        border.width: 2
        border.color: bar.canGoBack ? Style.ink : Style.rule
        radius: 6

        Text {
            anchors.centerIn: parent
            text: "Previous"
            font.pointSize: Style.smallSize
            color: bar.canGoBack ? Style.ink : Style.rule
        }

        MouseArea {
            id: previousArea
            anchors.fill: parent
            enabled: bar.canGoBack
            onClicked: bar.previousRequested()
        }
    }

    Text {
        objectName: "pagerLabel"
        anchors {
            left: previousButton.right; leftMargin: Style.gap
            right: nextButton.left; rightMargin: Style.gap
            verticalCenter: parent.verticalCenter
        }
        horizontalAlignment: Text.AlignHCenter
        elide: Text.ElideRight
        text: bar.busy ? ("Fetching page " + bar.pendingPage + "…")
                       : Paging.label(bar.page, bar.totalPages)
        font.pointSize: Style.bodySize
        color: Style.muted
    }

    Rectangle {
        id: nextButton
        objectName: "pagerNext"
        anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
        width: 200
        height: Style.buttonHeight
        color: nextArea.pressed ? Style.pressed : Style.paper
        border.width: 2
        border.color: bar.canGoOn ? Style.ink : Style.rule
        radius: 6

        Text {
            anchors.centerIn: parent
            text: "Next"
            font.pointSize: Style.smallSize
            color: bar.canGoOn ? Style.ink : Style.rule
        }

        MouseArea {
            id: nextArea
            anchors.fill: parent
            enabled: bar.canGoOn
            onClicked: bar.nextRequested()
        }
    }
}
