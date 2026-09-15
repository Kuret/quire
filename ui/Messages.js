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

var Search = 20
var SearchResults = 21

var SeriesDetail = 30
var SeriesDetailResult = 31

var EnqueueDownload = 40
var DownloadProgress = 41

var OpenInReader = 50

var Error = 90
