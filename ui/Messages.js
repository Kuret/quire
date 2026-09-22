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

// One query across every enabled source (grouped by title; a source that
// fails is a partial answer, not an error). Separate from Search because the
// paging, the result shape and the failure handling are all different.
var SearchAll = 26
var SearchAllResults = 27

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

// Queue exactly the chapters a watched series has gained. The backend picks
// them, because the backend is what decided they were new.
var DownloadNewChapters = 46

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
var DeleteSeries = 58
var DeleteSeriesConfirm = 59
var DownloadDeleted = 52

var WatchSeries = 60
var UnwatchSeries = 61
var CheckWatched = 62
var WatchList = 63
var WatchUpdate = 64

// Clear the "new" badge without downloading: the stored new ids move into the
// seen list, so it marks exactly what the badge was counting.
var MarkSeen = 69

// The downloaded overview (PLAN §12.5): one row per (source, series) with at
// least one volume on the tablet. Fetched when the screen opens.
var ListDownloaded = 65
var DownloadedList = 66
var DeleteFolder = 67
var FolderDeleted = 68

// The one global robots.txt switch (PLAN §7.4, off by default). The current
// value comes back on the Pong status rather than in a reply of its own.
var SetConsultRobots = 70

// Which layout a screen uses, "grid" or "list", stored per screen. The current
// values ride on the Pong status, like the robots switch.
var SetView = 75

// The download cache (PLAN §12.4): its size, and the two steps to clear it.
var GetCacheSize = 71
var CacheStatus = 72
var ClearCache = 73
var CacheConfirm = 74

// Setting, changing or clearing a source's proxy. An empty proxy removes it;
// clearing the proxy on a source confirmed self-hosted via that proxy also
// withdraws the confirmation. No reply of its own — a fresh Sources follows,
// like RenameSource.
var SetSourceProxy = 76

// The allowedHosts editor (record-and-offer, not prompt-on-first-sight): a
// pending host is offered from the source list, never from a modal that
// would appear unattended or be granted on reflex. Allow appends to the
// source's allowedHosts; Revoke removes one. Neither replies on its own — a
// fresh Sources follows, like SetSourceProxy.
var AllowSourceHost = 77
var RevokeSourceHost = 78

// Private sources: kept off the normal source list and the normal combined
// search, and given a list and a combined search of their own. No reply of
// its own for the toggle — a fresh Sources and PrivateSources both follow,
// since marking or unmarking moves a source between the two lists.
var SetSourcePrivate = 79
var ListPrivateSources = 80
var PrivateSources = 81

// The private list's own combined search, parallel to SearchAll. Its reply is
// SearchAllResults, unchanged.
var SearchAllPrivate = 82

// Try: reading a chapter in Quire's own reader without downloading it and
// without a library entry (milestone 1). TryChapter opens a session,
// ending whichever was open; TryReady says the session is open and its
// first page (index 0) is on disk, with `complete` saying whether pageCount
// is the whole chapter yet or still resolving behind a fast first page
// (theme.FirstPageProber) — TryPageCount follows once it is. TryPageRequest
// asks for a page the reader turned to and does not already have; TryPage
// answers with it. EndTry discards the session's cache — the entire cleanup
// Try needs, since there is no reading position and no library entry.
var TryChapter = 83
var TryReady = 84
var TryPage = 85
var TryPageRequest = 86
var EndTry = 87
var TryPageCount = 88

var Error = 90

// Saved in Quire: a comic/manga chapter kept in Quire's own storage and read
// in Quire's own reader (TryReader.qml's "saved" mode), rather than uploaded
// to xochitl's library. Unlike Try, every page path is already known and
// local when OpenSaved answers, so there is no page-by-page traffic — and a
// reading position is kept, sent back on a page turn (debounced) and on
// close. OpenSaved on a chapter that turns out not to be saved, or whose
// files are gone, answers MessageError instead of SavedOpened. DeleteSaved
// follows the same confirm/done shape as a library delete
// (MessageDeleteDownload / MessageDownloadDeleted).
var OpenSaved = 91
var SavedOpened = 92
var SavePosition = 93
var DeleteSaved = 94
var SavedDeleted = 95
