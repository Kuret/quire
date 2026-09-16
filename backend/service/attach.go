package service

import (
	"log/slog"

	"github.com/rickl/quire/backend/appload"
)

// AbnormalExitNotice is what the user is told when the previous session did not
// end properly.
//
// It is deliberately quiet and non-accusatory. The user did nothing wrong, the
// most likely cause is the backend being killed for memory on a device that
// shares ~2 GB with xochitl, and there is nothing for them to do about it
// except know that it happened — Quire disappearing mid-download with no
// explanation is what they actually saw.
const AbnormalExitNotice = "Quire closed unexpectedly last time. Nothing was lost — your sources and " +
	"anything already saved to your library are still here."

// FrontendAttached pushes the state a freshly-loaded frontend needs, without
// being asked.
//
// This exists because AppLoad **drops** messages sent to a backend whose socket
// is not up yet — it logs "No active socket for ID:quire" and discards the
// frame rather than queueing it. A ListSources sent from QML's
// Component.onCompleted therefore races the backend's connect, and when it
// loses, the user is shown an empty source list forever, because nothing ever
// asks again. Twice now that has looked to the user like their configured
// sources had been lost, when the state file was perfectly intact.
//
// Pushing on attach removes the race by construction: the backend cannot send
// this too early, because the message that triggers it *is* the frontend
// arriving. A retry or a delay in the QML would only narrow the window, and an
// app that silently claims the user's sources do not exist deserves better than
// a narrower window.
//
// Requests still work. This is an addition, not a replacement: the UI may ask
// again whenever it likes, and asking is still the only way to refresh.
func (s *Service) FrontendAttached(out Sender) error {
	if err := s.sendSources(out); err != nil {
		return err
	}

	// PLAN §12.2's watched series ride the same push, for the same reason: a
	// frontend that has to ask for the list races the socket coming up, and a
	// badge that silently never appears is worse than no badge at all.
	if err := s.sendWatchList(out); err != nil {
		return err
	}

	// And this is the *only* automatic trigger for a check. The app being
	// opened is the event; there is no timer behind it, nothing runs while the
	// frontend is away (FrontendDetached stops it), and Quire holds no wakelock
	// (PLAN §6 M7). It is started rather than waited for: the results stream in
	// one series at a time so the shell can draw immediately.
	s.startWatchCheck(out, false, nil)
	return nil
}

// StartupNotice is the one-off sentence to show a frontend on attach, or "" if
// there is nothing to say. It rides on the status message rather than in a
// message type of its own; PLAN §7.1's table is not worth growing for this.
func (s *Service) StartupNotice() string {
	if s.previousSessionCrashed {
		return AbnormalExitNotice
	}
	return ""
}

// FrontendDetached stops work that has no business continuing with nobody
// watching.
//
// PLAN §6 M7: "Never hold wifi awake for background work. Downloads only while
// foregrounded." Quire never acquires a wakelock, so it cannot hold wifi up
// directly — but a download running on after the user has closed the app keeps
// the radio busy and pegs a core for minutes at a time, which has the same
// effect on their battery and is the specific failure the plan names another
// extension for. It is also the work most likely to get us OOM-killed while
// the user is not there to see why.
//
// Stopping is safe precisely because cancel means *stop, not discard*: fetched
// pages stay on disk and resume skips them, so reopening Quire carries on from
// where it left off. Parts of a split volume that already reached the library
// stay there, as they do for any other cancel.
func (s *Service) FrontendDetached(log *slog.Logger) {
	if log == nil {
		log = s.log
	}

	// The watched-series check goes first: it is a poll for a convenience badge
	// nobody is looking at any more, which makes it the least defensible thing
	// on the radio once the frontend has gone.
	s.stopWatchCheck()

	s.dlMu.Lock()
	active := make([]downloadKey, 0, len(s.dlActive))
	cancels := make([]func(), 0, len(s.dlActive))
	for key, cancel := range s.dlActive {
		active = append(active, key)
		cancels = append(cancels, cancel)
	}
	s.dlMu.Unlock()

	// Drain anything still queued as well: it has not started, and starting it
	// now would be beginning new background work at exactly the wrong moment.
	//
	// Each drained job is told it stopped rather than silently dropped. The
	// frontend may well be gone, in which case the send goes nowhere and costs
	// nothing — but if another one has already attached, a queued row that
	// never resolves is a progress bar that hangs for ever.
	drained := 0
	for {
		select {
		case job := <-s.dlQueue:
			drained++
			_ = send(job.out, appload.MessageDownloadProgress, downloadProgress{
				SourceID: job.req.SourceID, SeriesID: job.req.SeriesID,
				VolumeID: job.req.VolumeID, Grouping: job.req.grouping(),
				Phase:   phaseCancelled,
				Message: "Stopped, because Quire was closed. Start it again to carry on.",
			})
			continue
		default:
		}
		break
	}

	for _, cancel := range cancels {
		cancel()
	}
	if len(cancels) > 0 || drained > 0 {
		log.Info("paused downloads because the frontend went away",
			"running", len(active), "queued", drained)
	}
}
