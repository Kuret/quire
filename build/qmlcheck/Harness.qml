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
    }
    WatchList   { id: watchList;   objectName: "watchList";   anchors.fill: parent; model: watchedModel }
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

        var deletes = win.findChildren(chapterList, "deleteButton", [])
        var visibleDeletes = function () {
            var n = 0
            for (var i = 0; i < deletes.length; ++i)
                if (deletes[i].visible)
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

        console.log(win.failures === 0 ? "HARNESS OK" : "HARNESS FAILED: " + win.failures)
        Qt.exit(win.failures === 0 ? 0 : 1)
    }
}
