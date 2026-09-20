package prober

import (
	"context"
	"errors"
	"testing"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// probeQueryTheme is the minimal theme.Theme needed to exercise
// capabilitySearch's query choice directly, plus an optional
// theme.ProbeQuerier.
type probeQueryTheme struct {
	// queries records every query capabilitySearch asked Search with, in
	// order.
	queries *[]string
	// results, keyed by query, are what Search answers. A query with no entry
	// answers no error and no results.
	results map[string][]theme.SeriesStub
	// probeQuery, when non-empty, makes this theme implement
	// theme.ProbeQuerier.
	probeQuery string
}

func (p probeQueryTheme) ID() string                             { return "probe" }
func (p probeQueryTheme) Fingerprint(*probe.Page) int            { return 0 }
func (p probeQueryTheme) AllowedHosts() []string                 { return nil }
func (p probeQueryTheme) SuggestedName() string                  { return "" }
func (p probeQueryTheme) ValidateOverrides(map[string]any) error { return nil }
func (p probeQueryTheme) OverrideKeys() []theme.OverrideDoc      { return nil }

func (p probeQueryTheme) Search(_ context.Context, _ *theme.Source, q string, _ int) ([]theme.SeriesStub, error) {
	*p.queries = append(*p.queries, q)
	return p.results[q], nil
}

func (p probeQueryTheme) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return nil, errors.New("not used")
}
func (p probeQueryTheme) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return nil, errors.New("not used")
}
func (p probeQueryTheme) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return nil, errors.New("not used")
}

// probeQuerierTheme adds theme.ProbeQuerier to probeQueryTheme.
type probeQuerierTheme struct{ probeQueryTheme }

func (p probeQuerierTheme) ProbeQuery() string { return p.probeQuery }

// A theme with no opinion gets the package default fallback query.
func TestCapabilitySearchUsesDefaultQueryWithoutProbeQuerier(t *testing.T) {
	var queries []string
	th := probeQueryTheme{queries: &queries, results: map[string][]theme.SeriesStub{
		capabilityQuery: {{ID: "/x", Title: "X"}},
	}}
	r := &run{}
	stubs, note := r.capabilitySearch(context.Background(), th, &theme.Source{})

	if note != "" {
		t.Fatalf("note = %q, want none", note)
	}
	if len(stubs) != 1 {
		t.Fatalf("stubs = %v, want one result", stubs)
	}
	if len(queries) != 2 || queries[1] != capabilityQuery {
		t.Errorf("queries = %v, want the listing then %q", queries, capabilityQuery)
	}
}

// A theme.ProbeQuerier's own query replaces the package default fallback —
// this is what lets shelfmark ask "dune" instead of "one".
func TestCapabilitySearchUsesTheThemesProbeQuery(t *testing.T) {
	var queries []string
	th := probeQuerierTheme{probeQueryTheme{
		queries: &queries,
		results: map[string][]theme.SeriesStub{
			"dune": {{ID: "/dune", Title: "Dune"}},
		},
		probeQuery: "dune",
	}}
	r := &run{}
	stubs, note := r.capabilitySearch(context.Background(), th, &theme.Source{})

	if note != "" {
		t.Fatalf("note = %q, want none", note)
	}
	if len(stubs) != 1 || stubs[0].ID != "/dune" {
		t.Fatalf("stubs = %v, want the dune result", stubs)
	}
	if len(queries) != 2 || queries[1] != "dune" {
		t.Errorf("queries = %v, want the listing then %q, not the package default", queries, "dune")
	}
}

// The misleading-attribution fix: a theme that legitimately requires a query
// (its empty listing errors, by design) must not have that error surfaced as
// the reason the probe failed when the fallback query reaches the site and
// merely finds nothing.
func TestCapabilitySearchDoesNotBlameTheListingForARequiredQuery(t *testing.T) {
	th := listingErrorsTheme{err: errors.New("shelfmark: /api/metadata/search: HTTP 400")}
	r := &run{}
	_, note := r.capabilitySearch(context.Background(), th, &theme.Source{})

	if note == "" {
		t.Fatal("note is empty, want a refusal: both the listing and the fallback found nothing")
	}
	if got := note; got != "the site returned no results at all." {
		t.Errorf("note = %q, want the fallback's own outcome, not the listing's HTTP 400", got)
	}
}

// listingErrorsTheme fails only the empty-query listing, exactly as a theme
// that requires a search query does, and answers the fallback with nothing —
// the shape Shelfmark's own metadata search had against a query with no
// matches (measured 2026-09-20).
type listingErrorsTheme struct {
	probeQueryTheme
	err error
}

func (l listingErrorsTheme) Search(_ context.Context, _ *theme.Source, q string, _ int) ([]theme.SeriesStub, error) {
	if q == capabilityListing {
		return nil, l.err
	}
	return nil, nil
}

// The verdict booleans, asked directly.
//
// stageCapability ends the stage the moment the strong check fails, so on the
// live path a failed Confirm can only ever be seen alongside a Search that was
// never run. That makes the guards in ok() and addableDegraded() unreachable
// from outside — and unreachable is exactly the state in which a guard rots.
// Here they are asked the question they exist to answer.
func TestAFailedStrongCheckIsNeverAddable(t *testing.T) {
	// Everything a working source has, except that the site is not the
	// application it claimed to be.
	c := capability{
		Confirm:  stepResult{Note: "it is missing books_output_mode."},
		Search:   stepResult{OK: true, Count: 40},
		Series:   stepResult{OK: true},
		Chapters: stepResult{OK: true, Count: 34},
		Pages:    stepResult{OK: true, Count: 12},
		Image:    stepResult{OK: true, Count: 900},
	}

	if c.ok() {
		t.Error("a source that failed the strong check was reported as working")
	}
	// The degraded allowance is for a source that is what it says it is and
	// does part of the job. A site that is not the application is not a weaker
	// version of it.
	if c.addableDegraded() {
		t.Error("a source that failed the strong check was offered in a degraded state")
	}
	if got := c.failure(); got == "" {
		t.Error("the failure names no step, so the user is told nothing")
	}
}

// The file shape of the same question: a release step that failed is as fatal
// as a page image that could not be fetched, and a page step that was never run
// must not be read as a failure.
func TestFileCapabilityIgnoresThePageSteps(t *testing.T) {
	working := capability{
		fileBased: true,
		Confirm:   stepResult{OK: true},
		Search:    stepResult{OK: true, Count: 40},
		Series:    stepResult{OK: true},
		Chapters:  stepResult{OK: true, Count: 34},
		Release:   stepResult{OK: true, Count: 34},
		// Pages and Image are deliberately zero: a file theme has none, and
		// their absence must not be read as their failure.
	}
	if !working.ok() {
		t.Errorf("a working book source was refused: %s", working.failure())
	}

	broken := working
	broken.Release = stepResult{Note: "nothing to ask for."}
	if broken.ok() || broken.addableDegraded() {
		t.Error("a book source with nothing to fetch was accepted")
	}
}
