package state

import (
	"errors"
	"fmt"
)

// Settings is the device-wide configuration that is not about any one source.
//
// It rides in the same envelope as the sources (state.go's `file`) rather than
// in a file of its own, for the same reason the watch list does: the envelope
// is what a user exports and imports to move a setup between devices, and a
// setting that did not travel with it would be a surprise on the other side.
//
// The shape is mirrored by schema/settings.schema.json.
type Settings struct {
	// ConsultRobots turns the robots.txt consultation on.
	//
	// **Off by default** (PLAN §7.4, superseded 2026-09-16): RFC 9309 scopes
	// robots.txt to "automatic clients known as crawlers", a person searching
	// and tapping is driving every request, and no comparable reader consults
	// it at all. There is deliberately no per-source equivalent — one switch,
	// one behaviour, nothing to reason about per site.
	//
	// It is a pointer so "absent" and "false" stay distinguishable in the
	// stored file, the same way theme.Source.Enabled is, and so that a future
	// change of default does not silently rewrite what an existing file meant.
	//
	// It narrows nothing else: the rate limits, the per-host delay, the honest
	// User-Agent, the size cap, the byte accounting and the SSRF guard apply in
	// full either way, and PLAN §7.6's no-circumvention rule is untouched.
	ConsultRobots *bool `json:"consultRobots,omitempty"`

	// The per-screen layout of PLAN §7.1 type 75: "grid" of covers or "list" of
	// rows, remembered for each screen separately.
	//
	// **One field per screen, not a map of screen to view.** A map would make
	// adding a screen a one-line change, and that is exactly what makes it the
	// wrong shape here: the envelope is what a user exports and imports between
	// devices, schema/settings.schema.json declares `additionalProperties:
	// false` so that a key nobody declared is a mistake rather than a setting,
	// and a map would round-trip any key at all straight back to disk. It would
	// also make `Settings` uncomparable, which state.go's save() relies on to
	// tell a store nobody configured from one that was.
	//
	// Pointers for the same reason ConsultRobots is one: "never chosen" and
	// "chosen, and it happens to be the default" stay distinguishable, so a
	// future change of default does not silently rewrite what an existing file
	// meant.
	SearchView     *string `json:"searchView,omitempty"`
	DownloadedView *string `json:"downloadedView,omitempty"`
	WatchingView   *string `json:"watchingView,omitempty"`
}

// RobotsConsulted applies the default of false.
func (s Settings) RobotsConsulted() bool { return s.ConsultRobots != nil && *s.ConsultRobots }

// The two layouts a screen can be in (PLAN §7.1 type 75).
const (
	// ViewGrid is covers, and is the default for every screen. Recognising a
	// book you already know is what a cover is for, and that is what Downloaded
	// and Watching are both for; search results are the case where a list wins
	// often enough to be worth storing, which is why this is per screen.
	ViewGrid = "grid"
	// ViewList is rows: title, and the sources it was found in.
	ViewList = "list"
)

// Screen names, as sent in MessageSetView's {screen, view}. Spelled out rather
// than derived, because they are wire values: a screen renamed in the frontend
// must not silently start reading a different setting.
const (
	ScreenSearch     = "search"
	ScreenDownloaded = "downloaded"
	ScreenWatching   = "watching"
)

// Screens is every screen that remembers a view, in the order the settings are
// written. Iteration order is fixed so the status payload does not shuffle.
var Screens = []string{ScreenSearch, ScreenDownloaded, ScreenWatching}

// ErrUnknownScreen and ErrUnknownView are what SetView refuses with.
//
// A bad screen or a bad view is an error and never a stored value. The
// alternative — store it and let the frontend cope — puts a value in the
// exported envelope that no version of Quire can draw, and the device it is
// imported onto has no way to tell it from something it is simply too old to
// know about.
var (
	ErrUnknownScreen = errors.New("state: no such screen")
	ErrUnknownView   = errors.New("state: a view is either \"grid\" or \"list\"")
)

// View returns the stored layout for a screen, applying the default of "grid".
//
// It cannot fail, and that is deliberate. An unknown screen, an absent value
// and a *stored* value that is not "grid" or "list" all read as the default
// rather than as an error: this is the reader the drawing code calls, and a
// hand-edited file, or one imported from a newer Quire that knows a third
// layout, must leave the screen drawable rather than empty. SetView is where a
// bad value is refused, on the way in, which is the only place refusing it can
// still tell the user something.
//
// Nothing is rewritten on the way through. The unreadable value stays in the
// file untouched, so a round trip back to the device that wrote it still means
// what it meant there.
func (s Settings) View(screen string) string {
	var stored *string
	switch screen {
	case ScreenSearch:
		stored = s.SearchView
	case ScreenDownloaded:
		stored = s.DownloadedView
	case ScreenWatching:
		stored = s.WatchingView
	default:
		return ViewGrid
	}
	if stored == nil || (*stored != ViewGrid && *stored != ViewList) {
		return ViewGrid
	}
	return *stored
}

// Views is every screen's current view, for the Pong status. Built from View so
// the frontend is told the same thing the drawing code would work out.
func (s Settings) Views() map[string]string {
	out := make(map[string]string, len(Screens))
	for _, screen := range Screens {
		out[screen] = s.View(screen)
	}
	return out
}

// SetView stores one screen's layout and saves.
//
// Both halves of the pair are validated before anything is written, so a
// refusal leaves the file exactly as it was.
func (s *Store) SetView(screen, view string) error {
	if view != ViewGrid && view != ViewList {
		return fmt.Errorf("%q: %w", view, ErrUnknownView)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := view
	switch screen {
	case ScreenSearch:
		s.settings.SearchView = &v
	case ScreenDownloaded:
		s.settings.DownloadedView = &v
	case ScreenWatching:
		s.settings.WatchingView = &v
	default:
		return fmt.Errorf("%q: %w", screen, ErrUnknownScreen)
	}
	return s.save()
}

// Settings returns the stored settings.
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copySettings(s.settings)
}

// SetConsultRobots stores the robots.txt setting and saves.
//
// It does not itself reach the fetch layer: the caller that owns the client
// calls fetch.Client.SetConsultRobots, because state has no business importing
// the HTTP client and the client has no business reading files.
func (s *Store) SetConsultRobots(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := on
	s.settings.ConsultRobots = &v
	return s.save()
}

func copySettings(in Settings) Settings {
	out := in
	if in.ConsultRobots != nil {
		v := *in.ConsultRobots
		out.ConsultRobots = &v
	}
	out.SearchView = copyString(in.SearchView)
	out.DownloadedView = copyString(in.DownloadedView)
	out.WatchingView = copyString(in.WatchingView)
	return out
}

func copyString(in *string) *string {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}
