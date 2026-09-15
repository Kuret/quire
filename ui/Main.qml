// Quire — the app shell.
//
// PLAN §2: the QML frontend is a dumb view. It owns no logic beyond sending
// messages and rendering the replies, because QML is the layer that breaks on
// an OS update and every line here is a line to re-check after one.
//
// Concretely, this file holds the models the backend fills, the AppLoad element
// that fills them, and the navigation between screens. Nothing in ui/ decides
// what a verdict means, whether a source may be added, or what a row says: all
// of that arrives as data (see backend/service).
//
// PLAN §6 M3 UI rules, which every file in ui/ follows:
//   - No animations. E-ink ghosting.
//   - Match the stock UI palette. Do not invent a brand. (ui/Style.js)
//
// Verified against the installed appload.so: the QML type exposes
// applicationID, messageReceived, sendMessage (one 's' in the middle — the
// upstream README's "sendMesssage" is a typo) and terminate. The host calls
// unloading() on the root element when the app is closed.

import QtQuick 2.5
import net.asivery.AppLoad 1.0
import "Messages.js" as Msg
import "Style.js" as Style

Rectangle {
    id: root
    anchors.fill: parent
    color: Style.paper

    // The AppLoad host connects to this and closes the frontend.
    signal close

    // Which screen is showing: "sources", "add", "browse", "series" or
    // "settings". A plain string rather than a stack, because the whole app is
    // five screens and a StackView would be a dependency for nothing.
    property string screen: "sources"

    // The source being browsed, carried between screens.
    property string currentSourceId: ""
    property string currentSourceName: ""
    property string currentSeriesId: ""

    // Backend status, shown on the settings screen.
    property string backendStatus: "Not yet asked."

    // The one error worth showing: whatever the backend last complained about.
    property string lastError: ""

    ListModel { id: sourcesModel }
    ListModel { id: seriesModel }
    ListModel { id: chaptersModel }

    // ---- transport ---------------------------------------------------------

    AppLoad {
        id: appload
        // Must equal the "id" field in manifest.json.
        applicationID: "quire"

        onMessageReceived: (type, contents) => root.dispatch(type, contents)
    }

    // send is the only way anything in ui/ talks to the backend. Payloads are
    // always JSON objects: PLAN §7.1 says all payloads are JSON, and a non-empty
    // payload keeps AppLoad's empty-packet edge case off the wire entirely.
    function send(type, payload) {
        appload.sendMessage(type, JSON.stringify(payload === undefined ? {} : payload))
    }

    function dispatch(type, contents) {
        var msg = null
        if (contents && contents.length > 0) {
            try {
                msg = JSON.parse(contents)
            } catch (e) {
                root.lastError = "The backend sent something unreadable."
                return
            }
        }

        switch (type) {
        case Msg.Pong:
            root.backendStatus = msg ? ("Backend " + msg.version + " on " + msg.os + "/" + msg.arch) : "Backend answered."
            return

        case Msg.Sources:
            root.fillSources(msg ? msg.sources : [])
            return

        case Msg.ProbeProgress:
            addSourceScreen.onProgress(msg)
            return

        case Msg.ProbeVerdict:
            addSourceScreen.onVerdict(msg)
            return

        case Msg.SearchResults:
            root.fillSeries(msg)
            return

        case Msg.CoverReady:
            root.applyCover(msg)
            return

        case Msg.SeriesDetailResult:
            root.fillChapters(msg)
            return

        case Msg.DownloadProgress:
            root.applyDownloadProgress(msg)
            return

        case Msg.Error:
            root.lastError = msg ? msg.message : "Something went wrong."
            addSourceScreen.onBackendError(root.lastError)
            seriesGridScreen.busy = false
            chapterListScreen.busy = false
            return
        }
    }

    // ---- model filling -----------------------------------------------------

    function fillSources(list) {
        sourcesModel.clear()
        for (var i = 0; i < (list ? list.length : 0); ++i) {
            var s = list[i]
            sourcesModel.append({
                "sourceId": s.id,
                "name": s.name,
                "baseUrl": s.baseUrl,
                "theme": s.theme,
                "lang": s.lang,
                "enabled": s.enabled,
                "status": s.status,
                "statusDetail": s.statusDetail ? s.statusDetail : ""
            })
        }
    }

    function fillSeries(msg) {
        seriesGridScreen.busy = false
        seriesModel.clear()
        var list = msg && msg.series ? msg.series : []
        for (var i = 0; i < list.length; ++i) {
            seriesModel.append({
                "seriesId": list[i].id,
                "title": list[i].title,
                "coverUrl": list[i].coverUrl ? list[i].coverUrl : "",
                "coverPath": ""
            })
        }
        seriesGridScreen.emptyMessage = list.length === 0
            ? "Nothing came back for that." : ""
    }

    // Covers never travel over the socket (PLAN §7.1): the backend writes a
    // downscaled copy to disk and sends its path, which is what lands here.
    function applyCover(msg) {
        if (!msg || !msg.seriesId) {
            return
        }
        for (var i = 0; i < seriesModel.count; ++i) {
            if (seriesModel.get(i).seriesId === msg.seriesId) {
                seriesModel.setProperty(i, "coverPath", "file://" + msg.path)
                return
            }
        }
    }

    function fillChapters(msg) {
        chapterListScreen.busy = false
        chaptersModel.clear()
        var list = msg && msg.chapters ? msg.chapters : []
        for (var i = 0; i < list.length; ++i) {
            chaptersModel.append({
                "chapterId": list[i].id,
                "title": list[i].title,
                "number": list[i].number,
                "published": list[i].published ? list[i].published : "",
                "scanlator": list[i].scanlator ? list[i].scanlator : "",
                "downloadState": "",
                "downloadMessage": "",
                "documentUuid": ""
            })
        }
        chapterListScreen.seriesTitle = msg && msg.series ? msg.series.title : ""
        chapterListScreen.synopsis = msg && msg.series && msg.series.description
            ? msg.series.description : ""
    }

    // The backend decides what a download looks like; this only finds the row.
    // Every sentence shown here was composed in backend/service (PLAN §2).
    function applyDownloadProgress(msg) {
        if (!msg || !msg.volumeId)
            return
        for (var i = 0; i < chaptersModel.count; ++i) {
            if (chaptersModel.get(i).chapterId !== msg.volumeId)
                continue
            chaptersModel.setProperty(i, "downloadState", msg.phase ? msg.phase : "")
            chaptersModel.setProperty(i, "downloadMessage", msg.message ? msg.message : "")
            if (msg.documentUuid)
                chaptersModel.setProperty(i, "documentUuid", msg.documentUuid)
            return
        }
    }

    // ---- navigation --------------------------------------------------------

    function openSource(sourceId, name) {
        root.currentSourceId = sourceId
        root.currentSourceName = name
        root.screen = "browse"
        seriesGridScreen.reset()
        seriesGridScreen.busy = true
        root.send(Msg.Browse, {"sourceId": sourceId, "page": 1})
    }

    function openSeries(seriesId, title) {
        root.currentSeriesId = seriesId
        root.screen = "series"
        chaptersModel.clear()
        chapterListScreen.seriesTitle = title
        chapterListScreen.synopsis = ""
        chapterListScreen.busy = true
        root.send(Msg.SeriesDetail, {"sourceId": root.currentSourceId, "seriesId": seriesId})
    }

    function goBack() {
        switch (root.screen) {
        case "series":
            root.screen = "browse"
            break
        case "browse":
        case "add":
        case "settings":
            root.screen = "sources"
            break
        default:
            root.close()
        }
    }

    function screenTitle() {
        switch (root.screen) {
        case "add": return "Add a source"
        case "browse": return root.currentSourceName
        case "series": return chapterListScreen.seriesTitle
        case "settings": return "Settings"
        }
        return "Quire"
    }

    Component.onCompleted: {
        root.send(Msg.ListSources)
        root.send(Msg.Ping)
    }

    // Called by the AppLoad host when the app is being unloaded. Terminating
    // the backend here is what stops the quired process.
    function unloading() {
        appload.terminate()
    }

    // ---- chrome ------------------------------------------------------------

    Item {
        id: header
        anchors { top: parent.top; left: parent.left; right: parent.right }
        height: Style.rowHeight

        Text {
            id: backLabel
            objectName: "backButton"
            anchors { left: parent.left; leftMargin: Style.margin; verticalCenter: parent.verticalCenter }
            text: root.screen === "sources" ? "" : "‹ Back"
            font.pointSize: Style.bodySize
            color: backArea.pressed ? Style.muted : Style.ink
        }

        MouseArea {
            id: backArea
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
            width: backLabel.text === "" ? 0 : Style.margin * 2 + backLabel.width
            onClicked: root.goBack()
        }

        Text {
            anchors.centerIn: parent
            width: parent.width - 360
            horizontalAlignment: Text.AlignHCenter
            elide: Text.ElideRight
            text: root.screenTitle()
            font.pointSize: Style.headingSize
            color: Style.ink
        }

        Text {
            id: settingsLabel
            anchors { right: parent.right; rightMargin: Style.margin; verticalCenter: parent.verticalCenter }
            text: "Settings"
            font.pointSize: Style.bodySize
            color: settingsArea.pressed ? Style.muted : Style.ink
            visible: root.screen !== "settings"
        }

        MouseArea {
            id: settingsArea
            anchors { right: parent.right; top: parent.top; bottom: parent.bottom }
            width: settingsLabel.visible ? Style.margin * 2 + settingsLabel.width : 0
            onClicked: { root.screen = "settings"; root.send(Msg.Ping) }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: Style.hairline
            color: Style.rule
        }
    }

    Item {
        id: body
        anchors { top: header.bottom; left: parent.left; right: parent.right; bottom: parent.bottom }

        SourceList {
            id: sourceListScreen
            objectName: "sourceList"
            anchors.fill: parent
            visible: root.screen === "sources"
            model: sourcesModel
            onAddRequested: { addSourceScreen.reset(); root.screen = "add" }
            onOpenRequested: root.openSource(sourceId, name)
            onToggleRequested: root.send(Msg.SetSourceEnabled, {"sourceId": sourceId, "enabled": enabled})
            onRemoveRequested: root.send(Msg.RemoveSource, {"sourceId": sourceId})
        }

        AddSource {
            id: addSourceScreen
            objectName: "addSource"
            anchors.fill: parent
            visible: root.screen === "add"
            onProbeRequested: root.send(Msg.ProbeSource, {"url": url})
            onAnswerRequested: root.send(Msg.ProbeAnswer, {"id": answerId})
            onConfirmRequested: {
                root.send(Msg.ConfirmAddSource, {"url": url, "theme": theme, "name": name, "lang": lang})
                root.screen = "sources"
            }
            onDoneRequested: root.screen = "sources"
        }

        SeriesGrid {
            id: seriesGridScreen
            objectName: "seriesGrid"
            anchors.fill: parent
            visible: root.screen === "browse"
            model: seriesModel
            onSearchRequested: {
                seriesGridScreen.busy = true
                root.send(Msg.Search, {"sourceId": root.currentSourceId, "query": query, "page": 1})
            }
            onBrowseRequested: {
                seriesGridScreen.busy = true
                root.send(Msg.Browse, {"sourceId": root.currentSourceId, "page": 1})
            }
            onCoverRequested: root.send(Msg.RequestCover,
                {"sourceId": root.currentSourceId, "seriesId": seriesId, "url": url})
            onOpenRequested: root.openSeries(seriesId, title)
        }

        ChapterList {
            id: chapterListScreen
            objectName: "chapterList"
            anchors.fill: parent
            visible: root.screen === "series"
            model: chaptersModel
            onDownloadRequested: root.send(Msg.EnqueueDownload,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId, "volumeId": chapterId})
        }

        Settings {
            id: settingsScreen
            objectName: "settings"
            anchors.fill: parent
            visible: root.screen === "settings"
            backendStatus: root.backendStatus
            lastError: root.lastError
            onPingRequested: root.send(Msg.Ping)
            onClearErrorRequested: root.lastError = ""
        }
    }
}
