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

    // The download cache (PLAN §12.4). The size and every sentence about it are
    // the backend's; this screen renders them and never formats a size of its
    // own, so "636 MB" is spelled one way in the whole application.
    property string cacheSummary: ""

    // The question, while it is being asked. Non-empty *is* the asking: there
    // is one question on this screen at a time and nowhere else to put it.
    property string cacheQuestion: ""

    signal pingRequested()
    signal logRequested()
    signal clearErrorRequested()
    signal consultRobotsRequested(bool on)

    // Clearing is two steps, like deleting a download: the first asks the
    // backend for the question, the second answers it. Hundreds of megabytes
    // and no way back but refetching is not something to do on one tap.
    signal cacheSizeRequested()
    signal clearCacheRequested()
    signal clearCacheConfirmed()

    // Asked for whenever the screen comes up, so the number on it is the number
    // now. The screen that shows a figure is the one that should ask for it —
    // with the caller asking instead, a new way onto this screen arrives with a
    // stale size and nothing to say it is stale.
    onVisibleChanged: {
        if (screen.visible)
            screen.cacheSizeRequested()
    }

    function askToClearCache() {
        screen.cacheQuestion = ""
        screen.clearCacheRequested()
    }

    function closeCacheQuestion() {
        screen.cacheQuestion = ""
    }

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

        // Settings is the one screen that is a list of unrelated sections
        // rather than a list of like things, so the headings are what is
        // actually navigated. Black words with the accent as a filled bar
        // under them — ui/SectionHeading.qml, which is where the reasoning
        // for that shape lives.
        SectionHeading {
            width: parent.width
            text: "Backend"
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

        SectionHeading {
            width: parent.width
            text: "Fetching"
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

        SectionHeading {
            width: parent.width
            text: "Last problem"
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

        // The download cache — PLAN §12.4.
        //
        // Page images stay on disk after a download so a repeat can skip them,
        // and deleting a document reclaims the ones behind it. What it cannot
        // reclaim is pages a delete had to skip because that chapter was
        // downloading at the time, or pages a failed download left before any
        // record existed: nothing names those again, so without this they stay
        // for good. One button, no schedule — a cache that empties itself is a
        // download that vanished the night before a flight.
        SectionHeading {
            width: parent.width
            text: "Downloads cache"
        }

        // The size, in the backend's words. A button to clear something whose
        // size you cannot see is a button nobody dares press, and this is also
        // how the user checks it worked.
        Text {
            objectName: "cacheSummary"
            width: parent.width
            wrapMode: Text.WordWrap
            text: screen.cacheSummary.length > 0 ? screen.cacheSummary : "Asking how much is cached…"
            font.pointSize: Style.bodySize
            color: Style.muted
        }

        Rectangle {
            objectName: "clearCacheButton"
            width: parent.width
            height: Style.buttonHeight
            color: clearCacheArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6
            visible: screen.cacheQuestion.length === 0

            Text {
                anchors.centerIn: parent
                text: "Clear the cache"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: clearCacheArea
                objectName: "clearCacheArea"
                anchors.fill: parent
                enabled: parent.visible
                onClicked: screen.askToClearCache()
            }
        }

        // The question, and its two answers. The same shape as the delete
        // confirmation: the way out is as easy to tap as the thing it protects.
        Column {
            objectName: "cacheConfirm"
            width: parent.width
            spacing: Style.gap
            visible: screen.cacheQuestion.length > 0

            Text {
                objectName: "cacheQuestion"
                width: parent.width
                wrapMode: Text.WordWrap
                text: screen.cacheQuestion
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            Row {
                spacing: Style.gap

                Rectangle {
                    objectName: "keepCacheButton"
                    width: 220
                    height: Style.buttonHeight
                    color: keepCacheArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: Style.ink
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Keep it"
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: keepCacheArea
                        objectName: "keepCacheArea"
                        anchors.fill: parent
                        onClicked: screen.closeCacheQuestion()
                    }
                }

                Rectangle {
                    objectName: "confirmClearCacheButton"
                    width: 260
                    height: Style.buttonHeight
                    color: confirmClearArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: Style.rule
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Clear it"
                        font.pointSize: Style.smallSize
                        color: Style.ink
                    }

                    MouseArea {
                        id: confirmClearArea
                        objectName: "confirmClearArea"
                        anchors.fill: parent
                        onClicked: {
                            screen.closeCacheQuestion()
                            screen.clearCacheConfirmed()
                        }
                    }
                }
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
        SectionHeading {
            width: parent.width
            text: "Log"
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
