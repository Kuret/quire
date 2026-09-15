// The list of configured sources — PLAN §6 M3: "Source list with per-source
// enable toggle and last-probe status."
//
// The status line is written by the backend in plain language (see
// service.VerdictHeadline); this file never maps a verdict to words, because
// then there would be two places that do it and one of them would drift.
//
// PLAN §1.3: Quire ships with no sources, so the empty state is the *normal*
// first screen and says what to do rather than looking broken.

import QtQuick 2.5
import "Style.js" as Style

Item {
    id: screen

    property alias model: list.model

    signal addRequested()
    signal openRequested(string sourceId, string name)
    signal toggleRequested(string sourceId, bool enabled)
    signal removeRequested(string sourceId)

    // Which row has its confirm-remove strip open. Removing a source is one tap
    // away from a full library of downloads still being there but nothing to
    // update them from, so it asks first.
    property string confirmingId: ""

    ListView {
        id: list
        anchors { top: parent.top; left: parent.left; right: parent.right; bottom: addBar.top }
        clip: true
        // No flick animation: a kinetic scroll on e-ink is a column of ghosts.
        boundsBehavior: Flickable.StopAtBounds
        flickDeceleration: 10000
        maximumFlickVelocity: 1600

        delegate: Item {
            width: list.width
            height: Style.rowHeight + (screen.confirmingId === model.sourceId ? Style.buttonHeight : 0)

            Item {
                id: row
                width: parent.width
                height: Style.rowHeight

                Column {
                    anchors {
                        left: parent.left; leftMargin: Style.margin
                        right: toggle.left; rightMargin: Style.gap
                        verticalCenter: parent.verticalCenter
                    }
                    spacing: 4

                    Text {
                        width: parent.width
                        elide: Text.ElideRight
                        text: model.name
                        font.pointSize: Style.bodySize
                        color: model.enabled ? Style.ink : Style.muted
                    }
                    Text {
                        width: parent.width
                        elide: Text.ElideRight
                        text: model.status + " · " + model.baseUrl
                        font.pointSize: Style.smallSize
                        color: Style.muted
                    }
                }

                MouseArea {
                    anchors { left: parent.left; top: parent.top; bottom: parent.bottom; right: toggle.left }
                    enabled: model.enabled
                    onClicked: screen.openRequested(model.sourceId, model.name)
                    onPressAndHold: screen.confirmingId =
                        screen.confirmingId === model.sourceId ? "" : model.sourceId
                }

                // The per-source toggle. A checkbox rather than a switch: a
                // switch wants an animation to read as one.
                Item {
                    id: toggle
                    anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
                    width: 120
                    height: Style.buttonHeight

                    Rectangle {
                        anchors.fill: parent
                        color: toggleArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: model.enabled ? "On" : "Off"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }
                    }

                    MouseArea {
                        id: toggleArea
                        anchors.fill: parent
                        onClicked: screen.toggleRequested(model.sourceId, !model.enabled)
                    }
                }
            }

            // The confirm strip, opened by holding a row.
            Item {
                anchors { top: row.bottom; left: parent.left; right: parent.right }
                height: screen.confirmingId === model.sourceId ? Style.buttonHeight : 0
                visible: height > 0

                Text {
                    anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
                    text: "Remove this source? Downloaded volumes stay in your library."
                    font.pointSize: Style.smallSize
                    color: Style.muted
                }

                Rectangle {
                    anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
                    width: 160
                    height: Style.buttonHeight - Style.gap
                    color: removeArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: Style.ink
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Remove"
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: removeArea
                        anchors.fill: parent
                        onClicked: {
                            screen.removeRequested(model.sourceId)
                            screen.confirmingId = ""
                        }
                    }
                }
            }

            Rectangle {
                anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                height: Style.hairline
                color: Style.rule
            }
        }
    }

    // The empty state. PLAN §1.3 is the reason it exists and the reason it is
    // worded as an instruction rather than an apology.
    Column {
        anchors.centerIn: list
        width: Math.min(parent.width - Style.margin * 2, 700)
        spacing: Style.gap
        visible: list.count === 0

        Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
            text: "No sources yet"
            font.pointSize: Style.headingSize
            color: Style.ink
        }
        Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
            text: "Quire ships with no sites. Add one by pasting its web address; " +
                  "you are responsible for the sites you choose to use."
            font.pointSize: Style.bodySize
            color: Style.muted
        }
    }

    Item {
        id: addBar
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        height: Style.rowHeight + Style.gap

        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: Style.hairline
            color: Style.rule
        }

        Rectangle {
            objectName: "addSourceButton"
            anchors.centerIn: parent
            width: Math.min(parent.width - Style.margin * 2, 460)
            height: Style.buttonHeight
            color: addArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Add a source"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: addArea
                anchors.fill: parent
                onClicked: screen.addRequested()
            }
        }
    }
}
