package theme

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/rickl/quire/backend/probe"
)

// GenericID is the raw-selector escape hatch. It is the only theme for which a
// source may carry `selectors` or `script` — both schema/source.schema.json and
// Registry.Validate enforce that, deliberately twice: a source can arrive via
// an import that never met the schema.
const GenericID = "generic"

// ErrUnknownTheme is returned when a source names a theme nobody registered.
var ErrUnknownTheme = errors.New("unknown theme")

// Registry holds the registered themes and answers the two questions the rest
// of the system asks of them: "give me the theme called X" (browsing) and "ask
// every theme to score this page" (PLAN §7.5 stage 4).
type Registry struct {
	mu     sync.RWMutex
	themes map[string]Theme
}

// NewRegistry returns an empty registry. There is no package-level default
// registry on purpose: a global would make it possible for a test, or a future
// plugin, to change what themes exist behind the caller's back.
func NewRegistry() *Registry {
	return &Registry{themes: make(map[string]Theme)}
}

// Register adds t. A duplicate ID is an error rather than a silent overwrite,
// because a silently shadowed theme would show up as a mysteriously wrong
// fingerprint months later.
func (r *Registry) Register(t Theme) error {
	id := t.ID()
	if id == "" {
		return errors.New("theme: cannot register a theme with an empty ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.themes[id]; exists {
		return fmt.Errorf("theme: %q is already registered", id)
	}
	r.themes[id] = t
	return nil
}

// MustRegister is Register for wiring code that cannot meaningfully continue.
func (r *Registry) MustRegister(t Theme) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Lookup returns the theme with the given ID.
func (r *Registry) Lookup(id string) (Theme, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.themes[id]
	return t, ok
}

// IDs returns every registered theme ID, sorted.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.themes))
	for id := range r.themes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Score is one theme's opinion of a page.
type Score struct {
	ThemeID string `json:"themeId"`
	Score   int    `json:"score"`
}

// Fingerprint is PLAN §7.5 stage 4: ask *every* registered theme to score the
// page, and return the scores sorted best first. The full list is returned,
// not just the winner, because §7.5 wants an "unrecognised" verdict to be able
// to name what was tried rather than just shrugging.
//
// The generic theme is excluded: it is the escape hatch a user reaches for
// deliberately, and letting it compete would turn "we don't recognise this
// site" into "we sort of recognise this site", which is the one answer the
// probe must never give.
func (r *Registry) Fingerprint(p *probe.Page) []Score {
	r.mu.RLock()
	themes := make([]Theme, 0, len(r.themes))
	for id, t := range r.themes {
		if id == GenericID {
			continue
		}
		themes = append(themes, t)
	}
	r.mu.RUnlock()

	scores := make([]Score, 0, len(themes))
	for _, t := range themes {
		n := t.Fingerprint(p)
		scores = append(scores, Score{ThemeID: t.ID(), Score: clamp(n)})
	}
	// Best first; ties broken by ID so the order is reproducible and a
	// near-miss between two themes is a stable, testable fact.
	slices.SortFunc(scores, func(a, b Score) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return cmp.Compare(a.ThemeID, b.ThemeID)
	})
	return scores
}

// ScoreMap is Fingerprint's result in the shape ProbeResult.ThemeScores wants.
func ScoreMap(scores []Score) map[string]int {
	m := make(map[string]int, len(scores))
	for _, s := range scores {
		m[s.ThemeID] = s.Score
	}
	return m
}

func clamp(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

// Validate checks a source against the registry: the theme exists, the URL is
// usable, the escape-hatch fields are only used by the escape hatch, and the
// overrides contain no key the theme does not declare.
//
// It is the one gate every source passes through before it is used, so a
// mistake surfaces when the user adds or imports the source rather than three
// screens later as an empty search result.
func (r *Registry) Validate(s *Source) error {
	if s == nil {
		return errors.New("theme: nil source")
	}
	if s.ID == "" {
		return errors.New("theme: source has no id")
	}
	t, ok := r.Lookup(s.Theme)
	if !ok {
		return fmt.Errorf("theme: source %q: %w %q; registered themes are %s",
			s.ID, ErrUnknownTheme, s.Theme, strings.Join(r.IDs(), ", "))
	}

	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return fmt.Errorf("theme: source %q: parse baseUrl: %w", s.ID, err)
	}
	if !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("theme: source %q: baseUrl must be an absolute http(s) URL, got %q", s.ID, s.BaseURL)
	}

	// The schema says this with an allOf clause. Saying it again here is not
	// belt-and-braces: an imported or hand-edited source reaches this struct
	// without ever meeting the schema.
	if s.Theme != GenericID {
		if len(s.Selectors) > 0 {
			return fmt.Errorf("theme: source %q: selectors are only valid when theme is %q, not %q", s.ID, GenericID, s.Theme)
		}
		if s.Script != "" {
			return fmt.Errorf("theme: source %q: script is only valid when theme is %q, not %q", s.ID, GenericID, s.Theme)
		}
	}

	if !validSplitStrips(s.SplitStrips) {
		return fmt.Errorf("theme: source %q: splitStrips must be one of %s, got %q",
			s.ID, strings.Join(SplitStripsValues, ", "), s.SplitStrips)
	}

	if !validGrouping(s.Grouping) {
		return fmt.Errorf("theme: source %q: grouping must be one of %s, got %q",
			s.ID, strings.Join(GroupingValues, ", "), s.Grouping)
	}
	if s.GroupSize != 0 && (s.GroupSize < GroupSizeMin || s.GroupSize > GroupSizeMax) {
		return fmt.Errorf("theme: source %q: groupSize must be between %d and %d, got %d",
			s.ID, GroupSizeMin, GroupSizeMax, s.GroupSize)
	}

	if v, ok := t.(OverrideValidator); ok {
		if err := v.ValidateOverrides(s.Overrides); err != nil {
			return fmt.Errorf("theme: source %q: %w", s.ID, err)
		}
	} else if len(s.Overrides) > 0 {
		return fmt.Errorf("theme: source %q: theme %q accepts no overrides, got %d", s.ID, s.Theme, len(s.Overrides))
	}

	// Last, anything only the theme itself can check — the generic theme's
	// selector vocabulary and script, which must be rejected when the source
	// is added rather than when a chapter is opened.
	if v, ok := t.(SourceValidator); ok {
		if err := v.Validate(s); err != nil {
			return fmt.Errorf("theme: source %q: %w", s.ID, err)
		}
	}
	return nil
}
