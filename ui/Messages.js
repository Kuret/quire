// Quire AppLoad message types — PLAN §7.1.
//
// This file mirrors backend/appload/messages.go. The Go test
// TestQMLMessagesMatchGo parses this file and fails if the two drift, so edit
// both together or not at all.
//
// Type 0 is reserved and never sent. Negative types are AppLoad system
// messages, owned by the host.
.pragma library

var SystemTerminate = -1
var SystemNewCoordinator = -2
var SystemLostCoordinator = -3

var Ping = 1
var Pong = 2

var ListSources = 10
var Sources = 11
var ProbeSource = 12
var ProbeProgress = 13
var ProbeVerdict = 14
var ConfirmAddSource = 15
var ProbeAnswer = 16
var SetSourceEnabled = 17
var RemoveSource = 18
var RenameSource = 19

var Search = 20
var SearchResults = 21
var Browse = 22
var RequestCover = 23
var CoverReady = 24

// Per-source strip splitting (PLAN §12.3). It belongs with 17-19; it is 25
// because 20-24 were already spent.
var SetSourceSplitStrips = 25

var SeriesDetail = 30
var SeriesDetailResult = 31

var EnqueueDownload = 40
var DownloadProgress = 41
var CancelDownload = 42

// Queueing a selection of rows (PLAN §12.1). One question for the whole
// selection, and one answer about what fitted on the queue.
var EnqueueDownloads = 43
var QueueResult = 45

var OpenInReader = 50

// Deleting a download (PLAN §12.4). The trash call itself is QML's — see
// ReaderHandoff.qml — and DeleteDownload reports what it did, so the backend
// can forget the record only when the document really went.
var DeleteDownload = 51
var DeleteConfirm = 53

// Sorting a finished download into its series folder (PLAN §6 M5, corrected
// 2026-09-17). Creating and moving are QML; finding an existing folder is HTTP.
var SortDocuments = 54
var DocumentsSorted = 55

// Reconciling the library with the tablet (PLAN §12.4). `checked` says whether
// the frontend could look at all: an empty `missing` from a frontend that never
// looked must never read as "none of them exist".
var CheckDocuments = 56
var DocumentsChecked = 57
var DownloadDeleted = 52

var WatchSeries = 60
var UnwatchSeries = 61
var CheckWatched = 62
var WatchList = 63
var WatchUpdate = 64

// The downloaded overview (PLAN §12.5): one row per (source, series) with at
// least one volume on the tablet. Fetched when the screen opens.
var ListDownloaded = 65
var DownloadedList = 66

// The one global robots.txt switch (PLAN §7.4, off by default). The current
// value comes back on the Pong status rather than in a reply of its own.
var SetConsultRobots = 70

// The download cache (PLAN §12.4): its size, and the two steps to clear it.
var GetCacheSize = 71
var CacheStatus = 72
var ClearCache = 73
var CacheConfirm = 74

var Error = 90
