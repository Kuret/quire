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

	// MessageOpenInReader is UI→BE, JSON {documentUuid}.
	MessageOpenInReader MessageType = 50

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

	// MessageWatchList is BE→UI, JSON {watched: [...]}. The whole list, pushed
	// on attach and after any change to it.
	MessageWatchList MessageType = 63

	// MessageWatchUpdate is BE→UI, JSON — one series' state, streamed as each
	// check lands.
	//
	// One per series rather than a list at the end because the checks are
	// serialised by the §7.4 limiter at two seconds per host: forty watched
	// series is well over a minute, and a shell that waits for all of it before
	// drawing is a shell that looks broken.
	MessageWatchUpdate MessageType = 64

	// MessageError is BE→UI, JSON {code, message}.
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
	MessageSeriesDetail:          "SeriesDetail",
	MessageSeriesDetailResult:    "SeriesDetailResult",
	MessageEnqueueDownload:       "EnqueueDownload",
	MessageDownloadProgress:      "DownloadProgress",
	MessageCancelDownload:        "CancelDownload",
	MessageOpenInReader:          "OpenInReader",
	MessageWatchSeries:           "WatchSeries",
	MessageUnwatchSeries:         "UnwatchSeries",
	MessageCheckWatched:          "CheckWatched",
	MessageWatchList:             "WatchList",
	MessageWatchUpdate:           "WatchUpdate",
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
