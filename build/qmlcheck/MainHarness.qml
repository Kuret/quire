// The offscreen harness for the real ui/Main.qml.
//
// # It really is the app
//
// Everything here drives `ui/Main.qml` itself — the shell, its models, its
// dispatch table and its navigation — not a copy of any part of it. For a long
// time comments in this repository said that could not be done because Main.qml
// "imports the Backend plugin". **That was wrong.** `Backend` is a plain QML
// file in Annex's lib/ (annex/lib/qmldir says so in as many words), and the only
// obstacle was ever a path: Main.qml's
//
//     import "../../../lib"
//
// resolves against the device layout, /home/root/annex/apps/quire/ui, and so
// lands on /home/root/annex/lib. From the repository it lands on /Users/…/lib,
// which does not exist.
//
// So build/qml-check.sh builds a directory with the device's shape at check
// time —
//
//     <fixture>/lib/Backend.qml        the stub, build/qmlcheck/fixture/
//     <fixture>/lib/qmldir
//     <fixture>/apps/quire/ui    ->    the repository's real ui/
//     <fixture>/apps/quire/harness/MainHarness.qml   this file
//
// — and the import resolves. **This file is only valid from that generated
// location**: its relative paths are the device's, not the repository's.
//
// # What a green run here does and does not mean
//
// The Backend next door is a stub (see its own comments). It records what the
// app sends and hands replies back in process, which covers our half of the
// conversation completely — and nothing else. It cannot catch the real
// Backend.qml failing to find its endpoint file, the HTTP transport mangling a
// payload, Annex failing to load the app at all, or anything about what draws
// over what on the device. A green harness is not a working device.
//
// The reader bridge is absent here too, and deliberately: ReaderHandoff.qml
// imports xochitl's own QML singletons, which do not exist off the tablet. So
// every answer this file asserts about deleting, sorting, checking and opening
// documents is the **no-bridge** branch — the honest failure Answers.js
// composes. The branches where the bridge answers are Answers.js's own, and
// they are driven from Harness.qml.
//
// # How it drives
//
// By message and by tap wherever it can: `deliver()` emits the transport's own
// message signal, exactly as a poll would, and the screens' controls are
// clicked. Where a case is about the wiring *between* a screen and the shell,
// it emits the screen's own signal — that signal is the wiring under test, and
// the taps that raise it are already asserted in Harness.qml. Nothing here
// calls a dispatch handler directly: a test that calls the handler proves the
// handler works, not that anything reaches it.
import QtQuick 2.5
import QtQuick.Window 2.2
import "../ui/Messages.js" as Msg
import "../ui/Views.js" as Views
import "../ui/Answers.js" as Answers
import "../ui/Style.js" as Style
import "../ui/Kinds.js" as Kinds

Window {
    id: win
    width: 1620; height: 2160
    visible: true

    // The app, loaded by path rather than as a type, so a failure to load is a
    // status and an error string rather than a compile error in this file.
    Loader {
        id: appLoader
        anchors.fill: parent
        asynchronous: false
        source: "../ui/Main.qml"
        onStatusChanged: if (status === Loader.Error)
                             console.log("FAIL Main.qml did not load: " + sourceComponent)
    }

    property var app: appLoader.item
    property var backend: null

    property int failures: 0
    function want(label, got, expected) {
        if (got !== expected) {
            console.log("FAIL " + label + ": got " + got + " want " + expected)
            win.failures++
        } else {
            console.log("ok   " + label + " = " + got)
        }
    }

    // deliver is one backend→frontend message, emitted on the transport's own
    // signal. This is the whole of how the backend talks to the app, so it is
    // the whole of how this file does.
    function deliver(type, payload) {
        win.backend.message(type, payload === undefined ? "" : JSON.stringify(payload), false)
    }

    function findChild(item, name) {
        if (!item)
            return null
        if (item.objectName === name)
            return item
        for (var i = 0; i < item.children.length; ++i) {
            var hit = win.findChild(item.children[i], name)
            if (hit)
                return hit
        }
        return null
    }

    function findChildren(item, name, into) {
        if (!item)
            return into
        if (item.objectName === name)
            into.push(item)
        for (var i = 0; i < item.children.length; ++i)
            win.findChildren(item.children[i], name, into)
        return into
    }

    // colorOf names a colour the way Style.js writes it: QML hands them back in
    // lower case, and a comparison against the token would otherwise fail for
    // the spelling rather than for the colour. Same as the other harness.
    function colorOf(item) {
        return String(item.color).toUpperCase()
    }

    // Every piece of text under an item, found by what a Text *is* rather than
    // by objectName — the same walk the other harness uses, and for the same
    // reason: the word somebody recolours later is the one with no name on it.
    function textsUnder(item, out) {
        if (!item)
            return out
        if (item.font !== undefined && typeof item.text === "string")
            out.push(item)
        for (var i = 0; i < item.children.length; ++i)
            win.textsUnder(item.children[i], out)
        return out
    }

    // Every word a screen actually puts in front of somebody, joined — the
    // same helper Harness.qml uses, and for the same reason: the question
    // worth asking of a screen is "what does it show", not "what is bound to
    // it", and a Text inside a collapsed strip reads false for `visible`
    // because that property is the effective one.
    function wordsOn(item) {
        var texts = win.textsUnder(item, [])
        var said = []
        for (var i = 0; i < texts.length; ++i)
            if (texts[i].visible && String(texts[i].text).length > 0)
                said.push(String(texts[i].text))
        return said.join(" | ")
    }

    // The screens, by the objectName Main.qml gives each one.
    function screenNamed(name) { return win.findChild(win.app, name) }

    // ---- fixtures ----------------------------------------------------------

    property var lanternGroup: ({
        "key": "the lantern keeper", "title": "The Lantern Keeper",
        "coverUrl": "https://example.invalid/lantern.jpg",
        "matches": [
            {"sourceId": "src-a", "sourceName": "Example Reader",
             "seriesId": "/manga/lantern/", "coverUrl": "https://example.invalid/a.jpg"},
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/series/lantern", "coverUrl": "https://example.invalid/b.jpg"}]})

    property var orphanGroup: ({
        "key": "an orphan", "title": "An Orphan", "coverUrl": "",
        "matches": [
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/series/orphan", "coverUrl": "https://example.invalid/o.jpg"}]})

    // A group whose cover came from a *different* source than the one it
    // opens on — matches[0] (src-a) has no cover, so the group's cover is
    // src-b's. The backend names that ownership alongside the URL
    // (searchall.go group()); a fixture that omitted it would be silently
    // testing the fallback path instead of the fix.
    property var mixedOwnerGroup: ({
        "key": "mixed owner", "title": "Mixed Owner",
        "coverUrl": "https://beta.invalid/cover.jpg",
        "coverSourceId": "src-b", "coverSeriesId": "/series/beta-owns-this/",
        "matches": [
            {"sourceId": "src-a", "sourceName": "Example Reader",
             "seriesId": "/manga/mixed/"},
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/series/beta-owns-this/",
             "coverUrl": "https://beta.invalid/cover.jpg"}]})

    // A book, from a Shelfmark source. Its rows are releases — the files it is
    // available as — and the only thing on the wire that says so is `kind`
    // (ui/Kinds.js): the title, the cover and the sources all arrive through
    // the same fields a comic's do.
    property var duneGroup: ({
        "key": "dune messiah", "title": "Dune Messiah", "kind": "book",
        "coverUrl": "https://example.invalid/dune.jpg",
        "matches": [
            {"sourceId": "src-s", "sourceName": "Shelfmark",
             "seriesId": "/book/dune-messiah", "coverUrl": ""}]})

    function mixedReply() {
        return {"query": "dune", "page": 1, "totalPages": 0, "hasMore": false,
                "groups": [win.lanternGroup, win.orphanGroup, win.duneGroup],
                "sourceErrors": []}
    }

    // A listing for src-a with one cover and one without, which is what makes
    // the cover batch worth asserting: it holds one entry, not two.
    function seriesReply(page, totalPages, hasMore) {
        return {
            "series": [
                // Two authors, to prove fillSeries joins a list rather than
                // just carrying the first name or the array itself (a
                // ListModel role cannot reliably hold one — see Main.qml).
                {"id": "/manga/lantern/", "title": "The Lantern Keeper",
                 "coverUrl": "https://example.invalid/a.jpg",
                 "authors": ["Frank Herbert", "Brian Herbert"]},
                // No authors at all, which is every comic source's reply
                // today and must produce an empty string, not a thrown error.
                {"id": "/manga/orphan/", "title": "An Orphan", "coverUrl": ""}],
            "page": page, "totalPages": totalPages, "hasMore": hasMore}
    }

    function downloadedReply() {
        return {"series": [
            {"sourceId": "src-a", "sourceName": "Example Reader",
             "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
             "detail": "3 downloads", "openable": true, "note": "",
             "coverUrl": "https://example.invalid/a.jpg", "latestUuid": "doc-9"},
            // A downloaded book, beside a downloaded comic whose reply says
            // nothing about kind at all — which is every row a Quire before
            // books wrote.
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/series/orphan", "title": "An Orphan", "kind": "book",
             "detail": "1 download", "openable": false, "note": "One file is missing.",
             "coverUrl": "", "latestUuid": ""}],
            "empty": ""}
    }

    function watchReply(rows, summary) {
        return {"watched": rows, "summary": summary}
    }

    property var lanternWatch: ({
        "sourceId": "src-a", "seriesId": "/manga/lantern/",
        "sourceName": "Example Reader", "title": "The Lantern Keeper",
        "newChapters": 0, "badge": "", "state": "ok", "status": "Up to date",
        "checkedAt": "2026-09-19T00:00:00Z",
        "coverUrl": "https://example.invalid/a.jpg"})

    // A watch with a cover nothing has fetched yet, so a push has something to
    // ask for.
    property var newWatch: ({
        "sourceId": "src-b", "seriesId": "/series/newone",
        "sourceName": "Other Reader", "title": "A New One",
        "newChapters": 0, "badge": "", "state": "ok", "status": "Up to date",
        "checkedAt": "2026-09-19T00:00:00Z",
        "coverUrl": "https://example.invalid/n.jpg"})

    property var orphanWatch: ({
        "sourceId": "src-b", "seriesId": "/series/orphan",
        "sourceName": "Other Reader", "title": "An Orphan",
        "newChapters": 0, "badge": "", "state": "ok", "status": "Up to date",
        "checkedAt": "2026-09-19T00:00:00Z", "coverUrl": ""})

    // Layout has to have run before a tile can be tapped: a page size comes
    // from a real viewport, and before the first pass there is none.
    Timer {
        interval: 120; running: true
        onTriggered: win.check()
    }

    // A case that throws would otherwise hang this process rather than fail it,
    // and a hang is not a failure anyone can read.
    function check() {
        try {
            win.runChecks()
        } catch (err) {
            console.log("FAIL the main harness threw before it finished: " + err)
            win.failures++
        }
        win.finish()
    }

    function runChecks() {
        // ---- it loads, and it is the real thing ---------------------------

        win.want("the real ui/Main.qml loads", appLoader.status, Loader.Ready)
        win.want("and there is a root to drive", win.app !== null, true)
        win.want("it opens on the source list", win.app.screen, "sources")

        // The transport it found for itself, not one handed to it: the app
        // declared a Backend and the fixture's lib/ is what satisfied it.
        win.backend = win.findChild(win.app, "stubBackend")
        win.want("the app connected itself to a Backend", win.backend !== null, true)
        win.want("naming the app the manifest names", win.backend.appId, "quire")
        win.want("which started itself", win.backend.status, "connected")

        // **The kind filter starts at All, every time the app starts.** Nothing
        // stores it — that would need a backend setting, and there is none — so
        // this is the whole of "it resets on start": the app above was loaded a
        // few milliseconds ago and has never been told anything.
        win.want("the combined search starts showing every kind",
                 win.screenNamed("searchAll").kindFilter, Kinds.ALL)

        // Off the device there is no library to hand documents to, so every
        // document answer below is the no-bridge one. Asserted rather than
        // assumed, because it is what those cases mean.
        win.want("there is no reader bridge off the device", win.app.bridge(), null)

        var backend = win.backend
        var sourceList = win.screenNamed("sourceList")
        var addSource = win.screenNamed("addSource")
        var seriesGrid = win.screenNamed("seriesGrid")
        var searchAll = win.screenNamed("searchAll")
        var chapterList = win.screenNamed("chapterList")
        var watchList = win.screenNamed("watchList")
        var downloadedList = win.screenNamed("downloadedList")
        var settings = win.screenNamed("settings")
        win.want("every screen is in the tree",
                 [sourceList, addSource, seriesGrid, searchAll, chapterList,
                  watchList, downloadedList, settings].indexOf(null), -1)

        // ---- what it says on startup ---------------------------------------
        //
        // Component.onCompleted asks for the source list and the status, in
        // that order, and nothing else. Both are sent before the backend has
        // been discovered on the device, which is why they may not be lost.
        win.want("startup asks for the sources and then the status",
                 backend.types(), Msg.ListSources + "," + Msg.Ping)
        win.want("and asks for nothing else", backend.sendCount, 2)
        win.want("with an empty JSON payload, never a bare send",
                 backend.rawOf(Msg.Ping), "{}")

        // ---- the status, and everything that rides on it -------------------

        backend.forget()
        win.deliver(Msg.Pong, {
            "version": "1.4.0", "os": "linux", "arch": "arm64",
            "notice": "Quire closed unexpectedly last time.",
            "consultRobots": true,
            "logTail": ["one", "two", "three"],
            "views": {"search": "list", "downloaded": "list", "watching": "grid"}})

        win.want("the status becomes the line the settings screen shows",
                 win.app.backendStatus, "Backend 1.4.0 on linux/arm64")
        win.want("and reaches that screen", settings.backendStatus,
                 "Backend 1.4.0 on linux/arm64")
        win.want("the notice reaches the source list",
                 sourceList.notice, "Quire closed unexpectedly last time.")
        win.want("the log tail reaches the viewer", settings.logLines.length, 3)
        win.want("the robots switch follows the store", settings.consultRobots, true)
        win.want("a status answers nothing back", backend.sendCount, 0)

        // The three stored layouts, off the same status and for the same
        // reason (Views.js).
        win.want("the search layout comes off the status", win.app.searchView, Views.LIST)
        win.want("the downloaded layout too", win.app.downloadedView, Views.LIST)
        win.want("and watching keeps its own", win.app.watchingView, Views.GRID)
        win.want("a screen draws the layout stored for it",
                 seriesGrid.view, Views.LIST)
        win.want("the combined search shares the search one",
                 searchAll.view, Views.LIST)
        win.want("the watched screen has its own", watchList.view, Views.GRID)

        // A backend too old to send layouts, or a status that arrived without
        // them, leaves every screen drawable rather than blank.
        win.deliver(Msg.Pong, {"version": "1.4.0", "os": "linux", "arch": "arm64"})
        win.want("a status with no layouts falls back to grid",
                 win.app.searchView, Views.GRID)
        win.want("on every screen",
                 win.app.downloadedView + "," + win.app.watchingView,
                 Views.GRID + "," + Views.GRID)
        win.want("and a status with no notice leaves the one showing alone",
                 win.app.notice, "Quire closed unexpectedly last time.")
        sourceList.noticeDismissed()
        win.want("dismissing the notice clears it", win.app.notice, "")

        // A status with no payload at all is still an answer.
        backend.message(Msg.Pong, "", false)
        win.want("an empty status still answers", win.app.backendStatus, "Backend answered.")
        win.want("and reads as robots off", settings.consultRobots, false)

        // A payload that is not JSON is reported rather than thrown past.
        backend.message(Msg.Pong, "{not json", false)
        win.want("an unreadable payload is reported",
                 win.app.lastError, "The backend sent something unreadable.")
        settings.clearErrorRequested()
        win.want("and the settings screen can clear it", win.app.lastError, "")

        // ---- the layout switch does not flip optimistically ----------------
        //
        // The value comes back on the *status*, never in a reply of its own, so
        // the screen draws what the store says rather than what the switch
        // hoped. A write that did not save cannot leave the two disagreeing.

        win.deliver(Msg.Pong, {"version": "1.4.0", "os": "linux", "arch": "arm64",
                               "views": {"search": "list", "downloaded": "list",
                                         "watching": "grid"}})
        backend.forget()
        win.app.setView("downloaded", Views.GRID)
        win.want("asking for a layout sets it and then asks for the status",
                 backend.types(), Msg.SetView + "," + Msg.Ping)
        win.want("naming the screen on the wire", backend.bodyOf(Msg.SetView).screen,
                 "downloaded")
        win.want("and the layout asked for", backend.bodyOf(Msg.SetView).view, Views.GRID)
        win.want("**and the screen does not flip on its own**",
                 win.app.downloadedView, Views.LIST)
        win.want("so the screen is still drawn the stored way",
                 downloadedList.view, Views.LIST)

        win.deliver(Msg.Pong, {"version": "1.4.0", "os": "linux", "arch": "arm64",
                               "views": {"search": "list", "downloaded": "grid",
                                         "watching": "grid"}})
        win.want("the status is what turns it over", win.app.downloadedView, Views.GRID)
        win.want("and the screen with it", downloadedList.view, Views.GRID)

        backend.forget()
        win.app.setView("sources", Views.LIST)
        win.want("a screen with no layout to remember sends nothing",
                 backend.sendCount, 0)

        // The same thing by hand, from the header. The toggle lives in the
        // chrome, so it is the one control that belongs to Main.qml itself.
        win.app.showScreen("watching")
        var toggle = win.findChild(win.app, "viewToggle")
        win.want("the header offers the switch on a screen that remembers one",
                 toggle.visible, true)
        win.want("showing the layout stored for that screen", toggle.view, Views.GRID)
        win.want("and the half you are in is inert",
                 win.findChild(toggle, "viewToggleGridArea").enabled, false)

        backend.forget()
        win.findChild(toggle, "viewToggleListArea").clicked(null)
        win.want("tapping the other half asks once",
                 backend.countOf(Msg.SetView), 1)
        win.want("for that screen", backend.bodyOf(Msg.SetView).screen, "watching")
        win.want("and that layout", backend.bodyOf(Msg.SetView).view, Views.LIST)
        win.want("and asks for the status behind it", backend.countOf(Msg.Ping), 1)
        win.want("the switch does not move until the store says so",
                 win.app.watchingView, Views.GRID)

        win.deliver(Msg.Pong, {"version": "1.4.0", "os": "linux", "arch": "arm64",
                               "views": {"watching": "list"}})
        win.want("and moves when it does", win.app.watchingView, Views.LIST)
        win.want("the toggle follows", toggle.view, Views.LIST)
        win.want("with the other half now the live one",
                 win.findChild(toggle, "viewToggleGridArea").enabled, true)

        win.app.showScreen("sources")
        win.want("a screen with nothing to remember draws no switch",
                 toggle.visible, false)

        // ---- showScreen, and what a screen re-asks for ---------------------

        backend.forget()
        win.app.showScreen("downloaded")
        win.want("opening Downloaded re-asks for the list",
                 backend.types(), String(Msg.ListDownloaded))
        win.want("with an empty payload", backend.rawOf(Msg.ListDownloaded), "{}")
        win.want("and the screen is the one showing", win.app.screen, "downloaded")
        win.want("which is the visible one", downloadedList.visible, true)
        win.want("and the others are not", sourceList.visible, false)

        // The private Downloaded screen (round 2): the same DownloadedList
        // instance, in its private mode, asking with private:true and
        // going back to the private source list rather than the ordinary
        // one.
        backend.forget()
        win.app.showScreen("downloadedPrivate")
        win.want("opening the private Downloaded screen asks with private:true",
                 backend.bodyOf(Msg.ListDownloaded).private, true)
        win.want("the same screen instance shows it", downloadedList.visible, true)
        win.want("titled for the private mode", win.app.screenTitle(), "Private · Downloaded")
        win.app.goBack()
        win.want("back goes to the private source list, not the ordinary one",
                 win.app.screen, "privateSources")

        backend.forget()
        win.app.showScreen("browse")
        win.want("a screen that needs nothing re-asks for nothing",
                 backend.sendCount, 0)
        win.app.goBack()
        win.want("Back from a listing goes to the sources", win.app.screen, "sources")

        // ---- the source list -----------------------------------------------

        win.deliver(Msg.Sources, {"sources": [
            {"id": "src-a", "name": "Example Reader", "baseUrl": "https://example.invalid",
             "theme": "madara", "lang": "en", "enabled": true, "splitStrips": "never",
             "status": "Working", "statusDetail": ""},
            {"id": "src-b", "name": "Other Reader", "baseUrl": "https://other.invalid",
             "theme": "mangadex", "lang": "en", "enabled": false,
             "status": "Off", "statusDetail": ""}]})
        win.want("the sources fill the list", sourceList.model.count, 2)
        win.want("with the name the backend gave", sourceList.model.get(0).name,
                 "Example Reader")
        win.want("and its stored splitting", sourceList.model.get(0).splitStrips, "never")
        // A source stored before PLAN §12.3 existed has none, and it must read
        // as Automatic rather than as blank.
        win.want("a source with none reads as automatic",
                 sourceList.model.get(1).splitStrips, "auto")
        win.want("and its enabled flag survives the trip",
                 sourceList.model.get(1).enabled, false)

        backend.forget()
        sourceList.toggleRequested("src-b", true)
        win.want("a toggle names the source",
                 backend.bodyOf(Msg.SetSourceEnabled).sourceId, "src-b")
        win.want("and what it was set to",
                 backend.bodyOf(Msg.SetSourceEnabled).enabled, true)
        sourceList.renameRequested("src-b", "A new name")
        win.want("a rename carries the name",
                 backend.bodyOf(Msg.RenameSource).name, "A new name")
        sourceList.splitStripsRequested("src-b", "always")
        win.want("splitting sends the schema's spelling",
                 backend.bodyOf(Msg.SetSourceSplitStrips).splitStrips, "always")
        sourceList.removeRequested("src-b")
        win.want("and a removal names it",
                 backend.bodyOf(Msg.RemoveSource).sourceId, "src-b")

        // ---- private sources -------------------------------------------------
        //
        // One SourceList instance and one SearchAll instance serve both
        // scopes (ui/Main.qml's sourceListScreen and searchAllScreen,
        // ui/SourceList.qml's showingPrivate) — an efficient reuse and
        // exactly the shape where a row, or a request, from the scope just
        // left survives the switch. There is no per-row filter anywhere in
        // ui/ (SourceList.qml draws whatever model it is given); the
        // guarantee that a private source never reaches the normal screen is
        // that the two lists are always fed by two different messages into
        // two different models, and that is what is asserted below — by what
        // is actually on screen, not by what was sent.

        backend.forget()
        win.deliver(Msg.Sources, {"sources": [
            {"id": "pub-1", "name": "Public Site", "baseUrl": "https://pub.invalid",
             "theme": "generic", "lang": "en", "enabled": true,
             "status": "Working", "statusDetail": ""}]})
        win.deliver(Msg.PrivateSources, {"sources": [
            {"id": "priv-1", "name": "Undercover Site", "baseUrl": "https://priv.invalid",
             "theme": "generic", "lang": "en", "enabled": true,
             "status": "Working", "statusDetail": ""}]})

        win.want("delivering the private list does not touch the normal one",
                 sourceList.model.count, 1)
        win.want("which still names the public source",
                 sourceList.model.get(0).name, "Public Site")
        win.want("and never draws the private one on the normal screen",
                 win.wordsOn(sourceList).indexOf("Undercover Site") >= 0, false)
        win.want("receiving both lists navigates nowhere by itself",
                 win.app.screen, "sources")

        // The eye-icon button: found by its objectName, never by a label —
        // it carries none, on purpose (ui/PrivateMark.qml) — and it is the
        // only way into the private list.
        var eyeButton = win.findChild(sourceList, "privateListButton")
        win.want("the eye button is there to find", eyeButton !== null, true)
        win.want("on the normal list, where it belongs", eyeButton.visible, true)
        win.want("carrying no label, no count and no badge of its own",
                 win.wordsOn(eyeButton), "")

        backend.forget()
        sourceList.privateListRequested()
        win.want("the eye button opens the private list",
                 win.app.screen, "privateSources")
        win.want("fetching it fresh, the way Downloaded is",
                 backend.types(), String(Msg.ListPrivateSources))
        win.want("this instance now knows it is the private one",
                 sourceList.showingPrivate, true)
        win.want("and the eye button is not offered on the list it opens",
                 eyeButton.visible, false)

        // **Repopulated, not merely relabelled.** The same ListView is
        // reused for both scopes, which is exactly the shape where a row from
        // the scope just left survives the switch.
        win.want("the private list shows the private source",
                 sourceList.model.count, 1)
        win.want("named as it arrived",
                 sourceList.model.get(0).name, "Undercover Site")
        win.want("and only that — nothing from the normal list rides along",
                 win.wordsOn(sourceList).indexOf("Public Site") >= 0, false)

        // private→normal is the direction that leaks: opening the private
        // list starts from a screen that had nothing on it yet, but going
        // back has to actually *replace* rows already drawn on screen.
        win.app.goBack()
        win.want("Back from the private list returns to the normal one",
                 win.app.screen, "sources")
        win.want("the model is repopulated, not left showing the private row",
                 sourceList.model.count, 1)
        win.want("back to the public source",
                 sourceList.model.get(0).name, "Public Site")
        win.want("the private row does not survive the trip back",
                 win.wordsOn(sourceList).indexOf("Undercover Site") >= 0, false)
        win.want("and the eye button is back too",
                 eyeButton.visible, true)

        // ---- marking and unmarking private -----------------------------------

        backend.forget()
        sourceList.privateRequested("pub-1", true)
        win.want("marking a source private names it",
                 backend.bodyOf(Msg.SetSourcePrivate).sourceId, "pub-1")
        win.want("and which way it moved",
                 backend.bodyOf(Msg.SetSourcePrivate).private, true)

        backend.forget()
        sourceList.privateRequested("priv-1", false)
        win.want("taking it back names the source too",
                 backend.bodyOf(Msg.SetSourcePrivate).sourceId, "priv-1")
        win.want("and the reverse direction",
                 backend.bodyOf(Msg.SetSourcePrivate).private, false)

        // ---- the private combined search ---------------------------------
        //
        // One SearchAll instance for both scopes too (root.searchAllPrivate).
        // The scope has to travel with the request itself, not just with
        // which screen happens to be on top when it is sent.

        backend.forget()
        sourceList.searchAllRequested()
        win.want("Search all, from the normal list, opens the normal search",
                 win.app.screen, "searchall")
        searchAll.query = "manga"
        searchAll.searchRequested("manga")
        win.want("it asks every source", backend.countOf(Msg.SearchAll), 1)
        win.want("never the private message",
                 backend.countOf(Msg.SearchAllPrivate), 0)

        win.app.goBack()
        win.want("Back from the normal search returns to the normal list",
                 win.app.screen, "sources")
        sourceList.privateListRequested()
        win.want("on the private list now", win.app.screen, "privateSources")

        backend.forget()
        sourceList.searchAllRequested()
        win.want("Search all, from the private list, opens the private search",
                 win.app.screen, "searchAllPrivate")
        searchAll.query = "manga"
        searchAll.searchRequested("manga")
        win.want("it asks only the private sources",
                 backend.countOf(Msg.SearchAllPrivate), 1)
        win.want("carrying the query",
                 backend.bodyOf(Msg.SearchAllPrivate).query, "manga")
        win.want("and never the normal message",
                 backend.countOf(Msg.SearchAll), 0)

        win.app.goBack()
        win.want("Back from the private search returns to the private list",
                 win.app.screen, "privateSources")
        win.app.goBack()
        win.want("and Back from there returns to the normal list",
                 win.app.screen, "sources")

        // ---- the private list's own empty state --------------------------
        //
        // The only place the honest limit is stated: marking a source
        // private does not touch anything already downloaded from it.
        backend.forget()
        win.deliver(Msg.PrivateSources, {"sources": []})
        sourceList.privateListRequested()
        win.want("an empty private list is empty", sourceList.model.count, 0)
        win.want("and says what marking a source private does and does not do",
                 win.wordsOn(sourceList).indexOf("reMarkable library") >= 0, true)
        win.app.goBack()
        win.want("leaving it back on the normal list", win.app.screen, "sources")

        // ---- adding a source ------------------------------------------------

        sourceList.addRequested()
        win.want("Add opens the form", win.app.screen, "add")
        win.want("on a fresh one", addSource.phase, "form")
        backend.forget()
        addSource.probeRequested("https://example.invalid")
        win.want("the check is asked for once", backend.countOf(Msg.ProbeSource), 1)
        win.want("naming the address typed",
                 backend.bodyOf(Msg.ProbeSource).url, "https://example.invalid")

        addSource.phase = "probing"
        win.deliver(Msg.ProbeVerdict, {
            "headline": "Looks like a Madara site", "detail": "Found 20 series.",
            "addable": true, "name": "Example Reader", "theme": "madara", "lang": "en",
            "url": "https://example.invalid"})
        win.want("the verdict reaches the form", addSource.phase, "verdict")
        win.want("headline and all", addSource.verdictHeadline, "Looks like a Madara site")
        win.want("and the name it proposes", addSource.draftName, "Example Reader")

        // An answer to a probe question goes out as one message, choice and
        // typed value together (PLAN §7.5 stage 1 offers a proxy with the
        // private-address question). One channel, because the probe has one
        // place where it stops and asks.
        backend.forget()
        addSource.answerRequested("continue", "http://localhost:1055")
        win.want("an answer is sent once", backend.countOf(Msg.ProbeAnswer), 1)
        win.want("naming the option chosen", backend.bodyOf(Msg.ProbeAnswer).id, "continue")
        win.want("and carrying what was typed with it",
                 backend.bodyOf(Msg.ProbeAnswer).text, "http://localhost:1055")
        addSource.answerRequested("cancel", "")
        win.want("a question with nothing typed sends an empty value",
                 backend.bodyOf(Msg.ProbeAnswer).text, "")

        backend.forget()
        addSource.confirmRequested("https://example.invalid", "madara", "Example Reader", "en")
        win.want("confirming adds the source",
                 backend.bodyOf(Msg.ConfirmAddSource).theme, "madara")
        win.want("and leaves the form behind", win.app.screen, "sources")

        // An error mid-check is the verdict, not a silence.
        sourceList.addRequested()
        addSource.phase = "probing"
        win.deliver(Msg.Error, {"message": "The site did not answer."})
        win.want("an error during a check ends the check", addSource.phase, "verdict")
        win.want("saying so", addSource.verdictHeadline, "Couldn't finish")
        win.want("in the backend's words", addSource.verdictDetail,
                 "The site did not answer.")
        win.want("and it is the app's last error too", win.app.lastError,
                 "The site did not answer.")
        addSource.doneRequested()
        settings.clearErrorRequested()

        // ---- browsing one source --------------------------------------------

        backend.forget()
        sourceList.openRequested("src-a", "Example Reader")
        win.want("opening a source browses it", win.app.screen, "browse")
        win.want("and remembers which", win.app.currentSourceId, "src-a")
        win.want("by name as well as by id", win.app.currentSourceName, "Example Reader")
        win.want("the grid waits for its first page", seriesGrid.busy, true)
        win.want("and names the page it is waiting for", seriesGrid.pendingPage, 1)
        win.want("a listing is a browse, not a search", backend.countOf(Msg.Browse), 1)
        win.want("and not a search", backend.countOf(Msg.Search), 0)
        win.want("naming the source", backend.bodyOf(Msg.Browse).sourceId, "src-a")
        win.want("the page asked for", backend.bodyOf(Msg.Browse).page, 1)
        // The page size is the grid's own geometry's answer, never a constant.
        win.want("and how much fits on this panel",
                 backend.bodyOf(Msg.Browse).pageSize, seriesGrid.pageSize)
        win.want("which is a real number of tiles", seriesGrid.pageSize > 0, true)

        backend.forget()
        seriesGrid.searchRequested("lantern")
        win.want("a query makes it a search", backend.countOf(Msg.Search), 1)
        win.want("carrying the query", backend.bodyOf(Msg.Search).query, "lantern")
        win.want("and no browse with it", backend.countOf(Msg.Browse), 0)

        // ---- one page of results --------------------------------------------

        backend.forget()
        win.deliver(Msg.SearchResults, win.seriesReply(2, 5, true))
        win.want("the results fill the grid", seriesGrid.model.count, 2)
        win.want("titled as they arrived", seriesGrid.model.get(0).title,
                 "The Lantern Keeper")
        win.want("with no cover on disk yet", seriesGrid.model.get(0).coverPath, "")
        // fillSeries joins multiple authors with ", " into one string, since a
        // ListModel role cannot reliably hold a list.
        win.want("multiple authors are comma-joined",
                 seriesGrid.model.get(0).authors, "Frank Herbert, Brian Herbert")
        // A row with no authors at all gets the role anyway, empty — a
        // ListModel fixes its roles on the first append, so the second row
        // could never be given one later if the first had skipped it.
        win.want("a row with no authors carries an empty string, not undefined",
                 seriesGrid.model.get(1).authors, "")
        win.want("the page is where the backend says", seriesGrid.page, 2)
        win.want("the total is the backend's too", seriesGrid.totalPages, 5)
        win.want("and whether there is more", seriesGrid.hasMore, true)
        win.want("the grid stops waiting", seriesGrid.busy, false)
        win.want("and forgets the page it was waiting for", seriesGrid.pendingPage, 0)
        win.want("a page with rows says nothing about being empty",
                 seriesGrid.emptyMessage, "")

        // The cover batch. This screen's rows carry no source of their own —
        // every row is the source being browsed — so the batch is what
        // Main.qml knows and the rows do not.
        win.want("one batch of covers is asked for", backend.countOf(Msg.RequestCover), 1)
        win.want("holding only the rows that have a cover to fetch",
                 backend.bodyOf(Msg.RequestCover).covers.length, 1)
        win.want("each entry named with the source being browsed",
                 backend.bodyOf(Msg.RequestCover).covers[0].sourceId, "src-a")
        win.want("and that source's series id",
                 backend.bodyOf(Msg.RequestCover).covers[0].seriesId, "/manga/lantern/")

        // **An empty batch is not sent at all.** SeriesGrid reports an empty
        // set rather than staying quiet, and the backend cancels the batch in
        // flight before it looks at whether the new one is empty — so a
        // message here would cancel a page of covers for nothing.
        backend.forget()
        win.deliver(Msg.SearchResults, {"series": [], "page": 1})
        win.want("a page with nothing on it says so",
                 seriesGrid.emptyMessage, "Nothing came back for that.")
        win.want("and empties the grid", seriesGrid.model.count, 0)
        win.want("**and asks for no covers at all**",
                 backend.countOf(Msg.RequestCover), 0)
        win.want("having sent nothing whatsoever", backend.sendCount, 0)

        // A page turn is composed in the one place that knows whether the
        // screen is showing a search or the site's own listing, so turning the
        // page of a search stays a search.
        backend.forget()
        seriesGrid.pageRequested(3)
        win.want("turning the page of a search stays a search",
                 backend.countOf(Msg.Search), 1)
        win.want("asking for the page turned to", backend.bodyOf(Msg.Search).page, 3)
        win.want("and never re-browses the catalogue underneath it",
                 backend.countOf(Msg.Browse), 0)

        // ---- a cover arrives ------------------------------------------------
        //
        // The same series can be on four screens at once, and the fan-out is
        // what keeps a cover already on disk from being fetched again for each
        // of them. So all four are filled and all four are asserted.

        win.deliver(Msg.SearchResults, win.seriesReply(1, 1, false))
        win.deliver(Msg.DownloadedList, win.downloadedReply())
        win.deliver(Msg.WatchList, win.watchReply([win.lanternWatch, win.orphanWatch], {}))
        win.app.openSearchAll()
        searchAll.query = "lantern"
        win.deliver(Msg.SearchAllResults, {
            "query": "lantern", "page": 1, "totalPages": 0, "hasMore": true,
            "groups": [win.lanternGroup, win.orphanGroup], "sourceErrors": []})

        win.want("the search grid is showing the series", seriesGrid.model.count, 2)
        win.want("the combined search is too", searchAll.model.count, 2)
        win.want("the downloaded overview is too", downloadedList.model.count, 2)
        win.want("and the watched list is too", watchList.model.count, 2)

        win.deliver(Msg.CoverReady, {"seriesId": "/manga/lantern/",
                                     "path": "/tmp/lantern.png"})
        win.want("the cover lands on the search grid's row",
                 seriesGrid.model.get(0).coverPath, "file:///tmp/lantern.png")
        win.want("on the combined search's row",
                 searchAll.model.get(0).coverPath, "file:///tmp/lantern.png")
        win.want("on the downloaded row",
                 downloadedList.model.get(0).coverPath, "file:///tmp/lantern.png")
        win.want("and on the watched row",
                 watchList.model.get(0).coverPath, "file:///tmp/lantern.png")
        win.want("and nowhere it does not belong",
                 seriesGrid.model.get(1).coverPath, "")

        // A cover for a series no screen is showing is a log line, not a
        // throw, and it writes nothing anywhere.
        win.deliver(Msg.CoverReady, {"seriesId": "/manga/nobody/", "path": "/tmp/x.png"})
        win.want("a cover for a series nothing shows writes nothing",
                 seriesGrid.model.get(0).coverPath, "file:///tmp/lantern.png")
        win.want("nor on the other screens",
                 downloadedList.model.get(0).coverPath + "," +
                 watchList.model.get(0).coverPath,
                 "file:///tmp/lantern.png,file:///tmp/lantern.png")
        // A cover with no series id at all is ignored before anything is
        // written, which is what stops a malformed reply blanking a row.
        win.deliver(Msg.CoverReady, {"path": "/tmp/y.png"})
        win.want("a cover naming no series is ignored",
                 seriesGrid.model.get(0).coverPath, "file:///tmp/lantern.png")

        // ---- the combined search --------------------------------------------

        win.want("the combined search is the screen showing", win.app.screen, "searchall")
        win.want("a group is one row", searchAll.model.count, 2)
        win.want("titled with the group's title", searchAll.model.get(0).title,
                 "The Lantern Keeper")
        win.want("opening on the first match's source",
                 searchAll.model.get(0).sourceId, "src-a")
        win.want("and that source's own series id",
                 searchAll.model.get(0).seriesId, "/manga/lantern/")
        win.want("the screen stops waiting", searchAll.busy, false)
        win.want("and a page of groups is not empty", searchAll.emptyMessage, "")

        // A source that did not answer is a line underneath, never an error
        // over the results.
        win.deliver(Msg.SearchAllResults, {
            "query": "lantern", "page": 1, "totalPages": 0, "hasMore": true,
            "groups": [win.lanternGroup, win.orphanGroup],
            "sourceErrors": [{"sourceId": "src-c", "sourceName": "Third Reader",
                              "message": "the request timed out"}]})
        win.want("a source that failed is named under the results",
                 searchAll.failedSources, "No answer from Third Reader")
        win.want("and is not an error", win.app.lastError, "")
        win.want("nor does it empty the page", searchAll.model.count, 2)

        // ---- a cover owned by a source other than the one a group opens on --
        //
        // This is the regression: a group's cover can come from a source that
        // is not the one the group opens on (mixedOwnerGroup). The request for
        // it must go out under the cover's own source, and the reply for that
        // source's fetch must land on this row even though the row's own
        // seriesId names a different series.
        win.deliver(Msg.SearchAllResults, {
            "query": "mixed", "page": 1, "totalPages": 0, "hasMore": false,
            "groups": [win.mixedOwnerGroup], "sourceErrors": []})
        win.want("the row opens on its first match",
                 searchAll.model.get(0).sourceId, "src-a")
        backend.forget()
        searchAll.requestVisibleCovers()
        win.want("the cover is asked for once", backend.countOf(Msg.RequestCover), 1)
        win.want("under the cover's own source, not the row's",
                 backend.bodyOf(Msg.RequestCover).covers[0].sourceId, "src-b")
        win.want("and that source's own series id",
                 backend.bodyOf(Msg.RequestCover).covers[0].seriesId,
                 "/series/beta-owns-this/")

        win.deliver(Msg.CoverReady, {"sourceId": "src-b",
                                     "seriesId": "/series/beta-owns-this/",
                                     "path": "/tmp/mixed.png"})
        win.want("the reply lands on the row despite the row's own seriesId differing",
                 searchAll.model.get(0).coverPath, "file:///tmp/mixed.png")

        // **An empty query sends nothing.** The screen has the first guard and
        // this is the second, here because this is where the message is
        // composed and three routes arrive at it.
        backend.forget()
        searchAll.query = ""
        searchAll.searchRequested("")
        win.want("an empty query is not a search", backend.countOf(Msg.SearchAll), 0)
        searchAll.query = "   "
        searchAll.pageRequested(2)
        win.want("nor is a page of one", backend.countOf(Msg.SearchAll), 0)
        searchAll.query = "lantern"
        searchAll.searchRequested("lantern")
        win.want("a real query is", backend.countOf(Msg.SearchAll), 1)
        win.want("carrying the query", backend.bodyOf(Msg.SearchAll).query, "lantern")
        win.want("and how much fits", backend.bodyOf(Msg.SearchAll).pageSize,
                 searchAll.pageSize)

        // ---- filtering the results by kind ----------------------------------
        //
        // Books and comics arrive on the same screen from different sources,
        // which is the whole reason the filter is here and on no other screen.
        // **The filter is part of the search, not a view on a page already in
        // hand.** Choosing a kind re-runs the query from page 1 asking the
        // backend to search only that kind (backend/service/searchall.go) — a
        // Books search never touches a manga site.

        // **A reply must not re-fire the search that produced it.** A page
        // landing asks for its covers and nothing else: a combined search that
        // answered its own reply would be an endless fan-out to every
        // configured source, which is invisible here and minutes of work per
        // round against a slow instance on the device.
        backend.forget()
        win.deliver(Msg.SearchAllResults, win.mixedReply())
        win.want("a page landing asks for its covers", backend.countOf(Msg.RequestCover), 1)
        win.want("**and never asks for itself again**",
                 backend.countOf(Msg.SearchAll), 0)
        win.want("having sent nothing else whatsoever", backend.sendCount, 1)

        win.want("a mixed page holds every group", searchAll.model.count, 3)
        win.want("with the book's kind on its row", searchAll.model.get(2).kind, "book")
        win.want("**and manga on the group whose reply never said**",
                 searchAll.model.get(0).kind, "manga")
        win.want("the book's row says what it is, beside where it is",
                 searchAll.model.get(2).sources, "Book · Shelfmark")
        win.want("while a comic's row is what it always was",
                 searchAll.model.get(0).sources, "Example Reader · Other Reader")
        win.want("and its tile is marked in the badge corner",
                 searchAll.model.get(2).badge, "Book")

        var kindNote = win.findChild(searchAll, "kindFilterNote")
        win.want("so the screen says nothing about a filter", kindNote.text, "")

        // Tapped, on the control the user would touch — not showKind(), which
        // would step straight over `enabled`. Choosing Books re-runs the
        // query the screen already has, from page 1, naming the kind.
        backend.forget()
        win.findChild(searchAll, "kindFilterArea-book").clicked(null)
        win.want("tapping Books asks the backend again", backend.countOf(Msg.SearchAll), 1)
        win.want("carrying the query already on screen",
                 backend.bodyOf(Msg.SearchAll).query, "lantern")
        win.want("from page 1", backend.bodyOf(Msg.SearchAll).page, 1)
        win.want("**naming the kind**", backend.bodyOf(Msg.SearchAll).kind, "book")
        win.want("and moves the filter", searchAll.kindFilter, Kinds.BOOK)
        win.want("**and says so in words**", kindNote.text, "Showing books only.")

        // The backend answers with only the book: a filtered search never
        // even asked the manga sources, so there is nothing here to hide.
        backend.forget()
        win.deliver(Msg.SearchAllResults, {
            "query": "lantern", "page": 1, "totalPages": 0, "hasMore": false,
            "groups": [win.duneGroup], "sourceErrors": []})
        win.want("the reply is the whole of the page", searchAll.model.count, 1)
        win.want("to the book", searchAll.model.get(0).title, "Dune Messiah")
        win.want("covers are asked for once", backend.countOf(Msg.RequestCover), 1)
        win.want("which is the book's",
                 backend.bodyOf(Msg.RequestCover).covers[0].seriesId, "/book/dune-messiah")

        backend.forget()
        win.findChild(searchAll, "kindFilterArea-manga").clicked(null)
        win.want("tapping Manga asks again too", backend.countOf(Msg.SearchAll), 1)
        win.want("**naming manga this time**", backend.bodyOf(Msg.SearchAll).kind, "manga")
        win.deliver(Msg.SearchAllResults, {
            "query": "lantern", "page": 1, "totalPages": 0, "hasMore": false,
            "groups": [win.lanternGroup, win.orphanGroup], "sourceErrors": []})
        win.want("tapping Manga shows the other two", searchAll.model.count, 2)
        win.want("**the one that said so and the one that said nothing**",
                 searchAll.model.get(0).title + "," + searchAll.model.get(1).title,
                 "The Lantern Keeper,An Orphan")
        win.want("still saying so", kindNote.text, "Showing manga only.")

        // A filtered search that came back with nothing is **not** the plain
        // "nothing came back" sentence: the screen names what was searched
        // for, which is the way out that stays on screen.
        win.findChild(searchAll, "kindFilterArea-book").clicked(null)
        win.deliver(Msg.SearchAllResults, {
            "query": "lantern", "page": 1, "totalPages": 0, "hasMore": false,
            "groups": [], "sourceErrors": []})
        win.want("a filtered search with nothing back has no rows",
                 searchAll.model.count, 0)
        win.want("**and says it was the filter**",
                 searchAll.emptyMessage, "No books in these results.")
        win.want("with the way out still named on the screen",
                 kindNote.text, "Showing books only.")

        backend.forget()
        win.findChild(searchAll, "kindFilterArea-all").clicked(null)
        win.want("All asks again too", backend.countOf(Msg.SearchAll), 1)
        win.want("**naming no kind at all**", backend.bodyOf(Msg.SearchAll).kind, "")

        win.deliver(Msg.SearchAllResults, {
            "query": "lantern", "page": 1, "totalPages": 0, "hasMore": false,
            "groups": [], "sourceErrors": []})
        win.want("an unfiltered search that really found nothing says that",
                 searchAll.emptyMessage, "Nothing came back for that.")

        win.deliver(Msg.SearchAllResults, win.mixedReply())
        win.want("All brings the rest back", searchAll.model.count, 3)
        win.want("and saying nothing", kindNote.text, "")

        // ---- and what a book opens onto --------------------------------------
        //
        // The series screen is told what it is showing by the row that opened
        // it: MessageSeriesDetailResult carries no kind, and inventing one on
        // the wire to save the lookup would be the frontend adding a field to
        // the protocol.

        var kindTiles = win.findChild(searchAll, "searchAllTiles")
        win.findChild(kindTiles, "coverTiles").forceLayout()
        var kindTileAreas = win.findChildren(kindTiles, "coverTileArea", [])
        win.want("every group on the page is a tap target", kindTileAreas.length, 3)
        kindTileAreas[2].clicked(null)
        win.want("tapping a book opens a series", win.app.screen, "series")
        win.want("**and the screen knows it is a book**", chapterList.kind, "book")
        win.want("so it says what its list holds",
                 win.findChild(chapterList, "releaseNote").text,
                 "Releases · pick the file to download.")
        win.want("on a strip that is drawn",
                 win.findChild(chapterList, "releaseStrip").visible, true)
        win.want("and it cannot be watched",
                 win.findChild(chapterList, "watchButton").visible, false)
        win.want("**nor bulk-downloaded over its alternatives**",
                 win.findChild(chapterList, "selectButton").visible, false)

        // **Even a reply that sent volumes draws none.** A book has one series
        // and no volumes; the screen refuses the view rather than relying on
        // the reply to be empty.
        win.deliver(Msg.SeriesDetailResult, {
            "series": {"title": "Dune Messiah", "description": "A novel about spice."},
            "chapters": [{"id": "r0", "title": "EPUB · 0.4MB · Direct Download · fiction"}],
            "volumes": [{"id": "r0", "title": "Volume 1", "detail": "7 chapters",
                         "chapterCount": 7}]})
        win.want("the volumes really did arrive", chapterList.volumeModel.count, 1)
        win.want("**and a book offers no volume view anyway**",
                 win.findChild(chapterList, "viewSwitch").visible, false)
        win.want("the releases are the list", chapterList.model.count, 1)
        win.want("titled as the backend titled them",
                 chapterList.model.get(0).title, "EPUB · 0.4MB · Direct Download · fiction")

        // A book row now offers Try beside Download, the same as a manga
        // row's (books-contract §C) — asserted here on the real screen, in
        // the real shell, rather than only on the standalone fixture
        // (build/qmlcheck/Harness.qml).
        win.findChild(chapterList, "chapterRows").forceLayout()
        win.want("a book's release offers Try",
                 win.findChild(chapterList, "tryButton-r0").visible, true)

        // And the other half: a group that never mentioned a kind opens the
        // screen it has always opened.
        win.app.goBack()
        win.want("Back returns to the combined search", win.app.screen, "searchall")
        kindTileAreas[0].clicked(null)
        win.want("**a group with no kind opens as manga**", chapterList.kind, "manga")
        win.want("with no release wording on it",
                 win.findChild(chapterList, "releaseStrip").visible, false)
        win.want("and its watch button back",
                 win.findChild(chapterList, "watchButton").visible, true)
        win.app.goBack()
        win.app.showScreen("searchall")

        // ---- opening a group: the matches become the source switcher --------
        //
        // Tapped for real, on the tile the user would touch. What arrives at
        // openSeries is the group — every source the same series was found in
        // — and it is the only route that carries one.

        win.deliver(Msg.SearchAllResults, {
            "query": "lantern", "page": 1, "totalPages": 0, "hasMore": true,
            "groups": [win.lanternGroup, win.orphanGroup], "sourceErrors": []})
        var saTiles = win.findChild(searchAll, "searchAllTiles")
        win.findChild(saTiles, "coverTiles").forceLayout()
        var saTileAreas = win.findChildren(saTiles, "coverTileArea", [])
        win.want("every group on the page is a tap target", saTileAreas.length, 2)

        backend.forget()
        saTileAreas[0].clicked(null)
        win.want("tapping a group opens a series", win.app.screen, "series")
        win.want("on the first match's source", win.app.currentSourceId, "src-a")
        win.want("named as the group named it", win.app.currentSourceName,
                 "Example Reader")
        win.want("with that source's series id", win.app.currentSeriesId,
                 "/manga/lantern/")
        win.want("**and the group's matches become the source switcher**",
                 chapterList.sources.length, 2)
        win.want("in the reply's order, which is the user's source order",
                 chapterList.sources[0].sourceId + "," + chapterList.sources[1].sourceId,
                 "src-a,src-b")
        win.want("each carrying its own series id",
                 chapterList.sources[1].seriesId, "/series/lantern")
        win.want("so the strip of chips is drawn",
                 win.findChild(chapterList, "sourceStrip").visible, true)
        win.want("with a chip for the other source",
                 win.findChild(chapterList, "sourceChip-src-b") !== null, true)
        win.want("and the one being read is inert",
                 win.findChild(chapterList, "sourceChipArea-src-a").enabled, false)
        win.want("the detail is asked for", backend.countOf(Msg.SeriesDetail), 1)
        win.want("naming the pair", backend.bodyOf(Msg.SeriesDetail).sourceId, "src-a")
        win.want("both halves of it", backend.bodyOf(Msg.SeriesDetail).seriesId,
                 "/manga/lantern/")
        win.want("the screen waits for it", chapterList.busy, true)
        win.want("and Back remembers where the user came from",
                 win.app.seriesCameFrom, "searchall")

        // Switching source switches the *pair*, and keeps the alternatives:
        // they belong to the group, not to the source being read.
        backend.forget()
        win.findChild(chapterList, "sourceChipArea-src-b").clicked(null)
        win.want("a chip switches the source", win.app.currentSourceId, "src-b")
        win.want("and its name", win.app.currentSourceName, "Other Reader")
        win.want("**with that source's own series id**",
                 win.app.currentSeriesId, "/series/lantern")
        win.want("the detail is re-asked for the new pair",
                 backend.bodyOf(Msg.SeriesDetail).sourceId, "src-b")
        win.want("naming its series, not the old one",
                 backend.bodyOf(Msg.SeriesDetail).seriesId, "/series/lantern")
        win.want("**the alternatives survive the switch**",
                 chapterList.sources.length, 2)
        win.want("the title is kept rather than re-derived",
                 chapterList.seriesTitle, "The Lantern Keeper")
        win.want("and Back still goes where the user came from",
                 win.app.seriesCameFrom, "searchall")
        win.want("now the other chip is the inert one",
                 win.findChild(chapterList, "sourceChipArea-src-b").enabled, false)

        win.app.goBack()
        win.want("Back returns to the combined search", win.app.screen, "searchall")

        // ---- and a series reached any other way has no switcher -------------
        //
        // The other half of the same line: with no matches the list is empty,
        // not undefined, and the strip collapses to nothing.

        win.app.showScreen("downloaded")
        backend.forget()
        downloadedList.openRequested("src-a", "Example Reader", "/manga/lantern/",
                                     "The Lantern Keeper")
        win.want("a downloaded row opens the series", win.app.screen, "series")
        win.want("**with no alternatives at all**", chapterList.sources.length, 0)
        win.want("so no chips are drawn",
                 win.findChild(chapterList, "sourceStrip").visible, false)
        win.want("and Back goes back to Downloaded", win.app.seriesCameFrom, "downloaded")
        win.want("the detail is still asked for", backend.countOf(Msg.SeriesDetail), 1)

        // ---- the series detail ----------------------------------------------

        backend.forget()
        win.deliver(Msg.SeriesDetailResult, {
            "series": {"title": "The Lantern Keeper", "description": "A long description."},
            "chapters": [
                {"id": "c1", "title": "Chapter 1", "number": 1,
                 "published": "2026-01-01", "scanlator": "Group"},
                {"id": "c2", "title": "Chapter 2", "number": 2,
                 "published": "2026-01-02", "scanlator": "Group",
                 "documentUuid": "doc-1"}],
            "volumes": [
                {"id": "c1", "title": "Volume 1", "detail": "7 chapters",
                 "chapterCount": 7}]})
        win.want("the chapters fill the list", chapterList.model.count, 2)
        win.want("titled as they arrived", chapterList.model.get(0).title, "Chapter 1")
        win.want("with the scanlator the source named",
                 chapterList.model.get(0).scanlator, "Group")
        win.want("a chapter with nothing downloaded is blank",
                 chapterList.model.get(0).downloadState, "")
        win.want("and one with a document says so",
                 chapterList.model.get(1).downloadState, "done")
        win.want("carrying the document", chapterList.model.get(1).documentUuid, "doc-1")
        win.want("the volumes fill their own list", chapterList.volumeModel.count, 1)
        win.want("each naming its first chapter",
                 chapterList.volumeModel.get(0).chapterId, "c1")
        win.want("the header takes the series' title",
                 chapterList.seriesTitle, "The Lantern Keeper")
        win.want("which is what the chrome draws", win.app.screenTitle(),
                 "The Lantern Keeper")
        win.want("an ordinary reply carries no note", chapterList.note, "")
        win.want("so the note band is not drawn",
                 win.findChild(chapterList, "noteBand").visible, false)

        // ---- PLAN §12.12: a second SeriesDetailResult for the same series ----
        //
        // The offline-first flow can send this twice — a cached or
        // synthesised reply immediately, then a fresh one once the live
        // fetch lands. Neither the page nor an in-flight row's own progress
        // may be disturbed by the second one, and the note is shown or hidden
        // exactly as the reply says.

        // A page turned away from 1, and a download in progress on c1 — both
        // must survive the second reply below untouched.
        chapterList.page = 2
        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "downloading", "message": "Page 3 of 20."})
        win.want("the row is downloading before the second reply",
                 chapterList.model.get(0).downloadState, "downloading")

        win.deliver(Msg.SeriesDetailResult, {
            "series": {"title": "The Lantern Keeper", "description": "A long description."},
            "cached": true, "fetchedAt": "2026-03-01T00:00:00Z",
            "note": "Showing the chapter list from 3 days ago while Quire checks for new chapters.",
            "chapters": [
                {"id": "c1", "title": "Chapter 1", "number": 1,
                 "published": "2026-01-01", "scanlator": "Group"},
                {"id": "c2", "title": "Chapter 2", "number": 2,
                 "published": "2026-01-02", "scanlator": "Group",
                 "documentUuid": "doc-1"}],
            "volumes": [
                {"id": "c1", "title": "Volume 1", "detail": "7 chapters",
                 "chapterCount": 7}]})
        win.want("the second reply's note is shown verbatim", chapterList.note,
                 "Showing the chapter list from 3 days ago while Quire checks for new chapters.")
        win.want("and the band is drawn",
                 win.findChild(chapterList, "noteBand").visible, true)
        win.want("**the page is not reset by a second reply**", chapterList.page, 2)
        win.want("**and the in-flight row keeps its own progress**",
                 chapterList.model.get(0).downloadState, "downloading")
        win.want("with the sentence it already had",
                 chapterList.model.get(0).downloadMessage, "Page 3 of 20.")
        win.want("while an untouched row still reflects the reply",
                 chapterList.model.get(1).documentUuid, "doc-1")

        // A third reply with no note at all — the live fetch landed — hides
        // the band again, and leaves the still-downloading row exactly as it
        // was.
        win.deliver(Msg.SeriesDetailResult, {
            "series": {"title": "The Lantern Keeper", "description": "A long description."},
            "chapters": [
                {"id": "c1", "title": "Chapter 1", "number": 1,
                 "published": "2026-01-01", "scanlator": "Group"},
                {"id": "c2", "title": "Chapter 2", "number": 2,
                 "published": "2026-01-02", "scanlator": "Group",
                 "documentUuid": "doc-1"}],
            "volumes": [
                {"id": "c1", "title": "Volume 1", "detail": "7 chapters",
                 "chapterCount": 7}]})
        win.want("a fresh reply with no note clears it", chapterList.note, "")
        win.want("hiding the band", win.findChild(chapterList, "noteBand").visible, false)
        win.want("the page still holds", chapterList.page, 2)
        win.want("and the row is still downloading",
                 chapterList.model.get(0).downloadState, "downloading")

        // Reset for what follows below, which assumes page 1 and no
        // in-flight state of its own.
        chapterList.page = 1
        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "cancelled", "message": "Stopped."})
        win.want("cleared back to nothing before the rest of the checks",
                 chapterList.model.get(0).downloadState, "cancelled")

        // ---- the screen title, and the bar under it --------------------------
        //
        // The title is the one piece of chrome with the same shape on every
        // screen, so it is the one worth making findable by colour (PLAN §6 M3
        // as amended). It is **not** coloured itself: measured on the device,
        // coloured text reads muddy on this panel, and the title was the
        // largest coloured text in the app. The accent is a filled bar under
        // it instead (ui/Style.js, ui/AccentRule.qml).
        //
        // What is asserted with it is the part a stale palette would quietly
        // break: the title still *says* where you are, and the controls either
        // side of it are still black. A header where everything were marked
        // would pass "the title has a bar" and have lost the point.
        var titleText = win.findChild(win.app, "screenTitle")
        var titleRule = win.findChild(win.app, "screenTitleRule")
        win.want("the screen title is ink, like every other word",
                 win.colorOf(titleText), Style.ink)
        win.want("with the accent in a bar under it",
                 win.colorOf(titleRule), Style.accent)
        win.want("which is a solid area rather than a hairline",
                 titleRule.height >= Style.hairline * 4, true)
        win.want("and as wide as the title's own words",
                 Math.round(titleRule.width), Math.round(titleText.contentWidth))
        win.want("sitting under the title, not through it",
                 titleRule.y >= titleText.y + titleText.height, true)
        win.want("and still names the screen in words", titleText.text,
                 "The Lantern Keeper")
        win.want("while Back beside it stays ink",
                 win.colorOf(win.findChild(win.app, "backButton")), Style.ink)
        // The intent, which comparing against the token cannot catch: set
        // Style.accent to black and every assertion above still passes, and
        // the title is no longer marked at all.
        win.want("which is a different colour from the title it marks",
                 win.colorOf(titleRule) !== Style.ink, true)

        // The colour belongs to the chrome, not to this screen: it is still
        // there after navigating, with whatever title the new screen has.
        win.app.showScreen("settings")
        win.want("the bar keeps the accent on another screen",
                 win.colorOf(titleRule), Style.accent)
        win.want("and follows the new title's width",
                 Math.round(titleRule.width), Math.round(titleText.contentWidth))
        win.want("under that screen's own name", titleText.text, "Settings")

        // The chrome's own sweep, the counterpart to the one over the screens
        // in build/qmlcheck/Harness.qml: the header is the one part of the UI
        // that is not on any screen, so nothing there would catch a coloured
        // word in it.
        var chromeText = win.textsUnder(win.findChild(win.app, "screenTitle").parent, [])
        win.want("the header has its words on it", chromeText.length >= 3, true)
        var chromeOffenders = []
        for (var ci = 0; ci < chromeText.length; ++ci)
            if (win.colorOf(chromeText[ci]) === Style.accent)
                chromeOffenders.push(String(chromeText[ci].text))
        win.want("and not one of them is drawn in the accent",
                 chromeOffenders.join(" | "), "")
        win.app.showScreen("series")
        win.want("the synopsis is the backend's words", chapterList.synopsis,
                 "A long description.")
        win.want("and the screen stops waiting", chapterList.busy, false)
        win.want("a series with volumes offers the view switch",
                 win.findChild(chapterList, "viewSwitch").visible, true)

        // ---- a download's progress -------------------------------------------

        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "downloading", "message": "Page 3 of 20."})
        win.want("progress lands on the chapter's row",
                 chapterList.model.get(0).downloadState, "downloading")
        win.want("with the backend's sentence",
                 chapterList.model.get(0).downloadMessage, "Page 3 of 20.")
        win.want("and nowhere else", chapterList.model.get(1).downloadState, "done")

        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "confirm",
            "message": "That is 40 chapters. Download them all?"})
        win.want("a confirm phase asks in the strip", chapterList.confirmingId, "c1")
        win.want("in the backend's words", chapterList.confirmingMessage,
                 "That is 40 chapters. Download them all?")

        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "queued", "message": "Queued."})
        win.want("any other phase is an answer, so the question closes",
                 chapterList.confirmingId, "")

        // The same id is in both models — a volume's id is its first chapter's
        // — so the backend says which it meant.
        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "done", "grouping": "volume",
            "message": "Downloaded.", "documentUuid": "doc-vol"})
        win.want("a volume's progress lands on the volume row",
                 chapterList.volumeModel.get(0).downloadState, "done")
        win.want("carrying its document",
                 chapterList.volumeModel.get(0).documentUuid, "doc-vol")
        win.want("and leaves the chapter row where it was",
                 chapterList.model.get(0).downloadState, "queued")

        // A stopped download goes back to where it started rather than leaving
        // a message that looks like frozen progress.
        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "cancelled", "message": "Stopped."})
        win.want("a cancelled row keeps no message",
                 chapterList.model.get(0).downloadMessage, "")

        // ---- saved in Quire: a quire download's own "done" ---------------------
        //
        // The final "done" of a quire download carries saved: true — the row
        // goes to [Delete][Read] (saved) without waiting for a fresh
        // SeriesDetailResult, the same immediacy documentUuid already gets.
        win.want("not saved before the download finishes",
                 chapterList.model.get(0).saved, false)
        win.deliver(Msg.DownloadProgress, {
            "volumeId": "c1", "phase": "done", "message": "Chapter 1 is saved in Quire.",
            "destination": "quire", "saved": true})
        win.want("a quire download's done marks the row saved",
                 chapterList.model.get(0).saved, true)

        // Reading and deleting it go through OpenSaved/DeleteSaved, not the
        // library's own messages.
        backend.forget()
        chapterList.readSavedRequested("c1")
        win.want("reading a saved chapter asks OpenSaved",
                 backend.countOf(Msg.OpenSaved), 1)
        win.want("naming the chapter", backend.bodyOf(Msg.OpenSaved).chapterId, "c1")

        backend.forget()
        chapterList.deleteSavedRequested("c1")
        win.want("asking to delete a saved chapter sends DeleteSaved unconfirmed",
                 backend.bodyOf(Msg.DeleteSaved).confirmed, false)
        chapterList.deleteSavedConfirmed("c1")
        win.want("confirming sends it again, confirmed",
                 backend.bodyOf(Msg.DeleteSaved).confirmed, true)

        // SavedDeleted follows MessageDownloadDeleted's own pattern: "confirm"
        // opens the strip with the backend's question, "done" clears the flag
        // immediately (no refetch needed), "failed" reports the sentence and
        // closes the strip without touching the row.
        win.deliver(Msg.SavedDeleted, {
            "chapterId": "c1", "phase": "confirm",
            "message": "Delete Chapter 1 from Quire? It will need downloading again to read."})
        win.want("a confirm phase opens the strip", chapterList.confirmingId, "c1")
        win.want("as its own kind", chapterList.confirmingKind, "deleteSaved")
        win.want("with the backend's question", chapterList.confirmingMessage,
                 "Delete Chapter 1 from Quire? It will need downloading again to read.")

        win.deliver(Msg.SavedDeleted, {"chapterId": "c1", "phase": "done",
                                       "message": "Chapter 1 is deleted."})
        win.want("done clears the saved flag immediately",
                 chapterList.model.get(0).saved, false)
        win.want("and closes the strip", chapterList.confirmingId, "")

        win.deliver(Msg.SavedDeleted, {"chapterId": "c1", "phase": "failed",
                                       "message": "Could not delete Chapter 1."})
        win.want("a failed delete is reported",
                 win.app.lastError, "Could not delete Chapter 1.")
        win.want("without marking the row saved again",
                 chapterList.model.get(0).saved, false)

        // ---- what the series screen sends -------------------------------------

        backend.forget()
        chapterList.downloadRequested("c1")
        win.want("a download names the pair and the chapter",
                 backend.bodyOf(Msg.EnqueueDownload).volumeId, "c1")
        win.want("on the source being read",
                 backend.bodyOf(Msg.EnqueueDownload).sourceId, "src-a")
        // Comics/manga downloads are saved in Quire by default — never a
        // library upload unless the reader overlay's own "Send to library"
        // asks for one explicitly (ui/TryReader.qml).
        win.want("and destined for Quire's own storage",
                 backend.bodyOf(Msg.EnqueueDownload).destination, "quire")
        chapterList.volumeDownloadRequested("c1")
        win.want("a volume download says which grouping it meant",
                 backend.bodyOf(Msg.EnqueueDownload).grouping, "volume")
        win.want("and is still destined for Quire",
                 backend.bodyOf(Msg.EnqueueDownload).destination, "quire")
        chapterList.queueConfirmed(["c1", "c2"], false)
        win.want("a selection is queued in one message",
                 backend.countOf(Msg.EnqueueDownloads), 1)
        win.want("holding every row picked",
                 backend.bodyOf(Msg.EnqueueDownloads).chapterIds.length, 2)
        win.want("and saying they are chapters",
                 backend.bodyOf(Msg.EnqueueDownloads).grouping, "chapter")
        win.want("destined for Quire too",
                 backend.bodyOf(Msg.EnqueueDownloads).destination, "quire")

        // ---- deleting one download --------------------------------------------
        //
        // Two steps: the question is the backend's, the deleting is ours.

        backend.forget()
        chapterList.deleteRequested("doc-1")
        win.want("the first tap asks the backend for the question",
                 backend.countOf(Msg.DeleteDownload), 1)
        win.want("naming the document", backend.bodyOf(Msg.DeleteDownload).documentUuid,
                 "doc-1")
        win.want("and nothing is confirmed by it",
                 backend.bodyOf(Msg.DeleteDownload).confirmed, undefined)

        win.deliver(Msg.DeleteConfirm, {"documentUuid": "doc-1",
                                        "message": "Delete Chapter 2?"})
        win.want("the question lands in the strip", chapterList.confirmingKind, "delete")
        win.want("about that document", chapterList.confirmingId, "doc-1")
        win.want("in the backend's words", chapterList.confirmingMessage,
                 "Delete Chapter 2?")

        backend.forget()
        chapterList.deleteConfirmed("doc-1")
        win.want("answering it reports what happened",
                 backend.countOf(Msg.DeleteDownload), 1)
        win.want("as a confirmed delete",
                 backend.bodyOf(Msg.DeleteDownload).confirmed, true)
        // No bridge off the device, so nothing moved — and saying so is what
        // stops the backend forgetting a record whose document is still there.
        win.want("with no library, nothing was trashed",
                 backend.bodyOf(Msg.DeleteDownload).trashed, false)
        win.want("nor removed", backend.bodyOf(Msg.DeleteDownload).removed, false)
        win.want("and the reason travels with it",
                 backend.bodyOf(Msg.DeleteDownload).detail, Answers.NO_BRIDGE)

        // The store deciding is what clears the row, not the trash call.
        win.want("the row still carries its document",
                 chapterList.model.get(1).documentUuid, "doc-1")
        win.deliver(Msg.DownloadDeleted, {"documentUuid": "doc-1"})
        win.want("the backend forgetting it is what clears the row",
                 chapterList.model.get(1).documentUuid, "")
        win.want("and the row offers a download again",
                 chapterList.model.get(1).downloadState, "")

        // ---- handing a document to the reader ----------------------------------

        win.deliver(Msg.SeriesDetailResult, {
            "series": {"title": "The Lantern Keeper", "description": ""},
            "chapters": [{"id": "c2", "title": "Chapter 2", "number": 2,
                          "documentUuid": "doc-1"}],
            "volumes": []})
        backend.forget()
        chapterList.readRequested("doc-1")
        win.want("Read answers the backend once", backend.countOf(Msg.OpenInReader), 1)
        win.want("naming the document", backend.bodyOf(Msg.OpenInReader).documentUuid,
                 "doc-1")
        win.want("with no library, it reports it as missing",
                 backend.bodyOf(Msg.OpenInReader).missing, true)
        win.want("and says why", backend.bodyOf(Msg.OpenInReader).detail,
                 Answers.NO_BRIDGE)
        win.want("a document that did not open is cleared off the row",
                 chapterList.model.get(0).documentUuid, "")

        // ---- the downloaded overview -------------------------------------------

        win.app.showScreen("downloaded")
        backend.forget()
        win.deliver(Msg.DownloadedList, win.downloadedReply())
        win.want("the overview fills", downloadedList.model.count, 2)
        win.want("with the backend's own detail line",
                 downloadedList.model.get(0).detail, "3 downloads")
        win.want("whether the series can be opened",
                 downloadedList.model.get(0).openable, true)
        win.want("and its note when it has one",
                 downloadedList.model.get(1).note, "One file is missing.")
        win.want("carrying the newest download",
                 downloadedList.model.get(0).latestUuid, "doc-9")
        win.want("the page starts at the top", downloadedList.page, 1)
        win.want("one batch of covers is asked for",
                 backend.countOf(Msg.RequestCover), 1)
        win.want("holding only the row that has one",
                 backend.bodyOf(Msg.RequestCover).covers.length, 1)
        win.want("each entry naming its own source, not a fallback",
                 backend.bodyOf(Msg.RequestCover).covers[0].sourceId, "src-a")

        backend.forget()
        win.deliver(Msg.DownloadedList, {"series": [], "empty": "Nothing downloaded yet."})
        win.want("an empty library empties the list", downloadedList.model.count, 0)
        win.want("and says so in the backend's words",
                 downloadedList.emptyNote, "Nothing downloaded yet.")
        win.want("asking for no covers", backend.countOf(Msg.RequestCover), 0)

        win.deliver(Msg.DownloadedList, win.downloadedReply())

        // Deleting a whole series: the question, then the answer, then the
        // documents — which the backend names and the frontend deletes.
        backend.forget()
        downloadedList.deleteRequested("src-a", "/manga/lantern/")
        win.want("asking to delete a series asks the backend first",
                 backend.countOf(Msg.DeleteSeries), 1)
        win.want("and confirms nothing",
                 backend.bodyOf(Msg.DeleteSeries).confirmed, undefined)

        win.deliver(Msg.DeleteSeriesConfirm, {
            "sourceId": "src-a", "seriesId": "/manga/lantern/",
            "message": "Delete 2 downloads of The Lantern Keeper?",
            "documentUuids": ["doc-1", "doc-2"]})
        win.want("the documents come with the question",
                 win.app.confirmingSeriesUuids.length, 2)
        win.want("and the question reaches the screen",
                 downloadedList.confirmingMessage,
                 "Delete 2 downloads of The Lantern Keeper?")
        win.want("about that series", downloadedList.confirmingSeriesId,
                 "/manga/lantern/")

        backend.forget()
        downloadedList.deleteConfirmed("src-a", "/manga/lantern/")
        win.want("answering reports every document on its own",
                 backend.bodyOf(Msg.DeleteSeries).results.length, 2)
        win.want("as a confirmed delete",
                 backend.bodyOf(Msg.DeleteSeries).confirmed, true)
        win.want("with no library, none of them moved",
                 backend.bodyOf(Msg.DeleteSeries).results[0].trashed, false)
        win.want("and the reason travels once",
                 backend.bodyOf(Msg.DeleteSeries).detail, Answers.NO_BRIDGE)
        win.want("the question is spent", win.app.confirmingSeriesUuids.length, 0)

        backend.forget()
        downloadedList.deleteConfirmed("src-a", "/manga/lantern/")
        win.want("so answering it twice deletes nothing twice", backend.sendCount, 0)

        // The empty folder the backend then found.
        backend.forget()
        win.deliver(Msg.DeleteFolder, {"folderId": "f-1",
                                       "folderName": "The Lantern Keeper"})
        win.want("an empty folder is answered", backend.countOf(Msg.FolderDeleted), 1)
        win.want("echoing the name the backend composed",
                 backend.bodyOf(Msg.FolderDeleted).folderName, "The Lantern Keeper")
        win.want("with no library, it did not go",
                 backend.bodyOf(Msg.FolderDeleted).removed, false)

        // ---- a downloaded book ---------------------------------------------------
        //
        // The same lookup from the other direction: a downloaded row carries
        // the kind the backend gave it, and opening one is what tells the
        // series screen whether it is listing chapters or releases.

        win.app.showScreen("downloaded")
        win.deliver(Msg.DownloadedList, win.downloadedReply())
        win.want("a downloaded row carries its kind",
                 downloadedList.model.get(1).kind, "book")
        win.want("**and a row whose reply never said is manga**",
                 downloadedList.model.get(0).kind, "manga")
        // The screen spans every source, so its rows are marked — in the badge
        // corner on a tile, in front of the source name on a row.
        win.want("a downloaded book is marked", downloadedList.model.get(1).badge, "Book")
        win.want("**and a downloaded comic is not**",
                 downloadedList.model.get(0).badge, "")
        downloadedList.view = Views.LIST
        win.findChild(downloadedList, "downloadedRows").forceLayout()
        var dlLines = win.findChildren(downloadedList, "downloadedRowDetail", [])
        win.want("which is what the row says", dlLines[1].text,
                 "Book · Other Reader · 1 download — One file is missing.")
        win.want("while a comic's row is untouched", dlLines[0].text,
                 "Example Reader · 3 downloads")
        downloadedList.view = Views.GRID

        downloadedList.openRequested("src-b", "Other Reader", "/series/orphan",
                                     "An Orphan")
        win.want("opening a downloaded book opens a book", chapterList.kind, "book")
        win.want("which lists releases",
                 win.findChild(chapterList, "releaseStrip").visible, true)
        win.app.showScreen("downloaded")
        downloadedList.openRequested("src-a", "Example Reader", "/manga/lantern/",
                                     "The Lantern Keeper")
        win.want("and opening a downloaded comic opens a comic",
                 chapterList.kind, "manga")
        win.want("with no release wording",
                 win.findChild(chapterList, "releaseStrip").visible, false)

        // A series reached from the watched list has no kind to be had at all —
        // those rows predate books — and lands on the default rather than on
        // nothing.
        win.app.showScreen("watching")
        watchList.openRequested("src-a", "Example Reader", "/manga/lantern/",
                                "The Lantern Keeper")
        win.want("a series opened from the watched list is manga",
                 chapterList.kind, "manga")

        // Coming to the combined search fresh is a fresh question, filter and
        // all — the same state the app starts in.
        win.app.openSearchAll()
        win.want("opening the combined search resets the filter",
                 searchAll.kindFilter, Kinds.ALL)
        win.want("and empties the page it was filtering",
                 searchAll.model.count, 0)
        backend.forget()
        win.findChild(searchAll, "kindFilterArea-book").clicked(null)
        win.want("a filter tapped on an empty screen moves the filter",
                 searchAll.kindFilter, Kinds.BOOK)
        win.want("**without asking anyone anything**",
                 backend.sendCount, 0)
        win.want("rather than the answer to somebody's earlier question",
                 searchAll.model.count, 0)

        win.app.showScreen("downloaded")

        // ---- reconciling with the tablet -----------------------------------------
        //
        // **The failure answer is explicit.** An empty `missing` from a
        // frontend that never looked would be read as "none of them exist",
        // and the backend acts on that by deleting every record.

        backend.forget()
        win.deliver(Msg.CheckDocuments, {"documentUuids": ["doc-1", "doc-2"]})
        win.want("the check is answered", backend.countOf(Msg.DocumentsChecked), 1)
        win.want("**saying it could not look**",
                 backend.bodyOf(Msg.DocumentsChecked).checked, false)
        win.want("rather than that nothing is missing",
                 backend.bodyOf(Msg.DocumentsChecked).missing.length, 0)
        win.want("and saying why", backend.bodyOf(Msg.DocumentsChecked).detail,
                 Answers.NO_BRIDGE)

        backend.forget()
        win.deliver(Msg.SortDocuments, {"documentUuids": ["doc-1"],
                                        "sourceId": "src-a",
                                        "seriesId": "/manga/lantern/",
                                        "folderName": "The Lantern Keeper"})
        win.want("a filing pass is always answered",
                 backend.countOf(Msg.DocumentsSorted), 1)
        win.want("with nothing moved", backend.bodyOf(Msg.DocumentsSorted).moved.length, 0)
        win.want("no folder created", backend.bodyOf(Msg.DocumentsSorted).created, false)
        win.want("and the pair echoed back so the backend knows what it is about",
                 backend.bodyOf(Msg.DocumentsSorted).seriesId, "/manga/lantern/")

        // ---- watched series --------------------------------------------------------

        win.app.showScreen("watching")
        backend.forget()
        win.deliver(Msg.WatchList, win.watchReply(
            [win.lanternWatch, win.orphanWatch, win.newWatch],
            {"seriesWithNew": 0, "newChapters": 0, "failed": 0,
             "short": "", "phrase": ""}))
        win.want("the watched list fills", watchList.model.count, 3)
        win.want("with the backend's status line", watchList.model.get(0).status,
                 "Up to date")
        win.want("no news is nothing to say", win.app.watchShort, "")
        win.want("so the entry point is the plain noun",
                 sourceList.watchingLabel, "")

        // The list is pushed rather than fetched, so this is the only cue that
        // a row the screen has no cover for has arrived. The lantern row
        // already has its file from the CoverReady above and is **not** asked
        // for again — a second fetch for a file on disk is work the politeness
        // limiter would put in front of a cover nobody has yet (PLAN §7.4).
        win.want("the rows that still need a cover ask for one",
                 backend.countOf(Msg.RequestCover), 1)
        win.want("and only those rows",
                 backend.bodyOf(Msg.RequestCover).covers.length, 1)
        win.want("naming that row's own source",
                 backend.bodyOf(Msg.RequestCover).covers[0].sourceId, "src-b")
        win.want("and its own series", backend.bodyOf(Msg.RequestCover).covers[0].seriesId,
                 "/series/newone")

        win.deliver(Msg.WatchList, win.watchReply(
            [win.lanternWatch, win.orphanWatch, win.newWatch],
            {"seriesWithNew": 1, "newChapters": 3, "failed": 0,
             "short": "3 new", "phrase": "1 series has new chapters"}))
        win.want("a summary is shown as it arrived", win.app.watchShort, "3 new")
        win.want("on the entry point", sourceList.watchingLabel, "3 new")
        win.want("and the phrase on the watched screen", watchList.phrase,
                 "1 series has new chapters")

        // One result arriving mid-round is written in place.
        var updated = {"sourceId": "src-a", "seriesId": "/manga/lantern/",
                       "sourceName": "Example Reader", "title": "The Lantern Keeper",
                       "newChapters": 3, "badge": "3 new chapters",
                       "state": "new", "status": "3 new chapters"}
        win.deliver(Msg.WatchUpdate, {"watch": updated})
        win.want("an update lands on its own row", watchList.model.get(0).badge,
                 "3 new chapters")
        win.want("with the backend's status", watchList.model.get(0).status,
                 "3 new chapters")
        win.want("and changes the count not at all", watchList.model.count, 3)
        win.want("nor the order", watchList.model.get(1).seriesId, "/series/orphan")

        // The flag on the other screens is re-derived from the store, never
        // toggled where the user tapped.
        win.deliver(Msg.DownloadedList, win.downloadedReply())
        win.want("a downloaded row knows it is watched",
                 downloadedList.model.get(0).watched, true)
        win.want("and so does the row for the other source",
                 downloadedList.model.get(1).watched, true)

        backend.forget()
        watchList.unwatchRequested("src-b", "/series/orphan")
        win.want("unwatching asks the backend", backend.countOf(Msg.UnwatchSeries), 1)
        win.want("naming the pair", backend.bodyOf(Msg.UnwatchSeries).seriesId,
                 "/series/orphan")
        win.want("and nothing is dropped until it answers", watchList.model.count, 3)

        win.deliver(Msg.WatchList, win.watchReply([win.lanternWatch],
                                                  {"short": "", "phrase": ""}))
        win.want("the store dropping it is what drops the row",
                 watchList.model.count, 1)
        win.want("and the downloaded row follows the store",
                 downloadedList.model.get(1).watched, false)
        win.want("while the watched one stays watched",
                 downloadedList.model.get(0).watched, true)
        win.want("an empty summary shows nothing, not zero",
                 sourceList.watchingLabel, "")

        // The series screen's button follows the store too.
        win.app.showScreen("watching")
        backend.forget()
        watchList.openRequested("src-a", "Example Reader", "/manga/lantern/",
                                "The Lantern Keeper")
        win.want("opening from the watched list opens the series",
                 win.app.screen, "series")
        win.want("and Back goes back there", win.app.seriesCameFrom, "watching")
        win.want("the series is known to be watched", chapterList.watched, true)
        win.deliver(Msg.WatchList, win.watchReply([], {"short": "", "phrase": ""}))
        win.want("and stops being when the store says so", chapterList.watched, false)

        backend.forget()
        watchList.checkRequested()
        win.want("a check round is asked for explicitly",
                 backend.countOf(Msg.CheckWatched), 1)
        watchList.downloadNewRequested("src-a", "/manga/lantern/")
        win.want("the menu's download asks the backend to pick the chapters",
                 backend.countOf(Msg.DownloadNewChapters), 1)
        watchList.markSeenRequested("src-a", "/manga/lantern/")
        win.want("and marking seen names the pair",
                 backend.bodyOf(Msg.MarkSeen).seriesId, "/manga/lantern/")

        // ---- the private Watching list (round 2) -----------------------------------
        //
        // privateWatched/privateSummary are the same shapes as watched/summary,
        // routed onto the same WatchList instance in its private mode — never
        // mixed into the ordinary watchedModel, which is what keeps the
        // ordinary "Watching · 3 new" badge from ever counting a private series.
        var privateLantern = Object.assign({}, win.lanternWatch, {"private": true})
        win.deliver(Msg.WatchList, {
            "watched": [], "summary": {"short": "", "phrase": ""},
            "privateWatched": [privateLantern],
            "privateSummary": {"short": "1 new", "phrase": "1 series has new chapters"}})
        win.want("the ordinary list stays empty", win.app.watchShort, "")
        win.app.showScreen("watchingPrivate")
        win.want("the private Watching screen shows the private list's own rows",
                 watchList.model.count, 1)
        win.want("titled for the private mode", win.app.screenTitle(), "Private · Watching")
        win.app.goBack()
        win.want("back goes to the private source list", win.app.screen, "privateSources")

        // ---- what a queue could not take -------------------------------------------

        win.deliver(Msg.QueueResult, {"message": "Two chapters did not fit."})
        win.want("a queue that could not take everything says so",
                 win.app.lastError, "Two chapters did not fit.")
        settings.clearErrorRequested()
        win.deliver(Msg.QueueResult, {})
        win.want("and a queue that took the lot says nothing", win.app.lastError, "")

        // ---- the cache ---------------------------------------------------------------

        win.deliver(Msg.CacheConfirm, {"message": "Clear 3.2 MB of cached pages?"})
        win.want("the cache question is the backend's",
                 settings.cacheQuestion, "Clear 3.2 MB of cached pages?")
        win.deliver(Msg.CacheStatus, {"message": "3.2 MB of pages cached."})
        win.want("the cache line is the backend's too",
                 settings.cacheSummary, "3.2 MB of pages cached.")
        win.want("and an answer puts the question away", settings.cacheQuestion, "")

        backend.forget()
        settings.cacheSizeRequested()
        settings.clearCacheRequested()
        win.want("asking the size asks once", backend.countOf(Msg.GetCacheSize), 1)
        win.want("and asking to clear asks for the question first",
                 backend.bodyOf(Msg.ClearCache).confirmed, undefined)
        settings.clearCacheConfirmed()
        win.want("only the second step confirms",
                 backend.bodyOf(Msg.ClearCache).confirmed, true)

        // ---- the settings screen's own messages ---------------------------------------

        backend.forget()
        settings.pingRequested()
        win.want("Ping is a ping", backend.rawOf(Msg.Ping), "{}")
        settings.logRequested()
        win.want("and the log rides on one", backend.bodyOf(Msg.Ping).log, true)
        backend.forget()
        settings.consultRobotsRequested(true)
        win.want("the robots switch sets and then asks",
                 backend.types(), Msg.SetConsultRobots + "," + Msg.Ping)
        win.want("for the value tapped",
                 backend.bodyOf(Msg.SetConsultRobots).consultRobots, true)
        win.want("and does not flip the switch on its own",
                 settings.consultRobots, false)

        // ---- an error ------------------------------------------------------------------

        win.app.showScreen("series")
        win.deliver(Msg.DeleteConfirm, {"documentUuid": "doc-1", "message": "Delete it?"})
        seriesGrid.busy = true
        seriesGrid.pendingPage = 4
        searchAll.busy = true
        searchAll.pendingPage = 4
        chapterList.busy = true
        win.deliver(Msg.Error, {"message": "The source did not answer."})
        win.want("an error is the app's last error",
                 win.app.lastError, "The source did not answer.")
        win.want("shown on the settings screen", settings.lastError,
                 "The source did not answer.")
        win.want("a delete question whose answer failed is put away",
                 chapterList.confirmingId, "")
        win.want("and the strip goes back to the ordinary kind",
                 chapterList.confirmingKind, "download")
        win.want("the grid stops waiting", seriesGrid.busy, false)
        win.want("and stops naming a page that never arrived",
                 seriesGrid.pendingPage, 0)
        win.want("the combined search too", searchAll.busy, false)
        win.want("and its pager as well", searchAll.pendingPage, 0)
        win.want("and the series screen", chapterList.busy, false)

        backend.message(Msg.Error, "", false)
        win.want("an error with nothing in it still says something",
                 win.app.lastError, "Something went wrong.")
        settings.clearErrorRequested()

        // A send that never reached the backend is not silent either: under
        // Annex the service can be stopped while the app is perfectly happy.
        backend.failed(Msg.Search, "The backend is not responding.")
        win.want("a send that did not arrive is reported",
                 win.app.lastError, "The backend is not responding.")
        settings.clearErrorRequested()

        // ---- a message nothing handles ---------------------------------------------------

        backend.forget()
        win.deliver(999, {"whatever": true})
        win.want("a message nothing handles is ignored, not fatal",
                 backend.sendCount, 0)
        win.want("and the app is still on its screen", win.app.screen, "series")

        // ---- escapeRequested: Annex's own escape hatch -----------------------------------
        //
        // Annex's host offers xochitl's swipe-down-from-top-left to the app
        // before treating it as "close Quire" (see ui/Main.qml's own comment
        // on escapeRequested). Two things have to be true of it:
        //
        //   (a) an ordinary screen does not consume it — an implementation
        //       that always answered true would still pass every other check
        //       in this file, and only fails here;
        //   (b) leaving the reader goes through the same teardown Back does
        //       — an implementation that switched the screen without calling
        //       endTry would still answer true and move the screen, and only
        //       the EndTry assertion below would catch it.

        win.want("on an ordinary screen the gesture is not consumed",
                 win.app.escapeRequested(), false)
        win.want("and the screen does not move", win.app.screen, "series")

        win.app.currentSourceId = "src-a"
        win.app.currentSeriesId = "/manga/lantern/"
        backend.forget()
        win.app.openTry("c2", "Chapter 2")
        win.want("opening Try switches to the reader", win.app.screen, "try")
        win.want("which asks the backend to start the session",
                 backend.countOf(Msg.TryChapter), 1)
        win.want("Try is never already in the library (see ChapterList's tryButton)",
                 win.findChild(win.app, "tryReader").chapterInLibrary, false)
        win.want("privacy still follows the series screen's own flag",
                 win.findChild(win.app, "tryReader").sourcePrivate, chapterList.isPrivate)

        backend.forget()
        win.want("the reader consumes the gesture", win.app.escapeRequested(), true)
        win.want("leaving the way Back and the reader's own Close both do",
                 win.app.screen, "series")
        win.want("which ends the Try session, sweeping its page cache",
                 backend.countOf(Msg.EndTry), 1)
        win.want("naming the chapter that was open",
                 backend.bodyOf(Msg.EndTry).chapterId, "c2")

        win.want("back on an ordinary screen, the gesture is not consumed again",
                 win.app.escapeRequested(), false)

        // ---- saved in Quire: reading a chapter kept in Quire's own storage -----------
        //
        // Unlike Try, the screen does not switch on the ask (openSaved) — only
        // on the answer (SavedOpened) — since a chapter that turns out not to
        // be saved answers MessageError instead, and switching first would
        // leave the reader open on nothing.
        var reader = win.findChild(win.app, "tryReader")
        win.app.currentSourceId = "src-a"
        win.app.currentSeriesId = "/manga/lantern/"
        win.app.openSaved("src-a", "/manga/lantern/", "c9")
        win.want("asking to read a saved chapter does not yet switch screens",
                 win.app.screen, "series")

        win.deliver(Msg.SavedOpened, {
            "sourceId": "src-a", "seriesId": "/manga/lantern/", "chapterId": "c9",
            "seriesTitle": "The Lantern Keeper", "chapterTitle": "Chapter 9",
            "pages": ["/tmp/s0.jpg", "/tmp/s1.jpg"], "position": 1})
        win.want("the answer is what switches to the reader", win.app.screen, "saved")
        win.want("opened in saved mode", reader.mode, "saved")
        win.want("at the stored position", reader.index, 1)
        win.want("not known to be private (the current series isn't)",
                 reader.sourcePrivate, false)
        win.want("not known to be in the library either", reader.chapterInLibrary, false)

        // Both facts come off the payload itself (backend/service/saved.go),
        // not off whatever ChapterList screen happens to be open — this is
        // what makes them right for a chapter opened from the Downloaded
        // overview, which never had one.
        win.deliver(Msg.SavedOpened, {
            "sourceId": "src-a", "seriesId": "/manga/lantern/", "chapterId": "c9",
            "chapterTitle": "Chapter 9", "pages": ["/tmp/s0.jpg"], "position": 0,
            "private": true, "inLibrary": true})
        win.want("private carried on the payload, not the series screen",
                 reader.sourcePrivate, true)
        win.want("and inLibrary too", reader.chapterInLibrary, true)

        // The overlay's two library actions, both an ordinary EnqueueDownload
        // naming the chapter and a destination.
        backend.forget()
        reader.sendToLibraryRequested()
        win.want("Send to library asks for the library",
                 backend.bodyOf(Msg.EnqueueDownload).destination, "library")
        win.want("naming the chapter",
                 backend.bodyOf(Msg.EnqueueDownload).volumeId, "c9")
        backend.forget()
        reader.saveInQuireRequested()
        win.want("Save in Quire asks for Quire's own storage",
                 backend.bodyOf(Msg.EnqueueDownload).destination, "quire")

        // Reopen it, turn a page and leave through the escape gesture: the
        // position at that page is what should reach the backend, flushed
        // rather than left waiting out its debounce (ui/TryReader.qml's
        // scheduleSavePosition/flushSavePosition).
        win.deliver(Msg.SavedOpened, {
            "sourceId": "src-a", "seriesId": "/manga/lantern/", "chapterId": "c9",
            "chapterTitle": "Chapter 9", "pages": ["/tmp/s0.jpg", "/tmp/s1.jpg"], "position": 0})
        reader.index = 1
        backend.forget()
        win.want("the reader consumes the escape gesture here too",
                 win.app.escapeRequested(), true)
        win.want("leaving a saved chapter goes back to its series",
                 win.app.screen, "series")
        win.want("leaving flushes the page it was on",
                 backend.countOf(Msg.SavePosition), 1)
        win.want("naming that page",
                 backend.bodyOf(Msg.SavePosition).position, 1)
        win.want("and the chapter it belongs to",
                 backend.bodyOf(Msg.SavePosition).chapterId, "c9")

        // "Read latest" from the Downloaded overview can open a saved
        // chapter without ever visiting its series screen — leaving has to
        // go back there instead of to a "series" screen this route never
        // opened (ui/Main.qml's savedReaderCameFrom).
        win.app.screen = "downloaded"
        win.app.openSaved("src-z", "/manga/never-visited/", "c1", "downloaded")
        win.deliver(Msg.SavedOpened, {
            "sourceId": "src-z", "seriesId": "/manga/never-visited/", "chapterId": "c1",
            "chapterTitle": "Chapter 1", "pages": ["/tmp/z0.jpg"], "position": 0})
        win.want("opened from Downloaded", win.app.screen, "saved")
        win.app.goBack()
        win.want("Back returns to Downloaded, not a series screen never opened",
                 win.app.screen, "downloaded")

        // ---- books: BookOpened answering either TryChapter or OpenSaved ---------------
        //
        // No branching on kind at either call site (root.openTry/openSaved):
        // the same TryChapter and OpenSaved a manga row sends are sent here
        // too, and it is the *answer* — BookOpened rather than TryReady or
        // SavedOpened — that moves the screen to "book".

        backend.forget()
        win.app.currentSourceId = "src-s"
        win.app.currentSeriesId = "/book/dune-messiah"
        // Try started on a book row (ChapterList's own isBook, the same way
        // it already knows kind for the release strip): the reader's
        // placeholder must show the backend's BookStatus sentence, never
        // Try's own "Fetching page N…" wording, while it waits for
        // BookOpened.
        chapterList.kind = "book"
        win.app.openTry("r0", "EPUB · 0.4MB · Direct Download · fiction")
        win.want("opening Try switches to the reader immediately, book or not",
                 win.app.screen, "try")
        win.want("sending the same TryChapter a manga row would",
                 backend.countOf(Msg.TryChapter), 1)
        win.want("a book row starts the placeholder empty, not Try's own wording",
                 win.findChild(reader, "tryLoadingLabel").visible, false)

        win.deliver(Msg.BookStatus, {
            "sourceId": "src-s", "seriesId": "/book/dune-messiah", "chapterId": "r0",
            "message": "Fetching the book…"})
        win.want("BookStatus reaches the reader while it waits",
                 reader.note, "Fetching the book…")
        win.want("and shows on the placeholder, not Try's page-fetching wording",
                 win.findChild(reader, "tryLoadingLabel").text, "Fetching the book…")
        win.want("visibly, while the page is not yet there",
                 win.findChild(reader, "tryLoadingLabel").visible, true)

        chapterList.kind = "manga"
        backend.forget()
        win.deliver(Msg.BookOpened, {
            "sourceId": "src-s", "seriesId": "/book/dune-messiah", "chapterId": "r0",
            "title": "Dune Messiah", "mode": "try", "fixedLayout": false,
            "pageCount": 40, "page": 0, "toc": [],
            "settings": {"font": "book", "size": 4, "margins": "normal",
                         "spacing": "book", "align": "book"},
            "settingsNote": "These apply to every book you read in Quire.",
            "settingsFields": [{"key": "font", "label": "Font", "help": "The typeface.",
                                 "choices": [{"id": "book", "label": "The book's own"}]}],
            "private": false, "inLibrary": false})
        win.want("BookOpened moves the screen to book, whichever question it answered",
                 win.app.screen, "book")
        win.want("and the reader knows which kind of session it is",
                 reader.bookMode, "try")
        win.want("asking for the page on screen, the same way Try's own page 0 is",
                 backend.countOf(Msg.BookPageRequest), 1)
        win.want("naming the book", backend.bodyOf(Msg.BookPageRequest).chapterId, "r0")

        win.deliver(Msg.BookPage, {
            "sourceId": "src-s", "seriesId": "/book/dune-messiah", "chapterId": "r0",
            "index": 0, "path": "/tmp/book0.png"})
        win.want("BookPage reuses Try's own page-arrived handling",
                 reader.pagePaths[0], "/tmp/book0.png")

        // A settings change: SetReaderSettings names the book and the page
        // on screen (for anchoring), and BookRelaid applies the answer.
        backend.forget()
        reader.settingsChangeRequested({"font": "garamond", "size": 4,
                                        "margins": "normal", "spacing": "book", "align": "book"})
        win.want("a settings change asks once", backend.countOf(Msg.SetReaderSettings), 1)
        win.want("naming the book", backend.bodyOf(Msg.SetReaderSettings).chapterId, "r0")
        win.want("and the page on screen", backend.bodyOf(Msg.SetReaderSettings).page, 0)
        win.want("carrying the settings chosen",
                 backend.bodyOf(Msg.SetReaderSettings).settings.font, "garamond")

        win.deliver(Msg.BookRelaid, {
            "sourceId": "src-s", "seriesId": "/book/dune-messiah", "chapterId": "r0",
            "pageCount": 42, "page": 0, "toc": [],
            "settings": {"font": "garamond", "size": 4, "margins": "normal",
                         "spacing": "book", "align": "book"}})
        win.want("BookRelaid updates the reader", reader.pageCount, 42)

        // Leaving sends CloseBook, naming the page on screen — one message
        // does everything a book's close needs (saves position in saved
        // mode, ends the session, sweeps the render cache), never EndTry or
        // SavePosition/flushSavePosition.
        backend.forget()
        win.want("the reader consumes the escape gesture for a book too",
                 win.app.escapeRequested(), true)
        win.want("leaving the way Back and the overlay's own Close both do",
                 win.app.screen, "series")
        win.want("sending CloseBook rather than EndTry",
                 backend.countOf(Msg.CloseBook), 1)
        win.want("and never EndTry for a book", backend.countOf(Msg.EndTry), 0)
        win.want("naming the book that was open",
                 backend.bodyOf(Msg.CloseBook).chapterId, "r0")

        // OpenSaved on a book answers BookOpened instead of SavedOpened —
        // the screen does not switch until that answer, same as a saved
        // chapter's.
        win.app.currentSourceId = "src-s"
        win.app.currentSeriesId = "/book/dune-messiah"
        win.app.openSaved("src-s", "/book/dune-messiah", "r0")
        win.want("asking to read a saved book does not yet switch screens",
                 win.app.screen, "series")
        win.deliver(Msg.BookOpened, {
            "sourceId": "src-s", "seriesId": "/book/dune-messiah", "chapterId": "r0",
            "title": "Dune Messiah", "mode": "saved", "fixedLayout": true,
            "pageCount": 300, "page": 12, "toc": [],
            "settings": {"font": "book", "size": 4, "margins": "normal",
                         "spacing": "book", "align": "book"},
            "settingsNote": "", "settingsFields": [],
            "private": false, "inLibrary": false})
        win.want("OpenSaved on a book answers BookOpened, not SavedOpened",
                 win.app.screen, "book")
        win.want("opened at the stored page", reader.index, 12)
        win.want("a fixed-layout book carries the flag through",
                 reader.fixedLayout, true)

        backend.forget()
        win.want("leaving a saved book consumes the gesture too",
                 win.app.escapeRequested(), true)
        win.want("going back to its series, the same as a saved chapter",
                 win.app.screen, "series")
        win.want("CloseBook names the page it was left on",
                 backend.bodyOf(Msg.CloseBook).page, 12)

        // ---- leaving --------------------------------------------------------------------
        //
        // **It detaches; it does not terminate.** The backend is a service that
        // outlives the screen, so unloading() stops the transport and nothing
        // else. Last, because it is the end of the app's life.

        win.want("the transport is still up", backend.status, "connected")
        win.app.unloading()
        win.want("unloading stops the transport once", backend.stops, 1)
        win.want("which goes idle rather than dying", backend.status, "idle")
    }

    function finish() {
        console.log(win.failures === 0 ? "MAIN HARNESS OK"
                                       : "MAIN HARNESS FAILED: " + win.failures)
        Qt.exit(win.failures === 0 ? 0 : 1)
    }
}
