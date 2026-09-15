package service

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
	return s.sendSources(out)
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
