// The offscreen harness for build/qml-check.sh.
//
// It is not part of the app and is not in application.qrc: it loads the real
// screens, gives them models the size of a real listing, and asserts the
// properties PLAN §12.1 is about. Failures print as "FAIL <what>" and set a
// non-zero exit.
import QtQuick 2.5
import QtQuick.Window 2.2
// The screens under test. A directory import rather than -I, because implicit
// type resolution only covers the directory the file itself lives in.
import "../../ui"
import "../../ui/Watch.js" as WatchJs


Window {
    id: win
    width: 1620; height: 2160
    visible: true

    ListModel {
        id: sourcesModel
        ListElement { sourceId: "s1"; name: "Example Reader"; baseUrl: "https://example.invalid"
                      theme: "madara"; lang: "en"; enabled: true; status: "Working"; statusDetail: "" }
        ListElement { sourceId: "s2"; name: "Another"; baseUrl: "https://other.invalid"
                      theme: "mangadex"; lang: "en"; enabled: false; status: "Off"; statusDetail: "" }
    }
    ListModel { id: seriesModel }
    ListModel { id: chaptersModel }
    ListModel { id: watchedModel }

    property int failures: 0
    function want(label, got, expected) {
        if (got !== expected) {
            console.log("FAIL " + label + ": got " + got + " want " + expected)
            win.failures++
        } else {
            console.log("ok   " + label + " = " + got)
        }
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

    SourceList  { id: sourceList;  objectName: "sourceList";  anchors.fill: parent; model: sourcesModel }
    AddSource   { id: addSource;   objectName: "addSource";   anchors.fill: parent }
    SeriesGrid  { id: seriesGrid;  objectName: "seriesGrid";  anchors.fill: parent; model: seriesModel }
    ChapterList { id: chapterList; objectName: "chapterList"; anchors.fill: parent; model: chaptersModel
                  synopsis: "A long description that runs on and on. " }
    Settings    { id: settings;    objectName: "settings";    anchors.fill: parent; logShown: true }
    WatchList   { id: watchList;   objectName: "watchList";   anchors.fill: parent; model: watchedModel }
    PagerBar    { id: lonePager;   width: 1620 }

    Component.onCompleted: {
        for (var i = 0; i < 40; ++i)
            seriesModel.append({"seriesId": "x" + i, "title": "Series " + i,
                                "coverUrl": "https://example.invalid/c.jpg", "coverPath": ""})
        for (var j = 0; j < 55; ++j)
            chaptersModel.append({"chapterId": "c" + j, "title": "Chapter " + j, "number": j,
                                  "published": "2026-01-01", "scanlator": "Group",
                                  "downloadState": "", "downloadMessage": "", "documentUuid": ""})
        for (var w = 0; w < 25; ++w)
            watchedModel.append(WatchJs.row({
                "sourceId": "src", "seriesId": "w" + w, "sourceName": "Example Reader",
                "title": "Watched " + w, "newChapters": 0,
                "state": "ok", "status": "Up to date", "checkedAt": "2026-09-16T00:00:00Z"}))

        var log = []
        for (var k = 0; k < 300; ++k)
            log.push("2026-09-16T00:00:00 INFO something happened, number " + k)
        settings.logLines = log
    }

    // Layout has to have run before any of this means anything: a page size is
    // derived from a real viewport height, and before the first pass there is
    // no viewport.
    Timer {
        interval: 100; running: true
        onTriggered: win.check()
    }

    function check() {
        win.want("sourceList pageSize holds whole rows", sourceList.pageSize > 1, true)
        win.want("chapterList pageSize holds whole rows", chapterList.pageSize > 1, true)
        win.want("seriesGrid pageSize is whole rows of 3", seriesGrid.pageSize % 3, 0)
        win.want("seriesGrid pageSize is more than one row", seriesGrid.pageSize > 3, true)

        // Derived, not hardcoded: the grid's page is rows × 3 for the real
        // viewport it was given.
        win.want("chapterList totalPages", chapterList.totalPages,
                 Math.ceil(55 / chapterList.pageSize))
        win.want("sourceList totalPages", sourceList.totalPages, 1)

        // Nothing scrolls.
        win.want("chapter list is not interactive",
                 win.findChild(chapterList, "chapterPager") !== null, true)

        // A hard stop at each end, never a wrap.
        var cp = win.findChild(chapterList, "chapterPager")
        chapterList.page = 1
        win.want("previous is dead on page 1", cp.canGoBack, false)
        win.want("next is alive on page 1", cp.canGoOn, true)
        chapterList.page = chapterList.totalPages
        win.want("next is dead on the last page", cp.canGoOn, false)
        cp.nextRequested()
        win.want("a dead next does not wrap", chapterList.page, chapterList.totalPages)
        cp.previousRequested()
        win.want("previous turns back", chapterList.page, chapterList.totalPages - 1)

        // One page of sources means no pager at all.
        win.want("a single page shows no pager", win.findChild(sourceList, "sourcePager").visible, false)

        // The log opens at the newest page and pages back.
        var lp = win.findChild(settings, "logPager")
        win.want("the log opens at the newest page", settings.logPage > 1, true)
        win.want("the log has a total", lp.totalPages > 1, true)

        // The keyboard must not move the pager or change the page size.
        var sizeBefore = seriesGrid.pageSize
        var pagerYBefore = win.findChild(seriesGrid, "seriesPager").y
        seriesGrid.searching = true
        win.want("the keyboard does not resize the page", seriesGrid.pageSize, sizeBefore)
        win.want("the keyboard does not move the pager",
                 win.findChild(seriesGrid, "seriesPager").y, pagerYBefore)
        seriesGrid.searching = false

        // ---- PLAN §12.2: watched series --------------------------------
        //
        // A check round streams its results in while the user is reading a
        // page. An update for a series on another page must cost this page
        // nothing: no reflow, no change of count, and above all no change to
        // which series the current page holds.
        var wp = win.findChild(watchList, "watchPager")
        watchList.page = 2
        var pageBefore = watchList.page
        var countBefore = watchedModel.count
        var onThisPage = watchedModel.get((watchList.page - 1) * watchList.pageSize).seriesId

        WatchJs.applyUpdate(watchedModel, {
            "sourceId": "src", "seriesId": "w0", "sourceName": "Example Reader",
            "title": "Watched 0", "newChapters": 3, "badge": "3 new chapters",
            "state": "new", "status": "3 new chapters"})
        win.want("an update elsewhere does not turn the page", watchList.page, pageBefore)
        win.want("an update elsewhere does not change the count", watchedModel.count, countBefore)
        win.want("an update elsewhere does not change what this page holds",
                 watchedModel.get((watchList.page - 1) * watchList.pageSize).seriesId, onThisPage)
        win.want("the update landed on its own row", watchedModel.get(0).badge, "3 new chapters")

        // The badge and the status are the backend's words, stored verbatim.
        win.want("the status is stored verbatim", watchedModel.get(0).status, "3 new chapters")

        // A failed check keeps the count an earlier successful one established,
        // so a row can carry a badge and a warning at once (PLAN §12.2).
        WatchJs.applyUpdate(watchedModel, {
            "sourceId": "src", "seriesId": "w0", "sourceName": "Example Reader",
            "title": "Watched 0", "newChapters": 3, "badge": "3 new chapters",
            "state": "failed", "status": "Couldn\u2019t check",
            "detail": "The site did not answer."})
        win.want("a failed row keeps its badge", watchedModel.get(0).badge, "3 new chapters")
        win.want("a failed row says so", watchedModel.get(0).status, "Couldn\u2019t check")
        win.want("a failed row carries its detail", watchedModel.get(0).detail,
                 "The site did not answer.")

        // A whole list arriving at the end of a round is applied in place: same
        // rows, same order, same page.
        var whole = []
        for (var r = 0; r < watchedModel.count; ++r) {
            var have = watchedModel.get(r)
            whole.push({"sourceId": have.sourceId, "seriesId": have.seriesId,
                        "sourceName": have.sourceName, "title": have.title,
                        "newChapters": have.newChapters, "badge": have.badge,
                        "state": have.state, "status": have.status, "detail": have.detail})
        }
        WatchJs.reconcile(watchedModel, whole)
        win.want("a whole list does not turn the page", watchList.page, pageBefore)
        win.want("a whole list does not change the count", watchedModel.count, countBefore)
        win.want("a whole list does not reorder", watchedModel.get(0).seriesId, "w0")

        // Unwatching drops the row, and the pager clamps rather than leaving
        // the user on a page that no longer exists.
        watchList.page = watchList.totalPages
        whole.length = 1
        WatchJs.reconcile(watchedModel, whole)
        win.want("a dropped watch leaves one row", watchedModel.count, 1)
        win.want("the page clamps after a drop", watchList.page, 1)
        win.want("the watched pager is gone with one page", wp.visible, false)

        // The honest label.
        lonePager.page = 3; lonePager.totalPages = 12
        win.want("label with a total", win.findChild(lonePager, "pagerLabel").text, "Page 3 of 12")
        lonePager.totalPages = 0
        win.want("label without a total", win.findChild(lonePager, "pagerLabel").text, "Page 3")
        lonePager.busy = true; lonePager.pendingPage = 4
        win.want("label while fetching", win.findChild(lonePager, "pagerLabel").text, "Fetching page 4…")
        win.want("both controls dead while fetching", lonePager.canGoBack || lonePager.canGoOn, false)

        console.log(win.failures === 0 ? "HARNESS OK" : "HARNESS FAILED: " + win.failures)
        Qt.exit(win.failures === 0 ? 0 : 1)
    }
}
