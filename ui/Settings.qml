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

    // The log, most recent last, as the backend sent it. Empty until asked for.
    property var logLines: []
    property bool logShown: false

    signal pingRequested()
    signal logRequested()
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

        // The log viewer — PLAN §6 M7's "SSH-free debugging".
        //
        // Every bug found so far needed someone reading journalctl over SSH,
        // which the person holding the tablet cannot do. This turns "it stopped
        // working" into something they can read out.
        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Log"
            font.pointSize: Style.headingSize
            color: Style.ink
        }

        Rectangle {
            objectName: "showLogButton"
            width: parent.width
            height: Style.buttonHeight
            color: logArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: screen.logShown ? "Hide the log" : "Show the log"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: logArea
                anchors.fill: parent
                onClicked: {
                    screen.logShown = !screen.logShown
                    if (screen.logShown)
                        screen.logRequested()
                }
            }
        }

        Rectangle {
            width: parent.width
            height: screen.logShown ? 520 : 0
            visible: screen.logShown
            color: Style.paper
            border.width: 1
            border.color: Style.rule

            ListView {
                id: logView
                objectName: "logView"
                anchors { fill: parent; margins: Style.gap }
                clip: true
                boundsBehavior: Flickable.StopAtBounds
                flickDeceleration: 10000
                maximumFlickVelocity: 1600
                model: screen.logLines

                // The newest lines are the ones anyone diagnosing a problem
                // wants, so start at the bottom.
                onCountChanged: positionViewAtEnd()

                delegate: Text {
                    width: logView.width
                    wrapMode: Text.WrapAnywhere
                    text: modelData
                    font.pointSize: Style.smallSize
                    font.family: "monospace"
                    color: Style.muted
                }
            }

            Text {
                anchors.centerIn: parent
                text: "Nothing logged yet."
                font.pointSize: Style.smallSize
                color: Style.muted
                visible: logView.count === 0
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
