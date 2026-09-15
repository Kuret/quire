// Settings.
//
// There is very little here on purpose. PLAN §7.4's politeness floors are not
// configurable — "the schema bounds what a user may write; this floor bounds
// what Quire actually does" — so there are no rate-limit sliders, and PLAN §7.6
// means there is nothing to toggle about challenges either. What is left is the
// two things a user genuinely needs: proof the backend is alive, and the last
// thing that went wrong.

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: screen

    property string backendStatus: ""
    property string lastError: ""

    signal pingRequested()
    signal clearErrorRequested()

    Column {
        anchors {
            top: parent.top; topMargin: Style.margin
            left: parent.left; leftMargin: Style.margin
            right: parent.right; rightMargin: Style.margin
        }
        spacing: Style.gap

        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Backend"
            font.pointSize: Style.headingSize
            color: Style.ink
        }

        Text {
            objectName: "backendStatus"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.backendStatus
            font.pointSize: Style.bodySize
            color: Style.muted
        }

        Rectangle {
            width: parent.width
            height: Style.buttonHeight
            color: pingArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Check the backend"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: pingArea
                anchors.fill: parent
                onClicked: screen.pingRequested()
            }
        }

        Rectangle {
            width: parent.width
            height: Style.hairline
            color: Style.rule
        }

        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Last problem"
            font.pointSize: Style.headingSize
            color: Style.ink
        }

        Text {
            objectName: "lastError"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.lastError.length > 0 ? screen.lastError : "Nothing has gone wrong."
            font.pointSize: Style.bodySize
            color: Style.muted
        }

        Rectangle {
            width: parent.width
            height: Style.buttonHeight
            color: clearArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.rule
            radius: 6
            visible: screen.lastError.length > 0

            Text {
                anchors.centerIn: parent
                text: "Clear"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: clearArea
                anchors.fill: parent
                onClicked: screen.clearErrorRequested()
            }
        }

        Rectangle {
            width: parent.width
            height: Style.hairline
            color: Style.rule
        }

        // PLAN §1.3 asks the README to state this; a user who never reads the
        // README still adds sources, so it is said here too.
        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Quire ships with no sources and provides no directory of them. " +
                  "Whether a site you add may lawfully be read this way is your call, " +
                  "not Quire's."
            font.pointSize: Style.smallSize
            color: Style.muted
        }
    }
}
