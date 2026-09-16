package prober_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.5 stage 5: "a search (or the popular/latest listing if search needs a
// query)".
//
// CORRECTION 2026-09-16 — the listing goes first, and the fallback query is
// never shorter than three characters. M3 probed with "a", on the reasoning
// that the listing had nowhere to be called from; PLAN §12.1 then defined
// Browse as a search with an empty query, which is exactly that call. Measured
// the same day: comick.art answers 444 with an empty body to `q=a` and `q=ab`,
// and 200 with a full catalogue to `q=dra` and to no query at all. Probing with
// one character manufactured the failure Quire then reported as the site's.

// querySpy records what stage 5 actually asks for. Everything except Search is
// the ordinary stub, so the only thing under test is the query.
type querySpy struct {
	stubTheme

	// listingEmpty makes the empty-query listing return nothing, which is the
	// search-only front page the fallback exists for.
	listingEmpty bool
	// listingErr makes the listing fail outright — a theme or site that will
	// not answer without a query at all.
	listingErr bool

	mu      sync.Mutex
	queries []string
}

func (q *querySpy) Search(ctx context.Context, src *theme.Source, query string, page int) ([]theme.SeriesStub, error) {
	q.mu.Lock()
	q.queries = append(q.queries, query)
	q.mu.Unlock()

	if query == "" {
		if q.listingErr {
			return nil, errors.New("alpha: /browse: HTTP 500")
		}
		if q.listingEmpty {
			return nil, nil
		}
	}
	return q.stubTheme.Search(ctx, src, query, page)
}

func (q *querySpy) asked() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.queries...)
}

func searchRoutes() map[string]themetest.Route {
	return map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	}
}

// The listing is what a user sees when they open Browse, so it is both the
// gentlest request to make and the most representative. When it answers, stage
// 5 asks for nothing else.
func TestStageFivePrefersTheListing(t *testing.T) {
	spy := &querySpy{stubTheme: stubTheme{id: "alpha", score: 90}}
	res := runWithTheme(t, searchRoutes(), spy)

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
	asked := spy.asked()
	if len(asked) != 1 || asked[0] != "" {
		t.Errorf("stage 5 asked %q; the listing answered, so nothing else should have been asked", asked)
	}
}

// The fallback exists for a site whose front page lists nothing, and only for
// that. A site with a catalogue must never see the invented query.
func TestStageFiveFallsBackOnlyWhenTheListingIsEmpty(t *testing.T) {
	t.Run("empty listing", func(t *testing.T) {
		spy := &querySpy{stubTheme: stubTheme{id: "alpha", score: 90}, listingEmpty: true}
		res := runWithTheme(t, searchRoutes(), spy)

		if res.Verdict != theme.VerdictOK || !res.Addable {
			t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
		}
		asked := spy.asked()
		if len(asked) != 2 || asked[0] != "" {
			t.Fatalf("stage 5 asked %q, want the listing then one query", asked)
		}
		if asked[1] == "" {
			t.Error("the fallback repeated the empty listing instead of searching")
		}
	})

	t.Run("listing refuses", func(t *testing.T) {
		spy := &querySpy{stubTheme: stubTheme{id: "alpha", score: 90}, listingErr: true}
		res := runWithTheme(t, searchRoutes(), spy)

		// A site that cannot list but can search is a working source.
		if res.Verdict != theme.VerdictOK || !res.Addable {
			t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
		}
		if asked := spy.asked(); len(asked) != 2 {
			t.Errorf("stage 5 asked %q, want the listing then one query", asked)
		}
	})
}

// The rule the comick probe cost us: a query short enough for an edge to treat
// as abuse is a failure Quire generated itself.
func TestStageFiveNeverProbesWithAShortQuery(t *testing.T) {
	for _, spy := range []*querySpy{
		{stubTheme: stubTheme{id: "alpha", score: 90}},
		{stubTheme: stubTheme{id: "alpha", score: 90}, listingEmpty: true},
		{stubTheme: stubTheme{id: "alpha", score: 90}, listingErr: true},
	} {
		runWithTheme(t, searchRoutes(), spy)
		for _, q := range spy.asked() {
			if q == "" {
				continue // the listing, which asks for nothing at all
			}
			if len([]rune(q)) < 3 {
				t.Errorf("stage 5 searched for %q; queries under three characters are what sites drop", q)
			}
			if strings.TrimSpace(q) == "" {
				t.Errorf("stage 5 searched for %q, which is whitespace, not a query", q)
			}
		}
	}
}
