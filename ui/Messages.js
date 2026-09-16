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

// Per-source grouping (PLAN §6 M4, reversed 2026-09-16): "chapter" — the
// default, one PDF per chapter — "volume" or "count". It sits beside 25 for the
// same reason: it is a per-source setting and 17-19 were full.
var SetSourceGrouping = 26

var SeriesDetail = 30
var SeriesDetailResult = 31

var EnqueueDownload = 40
var DownloadProgress = 41
var CancelDownload = 42

var OpenInReader = 50

var WatchSeries = 60
var UnwatchSeries = 61
var CheckWatched = 62
var WatchList = 63
var WatchUpdate = 64

// The one global robots.txt switch (PLAN §7.4, off by default). The current
// value comes back on the Pong status rather than in a reply of its own.
var SetConsultRobots = 70

var Error = 90
