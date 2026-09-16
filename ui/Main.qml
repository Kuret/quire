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
import "Watch.js" as WatchJs

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

    // PLAN §12.2's at-a-glance summary, composed in the backend and shown as
    // it arrived: "short" on the entry point, "phrase" on the watched screen.
    // Empty means there is nothing to report, and nothing is what is shown —
    // an indicator that is always lit teaches people to ignore it.
    property string watchShort: ""
    property string watchPhrase: ""

    // Which screen the open series was reached from, so Back goes where the
    // user came from rather than always to the grid they may never have seen.
    property string seriesCameFrom: "browse"

    // Backend status, shown on the settings screen.
    property string backendStatus: "Not yet asked."

    // The one error worth showing: whatever the backend last complained about.
    property string lastError: ""

    // A quiet, dismissible line from the backend — currently only "Quire closed
    // unexpectedly last time". Not an error and not a dialogue: there is
    // nothing for the user to do, and a modal on launch would be worse than the
    // silence it replaces.
    property string notice: ""

    // The native reader handoff lives behind a Loader so that its imports of
    // xochitl's own QML singletons cannot take the app down with them: if they
    // ever stop resolving, status goes to Loader.Error and "Read" degrades to
    // telling the user where the file is (PLAN §6 M6).
    Loader {
        id: readerHandoff
        source: "ReaderHandoff.qml"
        asynchronous: false
    }

    function openInReader(documentUuid) {
        if (!documentUuid)
            return
        var opened = readerHandoff.status === Loader.Ready && readerHandoff.item
                     ? readerHandoff.item.open(documentUuid, -1)
                     : false
        if (!opened) {
            // Tell the backend, which owns both the record and the wording.
            root.send(Msg.OpenInReader, {"documentUuid": documentUuid, "missing": true})
            root.forgetDocument(documentUuid)
            return
        }
        root.send(Msg.OpenInReader, {"documentUuid": documentUuid})
    }

    // deleteDownload moves a document to xochitl's Trash and tells the backend
    // what happened (PLAN §12.4).
    //
    // The three answers are three different situations and none of them may be
    // guessed at. "ok" is the delete; "gone" means the document was already off
    // the tablet, which is exactly the missing-document case §6 M6 already
    // answers, wording and all; "failed" means nothing moved, and saying so is
    // what stops the backend forgetting a record whose document is still there.
    function deleteDownload(documentUuid) {
        if (!documentUuid)
            return
        var result = readerHandoff.status === Loader.Ready && readerHandoff.item
                     ? readerHandoff.item.trash(documentUuid)
                     : "failed"
        if (result === "gone") {
            root.send(Msg.OpenInReader, {"documentUuid": documentUuid, "missing": true})
            root.forgetDocument(documentUuid)
            return
        }
        // "kept" is a delete that happened with a Trash that did not empty, so
        // it is reported as trashed — the download really is gone — with the
        // emptying reported separately. Calling it a failure would tell the
        // user their download survived when it did not.
        root.send(Msg.DeleteDownload, {
            "documentUuid": documentUuid,
            "confirmed": true,
            "trashed": result === "ok" || result === "kept",
            "emptied": result === "ok"})
    }

    // forgetDocument clears a dead UUID off every row that carried it, so the
    // button goes back to offering a download straight away.
    //
    // Both models, because both can be showing the document: a volume row
    // offers Read on the one document holding the whole volume, and a row still
    // offering to open something that is in the Trash is the same lie as a
    // chapter row doing it.
    function forgetDocument(documentUuid) {
        for (var i = 0; i < chaptersModel.count; ++i) {
            if (chaptersModel.get(i).documentUuid === documentUuid) {
                chaptersModel.setProperty(i, "documentUuid", "")
                chaptersModel.setProperty(i, "downloadState", "")
                chaptersModel.setProperty(i, "downloadMessage", "")
            }
        }
        for (var j = 0; j < volumesModel.count; ++j) {
            if (volumesModel.get(j).documentUuid === documentUuid)
                volumesModel.setProperty(j, "documentUuid", "")
        }
    }

    ListModel { id: sourcesModel }
    ListModel { id: seriesModel }
    ListModel { id: chaptersModel }

    // The volume view's rows (PLAN §6 M4, revised 2026-09-16). It is empty
    // whenever the backend sent no volumes, which is how the chapter screen
    // knows there is no second view to offer.
    ListModel { id: volumesModel }

    ListModel { id: watchedModel }

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
            // The backend composes the sentence (PLAN §2); this only decides
            // that it is worth one quiet line rather than a dialogue.
            if (msg && msg.notice)
                root.notice = msg.notice
            if (msg && msg.logTail)
                settingsScreen.logLines = msg.logTail
            // A plain bool, not the status object: the status is a fresh object
            // on every Pong, so assigning that would redraw the toggle each
            // time the settings screen pings. Assigning an identical bool emits
            // no change signal and nothing is repainted.
            settingsScreen.consultRobots = msg ? !!msg.consultRobots : false
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

        case Msg.WatchList:
            root.reconcileWatched(msg)
            return

        case Msg.WatchUpdate:
            root.applyWatchUpdate(msg ? msg.watch : null)
            return

        case Msg.DownloadProgress:
            root.applyDownloadProgress(msg)
            return

        case Msg.CacheStatus:
            // The size, and after a clear the sentence about what went. Both
            // land in the same place: the screen shows one line about the
            // cache, and it is always the most recent true thing said about it.
            settingsScreen.cacheSummary = msg && msg.message ? msg.message : ""
            settingsScreen.closeCacheQuestion()
            return

        case Msg.CacheConfirm:
            settingsScreen.cacheQuestion = msg && msg.message ? msg.message : ""
            return

        case Msg.QueueResult:
            // Only says anything when something did not fit. Every row that did
            // says "Queued." for itself, and a summary of what the list already
            // shows is a sentence that teaches people to skip sentences.
            //
            // With nothing asked before a selection is queued, this is the only
            // place the user hears about a selection the queue could not take.
            if (msg && msg.message)
                root.lastError = msg.message
            return

        case Msg.DeleteConfirm:
            // The backend's question, put where questions are asked. The strip
            // carries the document's UUID, because that is what the answer
            // deletes (PLAN §12.4).
            if (msg && msg.documentUuid) {
                chapterListScreen.confirmingKind = "delete"
                chapterListScreen.confirmingId = msg.documentUuid
                chapterListScreen.confirmingMessage = msg.message ? msg.message : ""
            }
            return

        case Msg.DownloadDeleted:
            // The backend has forgotten it, so the rows can. This is the only
            // cue that clears them: the store deciding, not the trash call
            // returning true.
            root.forgetDocument(msg ? msg.documentUuid : "")
            return

        case Msg.Error:
            // A delete question left open over an answer that went wrong would
            // invite tapping it again. Only that one: a download confirmation
            // is about a different row and is not what failed.
            if (chapterListScreen.confirmingKind === "delete")
                chapterListScreen.closeConfirm()
            // A question whose answer failed is a question to put away: leaving
            // it open invites tapping it again.
            settingsScreen.closeCacheQuestion()
            root.lastError = msg ? msg.message : "Something went wrong."
            addSourceScreen.onBackendError(root.lastError)
            seriesGridScreen.busy = false
            // The turn did not happen. The pager goes back to naming the page
            // still on screen rather than one that never arrived.
            seriesGridScreen.pendingPage = 0
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
                // The backend resolves "unset" to "auto", so this is never
                // blank; the fallback is for a reply from an older backend.
                "splitStrips": s.splitStrips ? s.splitStrips : "auto",
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
        // Where the backend says we are. It clamps a page that ran past the
        // end, so this is the page actually served rather than the one asked
        // for, and totalPages is 0 until the source has genuinely run out
        // (PLAN §12.1 — do not invent a total).
        seriesGridScreen.page = msg && msg.page > 0 ? msg.page : 1
        seriesGridScreen.totalPages = msg && msg.totalPages > 0 ? msg.totalPages : 0
        seriesGridScreen.hasMore = msg ? !!msg.hasMore : false
        seriesGridScreen.pendingPage = 0
        seriesGridScreen.emptyMessage = list.length === 0
            ? "Nothing came back for that." : ""
        // One batch for the page that just landed. The backend drops whatever
        // the previous page left in flight.
        seriesGridScreen.requestVisibleCovers()
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
                "downloadState": list[i].documentUuid ? "done" : "",
                "downloadMessage": "",
                "documentUuid": list[i].documentUuid ? list[i].documentUuid : ""
            })
        }
        volumesModel.clear()
        var vols = msg && msg.volumes ? msg.volumes : []
        for (var v = 0; v < vols.length; ++v) {
            volumesModel.append({
                // chapterId, not volumeId: a download request names the chapter
                // the user tapped and the backend works out the volume around
                // it. The row carries the first chapter of its volume.
                "chapterId": vols[v].id,
                "title": vols[v].title,
                "detail": vols[v].detail ? vols[v].detail : "",
                "chapterCount": vols[v].chapterCount,
                "downloadState": vols[v].documentUuid ? "done" : "",
                "downloadMessage": "",
                "documentUuid": vols[v].documentUuid ? vols[v].documentUuid : ""
            })
        }

        chapterListScreen.seriesTitle = msg && msg.series ? msg.series.title : ""
        chapterListScreen.synopsis = msg && msg.series && msg.series.description
            ? msg.series.description : ""

        // The rows are new objects even when they describe the same chapters,
        // so anything picked before this refill has to be checked against what
        // is actually on screen now.
        chapterListScreen.pruneSelection()
    }

    // ---- watched series (PLAN §12.2) ---------------------------------------
    //
    // Every string on a watch row — the badge, the status line, the failure
    // detail — was composed in backend/service/watch.go and is stored and drawn
    // as it arrived. Nothing here pluralises, formats a date or maps a state to
    // words (PLAN §2). The model bookkeeping is in Watch.js, which is where it
    // can be driven by the offscreen harness.

    function reconcileWatched(msg) {
        WatchJs.reconcile(watchedModel, msg ? msg.watched : [])
        // Written only where the string differs, so a check round that ends
        // with the same summary it began with repaints nothing.
        WatchJs.applySummary(root, msg)
        root.refreshWatchedFlag()
    }

    function applyWatchUpdate(w) {
        WatchJs.applyUpdate(watchedModel, w)
        root.refreshWatchedFlag()
    }

    // The series screen's button follows the store, never a local toggle: PLAN
    // §12.2 has the backend clear a badge when a series is opened and push the
    // result, and a view that guessed would disagree with it after a failed
    // round trip.
    function refreshWatchedFlag() {
        chapterListScreen.watched = WatchJs.indexOf(
            watchedModel, root.currentSourceId, root.currentSeriesId) >= 0
    }

    // The backend decides what a download looks like; this only finds the row.
    // Every sentence shown here was composed in backend/service (PLAN §2).
    function applyDownloadProgress(msg) {
        if (!msg || !msg.volumeId)
            return

        // Which view asked. A volume's id is its first chapter's, so the same
        // id appears in both models and the backend says which it meant. An
        // absent grouping is a chapter, the default (PLAN §6 M4).
        var target = msg.grouping === "volume" ? volumesModel : chaptersModel
        applyProgressToModel(target, msg)

        // The confirm phase is a question, and the strip is where it is
        // asked. Any other phase is an answer, so the strip closes. The
        // question travels with the id because the strip is drawn over the
        // foot of the list rather than inside the row (PLAN §12.1).
        if (msg.phase === "confirm") {
            chapterListScreen.confirmingId = msg.volumeId
            chapterListScreen.confirmingMessage = msg.message ? msg.message : ""
        } else if (chapterListScreen.confirmingId === msg.volumeId) {
            chapterListScreen.confirmingId = ""
            chapterListScreen.confirmingMessage = ""
        }
    }

    function applyProgressToModel(rows, msg) {
        for (var i = 0; i < rows.count; ++i) {
            if (rows.get(i).chapterId !== msg.volumeId)
                continue
            rows.setProperty(i, "downloadState", msg.phase ? msg.phase : "")
            // A stopped download returns the row to where it started: the
            // message would otherwise sit there looking like progress that has
            // frozen (PLAN §7.1).
            rows.setProperty(i, "downloadMessage",
                             msg.phase === "cancelled" ? "" : (msg.message ? msg.message : ""))
            if (msg.documentUuid)
                rows.setProperty(i, "documentUuid", msg.documentUuid)
            return
        }
    }

    // ---- navigation --------------------------------------------------------

    // requestSeriesPage asks for one display page of the listing the grid is
    // showing. The page size is the grid's, worked out from its own viewport —
    // the backend pages a cache of source pages against it, so this is usually
    // not an HTTP request at all (PLAN §12.1).
    function requestSeriesPage(page) {
        var req = {
            "sourceId": root.currentSourceId,
            "page": page,
            "pageSize": seriesGridScreen.pageSize
        }
        if (seriesGridScreen.query.length > 0) {
            req.query = seriesGridScreen.query
            root.send(Msg.Search, req)
        } else {
            root.send(Msg.Browse, req)
        }
    }

    function openSource(sourceId, name) {
        root.currentSourceId = sourceId
        root.currentSourceName = name
        root.screen = "browse"
        seriesGridScreen.reset()
        seriesGridScreen.busy = true
        seriesGridScreen.pendingPage = 1
        root.requestSeriesPage(1)
    }

    function openSeries(seriesId, title) {
        root.currentSeriesId = seriesId
        root.seriesCameFrom = root.screen === "watching" ? "watching" : "browse"
        root.screen = "series"
        chaptersModel.clear()
        // Emptied before the new series' detail arrives, so the previous
        // series' volume view is never on screen over this one's chapters.
        volumesModel.clear()
        chapterListScreen.view = "chapters"
        chapterListScreen.seriesTitle = title
        chapterListScreen.synopsis = ""
        chapterListScreen.page = 1
        chapterListScreen.closeConfirm()
        chapterListScreen.leaveSelection()
        chapterListScreen.busy = true
        root.refreshWatchedFlag()
        root.send(Msg.SeriesDetail, {"sourceId": root.currentSourceId, "seriesId": seriesId})
    }

    function goBack() {
        switch (root.screen) {
        case "series":
            root.screen = root.seriesCameFrom
            break
        case "watching":
            root.screen = "sources"
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
        case "watching": return "Watching"
        case "series": return chapterListScreen.seriesTitle
        case "settings": return "Settings"
        }
        return "Quire"
    }

    Component.onCompleted: {
        // These are belt and braces, not the mechanism.
        //
        // AppLoad discards messages aimed at a backend whose socket is not yet
        // up ("No active socket for ID:quire"), and this runs at exactly the
        // moment that race is live. The backend therefore *pushes* the source
        // list and the status when it sees the frontend attach, which is the
        // earliest point a send can work. Asking here as well costs two frames
        // and covers the case where the backend was already attached before
        // this QML loaded.
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
            onWatchingRequested: root.screen = "watching"
            watchingLabel: root.watchShort
            onOpenRequested: root.openSource(sourceId, name)
            notice: root.notice
            onNoticeDismissed: root.notice = ""
            onToggleRequested: root.send(Msg.SetSourceEnabled, {"sourceId": sourceId, "enabled": enabled})
            onRemoveRequested: root.send(Msg.RemoveSource, {"sourceId": sourceId})
            onRenameRequested: root.send(Msg.RenameSource, {"sourceId": sourceId, "name": name})
            onSplitStripsRequested: root.send(Msg.SetSourceSplitStrips,
                                              {"sourceId": sourceId, "splitStrips": mode})
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

        WatchList {
            id: watchListScreen
            objectName: "watchList"
            anchors.fill: parent
            visible: root.screen === "watching"
            model: watchedModel
            phrase: root.watchPhrase
            onCheckRequested: root.send(Msg.CheckWatched, {})
            onUnwatchRequested: root.send(Msg.UnwatchSeries,
                {"sourceId": sourceId, "seriesId": seriesId})
            onOpenRequested: {
                root.currentSourceId = sourceId
                root.currentSourceName = sourceName
                root.openSeries(seriesId, title)
            }
        }

        SeriesGrid {
            id: seriesGridScreen
            objectName: "seriesGrid"
            anchors.fill: parent
            visible: root.screen === "browse"
            model: seriesModel
            onSearchRequested: {
                seriesGridScreen.query = query
                seriesGridScreen.busy = true
                seriesGridScreen.pendingPage = 1
                root.requestSeriesPage(1)
            }
            onBrowseRequested: {
                seriesGridScreen.busy = true
                seriesGridScreen.pendingPage = 1
                root.requestSeriesPage(1)
            }
            // The grid asks for a page; the screen already knows whether it is
            // showing a search or the site's own listing, and the page size is
            // its geometry's answer, not a constant (PLAN §12.1).
            onPageRequested: root.requestSeriesPage(page)
            onCoversRequested: root.send(Msg.RequestCover,
                {"sourceId": root.currentSourceId, "covers": covers})
            onOpenRequested: root.openSeries(seriesId, title)
        }

        ChapterList {
            id: chapterListScreen
            objectName: "chapterList"
            anchors.fill: parent
            visible: root.screen === "series"
            model: chaptersModel
            volumeModel: volumesModel
            onWatchRequested: root.send(Msg.WatchSeries,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId,
                 "title": chapterListScreen.seriesTitle})
            onUnwatchRequested: root.send(Msg.UnwatchSeries,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId})
            onDownloadRequested: root.send(Msg.EnqueueDownload,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId, "volumeId": chapterId})
            onDownloadCancelled: root.send(Msg.CancelDownload,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId,
                 "volumeId": chapterId})
            onDownloadConfirmed: root.send(Msg.EnqueueDownload,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId,
                 "volumeId": chapterId, "confirmed": true})

            // The volume view's three. The grouping rides on the request
            // because it is situational — whether the user is about to be
            // without a connection — and not a property of the source.
            onVolumeDownloadRequested: root.send(Msg.EnqueueDownload,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId,
                 "volumeId": chapterId, "grouping": "volume"})
            onVolumeDownloadCancelled: root.send(Msg.CancelDownload,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId,
                 "volumeId": chapterId, "grouping": "volume"})
            onVolumeDownloadConfirmed: root.send(Msg.EnqueueDownload,
                {"sourceId": root.currentSourceId, "seriesId": root.currentSeriesId,
                 "volumeId": chapterId, "grouping": "volume", "confirmed": true})

            onReadRequested: root.openInReader(documentUuid)

            // Step one asks the backend for the question; step two does the
            // deleting. Both go through root so the QML that touches xochitl
            // stays behind the one Loader.
            onDeleteRequested: root.send(Msg.DeleteDownload, {"documentUuid": documentUuid})
            onDeleteConfirmed: root.deleteDownload(documentUuid)

            // A selection of rows, queued in one message and without a
            // question. The grouping rides on it exactly as it does on a single
            // download, because the two lists hold different things.
            onQueueConfirmed: root.send(Msg.EnqueueDownloads, {
                "sourceId": root.currentSourceId, "seriesId": root.currentSeriesId,
                "chapterIds": chapterIds, "grouping": volumes ? "volume" : "chapter"})
        }

        Settings {
            id: settingsScreen
            objectName: "settings"
            anchors.fill: parent
            visible: root.screen === "settings"
            backendStatus: root.backendStatus
            lastError: root.lastError
            onPingRequested: root.send(Msg.Ping)
            // The log rides on Ping with a flag rather than taking a message
            // type of its own: the viewer is a panel on this screen, and this
            // screen already pings.
            onLogRequested: root.send(Msg.Ping, {"log": true})
            // Set it, then ask for the status: the toggle draws what the store
            // says rather than what this screen hoped, so a setting that failed
            // to save cannot leave the switch showing the wrong thing.
            onConsultRobotsRequested: {
                root.send(Msg.SetConsultRobots, {"consultRobots": on})
                root.send(Msg.Ping)
            }
            onClearErrorRequested: root.lastError = ""

            // The cache: its size on arrival, and two steps to clear it. Every
            // sentence comes back from the backend (PLAN §2), including the
            // size, so nothing here formats a number.
            onCacheSizeRequested: root.send(Msg.GetCacheSize)
            onClearCacheRequested: root.send(Msg.ClearCache)
            onClearCacheConfirmed: root.send(Msg.ClearCache, {"confirmed": true})
        }
    }
}
