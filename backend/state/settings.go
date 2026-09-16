package state

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
}

// RobotsConsulted applies the default of false.
func (s Settings) RobotsConsulted() bool { return s.ConsultRobots != nil && *s.ConsultRobots }

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
	return out
}
