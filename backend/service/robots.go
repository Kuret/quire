package service

import (
	"github.com/rickl/quire/backend/appload"
)

// The one global robots.txt switch — PLAN §7.4, rewritten 2026-09-16.
//
// It is off by default. RFC 9309 scopes robots.txt to "automatic clients known
// as crawlers", and a person searching a site and tapping a series is driving
// every request Quire makes. The switch exists so that someone who wants the
// stricter behaviour can have it, not because the default is in doubt.
//
// It narrows nothing else. The rate limits, the per-host delay, the honest
// User-Agent, the size cap, the byte accounting and the SSRF guard all apply in
// full either way, and PLAN §7.6's no-circumvention rule is untouched.

// RobotsSwitch is implemented by a fetcher whose robots.txt consultation can be
// changed while it is running.
//
// It is a side interface rather than a method on theme.Fetcher for the same
// reason theme.DiscoveryClassifier is: Fetcher is the slice of the client a
// *theme* needs, and a theme has no business turning this on or off.
type RobotsSwitch interface {
	SetConsultRobots(on bool)
	ConsultRobots() bool
}

// ConsultRobots reports the stored setting, which is what the settings screen
// draws. It is read from the store rather than from the client so that what the
// user sees is what will still be true after a restart.
func (s *Service) ConsultRobots() bool {
	if s.store == nil {
		return false
	}
	return s.store.Settings().RobotsConsulted()
}

// setConsultRobots persists the setting and applies it to the live client.
//
// Both halves, or the setting lies: persisting alone gives a toggle that does
// nothing until the next restart, and applying alone gives one that forgets
// what it was told. The store is written first, because a setting that took
// effect and was then lost is the worse of the two failures — the user would
// have watched it work.
func (s *Service) setConsultRobots(out Sender, on bool) error {
	if err := s.store.SetConsultRobots(on); err != nil {
		return s.sendError(out, "not_saved", plain(err))
	}

	sw, ok := s.fetch.(RobotsSwitch)
	if !ok {
		// Only reachable with a fetcher built for a test; quired always passes
		// the real client. Saying so is cheap and the alternative is a silent
		// half-applied setting.
		s.log.Warn("robots.txt setting saved but not applied: this fetcher cannot be switched",
			"consulted", on)
		return nil
	}
	sw.SetConsultRobots(on)
	s.log.Info("robots.txt setting changed", "consulted", sw.ConsultRobots())
	return nil
}

// handleRobots answers MessageSetConsultRobots. There is no reply of its own:
// the frontend pings, and the current value comes back on the status, so what
// it draws is what the store says rather than what it hoped.
func (s *Service) handleRobots(out Sender, payload []byte) (bool, error) {
	var req struct {
		ConsultRobots bool `json:"consultRobots"`
	}
	if err := decode(payload, &req); err != nil {
		return true, s.sendError(out, "bad_request", err.Error())
	}
	return true, s.setConsultRobots(out, req.ConsultRobots)
}

// robotsMessage is the type handleRobots answers, kept beside it so the switch
// in service.go reads as one line.
const robotsMessage = appload.MessageSetConsultRobots
