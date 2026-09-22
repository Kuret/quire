package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// --- the fakes -------------------------------------------------------------

// fakeSearchTheme is every source in these tests: one theme serving a scripted
// set of source pages per source ID. No HTTP, because what is under test is the
// merge and the paging, and a test that had to route five fake sites would be
// testing themetest.
type fakeSearchTheme struct {
	mu sync.Mutex

	// pages[sourceID][sourcePage-1] is what that source returns for that page.
	// Past the end it returns nothing, which is how a source runs dry.
	pages map[string][][]theme.SeriesStub

	// fail[sourceID], when set, is returned instead for every page.
	fail map[string]error

	calls map[string]int

	// answered, when set, runs after a page has been produced but before it is
	// handed back. It is how a test cancels *during* a search without having to
	// guess at timing.
	answered func(sourceID string, page int)
}

func (f *fakeSearchTheme) ID() string                  { return "fakesearch" }
func (f *fakeSearchTheme) Fingerprint(*probe.Page) int { return 0 }
func (f *fakeSearchTheme) AllowedHosts() []string      { return nil }
func (f *fakeSearchTheme) SuggestedName() string       { return "Fake" }
func (f *fakeSearchTheme) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return nil, errors.New("not used")
}
func (f *fakeSearchTheme) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return nil, errors.New("not used")
}
func (f *fakeSearchTheme) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return nil, errors.New("not used")
}

func (f *fakeSearchTheme) Search(ctx context.Context, s *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	f.mu.Lock()
	f.calls[s.ID]++
	pages, fail, answered := f.pages[s.ID], f.fail[s.ID], f.answered
	f.mu.Unlock()

	if answered != nil {
		defer answered(s.ID, page)
	}

	// A real theme's fetch stops on a cancelled context; the fake has to as
	// well, or "did the cancellation reach the sources" would be untestable.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fail != nil {
		return nil, fail
	}
	if page < 1 || page > len(pages) {
		return nil, nil
	}
	return pages[page-1], nil
}

func (f *fakeSearchTheme) callCount(sourceID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[sourceID]
}

// capture is the frontend: it keeps what the service sent.
type capture struct {
	mu   sync.Mutex
	sent []struct {
		Type    int32
		Payload []byte
	}
}

func (c *capture) Send(msgType int32, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, struct {
		Type    int32
		Payload []byte
	}{msgType, append([]byte(nil), payload...)})
	return nil
}

func (c *capture) only(t *testing.T, msgType int32) []byte {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var found []byte
	for _, f := range c.sent {
		if f.Type == msgType {
			if found != nil {
				t.Fatalf("%s arrived more than once", appload.Name(msgType))
			}
			found = f.Payload
		}
	}
	if found == nil {
		var got []string
		for _, f := range c.sent {
			got = append(got, appload.Name(f.Type)+" "+string(f.Payload))
		}
		t.Fatalf("no %s arrived; got %v", appload.Name(msgType), got)
	}
	return found
}

func (c *capture) count(msgType int32) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, f := range c.sent {
		if f.Type == msgType {
			n++
		}
	}
	return n
}

// searchAllReply is the wire shape of MessageSearchAllResults, decoded the way
// the frontend would have to: nothing here shares a struct with the sender, so
// a renamed JSON field fails the test.
type searchAllReply struct {
	Query      string `json:"query"`
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
	TotalPages int    `json:"totalPages"`
	HasMore    bool   `json:"hasMore"`
	Groups     []struct {
		Key           string   `json:"key"`
		Title         string   `json:"title"`
		CoverURL      string   `json:"coverUrl"`
		CoverSourceID string   `json:"coverSourceId"`
		CoverSeriesID string   `json:"coverSeriesId"`
		Authors       []string `json:"authors"`
		Matches       []struct {
			SourceID   string `json:"sourceId"`
			SourceName string `json:"sourceName"`
			SeriesID   string `json:"seriesId"`
			CoverURL   string `json:"coverUrl"`
		} `json:"matches"`
	} `json:"groups"`
	SourceErrors []struct {
		SourceID   string `json:"sourceId"`
		SourceName string `json:"sourceName"`
		Message    string `json:"message"`
	} `json:"sourceErrors"`
}

// newSearchAllService wires a Service over `names` sources, all enabled, all on
// the fake theme. The store sorts by name, so the order the names are given in
// is the user's configured source order.
func newSearchAllService(t *testing.T, names ...string) (*Service, *fakeSearchTheme, *capture) {
	t.Helper()
	th := &fakeSearchTheme{
		pages: map[string][][]theme.SeriesStub{},
		fail:  map[string]error{},
		calls: map[string]int{},
	}
	reg := theme.NewRegistry()
	reg.MustRegister(th)

	store, err := state.Open(t.TempDir(), reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if _, err := store.Add(&theme.Source{
			ID:      name,
			Name:    name,
			BaseURL: "https://" + name + ".invalid",
			Theme:   th.ID(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(Options{Store: store, Registry: reg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	t.Cleanup(svc.Close)
	return svc, th, &capture{}
}

// stubs turns titles into a source page. The IDs are derived from the title so
// that the same series on two sources has two different IDs, as it would.
func stubs(sourceID string, titles ...string) []theme.SeriesStub {
	out := make([]theme.SeriesStub, 0, len(titles))
	for i, title := range titles {
		out = append(out, theme.SeriesStub{
			ID:       fmt.Sprintf("%s-%d-%s", sourceID, i, title),
			Title:    title,
			CoverURL: "https://cdn.invalid/" + sourceID + "/" + title + ".jpg",
		})
	}
	return out
}

func decodeReply(t *testing.T, payload []byte) searchAllReply {
	t.Helper()
	var reply searchAllReply
	if err := json.Unmarshal(payload, &reply); err != nil {
		t.Fatalf("the reply was not readable JSON: %v", err)
	}
	return reply
}

// groupTitles is what the user sees, in the order they see it.
func groupTitles(reply searchAllReply) []string {
	out := make([]string, 0, len(reply.Groups))
	for _, g := range reply.Groups {
		out = append(out, g.Title)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- grouping --------------------------------------------------------------

// The rule the whole feature turns on, in both directions. Over-merging hides
// a series the user asked for; under-merging shows one row too many. These are
// the two cases named in PLAN §12.1's revision, and they are the reason the
// comparison is exact equality of the normalised string.
func TestGroupingMergesOnlyExactNormalisedTitles(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		merge bool
	}{
		{"punctuation and case are noise", "Vagabond!", "vagabond", true},
		{"a parenthesised edition is a different series", "Vagabond", "Vagabond (colour)", false},
		{"a season marker is never stripped", "Solo Leveling", "Solo Leveling Season 2", false},
		{"a volume marker is never stripped", "Berserk", "Berserk Vol. 2", false},
		{"spacing and hyphens collapse", "Re:Zero - Starting Life", "re zero starting life", true},
		{"an em dash is a space", "Vinland Saga — Part 2", "Vinland saga part 2", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, th, out := newSearchAllService(t, "alpha", "beta")
			th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", tc.left)}
			th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", tc.right)}

			svc.runSearchAll(context.Background(), out, "q", 1, 10, false)
			reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

			want := 2
			if tc.merge {
				want = 1
			}
			if len(reply.Groups) != want {
				t.Fatalf("%q and %q made %d groups, want %d (%v)",
					tc.left, tc.right, len(reply.Groups), want, groupTitles(reply))
			}
			if tc.merge && len(reply.Groups[0].Matches) != 2 {
				t.Fatalf("the merged group names %d sources, want both", len(reply.Groups[0].Matches))
			}
		})
	}
}

// A merged group must still be able to say where to get the series, and the
// list of sources under it is the user's own source order — not whichever site
// answered first.
func TestAMergedGroupNamesItsSourcesInTheUsersOrder(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta", "gamma")
	// Only beta and gamma have it, and gamma ranks it first: the group is
	// still listed beta-then-gamma, because that is the source list's order.
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "Something Else")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "Filler", "Vagabond")}
	th.pages["gamma"] = [][]theme.SeriesStub{stubs("gamma", "Vagabond")}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

	var found bool
	for _, g := range reply.Groups {
		if g.Title != "Vagabond" {
			continue
		}
		found = true
		var got []string
		for _, m := range g.Matches {
			got = append(got, m.SourceID)
		}
		if !equalStrings(got, []string{"beta", "gamma"}) {
			t.Errorf("matches listed %v, want the source order [beta gamma]", got)
		}
		if g.Matches[0].SeriesID == "" {
			t.Error("a match carries no seriesId, so the group cannot be opened")
		}
	}
	if !found {
		t.Fatalf("no Vagabond group at all; got %v", groupTitles(reply))
	}
}

// The cover is the best-ranked match that has one. A source that lists results
// without thumbnails must not leave the group blank when another has a picture.
func TestAGroupTakesTheBestRankedCoverThatExists(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{{{ID: "a1", Title: "Vagabond"}}}
	th.pages["beta"] = [][]theme.SeriesStub{{{ID: "b1", Title: "Vagabond", CoverURL: "https://cdn.invalid/b1.jpg"}}}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

	if len(reply.Groups) != 1 {
		t.Fatalf("got %d groups, want one merged", len(reply.Groups))
	}
	if reply.Groups[0].CoverURL != "https://cdn.invalid/b1.jpg" {
		t.Errorf("cover = %q, want the only match that has one", reply.Groups[0].CoverURL)
	}
}

// The group's cover is attributed to the match it actually came from, not to
// the group's best (opening) match. This is the regression the SSRF guard's
// refusals traced back to: a cover fetched under the wrong source is a
// cross-domain request for a URL that source never served, which the guard
// correctly refuses. "alpha" is the best-ranked (rank 1, first in source
// order) and has no cover; "beta" ranks worse but is the one whose cover the
// group carries, so a fetch for that cover must be attributed to beta, with
// beta's own series id — never alpha's.
func TestGroupCoverIsAttributedToTheMatchItCameFrom(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{{{ID: "a1", Title: "Vagabond"}}}
	th.pages["beta"] = [][]theme.SeriesStub{{{ID: "b1", Title: "Vagabond", CoverURL: "https://cdn.invalid/b1.jpg"}}}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

	if len(reply.Groups) != 1 {
		t.Fatalf("got %d groups, want one merged", len(reply.Groups))
	}
	g := reply.Groups[0]
	if g.Matches[0].SourceID != "alpha" {
		t.Fatalf("test setup: matches[0] = %q, want alpha to be the best (opening) match", g.Matches[0].SourceID)
	}
	if g.CoverSourceID != "beta" {
		t.Errorf("coverSourceId = %q, want %q (the cover's own source, not the opening match's)", g.CoverSourceID, "beta")
	}
	if g.CoverSeriesID != "b1" {
		t.Errorf("coverSeriesId = %q, want %q (beta's own series id for that cover)", g.CoverSeriesID, "b1")
	}
}

// Authors follow the same rule as the cover: the best-ranked match that
// actually names one, not necessarily the group's own best match. A source
// with no author metadata must not blank out a group another source did name
// one for.
func TestAGroupTakesTheBestRankedAuthorsThatExist(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{{{ID: "a1", Title: "Dune"}}}
	th.pages["beta"] = [][]theme.SeriesStub{{{ID: "b1", Title: "Dune", Authors: []string{"Frank Herbert"}}}}

	svc.runSearchAll(context.Background(), out, "dune", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

	if len(reply.Groups) != 1 {
		t.Fatalf("got %d groups, want one merged", len(reply.Groups))
	}
	if got := reply.Groups[0].Authors; len(got) != 1 || got[0] != "Frank Herbert" {
		t.Errorf("Authors = %v, want [Frank Herbert]", got)
	}
}

// A group with no author metadata from any source must carry no "authors" key
// at all, the same omitempty promise seriesRow makes.
func TestAGroupOmitsAuthorsWhenNoMatchHasAny(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	th.pages["alpha"] = [][]theme.SeriesStub{{{ID: "a1", Title: "Vagabond"}}}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	raw := out.only(t, appload.MessageSearchAllResults)

	var generic struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if len(generic.Groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(generic.Groups))
	}
	if _, present := generic.Groups[0]["authors"]; present {
		t.Errorf("group carries an authors key (%v); want it omitted entirely", generic.Groups[0]["authors"])
	}
}

// PLAN §7.6: the cover URL round-trips through the frontend and comes back with
// no memory of the page it was parsed from, so the referrer is recorded when
// the stub is seen. Without this the covers 403 and the screen is a grid of
// blanks — the bug this check exists to catch is a *missing call*, which
// nothing else here would notice.
func TestEveryStubSeenRecordsItsCoverReferrer(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	th.pages["alpha"] = [][]theme.SeriesStub{{
		{ID: "a1", Title: "Vagabond", CoverURL: "https://cdn.invalid/a1.jpg", CoverReferrer: "https://alpha.invalid/manga/vagabond"},
	}}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	out.only(t, appload.MessageSearchAllResults)

	if got := svc.coverReferrerFor("alpha", "https://cdn.invalid/a1.jpg"); got != "https://alpha.invalid/manga/vagabond" {
		t.Errorf("referrer for the cover = %q, want the page it was parsed from", got)
	}
}

// --- ordering --------------------------------------------------------------

// The order is a promise: the same results in the same store produce the same
// screen. Rank first, then the user's source order, then title.
func TestGroupOrderIsRankThenSourceOrderThenTitle(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta", "gamma")
	// Ranks, 1-based per source:
	//   alpha: 1 Delta, 2 Shared, 3 Zulu
	//   beta:  1 Bravo, 2 Shared
	//   gamma: 1 Alpha, 2 Yankee
	// So: rank 1 — Alpha/Bravo/Delta, settled by source order (alpha, beta,
	// gamma) → Delta, Bravo, Alpha. Rank 2 — Shared (alpha) then Yankee
	// (gamma). Rank 3 — Zulu.
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "Delta", "Shared", "Zulu")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "Bravo", "Shared")}
	th.pages["gamma"] = [][]theme.SeriesStub{stubs("gamma", "Alpha", "Yankee")}

	want := []string{"Delta", "Bravo", "Alpha", "Shared", "Yankee", "Zulu"}

	svc.runSearchAll(context.Background(), out, "q", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if got := groupTitles(reply); !equalStrings(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	// Ten more runs on a fresh service each time: the merge fans out over
	// goroutines, so an order that depended on which one finished first would
	// pass once and then not.
	for i := 0; i < 10; i++ {
		svc2, th2, out2 := newSearchAllService(t, "alpha", "beta", "gamma")
		th2.pages = th.pages
		svc2.runSearchAll(context.Background(), out2, "q", 1, 10, false)
		got := groupTitles(decodeReply(t, out2.only(t, appload.MessageSearchAllResults)))
		if !equalStrings(got, want) {
			t.Fatalf("run %d ordered %v, want %v", i, got, want)
		}
	}
}

// Two results the sites both rank first are settled by the source list and by
// nothing else — in particular not by title, which is only the last resort.
func TestTiedRanksFollowTheConfiguredSourceOrder(t *testing.T) {
	tests := []struct {
		name       string
		firstTitle string // on "aaa", the source the user put first
		lastTitle  string // on "zzz"
		want       []string
	}{
		// The first source's result leads even when its title sorts last,
		// which is what distinguishes source order from alphabetical order.
		{"the leading source wins against the alphabet", "Zulu", "Anchor", []string{"Zulu", "Anchor"}},
		{"and still wins when the alphabet agrees", "Anchor", "Zulu", []string{"Anchor", "Zulu"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, th, out := newSearchAllService(t, "aaa", "zzz")
			th.pages["aaa"] = [][]theme.SeriesStub{stubs("aaa", tc.firstTitle)}
			th.pages["zzz"] = [][]theme.SeriesStub{stubs("zzz", tc.lastTitle)}

			svc.runSearchAll(context.Background(), out, "q", 1, 10, false)
			reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
			if got := groupTitles(reply); !equalStrings(got, tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- partial failure -------------------------------------------------------

// Four sites answered. Emptying the screen because the fifth timed out is the
// behaviour MessageSearchAllResults' sourceErrors field exists to prevent.
func TestOneSourceFailingIsAPartialAnswer(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "Vagabond")}
	th.fail["beta"] = errors.New("search beta: HTTP 503")

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)

	if n := out.count(appload.MessageError); n != 0 {
		t.Fatalf("one failing source produced %d error messages, want none", n)
	}
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if got := groupTitles(reply); !equalStrings(got, []string{"Vagabond"}) {
		t.Fatalf("the working source's rows were lost: %v", got)
	}
	if len(reply.SourceErrors) != 1 {
		t.Fatalf("sourceErrors = %v, want the one source that failed", reply.SourceErrors)
	}
	e := reply.SourceErrors[0]
	if e.SourceID != "beta" || e.SourceName != "beta" {
		t.Errorf("the error names %q/%q, want beta — the user cannot act on an unnamed site", e.SourceID, e.SourceName)
	}
	if e.Message == "" {
		t.Error("the error carries no message")
	}
}

// A source that refused is not asked again on the next page turn: it would
// cost a stall per tap to re-learn the same answer. The error stays on the
// reply, though, because it is still true.
func TestAFailedSourceIsNotRetriedOnEveryPageTurn(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "One", "Two", "Three", "Four")}
	th.fail["beta"] = errors.New("search beta: HTTP 503")

	svc.runSearchAll(context.Background(), out, "q", 1, 2, false)
	afterFirst := th.callCount("beta")
	if afterFirst == 0 {
		t.Fatal("the failing source was never asked at all")
	}

	svc.runSearchAll(context.Background(), &capture{}, "q", 2, 2, false)
	if got := th.callCount("beta"); got != afterFirst {
		t.Errorf("the failing source was asked again on a page turn: %d calls, want %d", got, afterFirst)
	}

	second := &capture{}
	svc.runSearchAll(context.Background(), second, "q", 2, 2, false)
	reply := decodeReply(t, second.only(t, appload.MessageSearchAllResults))
	if len(reply.SourceErrors) != 1 {
		t.Errorf("page 2 dropped the failure: sourceErrors = %v", reply.SourceErrors)
	}
}

// Only when nobody answered and there is nothing to show is this a failed
// search rather than a thin one.
func TestEverySourceFailingIsAnError(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.fail["alpha"] = errors.New("search alpha: HTTP 500")
	th.fail["beta"] = errors.New("search beta: HTTP 503")

	svc.runSearchAll(context.Background(), out, "q", 1, 10, false)

	if n := out.count(appload.MessageSearchAllResults); n != 0 {
		t.Fatalf("an empty screen was reported as a result %d times, want an error", n)
	}
	payload := out.only(t, appload.MessageError)
	var e struct{ Code, Message string }
	if err := json.Unmarshal(payload, &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "search_failed" {
		t.Errorf("code = %q, want search_failed", e.Code)
	}
	if e.Message == "" {
		t.Error("the error says nothing about why")
	}
}

// "Nothing found" and "nothing answered" are different sentences. A source
// that failed alongside sources that answered with nothing is still a search
// that ran: the user is told there were no results *and* which site is missing,
// rather than being shown a failure they cannot act on.
func TestAFailureBesideAnEmptyAnswerIsStillAResult(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = nil // answered, had nothing
	th.fail["beta"] = errors.New("search beta: HTTP 503")

	svc.runSearchAll(context.Background(), out, "q", 1, 10, false)

	if n := out.count(appload.MessageError); n != 0 {
		t.Fatalf("a search where one source answered emptily raised %d errors", n)
	}
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if len(reply.Groups) != 0 {
		t.Errorf("groups = %v, want none", groupTitles(reply))
	}
	if len(reply.SourceErrors) != 1 {
		t.Errorf("sourceErrors = %v, want the one source that failed", reply.SourceErrors)
	}
}

// One source, and it failed. The all-failed rule must not be "more than one
// source failed", nor "some source failed".
func TestTheOnlySourceFailingIsAnError(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	th.fail["alpha"] = errors.New("search alpha: HTTP 500")

	svc.runSearchAll(context.Background(), out, "q", 1, 10, false)

	if n := out.count(appload.MessageSearchAllResults); n != 0 {
		t.Fatalf("got %d result messages, want an error", n)
	}
	out.only(t, appload.MessageError)
}

// A disabled source is not searched. It is the only thing the switch in the
// source list does for search, so it has to be true here too.
func TestDisabledSourcesAreNotSearched(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "Vagabond")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "Vagabond")}
	if err := svc.store.SetEnabled("beta", false); err != nil {
		t.Fatal(err)
	}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

	if th.callCount("beta") != 0 {
		t.Errorf("the disabled source was searched %d times", th.callCount("beta"))
	}
	if len(reply.Groups) != 1 || len(reply.Groups[0].Matches) != 1 {
		t.Fatalf("the disabled source's row reached the screen: %+v", reply.Groups)
	}
}

// TestPrivateSourcesAreNotInTheCombinedSearch is the hard requirement's own
// test: the normal combined search must never touch a private source, and
// "never touch" is proven at the theme's Search method — callCount — not at
// the reply, so a bug that fetched but then filtered the row would still fail
// this test. This is the mutation in the task's own words: "let the combined
// search include a private source — a test must fail."
func TestPrivateSourcesAreNotInTheCombinedSearch(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "Vagabond")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "Vagabond")}
	if err := svc.store.SetPrivate("beta", true); err != nil {
		t.Fatal(err)
	}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

	if th.callCount("beta") != 0 {
		t.Fatalf("the private source received %d requests from the normal combined search, want 0",
			th.callCount("beta"))
	}
	if len(reply.Groups) != 1 || len(reply.Groups[0].Matches) != 1 {
		t.Fatalf("the private source's row reached the normal combined search: %+v", reply.Groups)
	}
}

// TestPrivateSearchTouchesOnlyPrivateSources is the mirror: the private
// list's own combined search (MessageSearchAllPrivate / runSearchAll with
// private=true) must ask only the sources marked private, and must never ask
// an ordinary one — proven the same way, by call count rather than by what
// made it into the reply.
func TestPrivateSearchTouchesOnlyPrivateSources(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "Vagabond")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "Vagabond")}
	if err := svc.store.SetPrivate("beta", true); err != nil {
		t.Fatal(err)
	}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, true)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))

	if th.callCount("alpha") != 0 {
		t.Fatalf("the normal source received %d requests from the private combined search, want 0",
			th.callCount("alpha"))
	}
	if th.callCount("beta") == 0 {
		t.Fatal("the private source was never asked by its own combined search")
	}
	if len(reply.Groups) != 1 || len(reply.Groups[0].Matches) != 1 || reply.Groups[0].Matches[0].SourceID != "beta" {
		t.Fatalf("the private search's reply = %+v, want beta's row only", reply.Groups)
	}
}

// TestSwitchingScopeDoesNotReuseTheOtherScopesPager guards the cache key
// (searchAllKey): the normal and private combined searches must not share a
// cached pager for the same query text, or one screen would serve up the
// other's source list.
func TestSwitchingScopeDoesNotReuseTheOtherScopesPager(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "Vagabond")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "Vagabond")}
	if err := svc.store.SetPrivate("beta", true); err != nil {
		t.Fatal(err)
	}

	svc.runSearchAll(context.Background(), out, "vagabond", 1, 10, false)
	out.only(t, appload.MessageSearchAllResults)

	second := &capture{}
	svc.runSearchAll(context.Background(), second, "vagabond", 1, 10, true)
	reply := decodeReply(t, second.only(t, appload.MessageSearchAllResults))
	if len(reply.Groups) != 1 || reply.Groups[0].Matches[0].SourceID != "beta" {
		t.Fatalf("the private search after a normal one = %+v, want beta's row only", reply.Groups)
	}
}

// --- paging ----------------------------------------------------------------

// PLAN §12.1: one tap is not one HTTP request, and one display page is not one
// source page. A page of 4 out of sources that hand back 2 at a time has to
// cost several rounds — and the *next* tap none at all.
func TestADisplayPageIsFilledFromAsManyRoundsAsItTakes(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{
		stubs("alpha", "A1", "A2"),
		stubs("alpha", "A3", "A4"),
		stubs("alpha", "A5", "A6"),
	}
	th.pages["beta"] = [][]theme.SeriesStub{
		stubs("beta", "B1", "B2"),
		stubs("beta", "B3", "B4"),
	}

	svc.runSearchAll(context.Background(), out, "q", 1, 4, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if len(reply.Groups) != 4 {
		t.Fatalf("page 1 served %d groups, want a full page of 4 (%v)", len(reply.Groups), groupTitles(reply))
	}
	if !reply.HasMore {
		t.Error("hasMore is false while both sources still have pages")
	}
	if th.callCount("alpha") < 2 {
		t.Errorf("alpha was asked %d times; a page of 4 out of pages of 2 needs more than one round", th.callCount("alpha"))
	}

	// The second display page comes out of what was already fetched for the
	// "one past the page" probe, and must not re-ask every site from scratch.
	before := th.callCount("alpha") + th.callCount("beta")
	second := &capture{}
	svc.runSearchAll(context.Background(), second, "q", 2, 4, false)
	page2 := decodeReply(t, second.only(t, appload.MessageSearchAllResults))
	if page2.Page != 2 {
		t.Errorf("page = %d, want 2", page2.Page)
	}
	after := th.callCount("alpha") + th.callCount("beta")
	if after-before > 2 {
		t.Errorf("turning one page cost %d requests, want at most one round", after-before)
	}
	// Page 1 and page 2 are different rows, not the same slice twice.
	if equalStrings(groupTitles(reply), groupTitles(page2)) {
		t.Errorf("page 2 served the same groups as page 1: %v", groupTitles(page2))
	}
}

// A total is never guessed. It appears only once every source has run dry, and
// a request past the end is clamped rather than answered with emptiness.
func TestTotalPagesAppearsOnlyWhenEverySourceIsExhausted(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	th.pages["alpha"] = [][]theme.SeriesStub{
		stubs("alpha", "A1", "A2"),
		stubs("alpha", "A3", "A4"),
		stubs("alpha", "A5"),
	}

	svc.runSearchAll(context.Background(), out, "q", 1, 2, false)
	first := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if first.TotalPages != 0 {
		t.Errorf("totalPages = %d before the source ran dry; 0 means 'not known yet'", first.TotalPages)
	}
	if !first.HasMore {
		t.Error("hasMore is false with four more results behind it")
	}

	// Far past the end: everything is fetched, the total becomes honest, and
	// the page asked for is clamped onto the last one that exists.
	last := &capture{}
	svc.runSearchAll(context.Background(), last, "q", 9, 2, false)
	reply := decodeReply(t, last.only(t, appload.MessageSearchAllResults))
	if reply.TotalPages != 3 {
		t.Errorf("totalPages = %d, want 3 (five results, two per page)", reply.TotalPages)
	}
	if reply.Page != 3 {
		t.Errorf("page = %d, want the request clamped to 3", reply.Page)
	}
	if reply.HasMore {
		t.Error("hasMore is true on the last page")
	}
	if len(reply.Groups) != 1 {
		t.Errorf("the last page served %d groups, want the single leftover", len(reply.Groups))
	}
}

// Merging changes how many display pages there are: ten rows that are five
// series are five rows, and the total has to count groups, not matches.
func TestPagingCountsGroupsNotMatches(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "One", "Two", "Three", "Four")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "One", "Two", "Three", "Four")}

	svc.runSearchAll(context.Background(), out, "q", 1, 4, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if reply.TotalPages != 1 {
		t.Errorf("totalPages = %d, want 1: eight rows are four series", reply.TotalPages)
	}
	if reply.HasMore {
		t.Error("hasMore is true when everything fits on one page")
	}
	for _, g := range reply.Groups {
		if len(g.Matches) != 2 {
			t.Errorf("%q names %d sources, want both", g.Title, len(g.Matches))
		}
	}
}

// A search with no results at all is an answer, not a failure: one page of
// nothing, no next page, and no error banner.
func TestNoResultsAnywhereIsAnEmptyPageNotAnError(t *testing.T) {
	svc, _, out := newSearchAllService(t, "alpha", "beta")

	svc.runSearchAll(context.Background(), out, "nothing matches this", 1, 10, false)
	if n := out.count(appload.MessageError); n != 0 {
		t.Fatalf("an empty result raised %d errors", n)
	}
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if len(reply.Groups) != 0 {
		t.Errorf("groups = %v, want none", groupTitles(reply))
	}
	if reply.TotalPages != 1 || reply.HasMore {
		t.Errorf("totalPages/hasMore = %d/%v, want 1/false", reply.TotalPages, reply.HasMore)
	}
	if reply.Query != "nothing matches this" || reply.PageSize != 10 {
		t.Errorf("the reply did not echo the request: %+v", reply)
	}
}

// A source serving the same page for every page number is a real site
// behaviour and, without the "nothing new came back" stop, an endless loop
// with a growing page counter. paging.go has the same guard for one source.
func TestASourceThatRepeatsItsPageDoesNotLoopForever(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	page := stubs("alpha", "One", "Two")
	th.pages["alpha"] = [][]theme.SeriesStub{page, page, page, page, page, page, page, page}

	svc.runSearchAll(context.Background(), out, "q", 3, 4, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if reply.TotalPages != 1 {
		t.Errorf("totalPages = %d, want 1: the source only ever had two results", reply.TotalPages)
	}
	if th.callCount("alpha") > 3 {
		t.Errorf("the repeating source was asked %d times, want it given up on quickly", th.callCount("alpha"))
	}
}

// --- cancellation ----------------------------------------------------------

// The frontend cancels when the user has moved on. Work must stop, and nothing
// must be sent: a reply would land on a screen showing something else, and the
// error path would turn a deliberate cancel into a banner.
func TestACancelledContextStopsTheWorkAndSaysNothing(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha", "beta")
	th.pages["alpha"] = [][]theme.SeriesStub{stubs("alpha", "One", "Two")}
	th.pages["beta"] = [][]theme.SeriesStub{stubs("beta", "Three", "Four")}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.runSearchAll(ctx, out, "q", 1, 10, false)

	if n := th.callCount("alpha") + th.callCount("beta"); n != 0 {
		t.Errorf("a cancelled search still asked the sources %d times", n)
	}
	if n := out.count(appload.MessageSearchAllResults) + out.count(appload.MessageError); n != 0 {
		t.Errorf("a cancelled search sent %d messages, want silence", n)
	}
}

// A cancelled request is one the user has already moved on from, so it must
// not cost the search they are still looking at: exactly one query is cached,
// and a cancelled search for a different one must not evict it.
func TestACancelledSearchDoesNotThrowAwayTheCachedQuery(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	th.pages["alpha"] = [][]theme.SeriesStub{
		stubs("alpha", "A1", "A2"),
		stubs("alpha", "A3", "A4"),
	}

	svc.runSearchAll(context.Background(), out, "q", 1, 2, false)
	before := th.callCount("alpha")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.runSearchAll(ctx, &capture{}, "something else", 1, 2, false)

	second := &capture{}
	svc.runSearchAll(context.Background(), second, "q", 2, 2, false)
	second.only(t, appload.MessageSearchAllResults)
	if got := th.callCount("alpha"); got > before+1 {
		t.Errorf("the page turn cost %d further requests: the cancelled search dropped the cache", got-before)
	}
}

// Cancelled part-way through: the first round is in flight and its answers are
// kept, but no further round is started and nothing is sent.
func TestCancellingMidSearchStopsAfterTheRoundInFlight(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	th.pages["alpha"] = [][]theme.SeriesStub{
		stubs("alpha", "A1"),
		stubs("alpha", "A2"),
		stubs("alpha", "A3"),
		stubs("alpha", "A4"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	// The user turns away while the first round is out: the round completes
	// and its answer is kept, but the next one is never started.
	th.answered = func(string, int) { cancel() }

	svc.runSearchAll(ctx, out, "q", 1, 4, false)

	if got := th.callCount("alpha"); got != 1 {
		t.Errorf("the source was asked %d times; the cancel should have stopped it after the round in flight", got)
	}
	if n := out.count(appload.MessageSearchAllResults) + out.count(appload.MessageError); n != 0 {
		t.Errorf("a cancelled search sent %d messages, want silence", n)
	}
}

// --- normalisation ---------------------------------------------------------

func TestNormaliseTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Vagabond!", "vagabond"},
		{"  Vagabond  ", "vagabond"},
		{"Re:Zero — Starting Life", "re zero starting life"},
		{"Solo Leveling Season 2", "solo leveling season 2"},
		{"Vagabond (colour)", "vagabond colour"},
		{"ONE PIECE", "one piece"},
		{"!!!", ""},
		{"", ""},
		{"ベルセルク", "ベルセルク"},
		{"a\t\nb", "a b"},
	}
	for _, tc := range tests {
		if got := normaliseTitle(tc.in); got != tc.want {
			t.Errorf("normaliseTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Rows with nothing but punctuation for a title must not all collapse into one
// group: that would be merging on the strength of having no evidence.
func TestUntitledRowsDoNotAllMergeTogether(t *testing.T) {
	svc, th, out := newSearchAllService(t, "alpha")
	th.pages["alpha"] = [][]theme.SeriesStub{{
		{ID: "a1", Title: "???"},
		{ID: "a2", Title: "!!!"},
	}}

	svc.runSearchAll(context.Background(), out, "q", 1, 10, false)
	reply := decodeReply(t, out.only(t, appload.MessageSearchAllResults))
	if len(reply.Groups) != 2 {
		t.Fatalf("two untitled rows made %d groups, want 2", len(reply.Groups))
	}
	if reply.Groups[0].Key == reply.Groups[1].Key {
		t.Errorf("both untitled rows share the key %q", reply.Groups[0].Key)
	}
}
