package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// seriesPath is the one series these tests watch.
const seriesPath = "/manga/the-lantern-keeper/"

// chaptersFragment builds the bare <ul> a current-shape Madara site returns
// from POST {series}ajax/chapters/.
//
// The entries are deliberately awkward. A real site's chapter list is not a run
// of "Chapter 1".."Chapter 5": it carries half-chapters, an extra with no number
// at all, a title with an em dash and an accent in it, an entry whose href is
// absolute while its neighbours are relative, and one row that is a placeholder
// with no link. Uniform fixtures are how this project's last three field bugs
// passed CI.
func chaptersFragment(entries ...string) string {
	var b strings.Builder
	b.WriteString(`<ul class="main version-chap no-volumn">`)
	for _, e := range entries {
		b.WriteString(e)
	}
	// Always present, always ignorable: an announced chapter with no href.
	b.WriteString(`<li class="wp-manga-chapter"><span>Chapter ??? (coming soon)</span>` +
		`<span class="chapter-release-date"><i></i></span></li>`)
	b.WriteString(`</ul>`)
	return b.String()
}

func chapterEntry(href, title, date string) string {
	return fmt.Sprintf(
		`<li class="wp-manga-chapter"><a href="%s">%s</a>`+
			`<span class="chapter-release-date"><i>%s</i></span></li>`,
		href, title, date)
}

// The list as the user last saw it: five real chapters, one of them a half,
// one an extra with no number, one linked absolutely.
func chaptersSeen() string {
	return chaptersFragment(
		chapterEntry("https://example.invalid"+seriesPath+"chapter-4/", "Chapter 4 – The Forty-Second Lamp", "March 2, 2026"),
		chapterEntry(seriesPath+"omake-lamplighters/", "Omake — Lamplighters’ Night", "March 1, 2026"),
		chapterEntry(seriesPath+"chapter-3-5/", "Chapter 3.5 - Interlude", "February 24, 2026"),
		chapterEntry(seriesPath+"chapter-3/", "Chapter 3", "February 17, 2026"),
		chapterEntry(seriesPath+"chapter-2/", "Chapter 2", "February 10, 2026"),
	)
}

// Three chapters later.
func chaptersGrown() string {
	return chaptersFragment(
		chapterEntry(seriesPath+"chapter-7/", "Chapter 7", "March 23, 2026"),
		chapterEntry(seriesPath+"chapter-6/", "Chapter 6", "March 16, 2026"),
		chapterEntry(seriesPath+"chapter-5/", "Chapter 5", "March 9, 2026"),
		chapterEntry("https://example.invalid"+seriesPath+"chapter-4/", "Chapter 4 – The Forty-Second Lamp", "March 2, 2026"),
		chapterEntry(seriesPath+"omake-lamplighters/", "Omake — Lamplighters’ Night", "March 1, 2026"),
		chapterEntry(seriesPath+"chapter-3-5/", "Chapter 3.5 - Interlude", "February 24, 2026"),
		chapterEntry(seriesPath+"chapter-3/", "Chapter 3", "February 17, 2026"),
		chapterEntry(seriesPath+"chapter-2/", "Chapter 2", "February 10, 2026"),
	)
}

// The awkward one: the site has *rewritten its history*. Two chapters are gone
// (a takedown), the half-chapter has been renumbered into a whole one at a new
// URL, and two genuinely new ones have appeared — leaving a list the same
// length as the one before it. A "the count went up" check reports nothing.
func chaptersRewritten() string {
	return chaptersFragment(
		chapterEntry(seriesPath+"chapter-6/", "Chapter 6", "March 16, 2026"),
		chapterEntry(seriesPath+"chapter-5/", "Chapter 5", "March 9, 2026"),
		chapterEntry("https://example.invalid"+seriesPath+"chapter-4/", "Chapter 4 – The Forty-Second Lamp", "March 2, 2026"),
		chapterEntry(seriesPath+"chapter-4-again/", "Chapter 4 (v2)", "March 2, 2026"),
		chapterEntry(seriesPath+"chapter-3/", "Chapter 3", "February 17, 2026"),
	)
}

// phasedFetcher serves one themetest fetcher at a time, so a test can say "and
// now the site has changed" without racing the goroutine that is reading it.
type phasedFetcher struct {
	mu    sync.Mutex
	inner theme.Fetcher
	calls int
}

func (p *phasedFetcher) use(f theme.Fetcher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inner = f
}

// countedCalls is how many requests any phase has seen. It is the honest test
// of "no check ran": a cooldown that suppresses a check but still fetches has
// not suppressed anything.
func (p *phasedFetcher) countedCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *phasedFetcher) Get(ctx context.Context, pol *fetch.Policy, u string) (*fetch.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.inner.Get(ctx, pol, u)
}

func (p *phasedFetcher) GetFrom(ctx context.Context, pol *fetch.Policy, u string, from fetch.Referrer) (*fetch.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.inner.GetFrom(ctx, pol, u, from)
}

func (p *phasedFetcher) GetRetrieval(ctx context.Context, pol *fetch.Policy, u string) (*fetch.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.inner.GetRetrieval(ctx, pol, u)
}

func (p *phasedFetcher) GetRetrievalFrom(ctx context.Context, pol *fetch.Policy, u string, from fetch.Referrer) (*fetch.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.inner.GetRetrievalFrom(ctx, pol, u, from)
}

func (p *phasedFetcher) PostForm(ctx context.Context, pol *fetch.Policy, u string, form url.Values) (*fetch.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.inner.PostForm(ctx, pol, u, form)
}

// watchRoutes is the site as it stands, with the chapter fragment supplied.
func watchRoutes(chapters string) map[string]themetest.Route {
	r := routes()
	r["POST "+seriesPath+"ajax/chapters/"] = themetest.Route{Body: chapters}
	return r
}

// clock is a test clock the service reads through Options.Now.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type watchHarness struct {
	svc   *service.Service
	store *state.Store
	rec   *recorder
	fetch *phasedFetcher
	clk   *clock
	t     *testing.T
}

func newWatchHarness(t *testing.T, chapters string) *watchHarness {
	t.Helper()
	pf := &phasedFetcher{inner: themetest.New(t, watchRoutes(chapters))}
	clk := &clock{at: fixedNow}

	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(pf, clk.now))

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store:      store,
		Registry:   reg,
		Fetcher:    pf,
		Covers:     covers.New(dir+"/covers", pf),
		Now:        clk.now,
		ProbeGuard: allowGuard{},
	})
	t.Cleanup(svc.Close)
	addExampleSource(t, store)
	return &watchHarness{svc: svc, store: store, rec: &recorder{}, fetch: pf, clk: clk, t: t}
}

// phase replaces the site with a different one.
func (h *watchHarness) phase(chapters string) {
	h.fetch.use(themetest.New(h.t, watchRoutes(chapters)))
}

// phaseRoutes replaces the site with arbitrary routes — a dead host, a
// challenge, an empty parse.
func (h *watchHarness) phaseRoutes(r map[string]themetest.Route) {
	h.fetch.use(themetest.New(h.t, r))
}

// openSeries is the user tapping the series: it serves the chapter list, which
// is what seeds a watch and what clears "new".
func (h *watchHarness) openSeries() {
	h.t.Helper()
	handle(h.t, h.svc, h.rec, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesPath+`"}`)
	h.rec.wait(h.t, appload.MessageSeriesDetailResult)
}

func (h *watchHarness) watch() {
	h.t.Helper()
	handle(h.t, h.svc, h.rec, appload.MessageWatchSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesPath+`","title":"The Lantern Keeper"}`)
	h.rec.wait(h.t, appload.MessageWatchList)
}

func (h *watchHarness) checkNow() {
	h.t.Helper()
	handle(h.t, h.svc, h.rec, appload.MessageCheckWatched, `{}`)
}

// watchRow is the UI's view of one watched series, decoded back.
type watchRow struct {
	SourceID    string `json:"sourceId"`
	SeriesID    string `json:"seriesId"`
	SourceName  string `json:"sourceName"`
	Title       string `json:"title"`
	CoverURL    string `json:"coverUrl"`
	NewChapters int    `json:"newChapters"`
	Badge       string `json:"badge"`
	State       string `json:"state"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	CheckedAt   string `json:"checkedAt"`
	Private     bool   `json:"private"`
}

// settled waits for a watch update that is not the "checking" placeholder.
func (h *watchHarness) settled() watchRow {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.rec.mu.Lock()
		var last *watchRow
		for _, f := range h.rec.sent {
			if f.Type != appload.MessageWatchUpdate {
				continue
			}
			var env struct {
				Watch watchRow `json:"watch"`
			}
			if err := json.Unmarshal(f.Payload, &env); err != nil {
				h.rec.mu.Unlock()
				h.t.Fatal(err)
			}
			if env.Watch.State == "checking" {
				continue
			}
			row := env.Watch
			last = &row
		}
		h.rec.mu.Unlock()
		if last != nil {
			return *last
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("no settled WatchUpdate arrived")
	return watchRow{}
}

// forget drops what has been recorded, so the next assertion is about the next
// check rather than the last one.
func (h *watchHarness) forget() {
	h.rec.mu.Lock()
	defer h.rec.mu.Unlock()
	h.rec.sent = nil
}

// watchSummaryRow is the at-a-glance half of a WatchList, decoded back.
type watchSummaryRow struct {
	SeriesWithNew int    `json:"seriesWithNew"`
	NewChapters   int    `json:"newChapters"`
	Failed        int    `json:"failed"`
	Short         string `json:"short"`
	Phrase        string `json:"phrase"`
}

type watchListMsg struct {
	Watched        []watchRow      `json:"watched"`
	Summary        watchSummaryRow `json:"summary"`
	PrivateWatched []watchRow      `json:"privateWatched"`
	PrivateSummary watchSummaryRow `json:"privateSummary"`
}

func (h *watchHarness) listMsg() watchListMsg {
	h.t.Helper()
	var env watchListMsg
	if err := json.Unmarshal(h.rec.wait(h.t, appload.MessageWatchList), &env); err != nil {
		h.t.Fatal(err)
	}
	return env
}

func (h *watchHarness) list() []watchRow {
	h.t.Helper()
	return h.listMsg().Watched
}

// The plain case, and the one the user asked for: three chapters appeared, and
// the badge says so in words.
func TestNewChaptersAreCountedAndSaidInPlainLanguage(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	h.phase(chaptersGrown())
	h.checkNow()

	got := h.settled()
	if got.NewChapters != 3 {
		t.Fatalf("newChapters = %d, want 3 (%+v)", got.NewChapters, got)
	}
	if got.Badge != "3 new chapters" {
		t.Errorf("badge = %q, want %q", got.Badge, "3 new chapters")
	}
	if got.State != "new" {
		t.Errorf("state = %q, want new", got.State)
	}
	if got.SourceName != "Example Reader" || got.Title != "The Lantern Keeper" {
		t.Errorf("row does not carry what a badge needs to be drawn: %+v", got)
	}
	if got.CheckedAt == "" {
		t.Error("checkedAt is empty after a check")
	}
}

// One is one, not "1 new chapters".
func TestOneNewChapterIsSingular(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	h.phase(chaptersFragment(
		chapterEntry(seriesPath+"chapter-5/", "Chapter 5", "March 9, 2026"),
		chapterEntry("https://example.invalid"+seriesPath+"chapter-4/", "Chapter 4 – The Forty-Second Lamp", "March 2, 2026"),
		chapterEntry(seriesPath+"omake-lamplighters/", "Omake — Lamplighters’ Night", "March 1, 2026"),
		chapterEntry(seriesPath+"chapter-3-5/", "Chapter 3.5 - Interlude", "February 24, 2026"),
		chapterEntry(seriesPath+"chapter-3/", "Chapter 3", "February 17, 2026"),
		chapterEntry(seriesPath+"chapter-2/", "Chapter 2", "February 10, 2026"),
	))
	h.checkNow()

	got := h.settled()
	if got.NewChapters != 1 || got.Badge != "1 new chapter" {
		t.Fatalf("badge = %q (%d new), want %q", got.Badge, got.NewChapters, "1 new chapter")
	}
}

// Nothing changed: no badge, and nothing that looks like one.
func TestNothingNewMeansNoBadge(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	h.phase(chaptersSeen())
	h.checkNow()

	got := h.settled()
	if got.NewChapters != 0 {
		t.Fatalf("newChapters = %d, want 0", got.NewChapters)
	}
	if got.Badge != "" {
		t.Errorf("badge = %q, want none", got.Badge)
	}
	if got.State != "ok" {
		t.Errorf("state = %q, want ok", got.State)
	}
}

// The case a naive "the count went up" check gets wrong in both directions.
func TestRemovedAndRenumberedChaptersAreCountedByIdentity(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	// Same number of chapters as before; two of them are new.
	h.phase(chaptersRewritten())
	h.checkNow()

	got := h.settled()
	if got.NewChapters != 3 {
		t.Fatalf("newChapters = %d, want 3 — chapter-5, chapter-6 and the re-uploaded chapter-4 (%+v)", got.NewChapters, got)
	}

	// Looking at it settles the new baseline, including the removals: a
	// subsequent check must not re-announce anything.
	h.forget()
	h.openSeries()
	h.forget()
	h.phase(chaptersRewritten())
	h.checkNow()
	if got := h.settled(); got.NewChapters != 0 {
		t.Fatalf("after looking, newChapters = %d, want 0", got.NewChapters)
	}
}

// PLAN §12.2: failure is not "new", and it is not a reassuring zero either.
func TestAFailedCheckSaysSoAndKeepsWhatItKnew(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	// A real check first, so there is something true to preserve.
	h.phase(chaptersGrown())
	h.checkNow()
	if got := h.settled(); got.NewChapters != 3 {
		t.Fatalf("setup: newChapters = %d, want 3", got.NewChapters)
	}

	tests := []struct {
		name  string
		route themetest.Route

		// reprobes says this shape parses cleanly into *no chapters*, which is
		// PLAN §6 M7's re-probe trigger rather than a transport failure. The
		// test then waits for the re-probe's own verdict, so the assertion is
		// about a settled state rather than a half-finished one.
		reprobes bool
	}{
		{name: "the host is unreachable", route: themetest.Route{Err: errors.New("dial tcp: no route to host")}},
		{name: "the site answers with a challenge", route: themetest.Route{
			Body:   challengePage,
			Status: http.StatusServiceUnavailable,
			Header: http.Header{"Server": []string{"cloudflare"}},
		}},
		{name: "the theme no longer matches the page", route: themetest.Route{
			Body: `<!doctype html><html><body><main>We have moved to a new app.</main></body></html>`,
		}, reprobes: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h.forget()
			r := routes()
			r["POST "+seriesPath+"ajax/chapters/"] = tc.route
			r[strings.TrimSuffix("GET "+seriesPath, "")] = tc.route
			// The re-probe, if one is triggered, finds the same wall.
			r["GET /"] = tc.route
			h.phaseRoutes(r)

			h.clk.advance(service.WatchCooldown + time.Minute)
			h.checkNow()

			got := h.settled()
			if got.State != "failed" {
				t.Fatalf("state = %q, want failed (%+v)", got.State, got)
			}
			// The count from the *earlier successful* check stands: those
			// chapters really are unread. What must not happen is a count
			// invented by the failure, or a reassuring zero.
			if got.Badge != "3 new chapters" {
				t.Errorf("badge = %q, want the 3 the last good check found", got.Badge)
			}
			if got.Detail == "" {
				t.Error("a failed check said nothing about what went wrong")
			}
			if strings.Contains(strings.ToLower(got.Status), "up to date") {
				t.Errorf("status %q claims success", got.Status)
			}
			if got.NewChapters != 3 {
				t.Errorf("newChapters = %d after a failure, want the 3 it legitimately had", got.NewChapters)
			}
			if tc.reprobes {
				// The re-probe writes its verdict to the store, so the test
				// must not finish underneath it.
				h.rec.wait(t, appload.MessageError)
			}
		})
	}
}

// A watched series that starts returning nothing is PLAN §6 M7's re-probe
// trigger, not a second mechanism — and it is reported as a failed check rather
// than as "nothing new".
func TestAWatchedSeriesThatGoesEmptyRePROBESTheSource(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	// The chapter list parses cleanly and is empty — the shape a site takes
	// when its markup has moved on. The series page behind it is empty too, so
	// the fallback finds nothing either.
	r := routes()
	r["POST "+seriesPath+"ajax/chapters/"] = themetest.Route{Body: chaptersFragment()}
	r["GET "+seriesPath] = themetest.Route{Body: emptyListing}
	r["GET /"] = themetest.Route{
		Body:   challengePage,
		Status: http.StatusServiceUnavailable,
		Header: http.Header{"Server": []string{"cloudflare"}, "Cf-Ray": []string{"8b0c000000000000-AMS"}},
	}
	h.phaseRoutes(r)

	h.checkNow()

	got := h.settled()
	if got.State != "failed" {
		t.Errorf("state = %q, want failed — an empty chapter list is not 'nothing new'", got.State)
	}
	if got.NewChapters != 0 || got.Badge != "" {
		t.Errorf("an empty result produced a badge: %+v", got)
	}

	// And the existing re-probe explains it, in the existing words.
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(h.rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "source_changed" {
		t.Errorf("code = %q, want source_changed", e.Code)
	}
}

// PLAN §12.2: check on app open, and not again if the app is opened twice in a
// row. "Check now" is the override.
func TestTheCooldownSuppressesASecondCheckAndCheckNowOverridesIt(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()

	// App open #1 — attach. The check runs.
	h.forget()
	before := h.fetch.countedCalls()
	if err := h.svc.FrontendAttached(h.rec); err != nil {
		t.Fatal(err)
	}
	if got := h.settled(); got.State != "ok" {
		t.Fatalf("first check: %+v", got)
	}
	afterFirst := h.fetch.countedCalls()
	if afterFirst == before {
		t.Fatal("the check on attach made no request at all")
	}

	// App open #2, a few minutes later. Nothing is fetched.
	h.forget()
	h.clk.advance(9 * time.Minute)
	if err := h.svc.FrontendAttached(h.rec); err != nil {
		t.Fatal(err)
	}
	// The list is still pushed — the badge has to be drawable immediately —
	// but nothing goes over the wire.
	h.list()
	time.Sleep(50 * time.Millisecond)
	if got := h.fetch.countedCalls(); got != afterFirst {
		t.Errorf("reopening the app re-polled: %d calls, want %d", got, afterFirst)
	}

	// The user asks. That is not the same as the app asking.
	h.forget()
	h.phase(chaptersGrown())
	h.checkNow()
	if got := h.settled(); got.NewChapters != 3 {
		t.Fatalf("check now was suppressed by the cooldown: %+v", got)
	}

	// And once the cooldown has passed, attach checks again by itself.
	h.forget()
	h.openSeries() // clears the badge, so the next result is distinguishable
	h.forget()
	h.clk.advance(service.WatchCooldown + time.Minute)
	h.phase(chaptersGrown())
	if err := h.svc.FrontendAttached(h.rec); err != nil {
		t.Fatal(err)
	}
	if got := h.settled(); got.State != "ok" {
		t.Fatalf("after the cooldown, attach did not check: %+v", got)
	}
}

// Seeing the series is what clears it (PLAN §12.2), and nothing else does.
func TestOpeningTheSeriesClearsTheBadge(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	h.phase(chaptersGrown())
	h.checkNow()
	if got := h.settled(); got.NewChapters != 3 {
		t.Fatalf("setup: %+v", got)
	}

	h.forget()
	h.openSeries()
	got := h.settled()
	if got.NewChapters != 0 || got.State != "ok" {
		t.Fatalf("after opening the series: %+v", got)
	}

	// It stays cleared on the next check, rather than coming straight back.
	h.forget()
	h.clk.advance(service.WatchCooldown + time.Minute)
	h.phase(chaptersGrown())
	h.checkNow()
	if got := h.settled(); got.NewChapters != 0 {
		t.Fatalf("the badge came back: %+v", got)
	}
}

// PLAN §12.2: a watched series whose source is removed goes with it, and the
// UI is told without having to ask.
func TestRemovingASourceRemovesItsWatchedSeries(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	if got := h.list(); len(got) != 1 {
		t.Fatalf("watch list = %+v", got)
	}

	h.forget()
	handle(t, h.svc, h.rec, appload.MessageRemoveSource, `{"sourceId":"example-reader"}`)
	if got := h.list(); len(got) != 0 {
		t.Fatalf("watch list after removing the source = %+v", got)
	}
	if got := h.store.Watches(); len(got) != 0 {
		t.Fatalf("the store kept an orphan: %+v", got)
	}
}

// TestMarkingASourcePrivateDropsItsWatchFromTheOrdinaryList covers the case
// state.Store.Watch's own guard cannot: a series watched *before* its source
// was marked private. The watch survives in the store — unwatching it is
// still the user's call, not something a privacy toggle should silently do —
// but it must vanish from the ordinary Watching list the moment the source
// does, or the badge and title on it are exactly the leak this feature exists
// to prevent.
func TestMarkingASourcePrivateDropsItsWatchFromTheOrdinaryList(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	if got := h.list(); len(got) != 1 {
		t.Fatalf("watch list before marking private = %+v", got)
	}

	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}
	h.forget()

	if err := h.svc.FrontendAttached(h.rec); err != nil {
		t.Fatal(err)
	}
	var env watchListMsg
	if err := json.Unmarshal(h.rec.wait(t, appload.MessageWatchList), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Watched) != 0 {
		t.Fatalf("watch list after marking the source private = %+v, want it dropped", env.Watched)
	}
	if got := h.store.Watches(); len(got) != 1 {
		t.Fatalf("the watch itself was deleted rather than merely hidden: %+v", got)
	}
}

// TestMarkingASourcePrivateMovesItsWatchToThePrivateList is the other half of
// the test above: the watch that vanished from the ordinary list is not gone,
// it is on the private one — checkWatched now checks private watches too, so
// it lands there with a result rather than sitting unchecked forever.
func TestMarkingASourcePrivateMovesItsWatchToThePrivateList(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()

	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}
	h.forget()

	if err := h.svc.FrontendAttached(h.rec); err != nil {
		t.Fatal(err)
	}
	env := h.listMsg()
	if len(env.PrivateWatched) != 1 || env.PrivateWatched[0].SeriesID != seriesPath {
		t.Fatalf("privateWatched = %+v, want the moved watch", env.PrivateWatched)
	}
	if !env.PrivateWatched[0].Private {
		t.Errorf("privateWatched row does not say private: %+v", env.PrivateWatched[0])
	}
}

// TestMarkingASourcePublicMovesItsWatchBackToTheOrdinaryList is
// TestMarkingASourcePrivateMovesItsWatchToThePrivateList run in reverse: a
// source marked private and then public again gets its watch back on the
// ordinary list, still the same watch throughout.
func TestMarkingASourcePublicMovesItsWatchBackToTheOrdinaryList(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()

	if err := h.store.SetPrivate("example-reader", true); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetPrivate("example-reader", false); err != nil {
		t.Fatal(err)
	}
	h.forget()

	if err := h.svc.FrontendAttached(h.rec); err != nil {
		t.Fatal(err)
	}
	env := h.listMsg()
	if len(env.Watched) != 1 || env.Watched[0].SeriesID != seriesPath {
		t.Fatalf("watched = %+v, want the watch back on the ordinary list", env.Watched)
	}
	if len(env.PrivateWatched) != 0 {
		t.Fatalf("privateWatched = %+v, want it emptied", env.PrivateWatched)
	}
	if got := h.store.Watches(); len(got) != 1 {
		t.Fatalf("the watch was duplicated or lost across the round trip: %+v", got)
	}
}

// Watching from a screen the user is looking at must not announce the back
// catalogue as new.
func TestWatchingFromTheSeriesScreenStartsFromWhatIsOnIt(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	h.phase(chaptersSeen())
	h.checkNow()
	if got := h.settled(); got.NewChapters != 0 {
		t.Fatalf("watching a series announced %d of its existing chapters as new", got.NewChapters)
	}
}

// A check that has to make a request says it is checking first, so a list of
// forty draws immediately instead of waiting out the §7.4 limiter.
func TestResultsArriveIncrementally(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()
	h.watch()
	h.forget()

	h.phase(chaptersGrown())
	h.checkNow()
	h.settled()

	var sawChecking bool
	h.rec.mu.Lock()
	for _, f := range h.rec.sent {
		if f.Type != appload.MessageWatchUpdate {
			continue
		}
		var env struct {
			Watch watchRow `json:"watch"`
		}
		if err := json.Unmarshal(f.Payload, &env); err == nil && env.Watch.State == "checking" {
			sawChecking = true
		}
	}
	h.rec.mu.Unlock()
	if !sawChecking {
		t.Error("no per-series update was sent before the result; the shell would have to wait")
	}
}

// PLAN §12.2 and §7.4: a watch check is discovery, however the same request
// would be classified had the user tapped the series.
func TestAWatchCheckAsksAsDiscovery(t *testing.T) {
	f := themetest.New(t, watchRoutes(chaptersGrown()))
	pf := &phasedFetcher{inner: f}
	clk := &clock{at: fixedNow}

	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(pf, clk.now))
	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store: store, Registry: reg, Fetcher: pf,
		Covers: covers.New(dir+"/covers", pf), Now: clk.now, ProbeGuard: allowGuard{},
	})
	t.Cleanup(svc.Close)
	addExampleSource(t, store)
	if _, err := store.Watch("example-reader", seriesPath, "The Lantern Keeper", nil, fixedNow); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageCheckWatched, `{}`)
	rec.wait(t, appload.MessageWatchList)

	for _, c := range f.Calls() {
		if c.Kind != fetch.KindDiscovery {
			t.Errorf("%s %s was made as %s; a watch check is discovery", c.Method, c.URL, c.Kind)
		}
	}
}

// PLAN §12.2's at-a-glance indicator. It has to be on the push that happens on
// attach: the entry point lives on a screen that may never open the watched
// list, so a summary only sent after a check round would leave it blank on the
// screen that matters most.
func TestTheSummaryRidesOnEveryWatchListAndAgreesWithTheRows(t *testing.T) {
	h := newWatchHarness(t, chaptersSeen())
	h.openSeries()

	// Watching it is itself a WatchList push, and there is nothing to report.
	h.watch()
	if got := h.listMsg().Summary; got.Short != "" || got.Phrase != "" {
		t.Errorf("a freshly watched series reports %+v, want silence", got)
	}

	h.forget()
	h.phase(chaptersGrown())
	h.checkNow()
	if got := h.settled(); got.NewChapters != 3 {
		t.Fatalf("setup: %+v", got)
	}

	// The check round ends with a list, and it carries the summary.
	afterCheck := h.listMsg()
	if afterCheck.Summary.Short != "1 new" {
		t.Errorf("short = %q, want %q", afterCheck.Summary.Short, "1 new")
	}
	if afterCheck.Summary.Phrase != "1 series has new chapters" {
		t.Errorf("phrase = %q", afterCheck.Summary.Phrase)
	}

	// And on attach, which is the one the entry point depends on.
	h.forget()
	if err := h.svc.FrontendAttached(h.rec); err != nil {
		t.Fatal(err)
	}
	onAttach := h.listMsg()
	if onAttach.Summary.Short != "1 new" || onAttach.Summary.NewChapters != 3 {
		t.Fatalf("summary on attach = %+v", onAttach.Summary)
	}

	// The summary is computed from the rows it travels with, so the two cannot
	// disagree — "3 new" over a list showing two is the bug this guards.
	var rowsWithBadge, chapters int
	for _, r := range onAttach.Watched {
		if r.NewChapters > 0 {
			rowsWithBadge++
			chapters += r.NewChapters
		}
	}
	if rowsWithBadge != onAttach.Summary.SeriesWithNew || chapters != onAttach.Summary.NewChapters {
		t.Errorf("summary %+v does not match its own rows (%d series, %d chapters)",
			onAttach.Summary, rowsWithBadge, chapters)
	}

	// Looking at the series empties it again, and the indicator goes out
	// rather than reading "0 new".
	h.forget()
	h.openSeries()
	handle(t, h.svc, h.rec, appload.MessageUnwatchSeries,
		`{"sourceId":"example-reader","seriesId":"`+seriesPath+`"}`)
	empty := h.listMsg()
	if len(empty.Watched) != 0 {
		t.Fatalf("watch list = %+v", empty.Watched)
	}
	if empty.Summary.Short != "" || empty.Summary.Phrase != "" {
		t.Errorf("an empty list reports %+v, want silence", empty.Summary)
	}
}
