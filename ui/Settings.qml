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
import "Paging.js" as Paging

Item {
    id: screen

    property string backendStatus: ""
    property string lastError: ""

    // PLAN §7.4's one global robots.txt switch, off by default. It is held as a
    // plain bool rather than read off the status object: the status is a fresh
    // object on every Pong, so binding to it would redraw the toggle each time
    // the settings screen pings, and a flash for no change is exactly what an
    // e-ink panel should not do. Assigning the same bool emits no change
    // signal, so an identical status writes nothing.
    property bool consultRobots: false

    // The log, most recent last, as the backend sent it. Empty until asked for.
    property var logLines: []
    property bool logShown: false

    // Which page of the log is shown. It opens at the newest (PLAN §12.1: the
    // log is paged like every other list, and nothing in Quire scrolls).
    property int logPage: 1

    // The newest lines are the ones anyone diagnosing a problem wants, so a
    // fresh log tail opens at the last page rather than the first.
    onLogLinesChanged: screen.logPage = Paging.pageCount(
        screen.logLines ? screen.logLines.length : 0,
        Paging.rowsPerPage(logViewport.height, logPanel.rowHeight))

    signal pingRequested()
    signal logRequested()
    signal clearErrorRequested()
    signal consultRobotsRequested(bool on)

    // toggleRobots asks for the opposite of what is currently stored. It does
    // not flip the property: the backend persists and applies, and the value
    // comes back on the status, so the switch can never end up showing a state
    // the store does not hold.
    function toggleRobots() {
        screen.consultRobotsRequested(!screen.consultRobots)
    }

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

        Rectangle {
            width: parent.width
            height: Style.hairline
            color: Style.rule
        }

        Text {
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Fetching"
            font.pointSize: Style.headingSize
            color: Style.ink
        }

        // The setting says what it does rather than hiding behind a label.
        // PLAN §7.4: RFC 9309 scopes robots.txt to crawlers, and a person
        // searching and tapping is driving every request — so this is off by
        // default and the sentence below is the honest description of that,
        // not an apology for it.
        //
        // The claim about logging is checkable from this very screen, which is
        // the point of making it: the log viewer is a few lines down.
        Text {
            objectName: "robotsExplanation"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.consultRobots
                  ? "On: Quire does not fetch pages a site\u2019s robots.txt asks crawlers not to."
                  : "Off: Quire fetches pages a site\u2019s robots.txt asks crawlers not to. " +
                    "Every such fetch is logged."
            font.pointSize: Style.bodySize
            color: Style.muted
        }

        Rectangle {
            objectName: "robotsToggle"
            width: parent.width
            height: Style.buttonHeight
            color: robotsArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors {
                    left: parent.left; leftMargin: Style.gap
                    verticalCenter: parent.verticalCenter
                }
                text: "Consult robots.txt"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            // A word rather than a switch, for the same reason the source list
            // uses one: a switch wants an animation to read as one.
            Text {
                objectName: "robotsToggleState"
                anchors {
                    right: parent.right; rightMargin: Style.gap
                    verticalCenter: parent.verticalCenter
                }
                text: screen.consultRobots ? "On" : "Off"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: robotsArea
                anchors.fill: parent
                onClicked: screen.toggleRobots()
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

        // The log, paged rather than scrolled (PLAN §12.1). Each entry is a
        // fixed two lines so that a page holds a whole number of them; a log
        // line longer than that is elided, which is a real loss and a smaller
        // one than a page whose last row is cut in half.
        Rectangle {
            id: logPanel
            width: parent.width
            height: screen.logShown ? 520 : 0
            visible: screen.logShown
            color: Style.paper
            border.width: 1
            border.color: Style.rule

            readonly property int rowHeight: Math.max(1, Math.round(logLineProbe.height * 2))
            readonly property int pageSize: Paging.rowsPerPage(logViewport.height, logPanel.rowHeight)
            // From the model, not the view: a ListView updates its own count
            // during a layout pass, so a page count taken from it lags a frame.
            readonly property int lineCount: screen.logLines ? screen.logLines.length : 0
            readonly property int totalPages: Paging.pageCount(logPanel.lineCount, logPanel.pageSize)

            Text {
                id: logLineProbe
                visible: false
                text: "Ag"
                font.pointSize: Style.smallSize
                font.family: "monospace"
            }

            Item {
                id: logViewport
                anchors {
                    top: parent.top; topMargin: Style.gap
                    left: parent.left; leftMargin: Style.gap
                    right: parent.right; rightMargin: Style.gap
                    bottom: logPager.top
                }

                ListView {
                    id: logView
                    objectName: "logView"
                    anchors { top: parent.top; left: parent.left; right: parent.right }
                    height: Paging.rowsPerPage(logViewport.height, logPanel.rowHeight) * logPanel.rowHeight
                    clip: true

                    interactive: false
                    cacheBuffer: 0
                    contentY: Paging.firstIndex(screen.logPage, logPanel.pageSize) * logPanel.rowHeight

                    model: screen.logLines

                    delegate: Item {
                        width: logView.width
                        height: logPanel.rowHeight

                        Text {
                            anchors.fill: parent
                            wrapMode: Text.WrapAnywhere
                            maximumLineCount: 2
                            elide: Text.ElideRight
                            text: modelData
                            font.pointSize: Style.smallSize
                            font.family: "monospace"
                            color: Style.muted
                        }
                    }
                }

                Text {
                    anchors.centerIn: parent
                    text: "Nothing logged yet."
                    font.pointSize: Style.smallSize
                    color: Style.muted
                    visible: logPanel.lineCount === 0
                }
            }

            PagerBar {
                id: logPager
                objectName: "logPager"
                anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                page: screen.logPage
                totalPages: logPanel.totalPages
                hasMore: screen.logPage < logPanel.totalPages
                onPreviousRequested: screen.logPage = Paging.clampPage(screen.logPage - 1, logPanel.totalPages)
                onNextRequested: screen.logPage = Paging.clampPage(screen.logPage + 1, logPanel.totalPages)
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
