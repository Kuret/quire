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
import "../../ui/Views.js" as Views
import "../../ui/Grouping.js" as Grouping
import "../../ui/Kinds.js" as Kinds
import "../../ui/Covers.js" as Covers


Window {
    id: win
    width: 1620; height: 2160
    visible: true

    ListModel {
        id: sourcesModel
        ListElement { sourceId: "s1"; name: "Example Reader"; baseUrl: "https://example.invalid"
                      theme: "madara"; lang: "en"; enabled: true; status: "Working"; statusDetail: ""
                      splitStrips: "never"; proxy: "http://localhost:1055"; selfHostedViaProxy: true
                      allowedHosts: "cdn.example.invalid"
                      pendingHosts: "[{\"host\":\"images.example.invalid\",\"purpose\":\"cover\"}]"
                      removeQuestion: "Remove Example Reader? Downloaded volumes stay in your library." }
        // No splitStrips at all: a source stored before PLAN §12.3 existed. It
        // has to read as Automatic rather than blank.
        ListElement { sourceId: "s2"; name: "Another"; baseUrl: "https://other.invalid"
                      theme: "mangadex"; lang: "en"; enabled: false; status: "Off"; statusDetail: ""
                      removeQuestion: "Remove Another? Its 4 chapters saved in Quire will be deleted; anything in your library stays." }
    }
    ListModel { id: seriesModel }
    ListModel { id: chaptersModel }
    ListModel { id: volumesModel }
    ListModel { id: emptyVolumesModel }
    ListModel { id: watchedModel }
    ListModel { id: downloadedModel }

    // One book's releases — the files it is available as, titled by the backend
    // exactly as they arrive (ui/Kinds.js). They fill the same roles a chapter
    // does, because they are the same rows on the same screen; what differs is
    // that the two a chapter has and a release does not, `published` and
    // `scanlator`, are empty.
    ListModel { id: releasesModel }

    // One page of the combined search, filled per case from a whole reply the
    // way Main.qml fills it — through Grouping.js, which is the half of that
    // path the harness can run.
    ListModel { id: searchAllModel }

    // Stands in for Main.qml's root, so the summary can be applied to a target
    // whose writes are counted. (Main.qml itself loads perfectly well and is
    // driven by build/qmlcheck/MainHarness.qml — the comment that used to be
    // here, saying it could not be loaded because of a plugin, was wrong:
    // `Backend` is a plain QML file in Annex's lib/ and the obstacle was a
    // relative path.) The counters make "did this repaint?" observable:
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

    property int proxyAsks: 0
    property string proxyAskedFor: ""
    property string proxyAskedAbout: ""

    property int allowHostAsks: 0
    property string allowHostAskedFor: ""
    property string allowHostAskedAbout: ""
    property int revokeHostAsks: 0
    property string revokeHostAskedFor: ""
    property string revokeHostAskedAbout: ""

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

    // The layout switch (PLAN §7.1 type 75). A count, because the property
    // under test is that the half you are already in asks for *nothing*.
    property int viewAsks: 0
    property string viewAskedFor: ""

    // What each screen asked to open from a tile, and what each asked to fetch
    // covers for. The cover batches are kept whole: a downloaded or watched
    // batch can span sources, and which source each entry names is the thing
    // Main.qml splits on.
    property int seriesOpens: 0
    property string seriesOpenedId: ""
    property var seriesCovers: []

    property int watchOpens: 0
    property string watchOpenedSource: ""
    property string watchOpenedSeries: ""
    property int watchCoverAsks: 0
    property var watchCovers: []

    property int downloadedCoverAsks: 0
    property var downloadedCovers: []

    // What the long-press menu asked for, per screen. Counters again, and for
    // the sharpest version of the usual reason: the menu's destructive lines
    // must route into the question each screen already asks, so what is being
    // asserted is a count that did **not** move — no delete, no unwatch —
    // beside one that did.
    property int seriesWatches: 0
    property string seriesWatchedSeries: ""
    property int seriesUnwatches: 0
    property string seriesUnwatchedSeries: ""

    property int downloadedReads: 0
    property string downloadedReadUuid: ""
    // "Read latest" on a row whose newest thing is a chapter saved in Quire.
    property int downloadedSavedReads: 0
    property string downloadedSavedReadSource: ""
    property string downloadedSavedReadSeries: ""
    property string downloadedSavedReadChapter: ""
    property int downloadedWatches: 0
    property string downloadedWatchedSeries: ""
    property int downloadedUnwatches: 0

    property int watchDownloadNews: 0
    property string watchDownloadNewSeries: ""
    property int watchMarkSeens: 0
    property string watchMarkSeenSeries: ""
    property int watchUnwatches: 0

    // Continuing to read from the Watching screen (PLAN §12.9) — the same
    // pair of signals ui/DownloadedList.qml's own counters cover above.
    property int watchReads: 0
    property string watchReadUuid: ""
    property int watchSavedReads: 0
    property string watchSavedReadSource: ""
    property string watchSavedReadSeries: ""
    property string watchSavedReadChapter: ""

    // What the combined search asked for. Counters throughout, because half of
    // what this screen has to get right is a request that must **not** happen:
    // an empty query fans out to every configured source, so "nothing was
    // sent" is only observable as a count that did not move.
    property int searchAllAsks: 0
    property string searchAllAsked: ""
    property int searchAllPageAsks: 0
    property int searchAllPageAsked: 0
    property int searchAllCoverAsks: 0
    property var searchAllCovers: []

    property int searchAllOpens: 0
    property string searchAllOpenedKey: ""
    property string searchAllOpenedSource: ""
    property string searchAllOpenedSeries: ""
    property string searchAllOpenedTitle: ""

    // What the series screen's source switcher asked for. The pair, both
    // halves: switching source switches *which* (sourceId, seriesId) every
    // message from that screen is about, and a chip that sent the new source
    // with the old series id would be a plausible-looking bug.
    property int sourceSwitches: 0
    property string switchedToSource: ""
    property string switchedToName: ""
    property string switchedToSeries: ""

    // Try (milestone 1): what a chapter row's Try button asked for, and what
    // the reader itself asked the host for and reported closing.
    property int tryAsks: 0
    property string triedChapterId: ""
    property string triedTitle: ""
    property int tryCloses: 0
    property var tryPageWants: []
    // Saved mode's reading position (SavePosition), sent debounced or by
    // flushSavePosition — see the reader's own comment.
    property var savePositionWants: []
    // The reader's two library actions.
    property int sendToLibraryAsks: 0
    property int saveInQuireAsks: 0

    // Book mode's own settings panel: every settings object the reader asked
    // to change, in order.
    property var settingsChangeAsks: []

    // Saved in Quire: what a saved row's Read/Delete asked for.
    property int readSavedAsks: 0
    property string readSavedChapterId: ""
    property int deleteSavedAsks: 0
    property string deleteSavedAskedAbout: ""
    property int deleteSavedConfirms: 0
    property string deleteSavedConfirmedAbout: ""

    // A whole saved volume's Delete (round 2): the label and chapter ids it
    // asked to remove.
    property int deleteVolumeAsks: 0
    property string deleteVolumeAskedLabel: ""
    property var deleteVolumeAskedAbout: []
    property int deleteVolumeConfirms: 0
    property var deleteVolumeConfirmedAbout: []

    // What the probe wizard answered with: the choice, and the value typed into
    // the question's own field. A counter, because the field must not turn into
    // an answer by itself — the answer is the option the user tapped.
    property int probeAnswers: 0
    property string probeAnsweredId: ""
    property string probeAnsweredText: ""

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

    // ---- reading a long-press menu off a screen ---------------------------
    //
    // The menu is addressed the way a finger addresses it: by what is actually
    // on screen. Nothing here reaches into the screen's item list — that list
    // is the thing under test, and a test that asked the screen what it meant
    // to draw would agree with it however wrong it was.

    function menuActions(screen) {
        var out = []
        var items = win.findChildren(screen, "contextMenuItem", [])
        for (var i = 0; i < items.length; ++i)
            out.push(items[i].action)
        return out
    }

    function menuLabels(screen) {
        var out = []
        var labels = win.findChildren(screen, "contextMenuLabel", [])
        for (var i = 0; i < labels.length; ++i)
            out.push(String(labels[i].text))
        return out
    }

    function menuItemFor(screen, action) {
        var items = win.findChildren(screen, "contextMenuItem", [])
        for (var i = 0; i < items.length; ++i)
            if (items[i].action === action)
                return items[i]
        return null
    }

    // menuLive answers "could a finger use this line?" with the property that
    // decides it for real input. A synthesised clicked() would invoke the
    // handler whatever `enabled` says, which is how a menu item that is greyed
    // out and still live passes a test written the other way.
    function menuLive(screen, action) {
        var item = win.menuItemFor(screen, action)
        if (!item)
            return false
        var area = win.findChild(item, "contextMenuItemArea")
        return area !== null && area.enabled
    }

    function tapMenu(screen, action) {
        var item = win.menuItemFor(screen, action)
        if (!item)
            return false
        win.findChild(item, "contextMenuItemArea").clicked(null)
        return true
    }

    // holdOn drives the long press the *screen* sees: the area's own signal,
    // which runs the delegate's handler, the coordinate mapping and the item
    // list with it. When the press has to be timed rather than assumed, the
    // hold phase at the foot of this file drives beginPress() instead and
    // lets the real timers decide.
    // The press point is the middle of the item unless a case is about where
    // the menu lands, which is what the clamping cases pass.
    function holdOn(area, x, y) {
        area.pressX = x === undefined ? area.width / 2 : x
        area.pressY = y === undefined ? area.height / 2 : y
        area.held()
    }

    // colorOf names a colour the way Style.js writes it. QML hands colours back
    // in lower case, and a comparison against the token would otherwise fail
    // for the spelling rather than for the colour.
    function colorOf(item) {
        return String(item.color).toUpperCase()
    }

    // channels splits "#RRGGBB" into its three numbers, so a case can say
    // "this is not a grey" and "this is deep, not pale" without naming the
    // value — which is the only way those two intents can be pinned at all.
    // Comparing a colour against Style.accent proves nothing about what
    // Style.accent is.
    function channels(hex) {
        return [parseInt(hex.substr(1, 2), 16),
                parseInt(hex.substr(3, 2), 16),
                parseInt(hex.substr(5, 2), 16)]
    }

    // The WCAG relative luminance and contrast ratio, so a case can assert the
    // pairing the accent was *chosen* for rather than restating its hex.
    // Style.js says black on the fill is 5.90:1 and the fill on paper is
    // 3.56:1; these are what check that the value still delivers them.
    function luminance(hex) {
        var c = win.channels(hex)
        var l = []
        for (var i = 0; i < 3; ++i) {
            var v = c[i] / 255
            l.push(v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4))
        }
        return 0.2126 * l[0] + 0.7152 * l[1] + 0.0722 * l[2]
    }

    function contrast(a, b) {
        var la = win.luminance(a)
        var lb = win.luminance(b)
        return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
    }

    // Every piece of text under an item, found by what a Text *is* rather than
    // by objectName: a heading somebody recolours is exactly the one nobody
    // remembered to give a name to. This is what "no word carries the accent"
    // is asserted over.
    function textsUnder(item, out) {
        if (!item)
            return out
        if (item.font !== undefined && typeof item.text === "string")
            out.push(item)
        for (var i = 0; i < item.children.length; ++i)
            win.textsUnder(item.children[i], out)
        return out
    }

    // Every word a screen actually puts in front of somebody, joined.
    //
    // Visible text only, and `visible` on a QML item is the *effective* one —
    // a Text inside a collapsed strip reads false — which is what makes this
    // the honest question to ask of a book's series screen: not "is the volume
    // switch hidden" but "does the word Volumes appear anywhere on it".
    function wordsOn(item) {
        var texts = win.textsUnder(item, [])
        var said = []
        for (var i = 0; i < texts.length; ++i)
            if (texts[i].visible && String(texts[i].text).length > 0)
                said.push(String(texts[i].text))
        return said.join(" | ")
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
        onProxyRequested: {
            win.proxyAsks++
            win.proxyAskedFor = proxy
            win.proxyAskedAbout = sourceId
        }
        onAllowHostRequested: {
            win.allowHostAsks++
            win.allowHostAskedFor = host
            win.allowHostAskedAbout = sourceId
        }
        onRevokeHostRequested: {
            win.revokeHostAsks++
            win.revokeHostAskedFor = host
            win.revokeHostAskedAbout = sourceId
        }
    }
    AddSource {
        id: addSource
        objectName: "addSource"
        anchors.fill: parent
        onAnswerRequested: {
            win.probeAnswers++
            win.probeAnsweredId = answerId
            win.probeAnsweredText = answerText
        }
    }
    SeriesGrid {
        id: seriesGrid
        objectName: "seriesGrid"
        anchors.fill: parent
        model: seriesModel
        onOpenRequested: {
            win.seriesOpens++
            win.seriesOpenedId = seriesId
        }
        onCoversRequested: win.seriesCovers = covers
        onWatchRequested: {
            win.seriesWatches++
            win.seriesWatchedSeries = seriesId
        }
        onUnwatchRequested: {
            win.seriesUnwatches++
            win.seriesUnwatchedSeries = seriesId
        }
    }
    SearchAll {
        id: searchAll
        objectName: "searchAll"
        anchors.fill: parent
        model: searchAllModel
        onSearchRequested: {
            win.searchAllAsks++
            win.searchAllAsked = query
        }
        onPageRequested: {
            win.searchAllPageAsks++
            win.searchAllPageAsked = page
        }
        onCoversRequested: {
            win.searchAllCoverAsks++
            win.searchAllCovers = covers
        }
        onOpenRequested: {
            win.searchAllOpens++
            win.searchAllOpenedKey = key
            win.searchAllOpenedSource = sourceId
            win.searchAllOpenedSeries = seriesId
            win.searchAllOpenedTitle = title
        }
    }

    ChapterList { id: chapterList; objectName: "chapterList"; anchors.fill: parent; model: chaptersModel
                  volumeModel: volumesModel
                  onTryRequested: {
                      win.tryAsks++
                      win.triedChapterId = chapterId
                      win.triedTitle = title
                  }
                  onSourceSwitchRequested: {
                      win.sourceSwitches++
                      win.switchedToSource = sourceId
                      win.switchedToName = sourceName
                      win.switchedToSeries = seriesId
                  }
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
                  onReadSavedRequested: {
                      win.readSavedAsks++
                      win.readSavedChapterId = chapterId
                  }
                  onDeleteSavedRequested: {
                      win.deleteSavedAsks++
                      win.deleteSavedAskedAbout = chapterId
                  }
                  onDeleteSavedConfirmed: {
                      win.deleteSavedConfirms++
                      win.deleteSavedConfirmedAbout = chapterId
                  }
                  onDeleteVolumeRequested: {
                      win.deleteVolumeAsks++
                      win.deleteVolumeAskedLabel = volumeLabel
                      win.deleteVolumeAskedAbout = chapterIds
                  }
                  onDeleteVolumeConfirmed: {
                      win.deleteVolumeConfirms++
                      win.deleteVolumeConfirmedAbout = chapterIds
                  }
                  synopsis: "A long description that runs on and on. " }

    // A second chapter screen with no volumes at all, which is what a source
    // that publishes no labels looks like. The affordance has to be absent
    // there, not empty: PLAN §6 M4, revised 2026-09-16.
    ChapterList { id: plainChapterList; objectName: "plainChapterList"; anchors.fill: parent
                  model: chaptersModel; volumeModel: emptyVolumesModel }

    // The same screen showing a book. **It is deliberately given the volume
    // model that has three rows in it**: a book has no volumes whatever a
    // reply happens to contain, and the screen has to refuse the view rather
    // than merely be handed nothing to draw in it.
    ChapterList { id: bookChapterList; objectName: "bookChapterList"; anchors.fill: parent
                  kind: "book"; model: releasesModel; volumeModel: volumesModel
                  synopsis: "A novel about spice." }

    // The Try reader (milestone 1): read a chapter without downloading it.
    // Standalone, the same way PagerBar and the tile grid above are — it is
    // driven directly by its own API (begin/ready/pageArrived/countUpdated)
    // exactly as Main.qml drives it, rather than through a fixture model.
    TryReader {
        id: tryReader
        objectName: "tryReader"
        anchors.fill: parent
        onCloseRequested: win.tryCloses++
        onPageWanted: {
            win.tryPageWants.push(index)
        }
        onSavePositionWanted: {
            win.savePositionWants.push(index)
        }
        onSendToLibraryRequested: win.sendToLibraryAsks++
        onSaveInQuireRequested: win.saveInQuireAsks++
        onSettingsChangeRequested: win.settingsChangeAsks.push(settings)
    }
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
    WatchList {
        id: watchList
        objectName: "watchList"
        anchors.fill: parent
        model: watchedModel
        onOpenRequested: {
            win.watchOpens++
            win.watchOpenedSource = sourceId
            win.watchOpenedSeries = seriesId
        }
        onCoversRequested: {
            win.watchCoverAsks++
            win.watchCovers = covers
        }
        onUnwatchRequested: win.watchUnwatches++
        onDownloadNewRequested: {
            win.watchDownloadNews++
            win.watchDownloadNewSeries = seriesId
        }
        onMarkSeenRequested: {
            win.watchMarkSeens++
            win.watchMarkSeenSeries = seriesId
        }
        onReadRequested: {
            win.watchReads++
            win.watchReadUuid = documentUuid
        }
        onReadSavedRequested: {
            win.watchSavedReads++
            win.watchSavedReadSource = sourceId
            win.watchSavedReadSeries = seriesId
            win.watchSavedReadChapter = chapterId
        }
    }

    // The layout switch lives in Main.qml's header. It is driven here on its
    // own, against Views.js, for what the control itself does: both halves
    // drawn, the half you are already in inert. What it asks the *shell* for —
    // and that the layout does not flip until the status comes back — is in
    // build/qmlcheck/MainHarness.qml, which loads the real Main.qml.
    ViewToggle {
        id: viewToggle
        objectName: "viewToggle"
        onViewRequested: {
            win.viewAsks++
            win.viewAskedFor = view
        }
    }

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
        onCoversRequested: {
            win.downloadedCoverAsks++
            win.downloadedCovers = covers
        }
        onReadRequested: {
            win.downloadedReads++
            win.downloadedReadUuid = documentUuid
        }
        onReadSavedRequested: {
            win.downloadedSavedReads++
            win.downloadedSavedReadSource = sourceId
            win.downloadedSavedReadSeries = seriesId
            win.downloadedSavedReadChapter = chapterId
        }
        onWatchRequested: {
            win.downloadedWatches++
            win.downloadedWatchedSeries = seriesId
        }
        onUnwatchRequested: win.downloadedUnwatches++
    }
    PagerBar    { id: lonePager;   width: 1620 }

    // A standalone tile grid, isolated from any screen, for the tile
    // subtitle's geometry: hasSubtitles is a plain property here, so setting
    // it directly is reliable, unlike going through a screen's own computed
    // binding (which only re-evaluates when the thing it actually reads
    // changes — see the search-results section below for that end-to-end
    // wiring instead).
    ListModel { id: subtitleTilesModel }
    CoverGrid {
        id: subtitleTiles
        objectName: "subtitleTiles"
        width: 1620; height: 2160
        model: subtitleTilesModel
    }

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
            // Every role Main.qml's fillSeries fills, including `watched`,
            // which the long-press menu's Watch / Stop watching line reads,
            // and `authors`, which the row's subtitle reads. A ListModel
            // fixes its roles on the first append, so a fixture without one
            // could never be given it later — see the authors-role-missing
            // case below, which uses a plain object rather than this model
            // for exactly that reason.
            seriesModel.append({"seriesId": "x" + i, "title": "Series " + i,
                                "coverUrl": "https://example.invalid/c.jpg", "coverPath": "",
                                "watched": false, "authors": ""})
        for (var j = 0; j < 55; ++j)
            chaptersModel.append({"chapterId": "c" + j, "title": "Chapter " + j, "number": j,
                                  "published": "2026-01-01", "scanlator": "Group",
                                  "downloadState": "", "downloadMessage": "", "documentUuid": "",
                                  "saved": false})
        for (var w = 0; w < 25; ++w)
            watchedModel.append(WatchJs.row({
                "sourceId": "src", "seriesId": "w" + w, "sourceName": "Example Reader",
                "title": "Watched " + w, "newChapters": 0,
                "state": "ok", "status": "Up to date", "checkedAt": "2026-09-16T00:00:00Z"}))

        // Every role a volume row can carry, from this first append: a
        // ListModel's schema is fixed by its first append, and a role
        // missing from it is silently dropped on every later write to any
        // row, even one that names it (round 2's savedCount/chapterIds/label).
        for (var v = 0; v < 3; ++v)
            volumesModel.append({"chapterId": "c" + (v * 7), "title": "Volume " + (v + 1),
                                 "label": "" + (v + 1),
                                 "detail": "7 chapters, Chapter 1 to Chapter 7",
                                 "chapterCount": 7,
                                 "downloadState": "", "downloadMessage": "", "documentUuid": "",
                                 "saved": false, "savedCount": 0, "chapterIdsJson": "[]"})

        // Three releases of one book, titled the way the backend titles them.
        // Every role a chapter row has, because it is the same row: the two a
        // release has nothing to put in are empty, which is what the screen
        // must not turn into "Date unknown".
        var releases = ["EPUB · 0.4MB · Direct Download · fiction",
                        "PDF · 2.1MB · Direct Download · fiction",
                        "MOBI · 0.5MB · Mirror · fiction"]
        for (var r = 0; r < releases.length; ++r)
            releasesModel.append({"chapterId": "r" + r, "title": releases[r], "number": 0,
                                  "published": "", "scanlator": "",
                                  "downloadState": "", "downloadMessage": "",
                                  "documentUuid": "", "saved": false})

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

    // An exception thrown anywhere in the cases below would otherwise leave
    // this process hung rather than failing: finish() is what exits, and a case
    // that threw never reaches it. A hang is not a failure anyone can read, and
    // it is exactly what a mutation that empties a batch produces when the next
    // line indexes into it.
    function check() {
        try {
            win.runChecks()
        } catch (err) {
            console.log("FAIL the harness threw before it finished: " + err)
            win.failures++
            win.finish()
        }
    }

    function runChecks() {
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

        // ---- the keyboard, and what it must not disturb -----------------
        //
        // Quire's keyboard (ui/Keyboard.qml) is part of the layout, not an
        // overlay: it takes the bottom of the screen and the screens are built
        // around it. On the series grid it is anchored above the pager rather
        // than over it, so the question that matters is the one below — does
        // raising it move the pager out from under a thumb, or change how many
        // tiles a page holds?
        var sizeBefore = seriesGrid.pageSize
        var pagerYBefore = win.findChild(seriesGrid, "seriesPager").y
        seriesGrid.searching = true
        win.want("the keyboard does not resize the page", seriesGrid.pageSize, sizeBefore)
        win.want("the keyboard does not move the pager",
                 win.findChild(seriesGrid, "seriesPager").y, pagerYBefore)
        seriesGrid.searching = false

        // Every screen that owns a field, showing, for the dismissal cases.
        seriesGrid.visible = true
        addSource.visible = true
        addSource.reset()
        sourceList.renamingId = "src-a"
        sourceList.visible = true

        // ---- and what puts it away ---------------------------------------
        //
        // Quire's keyboard goes away with the screen or the panel that owns it,
        // and `dismissInput` is what the rest of the app calls to be sure. It
        // is two moves in a fixed order — drop the field's focus, *then* lower
        // the keyboard — and both halves are asserted here: the focus half on
        // the two real TextInputs, the lowering half on the keyboard's own
        // visibility.

        // First, the fact the rule is built on, measured rather than assumed:
        // **a tap somewhere else does not move focus.** If it did, most of this
        // would be unnecessary.
        var nameField = win.findChild(sourceList, "nameField")
        nameField.forceActiveFocus()
        win.want("a field can take focus", nameField.activeFocus, true)
        win.findChild(sourceList, "renameSaveButton").children[1].clicked(null)
        win.want("a tap elsewhere does not clear it by itself",
                 nameField.activeFocus || nameField.focus, true)

        // The rename panel carries its own keyboard, so it is up exactly when
        // the panel is.
        var renameKeys = win.findChild(sourceList, "renameKeyboard")
        win.want("the rename panel brings a keyboard", renameKeys !== null, true)
        win.want("and it is up while the panel is", renameKeys.visible, true)

        // Closing the panel puts the keyboard away with it.
        sourceList.dismissInput()
        win.want("dismissing drops the field's focus", nameField.activeFocus, false)
        sourceList.renamingId = ""
        win.want("closing the panel takes the keyboard with it", renameKeys.visible, false)

        // Accept: the panel does not survive the thing it was raised for.
        sourceList.renamingId = "src-a"
        sourceList.renameText = "A new name"
        nameField.forceActiveFocus()
        sourceList.commitRename()
        win.want("committing a rename drops the focus", nameField.activeFocus, false)

        // Navigating away. showScreen is Main.qml's and cannot be driven here,
        // but what it calls is each screen's dismissInput, and that is what is
        // asserted: the screens put their own keyboards away when asked.
        //
        // The search bar is a display, not an input — nothing on the device
        // would raise a keyboard for a focused field — so "the keyboard is up"
        // is `searching`, and that is what is driven and asserted here, by way
        // of a real tap on the bar.
        var searchField = win.findChild(seriesGrid, "searchField")
        var searchKeys = win.findChild(seriesGrid, "searchKeyboard")
        win.findChild(seriesGrid, "searchBarArea").clicked(null)
        win.want("tapping the search bar raises the keyboard", seriesGrid.searching, true)
        win.want("and the keyboard is on screen", searchKeys.visible, true)
        seriesGrid.dismissInput()
        win.want("dismissing lowers the search keyboard", searchKeys.visible, false)
        // The placeholder comes back with it, deliberately: "Search this
        // source" is the right label for a screen nobody is typing into.
        win.want("and searching follows that too", seriesGrid.searching, false)
        win.want("the placeholder comes back",
                 win.findChild(seriesGrid, "searchPlaceholder").visible, true)

        // ---- Clear, on the search field ----------------------------------
        //
        // Asked for as a way to start a *new* search without holding backspace
        // down, which is what decides its behaviour: clearing is the beginning
        // of typing, so it must not put the keyboard away.

        // Absent on an empty field: a control that is always lit is one people
        // learn to ignore.
        seriesGrid.query = ""
        var clearButton = win.findChild(seriesGrid, "clearSearchButton")
        win.want("an empty search offers nothing to clear", clearButton.visible, false)

        // Present as soon as there is something to clear.
        seriesGrid.query = "lantern"
        win.want("a typed search offers Clear", clearButton.visible, true)

        // A finger-sized target on an e-ink screen, asserted as geometry.
        win.want("Clear is a real tap target",
                 clearButton.width >= 120 && clearButton.height >= 60, true)

        // It empties the field.
        seriesGrid.query = "lantern"
        win.findChild(seriesGrid, "clearSearchArea").clicked(null)
        win.want("Clear empties the field", searchField.text, "")
        win.want("and the screen agrees", seriesGrid.query, "")

        // **And keeps the keyboard up.** Tap Clear, keyboard vanishes, tap the
        // bar again to carry on -- that is more taps than the backspacing
        // this replaced.
        win.want("Clear leaves the keyboard on screen", searchKeys.visible, true)
        win.want("so the keyboard stays up", seriesGrid.searching, true)

        // The results stay until a new search runs: the user is mid-task, and
        // blanking the grid on the way to typing takes away what they may be
        // comparing against.
        win.want("Clear does not empty the grid", seriesGrid.model.count > 0, true)
        seriesGrid.dismissInput()

        // Typing does not dismiss anything. The failure mode of an over-eager
        // rule is a field that closes its own keyboard mid-word. Driven
        // through the keyboard's own signals, which is the only way text ever
        // reaches this field.
        seriesGrid.query = ""
        seriesGrid.searching = true
        searchKeys.keyTyped("l")
        searchKeys.keyTyped("a")
        searchKeys.keyTyped("n")
        win.want("the keys reach the query", seriesGrid.query, "lan")
        win.want("and the field shows what was typed", searchField.text, "lan")
        win.want("typing keeps the keyboard up", seriesGrid.searching, true)
        searchKeys.keyTyped("t")
        win.want("and keeps it as the query grows", seriesGrid.searching, true)
        searchKeys.backspace()
        win.want("backspace takes one character back", seriesGrid.query, "lan")
        searchKeys.clearAll()
        win.want("and a held backspace clears the lot", seriesGrid.query, "")
        win.want("without putting the keyboard away", seriesGrid.searching, true)
        seriesGrid.dismissInput()

        // ---- the browse listing picker -------------------------------------
        //
        // "Browse: <label>" once a Msg.Listings answer named the current
        // listing, plain "Browse" before that or when it will not fit;
        // tapping it opens a full-width, grouped picker; choosing an entry
        // browses it and closes the panel.
        seriesGrid.reset()
        var browseLabel = win.findChild(seriesGrid, "browseButtonLabel")
        win.want("before any Listings answer, the button just says Browse",
                 browseLabel.text, "Browse")
        win.want("and the default listing is latest", seriesGrid.currentListingId, "latest")

        seriesGrid.applyListings([
            {"id": "latest", "label": "Latest updates", "group": "sort"},
            {"id": "popular", "label": "Popular", "group": "sort"},
            {"id": "completed", "label": "Completed", "group": "status"},
            {"id": "genre:action", "label": "Action", "group": "genre"},
            {"id": "genre:romance", "label": "Romance", "group": "genre"}
        ])
        win.want("the backend's own label fills in for the listing showing now",
                 seriesGrid.currentListingLabel, "Latest updates")
        // "Browse: Latest updates" measures wider than this button — the
        // "just 'Browse' if too long" half of the rule, exercised for real
        // rather than assumed: the shorter "Browse: Completed" below is what
        // proves the full wording *can* show when it fits.
        win.want("too long to fit, so the button falls back to plain Browse",
                 browseLabel.text, "Browse")

        var picker = win.findChild(seriesGrid, "listingPicker")
        win.want("the picker starts closed", picker.visible, false)
        win.findChild(seriesGrid, "browseButtonArea").clicked(null)
        win.want("tapping the button opens it", seriesGrid.pickerOpen, true)
        win.want("and the panel is on screen", picker.visible, true)
        // Visible is not drawn: an anchor Qt refused once left this panel with
        // no geometry, so Browse "opened" and showed nothing on the device.
        var pickerPanel = win.findChild(seriesGrid, "listingPickerPanel")
        var searchBarItem = win.findChild(seriesGrid, "searchBar")
        win.want("the open panel has real height", pickerPanel.height > 100, true)
        win.want("the open panel has real width", pickerPanel.width > 100, true)
        if (searchBarItem)
            win.want("and sits below the search bar",
                     pickerPanel.y >= searchBarItem.y + searchBarItem.height, true)

        var entries = win.findChildren(seriesGrid, "listingPickerEntry", [])
        win.want("one row per listing: two sort, one status, two genres",
                 entries.length, 5)

        // Columns follow the longest label in this source's list, one to
        // four. The longest real genre name seen (18 characters, "double
        // penetration") must fit whole; a label far longer than any real one
        // must drop the count rather than clip.
        var pickerColumnItem = win.findChild(seriesGrid, "listingPickerColumn")
        win.want("short labels lay out in four columns", pickerColumnItem.columns, 4)
        win.want("the listing on screen is marked",
                 entries.filter(function (e) { return Qt.colorEqual(e.color, Style.rule) }).length, 1)
        var shortListings = seriesGrid.listings
        seriesGrid.applyListings(shortListings.concat([
            {"id": "genre:dp", "label": "double penetration", "group": "genre"}]))
        win.want("the longest real genre name fits whole in its cell",
                 pickerColumnItem.cellWidth >= pickerColumnItem.longestLabel + pickerColumnItem.cellPadding, true)
        seriesGrid.applyListings(shortListings.concat([
            {"id": "genre:x", "label": "An Implausibly Long Genre Name For Testing", "group": "genre"}]))
        win.want("a far longer label drops the column count",
                 pickerColumnItem.columns < 4 && pickerColumnItem.columns >= 1, true)
        win.want("and still fits whole in its cell",
                 pickerColumnItem.cellWidth >= pickerColumnItem.longestLabel + pickerColumnItem.cellPadding, true)
        seriesGrid.applyListings(shortListings)

        // Choosing "Completed" (status group) browses it and closes the
        // picker — through the same browseRequested signal Clear and the old
        // "Latest" tap already used.
        var browseSignalCount = 0
        var onChosenBrowse = function () { browseSignalCount++ }
        seriesGrid.browseRequested.connect(onChosenBrowse)
        seriesGrid.chooseListing("completed", "Completed")
        win.want("choosing an entry closes the picker", seriesGrid.pickerOpen, false)
        win.want("and switches the current listing", seriesGrid.currentListingId, "completed")
        win.want("with the label it was given", seriesGrid.currentListingLabel, "Completed")
        win.want("clears any query underneath it", seriesGrid.query, "")
        win.want("and asks Main.qml to browse it", browseSignalCount, 1)
        win.want("the button now says so", browseLabel.text, "Browse: Completed")
        seriesGrid.browseRequested.disconnect(onChosenBrowse)

        // Tapping the scrim outside the panel dismisses it without choosing.
        seriesGrid.pickerOpen = true
        win.findChild(seriesGrid, "listingPickerScrim").clicked(null)
        win.want("tapping outside the panel closes it without a choice",
                 seriesGrid.pickerOpen, false)
        win.want("and the listing is unchanged", seriesGrid.currentListingId, "completed")

        // reset() (a real change of source) always goes back to latest, with
        // no listing menu held over from the source just left.
        seriesGrid.reset()
        win.want("reset() returns to the default listing",
                 seriesGrid.currentListingId, "latest")
        win.want("with nothing left to show for it yet",
                 seriesGrid.currentListingLabel, "")
        win.want("and no stale menu", seriesGrid.listings.length, 0)

        // The address field, the same way round.
        var addrField = win.findChild(addSource, "urlField")
        addrField.forceActiveFocus()
        addSource.start()
        win.want("starting a check drops the address field's focus",
                 addrField.activeFocus, false)

        // Its keyboard belongs to the form, so leaving the form lowers it.
        // With no address typed, `start` returns early and the form — and the
        // keyboard with it — is still there, which is right: the user has not
        // finished.
        var urlKeyboard = win.findChild(addSource, "addSourceKeyboard")
        win.want("the form carries a keyboard", urlKeyboard !== null, true)
        win.want("an empty address leaves the keyboard up", urlKeyboard.visible, true)
        addSource.url = "example.invalid"
        addSource.start()
        win.want("a real check takes the form away", addSource.phase, "probing")
        win.want("and the keyboard with it", urlKeyboard.visible, false)
        addSource.reset()
        win.want("and it is back on the form", urlKeyboard.visible, true)

        // ---- PLAN §7.5 stage 1: the question about a private address -------
        //
        // A source on the user's own network was unaddable until the probe
        // learned to *offer* a private address instead of refusing it. The
        // wizard's side of that is one question with one extra thing on it: an
        // optional field for a proxy, because the person adding a host on their
        // own network is the one whose device may not be able to route to it.
        //
        // Everything asserted here is the backend's. The wizard picks no
        // wording, offers no default proxy, and decides nothing about which
        // answer is safe.

        // First, the questions that ask for nothing typed — a redirect, a theme
        // — must look exactly as they did: no field, no second keyboard.
        addSource.onProgress({"question": {
            "kind": "redirect", "text": "You asked for a.invalid, but it sent Quire to b.invalid. Add b.invalid instead?",
            "options": [{"id": "continue", "label": "Yes, use b.invalid"},
                        {"id": "cancel", "label": "No, stop"}]}})
        var qBox = win.findChild(addSource, "questionInputBox")
        var qKeys = win.findChild(addSource, "questionKeyboard")
        win.want("a question puts the wizard in its question phase", addSource.phase, "question")
        win.want("a question with nothing to type shows no field", qBox.visible, false)
        win.want("and raises no keyboard", qKeys.visible, false)

        // Answering it sends the choice and an empty typed value: there was no
        // field, so there is nothing to carry.
        win.probeAnswers = 0
        var qOptions = win.findChildren(addSource, "questionOptionArea", [])
        qOptions[0].clicked(null)
        win.want("the answer goes to the backend once", win.probeAnswers, 1)
        win.want("naming the option tapped", win.probeAnsweredId, "continue")
        win.want("with nothing typed alongside it", win.probeAnsweredText, "")

        // Now the self-hosted offer, as the backend sends it.
        addSource.onProgress({"question": {
            "kind": "selfhosted",
            "text": "zima.example resolves to 100.79.171.1, which is a private address. Is this a service on your own network that you run?",
            "options": [{"id": "cancel", "label": "No, stop"},
                        {"id": "continue", "label": "Yes, it's mine — add it"}],
            "input": {"label": "If Quire has to go through a proxy to reach it, type the proxy here.",
                      "placeholder": "http://localhost:1055"}}})

        // The address is on screen. Agreeing to something unnamed is not
        // agreeing to anything, and the sentence is the backend's, verbatim.
        win.want("the question is shown as the backend wrote it",
                 addSource.questionText.indexOf("100.79.171.1") >= 0, true)
        win.want("the field appears with it", qBox.visible, true)
        win.want("labelled by the backend",
                 win.findChild(addSource, "questionInputLabel").text.indexOf("proxy") >= 0, true)
        win.want("and empty, with the example showing",
                 win.findChild(addSource, "questionFieldHint").visible, true)
        win.want("the example is the backend's too",
                 win.findChild(addSource, "questionFieldHint").text, "http://localhost:1055")

        // No is first: the refusal is the answer already in force, and the
        // order the backend sent is the order on screen.
        qOptions = win.findChildren(addSource, "questionOptionArea", [])
        win.want("both answers are offered", qOptions.length, 2)

        // Typing goes through the question's own keyboard — the device has no
        // system one — and reaches the field.
        win.want("a question with a field raises a keyboard", qKeys.visible, true)
        qKeys.keyTyped("h"); qKeys.keyTyped("t"); qKeys.keyTyped("t"); qKeys.keyTyped("p")
        win.want("the keys reach the field", addSource.questionInput, "http")
        win.want("and the field shows them",
                 win.findChild(addSource, "questionField").text, "http")
        win.want("the example gets out of the way",
                 win.findChild(addSource, "questionFieldHint").visible, false)
        qKeys.backspace()
        win.want("backspace takes one character back", addSource.questionInput, "htt")
        qKeys.clearAll()
        win.want("and a held backspace clears the lot", addSource.questionInput, "")
        win.want("without putting the keyboard away", qKeys.visible, true)

        // Typing is not answering. The count must not have moved.
        win.want("typing a proxy answers nothing by itself", win.probeAnswers, 1)

        // The answer carries both halves.
        addSource.questionInput = "http://localhost:1055"
        qOptions[1].clicked(null)
        win.want("the answer reaches the backend", win.probeAnswers, 2)
        win.want("saying yes", win.probeAnsweredId, "continue")
        win.want("and carrying the proxy typed", win.probeAnsweredText, "http://localhost:1055")
        win.want("the wizard goes back to waiting", addSource.phase, "probing")
        win.want("and the question's keyboard goes with it", qKeys.visible, false)

        // The field does not survive the question it belonged to.
        win.want("the field is put away", qBox.visible, false)
        win.want("and holds nothing for the next one", addSource.questionInput, "")

        addSource.reset()

        sourceList.renamingId = ""
        seriesGrid.visible = false
        addSource.visible = false

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

        // ---- removing a source: the question is the backend's -----------
        //
        // The strip shows backend/service/service.go's removeQuestion
        // verbatim (PLAN §2) rather than a sentence composed in QML, because
        // only the backend knows whether removing this source also deletes
        // any chapters saved in Quire.
        var removeLabel = win.findChild(sourceList, "removeQuestionLabel")
        sourceList.confirmingId = "s1"
        sourceList.confirmingRemoveQuestion = sourcesModel.get(0).removeQuestion
        win.want("a source with nothing saved gets the plain sentence",
                 removeLabel.text,
                 "Remove Example Reader? Downloaded volumes stay in your library.")

        sourceList.confirmingId = "s2"
        sourceList.confirmingRemoveQuestion = sourcesModel.get(1).removeQuestion
        win.want("a source with saved chapters names how many",
                 removeLabel.text,
                 "Remove Another? Its 4 chapters saved in Quire will be deleted; " +
                 "anything in your library stays.")

        sourceList.confirmingId = ""
        sourceList.confirmingRemoveQuestion = ""

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

        // ---- editing a source's proxy after it was added -------------------
        //
        // Before this, a wrong proxy meant deleting the source and adding it
        // again. The Proxy action opens the same kind of panel Rename does,
        // prefilled with what is stored, and warns before clearing the field
        // takes a ViaProxy confirmation with it.
        var proxyButton = win.findChild(sourceList, "proxyButton")
        win.want("the Proxy action is offered on the row strip", proxyButton !== null, true)

        var proxyPanel = win.findChild(sourceList, "proxyPanel")
        win.want("the proxy panel is closed until asked for", proxyPanel.visible, false)

        // Opened from the row strip, the way Rename and Splitting are: carrying
        // the source's own proxy and viaProxy flag along, since the strip is
        // outside the delegate that knows them.
        sourceList.confirmingId = "s1"
        sourceList.confirmingName = "Example Reader"
        sourceList.confirmingProxy = "http://localhost:1055"
        sourceList.confirmingViaProxy = true
        sourceList.startProxy(sourceList.confirmingId, sourceList.confirmingName,
                               sourceList.confirmingProxy, sourceList.confirmingViaProxy)
        win.want("the panel opens", proxyPanel.visible, true)
        win.want("opening it closes the row strip", sourceList.confirmingId, "")
        var proxyField = win.findChild(sourceList, "proxyField")
        win.want("the field prefills with the stored proxy",
                 proxyField.text, "http://localhost:1055")

        // The proxy panel carries its own keyboard, the way the rename panel
        // does.
        var proxyKeys = win.findChild(sourceList, "proxyKeyboard")
        win.want("the proxy panel brings a keyboard", proxyKeys !== null, true)
        win.want("and it is up while the panel is", proxyKeys.visible, true)

        // Clearing the field warns, because this source's confirmation stands
        // on the proxy being cleared.
        var proxyWarning = win.findChild(sourceList, "proxyRevokeWarning")
        win.want("no warning while a proxy is still typed", proxyWarning.visible, false)
        sourceList.proxyText = ""
        win.want("clearing the field on a ViaProxy source warns", proxyWarning.visible, true)

        // Submitting sends the sourceId and the (now empty) proxy, exactly
        // once.
        win.proxyAsks = 0
        sourceList.commitProxy()
        win.want("submitting asks once", win.proxyAsks, 1)
        win.want("it names the source", win.proxyAskedAbout, "s1")
        win.want("it sends what was typed", win.proxyAskedFor, "")
        win.want("committing closes the panel", proxyPanel.visible, false)

        // A source with no proxy and not confirmed via one — s2 — offers the
        // same action, prefilled empty, and never warns.
        sourceList.confirmingId = "s2"
        sourceList.confirmingName = "Another"
        sourceList.confirmingProxy = ""
        sourceList.confirmingViaProxy = false
        sourceList.startProxy(sourceList.confirmingId, sourceList.confirmingName,
                               sourceList.confirmingProxy, sourceList.confirmingViaProxy)
        win.want("a source with no proxy prefills empty", proxyField.text, "")
        win.want("no warning for a source not confirmed via a proxy",
                 proxyWarning.visible, false)

        win.proxyAsks = 0
        sourceList.proxyText = "http://localhost:1080"
        sourceList.commitProxy()
        win.want("setting a proxy on a bare source asks once", win.proxyAsks, 1)
        win.want("with the typed value", win.proxyAskedFor, "http://localhost:1080")

        // ---- the allowedHosts editor (record-and-offer) ---------------------
        //
        // A pending host is offered here and nowhere else — there is no modal
        // anywhere in Quire for this — and the panel has to say enough that
        // Allow is not a reflex: the host, the source, and what it was
        // fetching.
        var hostsButton = win.findChild(sourceList, "hostsButton")
        win.want("the Hosts action is offered on the row strip", hostsButton !== null, true)

        var hostsPanel = win.findChild(sourceList, "hostsPanel")
        win.want("the hosts panel is closed until asked for", hostsPanel.visible, false)

        sourceList.confirmingId = "s1"
        sourceList.confirmingName = "Example Reader"
        sourceList.confirmingPendingHosts =
            "[{\"host\":\"images.example.invalid\",\"purpose\":\"cover\"}]"
        sourceList.confirmingAllowedHosts = "cdn.example.invalid"
        sourceList.startHosts(sourceList.confirmingId, sourceList.confirmingName,
                               sourceList.confirmingPendingHosts,
                               sourceList.confirmingAllowedHosts)
        win.want("the panel opens", hostsPanel.visible, true)
        win.want("opening it closes the row strip", sourceList.confirmingId, "")

        // The pending host is named, and says what it was fetching in words —
        // never a bare hostname beside a button.
        var pendingRow = win.findChild(sourceList, "pendingHost-images.example.invalid")
        win.want("the pending host is shown", pendingRow !== null, true)
        win.want("nothing pending says so only when the list is empty",
                 win.findChild(sourceList, "noPendingHosts").visible, false)

        var allowedRow = win.findChild(sourceList, "allowedHost-cdn.example.invalid")
        win.want("the already-allowed host is shown", allowedRow !== null, true)
        win.want("none allowed says so only when the list is empty",
                 win.findChild(sourceList, "noAllowedHosts").visible, false)

        // Allowing sends the exact source and the exact host, once, and moves
        // the row from Pending to Allowed without waiting for a reply.
        win.allowHostAsks = 0
        win.findChild(sourceList, "allowHostArea-images.example.invalid").clicked(null)
        win.want("allowing asks once", win.allowHostAsks, 1)
        win.want("it names the source", win.allowHostAskedAbout, "s1")
        win.want("it names the exact host", win.allowHostAskedFor, "images.example.invalid")
        win.want("the panel stays open", hostsPanel.visible, true)
        win.want("the granted host leaves Pending",
                 win.findChild(sourceList, "pendingHost-images.example.invalid"), null)
        win.want("nothing pending now reappears",
                 win.findChild(sourceList, "noPendingHosts").visible, true)
        win.want("the granted host joins Allowed",
                 win.findChild(sourceList, "allowedHost-images.example.invalid") !== null, true)

        // Revoking sends the exact source and host and removes it from
        // Allowed the same way.
        win.revokeHostAsks = 0
        win.findChild(sourceList, "revokeHostArea-cdn.example.invalid").clicked(null)
        win.want("revoking asks once", win.revokeHostAsks, 1)
        win.want("it names the source", win.revokeHostAskedAbout, "s1")
        win.want("it names the exact host", win.revokeHostAskedFor, "cdn.example.invalid")
        win.want("the revoked host leaves Allowed",
                 win.findChild(sourceList, "allowedHost-cdn.example.invalid"), null)

        // A source with neither pending nor allowed hosts (s2) says so
        // plainly rather than showing two empty lists with no explanation.
        sourceList.confirmingId = "s2"
        sourceList.confirmingName = "Another"
        sourceList.confirmingPendingHosts = "[]"
        sourceList.confirmingAllowedHosts = ""
        sourceList.startHosts(sourceList.confirmingId, sourceList.confirmingName,
                               sourceList.confirmingPendingHosts,
                               sourceList.confirmingAllowedHosts)
        win.want("a source with nothing pending says so",
                 win.findChild(sourceList, "noPendingHosts").visible, true)
        win.want("a source with nothing allowed says so",
                 win.findChild(sourceList, "noAllowedHosts").visible, true)

        // Malformed JSON from an older backend must not crash the panel: it
        // reads as an empty pending list rather than propagating the parse
        // failure.
        sourceList.startHosts("s2", "Another", "not json", "")
        win.want("malformed pending JSON is treated as empty",
                 win.findChild(sourceList, "noPendingHosts").visible, true)

        win.findChild(sourceList, "hostsDoneArea").clicked(null)
        win.want("done closes the panel", hostsPanel.visible, false)

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

        // ---- Try (milestone 1): read a chapter without downloading it -----
        //
        // Three claims: it sits beside Download without replacing it, it
        // disappears once the chapter is on the tablet (Read is the better
        // offer at that point), and a book (theme.FileTheme) is never shown
        // it at all — a mutation that started offering it there is exactly
        // what PLAN's proof section asks this file to catch.
        //
        // Rows are found by chapter id, not by position: a ListView recycles
        // and reorders its delegates, so the same convention the source
        // chips use (sourceChip-<id>) is what makes "this row's button"
        // findable at all — see the objectName comments on tryButton and
        // downloadButton in ChapterList.qml.
        var findByPrefix = function (item, prefix, into) {
            if (!item)
                return into
            if (typeof item.objectName === "string" && item.objectName.indexOf(prefix) === 0)
                into.push(item)
            for (var i = 0; i < item.children.length; ++i)
                findByPrefix(item.children[i], prefix, into)
            return into
        }
        var visibleTries = function (screen) {
            var found = findByPrefix(screen, "tryButton-", [])
            var n = 0
            for (var i = 0; i < found.length; ++i)
                if (found[i].visible)
                    n++
            return n
        }
        win.want("an ordinary chapter list offers Try", visibleTries(chapterList) > 0, true)
        win.want("Download is still there beside it",
                 findByPrefix(chapterList, "downloadButton-", []).length > 0, true)

        // Tapping it names the chapter, and touches nothing about the
        // download machinery — no message, no state change on the row.
        win.tryAsks = 0
        var stateBefore = chaptersModel.get(1).downloadState
        win.findChild(chapterList, "tryArea-c1").clicked(null)
        win.want("tapping Try asks once", win.tryAsks, 1)
        win.want("naming the chapter the row was for", win.triedChapterId, "c1")
        win.want("Try leaves the download state untouched",
                 chaptersModel.get(1).downloadState, stateBefore)

        // Once a chapter is on the tablet, Try steps aside for Read; Download
        // stays exactly as reachable as it always was (it now reads "Read").
        chaptersModel.setProperty(2, "documentUuid", "doc-2")
        win.want("a downloaded chapter is not offered Try",
                 win.findChild(chapterList, "tryButton-c2").visible, false)
        win.want("but Download (now \"Read\") is still there",
                 win.findChild(chapterList, "downloadButton-c2").visible, true)
        chaptersModel.setProperty(2, "documentUuid", "")

        // Selection mode hides it exactly as it hides Delete: there is
        // nothing to preview several rows at once into.
        chapterList.enterSelection()
        win.want("selection mode hides Try", visibleTries(chapterList), 0)
        chapterList.leaveSelection()

        // A book's release renders through MuPDF the same as a saved book
        // does (backend/bookrender), so Try is offered on it too — the same
        // TryChapter as a manga row sends, answered with BookOpened rather
        // than TryReady when the source turns out to be one (books-contract
        // §C: "book rows now get Try/Download and Read/Delete like comics").
        win.want("a book offers Try too", visibleTries(bookChapterList) > 0, true)
        win.want("and Download remains reachable beside it",
                 findByPrefix(bookChapterList, "downloadButton-", []).length > 0, true)

        // ---- saved in Quire: [Delete][Read] instead of [Try][Download] ----
        //
        // Three claims: a saved row drops Try in favour of Read/Delete, it
        // does so whether or not the same chapter is also in the library
        // (independent copies, one row — PLAN's saved-in-Quire design), and
        // its own Delete asks a different question (deleteSaved) from the
        // library's.
        win.want("buttonLabel with no saved flag is unaffected",
                 chapterList.buttonLabel("", "", undefined), "Download")
        win.want("a saved chapter reads Read", chapterList.buttonLabel("", "", true), "Read")
        win.want("saved wins over an in-library chapter too",
                 chapterList.buttonLabel("done", "doc-1", true), "Read")

        win.readSavedAsks = 0
        chapterList.tapped("c3", "", "", true)
        win.want("tapping a saved row's button asks to read it once", win.readSavedAsks, 1)
        win.want("naming the chapter", win.readSavedChapterId, "c3")

        // deleteButton/deleteArea are the same objectName on every row (like
        // visibleDeletes() above), so a row-specific one is found by
        // filtering for the one row this makes visible, rather than by name
        // alone.
        var visibleDeleteButtons = function () {
            var found = win.findChildren(chapterList, "deleteButton", [])
            var vis = []
            for (var i = 0; i < found.length; ++i)
                if (found[i].visible)
                    vis.push(found[i])
            return vis
        }

        // No forceLayout here or below — the delegates already exist and
        // their visibility is a live binding on model.saved/documentUuid, so
        // a plain setProperty is enough (forceLayout is for when the model's
        // *count* changes, not one of its roles).
        chaptersModel.setProperty(3, "saved", true)
        win.want("the saved row offers no Try",
                 win.findChild(chapterList, "tryButton-c3").visible, false)
        win.want("its Delete is offered instead", visibleDeleteButtons().length, 1)
        win.want("and its button reads Read",
                 win.findChild(chapterList, "downloadButton-c3").children[0].text, "Read")

        // Also true with the same chapter in the library — saved still wins.
        chaptersModel.setProperty(3, "documentUuid", "doc-3")
        win.want("saved still offers exactly one Delete with a documentUuid too",
                 visibleDeleteButtons().length, 1)

        // Its own Delete asks deleteSaved, not the library's delete.
        win.deleteSavedAsks = 0
        win.findChild(visibleDeleteButtons()[0], "deleteArea").clicked(null)
        win.want("Delete on a saved row asks deleteSaved once", win.deleteSavedAsks, 1)
        win.want("naming the chapter, not a document",
                 win.deleteSavedAskedAbout, "c3")
        win.want("the strip opens with the deleteSaved kind",
                 chapterList.confirmingKind, "deleteSaved")

        chapterList.confirmingId = "c3"
        chapterList.confirmingMessage = "Delete Chapter 3 from Quire? It will need downloading again to read."
        win.want("the strip shows the Keep / Delete-for-good shape",
                 win.findChild(chapterList, "confirmDeleteButton").visible, true)
        win.want("not the volume Download all button",
                 win.findChild(chapterList, "confirmDownloadButton").visible, false)

        win.deleteSavedConfirms = 0
        win.findChild(chapterList, "confirmDeleteArea").clicked(null)
        win.want("confirming answers deleteSavedConfirmed, not deleteConfirmed",
                 win.deleteSavedConfirms, 1)
        win.want("naming the chapter", win.deleteSavedConfirmedAbout, "c3")
        win.want("and closes the strip",
                 win.findChild(chapterList, "confirmStrip").visible, false)

        chaptersModel.setProperty(3, "saved", false)
        chaptersModel.setProperty(3, "documentUuid", "")
        win.want("clearing saved returns the row to Try/Download",
                 win.findChild(chapterList, "tryButton-c3").visible, true)

        // ---- the reader itself: full screen, three tap zones, the overlay,
        // the honesty line and closing ---------------------------------
        //
        // Driven directly through the same API Main.qml drives it with
        // (begin/ready/pageArrived/countUpdated), rather than through a
        // socket — this is the reader's own behaviour, independent of the
        // backend that feeds it.
        tryReader.begin("src", "series", "c1", "Chapter 1")
        win.want("begin shows the fetching placeholder",
                 win.findChild(tryReader, "tryLoadingLabel").visible, true)
        win.want("and asks for page 1 straight away", win.tryPageWants.indexOf(0) >= 0, true)

        // The honesty line is seen before a single tap: it opens on its own,
        // on the first page, with the overlay shut.
        win.want("the opening notice shows unasked, on page 1",
                 win.findChild(tryReader, "tryOpeningNotice").visible, true)
        win.want("and the overlay itself is shut", tryReader.overlayVisible, false)

        var leftZone = win.findChild(tryReader, "tryLeftZone")
        var middleZone = win.findChild(tryReader, "tryMiddleZone")
        var rightZone = win.findChild(tryReader, "tryRightZone")

        // The three zones divide the screen left to right in that order,
        // and the middle is the widest single-tap target of the three by
        // design (see zoneFraction) — wide enough to reach without aiming.
        win.want("the left zone starts at the left edge", leftZone.x, 0)
        win.want("the middle zone picks up where the left zone ends",
                 middleZone.x, leftZone.width)
        win.want("the right zone picks up where the middle zone ends",
                 rightZone.x, middleZone.x + middleZone.width)
        win.want("the right zone reaches the right edge",
                 rightZone.x + rightZone.width, tryReader.width)
        win.want("the middle zone is the widest of the three",
                 middleZone.width > leftZone.width && middleZone.width > rightZone.width, true)

        // Left is dead on the first page — the same hard stop every paged
        // screen in ui/ already has, just answered by a tap zone.
        leftZone.clicked(null)
        win.want("a tap on the left does nothing on page 1", tryReader.index, 0)

        tryReader.ready({"sourceId": "src", "seriesId": "series", "chapterId": "c1",
                          "index": 0, "path": "/tmp/p0.jpg", "pageCount": 3, "complete": true})
        win.want("the first page is shown", win.findChild(tryReader, "tryPageImage").visible, true)
        win.want("not the fetching placeholder any more",
                 win.findChild(tryReader, "tryLoadingLabel").visible, false)
        win.want("previous is dead on the first page", tryReader.canGoBack, false)
        win.want("next is alive with more pages known", tryReader.canGoOn, true)

        // The middle zone toggles the overlay, both ways — it does not turn
        // a page.
        middleZone.clicked(null)
        win.want("a middle tap opens the overlay", tryReader.overlayVisible, true)
        win.want("the page did not turn", tryReader.index, 0)
        win.want("the overlay carries the title",
                 win.findChild(tryReader, "tryOverlayTitle").text, "Chapter 1")
        win.want("and the page number",
                 win.findChild(tryReader, "tryOverlayPageLabel").text.indexOf("Page 1") === 0, true)
        // Reachable a second time from inside the overlay, for a reader who
        // skipped or has long since scrolled past the opening band.
        win.want("and the honesty line, every time it opens",
                 win.findChild(tryReader, "tryOverlayNotice").visible, true)
        // The opening band itself steps aside once the overlay is up, so
        // the two are never drawn over one another.
        win.want("the opening band yields to the overlay",
                 win.findChild(tryReader, "tryOpeningNotice").visible, false)

        // ---- the reader's two library actions ------------------------------
        //
        // Both default to offered in Try mode on a chapter that is neither
        // private nor already in the library — the ordinary case.
        win.want("Send to library is offered by default in Try mode",
                 win.findChild(tryReader, "trySendToLibraryButton").visible, true)
        win.want("so is Save in Quire", win.findChild(tryReader, "trySaveInQuireButton").visible, true)

        win.sendToLibraryAsks = 0
        win.findChild(tryReader, "trySendToLibraryArea").clicked(null)
        win.want("tapping it reports the tap once", win.sendToLibraryAsks, 1)

        win.saveInQuireAsks = 0
        win.findChild(tryReader, "trySaveInQuireArea").clicked(null)
        win.want("and so does Save in Quire", win.saveInQuireAsks, 1)

        // Never for a private source.
        tryReader.sourcePrivate = true
        win.want("a private source drops Send to library",
                 win.findChild(tryReader, "trySendToLibraryButton").visible, false)
        win.want("but Save in Quire is untouched by privacy",
                 win.findChild(tryReader, "trySaveInQuireButton").visible, true)
        tryReader.sourcePrivate = false

        // Never once the chapter is already in the library.
        tryReader.chapterInLibrary = true
        win.want("an already-library chapter drops Send to library",
                 win.findChild(tryReader, "trySendToLibraryButton").visible, false)
        tryReader.chapterInLibrary = false

        // Page-turning stands down while the overlay owns the screen —
        // tapping where the left/right zones would be instead falls through
        // to the overlay (which is a full-screen dismiss area under its own
        // bars) and closes it, exactly like tapping the middle again.
        win.want("the tap zones are disabled under the overlay",
                 win.findChild(tryReader, "tryTapZones").enabled, false)
        // A real tap in the middle of the screen while the overlay is up
        // lands on the overlay's own dismiss area, not the (disabled)
        // middle zone underneath it — this is that area, not middleZone
        // again, so the test exercises the actual hit-test path.
        win.findChild(tryReader, "tryOverlayDismissArea").clicked(null)
        win.want("tapping the page again closes the overlay", tryReader.overlayVisible, false)
        win.want("still without having turned a page", tryReader.index, 0)

        // The reader is not stuck once the overlay shuts again.
        win.want("the tap zones are back", win.findChild(tryReader, "tryTapZones").enabled, true)

        // Turning to a page not fetched yet degrades to the loading label —
        // never a blank page standing in for content, and never a freeze:
        // the zones stay fully interactive throughout.
        rightZone.clicked(null)
        win.want("the reader moved to page 2", tryReader.index, 1)
        win.want("which is not ready yet, so it shows fetching",
                 win.findChild(tryReader, "tryLoadingLabel").visible, true)
        win.want("and asked the host for it, exactly once",
                 win.tryPageWants.filter(function (i) { return i === 1 }).length, 1)
        win.want("the opening band does not show past page 1",
                 win.findChild(tryReader, "tryOpeningNotice").visible, false)

        tryReader.pageArrived({"sourceId": "src", "seriesId": "series", "chapterId": "c1",
                               "index": 1, "path": "/tmp/p1.jpg"})
        win.want("page 2 shows once it arrives",
                 win.findChild(tryReader, "tryPageImage").visible, true)

        rightZone.clicked(null)
        win.want("page 3 (the last known page)", tryReader.index, 2)
        win.want("next is dead at the last known page", tryReader.canGoOn, false)
        rightZone.clicked(null)
        win.want("a dead right zone does not run past the end", tryReader.index, 2)

        leftZone.clicked(null)
        leftZone.clicked(null)
        win.want("left turns all the way back to the first page", tryReader.index, 0)
        leftZone.clicked(null)
        win.want("a dead left zone does not run past the start", tryReader.index, 0)

        // Closing always tells the host, which is what ends the session on
        // the backend and is the entire cleanup Try needs — nothing here
        // decides that on its own, it only ever reports the tap. Reached
        // through the overlay's own Back, since that is the only Back this
        // full-screen reader draws.
        win.tryCloses = 0
        middleZone.clicked(null)
        win.want("the overlay opens for the way out", tryReader.overlayVisible, true)
        win.findChild(tryReader, "tryBackArea").clicked(null)
        win.want("closing the reader is reported once", win.tryCloses, 1)

        // ---- saved mode: every page known up front, no honesty line -------
        //
        // Unlike Try, openSaved arrives with the whole chapter already on
        // disk and the position to open at — no "Fetching…" placeholder is
        // possible and none of the "nothing is saved here" wording applies.
        tryReader.openSaved({"sourceId": "src", "seriesId": "series", "chapterId": "c9",
                             "seriesTitle": "The Lantern Keeper", "chapterTitle": "Chapter 9",
                             "pages": ["/tmp/s0.jpg", "/tmp/s1.jpg", "/tmp/s2.jpg"],
                             "position": 1})
        win.want("saved mode opens at the stored position", tryReader.index, 1)
        win.want("every page is already known",
                 win.findChild(tryReader, "tryPageImage").visible, true)
        win.want("so the fetching placeholder never shows",
                 win.findChild(tryReader, "tryLoadingLabel").visible, false)
        win.want("nothing to be honest about on the opening page",
                 win.findChild(tryReader, "tryOpeningNotice").visible, false)
        win.findChild(tryReader, "tryMiddleZone").clicked(null)
        win.want("nor in the overlay",
                 win.findChild(tryReader, "tryOverlayNotice").visible, false)
        // Save in Quire is Try-only — a saved chapter is already saved —
        // but Send to library still applies, on the same terms as Try's.
        win.want("saved mode drops Save in Quire",
                 win.findChild(tryReader, "trySaveInQuireButton").visible, false)
        win.want("but still offers Send to library by default",
                 win.findChild(tryReader, "trySendToLibraryButton").visible, true)
        win.findChild(tryReader, "tryMiddleZone").clicked(null)

        // ---- remembering the page: debounced on a turn, flushed on close ---
        //
        // A page turn does not send a position straight away — flicking
        // through a chapter would otherwise fire a message every tap — but
        // it does start the wait, and leaving the reader (flushSavePosition,
        // Main.qml's leaveReader) sends immediately rather than losing it.
        win.savePositionWants = []
        win.findChild(tryReader, "tryRightZone").clicked(null)
        win.want("turning a page schedules a save rather than sending one",
                 win.savePositionWants.length, 0)
        win.want("the reader did turn the page", tryReader.index, 2)

        tryReader.flushSavePosition()
        win.want("leaving flushes it immediately", win.savePositionWants.join(","), "2")

        // Try mode never sends one, scheduled or flushed: there is no
        // position to keep for a session that ends when the reader closes.
        win.savePositionWants = []
        tryReader.begin("src", "series", "c1", "Chapter 1")
        tryReader.ready({"sourceId": "src", "seriesId": "series", "chapterId": "c1",
                         "index": 0, "path": "/tmp/p0.jpg", "pageCount": 2, "complete": true})
        tryReader.index = 1
        tryReader.flushSavePosition()
        win.want("Try mode never asks to remember a page", win.savePositionWants.length, 0)

        // A position past the last page (a stale or malformed one) is
        // clamped rather than trusted — see openSaved's own comment.
        tryReader.openSaved({"sourceId": "src", "seriesId": "series", "chapterId": "c9",
                             "chapterTitle": "Chapter 9", "pages": ["/tmp/s0.jpg"],
                             "position": 99})
        win.want("a position past the end is clamped to the last page",
                 tryReader.index, 0)

        // ---- book mode: MuPDF-rendered pages, Contents and the Aa panel ----
        //
        // Driven the same way as try/saved above — directly, through the
        // reader's own API — since this is the same screen and the same tap
        // zones, just a third mode of it.
        //
        // bookSettingsFields mirrors the shape backend/bookrender.SettingsFields
        // sends — key, label, help, and either choices or steps — since this
        // harness drives the reader's own message handlers directly rather
        // than a real backend.
        var bookSettingsFields = [
            {"key": "font", "label": "Font",
             "help": "The typeface the text is set in.",
             "choices": [{"id": "book", "label": "The book's own"},
                         {"id": "garamond", "label": "EB Garamond"},
                         {"id": "noto", "label": "Noto Sans"}]},
            {"key": "size", "label": "Text size",
             "help": "How large the text is.",
             "steps": [{"id": 1, "label": "9 pt"}, {"id": 2, "label": "10 pt"},
                       {"id": 3, "label": "11 pt"}, {"id": 4, "label": "12 pt"},
                       {"id": 5, "label": "13 pt"}, {"id": 6, "label": "14 pt"},
                       {"id": 7, "label": "16 pt"}, {"id": 8, "label": "18 pt"},
                       {"id": 9, "label": "20 pt"}]},
            {"key": "margins", "label": "Page margins",
             "help": "The blank space between the text and the edges of the screen.",
             "choices": [{"id": "narrow", "label": "Narrow"},
                         {"id": "normal", "label": "Normal"},
                         {"id": "wide", "label": "Wide"}]},
            {"key": "spacing", "label": "Line spacing",
             "help": "The space between lines of text.",
             "choices": [{"id": "book", "label": "The book's own"},
                         {"id": "normal", "label": "Normal"},
                         {"id": "relaxed", "label": "Relaxed"}]},
            {"key": "align", "label": "Alignment",
             "help": "Left-aligned avoids ragged word gaps.",
             "choices": [{"id": "book", "label": "The book's own"},
                         {"id": "left", "label": "Left-aligned"}]}
        ]
        var bookSettingsNote = "These apply to every book you read in Quire."

        win.tryPageWants = []
        tryReader.openBook({
            "sourceId": "src", "seriesId": "series", "chapterId": "b1",
            "title": "Dune Messiah", "mode": "try", "fixedLayout": false,
            "pageCount": 20, "page": 0,
            "toc": [{"title": "Part One", "page": 0, "level": 0},
                    {"title": "Part Two", "page": 10, "level": 0}],
            "settings": {"font": "book", "size": 4, "margins": "normal",
                         "spacing": "book", "align": "book"},
            "settingsNote": bookSettingsNote,
            "settingsFields": bookSettingsFields})
        win.want("opening a book sets the reader's mode", tryReader.mode, "book")
        win.want("carrying which kind of session it is", tryReader.bookMode, "try")
        win.want("asking for the page on screen, the same way Try's page 0 is",
                 win.tryPageWants.join(","), "0")
        win.want("nothing to show until it arrives",
                 win.findChild(tryReader, "tryPageImage").visible, false)

        tryReader.bookStatus({"sourceId": "src", "seriesId": "series", "chapterId": "b1",
                              "message": "Laying out the book…"})
        win.want("a BookStatus sentence shows on the opening screen",
                 win.findChild(tryReader, "tryBookStatusLabel").text,
                 "Laying out the book…")
        win.want("and it is visible there", win.findChild(tryReader, "tryBookStatusLabel").visible, true)

        tryReader.pageArrived({"sourceId": "src", "seriesId": "series", "chapterId": "b1",
                               "index": 0, "path": "/tmp/b0.png"})
        win.want("the page reuses Try's own page-arrived handling",
                 win.findChild(tryReader, "tryPageImage").visible, true)

        var bookMiddleZone = win.findChild(tryReader, "tryMiddleZone")
        bookMiddleZone.clicked(null)
        win.want("the overlay opens the same way", tryReader.overlayVisible, true)
        win.want("naming the chapter under the current page",
                 win.findChild(tryReader, "tryOverlayChapterTitle").text, "Part One")
        win.want("Contents is offered", win.findChild(tryReader, "tryContentsButton").visible, true)
        win.want("so is Aa", win.findChild(tryReader, "tryAaButton").visible, true)
        // A book's own session is never the Try honesty line's to draw — that
        // is a comic's "Preview only" sentence, not a book's.
        win.want("book mode carries none of Try's own honesty line",
                 win.findChild(tryReader, "tryOverlayNotice").visible, false)

        win.findChild(tryReader, "tryContentsArea").clicked(null)
        win.want("Contents opens its own panel", tryReader.contentsVisible, true)
        var tocEntries = win.findChildren(tryReader, "tryContentsList", [])[0]
        win.want("every toc entry is drawn",
                 win.findChildren(tocEntries, "tryContentsEntryArea-0", []).length, 1)
        win.findChild(tryReader, "tryContentsEntryArea-1").clicked(null)
        win.want("tapping an entry jumps to its page", tryReader.index, 10)
        win.want("and closes the panel", tryReader.contentsVisible, false)
        win.want("and the overlay with it", tryReader.overlayVisible, false)

        // Fetching the new page, the same as any other turn.
        win.want("the jump asked for its page", win.tryPageWants.indexOf(10) >= 0, true)
        tryReader.pageArrived({"sourceId": "src", "seriesId": "series", "chapterId": "b1",
                               "index": 10, "path": "/tmp/b10.png"})

        bookMiddleZone.clicked(null)
        win.findChild(tryReader, "tryAaArea").clicked(null)
        win.want("Aa opens the settings panel", tryReader.settingsVisible, true)
        win.want("the panel's own note is the backend's sentence",
                 win.findChild(tryReader, "trySettingsNote").text, bookSettingsNote)
        win.want("offering the backend's own font choices",
                 win.findChild(tryReader, "trySettingsFont-garamond") !== null, true)
        win.want("labelled the backend's way, not this file's",
                 win.wordsOn(win.findChild(tryReader, "trySettingsFont-garamond")),
                 "EB Garamond")
        win.want("every field's own label renders",
                 win.findChild(tryReader, "trySettingsFieldLabel-margins").text, "Page margins")
        win.want("and its help sentence too",
                 win.findChild(tryReader, "trySettingsFieldHelp-margins").text,
                 "The blank space between the text and the edges of the screen.")
        win.want("the size field's current step is labelled by the backend, not composed here",
                 win.findChild(tryReader, "trySettingsSizeLabel").text, "12 pt")

        win.settingsChangeAsks = []
        // The MouseArea inside the font button carries no name of its own
        // (unlike the button it is one of several in); found by what a
        // MouseArea *is* — the presence of onClicked — rather than a name.
        var fontButton = win.findChild(tryReader, "trySettingsFont-garamond")
        var fontArea = null
        for (var fc = 0; fc < fontButton.children.length; ++fc)
            if (fontButton.children[fc].clicked !== undefined)
                fontArea = fontButton.children[fc]
        fontArea.clicked(null)
        win.want("tapping a font asks for it once", win.settingsChangeAsks.length, 1)
        win.want("naming the choice", win.settingsChangeAsks[0].font, "garamond")
        win.want("carrying the rest of the settings along unchanged",
                 win.settingsChangeAsks[0].size, 4)

        win.want("size can go up from the middle", win.findChild(tryReader, "trySettingsSizeUp").enabled, true)
        win.want("and down too", win.findChild(tryReader, "trySettingsSizeDown").enabled, true)
        tryReader.settings = {"font": "book", "size": 9, "margins": "normal",
                              "spacing": "book", "align": "book"}
        win.want("size is dead at the top", win.findChild(tryReader, "trySettingsSizeUp").enabled, false)
        win.want("but still live going down", win.findChild(tryReader, "trySettingsSizeDown").enabled, true)
        tryReader.settings = {"font": "book", "size": 1, "margins": "normal",
                              "spacing": "book", "align": "book"}
        win.want("and dead at the bottom the other way",
                 win.findChild(tryReader, "trySettingsSizeDown").enabled, false)

        win.settingsChangeAsks = []
        var marginsButton = win.findChild(tryReader, "trySettingsMargins-wide")
        var marginsArea = null
        for (var mc = 0; mc < marginsButton.children.length; ++mc)
            if (marginsButton.children[mc].clicked !== undefined)
                marginsArea = marginsButton.children[mc]
        marginsArea.clicked(null)
        win.want("margins ask by their schema value", win.settingsChangeAsks[0].margins, "wide")

        // BookRelaid answers a settings change: new count, new page, new toc,
        // and every cached page path is dropped — a stale page under the old
        // layout would otherwise show under the new one.
        tryReader.settingsVisible = false
        win.tryPageWants = []
        tryReader.relaid({"sourceId": "src", "seriesId": "series", "chapterId": "b1",
                          "pageCount": 24, "page": 3,
                          "toc": [{"title": "Part One", "page": 0, "level": 0}],
                          "settings": {"font": "garamond", "size": 6, "margins": "wide",
                                       "spacing": "book", "align": "book"},
                          "settingsNote": bookSettingsNote,
                          "settingsFields": bookSettingsFields})
        win.want("BookRelaid moves to the page it names", tryReader.index, 3)
        win.want("with the new count", tryReader.pageCount, 24)
        win.want("and asks for that page fresh, the cache having been dropped",
                 win.tryPageWants.join(","), "3")
        win.want("nothing left showing from before the relayout",
                 win.findChild(tryReader, "tryPageImage").visible, false)

        tryReader.settingsVisible = true
        win.want("the current size label updates after BookRelaid",
                 win.findChild(tryReader, "trySettingsSizeLabel").text, "14 pt")
        tryReader.settingsVisible = false

        // Fixed-layout books (PDF/XPS/CBZ) have nothing the Aa panel would
        // control, so the button that opens it is not offered at all —
        // Contents is what such a book keeps.
        tryReader.openBook({
            "sourceId": "src", "seriesId": "series", "chapterId": "b2",
            "title": "A Scanned Comic", "mode": "saved", "fixedLayout": true,
            "pageCount": 5, "page": 0, "toc": [],
            "settings": {"font": "book", "size": 4, "margins": "normal",
                         "spacing": "book", "align": "book"},
            "settingsNote": "", "settingsFields": []})
        bookMiddleZone.clicked(null)
        win.want("a fixed-layout book hides Aa entirely",
                 win.findChild(tryReader, "tryAaButton").visible, false)
        win.want("but keeps Contents", win.findChild(tryReader, "tryContentsButton").visible, true)

        // Position is tracked for a saved book, the same way a saved comic's
        // is — debounced on a turn (SavePosition (93) works for a book too),
        // never for a Try session of either kind.
        tryReader.pageArrived({"sourceId": "src", "seriesId": "series", "chapterId": "b2",
                               "index": 0, "path": "/tmp/c0.png"})
        win.savePositionWants = []
        var bookRightZone = win.findChild(tryReader, "tryRightZone")
        bookRightZone.clicked(null)
        win.want("a saved book schedules a save rather than sending one",
                 win.savePositionWants.length, 0)
        tryReader.flushSavePosition()
        win.want("and flushes it on leaving", win.savePositionWants.join(","), "1")

        tryReader.bookMode = "try"
        win.savePositionWants = []
        tryReader.index = 0
        tryReader.flushSavePosition()
        win.want("a book Try session tracks no position at all",
                 win.savePositionWants.length, 0)

        // The volume view offers it on the same terms as a downloaded
        // chapter — except for a saved row, which offers Read only (see
        // below): model.chapterId on a volume row is only its first chapter,
        // so a Delete button there would delete one chapter while claiming
        // to delete the whole volume.
        //
        // The volumes are put back first: the block above this one emptied the
        // model to prove a series that loses its volumes loses the switch.
        volumesModel.append({"chapterId": "c0", "title": "Volume 1",
                             "label": "1",
                             "detail": "7 chapters, Chapter 1 to Chapter 7", "chapterCount": 7,
                             "downloadState": "", "downloadMessage": "", "documentUuid": "",
                             "saved": false,
                             // Both roles have to exist from this first
                             // append, or a later setProperty onto them is
                             // silently dropped (ListModel's own rule, see
                             // ui/Main.qml's fillDownloaded comment).
                             "savedCount": 0, "chapterIdsJson": "[]"})
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

        // A saved volume row — even one also on the tablet — offers Delete
        // for its *saved chapters*, not the library document (round 2): the
        // library-document Delete (volumeDeleteButton) is offered only while
        // nothing is saved, and the saved-chapters one
        // (volumeDeleteSavedButton) takes over the moment savedCount > 0.
        // The library document itself stays reachable from the chapter rows
        // in the meantime.
        // Re-appended rather than mutated in place: ListModel.setProperty
        // does not reliably update an array-valued role (chapterIds) once a
        // row already exists — only a fresh append gives it the array this
        // step needs. Scalar roles (documentUuid, saved, savedCount) do not
        // have that limitation and are set the ordinary way everywhere else
        // in this file; this row is the one place chapterIds itself has to
        // change after creation.
        volumesModel.remove(0)
        volumesModel.append({"chapterId": "c0", "title": "Volume 1", "label": "1",
                             "detail": "7 chapters, Chapter 1 to Chapter 7", "chapterCount": 7,
                             "downloadState": "", "downloadMessage": "", "documentUuid": "doc-vol",
                             "saved": true, "savedCount": 7,
                             "chapterIdsJson": JSON.stringify(["c0", "c1", "c2", "c3", "c4", "c5", "c6"])})
        // The remove() above took volumeModel.count briefly to zero, which
        // trips hasVolumesChanged's own safety net ("a series that loses its
        // volumes must not leave the screen looking at nothing") and drops
        // the view back to "chapters" — exactly as it should for a real
        // refresh that loses its volumes, but not what this step means, so
        // it is put back explicitly.
        chapterList.showView("volumes")
        win.findChild(chapterList, "volumeRows").forceLayout()
        win.want("a saved volume row offers no library Delete, even when also downloaded",
                 visibleVolumeDeletes(), 0)
        var visibleVolumeSavedDeletes = function () {
            var found = win.findChildren(chapterList, "volumeDeleteSavedButton", [])
            var n = 0
            for (var i = 0; i < found.length; ++i)
                if (found[i].visible)
                    n++
            return n
        }
        win.want("but offers Delete for its saved chapters instead",
                 visibleVolumeSavedDeletes(), 1)
        win.want("its button reads Read",
                 win.findChild(chapterList, "volumeButton").children[0].text, "Read")

        win.deleteVolumeAsks = 0
        win.findChild(chapterList, "volumeDeleteSavedArea").clicked(null)
        win.want("the saved-chapters delete asks once", win.deleteVolumeAsks, 1)
        win.want("naming the volume's own chapters",
                 win.deleteVolumeAskedAbout.join(","), "c0,c1,c2,c3,c4,c5,c6")

        var readSavedAsksBefore = win.readSavedAsks
        win.findChild(chapterList, "volumeArea").clicked(null)
        win.want("tapping the volume button still reads the saved chapter, not the document",
                 win.readSavedAsks, readSavedAsksBefore + 1)
        win.want("naming the volume's first chapter", win.readSavedChapterId, "c0")

        // Reset the same way: chapterIds cannot be cleared by setProperty
        // either.
        volumesModel.remove(0)
        volumesModel.append({"chapterId": "c0", "title": "Volume 1", "label": "1",
                             "detail": "7 chapters, Chapter 1 to Chapter 7", "chapterCount": 7,
                             "downloadState": "", "downloadMessage": "", "documentUuid": "",
                             "saved": false, "savedCount": 0, "chapterIdsJson": "[]"})
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
                                 "downloadState": "", "downloadMessage": "", "documentUuid": "",
                                 "saved": false})
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
                parentOf: function (id) {
                    if (o.parentOfThrows) throw new Error("gone")
                    return parents[id] === undefined ? "" : parents[id]
                },
                createFolder: function (parent, name) {
                    this.log.push("create(" + parent + "," + name + ")")
                    if (o.createThrows) throw new Error("no")
                    if (o.createReturnsNothing) return ""
                    var id = "made-" + (++made)
                    // The silent failure the device really has: an unusable
                    // parent is ignored and the folder lands at the root.
                    // createLandsAt says exactly where instead, for the case
                    // where "ignored" and "root" are the same value and so
                    // cannot stand for each other — asking for a folder under
                    // ROOT itself.
                    if (o.createLandsAt !== undefined)
                        parents[id] = o.createLandsAt
                    else
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

        // Creation refused outright. This is what the owner's device hit on
        // 3.28: createFolder threw, and until now create()'s catch discarded
        // the reason — the backend log had only the generic fallback and
        // nobody could tell a throw from a folder landed at the wrong parent.
        dev = fakeDevice({ parents: { "doc-1": "comics" }, createThrows: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a create that throws moves nothing", res.moved.length, 0)
        win.want("and leaves the document where it was", dev.parents["doc-1"], "comics")
        win.want("and the reason names what the device threw",
                 res.detail.indexOf("threw Error: no") >= 0, true)
        win.want("not the generic fallback",
                 res.detail.indexOf("left where it is") >= 0, false)

        // The read-back after a create can itself throw — a second silent
        // catch this file used to have. The thrown reason must win over the
        // generic "left where it is" fallback, the same as a create() that
        // throws outright.
        dev = fakeDevice({ parents: { "doc-1": "comics" }, parentOfThrows: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a parent read-back that throws creates nothing usable", res.moved.length, 0)
        win.want("and the reason names what the read-back threw",
                 res.detail.indexOf("threw Error: gone") >= 0, true)
        win.want("not the generic fallback either",
                 res.detail.indexOf("left where it is") >= 0, false)

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

        // ---- the one-level Books folder (no per-title subfolder) -----------
        //
        // A book never gets a per-title subfolder, so there is no createComics
        // step and no second create: Books is made straight under ROOT — the
        // parent read-back check is the same create() used for every other
        // folder, exercised here at the root rather than under Comics.
        dev = fakeDevice({ parents: { "doc-1": "" } })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "", folderName: "Books"})
        win.want("Books is created straight under the root", dev.log[0], "create(,Books)")
        win.want("with no second create for a per-title folder", dev.log.length, 2)
        win.want("and the document lands in Books", dev.parents["doc-1"], "made-1")

        // The same silent failure as the series-folder case, but at the root:
        // a Books folder the device put somewhere else must be refused, and
        // nothing may be moved into it. Landed at "elsewhere" rather than at
        // "" — the root and "ignored" would otherwise be the same value and
        // the check would pass by accident.
        dev = fakeDevice({ parents: { "doc-1": "" }, createLandsAt: "elsewhere" })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "", folderName: "Books"})
        win.want("a Books folder at the wrong parent is not used", res.moved.length, 0)
        win.want("the document stays where it was", dev.parents["doc-1"], "")
        win.want("and the reason says where it landed",
                 res.detail.indexOf("rather than") >= 0, true)

        // createFolder returning "" without throwing and without landing at
        // the wrong parent — the shape the owner's device actually hit on
        // 3.28.0.172: createCollection ran, the folder was genuinely made,
        // but its id could not be read back. The old generic wording
        // ("could not create ...; left where it is") said the opposite of
        // what happened and is why an orphan folder was left behind on every
        // retry. This is the wording fix, not a claim that createFolder
        // itself changed — see ui/ReaderHandoff.qml for the device side,
        // which this harness cannot load (it imports xofm.libs.library).
        dev = fakeDevice({ parents: { "doc-1": "comics" }, createReturnsNothing: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "comics", folderName: "Wandance"})
        win.want("a bare empty return moves nothing", res.moved.length, 0)
        win.want("the document stays in Comics", dev.parents["doc-1"], "comics")
        win.want("and the reason says the folder was created",
                 res.detail.indexOf("was created but its id could not be read") >= 0, true)
        win.want("not the old could-not-create wording",
                 res.detail.indexOf("could not create") >= 0, false)
        win.want("not the old left-where-it-is wording",
                 res.detail.indexOf("left where it is") >= 0, false)

        // The same wording applies to the Comics folder itself, since both
        // go through the same create() and the same fallback message.
        dev = fakeDevice({ parents: { "doc-1": "" }, createReturnsNothing: true })
        res = Sorting.sortDocuments(dev, {
            documentUuids: ["doc-1"], folderId: "", createUnder: "", folderName: "Wandance",
            createComics: true, comicsName: "Comics"})
        win.want("Comics returning no id says the same thing",
                 res.detail.indexOf("was created but its id could not be read") >= 0, true)

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

        // extractId: the defensive read of whatever a create call hands
        // back. Not exercised through sortDocuments above, because by the
        // time a fake device's createFolder returns, the value is already a
        // string — this is the boundary ReaderHandoff.qml sits on, between
        // the device's raw answer and the string create() requires.
        win.want("a bare string passes through", Sorting.extractId("uuid-1"), "uuid-1")
        win.want("an object's id member is taken", Sorting.extractId({ id: "uuid-2" }), "uuid-2")
        // The wrapper convention this app already relies on elsewhere
        // (entryIds/entryId): an id that is itself an object whose
        // toString() is the uuid.
        win.want("a wrapped id is stringified",
                 Sorting.extractId({ id: { toString: function () { return "uuid-3" } } }),
                 "uuid-3")
        // The case mutation testing is aimed at: an object with no `id` at
        // all must not fall through to String(value), which would produce
        // the plausible-looking-but-wrong "[object Object]".
        win.want("an object with no id member is unusable", Sorting.extractId({}), "")
        win.want("an object with a null id is unusable", Sorting.extractId({ id: null }), "")
        win.want("undefined is unusable", Sorting.extractId(undefined), "")
        win.want("null is unusable", Sorting.extractId(null), "")

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
            for (var i = 0; i < rows.length; ++i) {
                // Every role Main.qml's fillDownloaded fills, on every row. A
                // ListModel fixes its roles on the first append and silently
                // drops keys added later, so a fixture that left the cover off
                // the first row would take the cover off every row after it —
                // and the tiles would then be asserted against a model shape
                // the app never produces.
                var r = rows[i]
                downloadedModel.append({
                    "sourceId": r.sourceId, "sourceName": r.sourceName,
                    "seriesId": r.seriesId, "title": r.title, "detail": r.detail,
                    "openable": r.openable, "note": r.note,
                    "coverUrl": r.coverUrl ? r.coverUrl : "",
                    "coverPath": r.coverPath ? r.coverPath : "",
                    "latestUuid": r.latestUuid ? r.latestUuid : "",
                    // A chapter saved in Quire, newest first — see
                    // "prefers a saved chapter over a library one" below.
                    "latestSavedChapterId": r.latestSavedChapterId ? r.latestSavedChapterId : "",
                    // Both of the kind's roles, on every row, for the reason
                    // above: a fixture that left them off the first row would
                    // take them off every row after it, and the mark would then
                    // be asserted against a model shape the app never produces.
                    "kind": Kinds.of(r),
                    "badge": Kinds.mark(r),
                    "watched": r.watched ? true : false,
                    // PLAN §12.9's continue target, flattened the way
                    // Main.qml's fillDownloaded flattens it.
                    "continueKind": r.continueKind ? r.continueKind : "",
                    "continueChapterId": r.continueChapterId ? r.continueChapterId : "",
                    "continueDocumentUuid": r.continueDocumentUuid ? r.continueDocumentUuid : ""})
            }
            win.findChild(downloadedList, "downloadedRows").forceLayout()
            win.findChild(downloadedList, "coverTiles").forceLayout()
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

        // ---- the layout each screen is remembered in (PLAN §7.1 type 75) ---
        //
        // Three screens remember a layout. The value lives in the backend's
        // store and comes back on the **Pong status**, never in a reply of its
        // own, so the screen draws what the store says rather than what the
        // switch hoped. Views.js is the frontend half of that contract.

        // Nothing has arrived yet, and the screen still has a layout to draw.
        // This is the case that would otherwise be a blank screen on the way
        // in: the frontend pings on startup, but the user can be standing on
        // Downloaded before the answer lands.
        win.want("no status at all means grid", Views.fromStatus(null, "downloaded"), "grid")
        win.want("a status with no views means grid", Views.fromStatus({}, "browse"), "grid")

        var stored = {"views": {"search": "list", "downloaded": "grid", "watching": "list"}}
        win.want("the search screen reads its own setting",
                 Views.fromStatus(stored, "browse"), "list")
        win.want("the downloaded screen reads its own",
                 Views.fromStatus(stored, "downloaded"), "grid")
        win.want("the watched screen reads its own",
                 Views.fromStatus(stored, "watching"), "list")

        // "browse" is "search" on the wire: one setting covers a source's
        // catalogue and its search results, which are the same tiles.
        win.want("browse is search on the wire", Views.wireName("browse"), "search")
        win.want("a screen with nothing to remember has no wire name",
                 Views.wireName("settings"), "")
        win.want("so the switch is not offered there", Views.remembers("settings"), false)
        win.want("but it is on watching", Views.remembers("watching"), true)

        // A layout this Quire cannot draw -- a hand-edited file, or one written
        // by a newer Quire -- is drawn the usual way rather than not at all.
        win.want("a layout Quire cannot draw is drawn as grid",
                 Views.fromStatus({"views": {"watching": "carousel"}}, "watching"), "grid")

        // ---- the switch itself ---------------------------------------------

        var gridHalf = win.findChild(viewToggle, "viewToggleGrid")
        var listHalf = win.findChild(viewToggle, "viewToggleList")
        var gridArea = win.findChild(viewToggle, "viewToggleGridArea")
        var listArea = win.findChild(viewToggle, "viewToggleListArea")

        win.want("the switch opens on grid", viewToggle.view, "grid")
        win.want("both layouts are named on it, always",
                 win.visibleKeyLabels(viewToggle).join(","), "Grid,List")
        win.want("the layout showing is the filled half", gridHalf.current, true)
        win.want("and the other half is not", listHalf.current, false)

        // `enabled` is the assertion, not a synthesised tap: emitting clicked()
        // invokes the handler directly and bypasses `enabled` entirely, so
        // "tapping it does nothing" written that way passes for reasons that
        // have nothing to do with the device.
        win.want("the half you are already in is inert", gridArea.enabled, false)
        win.want("and the other half is live", listArea.enabled, true)

        win.viewAsks = 0
        listArea.clicked(null)
        win.want("tapping the other half asks once", win.viewAsks, 1)
        win.want("for the layout it names", win.viewAskedFor, "list")

        // It flips nothing itself. Until a status says otherwise the switch
        // still shows the layout the store holds, which is what stops a write
        // that failed leaving the control and the screen disagreeing.
        win.want("the switch did not flip itself", viewToggle.view, "grid")
        win.want("and the filled half did not move", gridHalf.current, true)

        // The status arriving is what moves it.
        viewToggle.view = Views.fromStatus(stored, "browse")
        win.want("the stored layout is what fills a half", listHalf.current, true)
        win.want("the half you left becomes live", gridArea.enabled, true)
        win.want("and the one you are in goes inert", listArea.enabled, false)
        gridArea.clicked(null)
        win.want("asking to go back names grid, not “the other one”",
                 win.viewAskedFor, "grid")
        win.want("which is two asks in all", win.viewAsks, 2)

        // ---- search results, as tiles and as rows --------------------------

        // `visible` is effective visibility in QML, so a screen the harness
        // hid earlier reports every child of it as hidden too.
        seriesGrid.visible = true
        var searchTiles = win.findChild(seriesGrid, "seriesTiles")
        var searchRows = win.findChild(seriesGrid, "seriesRows")
        win.want("search opens as tiles before any status", seriesGrid.view, "grid")
        win.want("the tiles are what is on screen", searchTiles.visible, true)
        win.want("and the rows are not", searchRows.visible, false)

        var tilePage = seriesGrid.pageSize
        win.want("a page of tiles is whole rows of three", tilePage % 3, 0)

        seriesGrid.view = "list"
        win.want("the switch swaps the layout", searchRows.visible, true)
        win.want("and puts the tiles away", searchTiles.visible, false)
        win.want("a page of rows is one column", seriesGrid.columns, 1)
        win.want("and holds more results than a page of tiles",
                 seriesGrid.pageSize > tilePage, true)
        // Switching layout changes what a page *is*, so the page is refetched
        // rather than re-sliced: the backend owns the listing and this screen
        // has never held the extra rows a taller page needs.
        win.want("switching layout asks for the page again", seriesGrid.pendingPage, 1)
        seriesGrid.busy = false
        seriesGrid.pendingPage = 0

        // Covers are drawn in the rows now, so covers are asked for: this used
        // to assert that a list layout asked for *nothing*, which was right
        // while a row was two lines of text and is the wrong intent since the
        // row grew a thumbnail. It is the same already-downscaled cache entry
        // the tiles ask for -- no second size, no second request.
        win.want("switching to rows asks for the covers the rows draw",
                 win.seriesCovers.length, seriesGrid.rowCount)
        win.want("and asks for the file the tiles ask for, not a second one",
                 String(win.seriesCovers[0].url), "https://example.invalid/c.jpg")

        searchRows.forceLayout()

        // The thumbnail itself: on every row, inside the row's height, in the
        // shape the cache writes. Every number comes from Style.js.
        var searchThumbs = win.findChildren(searchRows, "rowCover", [])
        win.want("every row on the page carries a cover", searchThumbs.length > 1, true)
        win.want("the thumbnail is as wide as the token says",
                 searchThumbs[0].width, Style.thumbWidth)
        win.want("and as tall", searchThumbs[0].height, Style.thumbHeight)
        // The shape the cache writes (300x450), so nothing has to crop.
        win.want("which is the shape the cover cache writes", searchThumbs[0].width,
                 Math.round(searchThumbs[0].height / Style.coverRatio))

        // The arithmetic the thumbnail must not *decide*. The row height is
        // chosen (Style.coverRowHeight) and the page size follows from it; what
        // must never happen is a picture setting the row's height as a side
        // effect, because then the number of results on a page is whatever the
        // art happened to measure. The row was made taller on purpose after
        // seeing it on the device -- fewer results per page is the price, and
        // it was paid deliberately rather than by accident.
        win.want("the thumbnail is shorter than the row it sits in",
                 Style.thumbHeight < Style.coverRowHeight, true)
        // The intent, which every other assertion here is relative to and so
        // cannot catch: a row carrying a cover is deliberately taller than an
        // ordinary one. Collapse the two back together and the thumbnail is a
        // postage stamp again, with every proportion still "correct".
        win.want("a cover row is taller than an ordinary row",
                 Style.coverRowHeight > Style.rowHeight, true)
        win.want("so a row is still exactly one row high",
                 win.findChildren(seriesGrid, "seriesRowArea", [])[0].height,
                 Style.coverRowHeight)
        win.want("and a page of rows is still the rows that fit",
                 searchRows.height, seriesGrid.pageSize * Style.coverRowHeight)

        // No cover yet: the same titled placeholder the tiles draw, scaled
        // down (ui/CoverArt.qml). Never a ragged gap, never a broken image.
        var searchRowArt = win.findChildren(searchRows, "coverPlaceholder", [])
        win.want("a row with no cover says which book it is",
                 searchRowArt[0].text, "Series 0")
        win.want("and that is what is drawn", searchRowArt[0].visible, true)

        // And the cover arriving puts it away, on a row exactly as on a tile.
        seriesModel.setProperty(0, "coverPath", String(Qt.resolvedUrl("../../icon.svg")))
        win.want("a cover that arrived puts the row's placeholder away",
                 searchRowArt[0].visible, false)
        win.want("and the picture is what is drawn instead",
                 win.findChildren(searchRows, "coverImage", [])[0].visible, true)
        seriesModel.setProperty(0, "coverPath", "")

        seriesGrid.sourceName = "Example Reader"
        // findChildren rather than findChild from here on: once the rows stop
        // being identical, the first Text depth-first traversal happens to
        // find is not necessarily row 0's, and this is about row 0 in
        // particular (the row setProperty(0, ...) below reaches).
        win.want("a row with no authors says only where the result came from",
                 win.findChildren(seriesGrid, "seriesRowSubtitle", [])[0].text, "Example Reader")

        seriesModel.setProperty(0, "authors", "Frank Herbert")
        win.want("a row with both says the authors, then the source",
                 win.findChildren(seriesGrid, "seriesRowSubtitle", [])[0].text,
                 "Frank Herbert · Example Reader")

        seriesGrid.sourceName = ""
        win.want("with no source name it is the authors alone, no stray separator",
                 win.findChildren(seriesGrid, "seriesRowSubtitle", [])[0].text, "Frank Herbert")

        seriesModel.setProperty(0, "authors", "")
        win.want("with neither, the line is empty rather than an empty parenthetical",
                 win.findChildren(seriesGrid, "seriesRowSubtitle", [])[0].text, "")

        // Restored before the role-missing check below, which needs a
        // non-empty source name to tell "fell back to it" from "returned
        // nothing".
        seriesGrid.sourceName = "Example Reader"
        seriesModel.setProperty(0, "authors", "")

        // The role-missing case itself: a plain object, as a row from a
        // model that never appended "authors" at all would read back
        // (seriesModel above cannot demonstrate this directly any more, now
        // that its fixture carries the role like Main.qml's fillSeries
        // always does — so this calls the delegate's own composing function
        // with exactly the shape a ListModel would hand it: authors absent
        // as a property, not present-and-empty). Must not throw, and must
        // fall back to the source name alone.
        win.want("a row whose model never had the role at all still renders",
                 seriesGrid.seriesRowSubtitle({"title": "Untitled"}), "Example Reader")

        win.seriesOpens = 0
        var seriesRowAreas = win.findChildren(seriesGrid, "seriesRowArea", [])
        win.want("every row on the page is a tap target", seriesRowAreas.length > 1, true)
        seriesRowAreas[1].clicked(null)
        win.want("tapping a row opens once", win.seriesOpens, 1)
        win.want("the series that row names", win.seriesOpenedId, "x1")

        // ---- the tile grid's own subtitle line, and its geometry -----------
        //
        // A standalone grid (see its declaration above): hasSubtitles is a
        // plain property on CoverGrid, so setting it directly here is
        // reliable and does not depend on any screen's own wiring.

        for (var st = 0; st < 6; ++st)
            subtitleTilesModel.append({"seriesId": "t" + st, "title": "Tile " + st,
                                       "coverUrl": "", "coverPath": "", "authors": ""})
        win.findChild(subtitleTiles, "coverTiles").forceLayout()

        var geometryBefore = {
            captionHeight: subtitleTiles.captionHeight,
            cellHeight: subtitleTiles.cellHeight,
            pageSize: subtitleTiles.pageSize
        }
        win.want("with nothing to show, no tile carries a visible subtitle",
                 win.findChildren(subtitleTiles, "coverSubtitle", [])[0].visible, false)

        subtitleTiles.hasSubtitles = true
        win.findChild(subtitleTiles, "coverTiles").forceLayout()
        win.want("turning hasSubtitles on grows the caption",
                 subtitleTiles.captionHeight > geometryBefore.captionHeight, true)
        win.want("which grows the cell", subtitleTiles.cellHeight > geometryBefore.cellHeight, true)
        win.want("and so holds fewer per page",
                 subtitleTiles.pageSize <= geometryBefore.pageSize, true)

        subtitleTilesModel.setProperty(0, "authors", "Frank Herbert")
        win.want("a tile with an author shows the subtitle",
                 win.findChildren(subtitleTiles, "coverSubtitle", [])[0].visible, true)
        win.want("naming it", win.findChildren(subtitleTiles, "coverSubtitle", [])[0].text,
                 "Frank Herbert")

        subtitleTiles.hasSubtitles = false
        win.want("turning hasSubtitles back off restores the caption",
                 subtitleTiles.captionHeight, geometryBefore.captionHeight)
        win.want("and the cell with it", subtitleTiles.cellHeight, geometryBefore.cellHeight)
        win.want("and a tile's subtitle goes with it, even though it has an author",
                 win.findChildren(subtitleTiles, "coverSubtitle", [])[0].visible, false)

        // A model whose rows never carried "authors" at all — the missing
        // role, not merely an empty one (CoverGrid's own roleOf comment).
        var noAuthorsModel = Qt.createQmlObject(
            'import QtQuick 2.5; ListModel {}', subtitleTiles, "noAuthorsModel")
        noAuthorsModel.append({"seriesId": "n0", "title": "No Role", "coverUrl": "", "coverPath": ""})
        subtitleTiles.model = noAuthorsModel
        subtitleTiles.hasSubtitles = true
        win.findChild(subtitleTiles, "coverTiles").forceLayout()
        win.want("a tile whose model never had the role at all still renders",
                 win.findChildren(subtitleTiles, "coverSubtitle", [])[0].text, "")
        subtitleTiles.model = subtitleTilesModel
        subtitleTiles.hasSubtitles = false

        seriesGrid.view = "grid"
        seriesGrid.busy = false
        seriesGrid.pendingPage = 0
        win.findChild(searchTiles, "coverTiles").forceLayout()
        win.want("going back to tiles asks for their covers again",
                 win.seriesCovers.length > 0, true)
        var seriesTileAreas = win.findChildren(searchTiles, "coverTileArea", [])
        win.seriesOpens = 0
        seriesTileAreas[1].clicked(null)
        win.want("tapping a tile opens once", win.seriesOpens, 1)
        win.want("the series that tile names", win.seriesOpenedId, "x1")

        // ---- Downloaded as tiles -------------------------------------------
        //
        // The default, and the case the placeholder exists for: most of a
        // library predates covers being stored at all.

        win.want("Downloaded opens as tiles before any status", downloadedList.view, "grid")

        downloadedRows([
            {"sourceId": "src-a", "sourceName": "Example Reader",
             "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
             "detail": "3 downloads", "openable": true, "note": "",
             "coverUrl": "https://example.invalid/lantern.jpg", "latestUuid": "doc-1"},
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/series/orphan/", "title": "An Orphan",
             "detail": "1 download", "openable": true, "note": "", "coverUrl": ""},
            {"sourceId": "gone", "sourceName": "gone",
             "seriesId": "/manga/lost/", "title": "Lost",
             "detail": "2 downloads", "openable": false,
             "note": "The source this came from has been removed.", "coverUrl": ""}])

        var dlTiles = win.findChild(downloadedList, "downloadedTiles")
        var dlRows = win.findChild(downloadedList, "downloadedRows")
        win.want("the tiles are what is on screen", dlTiles.visible, true)
        win.want("and the rows are not", dlRows.visible, false)

        // A row with no cover is a titled placeholder, never a blank square and
        // never a broken image.
        var dlPlaceholders = win.findChildren(dlTiles, "coverPlaceholder", [])
        win.want("every tile can say which book it is", dlPlaceholders.length, 3)
        win.want("a series with no cover shows its title instead",
                 dlPlaceholders[1].text, "An Orphan")
        win.want("and that is what is drawn", dlPlaceholders[1].visible, true)

        // The batch names each row's own source: unlike a search, this screen
        // draws series from every source at once, and only the source knows how
        // to fetch its covers.
        win.downloadedCoverAsks = 0
        win.downloadedCovers = []
        downloadedList.requestVisibleCovers()
        win.want("covers are asked for once", win.downloadedCoverAsks, 1)
        win.want("only for the rows that have one", win.downloadedCovers.length, 1)
        win.want("named with that row's own source", win.downloadedCovers[0].sourceId, "src-a")
        win.want("and that row's own series",
                 win.downloadedCovers[0].seriesId, "/manga/lantern/")

        // A cover that has arrived replaces the placeholder, and is not asked
        // for a second time: the politeness limiter serialises fetches (PLAN
        // §7.4), so a refetch stands in front of a cover nobody has yet.
        downloadedModel.setProperty(0, "coverPath", String(Qt.resolvedUrl("../../icon.svg")))
        win.want("a cover that arrived puts the placeholder away",
                 dlPlaceholders[0].visible, false)
        win.downloadedCoverAsks = 0
        downloadedList.requestVisibleCovers()
        win.want("and is not asked for again", win.downloadedCoverAsks, 0)

        win.downloadedOpens = 0
        var dlTileAreas = win.findChildren(dlTiles, "coverTileArea", [])
        dlTileAreas[0].clicked(null)
        win.want("a tile opens its series once", win.downloadedOpens, 1)
        win.want("on its own source", win.downloadedOpenedSource, "src-a")
        win.want("and its own series", win.downloadedOpenedSeries, "/manga/lantern/")

        // A row whose source has been removed is inert in the grid too: there
        // is no source left to browse, and a tap that goes nowhere quietly is
        // worse than a tile that never offered.
        dlTileAreas[2].clicked(null)
        win.want("a tile with no source left opens nothing", win.downloadedOpens, 1)

        // Deleting stays in the rows for now. The grid is navigation only.
        downloadedList.view = "list"
        win.want("the switch swaps Downloaded's layout", dlRows.visible, true)
        win.want("and puts the tiles away", dlTiles.visible, false)
        win.want("rows hold more series than tiles",
                 downloadedList.pageSize > dlTiles.pageSize, true)
        win.want("Delete is on every row", win.findChildren(dlRows, "deleteSeriesArea", []).length, 3)
        win.want("and on no tile", win.findChildren(dlTiles, "deleteSeriesArea", []).length, 0)

        // The row's own cover. This screen is the one the placeholder exists
        // for: a series downloaded before covers were remembered has no URL at
        // all, so "no picture" is the common case and has to look deliberate.
        dlRows.forceLayout()
        var dlThumbs = win.findChildren(dlRows, "rowCover", [])
        var dlRowArt = win.findChildren(dlRows, "coverPlaceholder", [])
        win.want("every downloaded row carries a cover", dlThumbs.length, 3)
        win.want("sized from the row, not sizing it",
                 dlThumbs[0].height, Style.thumbHeight)
        win.want("so the row is still one row high",
                 win.findChildren(dlRows, "downloadedRowArea", [])[0].height,
                 Style.coverRowHeight)
        win.want("and the page still holds the rows that fit",
                 dlRows.height, downloadedList.pageSize * Style.coverRowHeight)
        win.want("a series with no cover shows its title instead",
                 dlRowArt[1].text, "An Orphan")
        win.want("and that is what is drawn", dlRowArt[1].visible, true)
        win.want("while the row whose cover arrived draws the picture",
                 dlRowArt[0].visible, false)

        // Covers for the rows on the page, in **one** message spanning both
        // sources. Splitting it per source was a real bug -- the backend
        // cancels the batch before it, so each message cancelled the last
        // (ui/Main.qml requestCoversBySource, backend/service/covers_batch_test.go).
        downloadedRows([
            {"sourceId": "src-a", "sourceName": "Example Reader",
             "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
             "detail": "3 downloads", "openable": true, "note": "",
             "coverUrl": "https://example.invalid/lantern.jpg"},
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/series/orphan/", "title": "An Orphan",
             "detail": "1 download", "openable": true, "note": "",
             "coverUrl": "https://example.invalid/orphan.jpg"}])
        win.downloadedCoverAsks = 0
        win.downloadedCovers = []
        downloadedList.requestVisibleCovers()
        win.want("the rows ask for their covers", win.downloadedCoverAsks, 1)
        win.want("in one message, however many sources", win.downloadedCovers.length, 2)
        win.want("each entry naming its own source",
                 win.downloadedCovers[0].sourceId + "," + win.downloadedCovers[1].sourceId,
                 "src-a,src-b")

        // ---- what a screen's report is turned into (Covers.js) -------------
        //
        // The one place a cover request is built, driven here as the library
        // it is. Its caller — Main.qml's requestCoversBySource, and the rule
        // that a null request is not sent at all — is asserted against the
        // real Main.qml in build/qmlcheck/MainHarness.qml.
        //
        // **An empty visible set sends nothing at all.** The screens report an
        // empty batch rather than staying quiet -- that is how a page turned
        // away from drops its fetches -- and the backend cancels the batch in
        // flight *before* it notices the new one is empty. So a screen that
        // simply has no rows yet, which is every screen at startup, would
        // otherwise cancel the covers another screen is waiting for.
        win.want("an empty visible set is not a message",
                 Covers.request([], "src-a"), null)
        win.want("nor is a screen that has no covers at all",
                 Covers.request(null, "src-a"), null)
        win.want("nor one whose rows all have their covers already",
                 Covers.request([{"sourceId": "src-a", "seriesId": "s1", "url": ""}],
                                "src-a"), null)
        // And a set with something in it *is* one, so the line above is a
        // decision and not a function that never sends.
        win.want("a set with a cover in it is a message",
                 Covers.request([{"sourceId": "src-a", "seriesId": "s1", "url": "u1"}],
                                "src-a").covers.length, 1)

        // One message, whatever the entries name. A batch that spans sources
        // stays whole: splitting it per source made each message cancel the
        // one before it (backend/service/covers_batch_test.go).
        var spanning = Covers.batch([{"sourceId": "src-a", "seriesId": "s1", "url": "u1"},
                                     {"sourceId": "src-b", "seriesId": "s2", "url": "u2"}], "")
        win.want("a batch spanning sources stays one batch", spanning.length, 2)
        win.want("with the first entry's own source", spanning[0].sourceId, "src-a")
        win.want("and the second's own", spanning[1].sourceId, "src-b")

        // The per-source search screen's rows carry no source: every row has
        // the same one and Main.qml is what knows which. An entry that names
        // its own keeps it.
        var stamped = Covers.batch([{"seriesId": "s1", "url": "u1"},
                                    {"sourceId": "src-b", "seriesId": "s2", "url": "u2"}],
                                   "src-a")
        win.want("a row with no source of its own is stamped with the screen's",
                 stamped[0].sourceId, "src-a")
        win.want("and a row that named one keeps it", stamped[1].sourceId, "src-b")

        // Nothing to fetch with, nothing to fetch: a row with no URL, and a
        // row nobody can name a source for, are both left out rather than
        // sent for the backend to refuse.
        win.want("a row with no cover URL is not asked about",
                 Covers.batch([{"sourceId": "src-a", "seriesId": "s1", "url": ""}], "").length, 0)
        win.want("nor is one with no source anywhere",
                 Covers.batch([{"seriesId": "s1", "url": "u1"}], "").length, 0)

        // The grid pages like everything else here, and the window moves a
        // whole page at a time.
        var lots = []
        for (var g = 0; g < 40; ++g)
            lots.push({"sourceId": "src-a", "sourceName": "Example Reader",
                       "seriesId": "/manga/" + g + "/", "title": "Series " + g,
                       "detail": "1 download", "openable": true, "note": "",
                       "coverUrl": "https://example.invalid/" + g + ".jpg"})
        downloadedList.view = "grid"
        downloadedRows(lots)
        downloadedList.page = 1
        win.want("a library of forty needs more than one page of tiles",
                 downloadedList.totalPages > 1, true)
        win.downloadedCovers = []
        downloadedList.page = 2
        win.want("the second page starts where the first ended",
                 dlTiles.firstIndex, downloadedList.pageSize)
        win.want("and covers are asked for that page only",
                 win.downloadedCovers.length, downloadedList.pageSize)
        win.want("starting with its first series", win.downloadedCovers[0].seriesId,
                 "/manga/" + downloadedList.pageSize + "/")

        // ---- Watching as tiles: the badge comes with them ------------------
        //
        // The badge is the reason this screen exists, so a layout that lost it
        // would answer the screen's own question with "open every one and see".

        WatchJs.reconcile(watchedModel, [
            {"sourceId": "src", "seriesId": "w-new", "sourceName": "Example Reader",
             "title": "Watched With News", "newChapters": 3, "badge": "3 new chapters",
             "state": "new", "status": "3 new chapters",
             "coverUrl": "https://example.invalid/w.jpg"},
            {"sourceId": "src", "seriesId": "w-quiet", "sourceName": "Example Reader",
             "title": "Watched Quietly", "newChapters": 0, "badge": "",
             "state": "ok", "status": "Up to date"}])

        var wTiles = win.findChild(watchList, "watchTiles")
        var wRows = win.findChild(watchList, "watchRows")
        win.findChild(wTiles, "coverTiles").forceLayout()

        win.want("Watching opens as tiles before any status", watchList.view, "grid")
        win.want("the tiles are what is on screen", wTiles.visible, true)
        win.want("and the rows are not", wRows.visible, false)

        var wBadges = win.findChildren(wTiles, "coverBadge", [])
        var wBadgeText = win.findChildren(wTiles, "coverBadgeText", [])
        win.want("every tile has room for a badge", wBadges.length, 2)
        win.want("the series with new chapters wears one", wBadges[0].visible, true)
        win.want("in the backend's words", wBadgeText[0].text, "3 new chapters")
        win.want("and a series with nothing new wears none", wBadges[1].visible, false)

        // Search results have no badge role at all, and the same delegate draws
        // them without one.
        win.want("a model with no badges draws none",
                 win.findChildren(searchTiles, "coverBadge", [])[0].visible, false)

        win.watchOpens = 0
        var wTileAreas = win.findChildren(wTiles, "coverTileArea", [])
        wTileAreas[0].clicked(null)
        win.want("a tile opens its series once", win.watchOpens, 1)
        win.want("on its own source", win.watchOpenedSource, "src")
        win.want("and its own series", win.watchOpenedSeries, "w-new")

        // The switch swaps which layout is drawn, and swaps it back. Stop
        // watching stays on the row's long-press strip for now, which is the
        // other reason the rows have to survive the tiles.
        watchList.view = "list"
        win.want("the switch swaps Watching's layout", wRows.visible, true)
        win.want("and puts the tiles away", wTiles.visible, false)

        // The rows carry the cover too, and the badge keeps its end of the
        // row: the badge is why this screen exists, so a thumbnail that
        // crowded it out would be the wrong trade.
        wRows.forceLayout()
        var wThumbs = win.findChildren(wRows, "rowCover", [])
        var wRowArt = win.findChildren(wRows, "coverPlaceholder", [])
        win.want("every watched row carries a cover", wThumbs.length, 2)
        win.want("sized from the row, not sizing it", wThumbs[0].height, Style.thumbHeight)
        win.want("so the row is still one row high",
                 win.findChildren(wRows, "watchRowArea", [])[0].height, Style.coverRowHeight)
        win.want("and the badge is still on the row",
                 win.findChildren(wRows, "watchBadge", [])[0].visible, true)
        // No watch row ever arrives with a cover file -- coverPath is the
        // frontend's own, filled when a fetch lands (Watch.js) -- so this is
        // what the screen looks like until one does.
        win.want("a watched series with no cover yet says which it is",
                 wRowArt[0].text, "Watched With News")
        win.want("and that is what is drawn", wRowArt[0].visible, true)

        win.watchCoverAsks = 0
        win.watchCovers = []
        watchList.requestVisibleCovers()
        win.want("the rows ask for the covers on the page", win.watchCoverAsks, 1)
        win.want("only for the row that has one to fetch", win.watchCovers.length, 1)
        win.want("named with that row's own source", win.watchCovers[0].sourceId, "src")

        watchList.view = "grid"
        win.want("and the tiles come back", wTiles.visible, true)

        // A cover already on disk survives the list being pushed again --
        // which is on attach, after every watch, and at the end of every check
        // round. No watch row ever carries a coverPath, so copying the incoming
        // empty over it would blank every tile on the screen.
        watchedModel.setProperty(0, "coverPath", "file:///tmp/w.png")
        WatchJs.reconcile(watchedModel, [
            {"sourceId": "src", "seriesId": "w-new", "sourceName": "Example Reader",
             "title": "Watched With News", "newChapters": 3, "badge": "3 new chapters",
             "state": "new", "status": "3 new chapters",
             "coverUrl": "https://example.invalid/w.jpg"},
            {"sourceId": "src", "seriesId": "w-quiet", "sourceName": "Example Reader",
             "title": "Watched Quietly", "newChapters": 0, "badge": "",
             "state": "ok", "status": "Up to date"}])
        win.want("a pushed list does not blank a cover already fetched",
                 watchedModel.get(0).coverPath, "file:///tmp/w.png")
        win.want("and the rest of the row is still written",
                 watchedModel.get(0).badge, "3 new chapters")

        // ---- one query across every source (Msg.SearchAll) -----------------
        //
        // A group is one series merged across sources by the backend, and the
        // frontend's whole job here is to show *which* sources without ever
        // pretending the difference has gone away: the list names them on the
        // subtitle line, the grid marks the tile, and the series screen keeps
        // the pair the user is acting on visible. Grouping.js is the part with
        // behaviour, and it is driven directly here — the model is filled
        // through the same function Main.qml fills it through, from a whole
        // reply. Main.qml's own half of it, including the group's matches
        // becoming the series screen's switcher, is asserted against the real
        // Main.qml in build/qmlcheck/MainHarness.qml.

        win.want("an empty query is not a search", Grouping.searchable(""), false)
        // Whitespace is empty. A space is what a full stop's neighbour key
        // produces by accident, and fanning out to every configured site for
        // one is the expensive version of a typo (PLAN §7.4).
        win.want("nor is a query of spaces", Grouping.searchable("   "), false)
        win.want("a real query is", Grouping.searchable("lantern"), true)

        var lanternGroup = {
            "key": "the lantern keeper", "title": "The Lantern Keeper",
            "coverUrl": "https://example.invalid/lantern.jpg",
            "matches": [
                {"sourceId": "src-a", "sourceName": "Example Reader",
                 "seriesId": "/manga/lantern/", "coverUrl": "https://example.invalid/a.jpg"},
                {"sourceId": "src-b", "sourceName": "Other Reader",
                 "seriesId": "/series/lantern", "coverUrl": "https://example.invalid/b.jpg"}]}
        var orphanGroup = {
            "key": "an orphan", "title": "An Orphan", "coverUrl": "",
            "matches": [
                {"sourceId": "src-b", "sourceName": "Other Reader",
                 "seriesId": "/series/orphan", "coverUrl": "https://example.invalid/o.jpg"}]}

        // The two composed lines, and the pair a tap opens.
        win.want("a group names every source it was found in",
                 Grouping.sourceLine(lanternGroup), "Example Reader · Other Reader")
        win.want("and a group from one source names that one",
                 Grouping.sourceLine(orphanGroup), "Other Reader")
        win.want("a group in several sources is marked",
                 Grouping.badgeFor(lanternGroup), "2 sources")
        win.want("a group in one is not", Grouping.badgeFor(orphanGroup), "")
        win.want("opening a group opens the first match's source",
                 Grouping.groupRow(lanternGroup).sourceId, "src-a")
        win.want("and the first match's series",
                 Grouping.groupRow(lanternGroup).seriesId, "/manga/lantern/")
        // The series id differs per source, which is the whole reason the pair
        // travels together rather than the source being remembered separately.
        win.want("a group with no cover of its own borrows its first match's",
                 Grouping.groupRow(orphanGroup).coverUrl, "https://example.invalid/o.jpg")

        // The bug this exists to catch: a cover attributed to the group's
        // opening match rather than to the match it actually came from is a
        // cross-domain fetch under a source that never served that URL, and
        // the backend's SSRF guard correctly refuses it. The backend names the
        // cover's own source and series alongside the URL (searchall.go
        // group()); this is that pairing surviving groupRow unmixed with
        // sourceId/seriesId, which still name the *opening* match.
        var mixedOwnerGroup = {
            "key": "mixed owner", "title": "Mixed Owner",
            "coverUrl": "https://beta.invalid/cover.jpg",
            "coverSourceId": "src-b", "coverSeriesId": "/series/beta-owns-this/",
            "matches": [
                {"sourceId": "src-a", "sourceName": "Example Reader",
                 "seriesId": "/manga/mixed/"},
                {"sourceId": "src-b", "sourceName": "Other Reader",
                 "seriesId": "/series/beta-owns-this/",
                 "coverUrl": "https://beta.invalid/cover.jpg"}]}
        var mixedRow = Grouping.groupRow(mixedOwnerGroup)
        win.want("a group still opens on its first match's source",
                 mixedRow.sourceId, "src-a")
        win.want("and that match's series", mixedRow.seriesId, "/manga/mixed/")
        win.want("but its cover is attributed to the cover's own source",
                 mixedRow.coverSourceId, "src-b")
        win.want("with that source's own series id",
                 mixedRow.coverSeriesId, "/series/beta-owns-this/")

        // A source that did not answer. Not an error: it contributed no rows
        // while the others filled the screen.
        win.want("every source answering says nothing", Grouping.failedLine([]), "")
        win.want("a source that did not answer is named",
                 Grouping.failedLine([{"sourceId": "src-c", "sourceName": "Third Reader",
                                       "message": "the request timed out"}]),
                 "No answer from Third Reader")

        var searchAllIndex = Grouping.fill(searchAllModel, {
            "query": "lantern", "page": 1, "pageSize": 6, "totalPages": 0, "hasMore": true,
            "groups": [lanternGroup, orphanGroup],
            "sourceErrors": [{"sourceId": "src-c", "sourceName": "Third Reader",
                              "message": "the request timed out"}]})
        win.want("the page holds one row per group", searchAllModel.count, 2)
        win.want("the switcher's sources are kept beside it",
                 Grouping.matchesFor(searchAllIndex, "the lantern keeper").length, 2)
        // A key from a page that has been turned away from resolves to
        // nothing, rather than to whatever the last page had under it.
        win.want("a group nobody is showing has no sources",
                 Grouping.matchesFor(searchAllIndex, "gone").length, 0)

        // ---- the combined results as tiles ---------------------------------

        searchAll.visible = true
        searchAll.failedSources = Grouping.failedLine(
            [{"sourceId": "src-c", "sourceName": "Third Reader", "message": "timed out"}])
        var saTiles = win.findChild(searchAll, "searchAllTiles")
        var saRows = win.findChild(searchAll, "searchAllRows")
        win.findChild(saTiles, "coverTiles").forceLayout()

        win.want("the combined search opens as tiles before any status",
                 searchAll.view, "grid")
        win.want("the tiles are what is on screen", saTiles.visible, true)
        win.want("and the rows are not", saRows.visible, false)
        win.want("a page of tiles is whole rows of three", searchAll.pageSize % 3, 0)

        var saCaptions = win.findChildren(saTiles, "coverCaption", [])
        win.want("every group is a tile", saCaptions.length, 2)
        win.want("titled with the group's title", saCaptions[0].text, "The Lantern Keeper")

        // The grid's mark. There is no room for a line of source names on a
        // tile, so the count goes in CoverGrid's badge corner -- the slot
        // Watching's "3 new chapters" already uses.
        var saBadges = win.findChildren(saTiles, "coverBadge", [])
        var saBadgeText = win.findChildren(saTiles, "coverBadgeText", [])
        win.want("a group found in several sources is marked", saBadges[0].visible, true)
        win.want("with how many have it", saBadgeText[0].text, "2 sources")
        win.want("and a group found in one wears no mark", saBadges[1].visible, false)

        // Covers: one batch, each entry naming its own source, because this
        // screen draws series from several at once.
        win.searchAllCoverAsks = 0
        win.searchAllCovers = []
        searchAll.requestVisibleCovers()
        win.want("covers are asked for once", win.searchAllCoverAsks, 1)
        win.want("for both groups", win.searchAllCovers.length, 2)
        win.want("named with the source the group opens on",
                 win.searchAllCovers[0].sourceId, "src-a")
        win.want("and that source's own series id",
                 win.searchAllCovers[0].seriesId, "/manga/lantern/")

        win.searchAllOpens = 0
        var saTileAreas = win.findChildren(saTiles, "coverTileArea", [])
        saTileAreas[0].clicked(null)
        win.want("tapping a tile opens once", win.searchAllOpens, 1)
        win.want("naming the group", win.searchAllOpenedKey, "the lantern keeper")
        win.want("on the first source", win.searchAllOpenedSource, "src-a")
        win.want("with that source's series id", win.searchAllOpenedSeries, "/manga/lantern/")
        win.want("and the group's title", win.searchAllOpenedTitle, "The Lantern Keeper")

        // **No menu on a group.** Watch on a group would have to pick a source
        // silently, and every record Quire keeps is per (source, series). A
        // hold does nothing at all here -- including, and especially, not
        // opening the series behind the user's back.
        win.want("there is no menu on the combined results",
                 win.findChildren(searchAll, "contextMenuItem", []).length, 0)
        win.holdOn(saTileAreas[0])
        win.want("and holding a tile opens nothing", win.searchAllOpens, 1)
        win.want("nor a menu", win.menuActions(searchAll).length, 0)

        // ---- the combined results as rows ----------------------------------

        searchAll.view = "list"
        // The rows are built on a layout pass, which has not run yet: the
        // switch was thrown a statement ago.
        saRows.forceLayout()
        win.want("the switch swaps the layout", saRows.visible, true)
        win.want("and puts the tiles away", saTiles.visible, false)
        win.want("a page of rows is one column", searchAll.columns, 1)
        win.want("and holds more groups than a page of tiles",
                 searchAll.pageSize > saTiles.pageSize, true)

        // The line SeriesGrid keeps for one source's name is where the sources
        // go here. This is the whole of "do not blur which source it came
        // from" in the list layout.
        var saSources = win.findChildren(saRows, "searchAllRowSources", [])
        win.want("a row names every source the group was found in",
                 saSources[0].text, "Example Reader · Other Reader")
        win.want("and a single-source group names its one",
                 saSources[1].text, "Other Reader")

        win.searchAllOpens = 0
        var saRowAreas = win.findChildren(saRows, "searchAllRowArea", [])
        win.want("every row on the page is a tap target", saRowAreas.length, 2)
        saRowAreas[1].clicked(null)
        win.want("tapping a row opens once", win.searchAllOpens, 1)
        win.want("that row's group", win.searchAllOpenedKey, "an orphan")
        win.want("on its own source", win.searchAllOpenedSource, "src-b")

        // The rows draw covers too, so the batch holds them -- this used to
        // assert the batch came back *empty*, which was the right intent while
        // a row was two lines of text and is the wrong one now. Still one
        // message, still one entry per row, each naming the source its group
        // opens on: the backend cancels the batch before it, so a batch split
        // per source would cancel itself (Main.qml requestCoversBySource).
        win.searchAllCoverAsks = 0
        win.searchAllCovers = []
        searchAll.requestVisibleCovers()
        win.want("rows ask for a batch too", win.searchAllCoverAsks, 1)
        win.want("holding the covers the rows draw", win.searchAllCovers.length, 2)
        win.want("each named with the source the group opens on",
                 win.searchAllCovers[0].sourceId + "," + win.searchAllCovers[1].sourceId,
                 "src-a,src-b")

        // The bug this exists to catch, end to end: a group whose cover came
        // from a source *other than* the one it opens on must have its cover
        // requested under that cover's own source and series — not the row's
        // sourceId/seriesId, which name the opening match and would ask the
        // wrong source's SSRF guard to fetch a URL it never served.
        Grouping.fill(searchAllModel, {"groups": [mixedOwnerGroup], "sourceErrors": []})
        saRows.forceLayout()
        win.searchAllCoverAsks = 0
        win.searchAllCovers = []
        searchAll.requestVisibleCovers()
        win.want("a mixed-owner group still asks for one cover",
                 win.searchAllCovers.length, 1)
        win.want("named with the cover's own source, not the opening match's",
                 win.searchAllCovers[0].sourceId, "src-b")
        win.want("and the cover's own series id",
                 win.searchAllCovers[0].seriesId, "/series/beta-owns-this/")
        win.want("the url is still the group's cover",
                 win.searchAllCovers[0].url, "https://beta.invalid/cover.jpg")

        // Restored, because everything after this in the list-view section
        // assumes the two-group page from before.
        Grouping.fill(searchAllModel, {"groups": [lanternGroup, orphanGroup], "sourceErrors": []})
        saRows.forceLayout()

        // The thumbnail, and the placeholder for a group whose cover has not
        // landed -- which is every group here, since nothing has fetched one.
        var saThumbs = win.findChildren(saRows, "rowCover", [])
        var saRowArt = win.findChildren(saRows, "coverPlaceholder", [])
        win.want("every group's row carries a cover", saThumbs.length, 2)
        win.want("sized from the row, not sizing it", saThumbs[0].height, Style.thumbHeight)
        win.want("so the row is still one row high",
                 saRowAreas[0].height, Style.coverRowHeight)
        win.want("and the page still holds the rows that fit",
                 saRows.height, searchAll.pageSize * Style.coverRowHeight)
        win.want("a group with no cover yet says which book it is",
                 saRowArt[0].text, "The Lantern Keeper")
        win.want("and that is what is drawn", saRowArt[0].visible, true)

        // ---- sources that did not answer -----------------------------------
        //
        // The line is under the results, not over them. A search where two
        // sources answered and one did not is a search that worked.

        var failedBar = win.findChild(searchAll, "searchAllFailedBar")
        var failedText = win.findChild(searchAll, "searchAllFailed")
        win.want("the sources that did not answer are named",
                 failedText.text, "No answer from Third Reader")
        win.want("on a line that is actually drawn", failedBar.visible, true)
        win.want("the results are still there", searchAll.rowCount, 2)
        win.want("and still on screen", saRows.visible, true)
        // The empty-state text is what a blanked page would show. It must not
        // be: nothing failed from the user's side, some sources are simply
        // missing from the answer.
        win.want("nothing reads as an empty search",
                 win.findChild(searchAll, "searchAllStatus").visible, false)

        // Every source answering takes the line away entirely, rather than
        // leaving a reassuring one nobody needs to read.
        searchAll.failedSources = ""
        win.want("a search where every source answered says nothing",
                 failedBar.visible, false)
        win.want("and the line takes no room", failedBar.height, 0)

        // ---- what an empty query costs -------------------------------------
        //
        // Nothing. An empty query here is not the browse it is on a single
        // source: it would fan out to every configured site at once.
        // Driven through the keyboard's own Search key, which is the only way
        // a query is ever submitted.

        var saKeys = win.findChild(searchAll, "searchAllKeyboard")
        win.searchAllAsks = 0
        searchAll.query = ""
        searchAll.searching = true
        saKeys.submit()
        win.want("an empty query sends nothing", win.searchAllAsks, 0)
        win.want("and the screen does not sit waiting for it", searchAll.busy, false)
        // It still puts the keyboard away: the user pressed Search, and a
        // keyboard that stays up reads as the key not having registered.
        win.want("but the keyboard still goes", searchAll.searching, false)

        searchAll.query = "   "
        searchAll.searching = true
        saKeys.submit()
        win.want("nor does a query of spaces", win.searchAllAsks, 0)

        searchAll.searching = true
        saKeys.keyTyped("l")
        saKeys.keyTyped("a")
        win.want("the keys reach the query", searchAll.query, "   la")
        searchAll.query = "lantern"
        win.want("and the field shows it",
                 win.findChild(searchAll, "searchField").text, "lantern")
        saKeys.submit()
        win.want("a real query is sent once", win.searchAllAsks, 1)
        win.want("as typed", win.searchAllAsked, "lantern")
        win.want("from the first page", searchAll.pendingPage, 1)
        win.want("with the screen waiting for it", searchAll.busy, true)
        win.want("and the keyboard down", searchAll.searching, false)

        // Clear is the same control as the per-source search's, and behaves
        // the same way: it empties the box, keeps the keyboard up, and leaves
        // the results the user may be comparing against alone.
        win.findChild(searchAll, "clearSearchArea").clicked(null)
        win.want("Clear empties the combined search too", searchAll.query, "")
        win.want("keeps its keyboard up", searchAll.searching, true)
        win.want("and leaves the groups on screen", searchAll.rowCount, 2)
        searchAll.dismissInput()

        // ---- paging --------------------------------------------------------
        //
        // The backend pages; this asks for a page and renders what comes back.
        // totalPages stays 0 until every source has run dry, and the label
        // says only what is known (PLAN §12.1).

        searchAll.busy = false
        searchAll.query = "lantern"
        searchAll.page = 1
        searchAll.totalPages = 0
        searchAll.hasMore = true
        searchAll.pendingPage = 0
        var saPager = win.findChild(searchAll, "searchAllPager")
        win.want("a total nobody knows yet is not invented",
                 win.findChild(saPager, "pagerLabel").text, "Page 1")
        win.want("previous is dead on page 1", saPager.canGoBack, false)
        win.want("next is alive while there is more", saPager.canGoOn, true)

        win.searchAllPageAsks = 0
        saPager.nextRequested()
        win.want("turning the page asks the backend once", win.searchAllPageAsks, 1)
        win.want("for the next page", win.searchAllPageAsked, 2)
        win.want("and says which page is in flight", searchAll.pendingPage, 2)
        // Both buttons go dead until it lands, so a second tap cannot queue a
        // second request for the same page.
        win.want("a page in flight stops another tap", saPager.canGoOn, false)

        // The reply lands, with a total this time: every source has run dry.
        Grouping.fill(searchAllModel, {"query": "lantern", "page": 2, "pageSize": 6,
                                       "totalPages": 3, "hasMore": true,
                                       "groups": [lanternGroup, orphanGroup],
                                       "sourceErrors": []})
        searchAll.busy = false
        searchAll.page = 2
        searchAll.totalPages = 3
        searchAll.hasMore = true
        searchAll.pendingPage = 0
        win.want("a total that is known is shown",
                 win.findChild(saPager, "pagerLabel").text, "Page 2 of 3")
        win.want("and previous is alive now", saPager.canGoBack, true)
        win.searchAllPageAsks = 0
        saPager.previousRequested()
        win.want("turning back asks for the page before", win.searchAllPageAsked, 1)

        searchAll.busy = false
        searchAll.pendingPage = 0
        searchAll.visible = false

        // ---- what a row is: manga, or a book -------------------------------
        //
        // A Shelfmark source publishes books; every source before it published
        // page-based series. The field says which, it may be **absent**, and
        // absent is the old world rather than an unknown one — so the default
        // is asserted first and from several directions, because it is the one
        // rule that, if it slipped, would silently reclassify every existing
        // source the day a filter was switched on.

        win.want("a row with no kind at all is manga",
                 Kinds.of({"title": "Something"}), Kinds.MANGA)
        win.want("so is one that says so", Kinds.of({"kind": "manga"}), Kinds.MANGA)
        win.want("a book says so", Kinds.of({"kind": "book"}), Kinds.BOOK)
        // A kind from a newer Quire, or a settings file edited into nonsense:
        // drawn the usual way rather than not drawn at all (Views.js takes the
        // same line on layouts).
        win.want("a kind nothing here knows is manga",
                 Kinds.of({"kind": "audiobook"}), Kinds.MANGA)
        win.want("and no row at all is manga", Kinds.of(null), Kinds.MANGA)
        win.want("a book is the one thing that is one", Kinds.isBook({"kind": "book"}), true)
        win.want("and a row with no kind is not", Kinds.isBook({"title": "x"}), false)

        // The filter itself. The case that matters is the third: a manga
        // filter has to keep the rows whose kind was never sent, or turning it
        // on would empty a library of sources that predate the field.
        win.want("All keeps a book", Kinds.matches(Kinds.ALL, Kinds.BOOK), true)
        win.want("All keeps a row with no kind", Kinds.matches(Kinds.ALL, undefined), true)
        win.want("**Manga keeps a row with no kind**",
                 Kinds.matches(Kinds.MANGA, undefined), true)
        win.want("Manga keeps manga", Kinds.matches(Kinds.MANGA, Kinds.MANGA), true)
        win.want("Manga hides books", Kinds.matches(Kinds.MANGA, Kinds.BOOK), false)
        win.want("Books keeps books", Kinds.matches(Kinds.BOOK, Kinds.BOOK), true)
        win.want("Books hides manga", Kinds.matches(Kinds.BOOK, Kinds.MANGA), false)
        win.want("**and Books hides a row with no kind**",
                 Kinds.matches(Kinds.BOOK, undefined), false)
        // A filter nothing recognises shows too much rather than nothing: an
        // empty screen is the failure that reads as broken.
        win.want("a filter nobody set keeps everything",
                 Kinds.matches("", Kinds.BOOK), true)

        win.want("the control offers three, in order",
                 Kinds.filters()[0].kind + "," + Kinds.filters()[1].kind + ","
                 + Kinds.filters()[2].kind,
                 "all,manga,book")
        win.want("labelled in words",
                 Kinds.filters()[0].label + "," + Kinds.filters()[1].label + ","
                 + Kinds.filters()[2].label,
                 "All,Manga,Books")
        // A .pragma library shares one copy of its state, so the segments are
        // handed out fresh rather than shared: one screen splicing the list
        // must not be a different control on the next.
        win.want("and handed out fresh each time",
                 Kinds.filters() !== Kinds.filters(), true)

        // The mark a book wears in a list. A word, because colour on this panel
        // is a solid area only and a mark that were only a fill would leave
        // "is this a book?" answerable by hue alone (ui/Style.js).
        win.want("a book is marked in a word", Kinds.mark({"kind": "book"}), "Book")
        win.want("and manga wears no mark at all", Kinds.mark({"kind": "manga"}), "")

        // **The screen has to say a filter is on.** A filtered screen and a
        // search that found nothing look identical otherwise, and the second
        // is the one people report as broken. There is no "how much it is
        // holding back" any more: the filter is part of the search itself
        // (backend/service/searchall.go), not a hide applied to a page that
        // was fetched unfiltered.
        win.want("showing everything says nothing", Kinds.filterLine(Kinds.ALL), "")
        win.want("a filter that is on says so",
                 Kinds.filterLine(Kinds.BOOK), "Showing books only.")
        win.want("and the manga filter its own",
                 Kinds.filterLine(Kinds.MANGA), "Showing manga only.")

        // The two emptinesses are different sentences: an unfiltered search
        // that found nothing is the backend's plain answer, and a filtered
        // one names what it was searching for — which is now always the
        // right answer, because the search itself was already scoped to that
        // kind (there is no separate "the filter hid it" case any more).
        win.want("a search that found nothing says so",
                 Kinds.emptyLine(Kinds.ALL), "Nothing came back for that.")
        win.want("a books search that found nothing says that instead",
                 Kinds.emptyLine(Kinds.BOOK), "No books in these results.")
        win.want("and the manga filter its own",
                 Kinds.emptyLine(Kinds.MANGA), "No manga in these results.")

        // ---- a book among the groups ---------------------------------------

        var duneGroup = {
            "key": "dune messiah", "title": "Dune Messiah", "kind": "book",
            "coverUrl": "https://example.invalid/dune.jpg",
            "matches": [
                {"sourceId": "src-s", "sourceName": "Shelfmark",
                 "seriesId": "/book/dune-messiah", "coverUrl": ""}]}
        var twoSiteBook = {
            "key": "dune", "title": "Dune", "kind": "book", "coverUrl": "",
            "matches": [
                {"sourceId": "src-s", "sourceName": "Shelfmark", "seriesId": "/book/dune"},
                {"sourceId": "src-t", "sourceName": "Other Shelf", "seriesId": "/b/dune"}]}
        var duneWithAuthors = {
            "key": "dune messiah", "title": "Dune Messiah", "kind": "book",
            "coverUrl": "", "authors": ["Frank Herbert"],
            "matches": [
                {"sourceId": "src-s", "sourceName": "Shelfmark",
                 "seriesId": "/book/dune-messiah", "coverUrl": ""}]}

        win.want("a group carries its kind onto the row",
                 Grouping.groupRow(duneGroup).kind, "book")
        win.want("and a group with none is manga on the row",
                 Grouping.groupRow(lanternGroup).kind, "manga")

        // The row's second line. A book's cover, title and author line all
        // arrive through the same fields a comic's do, so the word is the only
        // thing on the row that tells them apart.
        win.want("a book's row says what it is, then where it is",
                 Grouping.subtitleLine(duneGroup), "Book · Shelfmark")
        win.want("**and a manga row is exactly what it always was**",
                 Grouping.subtitleLine(lanternGroup), "Example Reader · Other Reader")
        win.want("which is what the row is filled with",
                 Grouping.groupRow(duneGroup).sources, "Book · Shelfmark")
        win.want("a tile marks a book in the badge corner",
                 Grouping.badgeFor(duneGroup), "Book")
        win.want("beside the count when there is one too",
                 Grouping.badgeFor(twoSiteBook), "Book · 2 sources")
        win.want("and a manga tile's badge is untouched",
                 Grouping.badgeFor(lanternGroup), "2 sources")

        // ---- authors on the combined-search row and tile --------------------

        win.want("a group with authors leads with them",
                 Grouping.subtitleLine(duneWithAuthors), "Frank Herbert · Book · Shelfmark")
        win.want("a group with none is exactly as it always was, no stray separator",
                 Grouping.subtitleLine(duneGroup), "Book · Shelfmark")
        win.want("authors alone when there is nothing else to say",
                 Grouping.subtitleLine({"authors": ["Ann Leckie"], "matches": []}), "Ann Leckie")
        win.want("and nothing at all when there is neither",
                 Grouping.subtitleLine({"matches": []}), "")
        win.want("the row is filled with the same composed line",
                 Grouping.groupRow(duneWithAuthors).sources, "Frank Herbert · Book · Shelfmark")
        // The tile's subtitle is authors alone — the sources stay on the
        // badge, which multiple authors joined with ", " would be lost among.
        win.want("the tile role carries authors alone, not the composed line",
                 Grouping.groupRow(duneWithAuthors).authors, "Frank Herbert")
        win.want("a group with none carries an empty string for it",
                 Grouping.groupRow(duneGroup).authors, "")
        win.want("and the badge is unaffected by any of this",
                 Grouping.badgeFor(duneWithAuthors), "Book")

        // Filling one page. **There is no filtering here any more**: the kind
        // filter now travels with the request and decides which sources are
        // ever asked (backend/service/searchall.go), so fill only ever shows
        // every group a reply carries — there is nothing left on the page in
        // hand for it to hide.
        var mixedReply = {
            "query": "dune", "page": 1, "pageSize": 6, "totalPages": 0, "hasMore": false,
            "groups": [lanternGroup, orphanGroup, duneGroup], "sourceErrors": []}

        var allIndex = Grouping.fill(searchAllModel, mixedReply)
        win.want("fill shows every group the reply carries", searchAllModel.count, 3)
        win.want("**including the book, whatever the mix**",
                 searchAllModel.get(2).title, "Dune Messiah")
        win.want("the switcher indexes every group by key",
                 Grouping.matchesFor(allIndex, "the lantern keeper").length, 2)
        win.want("the book too",
                 Grouping.matchesFor(allIndex, "dune messiah").length, 1)

        // ---- authors, rendered on the combined screen's own tiles and rows -

        Grouping.fill(searchAllModel, {"groups": [duneWithAuthors, duneGroup], "sourceErrors": []})
        searchAll.visible = true
        searchAll.view = "grid"
        var authorTiles = win.findChild(searchAll, "searchAllTiles")
        win.findChild(authorTiles, "coverTiles").forceLayout()
        win.want("a page with an author grows the tile's caption",
                 authorTiles.hasSubtitles, true)
        var authorSubtitles = win.findChildren(authorTiles, "coverSubtitle", [])
        win.want("the tile names the author", authorSubtitles[0].text, "Frank Herbert")
        win.want("a tile with none shows nothing", authorSubtitles[1].text, "")
        var authorBadgeText = win.findChildren(authorTiles, "coverBadgeText", [])
        win.want("the badge still marks what it always did, unmoved by any of this",
                 authorBadgeText[0].text, "Book")
        win.want("for both tiles", authorBadgeText[1].text, "Book")

        searchAll.view = "list"
        var authorRows = win.findChild(searchAll, "searchAllRows")
        authorRows.forceLayout()
        var authorRowSources = win.findChildren(authorRows, "searchAllRowSources", [])
        win.want("the row leads with the author, then what it is, then the source",
                 authorRowSources[0].text, "Frank Herbert · Book · Shelfmark")
        win.want("and a group with no author is exactly what it always was",
                 authorRowSources[1].text, "Book · Shelfmark")

        // A page with nothing but manga must not grow the tile at all — the
        // geometry a search for a comic sees is untouched by this feature.
        Grouping.fill(searchAllModel, {"groups": [lanternGroup, orphanGroup], "sourceErrors": []})
        searchAll.view = "grid"
        win.findChild(authorTiles, "coverTiles").forceLayout()
        win.want("a page with no authors at all keeps the tile as it always was",
                 authorTiles.hasSubtitles, false)

        // Changing the tile geometry above changes pageSize, which the
        // screen's own onPageSizeChanged reacts to by asking for the page
        // again (screen.query was still set from an earlier test) — settle
        // that before it is mistaken for a real result below.
        searchAll.busy = false
        searchAll.pendingPage = 0

        // ---- the filter, as a control --------------------------------------
        //
        // The same shape as the layout switch (ui/ViewToggle.qml): the one you
        // are in is the filled one and is dead to touch, so it answers "which
        // am I in?" before it is touched.

        Grouping.fill(searchAllModel, mixedReply)
        searchAll.visible = true
        var kindAll = win.findChild(searchAll, "kindFilter-all")
        var kindManga = win.findChild(searchAll, "kindFilter-manga")
        var kindBook = win.findChild(searchAll, "kindFilter-book")
        var kindAllArea = win.findChild(searchAll, "kindFilterArea-all")
        var kindBookArea = win.findChild(searchAll, "kindFilterArea-book")
        var kindNote = win.findChild(searchAll, "kindFilterNote")

        win.want("the filter is on the combined search",
                 kindAll !== null && kindManga !== null && kindBook !== null, true)
        win.want("and it is drawn", win.findChild(searchAll, "kindStrip").visible, true)
        win.want("**it starts showing everything**", searchAll.kindFilter, Kinds.ALL)
        win.want("with All the filled one", win.colorOf(kindAll), Style.accent)
        win.want("and the other two on paper", win.colorOf(kindBook), Style.paper)
        win.want("the one you are in is dead to touch", kindAllArea.enabled, false)
        win.want("and the others are live", kindBookArea.enabled, true)
        win.want("showing everything says nothing", kindNote.text, "")

        // Tapped, not called: a case that calls showKind() directly proves the
        // function works and says nothing about whether the segment is
        // reachable — `enabled` is exactly what it would step over. The
        // filter is part of the search now (ui/SearchAll.qml's showKind), so
        // tapping it — with a searchable query already on screen — asks for
        // the page again, from page 1, naming the kind.
        win.searchAllAsks = 0
        kindBookArea.clicked(null)
        win.want("tapping Books asks for the page again once", win.searchAllAsks, 1)
        win.want("carrying the query already on screen", win.searchAllAsked, "lantern")
        win.want("and moves the filter", searchAll.kindFilter, Kinds.BOOK)
        win.want("Books is the filled one now", win.colorOf(kindBook), Style.accent)
        win.want("All is back on paper", win.colorOf(kindAll), Style.paper)
        win.want("Books is the inert one now", kindBookArea.enabled, false)
        win.want("and All is live again", kindAllArea.enabled, true)

        // Asking for the filter already on asks for nothing. The guard is
        // asserted as well as `enabled`, because the two protect against
        // different mistakes: one is a tap, the other is a caller.
        win.searchAllAsks = 0
        searchAll.showKind(Kinds.BOOK)
        win.want("asking for the filter already on costs nothing",
                 win.searchAllAsks, 0)

        // **The screen says it is filtering, in words.** Without this a
        // filtered screen and a broken search are the same picture.
        win.want("a filter that is on says so", kindNote.text, "Showing books only.")
        win.want("and it is actually on screen", kindNote.visible, true)
        // In ink, like every other word in the app: the colour on this control
        // is the fill behind the segment and nothing else (ui/Style.js).
        win.want("said in ink, not in colour", win.colorOf(kindNote), Style.ink)
        win.want("as is the label on the filled segment",
                 win.colorOf(win.findChild(searchAll, "kindFilterLabel-book")), Style.ink)

        // The intent behind the fill, which comparing it to Style.accent cannot
        // catch: set the accent to paper and every assertion above still
        // passes while the control marks nothing at all.
        win.want("the filled segment is not the paper the others are",
                 win.colorOf(kindBook) !== win.colorOf(kindAll), true)
        win.want("and is not a grey: it has a hue",
                 win.channels(win.colorOf(kindBook))[0]
                 > win.channels(win.colorOf(kindBook))[2], true)
        win.want("dark enough to read as the filled one without its colour",
                 win.luminance(win.colorOf(kindBook)) < win.luminance(Style.paper) / 2, true)
        win.want("and black on it clears body text's contrast",
                 win.contrast(Style.ink, win.colorOf(kindBook)) >= 4.5, true)
        // The border is grey on both, because a coloured 2px stroke is the
        // shape the measurement in ui/Style.js rules out.
        win.want("no colour on the border of the filled one",
                 String(kindBook.border.color).toUpperCase(), Style.rule)

        // A reply a Books search found nothing in is not a search that found
        // nothing at all: the status text names the filter, and the strip
        // above still says it is on, so the way out is on the screen. The
        // backend already scoped the request to book sources — this reply is
        // exactly what it would send back for that.
        searchAll.busy = false
        searchAll.emptyMessage = Kinds.emptyLine(searchAll.kindFilter)
        Grouping.fill(searchAllModel, {"groups": []})
        win.want("a page the filter found nothing on has no rows", searchAll.rowCount, 0)
        win.want("and says it was the filter",
                 win.findChild(searchAll, "searchAllStatus").text,
                 "No books in these results.")
        win.want("where the user can read it",
                 win.findChild(searchAll, "searchAllStatus").visible, true)
        win.want("with the way back out still on screen",
                 kindNote.text.indexOf("Showing books only") === 0, true)

        // Opening the screen fresh starts the question again, filter and all.
        // The app starting is the same state by construction: nothing stores
        // this, so there is nothing to restore (see the note in SearchAll.qml).
        searchAll.reset()
        win.want("a fresh screen is back to showing everything",
                 searchAll.kindFilter, Kinds.ALL)
        win.want("and saying nothing about a filter", kindNote.text, "")
        win.want("All is the filled one again", win.colorOf(kindAll), Style.accent)

        searchAll.emptyMessage = ""
        Grouping.fill(searchAllModel, {"query": "lantern", "groups": [lanternGroup, orphanGroup]})
        searchAll.visible = false

        // ---- a book's series screen ----------------------------------------
        //
        // The rows are **releases** — the files this one book is available as —
        // and the screen around them has to stop talking about chapters,
        // volumes and reading order. It is the same screen with different
        // words rather than a second one: strip the words out and a book is a
        // series of one volume, and a copy of this file would be ~700 lines of
        // paging and selection bookkeeping duplicated to change four strings.

        bookChapterList.visible = true
        win.findChild(bookChapterList, "chapterRows").forceLayout()

        win.want("a screen nobody told anything is manga", plainChapterList.isBook, false)
        win.want("and one told it is a book is", bookChapterList.isBook, true)

        // **No volume view, and not because the model is empty.** This screen
        // was handed the three volumes on purpose.
        win.want("the volumes are there to be drawn",
                 bookChapterList.volumeModel.count > 0, true)
        win.want("**and a book offers no volume view anyway**",
                 bookChapterList.hasVolumes, false)
        win.want("so the switch is not on the screen",
                 win.findChild(bookChapterList, "viewSwitch").visible, false)
        win.want("and takes no room", win.findChild(bookChapterList, "viewSwitch").height, 0)
        bookChapterList.view = "volumes"
        win.want("and asking for it outright still shows the releases",
                 bookChapterList.showingVolumes, false)
        bookChapterList.view = "chapters"

        // Watching is the new-chapters machinery end to end (PLAN §12.2). A
        // book's releases are the files one finished book already exists as, so
        // the button would promise an announcement that cannot arrive.
        win.want("a book cannot be watched",
                 win.findChild(bookChapterList, "watchButton").visible, false)
        win.want("and the button is dead as well as hidden",
                 win.findChild(bookChapterList, "watchArea").enabled, false)
        win.want("**while a manga series still offers it**",
                 win.findChild(chapterList, "watchButton").visible, true)
        win.want("and it is still live there",
                 win.findChild(chapterList, "watchArea").enabled, true)

        // What the list is, said once where the volume switch would have been,
        // so a book's rows start exactly where a manga's do.
        win.want("a book says what its list holds",
                 win.findChild(bookChapterList, "releaseNote").text,
                 "Releases · pick the file to download.")
        win.want("on a strip that is drawn",
                 win.findChild(bookChapterList, "releaseStrip").visible, true)
        win.want("**and takes no room at all on a manga screen**",
                 win.findChild(chapterList, "releaseStrip").height, 0)
        win.want("where it is not drawn either",
                 win.findChild(chapterList, "releaseStrip").visible, false)

        // A release has no publication date and no scanlator — the whole of
        // what it is, is in the title the backend composed. "Date unknown"
        // under it would be the screen inventing a fact.
        var releaseSubs = win.findChildren(bookChapterList, "chapterSubtitle", [])
        var chapterSubs = win.findChildren(chapterList, "chapterSubtitle", [])
        win.want("a release's row says nothing under its title",
                 releaseSubs[0].text, "")
        win.want("**while a chapter's still carries its date and group**",
                 chapterSubs[0].text, "2026-01-01 · Group")
        win.want("and the release's title is the backend's, untouched",
                 win.findChildren(bookChapterList, "chapterTitle", [])[0].text,
                 "EPUB · 0.4MB · Direct Download · fiction")

        // **No bulk download over a set of alternatives.** Three releases of
        // one book are three copies of it in three formats; queueing them is
        // never what anyone meant, and each one is minutes of a slow queue.
        // For a manga the mode is the whole point, so this is the one place the
        // two want different behaviour rather than different words.
        win.want("the releases are there to be picked", bookChapterList.rowCount, 3)
        win.want("**and a book offers no Select**",
                 win.findChild(bookChapterList, "selectButton").visible, false)
        win.want("the way in is dead as well as hidden",
                 win.findChild(bookChapterList, "selectArea").enabled, false)
        win.want("no release is selectable in the first place",
                 bookChapterList.canSelect("", ""), false)
        // The door is barred as well as hidden: the two stop different
        // mistakes, and a caller is not a tap.
        bookChapterList.enterSelection()
        win.want("and asking to select outright does nothing",
                 bookChapterList.selecting, false)
        win.want("so no bar of bulk actions appears",
                 win.findChild(bookChapterList, "selectionBar").visible, false)
        // Visible boxes, not boxes that exist: the delegate builds one per row
        // and draws it only for a row that can be picked, which is the property
        // under test (the same count the manga case takes).
        var bookBoxes = win.findChildren(bookChapterList, "selectBox", [])
        var bookBoxesShown = 0
        for (var bb = 0; bb < bookBoxes.length; ++bb)
            if (bookBoxes[bb].visible)
                bookBoxesShown++
        win.want("and no boxes on the releases", bookBoxesShown, 0)

        // The counterpart: thirty chapters in one tap is untouched.
        win.want("**while a manga series still offers Select**",
                 win.findChild(chapterList, "selectButton").visible, true)
        win.want("with the way in live", win.findChild(chapterList, "selectArea").enabled, true)
        win.want("and its rows still pickable", chapterList.canSelect("", ""), true)

        // The empty state. The rows are gone, not the wording.
        win.want("an empty book list says releases",
                 win.findChild(bookChapterList, "chapterEmpty").text,
                 "No releases listed.")
        win.want("**and an empty chapter list still says chapters**",
                 win.findChild(chapterList, "chapterEmpty").text, "No chapters listed.")
        bookChapterList.model = emptyVolumesModel
        win.want("and it is what is actually shown when there is nothing",
                 win.findChild(bookChapterList, "chapterEmpty").visible, true)
        bookChapterList.model = releasesModel
        win.findChild(bookChapterList, "chapterRows").forceLayout()

        // And the whole point, asked of the screen rather than of its parts:
        // no word on a book's series screen is about chapters, volumes or the
        // order to read them in.
        var bookWords = win.wordsOn(bookChapterList)
        win.want("a book's screen says nothing about chapters",
                 bookWords.toLowerCase().indexOf("chapter"), -1)
        win.want("nothing about volumes", bookWords.toLowerCase().indexOf("volume"), -1)
        win.want("and invents no date for a file",
                 bookWords.indexOf("Date unknown"), -1)
        win.want("nor offers a bulk action over alternatives",
                 bookWords.indexOf("Select"), -1)
        win.want("of any wording", bookWords.indexOf("Download selected"), -1)
        win.want("while still saying what it does hold",
                 bookWords.indexOf("Releases") >= 0, true)

        // The counterpart, and the one that keeps this honest: the manga screen
        // is untouched. If the words above went missing everywhere, this fails.
        chapterList.visible = true
        win.findChild(chapterList, "chapterRows").forceLayout()
        var mangaWords = win.wordsOn(chapterList)
        win.want("**a manga screen still talks about chapters**",
                 mangaWords.indexOf("Chapter") >= 0, true)
        win.want("and still offers the bulk download",
                 mangaWords.indexOf("Select") >= 0, true)
        win.want("and still offers its volumes",
                 mangaWords.indexOf("Volumes") >= 0, true)
        win.want("with the view switch on screen",
                 win.findChild(chapterList, "viewSwitch").visible, true)
        win.want("and says nothing about releases",
                 mangaWords.indexOf("Releases"), -1)
        // Left as the cases after this one found it: every screen in this
        // harness is stacked over every other and drawn by default, and the
        // accent sweep further down reads effective visibility.
        chapterList.visible = true
        bookChapterList.visible = false

        // ---- a book in the downloaded library ------------------------------
        //
        // **This screen spans every source**, which is exactly why its rows are
        // marked where a single source's results are not: a book and a comic
        // sit in the same list, and the cover, the title and the author line
        // all arrive through identical fields. Same word, same two layouts, and
        // in words rather than in colour alone.

        var dlViewBefore = downloadedList.view
        downloadedList.visible = true
        downloadedRows([
            {"sourceId": "src-a", "sourceName": "Example Reader",
             "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
             "detail": "3 downloads", "openable": true, "note": "", "coverUrl": ""},
            // Watched on purpose: a book that somehow already carries a watch
            // record must not be offered the toggle either, or the line comes
            // back for exactly the rows that most look like it belongs.
            {"sourceId": "src-s", "sourceName": "Shelfmark",
             "seriesId": "/book/dune-messiah", "title": "Dune Messiah", "kind": "book",
             "detail": "1 download", "openable": true, "note": "", "coverUrl": "",
             "watched": true},
            {"sourceId": "gone", "sourceName": "gone", "seriesId": "/book/lost",
             "title": "Lost Book", "kind": "book",
             "detail": "1 download", "openable": false,
             "note": "The source this came from has been removed.", "coverUrl": ""},
            // A watched comic, so the two halves of the manga line are both on
            // the screen the book row is being compared against.
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/manga/orphan/", "title": "An Orphan",
             "detail": "2 downloads", "openable": true, "note": "", "coverUrl": "",
             "watched": true}])

        downloadedList.view = "list"
        win.findChild(downloadedList, "downloadedRows").forceLayout()
        var dlDetails = win.findChildren(downloadedList, "downloadedRowDetail", [])
        win.want("every downloaded row has its line", dlDetails.length, 4)
        win.want("**a downloaded comic's line is exactly what it always was**",
                 dlDetails[0].text, "Example Reader · 3 downloads")
        win.want("**and a book's says so before its source**",
                 dlDetails[1].text, "Book · Shelfmark · 1 download")
        // The count sentence is the backend's and is drawn verbatim either way
        // (PLAN §2); the mark is the only thing the view puts in front of it.
        win.want("with the backend's own count untouched",
                 dlDetails[1].text.indexOf("1 download") > 0, true)
        win.want("and nothing about chapters on a book's row",
                 dlDetails[1].text.toLowerCase().indexOf("chapter"), -1)
        win.want("a book whose source is gone keeps the mark and the note",
                 dlDetails[2].text,
                 "Book · gone · 1 download — The source this came from has been removed.")

        // The tile has no subtitle line, so the mark goes in the badge corner —
        // the slot Watching's count already uses.
        downloadedList.view = "grid"
        win.findChild(downloadedList, "coverTiles").forceLayout()
        var dlBadges = win.findChildren(downloadedList, "coverBadge", [])
        var dlBadgeText = win.findChildren(downloadedList, "coverBadgeText", [])
        win.want("a downloaded book's tile is marked", dlBadges[1].visible, true)
        win.want("in a word", dlBadgeText[1].text, "Book")
        win.want("**and a comic's tile wears no mark at all**",
                 dlBadges[0].visible, false)
        win.want("the mark is black on the fill, never a coloured word",
                 win.colorOf(dlBadgeText[1]), Style.ink)

        // ---- and what the long press offers a book -------------------------
        //
        // **No watching.** Every check is a real round trip per watched series
        // (PLAN §12.2), and a book's releases are the files one finished book
        // already exists as — a watch on one buys a request to a slow instance
        // that can only ever answer "nothing new". Absent rather than greyed
        // out, and the same answer in both layouts, because a book that could
        // be watched here but not from its own series screen would read as one
        // of the two screens being broken.

        downloadedList.view = "list"
        win.findChild(downloadedList, "downloadedRows").forceLayout()
        var dlAreas = win.findChildren(downloadedList, "downloadedRowArea", [])
        win.holdOn(dlAreas[1])
        var bookMenu = win.menuLabels(downloadedList)
        win.want("a held book row opens a menu", bookMenu.length > 0, true)
        win.want("**offering no way to watch it**",
                 bookMenu.indexOf("Watch"), -1)
        win.want("not even one that is already watched",
                 bookMenu.indexOf("Stop watching"), -1)
        win.want("while still offering everything that means something",
                 bookMenu.join(","), "Browse,Delete everything from this series")
        win.want("and not one line of it counts chapters",
                 bookMenu.join(" | ").toLowerCase().indexOf("chapter"), -1)
        win.findChild(downloadedList, "contextMenuScrim").clicked(null)

        // Side by side with a comic, so the case cannot pass by the menu
        // simply having come back empty.
        win.holdOn(dlAreas[0])
        var comicMenu = win.menuLabels(downloadedList)
        win.want("**while a comic beside it still offers Watch**",
                 comicMenu.indexOf("Watch") >= 0, true)
        win.findChild(downloadedList, "contextMenuScrim").clicked(null)
        win.holdOn(dlAreas[3])
        var watchedMenu = win.menuLabels(downloadedList)
        win.want("and a watched comic still offers the way out",
                 watchedMenu.indexOf("Stop watching") >= 0, true)
        win.findChild(downloadedList, "contextMenuScrim").clicked(null)

        // The same answer in the grid, where there is no row and no button on
        // it: the layout switch decides how the screen looks, never what it
        // can do.
        downloadedList.view = "grid"
        win.findChild(downloadedList, "coverTiles").forceLayout()
        var dlTileAreas = win.findChildren(downloadedList, "coverTileArea", [])
        win.holdOn(dlTileAreas[1])
        var bookTileMenu = win.menuLabels(downloadedList)
        win.want("a held book tile opens a menu too", bookTileMenu.length > 0, true)
        win.want("**offering no way to watch it either**",
                 bookTileMenu.indexOf("Watch"), -1)
        win.want("nor to stop", bookTileMenu.indexOf("Stop watching"), -1)
        win.findChild(downloadedList, "contextMenuScrim").clicked(null)
        win.holdOn(dlTileAreas[0])
        win.want("**while a comic's tile still offers it**",
                 win.menuLabels(downloadedList).indexOf("Watch") >= 0, true)
        win.findChild(downloadedList, "contextMenuScrim").clicked(null)

        // The book above is watched, so the line it would have worn is "Stop
        // watching". The other half of the same rule, on a book nobody has ever
        // watched, where the line would read "Watch".
        downloadedModel.setProperty(1, "watched", false)
        downloadedList.view = "list"
        win.findChild(downloadedList, "downloadedRows").forceLayout()
        win.holdOn(win.findChildren(downloadedList, "downloadedRowArea", [])[1])
        var freshBookMenu = win.menuLabels(downloadedList)
        win.want("**an unwatched book is offered no Watch either**",
                 freshBookMenu.indexOf("Watch"), -1)
        win.want("and the rest of its menu is unchanged",
                 freshBookMenu.join(","), "Browse,Delete everything from this series")
        win.findChild(downloadedList, "contextMenuScrim").clicked(null)

        downloadedList.view = dlViewBefore
        downloadedList.visible = false

        // ---- the series screen's source switcher ---------------------------
        //
        // Opening a group opens its first match. The other sources become
        // chips, and tapping one re-requests the detail for *that* pair: the
        // chapters, the downloads and the watch record it switches to are all
        // that source's, and none of them are merged with the ones it left.

        var strip = win.findChild(chapterList, "sourceStrip")

        // A series reached the ordinary way -- browsing one source -- has no
        // alternatives, and offers no switcher rather than one inert chip.
        chapterList.sources = []
        win.want("browsing one source offers no switcher", strip.visible, false)
        win.want("and the strip takes no room", strip.height, 0)
        var pageWithoutChips = chapterList.pageSize

        chapterList.sources = [{"sourceId": "src-a", "sourceName": "Example Reader",
                                "seriesId": "/manga/lantern/"}]
        win.want("a group found in one source offers none either", strip.visible, false)

        chapterList.sources = [{"sourceId": "src-a", "sourceName": "Example Reader",
                                "seriesId": "/manga/lantern/"},
                               {"sourceId": "src-b", "sourceName": "Other Reader",
                                "seriesId": "/series/lantern"}]
        chapterList.currentSourceId = "src-a"
        win.want("a group found in several offers the switcher", strip.visible, true)
        win.want("and it costs the page some rows",
                 chapterList.pageSize < pageWithoutChips, true)

        var chipA = win.findChild(chapterList, "sourceChip-src-a")
        var chipB = win.findChild(chapterList, "sourceChip-src-b")
        win.want("there is a chip per source", chipA !== null && chipB !== null, true)
        win.want("named after the source",
                 win.findChild(chapterList, "sourceChipLabel-src-b").text, "Other Reader")

        // Which one you are reading is filled, not outlined: on e-ink a border
        // alone does not answer that at a glance.
        win.want("the source being read is marked", win.colorOf(chipA), Style.accent)
        win.want("and the others are not", win.colorOf(chipB), Style.paper)

        // `enabled` is the assertion, not a synthesised tap: emitting
        // clicked() invokes the handler whatever enabled says, which is how a
        // dead control passes a test written the other way.
        win.want("the source already showing is inert",
                 win.findChild(chapterList, "sourceChipArea-src-a").enabled, false)
        win.want("and the other is live",
                 win.findChild(chapterList, "sourceChipArea-src-b").enabled, true)

        win.sourceSwitches = 0
        win.findChild(chapterList, "sourceChipArea-src-b").clicked(null)
        win.want("tapping another source asks once", win.sourceSwitches, 1)
        win.want("for that source", win.switchedToSource, "src-b")
        win.want("under its own name", win.switchedToName, "Other Reader")
        // The other half of the pair, and the one a bug would drop: the series
        // id is that source's, not the one the screen was just showing.
        win.want("and that source's own series id", win.switchedToSeries, "/series/lantern")

        // The mark follows the source being read, which Main.qml binds to the
        // pair it is acting on -- so the chip cannot end up naming a source
        // whose chapters are not the ones on screen.
        chapterList.currentSourceId = "src-b"
        win.want("the mark moves with the source", win.colorOf(chipB), Style.accent)
        win.want("and off the one left behind", win.colorOf(chipA), Style.paper)
        win.want("which is live again",
                 win.findChild(chapterList, "sourceChipArea-src-a").enabled, true)
        win.want("while the one being read is inert",
                 win.findChild(chapterList, "sourceChipArea-src-b").enabled, false)

        // The combined search is a search, so it draws in the layout the
        // search screen is stored in -- the same switch, in the same place,
        // writing the same setting (Views.js).
        win.want("the combined search is a search on the wire",
                 Views.wireName("searchall"), "search")
        win.want("so the header offers it the switch", Views.remembers("searchall"), true)
        win.want("and it reads the search screen's own setting",
                 Views.fromStatus(stored, "searchall"), "list")

        chapterList.sources = []
        chapterList.currentSourceId = ""

        // ---- the long-press menu (ui/ContextMenu.qml) ----------------------
        //
        // One menu, three screens, **both layouts of each**. The layout switch
        // is allowed to change how a screen looks and nothing else, so every
        // case below is run against the rows and against the tiles, and the
        // comparison is the assertion: the same set of actions, in the same
        // order, from the same hold.
        //
        // The hold itself is driven through the area's own `held`, so the
        // delegate's handler, the coordinate mapping and the screen's item
        // list are all in the path. *When* it fires is a different question
        // and is asked at the foot of this file, against real timers.

        // ---- search results ------------------------------------------------

        seriesGrid.view = "list"
        seriesGrid.busy = false
        seriesGrid.pendingPage = 0
        seriesModel.setProperty(0, "watched", false)
        var seriesRows = win.findChildren(seriesGrid, "seriesRowArea", [])

        win.want("no menu is showing to begin with", win.menuActions(seriesGrid).length, 0)
        win.holdOn(seriesRows[0])
        win.want("a held row opens the menu", win.menuActions(seriesGrid).join(","), "open,watch")
        win.want("in words rather than glyphs",
                 win.menuLabels(seriesGrid).join(","), "Open,Watch")

        // Watch or Stop watching, never both and never the wrong one: the row
        // reads the store's answer (Watch.js markWatched), it does not
        // remember what was tapped.
        seriesModel.setProperty(0, "watched", true)
        win.holdOn(seriesRows[0])
        win.want("a watched result offers to stop",
                 win.menuActions(seriesGrid).join(","), "open,unwatch")
        win.want("and says so", win.menuLabels(seriesGrid).join(","), "Open,Stop watching")

        win.seriesUnwatches = 0
        win.tapMenu(seriesGrid, "unwatch")
        win.want("choosing it asks once", win.seriesUnwatches, 1)
        win.want("about that row's series", win.seriesUnwatchedSeries, "x0")
        win.want("and the menu is gone", win.menuActions(seriesGrid).length, 0)

        seriesModel.setProperty(1, "watched", false)
        win.seriesWatches = 0
        win.seriesOpens = 0
        win.holdOn(seriesRows[1])
        win.tapMenu(seriesGrid, "watch")
        win.want("Watch asks once", win.seriesWatches, 1)
        win.want("about the row that was held", win.seriesWatchedSeries, "x1")
        win.want("and opens nothing", win.seriesOpens, 0)

        // Open is on the menu too, and goes exactly where a tap would.
        win.holdOn(seriesRows[1])
        win.tapMenu(seriesGrid, "open")
        win.want("Open from the menu opens once", win.seriesOpens, 1)
        win.want("the series that row names", win.seriesOpenedId, "x1")

        // The same menu on the tiles. This is the equivalence, asserted
        // directly rather than implied by two similar-looking lists.
        seriesGrid.view = "grid"
        seriesGrid.busy = false
        seriesGrid.pendingPage = 0
        win.findChild(searchTiles, "coverTiles").forceLayout()
        var seriesTiles = win.findChildren(searchTiles, "coverTileArea", [])
        var rowActions = "open,watch"
        seriesModel.setProperty(1, "watched", false)
        win.holdOn(seriesTiles[1])
        win.want("a held tile opens the same menu",
                 win.menuActions(seriesGrid).join(","), rowActions)
        win.seriesWatches = 0
        win.tapMenu(seriesGrid, "watch")
        win.want("and it acts on the tile's own row", win.seriesWatchedSeries, "x1")
        win.want("asking once", win.seriesWatches, 1)

        // ---- Downloaded ----------------------------------------------------

        downloadedRows([
            {"sourceId": "src-a", "sourceName": "Example Reader",
             "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
             "detail": "3 downloads", "openable": true, "note": "",
             "coverUrl": "", "latestUuid": "doc-1", "watched": false},
            // Downloaded before the newest document was recorded — there is
            // nothing this row could open.
            {"sourceId": "src-b", "sourceName": "Other Reader",
             "seriesId": "/series/orphan/", "title": "An Orphan",
             "detail": "1 download", "openable": true, "note": "",
             "coverUrl": "", "latestUuid": "", "watched": true},
            // Two orphans: one whose newest download is still recorded, one
            // from before the record existed. Both keep Delete; only the first
            // has anything to read.
            {"sourceId": "gone", "sourceName": "gone", "seriesId": "/manga/lost/",
             "title": "Lost", "detail": "2 downloads", "openable": false,
             "note": "The source this came from has been removed.",
             "coverUrl": "", "latestUuid": "doc-orphan"},
            {"sourceId": "gone", "sourceName": "gone", "seriesId": "/manga/older/",
             "title": "Older Still", "detail": "1 download", "openable": false,
             "note": "The source this came from has been removed.", "coverUrl": ""}])

        downloadedList.view = "list"
        var dlRowAreas = win.findChildren(downloadedList, "downloadedRowArea", [])
        win.holdOn(dlRowAreas[0])
        win.want("a held download row offers all four",
                 win.menuActions(downloadedList).join(","), "open,read,watch,delete")
        win.want("in the screen's own words",
                 win.menuLabels(downloadedList).join(","),
                 "Browse,Read latest,Watch,Delete everything from this series")

        // **Absent, not greyed out.** A row with nothing openable must not
        // offer to open it.
        win.holdOn(dlRowAreas[1])
        win.want("a row with no newest document offers no Read",
                 win.menuActions(downloadedList).join(","), "open,unwatch,delete")
        win.want("and nothing on the menu claims otherwise",
                 win.menuLabels(downloadedList).join(",").indexOf("Read latest"), -1)

        win.downloadedReads = 0
        win.holdOn(dlRowAreas[0])
        win.tapMenu(downloadedList, "read")
        win.want("Read latest opens once", win.downloadedReads, 1)
        win.want("the document that row carries", win.downloadedReadUuid, "doc-1")

        win.downloadedWatches = 0
        win.holdOn(dlRowAreas[0])
        win.tapMenu(downloadedList, "watch")
        win.want("Watch asks once", win.downloadedWatches, 1)
        win.want("about that row's series", win.downloadedWatchedSeries, "/manga/lantern/")

        // **Delete routes into the question, and deletes nothing.** The hold
        // made the choice deliberate; it did not make it safe, and the strip
        // the backend's sentence lands in is unchanged.
        win.downloadedDeleteAsks = 0
        win.downloadedDeletes = 0
        win.holdOn(dlRowAreas[0])
        win.tapMenu(downloadedList, "delete")
        win.want("Delete asks the backend for its question", win.downloadedDeleteAsks, 1)
        win.want("about that series", win.downloadedAskedSeries, "/manga/lantern/")
        win.want("and deletes nothing on the way", win.downloadedDeletes, 0)

        // The row's own button and the menu end in the same place: one
        // handler, reached two ways.
        win.downloadedDeleteAsks = 0
        win.findChildren(downloadedList, "deleteSeriesArea", [])[0].clicked(null)
        win.want("the row's button asks for the same thing", win.downloadedDeleteAsks, 1)
        win.want("about the same series", win.downloadedAskedSeries, "/manga/lantern/")

        // The tiles, again identical.
        downloadedList.view = "grid"
        win.findChild(dlTiles, "coverTiles").forceLayout()
        var dlHeldTiles = win.findChildren(dlTiles, "coverTileArea", [])
        win.holdOn(dlHeldTiles[0])
        win.want("a held tile offers the same four",
                 win.menuActions(downloadedList).join(","), "open,read,watch,delete")
        win.downloadedDeleteAsks = 0
        win.downloadedDeletes = 0
        win.tapMenu(downloadedList, "delete")
        win.want("and Delete from a tile asks the same question",
                 win.downloadedDeleteAsks, 1)
        win.want("without deleting either", win.downloadedDeletes, 0)

        // ---- a series whose source has been removed ------------------------
        //
        // The downloads are still on the tablet and still the user's to be rid
        // of — that is the only reason these rows are listed. In the list
        // layout Delete is the button on the row; in the grid there is no row
        // and no button, so without a menu an orphaned series could be deleted
        // in one layout and **not at all** in the other. The menu is shorter
        // rather than absent: everything that needs the source is left out,
        // because offering something that cannot work is worse than not
        // offering it.
        win.holdOn(dlHeldTiles[2])
        win.want("an orphaned tile still offers a menu",
                 win.menuActions(downloadedList).join(","), "read,delete")
        win.want("in words", win.menuLabels(downloadedList).join(","),
                 "Read latest,Delete everything from this series")
        // The absences are asserted as absences, one action at a time: a menu
        // that quietly grew an Open would otherwise pass every case above.
        win.want("with no Open, which has nowhere to go",
                 win.menuItemFor(downloadedList, "open"), null)
        win.want("and nothing to watch for new chapters",
                 win.menuItemFor(downloadedList, "watch"), null)
        win.want("nor to stop watching",
                 win.menuItemFor(downloadedList, "unwatch"), null)
        win.want("and no label offering either",
                 win.menuLabels(downloadedList).join(",").indexOf("Open"), -1)

        // Delete from it goes into the same question as every other Delete,
        // and deletes nothing by itself.
        win.downloadedDeleteAsks = 0
        win.downloadedDeletes = 0
        win.tapMenu(downloadedList, "delete")
        win.want("Delete on an orphan asks the question", win.downloadedDeleteAsks, 1)
        win.want("about the orphaned series", win.downloadedAskedSeries, "/manga/lost/")
        win.want("and deletes nothing on the way", win.downloadedDeletes, 0)

        // Read latest survives losing the source: OpenInReader takes a
        // document uuid and asks the source nothing.
        win.downloadedReads = 0
        win.holdOn(dlHeldTiles[2])
        win.tapMenu(downloadedList, "read")
        win.want("an orphan can still be read", win.downloadedReads, 1)
        win.want("from the document it carries", win.downloadedReadUuid, "doc-orphan")

        // An orphan from before the newest download was recorded has nothing
        // to read, and says so by not offering it.
        win.holdOn(dlHeldTiles[3])
        win.want("an orphan with no document offers only Delete",
                 win.menuActions(downloadedList).join(","), "delete")
        win.want("and no Read latest", win.menuItemFor(downloadedList, "read"), null)

        // The same menu in the list layout, which is the whole point: the tap
        // target stays inert — there is still nothing to open — and the hold
        // is what the second area on the row is for.
        downloadedList.view = "list"
        win.findChild(downloadedList, "downloadedRows").forceLayout()
        var dlTapAreas = win.findChildren(downloadedList, "downloadedRowArea", [])
        var dlOrphanAreas = win.findChildren(downloadedList, "downloadedOrphanArea", [])
        win.want("an orphaned row still cannot be tapped open",
                 dlTapAreas[2].enabled, false)
        win.want("but it can be held", dlOrphanAreas[2].enabled, true)
        // And the other way round on a row that opens: exactly one of the two
        // is live, so neither is ever in the other's way.
        win.want("a live row is tapped on its own area", dlTapAreas[0].enabled, true)
        win.want("and holds on it too", dlOrphanAreas[0].enabled, false)

        win.holdOn(dlOrphanAreas[2])
        win.want("a held orphaned row offers the same menu as its tile",
                 win.menuActions(downloadedList).join(","), "read,delete")
        win.want("with no Open there either",
                 win.menuItemFor(downloadedList, "open"), null)
        win.downloadedDeleteAsks = 0
        win.downloadedDeletes = 0
        win.tapMenu(downloadedList, "delete")
        win.want("and Delete from the row asks the same question",
                 win.downloadedDeleteAsks, 1)
        win.want("about the same series", win.downloadedAskedSeries, "/manga/lost/")
        win.want("without deleting either", win.downloadedDeletes, 0)

        // ---- saved in Quire: "Read latest" prefers a saved chapter --------
        //
        // A series can be here from saved chapters alone (backend/service/
        // downloaded.go), and a row that has both a library document and a
        // newer saved chapter still opens the saved one — it is the newest
        // of the two, and reading it goes through OpenSaved, not the
        // library handoff. A fresh downloadedRows() call, since every test
        // above this point is done with the rows it set up.
        downloadedRows([
            {"sourceId": "src-c", "sourceName": "Example Reader",
             "seriesId": "/manga/saved-only/", "title": "Saved Only",
             "detail": "3 chapters saved in Quire", "openable": true, "note": "",
             "coverUrl": "", "latestUuid": "", "latestSavedChapterId": "c9", "watched": false},
            {"sourceId": "src-d", "sourceName": "Example Reader",
             "seriesId": "/manga/both/", "title": "Both Kinds",
             "detail": "1 chapter saved in Quire · 2 in your library", "openable": true,
             "note": "", "coverUrl": "", "latestUuid": "doc-both",
             "latestSavedChapterId": "c12", "watched": false}])
        downloadedList.view = "list"
        var savedRowAreas = win.findChildren(downloadedList, "downloadedRowArea", [])

        win.holdOn(savedRowAreas[0])
        win.want("a saved-only row offers Read latest too",
                 win.menuActions(downloadedList).join(","), "open,read,watch,delete")

        win.downloadedSavedReads = 0
        win.downloadedReads = 0
        win.tapMenu(downloadedList, "read")
        win.want("it asks for OpenSaved, not the library handoff",
                 win.downloadedSavedReads, 1)
        win.want("naming the source", win.downloadedSavedReadSource, "src-c")
        win.want("the series", win.downloadedSavedReadSeries, "/manga/saved-only/")
        win.want("and the chapter", win.downloadedSavedReadChapter, "c9")
        win.want("never the library Read for this row", win.downloadedReads, 0)

        win.holdOn(savedRowAreas[1])
        win.downloadedSavedReads = 0
        win.downloadedReads = 0
        win.tapMenu(downloadedList, "read")
        win.want("a row with both kinds still prefers the saved chapter",
                 win.downloadedSavedReads, 1)
        win.want("naming it", win.downloadedSavedReadChapter, "c12")
        win.want("not the library document this time either", win.downloadedReads, 0)

        // ---- continue reading on a tap (PLAN §12.9) ------------------------
        //
        // A tap used to open the series' own results list, which fetches
        // from the web first and is slow. It continues reading instead now,
        // following the backend's "continue" target; the menu's Browse (the
        // old "Open", relabelled above) is what still opens the series.
        downloadedRows([
            {"sourceId": "src-e", "sourceName": "Example Reader",
             "seriesId": "/manga/continue-saved/", "title": "Continue Saved",
             "detail": "1 chapter saved in Quire", "openable": true, "note": "",
             "continueKind": "saved", "continueChapterId": "c7"},
            {"sourceId": "src-f", "sourceName": "Example Reader",
             "seriesId": "/manga/continue-library/", "title": "Continue Library",
             "detail": "1 download", "openable": true, "note": "",
             "continueKind": "library", "continueDocumentUuid": "doc-continue"},
            {"sourceId": "src-g", "sourceName": "Example Reader",
             "seriesId": "/manga/continue-none/", "title": "Continue None",
             "detail": "1 download", "openable": true, "note": ""}])
        downloadedList.view = "list"
        var continueRowAreas = win.findChildren(downloadedList, "downloadedRowArea", [])

        win.downloadedSavedReads = 0
        win.downloadedReads = 0
        win.downloadedOpens = 0
        continueRowAreas[0].clicked(null)
        win.want("a saved continue target sends OpenSaved", win.downloadedSavedReads, 1)
        win.want("naming the chapter to continue at", win.downloadedSavedReadChapter, "c7")
        win.want("and opens no series", win.downloadedOpens, 0)

        win.downloadedReads = 0
        win.downloadedOpens = 0
        continueRowAreas[1].clicked(null)
        win.want("a library continue target hands off to the reader", win.downloadedReads, 1)
        win.want("naming the document", win.downloadedReadUuid, "doc-continue")
        win.want("and opens no series", win.downloadedOpens, 0)

        win.downloadedReads = 0
        win.downloadedSavedReads = 0
        win.downloadedOpens = 0
        continueRowAreas[2].clicked(null)
        win.want("a row with nothing to continue falls back to Browse",
                 win.downloadedOpens, 1)
        win.want("naming its own series", win.downloadedOpenedSeries, "/manga/continue-none/")
        win.want("and asks for no read", win.downloadedReads + win.downloadedSavedReads, 0)

        // The tile grid follows exactly the same rule.
        downloadedList.view = "grid"
        win.findChild(dlTiles, "coverTiles").forceLayout()
        var continueTiles = win.findChildren(dlTiles, "coverTileArea", [])
        win.downloadedSavedReads = 0
        win.downloadedOpens = 0
        continueTiles[0].clicked(null)
        win.want("a saved continue target sends OpenSaved from a tile too",
                 win.downloadedSavedReads, 1)
        win.want("opening no series", win.downloadedOpens, 0)

        // The menu's Browse still opens the series, even on a row that
        // continues reading on a tap.
        win.holdOn(continueRowAreas[0])
        win.want("Browse is still on the menu",
                 win.menuLabels(downloadedList).join(","),
                 "Browse,Watch,Delete everything from this series")
        win.downloadedOpens = 0
        win.tapMenu(downloadedList, "open")
        win.want("and it opens the series, not the continue target",
                 win.downloadedOpens, 1)
        win.want("naming that series", win.downloadedOpenedSeries, "/manga/continue-saved/")

        // ---- Watching ------------------------------------------------------

        WatchJs.reconcile(watchedModel, [
            {"sourceId": "src", "seriesId": "w-new", "sourceName": "Example Reader",
             "title": "Watched With News", "newChapters": 3, "badge": "3 new chapters",
             "state": "new", "status": "3 new chapters"},
            {"sourceId": "src", "seriesId": "w-quiet", "sourceName": "Example Reader",
             "title": "Watched Quietly", "newChapters": 0, "badge": "",
             "state": "ok", "status": "Up to date"}])

        watchList.view = "list"
        watchList.closeStrip()
        win.findChild(watchList, "watchRows").forceLayout()
        var wRowAreas = win.findChildren(watchList, "watchRowArea", [])
        win.holdOn(wRowAreas[0])
        win.want("a held watch row offers all four",
                 win.menuActions(watchList).join(","), "open,download,seen,unwatch")
        win.want("in the screen's own words", win.menuLabels(watchList).join(","),
                 "Browse,Download new chapters,Mark as seen,Stop watching")
        win.want("and the two about new chapters are live",
                 win.menuLive(watchList, "download") && win.menuLive(watchList, "seen"), true)

        // **Disabled at zero, not hidden.** `enabled` is the assertion: a
        // synthesised clicked() would run the handler whatever it says, which
        // is exactly how a greyed-out item that is still live passes.
        win.holdOn(wRowAreas[1])
        win.want("a series with nothing new still lists both",
                 win.menuActions(watchList).join(","), "open,download,seen,unwatch")
        win.want("but Download new chapters cannot be used",
                 win.menuLive(watchList, "download"), false)
        win.want("nor Mark as seen", win.menuLive(watchList, "seen"), false)
        win.want("while the rest of the menu is live",
                 win.menuLive(watchList, "open") && win.menuLive(watchList, "unwatch"), true)

        win.watchDownloadNews = 0
        win.holdOn(wRowAreas[0])
        win.tapMenu(watchList, "download")
        win.want("Download new chapters asks once", win.watchDownloadNews, 1)
        win.want("for the row that was held", win.watchDownloadNewSeries, "w-new")

        // Mark as seen sends and waits. The badge is the backend's to clear —
        // it answers with a watch update and then the whole list — so nothing
        // here touches the row.
        win.watchMarkSeens = 0
        win.holdOn(wRowAreas[0])
        win.tapMenu(watchList, "seen")
        win.want("Mark as seen asks once", win.watchMarkSeens, 1)
        win.want("for that row", win.watchMarkSeenSeries, "w-new")
        win.want("and the badge is untouched until the backend answers",
                 watchedModel.get(0).badge, "3 new chapters")

        // Stop watching opens the strip this screen already had, and drops
        // nothing by itself.
        var wStrip = win.findChild(watchList, "watchStrip")
        win.watchUnwatches = 0
        win.holdOn(wRowAreas[0])
        win.tapMenu(watchList, "unwatch")
        win.want("Stop watching opens the question", wStrip.visible, true)
        win.want("about the row that was held", watchList.stripSeriesId, "w-new")
        win.want("and stops nothing yet", win.watchUnwatches, 0)
        win.findChild(watchList, "unwatchButton").children[1].clicked(null)
        win.want("answering it is what stops the watch", win.watchUnwatches, 1)
        win.want("and the question closes behind it", wStrip.visible, false)

        // The tiles, which had none of this before: the whole screen was
        // navigation, so preferring covers meant giving up Stop watching.
        watchList.view = "grid"
        win.findChild(wTiles, "coverTiles").forceLayout()
        var wHeldTiles = win.findChildren(wTiles, "coverTileArea", [])
        win.holdOn(wHeldTiles[0])
        win.want("a held tile offers the same four",
                 win.menuActions(watchList).join(","), "open,download,seen,unwatch")
        win.watchUnwatches = 0
        win.tapMenu(watchList, "unwatch")
        win.want("and Stop watching from a tile opens the same question",
                 wStrip.visible, true)
        win.want("without stopping anything", win.watchUnwatches, 0)
        watchList.closeStrip()

        // A tile with nothing new is as disabled as its row was.
        win.holdOn(wHeldTiles[1])
        win.want("a quiet tile cannot download new chapters",
                 win.menuLive(watchList, "download"), false)
        win.want("nor mark them seen", win.menuLive(watchList, "seen"), false)

        // ---- dismissal, and where the panel lands --------------------------

        var wMenu = win.findChild(watchList, "watchMenu")
        var wPanel = win.findChild(wMenu, "contextMenuPanel")
        var wScrim = win.findChild(wMenu, "contextMenuScrim")

        wScrim.clicked(null)
        win.want("a closed menu is not drawn", wMenu.visible, false)
        // A dismiss target left live over a closed menu would swallow the next
        // tap meant for the page.
        win.want("and cannot swallow a tap", wScrim.enabled, false)

        win.holdOn(wHeldTiles[0])
        win.want("an open menu is drawn", wMenu.visible, true)
        win.want("and its dismiss target is live", wScrim.enabled, true)
        wScrim.clicked(null)
        win.want("a tap outside puts it away", wMenu.visible, false)
        win.want("with nothing left of it", win.menuActions(watchList).length, 0)
        win.watchUnwatches = 0
        win.want("and nothing was chosen on the way out", win.watchUnwatches, 0)

        // Anchored near the press and clamped inside the screen. A menu held
        // open past the edge would put items where no finger can reach them,
        // and this screen has no scrolling to recover them with.
        win.holdOn(wHeldTiles[0], wHeldTiles[0].width * 4, wHeldTiles[0].height * 40)
        win.want("a menu held near the corner stays on screen",
                 wPanel.x + wPanel.width <= wMenu.width - Style.margin, true)
        win.want("and off neither edge", wPanel.x >= Style.margin, true)
        win.want("with its last item still on the panel",
                 wPanel.y + wPanel.height <= wMenu.height - Style.margin, true)
        win.want("and its first still under the top", wPanel.y >= Style.margin, true)

        // It never scrolls: four items at a full row each, and they fit.
        win.want("the menu is as tall as its items and no taller",
                 wPanel.height >= 4 * Style.rowHeight, true)
        win.want("and still fits the screen", wPanel.height < wMenu.height, true)

        // Held at the very top left: the panel is pushed in to the margin
        // rather than drawn half off the screen.
        win.holdOn(wHeldTiles[0], -500, -500)
        win.want("a menu held past the top left is pushed in", wPanel.x, Style.margin)
        win.want("on both axes", wPanel.y, Style.margin)
        wScrim.clicked(null)

        // ---- what the press feedback actually draws ------------------------
        //
        // `feedback` going true is half of it; the other half is the thing the
        // user sees, which is a different object in each layout. A tile is
        // covered by its own picture, so the fill it would darken is behind
        // the cover — the border is what has to carry it there.
        var searchTile = win.findChild(searchTiles, "coverTile")
        var searchTileArea = win.findChild(searchTiles, "coverTileArea")
        win.want("a tile at rest has a hairline border", searchTile.border.width, 1)
        searchTileArea.feedback = true
        win.want("a held tile draws a heavier border", searchTile.border.width > 1, true)
        win.want("in ink, which is visible over a cover",
                 win.colorOf(searchTile.border), Style.ink)
        win.want("and darkens its fill too", win.colorOf(searchTile), Style.pressed)
        searchTileArea.feedback = false
        win.want("and goes back when the press ends", searchTile.border.width, 1)

        // Every list layout has the same acknowledgement, because a row has no
        // picture to darken and the whole row is it.
        win.want("search rows can darken",
                 win.findChildren(seriesGrid, "rowPressFeedback", []).length > 0, true)
        win.want("watch rows can darken",
                 win.findChildren(watchList, "rowPressFeedback", []).length > 0, true)
        win.want("downloaded rows can darken",
                 win.findChildren(downloadedList, "rowPressFeedback", []).length > 0, true)

        // ---- which rows offer Watch, and which offer Stop watching ---------
        //
        // Neither screen owns the watch record, so neither may remember what
        // was tapped: the flag is re-derived from the watched model, which is
        // the only thing that knows whether the backend agreed (Watch.js).
        var otherRows = Qt.createQmlObject(
            'import QtQuick 2.5; ListModel {}', win, "markWatchedRows")
        otherRows.append({"seriesId": "w-new", "watched": false})
        otherRows.append({"seriesId": "nobody-watches-this", "watched": true})
        win.want("the flag follows the store",
                 WatchJs.markWatched(otherRows, watchedModel, "src"), 2)
        win.want("a watched series says so", otherRows.get(0).watched, true)
        win.want("and one nobody watches says that", otherRows.get(1).watched, false)
        // The same list again writes nothing: this runs on every watch, every
        // unwatch and at the end of every check round, and a round with no
        // news must repaint nothing.
        win.want("a second pass changes nothing",
                 WatchJs.markWatched(otherRows, watchedModel, "src"), 0)
        // A row that names its own source is answered for that source, not for
        // whichever one is being browsed — the same series can be watched on
        // one source and not on another.
        var pairRows = Qt.createQmlObject(
            'import QtQuick 2.5; ListModel {}', win, "markWatchedPairs")
        pairRows.append({"sourceId": "elsewhere", "seriesId": "w-new", "watched": false})
        win.want("a row's own source is what is asked about",
                 WatchJs.markWatched(pairRows, watchedModel, "src"), 0)
        win.want("so it is not watched", pairRows.get(0).watched, false)

        // ---- the accent: one colour, and only ever a solid area ------------
        //
        // PLAN §6 M3 as amended twice. One colour (Style.accent), spent only
        // on live state, on what is currently active, and on headings — and,
        // since it was measured on hardware, spent only on *shapes*: a filled
        // rule under a heading, a small solid square beside a line, or a fill
        // behind black text. Coloured text read muddy on this panel, because
        // black is drawn at the panel's full monochrome resolution while
        // colour is composed through the colour filter array, and a letterform
        // is almost entirely edge (ui/Style.js).
        //
        // Three kinds of assertion follow, and all three matter:
        //
        //   1. **The value itself**, pinned by what it has to do rather than
        //      by its hex: it is a colour, it is deep, black clears 4.5:1 on
        //      it and it clears 3:1 on paper. Every `=== Style.accent` below
        //      passes with the token set to black; these are the ones that do
        //      not.
        //   2. **No text anywhere carries it.** The sweep at the end of this
        //      section, over every screen at once and over every Text on them
        //      rather than a named list. That is the regression this change
        //      exists to prevent, and the heading somebody recolours later is
        //      exactly the one that will not be in a list.
        //   3. **Every case comes in pairs.** Asserting that something is
        //      Style.accent is nearly worthless on its own: it passes on a
        //      screen where the colour has become the *only* thing separating
        //      two states. So each accented shape is checked together with the
        //      signal that has to survive the colour being invisible — the
        //      active half of the switch is still filled and still dead to
        //      touch, the current chip is still filled, the badge is still
        //      drawn only when there is news and still spells the count out,
        //      the failed line still names its sources in words.

        var acc = win.channels(Style.accent)
        win.want("the accent is not the ordinary ink", Style.accent !== Style.ink, true)
        win.want("nor the paper under it", Style.accent !== Style.paper, true)
        win.want("nor either grey", Style.accent !== Style.muted
                                    && Style.accent !== Style.rule, true)
        // A grey has three equal channels, whatever its value. This is what
        // "there is now a colour" means, stated without naming the colour.
        win.want("it is a colour rather than another grey",
                 acc[0] !== acc[1] || acc[1] !== acc[2], true)
        win.want("and a warm one, which is what was chosen", acc[0] > acc[2], true)
        // Deep, not pale: Gallery 3 renders every hue lighter than a monitor
        // does, and a fill that washes toward cream stops reading as coloured
        // at all. Stated as "darker than halfway to paper" rather than as a
        // number, so it is the decision that is pinned and not the hex:
        // #F97316, the pale candidate that was rejected, fails this.
        win.want("and deep rather than pale",
                 acc[0] + acc[1] + acc[2] < win.channels(Style.paper)[0] * 3 / 2, true)

        // The contrast trap this change had to walk around, stated as the two
        // numbers that decided the value. The accent carries no text, so it
        // owes paper nothing more than the 3:1 a graphical object needs — but
        // black text sits *on* it in three places, and that is the pair that
        // is easy to get wrong, because it gets worse as the colour gets
        // deeper. #C2410C, the previous accent, is the proof: it was chosen
        // for 5.2:1 against paper and black on it is only 4.06:1, which is
        // worse than the coloured text it would have replaced.
        win.want("black text on an accent fill clears 4.5:1",
                 win.contrast(Style.accent, Style.ink) >= 4.5, true)
        win.want("which the deeper orange it replaced did not",
                 win.contrast("#C2410C", Style.ink) >= 4.5, false)
        win.want("and a shape drawn in the accent clears 3:1 on paper",
                 win.contrast(Style.accent, Style.paper) >= 3.0, true)
        // And it is still dark enough that a fill reads as the filled one with
        // the hue taken away — which is what keeps "colour is never the only
        // signal" true for the toggle and the chip, both of which now use the
        // accent as their fill.
        win.want("a fill in it is much darker than the paper around it",
                 win.luminance(Style.accent) < win.luminance(Style.paper) / 3, true)

        // The two shapes, pinned as shapes. A 1px coloured line is the same
        // failure as a coloured glyph drawn sideways, so the rule has to be
        // thick enough to be an area, and the mark has to be a real square
        // rather than a decorative speck.
        win.want("the accent rule is an area, not a hairline",
                 Style.accentRule >= Style.hairline * 4, true)
        win.want("and the mark is at least as tall as the type it sits beside",
                 Style.accentMark >= Style.smallSize, true)
        // But not so large that a page of rows is mostly orange: a mark is
        // smaller than a touch target and a rule is thinner than a line of
        // text.
        win.want("while the mark is far smaller than a row",
                 Style.accentMark < Style.rowHeight / 4, true)
        win.want("and the rule far thinner than the heading over it",
                 Style.accentRule < Style.headingSize / 2, true)

        // ---- what is currently active: the layout switch -------------------

        viewToggle.view = Views.GRID
        var accGrid = win.findChild(viewToggle, "viewToggleGrid")
        var accList = win.findChild(viewToggle, "viewToggleList")
        // The fill is the accent now. It was a grey fill with an accent border
        // and an accent word: a 2px coloured border is a thin stroke and the
        // word was a coloured glyph, so both went and the colour moved onto
        // the area that was already carrying the meaning.
        win.want("the half you are in is filled with the accent",
                 win.colorOf(accGrid), Style.accent)
        win.want("and the half you are not in is paper",
                 win.colorOf(accList), Style.paper)
        win.want("its word is black on that fill",
                 win.colorOf(win.findChild(viewToggle, "viewToggleLabelGrid")), Style.ink)
        win.want("and the other half's word is the grey it always was",
                 win.colorOf(win.findChild(viewToggle, "viewToggleLabelList")), Style.muted)
        win.want("neither half has a coloured outline",
                 win.colorOf(accGrid.border) === Style.rule
                 && win.colorOf(accList.border) === Style.rule, true)
        // The signals that were there before the colour and must still be:
        // the fill, the deadness, and both words on screen either way.
        win.want("the active half is still the filled one",
                 win.colorOf(accGrid) !== win.colorOf(accList), true)
        win.want("and still inert to a finger",
                 win.findChild(viewToggle, "viewToggleGridArea").enabled, false)
        win.want("with the other still live",
                 win.findChild(viewToggle, "viewToggleListArea").enabled, true)
        win.want("and both layouts still named",
                 win.visibleKeyLabels(viewToggle).join(","), "Grid,List")

        // The colour follows the layout rather than the position — a mark
        // stuck on the left-hand half would pass every assertion above.
        viewToggle.view = Views.LIST
        win.want("the accent moves with the layout",
                 win.colorOf(accList), Style.accent)
        win.want("and leaves the half behind it", win.colorOf(accGrid), Style.paper)
        win.want("as does the black label",
                 win.colorOf(win.findChild(viewToggle, "viewToggleLabelList")), Style.ink)
        win.want("and the deadness",
                 win.findChild(viewToggle, "viewToggleListArea").enabled, false)
        viewToggle.view = Views.GRID

        // ---- what is currently active: the source chip ---------------------

        chapterList.sources = [{"sourceId": "src-a", "sourceName": "Example Reader",
                                "seriesId": "/manga/lantern/"},
                               {"sourceId": "src-b", "sourceName": "Other Reader",
                                "seriesId": "/series/lantern"}]
        chapterList.currentSourceId = "src-a"
        var accChipA = win.findChild(chapterList, "sourceChip-src-a")
        var accChipB = win.findChild(chapterList, "sourceChip-src-b")
        // The fill was already what marked this chip; it is the accent now
        // instead of the grey, and the accent came off the border. That was
        // only possible because the colour changed with it: black on the old
        // deep orange was the one pairing worse than the grey it replaced,
        // which is the assertion two sections up.
        win.want("the chip for the source being read is filled with the accent",
                 win.colorOf(accChipA), Style.accent)
        win.want("and the others stay paper", win.colorOf(accChipB), Style.paper)
        win.want("its label is black on the fill",
                 win.colorOf(win.findChild(chapterList, "sourceChipLabel-src-a")), Style.ink)
        win.want("and every chip keeps the same ink outline",
                 win.colorOf(accChipA.border) === Style.ink
                 && win.colorOf(accChipB.border) === Style.ink, true)
        win.want("while the current chip is still the filled one",
                 win.colorOf(accChipA) !== win.colorOf(accChipB), true)
        win.want("and still inert",
                 win.findChild(chapterList, "sourceChipArea-src-a").enabled, false)
        win.want("and still named in words",
                 win.findChild(chapterList, "sourceChipLabel-src-a").text, "Example Reader")

        // ---- what is currently active: the page indicator ------------------

        lonePager.busy = false
        lonePager.hasMore = true
        lonePager.page = 3
        lonePager.totalPages = 12
        var accPagerLabel = win.findChild(lonePager, "pagerLabel")
        var accPagerMark = win.findChild(lonePager, "pagerMark")
        // A solid square beside the words, both of them on paper. The words
        // themselves used to be the accent and that is what read muddy.
        win.want("the page indicator carries an accent mark",
                 win.colorOf(accPagerMark), Style.accent)
        win.want("which is a square area rather than a stroke",
                 accPagerMark.width === Style.accentMark
                 && accPagerMark.height === Style.accentMark, true)
        win.want("and it sits beside the words, not behind them",
                 accPagerMark.x + accPagerMark.width <= accPagerLabel.x
                 + (accPagerLabel.width - accPagerLabel.contentWidth) / 2, true)
        win.want("while the indicator itself is ink",
                 win.colorOf(accPagerLabel), Style.ink)
        win.want("and still says where you are in words",
                 accPagerLabel.text, "Page 3 of 12")
        // One marked thing in the bar, not three. Marking the buttons too
        // would leave the accent meaning "pager" rather than "you are here".
        win.want("the buttons either side stay ink",
                 win.colorOf(win.findChild(lonePager, "pagerPrevious").border), Style.ink)
        win.want("both of them",
                 win.colorOf(win.findChild(lonePager, "pagerNext").border), Style.ink)

        // ---- continue reading on a tap (PLAN §12.9) ------------------------
        //
        // A watch row's tap continues reading the same way a downloaded
        // row's does — the same "continue" target, the same fallback to
        // Browse (the menu's relabelled "Open") when there is nothing to
        // continue yet. Cleared and rebuilt from empty, like every other
        // fresh scenario in this file — nothing after this point in the
        // Watching section holds a reference into what was here before.
        watchList.view = "list"
        watchedModel.clear()
        watchedModel.append(WatchJs.row({
            "sourceId": "src", "seriesId": "w-continue-saved", "sourceName": "Example Reader",
            "title": "Continue Saved", "state": "ok", "status": "Up to date",
            "continue": {"kind": "saved", "chapterId": "c7"}}))
        watchedModel.append(WatchJs.row({
            "sourceId": "src", "seriesId": "w-continue-library", "sourceName": "Example Reader",
            "title": "Continue Library", "state": "ok", "status": "Up to date",
            "continue": {"kind": "library", "documentUuid": "doc-continue"}}))
        watchedModel.append(WatchJs.row({
            "sourceId": "src", "seriesId": "w-continue-none", "sourceName": "Example Reader",
            "title": "Continue None", "state": "ok", "status": "Up to date"}))
        win.findChild(watchList, "watchRows").forceLayout()
        var continueWRowAreas = win.findChildren(watchList, "watchRowArea", [])

        win.watchSavedReads = 0
        win.watchReads = 0
        win.watchOpens = 0
        continueWRowAreas[0].clicked(null)
        win.want("a saved continue target sends OpenSaved", win.watchSavedReads, 1)
        win.want("naming the chapter to continue at", win.watchSavedReadChapter, "c7")
        win.want("and opens no series", win.watchOpens, 0)

        win.watchReads = 0
        win.watchOpens = 0
        continueWRowAreas[1].clicked(null)
        win.want("a library continue target hands off to the reader", win.watchReads, 1)
        win.want("naming the document", win.watchReadUuid, "doc-continue")
        win.want("and opens no series", win.watchOpens, 0)

        win.watchReads = 0
        win.watchSavedReads = 0
        win.watchOpens = 0
        continueWRowAreas[2].clicked(null)
        win.want("a row with nothing to continue falls back to Browse",
                 win.watchOpens, 1)
        win.want("naming its own series", win.watchOpenedSeries, "w-continue-none")

        // Browse is still on the menu, and still opens the series.
        win.holdOn(continueWRowAreas[0])
        win.want("Browse is on the watch menu",
                 win.menuLabels(watchList).join(","),
                 "Browse,Download new chapters,Mark as seen,Stop watching")
        win.watchOpens = 0
        win.tapMenu(watchList, "open")
        win.want("and it opens the series, not the continue target", win.watchOpens, 1)
        win.want("naming that series", win.watchOpenedSeries, "w-continue-saved")

        // ---- state: the new-chapters badge ---------------------------------

        WatchJs.reconcile(watchedModel, [
            {"sourceId": "src", "seriesId": "w-new", "sourceName": "Example Reader",
             "title": "Watched With News", "newChapters": 3, "badge": "3 new chapters",
             "state": "new", "status": "3 new chapters",
             "coverUrl": "https://example.invalid/w.jpg"},
            {"sourceId": "src", "seriesId": "w-quiet", "sourceName": "Example Reader",
             "title": "Watched Quietly", "newChapters": 0, "badge": "",
             "state": "ok", "status": "Up to date"}])

        watchList.view = "list"
        var accWRows = win.findChild(watchList, "watchRows")
        accWRows.forceLayout()
        var accBadges = win.findChildren(accWRows, "watchBadge", [])
        var accBadgeTexts = win.findChildren(accWRows, "watchBadgeText", [])
        // The whole badge is the accent — a filled block, not an outline round
        // a coloured count, which was a thin stroke and a coloured glyph at
        // once.
        win.want("the badge on a row is a solid accent block",
                 win.colorOf(accBadges[0]), Style.accent)
        win.want("with no coloured hairline round it", accBadges[0].border.width, 0)
        win.want("and its count in black on top", win.colorOf(accBadgeTexts[0]), Style.ink)
        // The badge is drawn *only* when there is something to report, and the
        // count is spelled out inside it: a reader who sees no colour at all
        // still gets the whole answer.
        win.want("and the count is still the backend's words",
                 accBadgeTexts[0].text, "3 new chapters")
        win.want("while a series with nothing new wears no badge at all",
                 accBadges[1].visible, false)
        win.want("and the row's own title stays ink",
                 win.colorOf(win.findChildren(accWRows, "watchRowTitle", [])[0]), Style.ink)

        // The same badge in the other layout. Two layouts, one badge: a tile
        // whose badge were black would read as a different kind of thing.
        watchList.view = "grid"
        var accWTiles = win.findChild(watchList, "watchTiles")
        win.findChild(accWTiles, "coverTiles").forceLayout()
        var accTileBadges = win.findChildren(accWTiles, "coverBadge", [])
        var accTileBadgeTexts = win.findChildren(accWTiles, "coverBadgeText", [])
        win.want("a tile's badge is the same accent block",
                 win.colorOf(accTileBadges[0]), Style.accent)
        win.want("with its count black on it too",
                 win.colorOf(accTileBadgeTexts[0]), Style.ink)
        win.want("with the same words", accTileBadgeTexts[0].text, "3 new chapters")
        win.want("and a tile with no news still wears nothing",
                 accTileBadges[1].visible, false)
        win.want("while the caption under it stays ink",
                 win.colorOf(win.findChildren(accWTiles, "coverCaption", [])[0]), Style.ink)

        // The entry point's half of the same badge. This one is a button, so
        // the mark arrives *with* the extra words and never instead of them:
        // with nothing to report it is an ordinary button.
        var accWatching = win.findChild(sourceList, "watchingLabel")
        var accWatchingMark = win.findChild(sourceList, "watchingMark")
        sourceList.watchingLabel = ""
        win.want("a Watching button with no news carries no mark",
                 accWatchingMark.visible, false)
        win.want("and its label is plain ink", win.colorOf(accWatching), Style.ink)
        win.want("saying only Watching", accWatching.text, "Watching")
        sourceList.watchingLabel = "3 new"
        win.want("news puts a mark on it", accWatchingMark.visible, true)
        win.want("in the accent", win.colorOf(accWatchingMark), Style.accent)
        win.want("beside a label that is still black",
                 win.colorOf(accWatching), Style.ink)
        win.want("with the news in the label itself",
                 accWatching.text, "Watching · 3 new")
        win.want("while its neighbour stays an ordinary button",
                 win.colorOf(win.findChild(sourceList, "downloadedButton").border), Style.ink)

        // ---- state: a download in flight -----------------------------------
        //
        // The predicate first, because it is what the two row types share and
        // the place a new backend phase would go wrong.
        win.want("a queued download is in flight", chapterList.inFlight("queued", ""), true)
        win.want("so is one fetching pages", chapterList.inFlight("downloading", ""), true)
        win.want("a phase this Quire has never heard of is too",
                 chapterList.inFlight("recompressing", ""), true)
        win.want("but a finished one is not", chapterList.inFlight("done", ""), false)
        win.want("nor a failed one", chapterList.inFlight("failed", ""), false)
        win.want("nor a stopped one", chapterList.inFlight("cancelled", ""), false)
        win.want("nor a row that has never been asked for",
                 chapterList.inFlight("", ""), false)
        win.want("nor one already in the library",
                 chapterList.inFlight("downloading", "doc-1"), false)

        chapterList.view = "chapters"
        chaptersModel.setProperty(0, "downloadState", "downloading")
        chaptersModel.setProperty(0, "downloadMessage", "Page 3 of 20.")
        var accChapterRows = win.findChild(chapterList, "chapterRows")
        accChapterRows.forceLayout()
        var accSubs = win.findChildren(accChapterRows, "chapterSubtitle", [])
        var accMarks = win.findChildren(accChapterRows, "chapterMark", [])
        // A mark beside the sentence, and the sentence left grey. The line
        // itself used to be drawn in the accent, which is the muddy text the
        // measurement in ui/Style.js is about.
        win.want("a downloading chapter carries a mark", accMarks[0].visible, true)
        win.want("drawn in the accent", win.colorOf(accMarks[0]), Style.accent)
        win.want("while the line under it stays grey",
                 win.colorOf(accSubs[0]), Style.muted)
        win.want("and says what is happening in the backend's words",
                 accSubs[0].text, "Page 3 of 20.")
        win.want("a row with nothing happening carries no mark",
                 accMarks[1].visible, false)
        win.want("and the chapter's own title stays ink",
                 win.colorOf(win.findChildren(accChapterRows, "chapterTitle", [])[0]),
                 Style.ink)

        // A failure is not progress. It is a whole sentence and it is already
        // the loudest thing on the row; marking it too would make the accent
        // mean two different things on one line.
        chaptersModel.setProperty(0, "downloadState", "failed")
        chaptersModel.setProperty(0, "downloadMessage", "The source stopped answering.")
        win.want("a failed download carries no mark", accMarks[0].visible, false)
        win.want("and is not accented either", win.colorOf(accSubs[0]), Style.muted)
        win.want("though it still says what happened",
                 accSubs[0].text, "The source stopped answering.")
        chaptersModel.setProperty(0, "downloadState", "")
        chaptersModel.setProperty(0, "downloadMessage", "")
        win.want("and a row back at rest is unmarked again", accMarks[0].visible, false)

        // The volume rows, through the same predicate: a volume is the longer
        // wait of the two, so if only one were marked it would be the wrong one.
        chapterList.view = "volumes"
        // A second volume, at rest: the case is that one row is marked and the
        // other is not, and a single-row list cannot show that.
        volumesModel.append({"chapterId": "c7", "title": "Volume 2",
                             "detail": "7 chapters, Chapter 1 to Chapter 7",
                             "chapterCount": 7, "downloadState": "",
                             "downloadMessage": "", "documentUuid": "",
                             "saved": false})
        volumesModel.setProperty(0, "downloadState", "downloading")
        volumesModel.setProperty(0, "downloadMessage", "Page 3 of 200.")
        var accVolumeRows = win.findChild(chapterList, "volumeRows")
        accVolumeRows.forceLayout()
        var accVolSubs = win.findChildren(accVolumeRows, "volumeSubtitle", [])
        var accVolMarks = win.findChildren(accVolumeRows, "volumeMark", [])
        win.want("a volume downloading is marked the same way",
                 win.colorOf(accVolMarks[0]), Style.accent)
        win.want("and the mark is there", accVolMarks[0].visible, true)
        win.want("in the backend's words again", accVolSubs[0].text, "Page 3 of 200.")
        win.want("with the sentence itself still grey",
                 win.colorOf(accVolSubs[0]), Style.muted)
        win.want("and a volume at rest carries no mark", accVolMarks[1].visible, false)
        win.want("keeping its own grey sentence",
                 win.colorOf(accVolSubs[1]), Style.muted)
        win.want("which is what the volume holds",
                 accVolSubs[1].text, "7 chapters, Chapter 1 to Chapter 7")
        volumesModel.setProperty(0, "downloadState", "")
        volumesModel.setProperty(0, "downloadMessage", "")
        chapterList.view = "chapters"

        // ---- state: a source that did not answer ---------------------------

        // On screen for this: `visible` is the assertion below, and an item
        // inside a hidden screen reports false whatever it asked for.
        searchAll.visible = true
        searchAll.failedSources = "No answer from Third Reader"
        var accFailedBar = win.findChild(searchAll, "searchAllFailedBar")
        var accFailed = win.findChild(searchAll, "searchAllFailed")
        var accFailedMark = win.findChild(searchAll, "searchAllFailedMark")
        win.want("the line naming a source that did not answer carries a mark",
                 win.colorOf(accFailedMark), Style.accent)
        win.want("with the words themselves in ink",
                 win.colorOf(accFailed), Style.ink)
        win.want("and the mark clear of them",
                 accFailedMark.x + accFailedMark.width <= accFailed.x, true)
        // The words are the whole signal. This line is the *list of sources*,
        // not a mark standing for one.
        win.want("and it names the source, rather than standing for it",
                 accFailed.text, "No answer from Third Reader")
        win.want("below the results, which are still there",
                 accFailedBar.visible, true)
        // Absent, not decoloured, when there is nothing to say: the accent
        // never sits on screen meaning "all is well".
        searchAll.failedSources = ""
        win.want("and there is no line at all when every source answered",
                 accFailedBar.visible, false)
        win.want("taking no room with it", accFailedBar.height, 0)
        win.want("so the mark is off the screen with it",
                 accFailedMark.visible, false)
        searchAll.visible = false

        // ---- the section headings ------------------------------------------
        //
        // Settings is the one screen that is a list of unrelated sections, so
        // its headings are what is actually navigated. The heading is black
        // and the bar under it is the accent (ui/SectionHeading.qml) — it was
        // the other way round, and on the device the coloured headings were
        // the worst of the muddy text because they are the largest coloured
        // type in the app.
        var accHeadings = win.findChildren(settings, "sectionHeading", [])
        var accHeadingRules = win.findChildren(settings, "sectionHeadingRule", [])
        win.want("every section has a heading", accHeadings.length > 3, true)
        win.want("and every heading a rule under it",
                 accHeadingRules.length, accHeadings.length)
        win.want("the first heading is ink", win.colorOf(accHeadings[0]), Style.ink)
        win.want("and the last too",
                 win.colorOf(accHeadings[accHeadings.length - 1]), Style.ink)
        win.want("the first rule is the accent",
                 win.colorOf(accHeadingRules[0]), Style.accent)
        win.want("and the last as well",
                 win.colorOf(accHeadingRules[accHeadingRules.length - 1]), Style.accent)
        // A bar, not a hairline, and pointing at the words rather than lying
        // across the page: it is as wide as the heading it belongs to and no
        // wider.
        win.want("a rule is drawn as an area",
                 accHeadingRules[0].height, Style.accentRule)
        win.want("as wide as the words above it",
                 Math.round(accHeadingRules[0].width),
                 Math.round(accHeadings[0].contentWidth))
        win.want("which is narrower than the column",
                 accHeadingRules[0].width < accHeadings[0].width, true)
        // The signals underneath the colour: a heading is still the largest
        // type in its section, and still says what the section is.
        win.want("a heading is still larger than the body under it",
                 Style.headingSize > Style.bodySize, true)
        win.want("and still says what its section is", accHeadings[0].text, "Backend")

        // ---- and the whole point: no word anywhere is coloured -------------
        //
        // Every case above names the element it is about, which means every
        // case above can be satisfied by a screen that also colours something
        // nobody thought to name. This is the sweep that cannot: it walks
        // every screen the harness holds, collects everything that is a Text
        // by construction rather than by objectName, and asserts that not one
        // of them is drawn in the accent.
        //
        // The state is set up again first, because the cases above reset it —
        // a sweep run over screens with nothing happening on them would pass
        // without ever seeing a badge, a progress line or a failed source.
        searchAll.visible = true
        searchAll.failedSources = "No answer from Third Reader"
        sourceList.watchingLabel = "3 new"
        chaptersModel.setProperty(0, "downloadState", "downloading")
        chaptersModel.setProperty(0, "downloadMessage", "Page 3 of 20.")
        accChapterRows.forceLayout()

        var accScreens = [sourceList, addSource, seriesGrid, chapterList, watchList,
                          downloadedList, searchAll, settings, viewToggle, lonePager]
        var accAllText = []
        for (var accI = 0; accI < accScreens.length; ++accI)
            win.textsUnder(accScreens[accI], accAllText)
        // The sweep is worthless if it found nothing, and "nothing" is exactly
        // what a broken walk returns.
        win.want("there is a screenful of text to check", accAllText.length > 50, true)
        win.want("including the lines these cases are about",
                 accAllText.indexOf(accFailed) >= 0
                 && accAllText.indexOf(accBadgeTexts[0]) >= 0
                 && accAllText.indexOf(accSubs[0]) >= 0, true)

        var accOffenders = []
        for (var accJ = 0; accJ < accAllText.length; ++accJ)
            if (win.colorOf(accAllText[accJ]) === Style.accent)
                accOffenders.push(String(accAllText[accJ].objectName) + ": "
                                  + String(accAllText[accJ].text))
        // Named, not counted: when this fails it has to say which line, or the
        // next person has to go looking for it by eye on a device.
        win.want("and not one word of it is drawn in the accent",
                 accOffenders.join(" | "), "")

        searchAll.failedSources = ""
        searchAll.visible = false
        chaptersModel.setProperty(0, "downloadState", "")
        chaptersModel.setProperty(0, "downloadMessage", "")
        // ---- and now the timing, against real timers -----------------------
        //
        // Everything above drove `held` directly, which says nothing about
        // when it fires. The rest of this file presses a row for real and
        // watches the clock: the feedback at Style.pressFeedbackDelay, the
        // menu at Style.holdDelay, and a held press that does **not** also
        // count as a tap on the way up.
        downloadedList.view = "list"
        downloadedRows([{
            "sourceId": "src-a", "sourceName": "Example Reader",
            "seriesId": "/manga/lantern/", "title": "The Lantern Keeper",
            "detail": "3 downloads", "openable": true, "note": "",
            "coverUrl": "", "latestUuid": "doc-1", "watched": false}])
        win.pressArea = win.findChild(downloadedList, "downloadedRowArea")
        win.pressMenu = win.findChild(downloadedList, "downloadedMenu")
        win.startPressTimings()
    }

    // ---- the press, timed --------------------------------------------------
    //
    // A press is started with beginPress() and then left alone: the component's
    // own timers are what move it on, so these cases fail if either delay
    // changes. Nothing here calls held() or tapped(), which is the point —
    // those are the handlers, and a test that invokes a handler proves only
    // that the handler exists.
    //
    // The checkpoints sit well inside the gaps (60ms and 260ms around a 150ms
    // feedback, 260ms and 620ms around a 500ms menu) because an offscreen Qt
    // timer is allowed to be late. They are scheduled against the wall clock
    // rather than one after another, so a step that overran does not push the
    // ones behind it.
    property var pressArea: null
    property var pressMenu: null
    property var pressSteps: []
    property int pressStep: 0
    property real pressStart: 0

    Timer { id: pressTimer; repeat: false; onTriggered: win.runPressStep() }

    function startPressTimings() {
        win.pressSteps = [
            {"at": 0, "run": function () {
                win.downloadedOpens = 0
                win.pressArea.beginPress()
            }},
            {"at": 60, "run": function () {
                // Too early for either. A tap that flashed the row would be a
                // row that flickers every time the list is used.
                win.want("a press this short does not darken the row",
                         win.pressArea.feedback, false)
                win.want("and the row is still paper",
                         win.colorOf(win.findChild(downloadedList, "rowPressFeedback")),
                         Style.paper)
                win.want("and opens no menu", win.pressMenu.opened, false)
            }},
            {"at": 260, "run": function () {
                // Past the feedback, nowhere near the menu. This is the gap
                // the feedback exists for: on e-ink the user cannot otherwise
                // tell a registered hold from a dead screen, and lifts early.
                win.want("a press held on darkens the row", win.pressArea.feedback, true)
                win.want("and the row is drawn dark",
                         win.colorOf(win.findChild(downloadedList, "rowPressFeedback")),
                         Style.pressed)
                win.want("but still opens no menu", win.pressMenu.opened, false)
            }},
            {"at": 620, "run": function () {
                win.want("holding on opens the menu", win.pressMenu.opened, true)
                win.want("for the row under the finger",
                         win.menuActions(downloadedList).join(","),
                         "open,read,watch,delete")

                // The finger comes up. MouseArea emits clicked() on release
                // whether or not the hold fired, so this is the case that
                // stops a long press opening the series behind its own menu.
                win.pressArea.endPress()
                win.pressArea.clicked(null)
                win.want("a held press is not also a tap", win.downloadedOpens, 0)
                win.want("and the darkening ends with the press",
                         win.pressArea.feedback, false)
                win.want("while the menu stays up", win.pressMenu.opened, true)
            }},
            {"at": 700, "run": function () {
                // A short tap, from the same machinery: down, up well before
                // the menu, and the click that follows.
                win.findChild(win.pressMenu, "contextMenuScrim").clicked(null)
                win.pressArea.beginPress()
            }},
            {"at": 780, "run": function () {
                win.pressArea.endPress()
                win.pressArea.clicked(null)
                win.want("a short tap opens the series", win.downloadedOpens, 1)
                win.want("and no menu with it", win.pressMenu.opened, false)
                win.want("having never darkened the row", win.pressArea.feedback, false)
            }}
        ]
        win.pressStep = 0
        win.pressStart = Date.now()
        win.schedulePressStep()
    }

    function schedulePressStep() {
        if (win.pressStep >= win.pressSteps.length) {
            win.finish()
            return
        }
        // Against the wall clock, so a late step does not drag the next one
        // past the delay it is measuring.
        var due = win.pressSteps[win.pressStep].at - (Date.now() - win.pressStart)
        pressTimer.interval = due > 1 ? due : 1
        pressTimer.start()
    }

    function runPressStep() {
        var step = win.pressSteps[win.pressStep]
        step.run()
        win.pressStep++
        win.schedulePressStep()
    }

    function finish() {
        console.log(win.failures === 0 ? "HARNESS OK" : "HARNESS FAILED: " + win.failures)
        Qt.exit(win.failures === 0 ? 0 : 1)
    }
}
