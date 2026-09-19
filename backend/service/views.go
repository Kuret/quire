package service

import (
	"errors"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/state"
)

// The per-screen layout switch — PLAN §7.1 type 75.
//
// It is stored per screen and not globally. Searching wants a list often enough
// — a row carries the title and the sources it was found in, where a tile
// carries a cover — while Downloaded and Watching are for recognising something
// you already know, which is what covers are good at. One setting would mean
// choosing a list to read search results and finding your library rearranged.

// Views reports the stored layout of every screen, which is what rides on the
// Pong status. Read from the store rather than remembered here, so what the user
// sees is what will still be true after a restart.
//
// A service with no store answers the defaults rather than nothing: the shell
// has to draw something, and "grid everywhere" is what a fresh device looks
// like anyway.
func (s *Service) Views() map[string]string {
	if s.store == nil {
		return state.Settings{}.Views()
	}
	return s.store.Settings().Views()
}

// handleSetView answers MessageSetView. Like the robots switch it has no reply
// of its own: the frontend pings, and the current values come back on the
// status, so what a screen draws is what the store says rather than what it
// hoped.
func (s *Service) handleSetView(out Sender, payload []byte) (bool, error) {
	var req struct {
		Screen string `json:"screen"`
		View   string `json:"view"`
	}
	if err := decode(payload, &req); err != nil {
		return true, s.sendError(out, "bad_request", err.Error())
	}
	if err := s.store.SetView(req.Screen, req.View); err != nil {
		// An unknown screen or view is the frontend asking for something that
		// does not exist, not a device that could not write — and the two want
		// different codes, because only one of them is worth retrying.
		if errors.Is(err, state.ErrUnknownScreen) || errors.Is(err, state.ErrUnknownView) {
			return true, s.sendError(out, "bad_request", plain(err))
		}
		return true, s.sendError(out, "not_saved", plain(err))
	}
	s.log.Info("screen view changed", "screen", req.Screen, "view", req.View)
	return true, nil
}

// viewMessage is the type handleSetView answers, kept beside it so the switch in
// service.go reads as one line.
const viewMessage = appload.MessageSetView
