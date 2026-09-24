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

	// MessageProbeAnswer is UI→BE, JSON {id, text}. The probe has three points
	// where PLAN §7.5 requires the *user* to decide rather than the code
	// guessing — a redirect that left the domain they typed, two themes too
	// close to call, and an address that is only reachable if the site is a
	// service of the user's own — so the stream of type 13 messages has to be
	// answerable.
	//
	// `text` is the optional value a question asked to be typed alongside the
	// choice, "" for the questions that ask for none. It rides here rather than
	// on a message of its own: there is one channel the probe asks on, and
	// keeping it one is worth more than a tidier payload.
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

	// MessageSearchAll is UI→BE, JSON {query, page, pageSize, kind}: one query
	// put to every enabled source at once.
	//
	// It is a separate message rather than MessageSearch with the sourceId
	// left out, because almost nothing about it is the same: the paging is
	// across sources, the results are grouped rather than listed, and a single
	// source failing is a partial answer rather than an error. Overloading
	// MessageSearch would have meant a reply whose shape depended on a field
	// being absent.
	//
	// `kind` is "" (every kind), "book" or "manga" — the combined search
	// screen's filter, echoed by kind.go's own vocabulary. It decides which
	// sources are ever asked (backend/service/searchall.go's
	// newSearchAllPager), not which rows of an unfiltered reply are shown: a
	// Books search never sends a request to a manga site, and page 1 of it is
	// the first N book groups rather than a page a client-side filter has
	// hollowed out.
	MessageSearchAll MessageType = 26
	// MessageSearchAllResults is BE→UI, JSON {query, page, pageSize,
	// totalPages, hasMore, groups, sourceErrors}.
	//
	// A group is one series as far as the user is concerned, with the sources
	// that have it:
	//
	//	{"key", "title", "coverUrl",
	//	 "matches": [{"sourceId", "sourceName", "seriesId", "coverUrl"}]}
	//
	// `sourceErrors` is [{sourceId, sourceName, message}] and is the reason
	// this is not an error message: a source that did not answer must not
	// empty a screen that four other sources filled, and must not be silently
	// dropped either.
	MessageSearchAllResults MessageType = 27

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

	// MessageDownloadNewChapters is UI→BE, JSON {sourceId, seriesId}: queue
	// exactly the chapters a watched series has gained since you last saw it.
	//
	// The backend picks the chapters, not the frontend, because the backend is
	// what decided they were new: the watch check stores their ids beside the
	// count it badges. Sending the list to the frontend so it could send it
	// back would let the two disagree about what "new" meant, which is the one
	// thing a badge and the action under it must never do.
	MessageDownloadNewChapters MessageType = 46

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

	// MessageMarkSeen is UI→BE, JSON {sourceId, seriesId}: clear the "new"
	// badge without downloading anything, for when you read it elsewhere.
	//
	// It moves the stored new ids into the seen list rather than refetching, so
	// it marks exactly what the badge was counting and needs no network.
	MessageMarkSeen MessageType = 69

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

	// MessageSetView is UI→BE, JSON {screen, view}: which of the two layouts a
	// screen should use, "grid" (covers) or "list" (rows).
	//
	// It is stored per screen and not globally. Searching wants a list often
	// enough — a row carries the title and the sources it was found in, where a
	// tile carries a cover — while Downloaded and Watching are for recognising
	// something you already know, which is what covers are good at. One
	// setting would mean choosing a list to read search results and finding
	// your library rearranged.
	//
	// Like the robots switch, there is no reply of its own: the current values
	// ride on the Pong status, so what a screen draws is what the store says.
	MessageSetView MessageType = 75

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

	// MessageSetSourceProxy is UI→BE, JSON {sourceId, proxy}. An empty (or
	// all-whitespace) proxy removes it; anything else is validated the same
	// way the add-source flow validates one, and an invalid proxy leaves the
	// source untouched.
	//
	// Clearing the proxy on a source confirmed self-hosted *by way of* it
	// (SelfHosted.ViaProxy) also withdraws that confirmation: the address
	// lives on the far side of the proxy and this device never learns it, so
	// without the proxy the confirmation would claim a route that no longer
	// exists. A source confirmed by address keeps its confirmation.
	//
	// There is no reply of its own; the reply is a fresh MessageSources, like
	// MessageRenameSource.
	MessageSetSourceProxy MessageType = 76

	// The allowedHosts editor: record-and-offer, not prompt-on-first-sight.
	// When a fetch on a source's behalf is refused for being off that
	// source's registrable domain (fetch.GuardError.OffDomain), the refusal
	// is noted rather than interrupting anything, and offered here — in the
	// source list, where the owner is present — instead of in a modal that
	// would either appear unattended or be granted on reflex.
	//
	// MessageAllowSourceHost is UI→BE, JSON {sourceId, host}: appends host to
	// the source's allowedHosts and persists it. MessageRevokeSourceHost is
	// UI→BE, JSON {sourceId, host}: removes it. Neither has a reply of its
	// own; both are followed by a fresh MessageSources, like
	// MessageSetSourceProxy — the row's pending and allowed lists both
	// change, and a whole redraw is simpler than two more message shapes.
	MessageAllowSourceHost  MessageType = 77
	MessageRevokeSourceHost MessageType = 78

	// Private sources: a source marked private is kept off the normal source
	// list and out of the normal combined search, and lives in a list of its
	// own with its own combined search — the discretion feature described
	// beside theme.Source.Private.
	//
	// MessageSetSourcePrivate is UI→BE, JSON {sourceId, private}: the same
	// shape as MessageSetSourceEnabled, and for the same reason it is a
	// distinct message rather than a field bolted onto one of the rename/proxy
	// calls — one message, one meaning. There is no reply of its own; both
	// MessageSources and MessagePrivateSources follow, because marking or
	// unmarking a source moves it from one list to the other and either
	// screen, if open, has to redraw.
	MessageSetSourcePrivate MessageType = 79

	// MessageListPrivateSources is UI→BE, no payload: the private source
	// list's own fetch, parallel to MessageListSources. MessagePrivateSources
	// is BE→UI, JSON {sources: [...]} — the same sourceView shape
	// MessageSources uses, containing only the sources marked private.
	MessageListPrivateSources MessageType = 80
	MessagePrivateSources     MessageType = 81

	// MessageSearchAllPrivate is UI→BE, JSON {query, page, pageSize, kind}: the
	// private list's own combined search — `kind` is MessageSearchAll's own
	// filter, unchanged. It is a distinct message rather than
	// a flag on MessageSearchAll so that "which sources this touches" is
	// decided by which message was sent, not by a field a bug could leave at
	// its zero value — a boolean that defaults to "search everything" would
	// turn any dropped field into exactly the leak this feature exists to
	// prevent.
	//
	// The reply is MessageSearchAllResults, unchanged: it is already keyed to
	// one screen's request by page and query, and the private and normal
	// searches never run at the same time (see Service.searchAllPagerFor's
	// scoped cache key).
	MessageSearchAllPrivate MessageType = 82

	// Try: reading a chapter in Quire's own reader without downloading it and
	// without a library entry (milestone 1's Try feature). See
	// backend/service/tryreader.go for the whole exchange.
	//
	// MessageTryChapter is UI→BE, JSON {sourceId, seriesId, chapterId}: open a
	// session on this chapter, ending whichever was open. MessageTryReady is
	// BE→UI, JSON {sourceId, seriesId, chapterId, index, path, pageCount,
	// complete}: the session is open and its first page (index 0) is on disk.
	// `complete` says whether pageCount is the whole chapter yet — false for a
	// theme that named a fast first page and is still resolving the rest
	// (theme.FirstPageProber); MessageTryPageCount follows once it has.
	MessageTryChapter MessageType = 83
	MessageTryReady   MessageType = 84

	// MessageTryPage is BE→UI, JSON {sourceId, seriesId, chapterId, index,
	// path}: one page, fetched and downscaled, in answer to
	// MessageTryPageRequest — UI→BE, JSON {sourceId, seriesId, chapterId,
	// index} — which the reader sends when it turns to a page it does not
	// already have. A page not yet answered is not an error; the reader says
	// so on screen and waits (PLAN's e-ink rule: never a blank page standing
	// in for content).
	MessageTryPage        MessageType = 85
	MessageTryPageRequest MessageType = 86

	// MessageEndTry is UI→BE, JSON {sourceId, seriesId, chapterId}: leaving
	// the reader, which discards the session's cache directory and is the
	// entire cleanup Try needs — there is no reading position and no library
	// entry to also undo.
	MessageEndTry MessageType = 87

	// MessageTryPageCount is BE→UI, JSON {sourceId, seriesId, chapterId,
	// pageCount, complete, note}: the page count changed because a theme's
	// full page list resolved (or failed to) behind the fast first page
	// MessageTryReady already showed. `note` is set only when the list did
	// not resolve, in plain language, and is empty on an ordinary completion.
	MessageTryPageCount MessageType = 88

	MessageError MessageType = 90

	// Saved in Quire: comics and manga downloads default to Quire's own
	// storage and Quire's own reader (ui/TryReader.qml's "saved" mode) rather
	// than the reMarkable library — see backend/service/download.go's package
	// comment and PLAN §2 for the reasoning. "Send to library" (the
	// `destination` field on MessageEnqueueDownload) is the explicit opt-in
	// that keeps today's PDF-and-upload behaviour.
	//
	// MessageOpenSaved is UI→BE, JSON {sourceId, seriesId, chapterId}: open a
	// saved chapter in the reader. MessageSavedOpened is BE→UI, JSON
	// {sourceId, seriesId, chapterId, seriesTitle, chapterTitle, pages,
	// position}: every page path is local and known up front, so the reader
	// needs no TryPageRequest traffic for a saved chapter. A chapter that is
	// not saved, or whose files are gone from disk, answers MessageError
	// (code saved_missing) instead, and a record whose files are gone is
	// dropped rather than left to fail the same way again.
	MessageOpenSaved   MessageType = 91
	MessageSavedOpened MessageType = 92

	// MessageSavePosition is UI→BE, JSON {sourceId, seriesId, chapterId,
	// position}, with no reply: the reader's own page-turn bookmark, sent
	// debounced rather than on every turn. Try mode never sends this — there
	// is no Try record to remember a place in.
	MessageSavePosition MessageType = 93

	// MessageDeleteSaved is UI→BE, JSON {sourceId, seriesId, chapterId,
	// confirmed}: the same two-step shape as MessageDeleteDownload, because
	// deleting a saved chapter is also for good — os.RemoveAll of its
	// directory, never the Trash, since there is no xochitl document to put
	// there. MessageSavedDeleted is BE→UI, JSON {sourceId, seriesId,
	// chapterId, phase, message}, phase one of "confirm", "done" or "failed".
	//
	// The payload also takes optional volumeLabel and chapterIds: when
	// chapterIds is non-empty this deletes every saved chapter it names
	// (ignoring any that are not saved) instead of the single chapterId —
	// "delete this whole volume from Quire" in one round trip. The reply
	// gains chapterIds too: the ones actually deleted, so the UI can clear
	// each row's saved flag without a second fetch.
	MessageDeleteSaved  MessageType = 94
	MessageSavedDeleted MessageType = 95

	// Books: a theme.FileTheme source's chapters now render as page images
	// in Quire's own reader (backend/bookrender), rather than always going
	// to the reMarkable library (books-contract.md §B). Three existing
	// entry points are reused rather than branched on kind: MessageOpenSaved
	// (91) on a saved book answers MessageBookOpened instead of
	// MessageSavedOpened; MessageTryChapter (83) on a book source answers
	// MessageBookStatus progress then MessageBookOpened (mode "try") instead
	// of the old try_unavailable refusal; MessageSavePosition (93) works for
	// a book too, its position being a page index the backend turns into a
	// fraction and a text snippet itself.
	//
	// MessageBookOpened is BE→UI, JSON {sourceId, seriesId, chapterId,
	// title, mode: "saved"|"try", fixedLayout, pageCount, page,
	// toc:[{title,page,level}], settings:{font,size,margins,spacing,align},
	// fontChoices:[{id,label}], sizeMin, sizeMax, private, inLibrary}: the
	// book is open and its current page is ready. fixedLayout (PDF/XPS/CBZ)
	// tells the overlay to hide the layout controls the settings panel
	// otherwise shows. settings and fontChoices are the whole of what the
	// "Aa" panel needs to draw — the labels are the backend's own, per
	// PLAN §2, never invented in QML.
	MessageBookOpened MessageType = 96

	// MessageBookPageRequest is UI→BE, JSON {sourceId, seriesId, chapterId,
	// index}: fetch (or serve from cache) one page, requested on demand the
	// same way MessageTryPageRequest is. MessageBookPage is BE→UI, JSON
	// {sourceId, seriesId, chapterId, index, path}: the answer. A page not
	// yet rendered is not an error — the reader shows its own "not ready"
	// placeholder and waits, exactly as Try's does.
	MessageBookPageRequest MessageType = 97
	MessageBookPage        MessageType = 98

	// MessageSetReaderSettings is UI→BE, JSON {sourceId, seriesId,
	// chapterId, page, settings:{font,size,margins,spacing,align}}: change
	// the reader's global settings while a book is open. page is the page
	// on screen at the moment of the change, so the backend can re-anchor
	// the reading position across the re-layout the new settings require
	// (books-contract.md §B, Reader sessions) — it is not itself a save of
	// the reading position, which MessageSavePosition still owns.
	// MessageBookRelaid is BE→UI, JSON {sourceId, seriesId, chapterId,
	// pageCount, page, toc, settings}: the new layout, in place of whatever
	// page count and table of contents the reader had before — every cached
	// page path from the old layout is stale and must be dropped.
	MessageSetReaderSettings MessageType = 99
	MessageBookRelaid        MessageType = 100

	// MessageCloseBook is UI→BE, JSON {sourceId, seriesId, chapterId,
	// page}: leaving the reader. In "saved" mode this saves the position
	// (the same computation MessageSavePosition does) before ending the
	// session; in "try" mode it deletes the fetched file instead, the same
	// as MessageEndTry's Try cleanup. Either way it ends the render session
	// and removes its page cache — there is one book open at a time, the
	// same rule Try's single session already keeps.
	MessageCloseBook MessageType = 101

	// MessageBookStatus is BE→UI, JSON {sourceId, seriesId, chapterId,
	// message}: a progress sentence while a book is being fetched (Try),
	// opened or laid out ("Laying out the book…") — the book equivalent of
	// MessageDownloadProgress's running commentary, for the moments a book
	// session cannot yet answer with a page.
	MessageBookStatus MessageType = 102
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
	MessageSearchAll:             "SearchAll",
	MessageSearchAllResults:      "SearchAllResults",
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
	MessageDownloadNewChapters:   "DownloadNewChapters",
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
	MessageMarkSeen:              "MarkSeen",
	MessageListDownloaded:        "ListDownloaded",
	MessageDownloadedList:        "DownloadedList",
	MessageDeleteFolder:          "DeleteFolder",
	MessageFolderDeleted:         "FolderDeleted",
	MessageSetConsultRobots:      "SetConsultRobots",
	MessageSetView:               "SetView",
	MessageGetCacheSize:          "GetCacheSize",
	MessageCacheStatus:           "CacheStatus",
	MessageClearCache:            "ClearCache",
	MessageCacheConfirm:          "CacheConfirm",
	MessageSetSourceProxy:        "SetSourceProxy",
	MessageAllowSourceHost:       "AllowSourceHost",
	MessageRevokeSourceHost:      "RevokeSourceHost",
	MessageSetSourcePrivate:      "SetSourcePrivate",
	MessageListPrivateSources:    "ListPrivateSources",
	MessagePrivateSources:        "PrivateSources",
	MessageSearchAllPrivate:      "SearchAllPrivate",
	MessageTryChapter:            "TryChapter",
	MessageTryReady:              "TryReady",
	MessageTryPage:               "TryPage",
	MessageTryPageRequest:        "TryPageRequest",
	MessageEndTry:                "EndTry",
	MessageTryPageCount:          "TryPageCount",
	MessageError:                 "Error",
	MessageOpenSaved:             "OpenSaved",
	MessageSavedOpened:           "SavedOpened",
	MessageSavePosition:          "SavePosition",
	MessageDeleteSaved:           "DeleteSaved",
	MessageSavedDeleted:          "SavedDeleted",
	MessageBookOpened:            "BookOpened",
	MessageBookPageRequest:       "BookPageRequest",
	MessageBookPage:              "BookPage",
	MessageSetReaderSettings:     "SetReaderSettings",
	MessageBookRelaid:            "BookRelaid",
	MessageCloseBook:             "CloseBook",
	MessageBookStatus:            "BookStatus",
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
