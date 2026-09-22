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
import "Screens.js" as Screens
import "Paging.js" as Paging

Item {
    id: screen

    property alias model: list.model

    // True when this instance is drawing the private source list rather than
    // the normal one — one component, reused for both (they share every
    // action a row offers except which direction "private" moves it, and
    // duplicating the file would be two things to keep in step forever). See
    // Main.qml for how the same SourceList and SearchAll instances are
    // repointed at the private scope's model and messages.
    property bool showingPrivate: false

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
    signal downloadedRequested()

    // One query across every enabled source (Msg.SearchAll). It is offered
    // here because this is the screen that *is* every source: searching one is
    // reached by opening it, so searching all of them belongs where all of
    // them are listed, not inside any one of them.
    signal searchAllRequested()

    // PLAN §12.2's "short" summary, composed in the backend. Empty means there
    // is nothing to report, and the button says only "Watching" — a badge that
    // is always lit is a badge nobody reads.
    property string watchingLabel: ""
    signal openRequested(string sourceId, string name)
    signal toggleRequested(string sourceId, bool enabled)
    signal removeRequested(string sourceId)
    signal renameRequested(string sourceId, string name)
    // PLAN §12.3's per-source override. mode is "auto", "never" or "always" —
    // the schema's own spellings, sent as data. The words the user reads are
    // chosen below and never travel.
    signal splitStripsRequested(string sourceId, string mode)
    // The proxy a source is reached through. An empty proxy removes it.
    signal proxyRequested(string sourceId, string proxy)
    // The allowedHosts editor: granting a pending host, or taking back one
    // already granted. Both name the source and the exact host — never a
    // domain, never inferred — because a permission a source did not ask for
    // is not one it should get.
    signal allowHostRequested(string sourceId, string host)
    signal revokeHostRequested(string sourceId, string host)
    signal noticeDismissed()

    // Marking a source private, or taking that back — reachable from a row's
    // long-press menu on either list, the same way Rename and Remove are.
    signal privateRequested(string sourceId, bool makePrivate)

    // The eye-icon button, lower right, only on the normal list: the way in to
    // the private one. It carries no state of its own — the private screen is
    // reached by name, not toggled — so there is nothing to bind besides the
    // tap.
    signal privateListRequested()

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
    // The confirm strip's own sentence for removing this source
    // (backend/service/service.go's removeQuestion) — composed there, not
    // here, because it depends on whether the source has any chapters saved
    // in Quire (PLAN §2).
    property string confirmingRemoveQuestion: ""
    // The open row's current splitting mode, carried for the same reason the
    // name is: the strip is drawn outside the delegate that knows it.
    property string confirmingSplit: "auto"
    // The open row's current proxy and whether its self-hosted confirmation
    // stands on it, carried the same way: to prefill the proxy panel and to
    // decide whether clearing the field needs a warning.
    property string confirmingProxy: ""
    property bool confirmingViaProxy: false
    // The open row's pending hosts (JSON, since a ListModel role cannot
    // reliably hold an array — see the comment on "authors" in Main.qml's
    // fillSeries) and already-allowed hosts (comma-joined, the same reason),
    // carried the same way the proxy fields are: to prefill the hosts panel
    // and to decide whether the row strip's Hosts button has anything to say.
    property string confirmingPendingHosts: "[]"
    property string confirmingAllowedHosts: ""

    // The source whose splitting panel is open, and the mode shown as chosen.
    // Kept as a plain string rather than read back through the model on each
    // paint: an e-ink screen must not repaint on an unchanged value, and
    // assigning the same string to a QML property emits no change signal.
    property string splittingId: ""
    property string splittingName: ""
    property string splittingMode: "auto"

    // The words for each mode, in one place. The backend never sends these —
    // it sends "auto", "never", "always" — because PLAN §2 keeps the wording
    // on the side that shows it, and because a mode is a value, not a sentence.
    function splitModeLabel(mode) {
        if (mode === "never")
            return "Never"
        if (mode === "always")
            return "Always"
        return "Automatic"
    }

    function startSplitting(sourceId, name, mode) {
        screen.confirmingId = ""
        screen.confirmingName = ""
        screen.splittingId = sourceId
        screen.splittingName = name
        screen.splittingMode = mode ? mode : "auto"
    }

    function chooseSplitMode(mode) {
        // Sending an unchanged value would make the backend rewrite the store
        // and push a fresh source list for nothing, which on e-ink is a visible
        // repaint of the whole screen.
        if (mode !== screen.splittingMode)
            screen.splitStripsRequested(screen.splittingId, mode)
        screen.splittingMode = mode
    }

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
        screen.splittingId = ""
        screen.renamingId = sourceId
        screen.renameText = name
    }

    // dismissInput puts the keyboard away (PLAN §11 Q5). Called when the
    // rename panel closes, either way, and when this screen is navigated away
    // from — never while the user is still typing a name.
    //
    // Quire's keyboard lives inside the rename panel, so closing the panel is
    // what lowers it; the focus drop still comes first, for the reason in
    // Screens.js.
    function dismissInput() {
        Screens.dismissKeyboard([nameField, proxyField])
    }

    function commitRename() {
        if (screen.renameText.trim().length === 0)
            return
        screen.dismissInput()
        screen.renameRequested(screen.renamingId, screen.renameText.trim())
        screen.renamingId = ""
        screen.renameText = ""
    }

    // The source being given, changed, or cleared of a proxy. A wrong proxy
    // used to mean deleting the source and adding it again; this is the way
    // back without that.
    property string proxyingId: ""
    property string proxyingName: ""
    property string proxyText: ""
    // Whether the source's self-hosted confirmation stands on the proxy being
    // edited — carried from confirmingViaProxy at the moment the panel opens,
    // so the warning below reads against the value the field started at, not
    // against whatever is typed.
    property bool proxyingViaProxy: false

    function startProxy(sourceId, name, proxy, viaProxy) {
        screen.confirmingId = ""
        screen.confirmingName = ""
        screen.splittingId = ""
        screen.renamingId = ""
        screen.proxyingId = sourceId
        screen.proxyingName = name
        screen.proxyText = proxy
        screen.proxyingViaProxy = viaProxy
    }

    function commitProxy() {
        screen.dismissInput()
        screen.proxyRequested(screen.proxyingId, screen.proxyText.trim())
        screen.proxyingId = ""
        screen.proxyText = ""
    }

    // The source whose allowedHosts panel is open: which hosts are pending
    // review and which are already granted. Parsed once, on open, into plain
    // arrays a Repeater can iterate — see confirmingPendingHosts above for
    // why the row strip carries them as strings.
    property string hostingId: ""
    property string hostingName: ""
    property var hostingPending: []
    property var hostingAllowed: []

    function startHosts(sourceId, name, pendingJson, allowedCsv) {
        screen.confirmingId = ""
        screen.confirmingName = ""
        screen.splittingId = ""
        screen.renamingId = ""
        screen.proxyingId = ""
        screen.hostingId = sourceId
        screen.hostingName = name
        var pending = []
        try {
            pending = JSON.parse(pendingJson)
        } catch (e) {
            pending = []
        }
        screen.hostingPending = pending ? pending : []
        screen.hostingAllowed = allowedCsv && allowedCsv.length > 0
                                 ? allowedCsv.split(",") : []
    }

    // Allowing moves a host from Pending to Allowed on the same source; the
    // backend answers with a fresh source list, and the panel's own arrays
    // are updated here so the row does not wait for that round trip to stop
    // showing a host that was just granted.
    function allowHost(host) {
        screen.allowHostRequested(screen.hostingId, host)
        var kept = []
        for (var i = 0; i < screen.hostingPending.length; ++i) {
            if (screen.hostingPending[i].host !== host)
                kept.push(screen.hostingPending[i])
        }
        screen.hostingPending = kept
        var allowed = screen.hostingAllowed.slice()
        allowed.push(host)
        screen.hostingAllowed = allowed
    }

    function revokeHost(host) {
        screen.revokeHostRequested(screen.hostingId, host)
        var kept = []
        for (var i = 0; i < screen.hostingAllowed.length; ++i) {
            if (screen.hostingAllowed[i] !== host)
                kept.push(screen.hostingAllowed[i])
        }
        screen.hostingAllowed = kept
    }

    // The plain word for what a pending host was refused while fetching —
    // the backend sends "cover" or "page" as data (PLAN §2), and the
    // sentence around it is this screen's to write.
    function hostPurposeLabel(purpose) {
        if (purpose === "page")
            return "a page"
        return "a cover"
    }

    // The notice strip. One line, on the first screen, with a way to dismiss
    // it — the thing it reports (Quire closed unexpectedly) is worth knowing
    // once and worth never seeing again after that.
    Item {
        id: noticeStrip
        objectName: "noticeStrip"
        anchors { top: parent.top; left: parent.left; right: parent.right }
        // Tall enough for whichever of the two is taller. The notice is one
        // short line and the button is a full tap target, so measuring the
        // text alone left the button hanging out of the bottom of the strip
        // and over the first source row.
        height: screen.notice.length > 0
                ? Math.max(noticeText.height, dismissButton.height) + Style.gap * 2
                : 0
        visible: screen.notice.length > 0

        Text {
            id: noticeText
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: dismissButton.left; rightMargin: Style.gap
                // Centred against the button rather than both hanging from the
                // top, so a one-line notice reads level with the thing that
                // dismisses it.
                verticalCenter: dismissButton.verticalCenter
            }
            wrapMode: Text.WordWrap
            text: screen.notice
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        Rectangle {
            id: dismissButton
            objectName: "dismissNoticeButton"
            anchors { right: parent.right; rightMargin: Style.margin
                      top: parent.top; topMargin: Style.gap }
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
                            screen.confirmingRemoveQuestion = open && model.removeQuestion
                                                            ? model.removeQuestion : ""
                            screen.confirmingSplit = open && model.splitStrips
                                                   ? model.splitStrips : "auto"
                            screen.confirmingProxy = open && model.proxy ? model.proxy : ""
                            screen.confirmingViaProxy = open && !!model.selfHostedViaProxy
                            screen.confirmingPendingHosts = open && model.pendingHosts
                                                          ? model.pendingHosts : "[]"
                            screen.confirmingAllowedHosts = open && model.allowedHosts
                                                           ? model.allowedHosts : ""
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
            text: screen.showingPrivate ? "No private sources" : "No sources yet"
            font.pointSize: Style.headingSize
            color: Style.ink
        }
        Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.WordWrap
            // The private list's own empty state says what marking a source
            // private does and, as plainly, what it does not: a volume already
            // downloaded from it is a document in your reMarkable library, and
            // this screen has no say over that library.
            text: screen.showingPrivate
                  ? "Nothing here. Mark a source private from its menu on the " +
                    "main source list to keep it off that list and out of its " +
                    "combined search. Anything already downloaded from it stays " +
                    "in your reMarkable library exactly as before."
                  : "Quire ships with no sites. Add one by pasting its web address; " +
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
            objectName: "removeQuestionLabel"
            anchors {
                left: parent.left; leftMargin: Style.margin
                right: splitButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            // The backend's own sentence (backend/service/service.go's
            // removeQuestion) — composed there, not here, because whether
            // removing this source also deletes chapters saved in Quire is
            // not something QML knows (PLAN §2). Two lines rather than one:
            // the strip has the height to spare, and the "chapters saved in
            // Quire will be deleted" sentence is worth reading in full
            // rather than elided away.
            wrapMode: Text.WordWrap
            maximumLineCount: 2
            elide: Text.ElideRight
            text: screen.confirmingRemoveQuestion
            font.pointSize: Style.smallSize
            color: Style.muted
        }

        // The splitting setting reads its current value on the button, so the
        // answer to "is this source splitting my pages?" costs a long press
        // rather than opening anything.
        Rectangle {
            id: splitButton
            objectName: "splitButton"
            anchors {
                right: proxyButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            width: 300
            height: Style.buttonHeight - Style.gap
            color: splitArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                objectName: "splitButtonLabel"
                anchors.centerIn: parent
                text: "Splitting: " + screen.splitModeLabel(screen.confirmingSplit)
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: splitArea
                anchors.fill: parent
                onClicked: screen.startSplitting(screen.confirmingId, screen.confirmingName,
                                                 screen.confirmingSplit)
            }
        }

        // The proxy a source is reached through, editable the same way its
        // name is: opening a panel over the list rather than a screen of its
        // own, since it too is one field.
        Rectangle {
            id: proxyButton
            objectName: "proxyButton"
            anchors {
                right: hostsButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            width: 160
            height: Style.buttonHeight - Style.gap
            color: proxyArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Proxy"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: proxyArea
                anchors.fill: parent
                onClicked: screen.startProxy(screen.confirmingId, screen.confirmingName,
                                             screen.confirmingProxy, screen.confirmingViaProxy)
            }
        }

        // The allowedHosts editor: what a source asked to reach beyond its own
        // domain, pending review or already granted. Opened the same way
        // Proxy is, from the row strip, carrying the source's own lists along
        // since the strip is outside the delegate that knows them.
        Rectangle {
            id: hostsButton
            objectName: "hostsButton"
            anchors {
                right: renameButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            width: 160
            height: Style.buttonHeight - Style.gap
            color: hostsArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Hosts"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: hostsArea
                anchors.fill: parent
                onClicked: screen.startHosts(screen.confirmingId, screen.confirmingName,
                                              screen.confirmingPendingHosts,
                                              screen.confirmingAllowedHosts)
            }
        }

        Rectangle {
            id: renameButton
            objectName: "renameButton"
            anchors {
                right: privateButton.left; rightMargin: Style.gap
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

        // Marking private, or taking that back — the reverse action the
        // private list needs is exactly this button with its label flipped,
        // since it is the same row menu on either list (screen.showingPrivate).
        Rectangle {
            id: privateButton
            objectName: "privateButton"
            anchors {
                right: removeButton.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            width: 160
            height: Style.buttonHeight - Style.gap
            color: privateArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: screen.showingPrivate ? "Make public" : "Make private"
                font.pointSize: Style.smallSize
                color: Style.ink
            }

            MouseArea {
                id: privateArea
                anchors.fill: parent
                onClicked: {
                    screen.privateRequested(screen.confirmingId, !screen.showingPrivate)
                    screen.confirmingId = ""
                    screen.confirmingName = ""
                }
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

        // The eye-icon button sits at the right-hand end of the labelled row
        // rather than on a strip of its own, and privateSlot is what makes
        // that safe: the labelled buttons divide the space *after* this is
        // taken out, so the two cannot collide however narrow the panel gets.
        // Zero on the private list, where there is no eye to make room for.
        readonly property int privateMarkWidth: 64
        readonly property int privateSlot: screen.showingPrivate ? 0 : privateMarkWidth + Style.gap

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
            id: actionRow
            anchors { horizontalCenter: parent.horizontalCenter
                      top: parent.top; topMargin: Style.gap / 2 }
            height: Style.rowHeight + Style.gap / 2
            spacing: Style.gap

            // Four buttons now: Search, Watching, Downloaded and Add.
            // Downloaded sits beside Watching because they answer the same
            // question from opposite ends — what is new, and what is already
            // here — and downloads happen without watching (PLAN §12.5).
            // Search leads, because it is the only one of the four that is
            // about finding something rather than about what is already found.
            //
            // The width is a quarter of the bar rather than a third, and the
            // cap is what keeps the row from spreading across the panel: the
            // arithmetic has to stay derived, because the AppLoad PC emulator
            // is a window and a bar sized to the panel overflows it.
            Rectangle {
                objectName: "searchAllButton"
                width: Math.min((parent.parent.width - Style.margin * 2 - Style.gap * 3 - parent.parent.privateSlot) / 4, 300)
                height: Style.buttonHeight
                color: searchAllArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    anchors.centerIn: parent
                    // "Search all", not "Search": the screen it opens asks
                    // every source at once, and the per-source search lives
                    // inside a source. A word each way, so neither reads as
                    // the other.
                    text: "Search all"
                    font.pointSize: Style.bodySize
                    color: Style.ink
                }

                MouseArea {
                    id: searchAllArea
                    objectName: "searchAllArea"
                    anchors.fill: parent
                    onClicked: screen.searchAllRequested()
                }
            }

            Rectangle {
                objectName: "downloadedButton"
                // Downloaded and Watching are both about the *normal* library:
                // private sources have no counterpart list of their own (see
                // PrivateMark.qml's comment and the header note above this
                // file's empty state) — the discretion this feature offers is
                // scoped to the list and the combined search, not to every
                // screen a source's series can reach.
                visible: !screen.showingPrivate
                width: Math.min((parent.parent.width - Style.margin * 2 - Style.gap * 3 - parent.parent.privateSlot) / 4, 300)
                height: Style.buttonHeight
                color: downloadedArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    anchors.centerIn: parent
                    text: "Downloaded"
                    font.pointSize: Style.bodySize
                    color: Style.ink
                }

                MouseArea {
                    id: downloadedArea
                    objectName: "downloadedArea"
                    anchors.fill: parent
                    onClicked: screen.downloadedRequested()
                }
            }

            Rectangle {
                objectName: "watchingButton"
                visible: !screen.showingPrivate
                width: Math.min((parent.parent.width - Style.margin * 2 - Style.gap * 3 - parent.parent.privateSlot) / 4, 300)
                height: Style.buttonHeight
                color: watchingArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                // The entry point's half of the badge: a solid square inside
                // the button, left of its label, and only when there is
                // something to report. The label itself was the coloured part
                // until the device said otherwise (ui/Style.js); the mark
                // carries the colour now and the words carry the news, which
                // is the order those two were always meant to be in.
                //
                // Placed off the label's contentWidth for the same reason the
                // pager's is (ui/PagerBar.qml): the label is centred in a
                // fixed-width button, so an anchor to the button's edge would
                // sit the mark a finger away from the word it belongs to.
                AccentMark {
                    objectName: "watchingMark"
                    anchors.verticalCenter: parent.verticalCenter
                    x: watchingText.x + (watchingText.width - watchingText.contentWidth) / 2
                       - Style.gap / 2 - width
                    visible: screen.watchingLabel.length > 0
                }

                Text {
                    id: watchingText
                    objectName: "watchingLabel"
                    anchors.centerIn: parent
                    // The backend's words after the view's own noun. Nothing
                    // here counts, pluralises or decides what "3 new" means.
                    //
                    // With nothing to report it says "Watching" in black,
                    // exactly like its neighbours; the mark arrives with the
                    // extra words, never instead of them.
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
                // A source is added on the normal list; marking it private is
                // a separate, later action from that list's own row menu (see
                // privateButton above), so the private screen offers no add
                // button of its own.
                visible: !screen.showingPrivate
                width: Math.min((parent.parent.width - Style.margin * 2 - Style.gap * 3 - parent.parent.privateSlot) / 4, 300)
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

        // The way in to the private source list: small, icon-only, in the
        // lower right of the screen — deliberately not one of the labelled
        // buttons above. Those name what they do; this one is meant to be
        // found by someone who is looking for it and to read as nothing in
        // particular to someone who is not (see PrivateMark.qml). It carries
        // no label and no count, on purpose: a badge here would be exactly
        // the signpost the brief asks this not to be.
        //
        // It sits at the right-hand end of the labelled row. That row's
        // buttons are capped at 300px each and centred, so the slack beside
        // them can shrink to nothing on a narrow panel — which is why the
        // eye's width is subtracted from their arithmetic (addBar.privateSlot)
        // rather than hoped for. A button that only sometimes has room is a
        // button that sometimes cannot be tapped.
        //
        // Only on the normal list. The private list is reached from here and
        // left by the header's own Back, so it does not need a way back to
        // itself.
        Rectangle {
            id: privateListButton
            objectName: "privateListButton"
            anchors {
                right: parent.right; rightMargin: Style.margin
                verticalCenter: actionRow.verticalCenter
            }
            width: addBar.privateMarkWidth
            height: Style.buttonHeight
            visible: !screen.showingPrivate
            color: privateListArea.pressed ? Style.pressed : Style.paper
            border.width: 1
            border.color: Style.rule
            radius: 6

            PrivateMark {
                anchors.centerIn: parent
            }

            MouseArea {
                id: privateListArea
                objectName: "privateListArea"
                anchors.fill: parent
                onClicked: screen.privateListRequested()
            }
        }
    }

    // ---- rename ------------------------------------------------------------
    //
    // A panel over the list rather than a screen of its own: it is one field,
    // and the device has no system keyboard available to an embedded app
    // (PLAN §11 Q5) — Annex supplies none — so the text comes from
    // ui/Keyboard.qml exactly as the add-source form does.
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
                    // Fed by ui/Keyboard.qml below, never focused for input of
                    // its own.
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
                        onClicked: {
                            screen.dismissInput()
                            screen.renamingId = ""
                            screen.renameText = ""
                        }
                    }
                }
            }
        }

        Keyboard {
            objectName: "renameKeyboard"
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            layout: "text"
            onKeyTyped: screen.renameText += text
            onBackspace: screen.renameText = screen.renameText.substring(0, screen.renameText.length - 1)
            onClearAll: screen.renameText = ""
            onSubmit: screen.commitRename()
        }
    }

    // ---- proxy --------------------------------------------------------------
    //
    // The address a source is reached through, editable after the fact — until
    // this, a wrong proxy meant deleting the source and adding it again. A
    // panel over the list for the same reason renaming is one: it is a single
    // field, and there is no system keyboard here (PLAN §11 Q5).
    Rectangle {
        id: proxyPanel
        objectName: "proxyPanel"
        anchors.fill: parent
        color: Style.paper
        visible: screen.proxyingId.length > 0

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
                text: "What proxy should " + screen.proxyingName + " be reached through? " +
                      "Leave it blank to reach the source directly."
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
                    id: proxyField
                    objectName: "proxyField"
                    anchors {
                        fill: parent
                        leftMargin: Style.gap
                        rightMargin: Style.gap
                    }
                    verticalAlignment: TextInput.AlignVCenter
                    font.pointSize: Style.bodySize
                    color: Style.ink
                    // Fed by ui/Keyboard.qml below, never focused for input of
                    // its own.
                    activeFocusOnPress: false
                    text: screen.proxyText
                    onTextChanged: screen.proxyText = text
                }
            }

            // Clearing the field on a source confirmed self-hosted *by way of*
            // the proxy takes the confirmation with it: the address lives on
            // the far side of the proxy and this device never learns it, so
            // without the proxy the confirmation would be permission for a
            // route that no longer exists. Said here, before Save is pressed,
            // rather than discovered afterwards.
            Text {
                objectName: "proxyRevokeWarning"
                width: parent.width
                wrapMode: Text.WordWrap
                visible: screen.proxyingViaProxy && screen.proxyText.trim().length === 0
                text: "This also withdraws permission for the private address this source " +
                      "reaches, because that permission was only ever granted through the proxy."
                font.pointSize: Style.smallSize
                color: Style.muted
            }

            Row {
                spacing: Style.gap

                Rectangle {
                    objectName: "proxySaveButton"
                    width: 220
                    height: Style.buttonHeight
                    color: proxySaveArea.pressed ? Style.pressed : Style.paper
                    border.width: 2
                    border.color: Style.ink
                    radius: 6

                    Text {
                        anchors.centerIn: parent
                        text: "Save"
                        font.pointSize: Style.bodySize
                        color: Style.ink
                    }

                    MouseArea {
                        id: proxySaveArea
                        anchors.fill: parent
                        onClicked: screen.commitProxy()
                    }
                }

                Rectangle {
                    width: 220
                    height: Style.buttonHeight
                    color: cancelProxyArea.pressed ? Style.pressed : Style.paper
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
                        id: cancelProxyArea
                        anchors.fill: parent
                        onClicked: {
                            screen.dismissInput()
                            screen.proxyingId = ""
                            screen.proxyText = ""
                        }
                    }
                }
            }
        }

        Keyboard {
            objectName: "proxyKeyboard"
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            layout: "url"
            onKeyTyped: screen.proxyText += text
            onBackspace: screen.proxyText = screen.proxyText.substring(0, screen.proxyText.length - 1)
            onClearAll: screen.proxyText = ""
            onSubmit: screen.commitProxy()
        }
    }

    // ---- allowedHosts editor ------------------------------------------------
    //
    // Record-and-offer, not prompt-on-first-sight: a fetch on this source's
    // behalf that reached outside its own domain was refused and noted rather
    // than interrupting anything, and is only ever offered here, where the
    // owner is present and looking at this source on purpose. There is no
    // modal for this anywhere else in Quire.
    //
    // A panel over the list, like Proxy and Rename, and for the same reason:
    // it is a short list to read and a couple of buttons, not a screen of its
    // own. No keyboard — nothing here is typed, only chosen.
    Rectangle {
        id: hostsPanel
        objectName: "hostsPanel"
        anchors.fill: parent
        color: Style.paper
        visible: screen.hostingId.length > 0

        // Swallow taps so the list underneath cannot be operated while this is
        // open.
        MouseArea { anchors.fill: parent }

        Column {
            anchors {
                top: parent.top; topMargin: Style.margin
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
                bottom: hostsDoneButton.top; bottomMargin: Style.gap
            }
            spacing: Style.gap

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                text: "Hosts for " + screen.hostingName
                font.pointSize: Style.headingSize
                color: Style.ink
            }

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                text: "These were refused because they are outside this source's own " +
                      "address. Allow one only if you recognise it as this source's own " +
                      "image or page server — allowing it lets this source, and no other, " +
                      "reach that address."
                font.pointSize: Style.smallSize
                color: Style.muted
            }

            Text {
                objectName: "pendingHeading"
                width: parent.width
                text: "Pending"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            Text {
                objectName: "noPendingHosts"
                width: parent.width
                wrapMode: Text.WordWrap
                visible: screen.hostingPending.length === 0
                text: "Nothing pending."
                font.pointSize: Style.smallSize
                color: Style.muted
            }

            Repeater {
                model: screen.hostingPending

                Rectangle {
                    objectName: "pendingHost-" + modelData.host
                    width: hostsPanel.width - Style.margin * 2
                    height: Style.rowHeight
                    color: Style.paper
                    border.width: 2
                    border.color: Style.rule
                    radius: 6

                    Column {
                        anchors {
                            left: parent.left; leftMargin: Style.gap
                            right: allowButton.left; rightMargin: Style.gap
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: 4

                        Text {
                            width: parent.width
                            elide: Text.ElideRight
                            text: modelData.host
                            font.pointSize: Style.bodySize
                            color: Style.ink
                        }
                        Text {
                            width: parent.width
                            elide: Text.ElideRight
                            text: "Fetching " + screen.hostPurposeLabel(modelData.purpose)
                            font.pointSize: Style.smallSize
                            color: Style.muted
                        }
                    }

                    Rectangle {
                        id: allowButton
                        objectName: "allowHostButton-" + modelData.host
                        anchors { right: parent.right; rightMargin: Style.gap
                                  verticalCenter: parent.verticalCenter }
                        width: 160
                        height: Style.buttonHeight - Style.gap
                        color: allowArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: "Allow"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: allowArea
                            objectName: "allowHostArea-" + modelData.host
                            anchors.fill: parent
                            onClicked: screen.allowHost(modelData.host)
                        }
                    }
                }
            }

            Text {
                objectName: "allowedHeading"
                width: parent.width
                text: "Allowed"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            Text {
                objectName: "noAllowedHosts"
                width: parent.width
                wrapMode: Text.WordWrap
                visible: screen.hostingAllowed.length === 0
                text: "None allowed yet."
                font.pointSize: Style.smallSize
                color: Style.muted
            }

            Repeater {
                model: screen.hostingAllowed

                Rectangle {
                    objectName: "allowedHost-" + modelData
                    width: hostsPanel.width - Style.margin * 2
                    height: Style.rowHeight
                    color: Style.paper
                    border.width: 2
                    border.color: Style.rule
                    radius: 6

                    Text {
                        anchors {
                            left: parent.left; leftMargin: Style.gap
                            right: revokeButton.left; rightMargin: Style.gap
                            verticalCenter: parent.verticalCenter
                        }
                        elide: Text.ElideRight
                        text: modelData
                        font.pointSize: Style.bodySize
                        color: Style.ink
                    }

                    Rectangle {
                        id: revokeButton
                        objectName: "revokeHostButton-" + modelData
                        anchors { right: parent.right; rightMargin: Style.gap
                                  verticalCenter: parent.verticalCenter }
                        width: 160
                        height: Style.buttonHeight - Style.gap
                        color: revokeArea.pressed ? Style.pressed : Style.paper
                        border.width: 2
                        border.color: Style.ink
                        radius: 6

                        Text {
                            anchors.centerIn: parent
                            text: "Revoke"
                            font.pointSize: Style.smallSize
                            color: Style.ink
                        }

                        MouseArea {
                            id: revokeArea
                            objectName: "revokeHostArea-" + modelData
                            anchors.fill: parent
                            onClicked: screen.revokeHost(modelData)
                        }
                    }
                }
            }
        }

        Rectangle {
            id: hostsDoneButton
            objectName: "hostsDoneButton"
            anchors { left: parent.left; leftMargin: Style.margin
                      bottom: parent.bottom; bottomMargin: Style.margin }
            width: 220
            height: Style.buttonHeight
            color: hostsDoneArea.pressed ? Style.pressed : Style.paper
            border.width: 2
            border.color: Style.ink
            radius: 6

            Text {
                anchors.centerIn: parent
                text: "Done"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            MouseArea {
                id: hostsDoneArea
                objectName: "hostsDoneArea"
                anchors.fill: parent
                onClicked: screen.hostingId = ""
            }
        }
    }

    // ---- strip splitting ---------------------------------------------------
    //
    // PLAN §12.3. Detection decides; the user overrules. It is a panel rather
    // than a cycling button because three states on one button cannot say what
    // any of them mean, and the explanation is the load-bearing part: the worry
    // this feature answers is "an actual manga gets recognised as a webtoon and
    // weirdly split", so the screen has to say plainly that that is what
    // automatic avoids.
    //
    // No animation and no repaint on an unchanged value: taps that re-choose
    // the current mode send nothing (see chooseSplitMode).
    Rectangle {
        id: splitPanel
        objectName: "splitPanel"
        anchors.fill: parent
        color: Style.paper
        visible: screen.splittingId.length > 0

        MouseArea { anchors.fill: parent }

        Column {
            anchors {
                top: parent.top; topMargin: Style.margin
                left: parent.left; leftMargin: Style.margin
                right: parent.right; rightMargin: Style.margin
            }
            spacing: Style.gap

            Text {
                objectName: "splitHeading"
                width: parent.width
                wrapMode: Text.WordWrap
                text: "Split tall strip images into pages"
                font.pointSize: Style.bodySize
                color: Style.ink
            }

            Text {
                objectName: "splitExplanation"
                width: parent.width
                wrapMode: Text.WordWrap
                text: "Some sites publish a chapter as one very tall image meant " +
                      "for scrolling. On a page it shrinks to an unreadable strip " +
                      "down the middle, so Quire cuts it into page-shaped pieces.\n\n" +
                      "Automatic only splits images far taller than a page, and " +
                      "only when the rest of the chapter is the same shape. " +
                      "Ordinary comic pages are left alone."
                font.pointSize: Style.smallSize
                color: Style.muted
            }

            Repeater {
                model: [
                    {"mode": "auto",   "label": "Automatic",
                     "note": "Split only what is clearly a scrolling strip. The usual choice."},
                    {"mode": "never",  "label": "Never",
                     "note": "Leave every image whole, even a tall one."},
                    {"mode": "always", "label": "Always",
                     "note": "Split any image taller than a page, without checking the chapter."}
                ]

                Rectangle {
                    objectName: "splitOption-" + modelData.mode
                    width: splitPanel.width - Style.margin * 2
                    height: Style.rowHeight
                    // The chosen row is drawn heavier rather than tinted: a fill
                    // change is a full-row repaint on e-ink, and a border is
                    // legible without one.
                    color: optionArea.pressed ? Style.pressed : Style.paper
                    border.width: screen.splittingMode === modelData.mode ? 4 : 2
                    border.color: screen.splittingMode === modelData.mode ? Style.ink : Style.rule
                    radius: 6

                    Column {
                        anchors {
                            left: parent.left; leftMargin: Style.gap
                            right: parent.right; rightMargin: Style.gap
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: 4

                        Text {
                            objectName: "splitOptionLabel-" + modelData.mode
                            text: screen.splittingMode === modelData.mode
                                  ? modelData.label + " (chosen)" : modelData.label
                            font.pointSize: Style.bodySize
                            color: Style.ink
                        }

                        Text {
                            width: parent.width
                            elide: Text.ElideRight
                            text: modelData.note
                            font.pointSize: Style.smallSize
                            color: Style.muted
                        }
                    }

                    MouseArea {
                        id: optionArea
                        anchors.fill: parent
                        onClicked: screen.chooseSplitMode(modelData.mode)
                    }
                }
            }

            Rectangle {
                objectName: "splitDoneButton"
                width: 220
                height: Style.buttonHeight
                color: splitDoneArea.pressed ? Style.pressed : Style.paper
                border.width: 2
                border.color: Style.ink
                radius: 6

                Text {
                    anchors.centerIn: parent
                    text: "Done"
                    font.pointSize: Style.bodySize
                    color: Style.ink
                }

                MouseArea {
                    id: splitDoneArea
                    objectName: "splitDoneArea"
                    anchors.fill: parent
                    onClicked: screen.splittingId = ""
                }
            }
        }
    }
}
