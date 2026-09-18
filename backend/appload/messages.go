package appload

// MessageType is an AppLoad message type tag: the first int32 of a frame.
//
// Negative values are reserved by the AppLoad host for system messages.
// Type 0 is reserved by PLAN §7.1 and never used by Quire.
type MessageType = int32

// System message types, defined by the AppLoad host (src/protocol.h).
const (
	// MessageSystemTerminate tells the backend to shut down. Conn.Recv
	// surfaces this as ErrTerminated.
	MessageSystemTerminate MessageType = -1
	// MessageSystemNewCoordinator announces that a frontend attached.
	MessageSystemNewCoordinator MessageType = -2
	// MessageSystemLostCoordinator announces that the frontend detached. The
	// backend keeps running; another frontend may attach later.
	MessageSystemLostCoordinator MessageType = -3
)

// Quire message types — PLAN §7.1. All payloads are JSON (or empty).
//
// Images are never sent over this socket: write to disk and send a path. The
// 10 MiB cap and AppLoad's per-hook mutex make large transfers a bad idea.
//
// These constants are mirrored in ui/Messages.js; messages_test.go fails if the
// two drift.
const (
	// MessageReserved is PLAN §7.1's reserved 0. Never sent.
	MessageReserved MessageType = 0

	// MessagePing is UI→BE, no payload.
	MessagePing MessageType = 1
	// MessagePong is BE→UI, JSON status.
	MessagePong MessageType = 2

	// MessageListSources is UI→BE, no payload.
	MessageListSources MessageType = 10
	// MessageSources is BE→UI, JSON array.
	MessageSources MessageType = 11
	// MessageProbeSource is UI→BE, JSON {url}.
	MessageProbeSource MessageType = 12
	// MessageProbeProgress is BE→UI, JSON, streamed, one per probe stage.
	MessageProbeProgress MessageType = 13
	// MessageProbeVerdict is BE→UI, JSON {verdict, theme, detail}.
	MessageProbeVerdict MessageType = 14
	// MessageConfirmAddSource is UI→BE, JSON {url, theme, name, lang}.
	MessageConfirmAddSource MessageType = 15

	// MessageProbeAnswer is UI→BE, JSON {id}. The probe has two points where
	// PLAN §7.5 requires the *user* to decide rather than the code guessing — a
	// redirect that left the domain they typed, and two themes too close to
	// call — so the stream of type 13 messages has to be answerable.
	MessageProbeAnswer MessageType = 16
	// MessageSetSourceEnabled is UI→BE, JSON {sourceId, enabled}: PLAN §6 M3's
	// per-source toggle.
	MessageSetSourceEnabled MessageType = 17
	// MessageRemoveSource is UI→BE, JSON {sourceId}.
	MessageRemoveSource MessageType = 18
	// MessageRenameSource is UI→BE, JSON {sourceId, name}. Added after first
	// real use: probing https://api.mangadex.org named the source "MangaDex
	// API documentation", because that is what the page's <title> says. A
	// theme-suggested default fixes the themes we ship; renaming is the
	// general fix, because title detection will be wrong for *some* site
	// forever and being permanently stuck with a bad name is out of all
	// proportion to the cost of allowing an edit.
	MessageRenameSource MessageType = 19

	// MessageSearch is UI→BE, JSON {sourceId, query}.
	MessageSearch MessageType = 20
	// MessageSearchResults is BE→UI, JSON array.
	MessageSearchResults MessageType = 21
	// MessageBrowse is UI→BE, JSON {sourceId, page}: the popular/latest listing,
	// which is also what PLAN §7.5 stage 5 falls back to when search needs a
	// query. Its reply is MessageSearchResults, because a listing and a search
	// produce the same rows.
	MessageBrowse MessageType = 22
	// MessageRequestCover is UI→BE, JSON {sourceId, seriesId, url}. Images are
	// never sent over this socket (PLAN §7.1): the backend writes a downscaled
	// copy to disk and answers with its path.
	MessageRequestCover MessageType = 23
	// MessageCoverReady is BE→UI, JSON {seriesId, path}.
	MessageCoverReady MessageType = 24

	// MessageSetSourceSplitStrips is UI→BE, JSON {sourceId, splitStrips}, where
	// splitStrips is "auto", "never" or "always" (PLAN §12.3). It belongs with
	// the per-source settings at 17–19 and takes 25 only because 20–24 were
	// already spent on search and covers.
	//
	// It exists because strip detection will eventually be wrong on some image,
	// and the user should be able to stop it without editing JSON over SSH. The
	// reply is MessageSources, so the list redraws with the new value.
	MessageSetSourceSplitStrips MessageType = 25

	// MessageSeriesDetail is UI→BE, JSON {sourceId, seriesId}.
	MessageSeriesDetail MessageType = 30
	// MessageSeriesDetailResult is BE→UI, JSON.
	MessageSeriesDetailResult MessageType = 31

	// MessageEnqueueDownload is UI→BE, JSON {sourceId, seriesId, volumeId}.
	MessageEnqueueDownload MessageType = 40
	// MessageDownloadProgress is BE→UI, JSON, streamed.
	MessageDownloadProgress MessageType = 41
	// MessageCancelDownload is UI→BE, JSON {sourceId, seriesId, volumeId}.
	//
	// A volume is 7–10 minutes of CPU and up to 90 MB of traffic on a battery-
	// powered device. Starting one by mistake and being unable to stop it is
	// the kind of thing that makes an app feel broken, which is why PLAN §7.1
	// calls this not optional.
	MessageCancelDownload MessageType = 42

	// MessageEnqueueDownloads is UI→BE, JSON
	// {sourceId, seriesId, grouping, chapterIds}: one selection of rows, queued
	// together and without a question — picking the rows was the deliberate
	// step.
	//
	// It is its own message rather than a loop of MessageEnqueueDownload on the
	// frontend, because a selection differs from a run of taps in what the
	// backend *answers*: one reply saying what fitted on the queue, and no
	// per-row question. Sent as a loop, a selection of thirty against a queue of
	// sixteen would answer fourteen times with the same refusal, and a selection
	// of volumes would ask about each of them in turn.
	MessageEnqueueDownloads MessageType = 43

	// 44 was MessageQueueConfirm, the question asked before a selection was
	// queued. The user asked for the selection path to queue instantly — "just
	// download and queue instantly when i click download" — so there is no
	// question to carry any more.
	//
	// The number stays retired rather than being reused or closed up: these
	// constants are a wire protocol, pinned on both sides by
	// TestQMLMessagesMatchGo, and a frontend and backend that disagree about
	// what 44 means is worse than a gap in the numbering.
	//
	// MessageQueueResult is BE→UI, JSON {queued, skipped, message}: what the
	// queue actually took.
	//
	// The rows that fitted already say "Queued." for themselves, so `message`
	// is empty when everything fitted and carries the shortfall when it did
	// not. The queue is sixteen deep on purpose (a deeper one is a way to fill
	// /home while nobody is looking), so a long selection meeting that ceiling
	// is ordinary, and it is told once.
	MessageQueueResult MessageType = 45

	// MessageOpenInReader is UI→BE, JSON {documentUuid}.
	MessageOpenInReader MessageType = 50

	// MessageDeleteDownload is UI→BE, JSON {documentUuid, trashed}, sent after
	// the frontend has asked xochitl to move the document to its Trash.
	//
	// Like MessageOpenInReader, the act itself happens in QML: only an AppLoad
	// app's QML can reach xochitl's own singletons, and PLAN §12.4 proved the
	// route. The backend's half is the bookkeeping — forgetting the
	// library.Record and reclaiming the assembled PDF.
	//
	// `trashed` is what the frontend saw, not what it hoped for. False means the
	// document is still on the tablet, and the record must survive: a record
	// dropped for a document that is still there is an orphan Quire can no
	// longer account for, which is strictly worse than no delete button.
	MessageDeleteDownload MessageType = 51

	// MessageDeleteConfirm is BE→UI, JSON {documentUuid, message}: the question
	// to put in front of the user before anything is deleted.
	//
	// It mirrors the download confirmation, and for the same reason — the
	// sentence is the backend's (PLAN §2), and here it is also the only side
	// that knows what the document is called on the tablet, which for a split
	// volume is the part number the user needs to see.
	MessageDeleteConfirm MessageType = 53

	// MessageDownloadDeleted is BE→UI, JSON {documentUuid}. It is the backend
	// saying it has forgotten that document, which is the frontend's cue to put
	// every row pointing at it back to offering a download.
	//
	// Rows are cleared on this rather than on the QML call returning true,
	// because the row's state must follow the store. If the store could not be
	// updated, the row keeps saying "Read" — which is the truth.
	MessageDownloadDeleted MessageType = 52

	// Sorting a finished download into a per-series folder (PLAN §6 M5, the
	// conclusion corrected 2026-09-17).
	//
	// It is split across the socket because neither side can do it alone.
	// Creating a folder and moving a document into one are xochitl QML calls —
	// `Library.createCollectionWrapper(parentId, name)` and
	// `explorer.selectionMove(folderId)` — and nothing in that QML enumerates a
	// folder's children, so "is there already a folder for this series?" can
	// only be asked over HTTP, which only the backend speaks.
	//
	// MessageSortDocuments is BE→UI, JSON
	// {documentUuids, folderId, createUnder, folderName, createComics, comicsName,
	//  sourceId, seriesId}: everything the frontend needs to place one volume,
	// including a folder id when the backend already found one.
	//
	// The uuids are a list because a volume too big for the upload cap arrives
	// as several documents, and one selection moves them all in one call.
	MessageSortDocuments MessageType = 54

	// MessageDocumentsSorted is UI→BE, JSON
	// {documentUuids, moved, folderId, folderName, created, detail}: what
	// actually happened, measured by reading each document's parent back.
	//
	// `moved` is the documents whose parent really changed, not the ones the
	// call was made for. A download that could not be sorted is still a
	// download: the volume is in Comics, which is where it has always been, and
	// nothing about it is reported as a failure.
	MessageDocumentsSorted MessageType = 55

	// Reconciling the library with the tablet (PLAN §12.4). A document the user
	// deleted in xochitl leaves a record claiming it and a page cache nothing
	// can reach, and until this existed Quire only noticed when they tapped
	// Read.
	//
	// MessageCheckDocuments is BE→UI, JSON {documentUuids}: which of these
	// still resolve? Only the frontend can ask — Library.entryForId is QML.
	MessageCheckDocuments MessageType = 56

	// MessageDocumentsChecked is UI→BE, JSON {checked, documentUuids, missing}.
	//
	// **`checked` is the whole safety of this exchange and it is data, not an
	// absence.** A frontend that could not look — no bridge, a call that threw
	// — answers `checked: false`, and an empty `missing` from it must be
	// impossible to confuse with an empty `missing` from a frontend that looked
	// and found everything present. The backend acts only on `checked: true`.
	//
	// The consequence of getting that wrong is not a stale button: it is every
	// record dropped and the whole page cache deleted, silently, because a QML
	// file did not load.
	MessageDocumentsChecked MessageType = 57

	// Deleting every download of one series, from the downloaded overview
	// (PLAN §12.5). Asked for as a single action on the row: "we should add a
	// delete button to the entries in the download overview, which directly
	// deletes everything from that manga".
	//
	// MessageDeleteSeries is UI→BE, JSON {sourceId, seriesId, confirmed,
	// results}. Without `confirmed` it is a request for the question, answered
	// with MessageDeleteSeriesConfirm, JSON {sourceId, seriesId, documentUuids,
	// message}: the sentence, and the documents it is about.
	//
	// **It asks, where the multi-select queue does not.** Queueing is
	// reversible and costs only time; this destroys every download of a series
	// at once and there is no way back but to fetch them all again.
	//
	// `results` is what the frontend observed, one entry per document —
	// {documentUuid, trashed, removed} — and not what it intended. Each
	// document fails separately, so a single flag for the lot of them would
	// have to lie about one end or the other.
	MessageDeleteSeries        MessageType = 58
	MessageDeleteSeriesConfirm MessageType = 59

	// The 60s are PLAN §12.2's watched series: mark a series watched, and know
	// when it has gained chapters since you last looked.
	//
	// MessageWatchSeries is UI→BE, JSON {sourceId, seriesId, title}. Sent from
	// the series screen, where the user is by definition looking at the chapter
	// list — so watching starts from "everything here is seen", not from an
	// announcement of the entire back catalogue.
	MessageWatchSeries MessageType = 60

	// MessageUnwatchSeries is UI→BE, JSON {sourceId, seriesId}.
	MessageUnwatchSeries MessageType = 61

	// MessageCheckWatched is UI→BE, JSON {} — or {sourceId, seriesId} for one
	// series. It is the explicit "check now", and it overrides the per-source
	// cooldown: a user who taps a button has asked, and answering a direct
	// request with silence because of a timer is the kind of thing that makes
	// an app feel broken.
	//
	// The automatic check has no message of its own. It runs on attach and
	// nowhere else — never on a timer, never in the background (PLAN §6 M7).
	MessageCheckWatched MessageType = 62

	// MessageWatchList is BE→UI, JSON {watched: [...], summary: {...}}. The
	// whole list, pushed on attach and after any change to it.
	//
	// The summary is the at-a-glance indicator: counts, plus the wording for
	// the entry point and for the watched screen's header. It rides on every
	// push, including the one on attach, because the entry point lives on a
	// screen that may never open the watched list at all. It is computed from
	// the rows in the same message — two counts maintained separately drift,
	// and the user meets that as "3 new" over a list showing two.
	MessageWatchList MessageType = 63

	// MessageWatchUpdate is BE→UI, JSON — one series' state, streamed as each
	// check lands.
	//
	// One per series rather than a list at the end because the checks are
	// serialised by the §7.4 limiter at two seconds per host: forty watched
	// series is well over a minute, and a shell that waits for all of it before
	// drawing is a shell that looks broken.
	MessageWatchUpdate MessageType = 64

	// The downloaded overview (PLAN §12.5): every series with at least one
	// volume on the tablet, so a download that was never watched is still
	// findable.
	//
	// MessageListDownloaded is UI→BE, JSON {}. MessageDownloadedList is the
	// reply, JSON {series: [...], empty}: one row per **(source, series)**, and
	// every row naming its source. Always the pair, never "series, with the
	// source shown when two collide" — a row has to know which source to open,
	// and a label that appears conditionally is one the user cannot rely on.
	//
	// Fetched when the screen opens. There is no push and no live update: a
	// download or a delete shows up the next time it is opened, which is enough
	// and is a great deal less machinery.
	MessageListDownloaded MessageType = 65
	MessageDownloadedList MessageType = 66

	// The empty series folder left behind when a series' last download goes
	// (PLAN §12.5).
	//
	// MessageDeleteFolder is BE→UI, JSON {folderId, folderName}: the backend
	// has *listed* that folder over the web interface and found it empty, and
	// asks the frontend — the only side that can delete anything — to remove
	// it. MessageFolderDeleted is the answer, JSON {folderId, folderName,
	// trashed, removed}.
	//
	// **The backend asks only for a folder it listed and found empty.** The
	// folder is the user's once it exists and may hold something of theirs, so
	// emptiness is measured rather than inferred from "we deleted everything we
	// knew about" — and a listing that fails is never an empty folder.
	//
	// `folderName` is composed by the backend and echoed back untouched: by the
	// time the answer arrives the records it was taken from are gone, and the
	// frontend inventing a name for the sentence would be the view writing
	// copy (PLAN §2).
	MessageDeleteFolder  MessageType = 67
	MessageFolderDeleted MessageType = 68

	// The 70s are device-wide settings — things that are about Quire rather
	// than about any one source.
	//
	// MessageSetConsultRobots is UI→BE, JSON {consultRobots}. It is the one
	// global robots.txt switch of PLAN §7.4, which is off by default: RFC 9309
	// scopes robots.txt to crawlers, and a person searching and tapping is
	// driving every request.
	//
	// There is no BE→UI answer of its own. The current value rides on the Pong
	// status, which is the one message the shell already has to receive before
	// it can draw itself, so the settings screen pings after toggling and gets
	// the stored value back rather than trusting its own optimism.
	MessageSetConsultRobots MessageType = 70

	// MessageError is BE→UI, JSON {code, message}.
	// The download cache (PLAN §12.4). Page images outlive the download that
	// fetched them so a repeat can skip them, and a delete cannot always reach
	// them afterwards — see clearCache for the two ways they are orphaned.
	//
	// MessageGetCacheSize is UI→BE, JSON {}. MessageCacheStatus is the reply,
	// JSON {bytes, message}: the size, and the sentence for it.
	MessageGetCacheSize MessageType = 71
	MessageCacheStatus  MessageType = 72

	// MessageClearCache is UI→BE, JSON {confirmed}. Without `confirmed` it is a
	// request for the question, answered with MessageCacheConfirm, JSON
	// {bytes, message}; with it, the cache is cleared and the reply is a fresh
	// MessageCacheStatus saying what went and what was kept.
	//
	// It asks first for the same reason deleting a download does: it is
	// hundreds of megabytes and the only way back is to fetch it all again.
	MessageClearCache   MessageType = 73
	MessageCacheConfirm MessageType = 74

	MessageError MessageType = 90
)

// messageNames covers every type Quire defines, system types included. It is
// the single source of truth for the drift test against ui/Messages.js and for
// log output.
var messageNames = map[MessageType]string{
	MessageSystemTerminate:       "SystemTerminate",
	MessageSystemNewCoordinator:  "SystemNewCoordinator",
	MessageSystemLostCoordinator: "SystemLostCoordinator",
	MessagePing:                  "Ping",
	MessagePong:                  "Pong",
	MessageListSources:           "ListSources",
	MessageSources:               "Sources",
	MessageProbeSource:           "ProbeSource",
	MessageProbeProgress:         "ProbeProgress",
	MessageProbeVerdict:          "ProbeVerdict",
	MessageConfirmAddSource:      "ConfirmAddSource",
	MessageProbeAnswer:           "ProbeAnswer",
	MessageSetSourceEnabled:      "SetSourceEnabled",
	MessageRemoveSource:          "RemoveSource",
	MessageRenameSource:          "RenameSource",
	MessageSearch:                "Search",
	MessageSearchResults:         "SearchResults",
	MessageBrowse:                "Browse",
	MessageRequestCover:          "RequestCover",
	MessageCoverReady:            "CoverReady",
	MessageSetSourceSplitStrips:  "SetSourceSplitStrips",
	MessageSeriesDetail:          "SeriesDetail",
	MessageSeriesDetailResult:    "SeriesDetailResult",
	MessageEnqueueDownload:       "EnqueueDownload",
	MessageDownloadProgress:      "DownloadProgress",
	MessageCancelDownload:        "CancelDownload",
	MessageEnqueueDownloads:      "EnqueueDownloads",
	MessageQueueResult:           "QueueResult",
	MessageOpenInReader:          "OpenInReader",
	MessageDeleteDownload:        "DeleteDownload",
	MessageDeleteConfirm:         "DeleteConfirm",
	MessageSortDocuments:         "SortDocuments",
	MessageDocumentsSorted:       "DocumentsSorted",
	MessageCheckDocuments:        "CheckDocuments",
	MessageDocumentsChecked:      "DocumentsChecked",
	MessageDeleteSeries:          "DeleteSeries",
	MessageDeleteSeriesConfirm:   "DeleteSeriesConfirm",
	MessageDownloadDeleted:       "DownloadDeleted",
	MessageWatchSeries:           "WatchSeries",
	MessageUnwatchSeries:         "UnwatchSeries",
	MessageCheckWatched:          "CheckWatched",
	MessageWatchList:             "WatchList",
	MessageWatchUpdate:           "WatchUpdate",
	MessageListDownloaded:        "ListDownloaded",
	MessageDownloadedList:        "DownloadedList",
	MessageDeleteFolder:          "DeleteFolder",
	MessageFolderDeleted:         "FolderDeleted",
	MessageSetConsultRobots:      "SetConsultRobots",
	MessageGetCacheSize:          "GetCacheSize",
	MessageCacheStatus:           "CacheStatus",
	MessageClearCache:            "ClearCache",
	MessageCacheConfirm:          "CacheConfirm",
	MessageError:                 "Error",
}

// Name returns the PLAN §7.1 name of a message type, or "Unknown(<n>)".
func Name(t MessageType) string {
	if n, ok := messageNames[t]; ok {
		return n
	}
	if t == MessageReserved {
		return "Reserved"
	}
	return "Unknown(" + itoa(t) + ")"
}

func itoa(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	var buf [12]byte
	i := len(buf)
	u := uint32(v)
	if neg {
		u = uint32(-v)
	}
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
