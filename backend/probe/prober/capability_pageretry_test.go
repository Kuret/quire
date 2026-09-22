package prober_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.5 stage 5, Part C (2026-09-22): MangaHere's own false `partial` —
// its most *popular* series, which stage 5's empty-query listing samples
// first, is licensed and legitimately serves no pages, while the site itself
// works fine (see fanfox's licensedRE). Page extraction for a page-based
// theme now gets the same up-to-three-candidate retry the file-based branch
// already had: the first candidate whose chapters list succeeds but whose
// pages do not is not the last word, and a second search result that does
// have working pages must still be accepted.
func TestStageFivePageBasedThemeRetriesPastALicensedFirstCandidate(t *testing.T) {
	var pagesCalls []string
	th := stubTheme{
		id:    "alpha",
		score: 90,
		stubs: []theme.SeriesStub{
			{ID: "/series/licensed/", Title: "Licensed"},
			{ID: "/series/free/", Title: "Free"},
		},
		chaptersByID: map[string][]theme.Chapter{
			"/series/licensed/": {{ID: "/series/licensed/1/", Title: "Chapter 1", Number: 1}},
			"/series/free/":     {{ID: "/series/free/1/", Title: "Chapter 1", Number: 1}},
		},
		pagesErrByID: map[string]error{
			"/series/licensed/1/": errors.New("fanfox: /reader/1: the site says this series is licensed and it serves no pages for it"),
		},
		pagesByID: map[string][]string{
			"/series/free/1/": {"https://example.invalid/p/1.jpg"},
		},
		pagesCalls: &pagesCalls,
	}
	res := runWithTheme(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	}, th)

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok once the second candidate's pages worked", res.Verdict, res.Detail)
	}
	// Mutation (b): a retry capped at one candidate would never have reached
	// the second series' pages at all.
	if len(pagesCalls) != 2 || pagesCalls[0] != "/series/licensed/1/" || pagesCalls[1] != "/series/free/1/" {
		t.Errorf("pagesCalls = %v, want both candidates tried in order", pagesCalls)
	}
}

// The mirror case: every candidate's pages come back with the site's own
// precise refusal, not a made-up "broken" reading. The report must carry the
// theme's own explanation — the one fanfox already writes for a licensed
// series — through the retry rather than collapsing it into whatever
// generic wording a swallowed error would produce (mutation (c): retrying an
// error as though it were a routine empty result must not lose the error's
// own text).
func TestStageFivePageBasedThemeReportsTheRealReasonWhenEveryCandidateIsLicensed(t *testing.T) {
	th := stubTheme{
		id:    "alpha",
		score: 90,
		stubs: []theme.SeriesStub{
			{ID: "/series/one/", Title: "One"},
			{ID: "/series/two/", Title: "Two"},
		},
		chaptersByID: map[string][]theme.Chapter{
			"/series/one/": {{ID: "/series/one/1/", Title: "Chapter 1", Number: 1}},
			"/series/two/": {{ID: "/series/two/1/", Title: "Chapter 1", Number: 1}},
		},
		pagesErrByID: map[string]error{
			"/series/one/1/": errors.New("fanfox: /reader/1: the site says this series is licensed and it serves no pages for it"),
			"/series/two/1/": errors.New("fanfox: /reader/2: the site says this series is licensed and it serves no pages for it"),
		},
	}
	res := runWithTheme(t, map[string]themetest.Route{
		"GET /": {File: "home-unrecognised.html"},
	}, th)

	if res.Verdict != theme.VerdictPartial {
		t.Fatalf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
	}
	if res.Addable || res.Draft != nil {
		t.Fatal("a source where every sampled title was licensed must not be added")
	}
	if !strings.Contains(res.Detail, "licensed") {
		t.Errorf("detail %q does not carry the site's own reason through the retry", res.Detail)
	}
}

// Mutation (a): stageCapability must ask a theme.FirstPageProber for one page
// rather than calling Pages(), which for a family like doujinreader is the
// difference between one request and one per page. Pages() here fails the
// check outright if it is ever called, so a regression that ignored the
// interface and fell back to Pages() unconditionally is caught by a failing
// verdict, not merely a slower one.
func TestStageFiveUsesFirstPageProberInsteadOfPages(t *testing.T) {
	var firstPageCalled bool
	th := firstPageOnlyTheme{
		stubTheme: stubTheme{id: "alpha", score: 90},
	}
	th.firstPageCalled = &firstPageCalled

	res := runWithTheme(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	}, th)

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
	if !firstPageCalled {
		t.Error("FirstPage was never called; stage 5 fell back to Pages() despite the theme implementing theme.FirstPageProber")
	}
}

// firstPageOnlyTheme implements theme.FirstPageProber over stubTheme, and
// poisons Pages() so that calling it is a visible test failure rather than a
// silent, slower success.
type firstPageOnlyTheme struct {
	stubTheme
	firstPageCalled *bool
}

func (f firstPageOnlyTheme) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return nil, errors.New("stage 5 called Pages() instead of FirstPage")
}

func (f firstPageOnlyTheme) FirstPage(_ context.Context, _ *theme.Source, _ string) ([]string, error) {
	if f.firstPageCalled != nil {
		*f.firstPageCalled = true
	}
	return []string{"https://example.invalid/p/1.jpg"}, nil
}
