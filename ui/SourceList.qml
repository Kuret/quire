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
import "Paging.js" as Paging

Item {
    id: screen

    property alias model: list.model

    // PLAN §12.1: the list turns pages rather than scrolling. The whole list is
    // already in hand here — the backend sends every configured source in one
    // message — so choosing the window into it is presentational and stays in
    // the view. Only the series grid, where the list is fetched lazily, needs
    // the backend's pager.
    property int page: 1
    // From the model, not the view: a ListView updates its own count during a
    // layout pass, so a page count taken from it lags by a frame.
    readonly property int rowCount: screen.model ? screen.model.count : 0
    readonly property int pageSize: Paging.rowsPerPage(viewport.height, Style.rowHeight)
    readonly property int totalPages: Paging.pageCount(screen.rowCount, screen.pageSize)

    // Removing the last source on the last page would otherwise leave the user
    // on a page that no longer exists.
    onTotalPagesChanged: screen.page = Paging.clampPage(screen.page, screen.totalPages)

    signal addRequested()
    signal watchingRequested()

    // PLAN §12.2's "short" summary, composed in the backend. Empty means there
    // is nothing to report, and the button says only "Watching" — a badge that
    // is always lit is a badge nobody reads.
    property string watchingLabel: ""
    signal openRequested(string sourceId, string name)
    signal toggleRequested(string sourceId, bool enabled)
    signal removeRequested(string sourceId)
    signal renameRequested(string sourceId, string name)
    signal noticeDismissed()

    // A quiet line from the backend, above the list. Composed there, not here.
    property string notice: ""

    // Which row has its confirm-remove strip open. Removing a source is one tap
    // away from a full library of downloads still being there but nothing to
    // update them from, so it asks first.
    //
    // The strip is drawn over the bottom of the list rather than inside the
    // row: an expanding row would push the rows below it off a page whose size
    // is fixed, and a half row is exactly what PLAN §12.1 forbids. The name
    // rides along because the strip is no longer inside the delegate that knows
    // it.
    property string confirmingId: ""
    property string confirmingName: ""

    // The source being renamed, and the name being typed. Renaming is here
    // rather than on a screen of its own because it is one field: the name is
    // guessed from the site's <title> at probe time and that guess is
    // sometimes wrong ("MangaDex API documentation"), so editing it should cost
    // a tap, not a navigation.
    property string renamingId: ""
    property string renameText: ""

    function startRename(sourceId, name) {
        screen.confirmingId = ""
        screen.confirmingName = ""
        screen.renamingId = sourceId
        screen.renameText = name
    }

    function commitRename() {
        if (screen.renameText.trim().length === 0)
            return
        screen.renameRequested(screen.renamingId, screen.renameText.trim())
        screen.renamingId = ""
        screen.renameText = ""
    }

    // The notice strip. One line, on the first screen, with a way to dismiss
    // it — the thing it reports (Quire closed unexpectedly) is worth knowing
    // once and worth never seeing again after that.
    Item {
        id: noticeStrip
        objectName: "noticeStrip"
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: screen.notice.length > 0 ? noticeText.height + Style.gap * 2 : 0
        visible: screen.notice.length > 0

        Text {
            id: noticeText
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: dismissButton.left; rightMargin: Style.gap
                top: parent.top; topMargin: Style.gap
            }
            wrapMode: Text.WordWrap
            text: screen.notice
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        Rectangle {
            id: dismissButton
            objectName: "dismissNoticeButton"
            anchors { right: parent.right; rightMargin: Style.margin; top: parent.top; topMargin: Style.gap }
            width: 120
            height: Style.buttonHeight - Style.gap
            color: dismissArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "OK"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: dismissArea
                anchors.fill: parent
                onClicked: screen.noticeDismissed()
            }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    // The viewport is fixed: it does not change when a row's confirm strip
    // opens, because that strip is an overlay above the pager rather than an
    // expansion inside the list. A list that reflows under the finger is the
    // scrolling problem wearing a different hat (PLAN §12.1).
    Item {
        id: viewport
        anchors {
            top: noticeStrip.bottom
            left: parent.left; right: parent.right
            bottom: pagerBar.top
        }

        ListView {
            id: list
            anchors { top: parent.top; left: parent.left; right: parent.right }
            // Whole rows only. The remainder is blank rather than a half row.
            height: Paging.rowsPerPage(viewport.height, Style.rowHeight) * Style.rowHeight
            clip: true

            // Nothing scrolls. contentY is set outright, which is one settled
            // repaint rather than a stream of partial ones.
            interactive: false
            cacheBuffer: 0
            contentY: Paging.firstIndex(screen.page, screen.pageSize) * Style.rowHeight

            delegate: Item {
                width: list.width
                height: Style.rowHeight

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
                        onPressAndHold: {
                            var open = screen.confirmingId !== model.sourceId
                            screen.confirmingId = open ? model.sourceId : ""
                            screen.confirmingName = open ? model.name : ""
                        }
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

                Rectangle {
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    height: Style.hairline
                    color: Style.rule
                }
            }
        }
    }

    // The empty state. PLAN §1.3 is the reason it exists and the reason it is
    // worded as an instruction rather than an apology.
    Column {
        anchors.centerIn: viewport
        width: Math.min(parent.width - Style.margin * 2, 700)
        spacing: Style.gap
        visible: screen.rowCount === 0

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

    // ---- the confirm strip -------------------------------------------------
    //
    // Drawn over the foot of the list rather than inside the row it belongs to.
    // Inside the row it would push the rows below it down and off a page of
    // fixed size; here nothing reflows, the question always appears in the same
    // place, and only one is ever open (two open questions is two ways to tap
    // the wrong answer).
    Rectangle {
        id: confirmStrip
        objectName: "confirmStrip"
        anchors { left: parent.left; right: parent.right; bottom: pagerBar.top }
        height: Style.rowHeight
        color: Style.paper
        visible: screen.confirmingId.length > 0

        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: Style.hairline
            color: Style.rule
        }

        Text {
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: renameButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            elide: Text.ElideRight
            text: "Remove " + screen.confirmingName +
                  "? Downloaded volumes stay in your library."
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        Rectangle {
            id: renameButton
            objectName: "renameButton"
            anchors {
                right: removeButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            width: 160
            height: Style.buttonHeight - Style.gap
            color: renameArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Rename"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: renameArea
                anchors.fill: parent
                onClicked: screen.startRename(screen.confirmingId, screen.confirmingName)
            }
        }

        Rectangle {
            id: removeButton
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
                    screen.removeRequested(screen.confirmingId)
                    screen.confirmingId = ""
                    screen.confirmingName = ""
                }
            }
        }
    }

    // ---- paging ------------------------------------------------------------

    PagerBar {
        id: pagerBar
        objectName: "sourcePager"
        anchors { left: parent.left; right: parent.right; bottom: addBar.top }
        page: screen.page
        totalPages: screen.totalPages
        hasMore: screen.page < screen.totalPages
        onPreviousRequested: screen.page = Paging.clampPage(screen.page - 1, screen.totalPages)
        onNextRequested: screen.page = Paging.clampPage(screen.page + 1, screen.totalPages)
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

        // Two buttons, side by side. "Watching" lives here rather than in the
        // header because the header's one slot is Settings on every screen, and
        // because the first screen is where a list of things that may have
        // gained a chapter belongs (PLAN §12.2).
        Row {
            anchors.centerIn: parent
            spacing: Style.gap

            Rectangle {
                objectName: "watchingButton"
                width: Math.min((parent.parent.width - Style.margin * 2 - Style.gap) / 2, 300)
                height: Style.buttonHeight
                color: watchingArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    objectName: "watchingLabel"
                    anchors.centerIn: parent
                    // The backend's words after the view's own noun. Nothing
                    // here counts, pluralises or decides what "3 new" means.
                    text: screen.watchingLabel.length > 0
                          ? "Watching · " + screen.watchingLabel : "Watching"
                    font.pointSize: Style.bodySize
                    color: Style.ink
                }

                MouseArea {
                    id: watchingArea
                    anchors.fill: parent
                    onClicked: screen.watchingRequested()
                }
            }

            Rectangle {
                objectName: "addSourceButton"
                width: Math.min((parent.parent.width - Style.margin * 2 - Style.gap) / 2, 300)
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

    // ---- rename ------------------------------------------------------------
    //
    // A panel over the list rather than a screen of its own: it is one field,
    // and the device has no system keyboard available to an AppLoad app
    // (PLAN §11 Q5), so the text comes from ui/Keyboard.qml exactly as the
    // add-source form does.
    Rectangle {
        id: renamePanel
        objectName: "renamePanel"
        anchors.fill: parent
        color: Style.paper
        visible: screen.renamingId.length > 0

        // Swallow taps so the list underneath cannot be operated while this is
        // open.
        MouseArea { anchors.fill: parent }

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
                text: "What should this source be called?"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            Rectangle {
                width: parent.width
                height: Style.buttonHeight
                color: Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                TextInput {
                    id: nameField
                    objectName: "nameField"
                    anchors {
                        fill: parent
                        leftMargin: Style.gap
                        rightMargin: Style.gap
                    }
                    verticalAlignment: TextInput.AlignVCenter
                    font.pointSize: Style.bodySize
                    color: Style.ink
                    // schema/source.schema.json: 1-120 characters.
                    maximumLength: 120
                    activeFocusOnPress: false
                    text: screen.renameText
                    onTextChanged: screen.renameText = text
                }
            }

            Row {
                spacing: Style.gap

                Rectangle {
                    objectName: "renameSaveButton"
                    width: 220
                    height: Style.buttonHeight
                    color: saveArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: screen.renameText.trim().length > 0 ? Style.ink : Style.rule
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Save"
                        font.pointSize: Style.bodySize
                        color: screen.renameText.trim().length > 0 ? Style.ink : Style.rule
                    }

                    MouseArea {
                        id: saveArea
                        anchors.fill: parent
                        enabled: screen.renameText.trim().length > 0
                        onClicked: screen.commitRename()
                    }
                }

                Rectangle {
                    width: 220
                    height: Style.buttonHeight
                    color: cancelRenameArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: Style.ink
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Cancel"
                        font.pointSize: Style.bodySize
                        color: Style.ink
                    }

                    MouseArea {
                        id: cancelRenameArea
                        anchors.fill: parent
                        onClicked: { screen.renamingId = ""; screen.renameText = "" }
                    }
                }
            }
        }

        Keyboard {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            layout: "text"
            onKeyTyped: screen.renameText += text
            onBackspace: screen.renameText = screen.renameText.substring(0, screen.renameText.length - 1)
            onClearAll: screen.renameText = ""
            onSubmit: screen.commitRename()
        }
    }
}
