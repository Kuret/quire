// Quire — the app shell.
//
// PLAN §2: the QML frontend is a dumb view. It owns no logic beyond sending
// messages and rendering the replies, because QML is the layer that breaks on
// an OS update and every line here is a line to re-check after one.
//
// Concretely, this file holds the models the backend fills, the Backend
// element that fills them, and the navigation between screens. Nothing in ui/
// decides what a verdict means, whether a source may be added, or what a row
// says: all of that arrives as data (see backend/service).
//
// PLAN §6 M3 UI rules, which every file in ui/ follows:
//   - No animations. E-ink ghosting.
//   - Match the stock UI palette, and spend colour only where it carries
//     meaning. Do not invent a brand. (ui/Style.js)
//
//     This used to read "do not invent a brand" full stop, on the premise that
//     the panel was greyscale. It is not: this is a Paper Pro and its Gallery 3
//     display is colour, which is why there is now exactly one accent
//     (Style.accent) and why it is rationed — colour regions refresh slower and
//     ghost harder than black on white. It marks live state, what is currently
//     active, and screen titles; it never carries a meaning on its own, because
//     every place it appears already says the same thing in shape or words.
//
//   - **The accent is only ever a solid area — never a glyph, never a
//     hairline, never a thin stroke.** Measured on the device: coloured text
//     read muddy, because Gallery 3 draws black at the panel's full resolution
//     and composes colour through its filter array at a fraction of it, and a
//     letterform is almost entirely edge. So every word in ui/ is Style.ink
//     and the accent is a bar, a square or a fill. The full measurement, and
//     the contrast pairs that follow from it, are in ui/Style.js.
//
// **This runs under Annex, not AppLoad.** The host loads this file from disk
// by path — there is no resource bundle — and calls unloading() on the root
// element when the app is closed, which is the one part of AppLoad's contract
// Annex kept verbatim. The message protocol is also unchanged; only the
// transport underneath it moved, from a unix socket to loopback HTTP, for the
// reasons in Annex's DESIGN.md §5.

import QtQuick 2.5
import "../../../lib"
import "Messages.js" as Msg
import "Style.js" as Style
import "Watch.js" as WatchJs
import "Screens.js" as Screens
import "Answers.js" as Answers
import "Views.js" as Views
import "Grouping.js" as Grouping
import "Kinds.js" as Kinds
import "Covers.js" as Covers

Rectangle {
    id: root
    anchors.fill: parent
    color: Style.paper

    // The host connects to this and closes the frontend. Annex kept AppLoad's
    // contract here verbatim, so the signal is unchanged.
    signal close

    // Which screen is showing: "sources", "add", "browse", "searchall",
    // "series" or "settings". A plain string rather than a stack, because the
    // whole app is a handful of screens and a StackView would be a dependency
    // for nothing.
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

    // The layout each of the three screens is stored in (PLAN §7.1 type 75).
    //
    // Plain strings rather than the status object, for the reason the robots
    // switch is a plain bool: the status is a fresh object on every Pong, so
    // binding a screen to it would redraw the whole layout every time anything
    // pings. Assigning a string a screen already holds emits no change signal
    // and nothing is repainted.
    //
    // They start at "grid" and are never empty, which is what lets a screen be
    // drawn before the first status arrives — the frontend pings on startup,
    // but the user can be standing on Downloaded before the answer lands.
    property string searchView: Views.GRID
    property string downloadedView: Views.GRID
    property string watchingView: Views.GRID

    // viewFor is the stored layout of a screen, by the screen's own name.
    function viewFor(name) {
        switch (name) {
        // One stored layout for both searches: a combined search is a search
        // (Views.js wireName).
        case "browse":
        case "searchall": return root.searchView
        case "downloaded": return root.downloadedView
        case "watching": return root.watchingView
        }
        return Views.GRID
    }

    // setView asks for a layout, then asks for the status.
    //
    // Exactly the robots switch's shape (ui/Settings.qml): the value comes back
    // on the Pong rather than in a reply of its own, so the screen draws what
    // the store says rather than what the toggle hoped. A setting that failed
    // to save cannot leave the switch and the layout disagreeing.
    function setView(name, view) {
        var screen = Views.wireName(name)
        if (!screen)
            return
        root.send(Msg.SetView, {"screen": screen, "view": view})
        root.send(Msg.Ping)
    }

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

        // The handoff is a QtObject and so is not in the scene. Finding the
        // stock reader means walking the object tree — `Global.documentViewLoader`
        // is gone on 3.28 — and it needs something that *is* in the tree to
        // start from. This root is it.
        onLoaded: if (item) item.anchor = root
    }

    // The documents the open question is about, straight from the backend's
    // MessageDeleteSeriesConfirm. Held here rather than worked out in the view:
    // which documents belong to a series is the backend's answer (PLAN §2), and
    // a second answer computed from the rows would be a second answer.
    property var confirmingSeriesUuids: []

    // deleteSeries deletes every download of one series and reports each one.
    //
    // Nothing is guessed at. Every document gets its own result, because each
    // fails on its own, and the backend composes the "five of seven" sentence
    // from exactly those results.
    function deleteSeries(sourceId, seriesId) {
        var uuids = root.confirmingSeriesUuids
        root.confirmingSeriesUuids = []
        if (!uuids || !uuids.length)
            return

        // With no bridge — or one that threw — nothing was deleted, and every
        // document says so rather than the whole thing reporting one vague
        // failure.
        var outcome = Answers.trashedMany(root.bridge(), uuids)

        for (var i = 0; i < outcome.results.length; ++i)
            root.forgetDocument(outcome.results[i].documentUuid)

        root.send(Msg.DeleteSeries, {
            "sourceId": sourceId,
            "seriesId": seriesId,
            "confirmed": true,
            "results": outcome.results,
            "detail": outcome.detail ? outcome.detail : ""})
    }

    // deleteFolder removes the now-empty series folder the backend asked about.
    //
    // A folder is an entry like any other to this API, so it goes through the
    // same trash-then-delete as a document — the `quiredelete` probe created
    // folders in Comics and removed them exactly this way.
    //
    // The name is echoed back untouched. The backend composed it and will put
    // it in a sentence (PLAN §2); by the time this answer arrives the records
    // it came from are gone.
    function deleteFolder(folderId, folderName) {
        if (!folderId)
            return
        var answer = Answers.trashed(root.bridge(), folderId)
        root.send(Msg.FolderDeleted, {
            "folderId": folderId,
            "folderName": folderName ? folderName : "",
            "trashed": answer.result === "ok" || answer.result === "kept",
            "removed": answer.result === "ok",
            "detail": answer.detail})
    }

    // dismissInput asks every screen that owns a text field to put the keyboard
    // away. Three calls rather than a search for inputs: a helper that went
    // looking would eventually find one the caller did not mean.
    function dismissInput() {
        addSourceScreen.dismissInput()
        seriesGridScreen.dismissInput()
        searchAllScreen.dismissInput()
        sourceListScreen.dismissInput()
    }

    // bridge is ReaderHandoff.qml, or null when its imports of xochitl's own QML
    // did not resolve. Every handler below goes through it, and through
    // Answers.js, so that "the bridge is missing" and "the bridge threw" both
    // end in a reply rather than in silence.
    function bridge() {
        return readerHandoff.status === Loader.Ready && readerHandoff.item
               ? readerHandoff.item : null
    }

    function openInReader(documentUuid) {
        if (!documentUuid)
            return
        // The reply and whether the rows must forget this document — one
        // decision, made in Answers.js where the harness can drive it.
        var answer = Answers.handoff(root.bridge(), documentUuid, -1)

        // Tell the backend, which owns both the record and the wording.
        root.send(Msg.OpenInReader, answer.reply)
        if (answer.forget)
            root.forgetDocument(documentUuid)

        // The frontend stays up, and under Annex that is a guarantee rather
        // than a happy accident.
        //
        // An Annex app is parented inside the navigator, so the document view
        // — later in MainView — draws over it. Opening a comic brings the
        // reader forward and leaves Quire exactly where it was underneath, so
        // closing the comic returns the user to the chapter list they tapped
        // Read from.
        //
        // This is the whole reason Annex exists. AppLoad v0.5.0 happened to
        // behave this way; v0.5.1+ parents an app's window *above* the
        // document, which is why closing the app had to be invented to
        // imitate it, and why that imitation dropped the user at the library
        // instead of back in Quire. See PLAN §6 M6's footnote and Annex's
        // DESIGN.md §1.
    }

    // deleteDownload moves a document to xochitl's Trash and tells the backend
    // what happened (PLAN §12.4).
    //
    // The four answers are four different situations and none of them may be
    // guessed at. "ok" is the delete; "gone" means the document was already off
    // the tablet, which is exactly the missing-document case §6 M6 already
    // answers, wording and all; "failed" means nothing moved, and saying so is
    // what stops the backend forgetting a record whose document is still there;
    // "kept" is below.
    function deleteDownload(documentUuid) {
        if (!documentUuid)
            return
        var answer = Answers.trashed(root.bridge(), documentUuid)
        var result = answer.result
        if (result === "gone") {
            root.send(Msg.OpenInReader, {"documentUuid": documentUuid, "missing": true})
            root.forgetDocument(documentUuid)
            return
        }
        // "kept" is a delete that happened, with the document left sitting in
        // the Trash because the second step did not take. It is reported as
        // trashed — the download really is out of the library — with the
        // removal reported separately. Calling it a failure would tell the
        // user their download survived when it did not.
        root.send(Msg.DeleteDownload, {
            "documentUuid": documentUuid,
            "confirmed": true,
            "trashed": result === "ok" || result === "kept",
            "removed": result === "ok",
            "detail": answer.detail})
    }

    // sortDownload puts a finished download in its series folder and tells the
    // backend what happened (PLAN §6 M5, corrected 2026-09-17).
    //
    // Every way this can fail ends the same way: the documents stay in Comics,
    // which is where every download went before this existed. The backend is
    // told so it can stop recording a folder that is not there — never so it
    // can call the download a failure.
    function sortDownload(msg) {
        if (!msg || !msg.documentUuids || !msg.documentUuids.length)
            return
        // The bridge is the one file that imports xochitl's QML, so a future OS
        // closing that door — or a call inside it throwing, which is what 3.27
        // did — takes sorting with it and nothing else (PLAN §6 M6). Either way
        // the backend gets an answer.
        var answer = Answers.sorted(root.bridge(), msg)
        answer.sourceId = msg.sourceId
        answer.seriesId = msg.seriesId
        // Echoed straight off the request, the same as sourceId and seriesId:
        // it says which of the two shapes of ask this was (a per-series
        // folder, or the one-level Books folder), and Sorting.js has no reason
        // to know or care about that distinction itself.
        answer.kind = msg.kind ? msg.kind : ""
        root.send(Msg.DocumentsSorted, answer)
    }

    // checkDocuments answers the backend's reconcile question (PLAN §12.4).
    //
    // **The failure answer is explicit.** With no bridge there is no way to ask
    // xochitl anything, and the honest reply is "I could not check" — never an
    // empty list of missing documents, which the backend would otherwise be
    // entitled to read as "none of them exist" and act on by deleting every
    // record and the whole page cache.
    function checkDocuments(msg) {
        var uuids = msg && msg.documentUuids ? msg.documentUuids : []
        root.send(Msg.DocumentsChecked, Answers.checked(root.bridge(), uuids))
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

    // The backend's line for an empty library, shown as it arrived.
    property string downloadedEmpty: ""

    function fillDownloaded(msg) {
        downloadedModel.clear()
        var rows = msg && msg.series ? msg.series : []
        for (var i = 0; i < rows.length; ++i) {
            var r = rows[i]
            downloadedModel.append({
                "sourceId": r.sourceId ? r.sourceId : "",
                "sourceName": r.sourceName ? r.sourceName : "",
                "seriesId": r.seriesId ? r.seriesId : "",
                "title": r.title ? r.title : "",
                "detail": r.detail ? r.detail : "",
                "openable": r.openable ? true : false,
                "note": r.note ? r.note : "",
                // The cover the source publishes, for the grid layout. Often
                // empty: a series downloaded before covers were stored has
                // none, and the tile is a titled placeholder rather than a
                // blank square.
                "coverUrl": r.coverUrl ? r.coverUrl : "",
                // Whether this row is a book. Carried so the series screen can
                // be told what it is showing by the row that opened it — the
                // detail reply says nothing about kind (ui/Kinds.js) — and
                // written on every row because a ListModel fixes its roles on
                // the first append and drops keys added later.
                "kind": Kinds.of(r),
                // The word a book wears on this screen, in both layouts: the
                // tile's badge corner reads it directly, and the row's subtitle
                // line puts it in front of the source name
                // (ui/DownloadedList.qml subtitleOf). Composed once, here, so
                // "absent means manga" is decided in exactly one place.
                "badge": Kinds.mark(r),
                // Filled in later, when MessageCoverReady lands. The role has
                // to exist from the first append or that write is dropped.
                "coverPath": "",
                // The newest download of this series. Nothing on this screen
                // uses it yet — the grid is navigation only for now — and it
                // is carried because it is what a "Read the latest one" action
                // needs and because a row is filled in one place. The
                // long-press menu's "Read latest" is that action.
                "latestUuid": r.latestUuid ? r.latestUuid : "",
                // The menu's Watch / Stop watching line, from the store. A
                // row here is a (source, series) pair, so it answers for
                // itself rather than for the source being browsed.
                "watched": WatchJs.indexOf(watchedModel, r.sourceId ? r.sourceId : "",
                                           r.seriesId ? r.seriesId : "") >= 0})
        }
        root.downloadedEmpty = msg && msg.empty ? msg.empty : ""
        downloadedListScreen.page = 1
        downloadedListScreen.requestVisibleCovers()
    }

    ListModel { id: sourcesModel }
    // The private source list's own model, filled only from
    // Msg.PrivateSources — never from Msg.Sources, and never by filtering
    // sourcesModel here. The backend already partitions the two lists
    // (service.buildSourceViews); a second partition in the view would be
    // exactly the "filtering in the UI" the design forbids for the combined
    // search, applied to the list a bug there would be just as able to leak.
    ListModel { id: privateSourcesModel }
    ListModel { id: seriesModel }

    // One page of the combined search's groups (Msg.SearchAllResults). A row
    // is a series as the user sees it, merged across sources by the backend.
    ListModel { id: searchAllModel }

    // Every group's matches on the page in hand, keyed by group. Kept beside
    // the model rather than inside it: a nested list in a ListModel role is a
    // second model to keep in step, and this one is read exactly twice — when
    // a group is opened, and when the series screen's chips are built from it.
    //
    // It is replaced wholesale with each page, so a key from a page that has
    // been turned away from resolves to nothing rather than to the wrong
    // sources (Grouping.js matchesFor).
    property var searchAllMatches: ({})

    // The last page of combined results exactly as it arrived, so the kind
    // filter can redraw it without asking every source again. Null until a
    // search has come back, and emptied when the screen is opened fresh: a
    // filter tapped on an empty screen must not repopulate it with the answer
    // to somebody's earlier question.
    property var lastSearchAll: null
    ListModel { id: chaptersModel }

    // The volume view's rows (PLAN §6 M4, revised 2026-09-16). It is empty
    // whenever the backend sent no volumes, which is how the chapter screen
    // knows there is no second view to offer.
    ListModel { id: volumesModel }

    ListModel { id: watchedModel }

    // The downloaded overview's rows (PLAN §12.5). Filled from the backend when
    // the screen opens; there is no push, so a download or a delete shows up
    // the next time it is opened.
    ListModel { id: downloadedModel }

    // ---- transport ---------------------------------------------------------

    Backend {
        id: backend
        // Must equal the "id" field in manifest.json, and the systemd
        // instance name: it is what names the endpoint file this reads to
        // find the backend's port.
        appId: "quire"

        onMessage: (type, data, b64) => root.dispatch(type, data)

        // A send that did not reach the backend must not be silent. Under
        // AppLoad the backend died with the app, so "not running" was not a
        // state the user could be in; under Annex it is a service that can be
        // stopped or crash-looping while the app is perfectly happy, and a UI
        // that just sits there is the worst way to show that.
        onFailed: (type, reason) => { root.lastError = reason }
    }

    // send is the only way anything in ui/ talks to the backend. Payloads are
    // always JSON objects: PLAN §7.1 says all payloads are JSON, and a
    // non-empty payload costs nothing now that the wire is HTTP — it was
    // load-bearing under AppLoad, whose empty-packet handling was asymmetric.
    function send(type, payload) {
        backend.send(type, JSON.stringify(payload === undefined ? {} : payload))
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
            // The stored layouts ride on the same status and for the same
            // reason. Views.js applies the default of grid, so a backend too
            // old to send them — or a status that arrived without them — leaves
            // every screen drawable.
            root.searchView = Views.fromStatus(msg, "browse")
            root.downloadedView = Views.fromStatus(msg, "downloaded")
            root.watchingView = Views.fromStatus(msg, "watching")
            return

        case Msg.Sources:
            root.fillSources(msg ? msg.sources : [])
            return

        case Msg.PrivateSources:
            root.fillPrivateSources(msg ? msg.sources : [])
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

        case Msg.SearchAllResults:
            root.fillSearchAll(msg)
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

        case Msg.DownloadedList:
            root.fillDownloaded(msg)
            return

        case Msg.DeleteFolder:
            // The backend listed that folder and found it empty; this side is
            // the only one that can delete anything. Nothing here decides
            // whether it should go — a view that second-guessed the listing
            // would be deciding what to delete from a screen that cannot see
            // the folder at all.
            root.deleteFolder(msg ? msg.folderId : "", msg ? msg.folderName : "")
            return

        case Msg.DeleteSeriesConfirm:
            // The strip carries the documents as well as the sentence: the
            // frontend is what deletes them, and the backend is what knows
            // which ones they are.
            root.confirmingSeriesUuids = msg && msg.documentUuids ? msg.documentUuids : []
            downloadedListScreen.confirmingSourceId = msg ? msg.sourceId : ""
            downloadedListScreen.confirmingSeriesId = msg ? msg.seriesId : ""
            downloadedListScreen.confirmingMessage = msg ? msg.message : ""
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

        case Msg.CheckDocuments:
            root.checkDocuments(msg)
            return

        case Msg.SortDocuments:
            root.sortDownload(msg)
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
            // The combined search says the same thing with the same two
            // lines. A source that merely failed to answer never gets here —
            // that is a partial result and arrives as one (fillSearchAll).
            searchAllScreen.busy = false
            searchAllScreen.pendingPage = 0
            chapterListScreen.busy = false
            return
        }
    }

    // ---- model filling -----------------------------------------------------

    // fillSourceRows is fillSources and fillPrivateSources at once: the two
    // lists are the same row shape, partitioned by the backend rather than by
    // this file (service.buildSourceViews), so there is exactly one place
    // that turns a sourceView into what a row draws.
    function fillSourceRows(model, list) {
        model.clear()
        for (var i = 0; i < (list ? list.length : 0); ++i) {
            var s = list[i]
            model.append({
                "sourceId": s.id,
                "name": s.name,
                "baseUrl": s.baseUrl,
                "theme": s.theme,
                "lang": s.lang,
                "enabled": s.enabled,
                "private": !!s.private,
                // The backend resolves "unset" to "auto", so this is never
                // blank; the fallback is for a reply from an older backend.
                "splitStrips": s.splitStrips ? s.splitStrips : "auto",
                "status": s.status,
                "statusDetail": s.statusDetail ? s.statusDetail : "",
                "proxy": s.proxy ? s.proxy : "",
                "selfHostedViaProxy": !!s.selfHostedViaProxy,
                // Comma-joined and JSON respectively, for the reason
                // "authors" below is: a ListModel role cannot reliably hold
                // an array. Written on every row, empty included, because a
                // ListModel fixes its roles on the first append.
                "allowedHosts": (s.allowedHosts && s.allowedHosts.length > 0)
                    ? s.allowedHosts.join(",") : "",
                "pendingHosts": JSON.stringify(s.pendingHosts ? s.pendingHosts : [])
            })
        }
    }

    function fillSources(list) {
        fillSourceRows(sourcesModel, list)
    }

    function fillPrivateSources(list) {
        fillSourceRows(privateSourcesModel, list)
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
                "coverPath": "",
                // Comma-joined rather than carried as a list: a ListModel role
                // cannot reliably hold an array (append stores it, but reading
                // .length or an index back off it does not work), and most
                // sources have none of this to give anyway (SeriesGrid.qml).
                // The role is written on every row, empty string included, for
                // the same reason "watched" below is — a ListModel fixes its
                // roles on the first append and drops keys added later.
                "authors": (list[i].authors && list[i].authors.length > 0)
                    ? list[i].authors.join(", ") : "",
                // What this result is — absent means manga (ui/Kinds.js). One
                // source is one kind, so this screen draws no filter; it is
                // here because opening a result is what tells the series screen
                // whether it is listing chapters or releases.
                "kind": Kinds.of(list[i]),
                // Whether this result is already watched, for the long-press
                // menu's Watch / Stop watching line. Derived from the watched
                // model rather than carried by the search reply: the store is
                // the only thing that knows, and it is already here.
                //
                // The role is written on the first append or not at all — a
                // ListModel fixes its roles then and drops keys added later —
                // which is why it is filled in even for a source with no
                // watches at all.
                "watched": WatchJs.indexOf(watchedModel, root.currentSourceId,
                                           list[i].id) >= 0
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

    // fillSearchAll renders one page of the combined search.
    //
    // **A source that failed is not an error.** Its rows are missing and the
    // others are not, so the page is drawn exactly as it arrived and the
    // sources that did not answer go on one subdued line underneath
    // (Grouping.js failedLine). Nothing here blanks the results, sets
    // lastError, or lets "some sources failed" reach the user as "the search
    // failed" — which is what a half-empty screen with an error over it would
    // say.
    function fillSearchAll(msg) {
        searchAllScreen.busy = false

        // The reply is kept whole, because the kind filter is drawn from it
        // again every time it moves. **Nothing is re-fetched to filter**: this
        // page was merged and paged by the backend out of a request to every
        // configured site, and spending that again to show a subset of rows
        // already on screen would make a filter something that can fail
        // (PLAN §7.4).
        root.lastSearchAll = msg

        // Where the backend says we are. It clamps a page that ran past the
        // end, so this is the page actually served, and totalPages stays 0
        // until every source has run dry rather than being guessed at (PLAN
        // §12.1 — do not invent a total).
        searchAllScreen.page = msg && msg.page > 0 ? msg.page : 1
        searchAllScreen.totalPages = msg && msg.totalPages > 0 ? msg.totalPages : 0
        searchAllScreen.hasMore = msg ? !!msg.hasMore : false
        searchAllScreen.pendingPage = 0
        searchAllScreen.failedSources = Grouping.failedLine(msg ? msg.sourceErrors : [])
        root.showSearchAllGroups()
    }

    // showSearchAllGroups draws the page in hand through the screen's kind
    // filter. Called when a page lands and again whenever the filter moves.
    //
    // The two emptinesses are kept apart here (ui/Kinds.js): a search that
    // found nothing is the backend's answer, and a page whose results the
    // filter is holding back is the filter's — and is undone by tapping All.
    // Saying the first when the second is true is how a working search gets
    // reported as broken.
    function showSearchAllGroups() {
        var msg = root.lastSearchAll
        var total = Grouping.countOf(msg)
        root.searchAllMatches = Grouping.fill(searchAllModel, msg,
                                              searchAllScreen.kindFilter)
        searchAllScreen.hiddenCount = total - searchAllModel.count
        searchAllScreen.emptyMessage = searchAllModel.count === 0
            ? Kinds.emptyLine(searchAllScreen.kindFilter, total) : ""
        // One batch for the rows now on screen, every entry naming its own
        // source: this screen draws series from several at once. A filtered-out
        // group is not in the batch, and an empty batch supersedes the last one
        // — which is what drops fetches for covers nobody can see (Covers.js).
        searchAllScreen.requestVisibleCovers()
    }

    // Covers never travel over the socket (PLAN §7.1): the backend writes a
    // downscaled copy to disk and sends its path, which is what lands here.
    function applyCover(msg) {
        if (!msg || !msg.seriesId) {
            return
        }
        // Four models can want the same cover now: the search grid, the
        // combined search, the downloaded overview and the watched list all
        // draw tiles — and the combined search is the case that makes this
        // more than housekeeping, since the same series can be on it and on
        // any of the others at the same time. It is
        // written to every row that matches rather than to the first, because
        // a series really can be in all three at once, and a second fetch for
        // a file already on disk is work the politeness limiter would
        // serialise in front of a cover nobody has yet (PLAN §7.4).
        var wrote = root.writeCover(seriesModel, msg)
                  + root.writeCover(searchAllModel, msg)
                  + root.writeCover(downloadedModel, msg)
                  + root.writeCover(watchedModel, msg)
        if (wrote > 0)
            return
        // Fell through: a cover arrived for a series no screen is showing.
        // Silent otherwise, and indistinguishable from a cover that was never
        // fetched — so it says so, with the id, because the id is the thing
        // that would have to differ for a fetched cover to go nowhere.
        console.log("[quire] cover for a series no screen is showing: " + msg.seriesId)
    }

    // writeCover puts the path on every row of one model that is the series,
    // and reports how many it wrote.
    //
    // A combined-search row's `seriesId` is the match it opens on, which is
    // not always the match its cover came from (SearchAll.qml's
    // requestVisibleCovers, Grouping.js groupRow) — so the reply this is
    // matching against is keyed by `coverSeriesId` when the row has one.
    // Every other model's rows carry no such role, and reading an undefined
    // one is simply not equal to msg.seriesId, so this costs those rows
    // nothing.
    function writeCover(rows, msg) {
        var n = 0
        for (var i = 0; i < rows.count; ++i) {
            var row = rows.get(i)
            var owner = row.coverSeriesId !== undefined && row.coverSeriesId !== ""
                      ? row.coverSeriesId : row.seriesId
            if (owner !== msg.seriesId)
                continue
            rows.setProperty(i, "coverPath", "file://" + msg.path)
            n++
        }
        return n
    }

    // requestCoversBySource asks for a batch of covers that may span sources.
    // Every screen's covers go through here, the per-source search grid
    // included: what the batch is made of, and the rule that an empty one is
    // not sent at all, are in Covers.js with the reasoning and the tests.
    //
    // `fallbackSourceId` is the source a screen showing one site at a time is
    // browsing. Its rows carry no source of their own — every row has the same
    // one — and it is this file that knows which.
    function requestCoversBySource(covers, fallbackSourceId) {
        // Whatever Covers.js says to send, and nothing when it says nothing.
        var msg = Covers.request(covers, fallbackSourceId)
        if (msg)
            root.send(Msg.RequestCover, msg)
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

    // markWatchedElsewhere re-derives the Watch / Stop watching line on the two
    // screens that show series they do not own the watch record for.
    //
    // Called from both cues rather than from one: a single watch landing mid
    // round is what a tap on the menu produces, and the whole list is what
    // arrives after it and at the end of every check. Neither writes anything
    // where the flag already agrees (Watch.js), so the common case — a check
    // round with no news — repaints nothing.
    function markWatchedElsewhere() {
        WatchJs.markWatched(seriesModel, watchedModel, root.currentSourceId)
        WatchJs.markWatched(downloadedModel, watchedModel, "")
    }

    function reconcileWatched(msg) {
        WatchJs.reconcile(watchedModel, msg ? msg.watched : [])
        root.markWatchedElsewhere()
        // Written only where the string differs, so a check round that ends
        // with the same summary it began with repaints nothing.
        WatchJs.applySummary(root, msg)
        root.refreshWatchedFlag()
        // The list is pushed rather than fetched, so this is the only cue that
        // a row the grid has no cover for has arrived.
        watchListScreen.requestVisibleCovers()
    }

    function applyWatchUpdate(w) {
        WatchJs.applyUpdate(watchedModel, w)
        root.refreshWatchedFlag()
        root.markWatchedElsewhere()
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

    // requestSearchAllPage asks every enabled source for one display page of
    // the merged results. The backend does the fanning out, the merging and
    // the paging; this only says how much fits.
    //
    // **An empty query sends nothing.** It is the second of the two guards —
    // the screen has the first — and it is here because this is the one place
    // the message is composed: a page turn, a relayout and the Search key all
    // arrive through it, and only one of them has a keyboard in front of it.
    // Which scope the one combined-search screen is currently answering for.
    // The screen and its model (searchAllScreen, searchAllModel) are shared
    // between the normal and the private search — they are never open at the
    // same time, so there is nothing to keep in step by duplicating either —
    // and this is the one flag that decides which message a page request
    // sends. See openSearchAll / openPrivateSearchAll.
    property bool searchAllPrivate: false

    function requestSearchAllPage(page) {
        if (!Grouping.searchable(searchAllScreen.query))
            return
        root.send(root.searchAllPrivate ? Msg.SearchAllPrivate : Msg.SearchAll, {
            "query": searchAllScreen.query,
            "page": page,
            "pageSize": searchAllScreen.pageSize})
    }

    // openSearchAll is the way in from the sources screen. openPrivateSearchAll
    // is its private-list counterpart, reached only from the eye-icon screen
    // and touching only private sources (backend/service/searchall.go's own
    // partition, not a filter here).
    //
    // Both start empty. A search is a question the user asks, and coming back
    // to the screen later to find someone else's old answer — possibly from
    // sources that have since been disabled — is worse than an empty box.
    // Returning *from a series* is a different route and goes through
    // showScreen, which keeps the page the user was on.
    function openSearchAllScoped(screenName, private_) {
        root.searchAllPrivate = private_
        root.showScreen(screenName)
        searchAllModel.clear()
        root.searchAllMatches = ({})
        root.lastSearchAll = null
        searchAllScreen.reset()
        searchAllScreen.busy = false
        // The keyboard comes up with the screen: there is nothing else to do
        // here, and the alternative is a tap on a box that is already the only
        // thing on screen.
        searchAllScreen.searching = true
    }

    function openSearchAll() {
        root.openSearchAllScoped("searchall", false)
    }

    function openPrivateSearchAll() {
        root.openSearchAllScoped("searchAllPrivate", true)
    }

    // browseCameFrom is where Back from a single source's catalogue goes:
    // "sources" ordinarily, "privateSources" when the row that was tapped
    // came from the private list. Without it, browsing a private source and
    // pressing Back would land on the normal list rather than the private one
    // it was reached from — a small thing, but exactly the kind of paper cut
    // that makes someone stop using the private list at all.
    property string browseCameFrom: "sources"

    function openSource(sourceId, name) {
        root.currentSourceId = sourceId
        root.currentSourceName = name
        root.browseCameFrom = root.screen === "privateSources" ? "privateSources" : "sources"
        root.showScreen("browse")
        seriesGridScreen.reset()
        seriesGridScreen.busy = true
        seriesGridScreen.pendingPage = 1
        root.requestSeriesPage(1)
    }

    // openSeries opens one (source, series) pair. `matches` is the combined
    // search's group — every source the same series was found in — and is
    // absent for every other route in, which is what decides that the source
    // chips are drawn at all (ui/ChapterList.qml).
    // `kind` is what the row that opened this said it was, and is absent for
    // the routes that cannot know — the watched list, whose rows predate books
    // entirely. Absent is manga, which is what every source but a Shelfmark one
    // is (ui/Kinds.js).
    function openSeries(seriesId, title, matches, kind) {
        root.currentSeriesId = seriesId
        // Where Back goes. Not recomputed when this is re-entered from the
        // series screen itself — which is what switching source does — because
        // the answer is where the user came *from*, and by then that is no
        // longer what `screen` says.
        if (root.screen !== "series")
            root.seriesCameFrom = root.screen === "watching"
                               || root.screen === "downloaded"
                               || root.screen === "searchall"
                               || root.screen === "searchAllPrivate"
                                  ? root.screen : "browse"
        // The alternatives belong to the group, not to the source being read,
        // so they survive a switch between them.
        // (Which of them is being read is bound to root.currentSourceId below,
        // not assigned here: an assignment would break that binding and leave
        // the marked chip behind on the next switch.)
        chapterListScreen.sources = matches ? matches : []
        root.showScreen("series")
        chaptersModel.clear()
        // Emptied before the new series' detail arrives, so the previous
        // series' volume view is never on screen over this one's chapters.
        volumesModel.clear()
        chapterListScreen.view = "chapters"
        // Set before the detail is asked for, so the screen is never briefly a
        // chapter list for a book: "Fetching…" arrives under the right heading.
        chapterListScreen.kind = Kinds.normalise(kind)
        chapterListScreen.seriesTitle = title
        chapterListScreen.synopsis = ""
        chapterListScreen.page = 1
        chapterListScreen.closeConfirm()
        chapterListScreen.leaveSelection()
        chapterListScreen.busy = true
        root.refreshWatchedFlag()
        root.send(Msg.SeriesDetail, {"sourceId": root.currentSourceId, "seriesId": seriesId})
    }

    // switchSource re-opens the series being read on one of the other sources
    // the combined search found it on.
    //
    // **It switches the pair, it does not merge anything.** Every message this
    // screen sends — the detail, a download, a watch — names (sourceId,
    // seriesId), and after this they all name the new one. The chapters are
    // re-listed from that source because they are that source's chapters: a
    // numbering, a scanlator and a set of downloads that have nothing to do
    // with the ones just on screen.
    //
    // The title is kept rather than re-derived: it is the group's, the sources
    // agreed on it closely enough to be merged, and it stops the header going
    // blank while the detail is in flight.
    function switchSource(sourceId, sourceName, seriesId) {
        root.currentSourceId = sourceId
        root.currentSourceName = sourceName
        // The kind travels with it: the sources in a group were merged because
        // they are the same work, and a switch between them changes which site
        // it is being read from, not what it is.
        root.openSeries(seriesId, chapterListScreen.seriesTitle, chapterListScreen.sources,
                        chapterListScreen.kind)
    }

    // showScreen is the one way a screen becomes the active one.
    //
    // Every route goes through it, including Back, because the bug it fixes was
    // a screen that fetched its list on one route in and not on the other: the
    // Downloaded overview kept showing a series whose last download had just
    // been deleted from the screen underneath it. Screens.js decides what needs
    // re-asking for and says why the other screens do not.
    function showScreen(name) {
        // Every route into a screen comes through here, which makes it the one
        // place that can be sure the keyboard does not follow the user to the
        // next screen (PLAN §11 Q5). Annex supplies no keyboard, so this is
        // Quire's own again — but the rule is unchanged, because the reason
        // for it never depended on whose keyboard it was: a keyboard left up
        // covers the bottom of the screen, which is the pager and the last
        // rows of whatever is now showing.
        root.dismissInput()
        root.screen = name
        switch (Screens.refreshOnShow(name)) {
        case "listDownloaded":
            root.send(Msg.ListDownloaded, {})
            break
        case "listPrivateSources":
            root.send(Msg.ListPrivateSources, {})
            break
        }
    }

    function goBack() {
        switch (root.screen) {
        case "series":
            root.showScreen(root.seriesCameFrom)
            break
        case "watching":
        case "downloaded":
        case "privateSources":
            root.showScreen("sources")
            break
        case "browse":
            root.showScreen(root.browseCameFrom)
            break
        case "searchall":
        case "add":
        case "settings":
            root.showScreen("sources")
            break
        // The private search goes back to the private list it was opened
        // from, the same way "searchall" goes back to the normal list —
        // never straight to "sources", or the private list the user came
        // from would be one extra Back away.
        case "searchAllPrivate":
            root.showScreen("privateSources")
            break
        default:
            root.close()
        }
    }

    function screenTitle() {
        switch (root.screen) {
        case "add": return "Add a source"
        case "browse": return root.currentSourceName
        // The scope, not the query: the query is in the field, two centimetres
        // below, and repeating it in the header would be the only title on any
        // screen that changes as the user types.
        case "searchall": return "Every source"
        case "privateSources": return "Private sources"
        case "searchAllPrivate": return "Every private source"
        case "watching": return "Watching"
        case "downloaded": return "Downloaded"
        case "series": return chapterListScreen.seriesTitle
        case "settings": return "Settings"
        }
        return "Quire"
    }

    Component.onCompleted: {
        // These are belt and braces, not the mechanism.
        //
        // AppLoad discarded messages aimed at a backend whose socket was not
        // yet up ("No active socket for ID:quire"), and this ran at exactly
        // the moment that race was live. The backend therefore *pushes* the
        // source list and the status when it sees the frontend attach, which
        // is the earliest point a send can work.
        //
        // Annex does not have that race — a message posted before the backend
        // is reachable fails loudly rather than vanishing, and one posted
        // after is queued — but the push is kept because it is the right
        // shape regardless, and twice it was the thing standing between the
        // user and an app claiming their sources were gone.
        //
        // These two run before the backend has been discovered — reading its
        // endpoint file is asynchronous and cannot have finished yet — so
        // Backend queues them and sends them the moment it connects. They are
        // not lost and they do not report a failure.
        root.send(Msg.ListSources)
        root.send(Msg.Ping)
    }

    // Called by the host when the app is being unloaded.
    //
    // **It detaches; it does not terminate.** Under AppLoad this called
    // terminate() and the quired process died with the app. Under Annex the
    // backend is a systemd service that outlives the screen, so the right
    // thing is a clean detach: the backend hears LostCoordinator immediately
    // rather than waiting out its timeout, and whatever it does when the
    // frontend leaves happens at once.
    //
    // What the backend then does is the backend's decision and is unchanged
    // by this port — Quire pauses downloads on detach (PLAN §6 M7, no
    // wakelock). The difference is that the *state* now survives: reopening
    // resumes instead of starting the process over.
    function unloading() {
        backend.stop()
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
            // On every screen but the first this goes back one step. On the
            // first it leaves Quire, and it says so rather than saying
            // "Back" — the destination is the library, not a screen of ours.
            //
            // **It used to be blank here, and under Annex that would trap the
            // user.** AppLoad drew its own chrome around an app and that
            // chrome is what closed it; an Annex app fills the navigator and
            // the host draws nothing, so if the app offers no way out there
            // is none. goBack() has always called root.close() on this
            // screen; until now nothing could reach it.
            text: root.screen === "sources" ? "‹ Library" : "‹ Back"
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
            id: titleLabel
            objectName: "screenTitle"
            anchors.centerIn: parent
            // Room for Back on one side and Settings on the other, less the
            // layout switch when the screen has one. The title stays centred
            // on the header and gives up width rather than running under a
            // control.
            width: parent.width - 360
                   - (viewToggle.visible ? viewToggle.width + Style.gap * 2 : 0)
            horizontalAlignment: Text.AlignHCenter
            elide: Text.ElideRight
            text: root.screenTitle()
            font.pointSize: Style.headingSize
            // Black, like every other word in the app. This used to be drawn
            // in the accent and on the device it read muddy — the panel
            // composes a coloured pixel through its colour filter array and a
            // letterform is nearly all edge (ui/Style.js). The colour moved to
            // the bar below.
            color: Style.ink
        }

        // The one piece of chrome that is the same shape on every screen, so
        // it is the one worth making findable by colour. The bar is a solid
        // area, which is the only thing this panel renders cleanly in colour,
        // and it is as wide as the title's own words so it points at them.
        //
        // It is a fourth signal, not the first: the title is already the
        // largest type in the header, already the only centred thing in it,
        // and still says where you are in words.
        AccentRule {
            objectName: "screenTitleRule"
            anchors {
                top: titleLabel.bottom; topMargin: 4
                horizontalCenter: titleLabel.horizontalCenter
            }
            width: Math.min(titleLabel.contentWidth, titleLabel.width)
        }

        // The layout switch, in the chrome rather than on each screen: it is
        // the same control in the same place on all three, and a screen that
        // drew its own would put it above its own search bar or check button,
        // where it would read as part of that screen's work.
        ViewToggle {
            id: viewToggle
            objectName: "viewToggle"
            anchors {
                right: settingsArea.left; rightMargin: Style.gap
                verticalCenter: parent.verticalCenter
            }
            // Only the three screens that remember one (Views.js).
            visible: Views.remembers(root.screen)
            view: root.viewFor(root.screen)
            onViewRequested: root.setView(root.screen, view)
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
            onClicked: { root.showScreen("settings"); root.send(Msg.Ping) }
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

        // One instance for both the normal and the private source list. They
        // are the same screen in every way but which model backs it, which
        // scope its own combined search touches, and which direction the
        // "Make private"/"Make public" row action goes — see
        // SourceList.qml's showingPrivate.
        SourceList {
            id: sourceListScreen
            objectName: "sourceList"
            anchors.fill: parent
            visible: root.screen === "sources" || root.screen === "privateSources"
            showingPrivate: root.screen === "privateSources"
            model: root.screen === "privateSources" ? privateSourcesModel : sourcesModel
            onAddRequested: { addSourceScreen.reset(); root.showScreen("add") }
            onWatchingRequested: root.showScreen("watching")
            onSearchAllRequested: root.screen === "privateSources"
                                   ? root.openPrivateSearchAll() : root.openSearchAll()
            // Fetched on the way in rather than pushed: a list that is right
            // when it is opened is enough, and much less machinery. "Opened"
            // includes being returned to — see showScreen.
            onDownloadedRequested: root.showScreen("downloaded")
            watchingLabel: root.watchShort
            onOpenRequested: root.openSource(sourceId, name)
            notice: root.notice
            onNoticeDismissed: root.notice = ""
            onToggleRequested: root.send(Msg.SetSourceEnabled, {"sourceId": sourceId, "enabled": enabled})
            onRemoveRequested: root.send(Msg.RemoveSource, {"sourceId": sourceId})
            onRenameRequested: root.send(Msg.RenameSource, {"sourceId": sourceId, "name": name})
            onSplitStripsRequested: root.send(Msg.SetSourceSplitStrips,
                                              {"sourceId": sourceId, "splitStrips": mode})
            onProxyRequested: root.send(Msg.SetSourceProxy,
                                        {"sourceId": sourceId, "proxy": proxy})
            onAllowHostRequested: root.send(Msg.AllowSourceHost,
                                             {"sourceId": sourceId, "host": host})
            onRevokeHostRequested: root.send(Msg.RevokeSourceHost,
                                              {"sourceId": sourceId, "host": host})
            // Marking or unmarking private: the backend moves the source
            // between the two lists and answers with both (see
            // Service.buildSourceViews / sendSourceListFor), so nothing here
            // has to guess which screen to refresh.
            onPrivateRequested: root.send(Msg.SetSourcePrivate,
                                          {"sourceId": sourceId, "private": makePrivate})
            // The eye-icon button. The private list is fetched on the way in,
            // like Downloaded — see Screens.js.
            onPrivateListRequested: root.showScreen("privateSources")
        }

        AddSource {
            id: addSourceScreen
            objectName: "addSource"
            anchors.fill: parent
            visible: root.screen === "add"
            onProbeRequested: root.send(Msg.ProbeSource, {"url": url})
            onAnswerRequested: root.send(Msg.ProbeAnswer, {"id": answerId, "text": answerText})
            onConfirmRequested: {
                root.send(Msg.ConfirmAddSource, {"url": url, "theme": theme, "name": name, "lang": lang})
                root.showScreen("sources")
            }
            onDoneRequested: root.showScreen("sources")
        }

        WatchList {
            id: watchListScreen
            objectName: "watchList"
            anchors.fill: parent
            visible: root.screen === "watching"
            model: watchedModel
            view: root.watchingView
            phrase: root.watchPhrase
            onCheckRequested: root.send(Msg.CheckWatched, {})
            onCoversRequested: root.requestCoversBySource(covers)
            onUnwatchRequested: root.send(Msg.UnwatchSeries,
                {"sourceId": sourceId, "seriesId": seriesId})
            // The long-press menu's two. Both are answered by the backend and
            // neither is echoed on the row first: DownloadNewChapters comes
            // back as the same MessageQueueResult a multi-select download
            // does — handled once, in dispatch — and MarkSeen comes back as a
            // watch update and then the whole list, so the row redraws itself
            // from the store (PLAN §12.2).
            onDownloadNewRequested: root.send(Msg.DownloadNewChapters,
                {"sourceId": sourceId, "seriesId": seriesId})
            onMarkSeenRequested: root.send(Msg.MarkSeen,
                {"sourceId": sourceId, "seriesId": seriesId})
            onOpenRequested: {
                root.currentSourceId = sourceId
                root.currentSourceName = sourceName
                root.openSeries(seriesId, title)
            }
        }

        DownloadedList {
            id: downloadedListScreen
            objectName: "downloadedList"
            anchors.fill: parent
            visible: root.screen === "downloaded"
            model: downloadedModel
            view: root.downloadedView
            emptyNote: root.downloadedEmpty
            onCoversRequested: root.requestCoversBySource(covers)
            // The same route into a series the grid and the watched list use:
            // one series screen, one Back behaviour, one message.
            onOpenRequested: {
                root.currentSourceId = sourceId
                root.currentSourceName = sourceName
                // What the row said it was. Looked up by the pair rather than
                // carried on the signal: the signal is the same one three
                // screens raise, and only this one's rows have a kind.
                root.openSeries(seriesId, title, undefined,
                                Kinds.kindFor(downloadedModel,
                                              {"sourceId": sourceId, "seriesId": seriesId}))
            }
            onDeleteRequested: root.send(Msg.DeleteSeries,
                {"sourceId": sourceId, "seriesId": seriesId})
            onDeleteConfirmed: root.deleteSeries(sourceId, seriesId)

            // "Read latest" — the newest download of the series, opened
            // through the one handoff every other Read goes through, so a
            // missing document is reported the same way here as anywhere
            // else (PLAN §6 M6).
            onReadRequested: root.openInReader(documentUuid)

            // Watching a series from the screen that lists what is already on
            // the tablet. A row here is a (source, series) pair, so it names
            // both rather than borrowing whatever source is being browsed.
            onWatchRequested: root.send(Msg.WatchSeries,
                {"sourceId": sourceId, "seriesId": seriesId, "title": title})
            onUnwatchRequested: root.send(Msg.UnwatchSeries,
                {"sourceId": sourceId, "seriesId": seriesId})
        }

        SeriesGrid {
            id: seriesGridScreen
            objectName: "seriesGrid"
            anchors.fill: parent
            visible: root.screen === "browse"
            model: seriesModel
            view: root.searchView
            // The line under each title in the list layout. One source per
            // listing today, so it is the screen's rather than the row's.
            sourceName: root.currentSourceName
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
            // Through the same builder every other screen uses. This used to
            // send its own message with the source at the top level and none
            // on the entries — which the backend accepts, falling back to the
            // message's source, but it was a second implementation of the one
            // thing and it sent an empty batch at startup, before any screen
            // had a tile. An empty batch cancels whatever is in flight
            // (Covers.js), so the second path was one screen away from
            // cancelling another's covers.
            onCoversRequested: root.requestCoversBySource(covers, root.currentSourceId)
            onOpenRequested: root.openSeries(seriesId, title, undefined,
                                            Kinds.kindFor(seriesModel, {"seriesId": seriesId}))

            // Watching straight from the results, without opening the series
            // first. One source is being browsed, so it is this screen's
            // rather than the row's — the same reason the rows' subtitle is.
            onWatchRequested: root.send(Msg.WatchSeries,
                {"sourceId": root.currentSourceId, "seriesId": seriesId, "title": title})
            onUnwatchRequested: root.send(Msg.UnwatchSeries,
                {"sourceId": root.currentSourceId, "seriesId": seriesId})
        }

        SearchAll {
            id: searchAllScreen
            objectName: "searchAll"
            anchors.fill: parent
            visible: root.screen === "searchall" || root.screen === "searchAllPrivate"
            model: searchAllModel
            // The same stored layout the per-source results use (Views.js).
            view: root.searchView
            onSearchRequested: root.requestSearchAllPage(1)
            onPageRequested: root.requestSearchAllPage(page)
            // The batch spans sources, so it goes through the one path that
            // keeps it whole and lets each entry name its own source. Splitting
            // it per source was a real bug on Downloaded: each message
            // cancelled the one before it.
            onCoversRequested: root.requestCoversBySource(covers)
            // The kind filter moved. The page is already here, so this only
            // draws it again — see showSearchAllGroups.
            onRefilterRequested: root.showSearchAllGroups()
            // Opening a group opens its first match — the one the backend put
            // first, which is first in the user's own source order. The rest
            // go with it, and become the chips on the series screen; nothing
            // is merged and nothing is chosen silently.
            onOpenRequested: {
                var matches = Grouping.matchesFor(root.searchAllMatches, key)
                root.currentSourceId = sourceId
                root.currentSourceName = matches.length > 0 && matches[0].sourceName
                    ? matches[0].sourceName : ""
                root.openSeries(seriesId, title, matches,
                                Kinds.kindFor(searchAllModel, {"key": key}))
            }
        }

        ChapterList {
            id: chapterListScreen
            objectName: "chapterList"
            anchors.fill: parent
            visible: root.screen === "series"
            model: chaptersModel
            volumeModel: volumesModel
            // Which source this series is being read on, and which others the
            // combined search found it on. Both are set by openSeries; the
            // chips are absent for every other route in.
            currentSourceId: root.currentSourceId
            onSourceSwitchRequested: root.switchSource(sourceId, sourceName, seriesId)
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
