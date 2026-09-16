package state

import (
	"fmt"
	"log/slog"

	"github.com/rickl/quire/backend/theme"
)

// CurrentVersion is the envelope version this build writes. A file found at a
// lower version is migrated forward on load; a file from the future is refused,
// because guessing at a shape a newer Quire wrote is how a user's sources get
// mangled by a downgrade.
const CurrentVersion = 3

// migration is one forward step. Steps are applied in order, each taking the
// file from To-1 to To, and the envelope version is written only once every
// step has succeeded — so a migration that fails leaves the file exactly as it
// was rather than half-converted.
type migration struct {
	To   int
	Name string

	// Apply mutates the loaded file in place and reports whether it changed
	// anything. A migration that finds nothing to do is normal, not an error.
	Apply func(f *file, reg *theme.Registry, log *slog.Logger) (bool, error)
}

// migrations is the ordered list. Append; never renumber, never edit a released
// step. A step's job is to make an *old* file valid for the current code, and
// rewriting history breaks the one device that skipped a version.
var migrations = []migration{
	{To: 2, Name: "seed allowedHosts from the theme", Apply: seedAllowedHosts},
	{To: 3, Name: "drop watched series with no source", Apply: dropOrphanWatches},
}

// migrate brings a loaded file up to CurrentVersion.
//
// It reports whether anything changed, so the caller can write the file back
// exactly when there is something to write. A store that is already current
// must not be rewritten on every launch: a needless write is a needless chance
// to lose the file to a power cut.
func migrate(f *file, reg *theme.Registry, log *slog.Logger) (bool, error) {
	if f.Version > CurrentVersion {
		return false, fmt.Errorf(
			"state: this file was written by a newer version of Quire (format %d, this build understands %d)",
			f.Version, CurrentVersion)
	}
	if f.Version == CurrentVersion {
		return false, nil
	}

	changed := false
	for _, m := range migrations {
		if f.Version >= m.To {
			continue
		}
		did, err := m.Apply(f, reg, log)
		if err != nil {
			return false, fmt.Errorf("state: migrating to format %d (%s): %w", m.To, m.Name, err)
		}
		log.Info("migrated the source store", "to", m.To, "migration", m.Name, "changed", did)
		changed = changed || did
	}

	f.Version = CurrentVersion
	return true, nil
}

// seedAllowedHosts is migration #1, and it exists because of a real bug rather
// than as a demonstration.
//
// Themes gained AllowedHosts after the first sources were stored (PLAN §7.2),
// and §7.5 stage 6 seeds a *new* source from the theme's declaration. A source
// added before that has an empty list — so a MangaDex source passes search,
// series and chapters, and then fails at the last moment with an SSRF refusal,
// because the page images come from *.mangadex.network and nothing says that is
// allowed.
//
// **This is deliberately not "seed whenever the list is empty".** An empty list
// is a legitimate state: a user may have cleared it on purpose, and a rule that
// re-seeds on sight would silently undo them every launch, with no way to say
// no. Running exactly once, keyed on the envelope version, is the whole
// difference — and is why the store carries a version at all.
func seedAllowedHosts(f *file, reg *theme.Registry, log *slog.Logger) (bool, error) {
	if reg == nil {
		return false, nil
	}
	changed := false
	for _, src := range f.Sources {
		if len(src.AllowedHosts) > 0 {
			continue
		}
		th, ok := reg.Lookup(src.Theme)
		if !ok {
			// A theme Quire no longer ships. Nothing to seed from, and not a
			// reason to fail the migration: the source is already reported as
			// unusable elsewhere.
			continue
		}
		hosts := th.AllowedHosts()
		if len(hosts) == 0 {
			continue
		}
		src.AllowedHosts = append([]string(nil), hosts...)
		changed = true
		log.Info("seeded allowedHosts for an existing source",
			"source", src.ID, "theme", src.Theme, "hosts", hosts)
	}
	return changed, nil
}

// dropOrphanWatches is migration #2: the envelope gained PLAN §12.2's watched
// series, and this is the step that says so.
//
// A file written before the watch list simply has none, and on that file this
// step finds nothing to do — which is the normal case and is why it reports
// false rather than rewriting the store for nothing.
//
// Its real work is the invariant it enforces on the way in: a watch whose
// source is not in the file is dropped. Store.Remove takes the watches with the
// source, so Quire itself does not create orphans — but this file is also
// hand-editable and importable from another device (PLAN §7.2), and an imported
// half of someone else's setup is exactly where a watch naming a source that
// was never brought across would arrive. A watch like that can never be checked
// (no source, so no theme) and can never be cleared from the UI (no row to put
// it on), so the only honest thing to do with it is not to keep it.
func dropOrphanWatches(f *file, _ *theme.Registry, log *slog.Logger) (bool, error) {
	if len(f.Watched) == 0 {
		return false, nil
	}
	known := make(map[string]struct{}, len(f.Sources))
	for _, src := range f.Sources {
		known[src.ID] = struct{}{}
	}
	kept := make([]*Watch, 0, len(f.Watched))
	changed := false
	for _, w := range f.Watched {
		if _, ok := known[w.SourceID]; !ok {
			changed = true
			log.Info("dropped a watched series whose source is gone",
				"source", w.SourceID, "series", w.SeriesID, "title", w.Title)
			continue
		}
		kept = append(kept, w)
	}
	f.Watched = kept
	return changed, nil
}
