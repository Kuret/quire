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
import "../../ui/Sorting.js" as Sorting
import "../../ui/Reconcile.js" as Reconcile
import "../../ui/Deleting.js" as Deleting
import "../../ui/Screens.js" as Screens
import "../../ui/Answers.js" as Answers
import "../../ui/Style.js" as Style


Window {
    id: win
    width: 1620; height: 2160
    visible: true

    ListModel {
        id: sourcesModel
        ListElement { sourceId: "s1"; name: "Example Reader"; baseUrl: "https://example.invalid"
                      theme: "madara"; lang: "en"; enabled: true; status: "Working"; statusDetail: ""
                      splitStrips: "never" }
        // No splitStrips at all: a source stored before PLAN §12.3 existed. It
        // has to read as Automatic rather than blank.
        ListElement { sourceId: "s2"; name: "Another"; baseUrl: "https://other.invalid"
                      theme: "mangadex"; lang: "en"; enabled: false; status: "Off"; statusDetail: "" }
    }
    ListModel { id: seriesModel }
    ListModel { id: chaptersModel }
    ListModel { id: volumesModel }
    ListModel { id: emptyVolumesModel }
    ListModel { id: watchedModel }
    ListModel { id: downloadedModel }

    // Stands in for Main.qml's root, which the harness cannot load (it imports
    // the AppLoad plugin). The counters make "did this repaint?" observable:
    // assigning the same string to a QML property emits no change signal, so a
    // count that does not move is a label that did not redraw.
    QtObject {
        id: summaryTarget
        property string watchShort: ""
        property string watchPhrase: ""
        property int shortWrites: 0
        property int phraseWrites: 0
        onWatchShortChanged: summaryTarget.shortWrites++
        onWatchPhraseChanged: summaryTarget.phraseWrites++
    }

    property var typed: []

    property int splitAsks: 0
    property string splitAskedFor: ""
    property string splitAskedAbout: ""

    property int volumeAsks: 0
    property string volumeAskedFor: ""

    property int robotsWrites: 0
    property int robotsAsks: 0
    property bool robotsAskedFor: false

    // What the delete affordance asked for (PLAN §12.4). Counters, because the
    // point of the confirmation step is that the first tap asks and does not
    // delete — which is only observable as a count that did not move.
    property int deleteAsks: 0
    property string deleteAskedAbout: ""
    property int deleteConfirms: 0
    property string deleteConfirmedAbout: ""

    // What a selection queued (PLAN §12.1). Counters, because the properties
    // worth holding are all about how *many* times something happened: picking
    // rows queues nothing, and the footer queues once.
    property int queueConfirms: 0
    property var queueSent: []
    property bool queueSentVolumes: false

    // The download cache control (PLAN §12.4). Counters for the same reason as
    // everywhere else here: the property under test is that the first tap asks
    // and clears *nothing*, which is a count that did not move.
    property int cacheSizeAsks: 0
    property int cacheClearAsks: 0
    property int cacheClears: 0

    // What a downloaded row asked to delete: the question, and the answer.
    property int downloadedDeleteAsks: 0
    property string downloadedAskedSeries: ""
    property int downloadedDeletes: 0
    property string downloadedDeletedSeries: ""

    // What a downloaded row asked to open.
    property int downloadedOpens: 0
    property string downloadedOpenedSource: ""
    property string downloadedOpenedSeries: ""

    property int failures: 0
    function want(label, got, expected) {
        if (got !== expected) {
            console.log("FAIL " + label + ": got " + got + " want " + expected)
            win.failures++
        } else {
            console.log("ok   " + label + " = " + got)
        }
    }

    // findChildren collects every descendant with the objectName, which is how
    // the keyboard's keys are counted.
    function findChildren(item, name, into) {
        if (!item)
            return into
        if (item.objectName === name)
            into.push(item)
        for (var i = 0; i < item.children.length; ++i)
            win.findChildren(item.children[i], name, into)
        return into
    }

    // visibleKeyLabels returns the text of every key that is actually on
    // screen, which is what "how many full stops does this layout have?" means.
    function visibleKeyLabels(kb) {
        var out = []
        function walk(item) {
            if (!item || item.visible === false)
                return
            if (item.text !== undefined && String(item.text).length > 0)
                out.push(String(item.text))
            for (var i = 0; i < item.children.length; ++i)
                walk(item.children[i])
        }
        walk(kb)
        return out
    }

    function countLabel(kb, want) {
        var labels = win.visibleKeyLabels(kb)
        var n = 0
        for (var i = 0; i < labels.length; ++i)
            if (labels[i] === want)
                n++
        return n
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

    SourceList {
        id: sourceList
        objectName: "sourceList"
        anchors.fill: parent
        model: sourcesModel
        onSplitStripsRequested: {
            win.splitAsks++
            win.splitAskedFor = mode
            win.splitAskedAbout = sourceId
        }
    }
    AddSource   { id: addSource;   objectName: "addSource";   anchors.fill: parent }
    SeriesGrid  { id: seriesGrid;  objectName: "seriesGrid";  anchors.fill: parent; model: seriesModel }
    ChapterList { id: chapterList; objectName: "chapterList"; anchors.fill: parent; model: chaptersModel
                  volumeModel: volumesModel
                  onVolumeDownloadRequested: {
                      win.volumeAsks++
                      win.volumeAskedFor = chapterId
                  }
                  onQueueConfirmed: {
                      win.queueConfirms++
                      win.queueSent = chapterIds
                      win.queueSentVolumes = volumes
                  }
                  onDeleteRequested: {
                      win.deleteAsks++
                      win.deleteAskedAbout = documentUuid
                  }
                  onDeleteConfirmed: {
                      win.deleteConfirms++
                      win.deleteConfirmedAbout = documentUuid
                  }
                  synopsis: "A long description that runs on and on. " }

    // A second chapter screen with no volumes at all, which is what a source
    // that publishes no labels looks like. The affordance has to be absent
    // there, not empty: PLAN §6 M4, revised 2026-09-16.
    ChapterList { id: plainChapterList; objectName: "plainChapterList"; anchors.fill: parent
                  model: chaptersModel; volumeModel: emptyVolumesModel }
    Settings {
        id: settings
        objectName: "settings"
        anchors.fill: parent
        logShown: true
        // Counters, so "did this repaint?" is observable: a QML property
        // emits no change signal when assigned the value it already holds,
        // so a count that does not move is a toggle that did not redraw.
        onConsultRobotsChanged: win.robotsWrites++
        onConsultRobotsRequested: {
            win.robotsAsks++
            win.robotsAskedFor = on
        }
        onCacheSizeRequested: win.cacheSizeAsks++
        onClearCacheRequested: win.cacheClearAsks++
        onClearCacheConfirmed: win.cacheClears++
    }
    WatchList   { id: watchList;   objectName: "watchList";   anchors.fill: parent; model: watchedModel }

    // The downloaded overview (PLAN §12.5). Its own model, filled per case, so
    // no case inherits the rows of the one before it.
    DownloadedList {
        id: downloadedList
        objectName: "downloadedList"
        anchors.fill: parent
        model: downloadedModel
        onOpenRequested: {
            win.downloadedOpens++
            win.downloadedOpenedSource = sourceId
            win.downloadedOpenedSeries = seriesId
        }
        onDeleteRequested: {
            win.downloadedDeleteAsks++
            win.downloadedAskedSeries = seriesId
        }
        onDeleteConfirmed: {
            win.downloadedDeletes++
            win.downloadedDeletedSeries = seriesId
        }
    }
    PagerBar    { id: lonePager;   width: 1620 }

    // Both keyboard layouts, so the URL one can be inspected without driving a
    // screen into its search state.
    Keyboard {
        id: textKeys
        objectName: "textKeys"
        width: 1620
        layout: "text"
        onKeyTyped: { win.typed.push(text); win.typedChanged() }
    }
    Keyboard {
        id: urlKeys
        objectName: "urlKeys"
        width: 1620
        layout: "url"
        onKeyTyped: { win.typed.push(text); win.typedChanged() }
    }

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

        for (var v = 0; v < 3; ++v)
            volumesModel.append({"chapterId": "c" + (v * 7), "title": "Volume " + (v + 1),
                                 "detail": "7 chapters, Chapter 1 to Chapter 7",
                                 "chapterCount": 7,
                                 "downloadState": "", "downloadMessage": "", "documentUuid": ""})

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

        // ---- PLAN §12.2: the at-a-glance summary -----------------------
        //
        // WatchList arrives on attach, after every watch and unwatch, and
        // again at the end of every check round. A round that ends with the
        // same summary it started with must produce no visible change at all:
        // a flash on the source-list screen for no news is worse than no
        // indicator.
        var withNews = {"summary": {"seriesWithNew": 3, "newChapters": 11, "failed": 1,
                                    "short": "3 new",
                                    "phrase": "3 series have new chapters"}}

        win.want("a summary is applied", WatchJs.applySummary(summaryTarget, withNews), true)
        win.want("short is stored verbatim", summaryTarget.watchShort, "3 new")
        win.want("phrase is stored verbatim", summaryTarget.watchPhrase,
                 "3 series have new chapters")
        var writesAfterFirst = summaryTarget.shortWrites + summaryTarget.phraseWrites

        win.want("an unchanged summary writes nothing",
                 WatchJs.applySummary(summaryTarget, withNews), false)
        win.want("an unchanged summary repaints nothing",
                 summaryTarget.shortWrites + summaryTarget.phraseWrites, writesAfterFirst)

        // Empty means show nothing — not "0 new", not a placeholder.
        win.want("an absent summary clears it",
                 WatchJs.applySummary(summaryTarget, {"watched": []}), true)
        win.want("short goes empty, not zero", summaryTarget.watchShort, "")
        win.want("phrase goes empty, not zero", summaryTarget.watchPhrase, "")
        win.want("an unchanged empty summary writes nothing",
                 WatchJs.applySummary(summaryTarget, {"watched": []}), false)

        // And back again, which is what the end of a fruitful round looks like.
        win.want("empty to non-empty writes",
                 WatchJs.applySummary(summaryTarget, withNews), true)
        win.want("short is back", summaryTarget.watchShort, "3 new")

        // The entry point: the view's own noun, then the backend's words.
        var wl = win.findChild(sourceList, "watchingLabel")
        sourceList.watchingLabel = ""
        win.want("the plain entry point", wl.text, "Watching")
        sourceList.watchingLabel = "3 new"
        win.want("the entry point carries the summary", wl.text, "Watching · 3 new")
        // It has to stay legible in the button it sits in: an elided or
        // overflowing indicator is worse than none.
        win.want("the grown label still fits its button",
                 wl.implicitWidth <= wl.parent.width - Style.gap * 2, true)
        // The backend owns this string and may make it longer — a failure
        // case reads something like "3 new, 1 failed". Check the roomiest
        // plausible one rather than only the happy short one.
        sourceList.watchingLabel = "12 new, 3 failed"
        console.log("     entry point width: label " + Math.round(wl.implicitWidth) +
                    " of " + Math.round(wl.parent.width) + " available")
        win.want("a long summary still fits",
                 wl.implicitWidth <= wl.parent.width - Style.gap * 2, true)
        sourceList.watchingLabel = "3 new"

        // The watched screen: the phrase is absent, not blank, when empty.
        var wph = win.findChild(watchList, "watchPhrase")
        watchList.phrase = ""
        win.want("no phrase means no line", wph.visible, false)
        watchList.phrase = "3 series have new chapters"
        win.want("the phrase is drawn as it arrived", wph.text, "3 series have new chapters")
        win.want("the phrase line is there when there is news", wph.visible, true)

        // ---- PLAN §7.4: the global robots.txt switch -------------------
        //
        // The value is pushed on the status, which arrives on attach and on
        // every ping. Binding to a plain bool rather than to the status object
        // is what keeps an identical push from repainting the toggle.
        var robotsState = win.findChild(settings, "robotsToggleState")
        var robotsWhy = win.findChild(settings, "robotsExplanation")

        // Off by default, and the copy says what that means rather than
        // hiding behind a label — including the claim the log viewer on this
        // same screen makes checkable.
        win.want("the toggle is off by default", settings.consultRobots, false)
        win.want("off reads as Off", robotsState.text, "Off")
        win.want("off says what it does", robotsWhy.text,
                 "Off: Quire fetches pages a site\u2019s robots.txt asks crawlers not to. " +
                 "Every such fetch is logged.")

        settings.consultRobots = true
        var writesAfterOn = win.robotsWrites
        win.want("the toggle follows the pushed value", robotsState.text, "On")

        // An identical status push writes nothing and so repaints nothing.
        settings.consultRobots = true
        win.want("an identical push does not repaint the toggle",
                 win.robotsWrites, writesAfterOn)

        // Flipping asks once, for the opposite of what is stored — and does
        // not flip locally, because the store is what the switch must show.
        var asksBefore = win.robotsAsks
        settings.toggleRobots()
        win.want("flipping asks exactly once", win.robotsAsks, asksBefore + 1)
        win.want("flipping asks for the opposite", win.robotsAskedFor, false)
        win.want("flipping does not change the toggle on its own",
                 settings.consultRobots, true)

        settings.consultRobots = false
        win.want("the toggle follows the push back", robotsState.text, "Off")

        // ---- the on-screen keyboard ------------------------------------
        //
        // The device ships Noto Sans, Noto Serif, NotoSansUI and Noto Mono and
        // nothing else. U+232B ERASE TO THE LEFT is in Noto Sans *Symbols*,
        // which is not installed, so the backspace key rendered as a tofu box.
        // It is drawn now, and the assertion is that nothing in the keyboard
        // depends on a glyph at all.
        var backKey = win.findChild(urlKeys, "keyboardBackspace")
        win.want("the backspace key exists", backKey !== null, true)
        var glyph = null
        for (var g = 0; g < backKey.children.length; ++g)
            if (backKey.children[g].toString().indexOf("QQuickCanvasItem") === 0)
                glyph = backKey.children[g]
        win.want("the backspace glyph is drawn, not typed", glyph !== null, true)
        win.want("the drawn glyph has a size", glyph.width > 0 && glyph.height > 0, true)
        win.want("the backspace key carries no text",
                 win.countLabel(backKey, "") === 0 && win.visibleKeyLabels(backKey).length, 0)

        // A URL layout with two full stops had one key that did nothing the
        // other did not, and a space bar that silently was not one.
        win.want("the URL layout has exactly one full stop", win.countLabel(urlKeys, "."), 1)
        win.want("the URL layout has no space bar",
                 win.findChild(urlKeys, "keyboardSpace").visible, false)
        win.want("the text layout keeps its space bar",
                 win.findChild(textKeys, "keyboardSpace").visible, true)
        win.want("the text layout has one full stop", win.countLabel(textKeys, "."), 1)

        // The suffix keys, in the URL layout only, inserting the whole string.
        var urlSuffixes = win.findChildren(urlKeys, "keyboardSuffix", [])
        var textSuffixes = win.findChildren(textKeys, "keyboardSuffix", [])
        win.want("the URL layout has two suffix keys", urlSuffixes.length, 2)
        win.want("the text layout has none", textSuffixes.length, 0)

        win.typed = []
        urlSuffixes[0].children[1].clicked(null)
        urlSuffixes[1].children[1].clicked(null)
        win.want("the first suffix types .com", win.typed[0], ".com")
        win.want("the second suffix types .org", win.typed[1], ".org")

        // The row still fits the panel without reflowing or shrinking keys.
        win.want("the URL keyboard fits the panel", urlKeys.width >= 1620, true)
        win.want("the suffix keys stay a comfortable target",
                 urlSuffixes[0].width >= 150 && urlSuffixes[0].height >= 80, true)

        // The honest label.
        lonePager.page = 3; lonePager.totalPages = 12
        win.want("label with a total", win.findChild(lonePager, "pagerLabel").text, "Page 3 of 12")
        lonePager.totalPages = 0
        win.want("label without a total", win.findChild(lonePager, "pagerLabel").text, "Page 3")
        lonePager.busy = true; lonePager.pendingPage = 4
        win.want("label while fetching", win.findChild(lonePager, "pagerLabel").text, "Fetching page 4…")
        win.want("both controls dead while fetching", lonePager.canGoBack || lonePager.canGoOn, false)

        // ---- the notice strip ------------------------------------------
        //
        // It is one short line beside a full-size tap target, and the strip
        // sized itself from the *text*, so the button hung out of the bottom
        // and over the first source row. Geometry, so it is checked as
        // geometry: the button has to fit inside the strip that claims to
        // contain it.
        sourceList.notice = "Quire closed unexpectedly last time."
        var noticeStrip = win.findChild(sourceList, "noticeStrip")
        var dismiss = win.findChild(sourceList, "dismissNoticeButton")
        win.want("a notice shows its strip", noticeStrip.visible, true)
        win.want("and the OK button fits inside it",
                 dismiss.y + dismiss.height <= noticeStrip.height, true)
        win.want("and the strip is at least as tall as the button",
                 noticeStrip.height >= dismiss.height, true)
        sourceList.notice = ""
        win.want("no notice, no strip", noticeStrip.visible, false)
        win.want("and no height taken from the list", noticeStrip.height, 0)

        // ---- PLAN §12.3: the per-source strip-splitting override --------
        //
        // The worry this feature answers is an ordinary manga being mistaken
        // for a webtoon, so the control exists to be *found* and *understood*,
        // not merely to exist. These check the two things a user would notice:
        // that a source with nothing set reads as Automatic rather than blank,
        // and that choosing a mode sends the schema's spelling exactly once.
        var splitPanel = win.findChild(sourceList, "splitPanel")
        win.want("the splitting panel is closed until asked for", splitPanel.visible, false)

        // A source stored before the feature existed, opened from the row strip.
        sourceList.confirmingId = "s2"
        sourceList.confirmingName = "Another"
        sourceList.confirmingSplit = ""
        var splitLabel = win.findChild(sourceList, "splitButtonLabel")
        win.want("an unset source reads as automatic, not blank",
                 splitLabel.text, "Splitting: Automatic")

        sourceList.confirmingSplit = "never"
        win.want("the button says what is set", splitLabel.text, "Splitting: Never")
        sourceList.confirmingSplit = "always"
        win.want("and for always", splitLabel.text, "Splitting: Always")

        // Opening the panel closes the row strip, so two things are never open.
        sourceList.confirmingSplit = "never"
        sourceList.startSplitting("s2", "Another", sourceList.confirmingSplit)
        win.want("the panel opens", splitPanel.visible, true)
        win.want("opening it closes the row strip", sourceList.confirmingId, "")
        win.want("the chosen mode is marked",
                 win.findChild(sourceList, "splitOptionLabel-never").text, "Never (chosen)")
        win.want("the others are not",
                 win.findChild(sourceList, "splitOptionLabel-auto").text, "Automatic")

        // Re-choosing what is already chosen must send nothing: the reply is a
        // fresh source list, which on e-ink repaints the whole screen.
        win.splitAsks = 0
        sourceList.chooseSplitMode("never")
        win.want("re-choosing the current mode sends nothing", win.splitAsks, 0)

        // A real change sends the schema's own spelling, once.
        sourceList.chooseSplitMode("always")
        win.want("choosing a mode asks once", win.splitAsks, 1)
        win.want("it sends the schema spelling, not the label", win.splitAskedFor, "always")
        win.want("it names the source", win.splitAskedAbout, "s2")
        win.want("the panel follows the choice",
                 win.findChild(sourceList, "splitOptionLabel-always").text, "Always (chosen)")

        // And every mode the schema offers is reachable from the panel.
        win.want("automatic is offered", win.findChild(sourceList, "splitOption-auto") !== null, true)
        win.want("never is offered", win.findChild(sourceList, "splitOption-never") !== null, true)
        win.want("always is offered", win.findChild(sourceList, "splitOption-always") !== null, true)

        // Tapped for real, not closed by assignment: the assertion is about the
        // button, so reaching past it would be testing nothing.
        win.findChild(sourceList, "splitDoneArea").clicked(null)
        win.want("done closes the panel", splitPanel.visible, false)

        // ---- the volume view (PLAN §6 M4, revised 2026-09-16) --------------

        // Absent, not empty, for a source with no volume labels.
        win.want("no volumes means no view switch",
                 win.findChild(plainChapterList, "viewSwitch").visible, false)
        win.want("and no height taken from the list",
                 win.findChild(plainChapterList, "viewSwitch").height, 0)
        win.want("chapters are still shown",
                 win.findChild(plainChapterList, "chapterRows").visible, true)

        // Offered when there are volumes, and chapters are what it opens on.
        var vs = win.findChild(chapterList, "viewSwitch")
        win.want("volumes mean a view switch", vs.visible, true)
        win.want("the screen opens on chapters", chapterList.view, "chapters")
        win.want("the chapter rows are the ones shown",
                 win.findChild(chapterList, "chapterRows").visible, true)
        win.want("the volume rows are not", win.findChild(chapterList, "volumeRows").visible, false)

        // Switching views swaps the list and starts the paging again, because
        // three volumes and fifty-five chapters are not the same number of
        // pages.
        chapterList.page = 2
        win.findChild(chapterList, "viewButton-volumes").children[1].clicked(null)
        win.want("the volume rows are shown", win.findChild(chapterList, "volumeRows").visible, true)
        win.want("the chapter rows are not", win.findChild(chapterList, "chapterRows").visible, false)
        win.want("the page starts again", chapterList.page, 1)
        win.want("the pager counts volumes, not chapters", chapterList.totalPages, 1)

        // Tapping a volume row asks for a volume, not a chapter.
        win.volumeAsks = 0
        chapterList.volumeTapped("c7", "", "")
        win.want("a volume row asks once", win.volumeAsks, 1)
        win.want("and names its first chapter", win.volumeAskedFor, "c7")

        // Re-choosing the view already on screen repaints nothing.
        chapterList.page = 1
        win.findChild(chapterList, "viewButton-volumes").children[1].clicked(null)
        win.want("re-choosing the current view does not reset anything", chapterList.page, 1)

        // Back to chapters, and the volume view empties away cleanly.
        chapterList.showView("chapters")
        win.want("switching back shows chapters",
                 win.findChild(chapterList, "chapterRows").visible, true)
        volumesModel.clear()
        win.want("a series that loses its volumes loses the switch", vs.visible, false)
        win.want("and is left looking at chapters", chapterList.view, "chapters")

        // ---- deleting a download (PLAN §12.4) ------------------------------
        //
        // Two properties of the affordance are worth holding on to: it is only
        // on rows that have something to delete, and the first tap asks rather
        // than deletes.

        chapterList.showView("chapters")
        chapterList.closeConfirm()

        var visibleDeletes = function () {
            var found = win.findChildren(chapterList, "deleteButton", [])
            var n = 0
            for (var i = 0; i < found.length; ++i)
                if (found[i].visible)
                    n++
            return n
        }
        win.want("nothing is downloaded, so no row offers Delete", visibleDeletes(), 0)

        chaptersModel.setProperty(0, "documentUuid", "doc-1")
        win.want("a downloaded row offers Delete", visibleDeletes(), 1)

        // The first tap asks the backend for the question. Nothing is deleted
        // by it, and that is the whole reason the step exists.
        win.findChild(chapterList, "deleteArea").clicked(null)
        win.want("tapping Delete asks once", win.deleteAsks, 1)
        win.want("and asks about the document on the row", win.deleteAskedAbout, "doc-1")
        win.want("asking deletes nothing", win.deleteConfirms, 0)

        // The backend's answer, as Main.qml applies it.
        chapterList.confirmingKind = "delete"
        chapterList.confirmingId = "doc-1"
        chapterList.confirmingMessage = "Move “The Lantern Keeper — Ch 0001.pdf” to your reMarkable’s Trash?"
        var strip = win.findChild(chapterList, "confirmStrip")
        win.want("the question is on screen", strip.visible, true)
        win.want("and it is not offering a download",
                 win.findChild(chapterList, "confirmDownloadButton").visible, false)

        // Keep is the way out, and it must leave the download alone.
        win.findChild(chapterList, "keepArea").clicked(null)
        win.want("Keep closes the question", strip.visible, false)
        win.want("Keep deletes nothing", win.deleteConfirms, 0)
        win.want("and puts the strip back to downloads", chapterList.confirmingKind, "download")

        // The answer that does delete carries the document, not the chapter.
        chapterList.confirmingKind = "delete"
        chapterList.confirmingId = "doc-1"
        chapterList.confirmingMessage = "Move it to the Trash?"
        win.findChild(chapterList, "confirmDeleteArea").clicked(null)
        win.want("confirming deletes once", win.deleteConfirms, 1)
        win.want("and names the document", win.deleteConfirmedAbout, "doc-1")
        win.want("confirming closes the question", strip.visible, false)

        // A question about a row that is no longer on screen must not survive
        // the view changing under it.
        chapterList.confirmingKind = "delete"
        chapterList.confirmingId = "doc-1"
        chapterList.confirmingMessage = "Move it to the Trash?"
        chapterList.showView("volumes")
        win.want("changing view closes the delete question", strip.visible, false)
        win.want("and forgets which kind it was", chapterList.confirmingKind, "download")
        chapterList.showView("chapters")

        // Put the row back as it was, so nothing below inherits a downloaded
        // row it did not ask for.
        chaptersModel.setProperty(0, "documentUuid", "")
        win.want("clearing the UUID takes the Delete button with it", visibleDeletes(), 0)

        // The volume view offers it on the same terms. A row that offers Read
        // offers Delete, whichever view the document is being looked at from.
        //
        // The volumes are put back first: the block above this one emptied the
        // model to prove a series that loses its volumes loses the switch.
        volumesModel.append({"chapterId": "c0", "title": "Volume 1",
                             "detail": "7 chapters, Chapter 1 to Chapter 7", "chapterCount": 7,
                             "downloadState": "", "downloadMessage": "", "documentUuid": ""})
        chapterList.showView("volumes")
        // Collected fresh each time: a ListView destroys and rebuilds its
        // delegates when the model changes, so a list held from before an
        // append is a list of objects that no longer exist.
        var visibleVolumeDeletes = function () {
            var found = win.findChildren(chapterList, "volumeDeleteButton", [])
            var n = 0
            for (var i = 0; i < found.length; ++i)
                if (found[i].visible)
                    n++
            return n
        }
        win.want("a volume with nothing downloaded offers no Delete", visibleVolumeDeletes(), 0)

        volumesModel.setProperty(0, "documentUuid", "doc-vol")
        // The list is laid out before it is inspected: the block above emptied
        // this model, and a ListView keeps the delegates of a model it no
        // longer has until it is asked to lay out again. Without this the
        // assertion below reads three dead rows instead of the live one.
        win.findChild(chapterList, "volumeRows").forceLayout()
        win.want("a downloaded volume row offers Delete", visibleVolumeDeletes(), 1)

        var asksBeforeVolume = win.deleteAsks
        win.findChild(chapterList, "volumeDeleteArea").clicked(null)
        win.want("the volume row asks once", win.deleteAsks, asksBeforeVolume + 1)
        win.want("and asks about the volume's document", win.deleteAskedAbout, "doc-vol")
        win.want("asking from the volume view deletes nothing", win.deleteConfirms, 1)

        chapterList.closeConfirm()
        volumesModel.setProperty(0, "documentUuid", "")
        win.want("clearing it takes the volume Delete button too", visibleVolumeDeletes(), 0)
        chapterList.showView("chapters")

        // ---- selecting several rows (PLAN §12.1) ---------------------------
        //
        // The properties worth holding: the mode is entered and left from one
        // control each, only rows with something to queue can be picked,
        // leaving clears, and the question is answered once for the lot.

        chapterList.showView("chapters")
        chapterList.leaveSelection()
        chapterList.closeConfirm()


        var selectBoxes = function () {
            var found = win.findChildren(chapterList, "selectBox", [])
            var n = 0
            for (var i = 0; i < found.length; ++i)
                if (found[i].visible)
                    n++
            return n
        }

        var bar = win.findChild(chapterList, "selectionBar")
        win.want("the list starts out of selection mode", chapterList.selecting, false)
        win.want("so there is no selection bar", bar.visible, false)
        win.want("and no boxes on the rows", selectBoxes(), 0)

        win.findChild(chapterList, "selectArea").clicked(null)
        win.want("Select enters the mode", chapterList.selecting, true)
        win.want("the bar comes up with it", bar.visible, true)
        win.want("the way in goes away while the mode is on",
                 win.findChild(chapterList, "selectButton").visible, false)

        // Only rows that can be downloaded are candidates. Counted as a
        // difference rather than against the page size: what matters is that
        // finishing a row takes it out of the running, not how many rows a
        // viewport of this height happens to hold.
        var pickable = selectBoxes()
        chaptersModel.setProperty(1, "downloadState", "done")
        chaptersModel.setProperty(3, "documentUuid", "doc-read")
        win.findChild(chapterList, "chapterRows").forceLayout()
        win.want("a row in the library and a row already downloaded drop out",
                 selectBoxes(), pickable - 2)

        // Picking is toggling, and it queues nothing on its own.
        chapterList.toggleSelected("c0", "", "")
        chapterList.toggleSelected("c2", "", "")
        win.want("two rows are selected", chapterList.selectedCount, 2)
        win.want("the count says so", win.findChild(chapterList, "selectionCount").text, "2 selected")
        win.want("picking rows queues nothing", win.queueConfirms, 0)

        chapterList.toggleSelected("c2", "", "")
        win.want("tapping a selected row takes it off", chapterList.selectedCount, 1)

        // A row with nothing to queue cannot be added, however it is asked.
        chapterList.toggleSelected("c1", "done", "")
        chapterList.toggleSelected("c3", "", "doc-read")
        win.want("a finished row cannot be selected", chapterList.selectedCount, 1)

        // The footer queues, immediately: no question, no strip. Picking the
        // rows was the deliberate step, and the user asked for the second one
        // to go.
        win.findChild(chapterList, "queueSelectionArea").clicked(null)
        win.want("the footer queues once", win.queueConfirms, 1)
        win.want("it queues exactly the rows picked", win.queueSent.length, 1)
        win.want("and the row it queues is the one picked", win.queueSent[0], "c0")
        win.want("it says which list they came from", win.queueSentVolumes, false)
        win.want("queueing leaves the mode", chapterList.selecting, false)
        win.want("and clears the selection", chapterList.selectedCount, 0)

        // Nothing is asked on the way: the strip stays where it was, which is
        // away.
        var strip = win.findChild(chapterList, "confirmStrip")
        win.want("no question is put in front of it", strip.visible, false)
        win.want("and the strip is not left in a queue mode",
                 chapterList.confirmingKind, "download")

        // An empty selection has nothing to queue, and the footer says no by
        // doing nothing rather than by sending an empty message.
        win.findChild(chapterList, "selectArea").clicked(null)
        win.findChild(chapterList, "queueSelectionArea").clicked(null)
        win.want("an empty selection queues nothing", win.queueConfirms, 1)
        win.want("and stays in the mode", chapterList.selecting, true)
        win.findChild(chapterList, "cancelSelectionArea").clicked(null)

        // Leaving by the footer selects nothing, and turning a page forgets
        // what was picked on the page before it.
        win.findChild(chapterList, "selectArea").clicked(null)
        chapterList.toggleSelected("c0", "", "")
        win.want("picked again", chapterList.selectedCount, 1)
        win.findChild(chapterList, "cancelSelectionArea").clicked(null)
        win.want("Cancel leaves the mode", chapterList.selecting, false)
        win.want("Cancel selects nothing", chapterList.selectedCount, 0)
        win.want("Cancel queues nothing", win.queueConfirms, 1)

        win.findChild(chapterList, "selectArea").clicked(null)
        chapterList.toggleSelected("c0", "", "")
        chapterList.page = 2
        win.want("turning the page clears the selection", chapterList.selectedCount, 0)
        win.want("but stays in the mode", chapterList.selecting, true)
        chapterList.page = 1

        // Switching lists leaves the mode outright: a selection of chapters
        // means nothing among volumes.
        chapterList.toggleSelected("c0", "", "")
        chapterList.showView("volumes")
        win.want("switching view leaves selection mode", chapterList.selecting, false)
        win.want("and takes the selection with it", chapterList.selectedCount, 0)
        chapterList.showView("chapters")

        // A refresh of the same series refills the model, and the selection
        // survives that only if the rows it names are still there and still
        // have something to queue. Both ways of losing one are checked,
        // because the failure is silent either way: a count that says 2 with
        // one row on screen carrying it.
        // Each case starts from its own selection rather than carrying on from
        // the one before: chained state is how an assertion ends up passing
        // for a reason that has nothing to do with what it claims to test.
        chapterList.enterSelection()
        chapterList.toggleSelected("c0", "", "")
        chapterList.toggleSelected("c2", "", "")
        win.want("two rows picked before a refresh", chapterList.selectedCount, 2)
        chaptersModel.setProperty(2, "downloadState", "done")
        chapterList.pruneSelection()
        win.want("a picked row that came back downloaded drops out",
                 chapterList.selectedCount, 1)
        win.want("and the one still offering a download stays",
                 chapterList.selectedIds[0], "c0")
        chaptersModel.setProperty(2, "downloadState", "")
        chapterList.leaveSelection()

        chapterList.enterSelection()
        chapterList.toggleSelected("c0", "", "")
        chapterList.toggleSelected("c2", "", "")
        win.want("two rows picked before a shorter refresh", chapterList.selectedCount, 2)
        chaptersModel.remove(2)
        chapterList.pruneSelection()
        win.want("a picked row the refresh no longer lists drops out",
                 chapterList.selectedCount, 1)
        win.want("and the row still listed stays", chapterList.selectedIds[0], "c0")
        chaptersModel.insert(2, {"chapterId": "c2", "title": "Chapter 2", "number": 2,
                                 "published": "2026-01-01", "scanlator": "Group",
                                 "downloadState": "", "downloadMessage": "", "documentUuid": ""})
        chapterList.leaveSelection()

        // Put the rows back as they were found.
        chaptersModel.setProperty(1, "downloadState", "")
        chaptersModel.setProperty(3, "documentUuid", "")

        // ---- the download cache control (PLAN §12.4) -----------------------
        //
        // The size is shown because a button to clear something of unknown size
        // is a button nobody presses; the first tap asks; confirming clears
        // once. Every sentence here is the backend's — the screen is asserted
        // to render what it is given, not to compose anything.

        settings.closeCacheQuestion()
        var cacheLine = win.findChild(settings, "cacheSummary")
        var clearButton = win.findChild(settings, "clearCacheButton")
        var cachePanel = win.findChild(settings, "cacheConfirm")

        settings.cacheSummary = "The download cache is holding 636.0 MB of page images."
        win.want("the cache size is on screen", cacheLine.text,
                 "The download cache is holding 636.0 MB of page images.")
        win.want("the clear button is offered", clearButton.visible, true)
        win.want("and no question is open yet", cachePanel.visible, false)

        // The first tap asks the backend for the question and clears nothing.
        var clearsBefore = win.cacheClears
        win.findChild(settings, "clearCacheArea").clicked(null)
        win.want("tapping Clear asks once", win.cacheClearAsks, 1)
        win.want("asking clears nothing", win.cacheClears, clearsBefore)

        // The backend's question, as Main.qml applies it.
        settings.cacheQuestion = "Clear 636.0 MB of cached page images?"
        win.want("the question is on screen", cachePanel.visible, true)
        win.want("in the backend's words",
                 win.findChild(settings, "cacheQuestion").text,
                 "Clear 636.0 MB of cached page images?")
        win.want("and the button it replaces is gone", clearButton.visible, false)

        // "Keep it" is the way out, and it clears nothing.
        win.findChild(settings, "keepCacheArea").clicked(null)
        win.want("Keep it closes the question", cachePanel.visible, false)
        win.want("Keep it clears nothing", win.cacheClears, clearsBefore)
        win.want("and the clear button comes back", clearButton.visible, true)

        // Confirming clears exactly once.
        settings.cacheQuestion = "Clear 636.0 MB of cached page images?"
        win.findChild(settings, "confirmClearArea").clicked(null)
        win.want("confirming clears once", win.cacheClears, clearsBefore + 1)
        win.want("confirming closes the question", cachePanel.visible, false)

        // The result sentence lands in the same line as the size, because it is
        // the most recent true thing said about the cache.
        settings.cacheSummary = "Cleared 592.0 MB. 44.0 MB was left, because a download is still using it."
        win.want("the outcome replaces the size", cacheLine.text,
                 "Cleared 592.0 MB. 44.0 MB was left, because a download is still using it.")

        // ---- sorting a download into its series folder (PLAN §6 M5) --------
        //
        // The device is five callbacks, and every case below builds its own —
        // a fallback that passes because the case before it left the world in
        // the right state is not a test of anything.
        //
        // fakeDevice(opts) answers the way the hardware does: creating returns
        // an id and puts the folder under the parent it was given, moving
        // reparents the selection, and both can be told to misbehave.
        function fakeDevice(opts) {
            var o = opts || {}
            var parents = o.parents || {}
            var made = 0
            return {
                log: [],
                parents: parents,
                parentOf: function (id) { return parents[id] === undefined ? "" : parents[id] },
                createFolder: function (parent, name) {
                    this.log.push("create(" + parent + "," + name + ")")
                    if (o.createThrows) throw new Error("no")
                    if (o.createReturnsNothing) return ""
                    var id = "made-" + (++made)
                    // The silent failure the device really has: an unusable
                    // parent is ignored and the folder lands at the root.
                    parents[id] = o.createIgnoresParent ? "" : parent
                    return id
                },
                // One call, named documents, no selection: the library-level
                // move that replaced the tree explorer's on 2026-09-18.
                move: function (ids, folderId) {
                    this.log.push("move(" + ids.join(",") + "->" + folderId + ")")
                    if (o.moveThrows) throw new Error("refused")
                    if (o.moveDoesNothing) return
                    var n = o.moveOnlyFirst ? 1 : ids.length
                    for (var i = 0; i < n; ++i) parents[ids[i]] = folderId
                }
            }
        }

        // The ordinary case: no folder yet, so one is made under Comics and the
        // documents go into it.
        var dev = fakeDevice({ parents: { "doc-1": "comics" } })
        var res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a folder is created when there is none", res.created, true)
        win.want("the document is moved into it", res.moved.length, 1)
        win.want("and the folder id comes back", res.folderId, "made-1")
        win.want("the folder is made under Comics", dev.log[0], "create(comics,Wandance)")

        // A folder the backend already found is used as it is.
        dev = fakeDevice({ parents: { "doc-1": "comics", "known": "comics" } })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "known", createUnder: "comics", folderName: "Wandance"})
        win.want("a known folder is not created again", res.created, false)
        win.want("and is what the document is moved into", dev.parents["doc-1"], "known")

        // Every part of a split volume moves, in one selection.
        dev = fakeDevice({ parents: { "p1": "comics", "p2": "comics", "p3": "comics" } })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["p1", "p2", "p3"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("every part of a split volume moves", res.moved.length, 3)
        win.want("in one move call", dev.log.length, 2)
        win.want("naming every one of them", dev.log[1], "move(p1,p2,p3->made-1)")

        // The silent failure that cost five probe rounds: a parent the device
        // does not accept is ignored and the folder lands at the root. Nothing
        // may be moved into it.
        dev = fakeDevice({ parents: { "doc-1": "comics" }, createIgnoresParent: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a folder at the wrong parent is not used", res.moved.length, 0)
        win.want("the document stays in Comics", dev.parents["doc-1"], "comics")
        win.want("and the reason says where it landed",
                 res.detail.indexOf("rather than") >= 0, true)

        // Creation refused outright.
        dev = fakeDevice({ parents: { "doc-1": "comics" }, createThrows: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a create that throws moves nothing", res.moved.length, 0)
        win.want("and leaves the document where it was", dev.parents["doc-1"], "comics")

        // The move that returns quietly and changes nothing — the failure this
        // whole file is written around.
        dev = fakeDevice({ parents: { "doc-1": "comics" }, moveDoesNothing: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a move that changes nothing is not a move", res.moved.length, 0)
        win.want("and says so", res.detail, "the move changed nothing")

        // A partial move is reported as a partial move.
        dev = fakeDevice({ parents: { "p1": "comics", "p2": "comics" }, moveOnlyFirst: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["p1", "p2"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a partial move counts what moved", res.moved.length, 1)
        win.want("and says how many of how many", res.detail, "moved 1 of 2")

        // A move that throws still has its work checked: the parents are read
        // back either way, because a call that complains and moves things is
        // not the same as one that complains and does not.
        dev = fakeDevice({ parents: { "doc-1": "comics" }, moveThrows: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a move that throws moves nothing", res.moved.length, 0)
        win.want("and the reason carries the throw", res.detail.indexOf("refused") >= 0, true)

        // Comics itself missing: it is created first, and the series folder
        // goes inside it.
        dev = fakeDevice({ parents: { "doc-1": "" } })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "", folderName: "Wandance",
            createComics: true, comicsName: "Comics"})
        win.want("Comics is created when it is missing", dev.log[0], "create(,Comics)")
        win.want("and the series folder goes inside it", dev.log[1], "create(made-1,Wandance)")
        win.want("and the document lands in the series folder", dev.parents["doc-1"], "made-2")

        // Nothing to sort is not an error.
        dev = fakeDevice({})
        res = Sorting.sortDocuments(dev, { documentUuids: [], folderName: "Wandance" })
        win.want("an empty list creates nothing", dev.log.length, 0)
        win.want("and says why", res.detail, "nothing to sort")

        // The folder name is the series' own name, tidied rather than schemed.
        win.want("a title is trimmed", Sorting.folderName("  Wandance  "), "Wandance")
        win.want("inner whitespace is collapsed", Sorting.folderName("A\n\tB"), "A B")
        win.want("path characters are dropped", Sorting.folderName("Fate/Zero"), "Fate Zero")
        win.want("a very long title is capped",
                 Sorting.folderName(new Array(200).join("x")).length, 60)
        win.want("an empty title stays empty", Sorting.folderName(null), "")

        // ---- reconciling with the tablet (PLAN §12.4) ----------------------
        //
        // The guard worth the most here is the one that says "I could not
        // check": an empty list of missing documents from a frontend that never
        // looked must never read as "none of them exist", because what follows
        // from that is every record dropped and the whole page cache deleted.

        var present = {"a": true, "b": true, "c": true}
        var device = {resolves: function (id) { return present[id] === true }}

        var all = Reconcile.check(device, ["a", "b", "c"])
        win.want("a complete check says so", all.checked, true)
        win.want("and finds nothing missing", all.missing.length, 0)

        // A document the user deleted on the tablet.
        present = {"a": true, "c": true}
        var some = Reconcile.check(device, ["a", "b", "c"])
        win.want("a deleted document is reported missing", some.missing.length, 1)
        win.want("and it is the one that went", some.missing[0], "b")
        win.want("the check still counts as done", some.checked, true)

        // A document that moved is not a document that went: entryForId still
        // resolves it wherever the user filed it, and folder membership is
        // never the question.
        present = {"a": true, "b": true, "c": true}
        var moved = Reconcile.check({resolves: function (id) {
            // Pretend "b" now lives in a folder of the user's own; it still
            // resolves.
            return present[id] === true
        }}, ["a", "b", "c"])
        win.want("a document that merely moved is not missing", moved.missing.length, 0)

        // No bridge at all.
        var blind = Reconcile.check(null, ["a", "b", "c"])
        win.want("no bridge means the check was not done", blind.checked, false)
        win.want("and reports nothing missing", blind.missing.length, 0)

        // A lookup that throws abandons the whole answer rather than returning
        // the part of it that ran.
        var thrower = Reconcile.check({resolves: function (id) {
            if (id === "b")
                throw new Error("no")
            return false
        }}, ["a", "b", "c"])
        win.want("a lookup that throws fails the whole check", thrower.checked, false)
        win.want("and reports nothing missing, not the part it saw",
                 thrower.missing.length, 0)

        // Nothing to ask about is a complete answer.
        var none = Reconcile.check(device, [])
        win.want("an empty question is answered completely", none.checked, true)

        // ---- the downloaded overview (PLAN §12.5) --------------------------
        //
        // Each case refills the model from empty, because a row surviving from
        // the case before it is a row nobody chose.
        function downloadedRows(rows) {
            downloadedModel.clear()
            for (var i = 0; i < rows.length; ++i)
                downloadedModel.append(rows[i])
            win.findChild(downloadedList, "downloadedRows").forceLayout()
        }

        function visibleRowAreas() {
            var found = win.findChildren(downloadedList, "downloadedRowArea", [])
            var n = 0
            for (var i = 0; i < found.length; ++i)
                if (found[i].enabled)
                    n++
            return n
        }

        // The empty state is the backend's sentence, and it is the only thing
        // on screen when there is nothing.
        downloadedRows([])
        downloadedList.emptyNote = "Nothing downloaded yet."
        var emptyLine = win.findChild(downloadedList, "downloadedEmpty")
        win.want("an empty library shows the backend's line", emptyLine.visible, true)
        win.want("in the backend's words", emptyLine.text, "Nothing downloaded yet.")

        // One row per downloaded series, and the empty line goes away.
        downloadedRows([{
            "sourceId": "src-a", "sourceName": "Example Reader",
            "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
            "detail": "3 downloads", "openable": true, "note": ""}])
        win.want("a downloaded series is listed", downloadedList.rowCount, 1)
        win.want("and the empty line is gone", emptyLine.visible, false)

        // Tapping a row opens *that* series on *that* source.
        var opensBefore = win.downloadedOpens
        win.findChild(downloadedList, "downloadedRowArea").clicked(null)
        win.want("a row opens once", win.downloadedOpens, opensBefore + 1)
        win.want("it opens the row's source", win.downloadedOpenedSource, "src-a")
        win.want("and the row's series", win.downloadedOpenedSeries, "/manga/lantern/")

        // Two sources holding the same series are two rows, each carrying its
        // own source — the navigational reason the row is a pair.
        downloadedRows([
            {"sourceId": "src-a", "sourceName": "Example Reader", "seriesId": "/manga/lantern/",
             "title": "The Lantern Keeper", "detail": "1 download", "openable": true, "note": ""},
            {"sourceId": "src-b", "sourceName": "Other Reader", "seriesId": "/series/lantern/",
             "title": "The Lantern Keeper", "detail": "2 downloads", "openable": true, "note": ""}])
        win.want("the same series from two sources is two rows", downloadedList.rowCount, 2)
        var areas = win.findChildren(downloadedList, "downloadedRowArea", [])
        win.downloadedOpens = 0
        areas[1].clicked(null)
        win.want("the second row opens its own source", win.downloadedOpenedSource, "src-b")
        win.want("and its own series id", win.downloadedOpenedSeries, "/series/lantern/")

        // A row whose source has been removed is listed and inert.
        downloadedRows([{
            "sourceId": "gone", "sourceName": "gone", "seriesId": "/manga/orphan/",
            "title": "An Orphan", "detail": "2 downloads", "openable": false,
            "note": "The source this came from has been removed."}])
        win.want("a removed source still lists its downloads", downloadedList.rowCount, 1)
        // `enabled` is the assertion, not a synthesised tap: emitting clicked()
        // from here invokes the handler directly and bypasses `enabled`
        // entirely, so a "tapping it does nothing" check written that way
        // passes or fails for reasons that have nothing to do with the device.
        // What makes the row inert for real input is the property.
        win.want("but the row cannot be tapped", visibleRowAreas(), 0)
        win.want("the row's tap target is disabled",
                 win.findChild(downloadedList, "downloadedRowArea").enabled, false)

        // It pages, like every other list here.
        var many = []
        for (var d = 0; d < 40; ++d)
            many.push({"sourceId": "src-a", "sourceName": "Example Reader",
                       "seriesId": "/manga/" + d + "/", "title": "Series " + d,
                       "detail": "1 download", "openable": true, "note": ""})
        downloadedRows(many)
        win.want("a long list pages", downloadedList.totalPages > 1, true)
        var pagerNext = win.findChild(downloadedList, "downloadedPager")
        win.want("and the pager is on screen", pagerNext.visible, true)
        downloadedList.page = 2
        win.want("turning the page moves the window", downloadedList.page, 2)
        downloadedRows([])
        win.want("a list that empties resets to the first page", downloadedList.page, 1)

        // ---- deleting one document (PLAN §12.4) ----------------------------
        //
        // Measured on hardware 2026-09-17: a live entry cannot be deleted --
        // `deleteEntries` on one is accepted and ignored -- and an entry that
        // is in the Trash is removed without disturbing anything else in
        // there. Both halves are what the confirmation sentence now promises,
        // so both are driven here, and every case builds its own device.

        function tablet(opts) {
            var o = opts || {}
            var parent = o.parent === undefined ? "comics" : o.parent
            var here = o.absent ? false : true
            return {
                calls: [],
                selected: [],
                deleted: null,
                exists: function () { return here },
                parentOf: function () { return parent },
                idFor: function (uuid) {
                    if (o.noEntry)
                        return ""
                    // The id is read again between the two steps, so a device
                    // that can name the entry before the move and not after is
                    // a state worth having: it is what an entry that changed
                    // identity on its way to the Trash would look like.
                    if (o.loseEntryAfterTrash && parent === "trash")
                        return ""
                    return o.wrapperIds ? "entry:" + uuid : uuid
                },
                // moveEntriesToTrash, named: no selection to build up, drop,
                // or leave behind. The trash step stopped going through the
                // tree explorer on 2026-09-18, because 3.27 has no
                // `explorer.selection` inside an AppLoad app.
                moveToTrash: function (ids) {
                    this.calls.push("moveToTrash")
                    this.trashed = ids
                    if (o.trashFails)
                        return
                    parent = "trash"
                },
                deleteEntries: function (ids) {
                    this.calls.push("deleteEntries")
                    this.deleted = ids
                    if (o.deleteThrows)
                        throw new Error("no")
                    // The measured precondition: only an entry already in the
                    // Trash goes, and an entry object in the list is accepted
                    // and ignored.
                    if (parent === "trash" && !o.deleteIgnored)
                        here = false
                }
            }
        }

        // The ordinary delete: trashed, then removed, and nothing left selected.
        var dev = tablet({})
        win.want("a document is deleted", Deleting.deleteDocument(dev, "doc-1"), "ok")
        win.want("by trashing it first",
                 dev.calls.indexOf("moveToTrash") < dev.calls.indexOf("deleteEntries"), true)
        win.want("and then removing that one entry",
                 dev.calls[dev.calls.length - 1], "deleteEntries")
        win.want("with nothing left selected", dev.selected.length, 0)

        // The id goes through the entry, not straight from the uuid. On 3.25
        // they are the same string, so only a device that distinguishes them
        // can show that this is what the code does -- and 3.28 is such a
        // device, which is the whole reason for the indirection.
        dev = tablet({wrapperIds: true})
        Deleting.deleteDocument(dev, "doc-1")
        win.want("the entry id is what is deleted", dev.deleted[0], "entry:doc-1")

        // The measured silent failure: the call is accepted and the document is
        // still there. Reporting that as a delete would tell the user their
        // download is gone while it sits in their Trash.
        dev = tablet({deleteIgnored: true})
        win.want("a delete that did nothing is not a delete",
                 Deleting.deleteDocument(dev, "doc-1"), "kept")

        // The same honesty when the call throws.
        dev = tablet({deleteThrows: true})
        win.want("a throw on the second step is kept, not failed",
                 Deleting.deleteDocument(dev, "doc-1"), "kept")

        // No entry to name means no id to act on at all, and now that the first
        // step is `moveEntriesToTrash([id])` rather than a selection, that is
        // caught before anything is touched: nothing moved, so the answer is
        // "failed" -- the document is still in the user's library and the
        // backend must keep its record. Falling back to the raw uuid would work
        // on 3.25 and be the silent no-op on 3.28.
        dev = tablet({noEntry: true})
        win.want("an unnameable entry is a failure, not a guess",
                 Deleting.deleteDocument(dev, "doc-1"), "failed")
        win.want("and nothing was trashed", dev.calls.indexOf("moveToTrash"), -1)
        win.want("and nothing was passed to deleteEntries", dev.deleted, null)

        // Unnameable only *after* the trash step: the document is out of the
        // library, so this is "kept" and not a failure -- the record goes and
        // the note says where it ended up.
        dev = tablet({loseEntryAfterTrash: true})
        win.want("an entry that cannot be named for the second step is kept",
                 Deleting.deleteDocument(dev, "doc-1"), "kept")
        win.want("having been trashed", dev.calls.indexOf("moveToTrash") >= 0, true)
        win.want("and nothing was passed to deleteEntries", dev.deleted, null)

        // The trash step did not take: the selection did not empty. Nothing may
        // be deleted on top of that.
        dev = tablet({trashFails: true})
        win.want("a document that would not move is a failure",
                 Deleting.deleteDocument(dev, "doc-1"), "failed")
        win.want("and nothing was deleted", dev.deleted, null)
        win.want("though it was asked for by name", dev.trashed[0], "doc-1")

        // The measured shape of a wrong argument on this surface: the call is
        // accepted, returns normally, and nothing moves. The parent read back
        // is the only thing that catches it, and the delete that would follow
        // is the measured no-op on a live entry, so it is never attempted.
        dev = tablet({})
        dev.moveToTrash = function (ids) { this.calls.push("moveToTrash"); this.trashed = ids }
        win.want("a trash call that changed nothing is a failure",
                 Deleting.deleteDocument(dev, "doc-1"), "failed")
        win.want("and deleteEntries was never called", dev.deleted, null)

        // A trash call that throws. Same answer, for the same reason: the
        // document is still in the user's library.
        dev = tablet({})
        dev.moveToTrash = function () { this.calls.push("moveToTrash"); throw new Error("refused") }
        win.want("a trash call that threw is a failure",
                 Deleting.deleteDocument(dev, "doc-1"), "failed")
        win.want("and nothing was deleted", dev.deleted, null)

        // Already off the tablet. §6 M6 owns that wording, so it is named, not
        // reported as a delete.
        dev = tablet({absent: true})
        win.want("a document already gone says so",
                 Deleting.deleteDocument(dev, "doc-1"), "gone")

        // No device at all: the bridge failed to load, and nothing may be
        // claimed.
        win.want("no tablet deletes nothing", Deleting.deleteDocument(null, "doc-1"), "failed")

        // ---- what a screen re-asks for when it is shown --------------------
        //
        // The Downloaded overview was fetched on the way in from the source
        // list and not when it was returned to from a series, so deleting a
        // series' last download left a row on screen pointing at nothing.

        win.want("the downloaded overview is fetched every time it is shown",
                 Screens.refreshOnShow("downloaded"), "listDownloaded")

        // The screens that need nothing, and each for its own reason: the
        // watched list is pushed by the backend, browse is the source's own
        // catalogue, and the series screen already refetches on every route in.
        win.want("the watched list is not refetched", Screens.refreshOnShow("watching"), "")
        win.want("browse is not refetched", Screens.refreshOnShow("browse"), "")
        win.want("the series screen is not refetched here", Screens.refreshOnShow("series"), "")
        win.want("the source list is not refetched", Screens.refreshOnShow("sources"), "")
        win.want("an unknown screen asks for nothing", Screens.refreshOnShow("no-such-screen"), "")

        // ---- deleting every download of a series (PLAN §12.5) --------------
        //
        // One action on the row, several documents underneath it, and each of
        // them fails on its own.

        // The report is per document, and a run does not stop at the first
        // failure: four deletable downloads must not be abandoned because the
        // first one would not move.
        var world = {
            gone: {},
            refuse: {},
            exists: function (id) { return !this.gone[id] },
            parentOf: function (id) { return this.gone[id] ? "" : this.trashed === id ? "trash" : "comics" },
            idFor: function (id) { return id },
            moveToTrash: function (ids) {
                if (this.refuse[ids[0]])
                    return
                this.trashed = ids[0]
            },
            deleteEntries: function (ids) {
                if (this.trashed === ids[0])
                    this.gone[ids[0]] = true
            }
        }
        world.refuse["doc-b"] = true
        var many = Deleting.deleteMany(world, ["doc-a", "doc-b", "doc-c"])
        win.want("every document is reported", many.results.length, 3)
        win.want("the first went", many.results[0].removed, true)
        win.want("the one that would not move is reported as not trashed",
                 many.results[1].trashed, false)
        win.want("and the run carried on past it", many.results[2].removed, true)
        win.want("two deleted", many.deleted, 2)
        win.want("one failed", many.failed, 1)

        // A document already off the tablet counts as deleted: it is not on the
        // reMarkable, which is the state the user asked for, and calling it a
        // failure would keep a record for a document that does not exist.
        var absent = {
            exists: function () { return false },
            parentOf: function () { return "" },
            idFor: function (id) { return id },
            moveToTrash: function () {},
            deleteEntries: function () {}
        }
        var already = Deleting.deleteMany(absent, ["doc-a"])
        win.want("a document already gone counts as deleted", already.deleted, 1)
        win.want("and is reported as trashed", already.results[0].trashed, true)

        // No bridge: nothing was deleted, and every document says so rather
        // than the batch reporting one vague failure.
        var nothing = Deleting.deleteMany(null, ["doc-a", "doc-b"])
        win.want("with no library nothing is deleted", nothing.deleted, 0)
        win.want("and every document is still reported", nothing.results.length, 2)
        win.want("each as a failure", nothing.results[0].trashed, false)

        // Nothing to delete is not an error, and produces no claims.
        win.want("an empty list reports nothing", Deleting.deleteMany(world, []).results.length, 0)

        // ---- the row's delete button ---------------------------------------

        downloadedRows([{
            "sourceId": "src-a", "sourceName": "Example Reader",
            "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
            "detail": "3 downloads", "openable": true, "note": ""}])

        // Asking is not deleting: the tap asks the backend for a sentence and
        // nothing else happens.
        var asksBefore = win.downloadedDeleteAsks
        win.findChild(downloadedList, "deleteSeriesArea").clicked(null)
        win.want("the row asks to delete", win.downloadedDeleteAsks, asksBefore + 1)
        win.want("naming its own series", win.downloadedAskedSeries, "/manga/lantern/")
        win.want("and nothing is deleted yet", win.downloadedDeletes, 0)

        // The strip is closed until the backend answers, and the question is
        // the backend's sentence rather than one composed here.
        var strip = win.findChild(downloadedList, "downloadedConfirmStrip")
        win.want("no question is open before the backend answers", strip.visible, false)
        downloadedList.confirmingSourceId = "src-a"
        downloadedList.confirmingSeriesId = "/manga/lantern/"
        downloadedList.confirmingMessage = "Delete all 3 downloads of “The Lantern Keeper”?"
        win.want("the backend's answer opens the question", strip.visible, true)
        win.want("in the backend's words",
                 win.findChild(downloadedList, "downloadedConfirmMessage").text,
                 "Delete all 3 downloads of “The Lantern Keeper”?")

        // Keep is the way out, and it deletes nothing.
        win.findChild(downloadedList, "keepSeriesArea").clicked(null)
        win.want("Keep closes the question", strip.visible, false)
        win.want("and deletes nothing", win.downloadedDeletes, 0)

        // Confirming deletes that series, once, and closes the question.
        downloadedList.confirmingSourceId = "src-a"
        downloadedList.confirmingSeriesId = "/manga/lantern/"
        downloadedList.confirmingMessage = "Delete all 3 downloads of “The Lantern Keeper”?"
        win.findChild(downloadedList, "confirmDeleteSeriesArea").clicked(null)
        win.want("confirming deletes once", win.downloadedDeletes, 1)
        win.want("the series it named", win.downloadedDeletedSeries, "/manga/lantern/")
        win.want("and the question closes behind it", strip.visible, false)

        // The row body still opens the series -- explicitly asked for -- so the
        // tap target stops where the button starts instead of covering it.
        var rowArea = win.findChild(downloadedList, "downloadedRowArea")
        var button = win.findChild(downloadedList, "deleteSeriesButton")
        win.want("the row body does not cover the delete button",
                 rowArea.width + button.width <= downloadedList.width, true)
        win.downloadedOpens = 0
        rowArea.clicked(null)
        win.want("and tapping the row still opens the series", win.downloadedOpens, 1)

        // A row whose source is gone can still be deleted: those downloads are
        // on the tablet and this screen is the only way left to reach them.
        downloadedRows([{
            "sourceId": "src-x", "sourceName": "src-x",
            "seriesId": "/manga/orphan/", "title": "Orphaned",
            "detail": "1 download", "openable": false, "note": "The source has been removed."}])
        asksBefore = win.downloadedDeleteAsks
        win.findChild(downloadedList, "deleteSeriesArea").clicked(null)
        win.want("a row with no source still offers Delete",
                 win.downloadedDeleteAsks, asksBefore + 1)
        win.want("for its own series", win.downloadedAskedSeries, "/manga/orphan/")

        // ---- answering even when the device throws -------------------------
        //
        // On 3.27 a filing request got **no answer at all**: the call into
        // xochitl's QML threw, the exception unwound past the send that would
        // have reported it, and the backend waited out its thirty-second
        // ceiling on a question the frontend had already given up on. Every
        // handler has the same shape, so every handler is driven here against a
        // bridge that throws.

        function angryBridge() {
            return {
                sort: function () { throw new Error("treeExplorerForNavigation is null") },
                check: function () { throw new Error("entryForId is not a function") },
                trash: function () { throw new Error("selection is undefined") },
                trashMany: function () { throw new Error("selection is undefined") },
                open: function () { throw new Error("documentViewLoader is null") }
            }
        }

        // First, the property underneath all of them: **nothing
        // escapes**. Without this the symptom of a missing catch is that the
        // harness itself dies mid-script -- silence, which is exactly the
        // failure being guarded against and a terrible thing to have to read.
        function escapes(call) {
            try {
                call()
            } catch (e) {
                return true
            }
            return false
        }

        win.want("a throw never escapes the filing answer",
                 escapes(function () { Answers.sorted(angryBridge(), {"documentUuids": ["doc-a"]}) }), false)
        win.want("nor the reconcile answer",
                 escapes(function () { Answers.checked(angryBridge(), ["doc-a"]) }), false)
        win.want("nor the delete answer",
                 escapes(function () { Answers.trashed(angryBridge(), "doc-a") }), false)
        win.want("nor the series delete answer",
                 escapes(function () { Answers.trashedMany(angryBridge(), ["doc-a"]) }), false)
        win.want("nor the reader handoff",
                 escapes(function () { Answers.handoff(angryBridge(), "doc-a", -1) }), false)

        // Filing: an answer, naming the throw, and claiming nothing was moved.
        var sortAsk = {"documentUuids": ["doc-a", "doc-b"], "folderId": "f1",
                       "folderName": "Kingdom", "sourceId": "src", "seriesId": "ser"}
        var sortReply = Answers.sorted(angryBridge(), sortAsk)
        win.want("a filing that threw still answers", sortReply.moved.length, 0)
        win.want("and keeps the documents it was asked about",
                 sortReply.documentUuids.length, 2)
        win.want("and reports the throw", sortReply.detail.indexOf("treeExplorerForNavigation") >= 0, true)
        win.want("and does not claim a folder was made", sortReply.created, false)

        // Without a bridge at all the answer is the same shape, with the other
        // reason: the log must be able to tell the two apart.
        var noBridge = Answers.sorted(null, sortAsk)
        win.want("no bridge answers too", noBridge.moved.length, 0)
        win.want("with its own reason", noBridge.detail, Answers.NO_BRIDGE)

        // Reconcile: the dangerous one. A throw must answer `checked: false`
        // with an empty missing list -- never an empty list on its own, which
        // the backend is entitled to read as "none of them exist" and act on by
        // deleting every record and the whole page cache.
        var checkReply = Answers.checked(angryBridge(), ["doc-a", "doc-b"])
        win.want("a check that threw says it could not check", checkReply.checked, false)
        win.want("and reports nothing missing", checkReply.missing.length, 0)
        win.want("and echoes what it was asked about", checkReply.documentUuids.length, 2)
        win.want("and reports the throw", checkReply.detail.indexOf("entryForId") >= 0, true)

        // A bridge that answers with nothing has not answered.
        var mute = Answers.checked({check: function () { return null }}, ["doc-a"])
        win.want("a silent bridge is not a clean check", mute.checked, false)
        win.want("and nothing is reported missing", mute.missing.length, 0)

        // A bridge's own answer is passed through untouched: deciding here what
        // a reply "really means" would be a second opinion on a question only
        // the bridge can ask.
        var real = Answers.checked({check: function () {
            return {"checked": true, "documentUuids": ["doc-a"], "missing": ["doc-a"]}
        }}, ["doc-a"])
        win.want("a real check is passed through", real.checked, true)
        win.want("with its own findings", real.missing[0], "doc-a")

        // Deleting one document: a throw is "failed", which is what stops the
        // backend forgetting a record whose document is still on the tablet.
        var one = Answers.trashed(angryBridge(), "doc-a")
        win.want("a delete that threw is a failure", one.result, "failed")
        win.want("and says why", one.detail.indexOf("selection is undefined") >= 0, true)

        // Deleting a series: one failure per document, not one for the batch.
        var batch = Answers.trashedMany(angryBridge(), ["doc-a", "doc-b", "doc-c"])
        win.want("a batch that threw reports every document", batch.results.length, 3)
        win.want("each as a failure", batch.results[2].trashed, false)
        win.want("nothing deleted", batch.deleted, 0)
        win.want("and says why once", batch.detail.indexOf("selection is undefined") >= 0, true)

        // ---- the handoff ---------------------------------------------------
        //
        // The frontend used to close itself here, to stop the reader opening
        // behind it on AppLoad v0.5.3. On v0.5.0 the reader comes forward on
        // its own, so the handoff is back to being one thing: open the
        // document, and tell the backend what happened.

        var good = Answers.handoff({open: function () { return true }}, "doc-a", -1)
        win.want("a successful open forgets nothing", good.forget, false)
        win.want("and reports no missing document", good.reply.missing, undefined)
        win.want("and does not ask the shell to close", good.close, undefined)

        // A failed open: the row forgets the dead document, and the backend is
        // told so it can compose the sentence about it.
        var bad = Answers.handoff({open: function () { return false }}, "doc-a", -1)
        win.want("a failed open reports the document missing", bad.reply.missing, true)
        win.want("and the row forgets it", bad.forget, true)

        // A throw is a failed open, with the reason carried to the backend log.
        var threwOpen = Answers.handoff(angryBridge(), "doc-a", -1)
        win.want("an open that threw still answers the backend", threwOpen.reply.missing, true)
        win.want("and reports the throw",
                 threwOpen.reply.detail.indexOf("documentViewLoader") >= 0, true)

        // No bridge: the same, with the other reason.
        var noReader = Answers.handoff(null, "doc-a", -1)
        win.want("no bridge reports the document missing", noReader.reply.missing, true)
        win.want("and says why", noReader.reply.detail, Answers.NO_BRIDGE)

        console.log(win.failures === 0 ? "HARNESS OK" : "HARNESS FAILED: " + win.failures)
        Qt.exit(win.failures === 0 ? 0 : 1)
    }
}
