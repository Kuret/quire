package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// WatchCooldown is the shortest gap between two *automatic* checks of the same
// source's watched series.
//
// Six hours, and the number is argued rather than picked. The check exists to
// answer "has anything appeared?", and the thing being waited on publishes on a
// scale of days — a chapter that lands at nine in the morning is not more useful
// to know about at 09:05 than at noon. What varies on a scale of minutes is how
// often the app is opened, and this device is a tablet someone picks up and puts
// down a dozen times an evening.
//
// So the cooldown is set by the traffic it prevents, not by the freshness it
// costs. At six hours a source is polled at most four times a day however often
// Quire is opened; at the re-probe's fifteen minutes it would be polled up to
// ninety-six times, for a result that changed at most once. That is a crawl, and
// PLAN §12.2 is explicit that a convenience poll deserves §6 M7's restraint more
// than a user-driven request does, not less.
//
// It is longer than ReprobeCooldown deliberately. A re-probe happens *because
// something already looks wrong* and the user is sitting in front of an empty
// screen; this happens because an app was opened, and nobody asked.
//
// An explicit "check now" ignores it entirely. A user who taps a button has
// asked, and answering that with a timer would be the app arguing with them.
const WatchCooldown = 6 * time.Hour

// watchKey identifies one watched series.
type watchKey struct {
	SourceID string
	SeriesID string
}

// watchView is one row of the watch list. Like sourceView, it is a view rather
// than the stored entry: the UI has no business with chapter digests, and every
// word it renders is decided here (PLAN §2 — the frontend is a dumb view).
type watchView struct {
	SourceID   string `json:"sourceId"`
	SeriesID   string `json:"seriesId"`
	SourceName string `json:"sourceName"`
	Title      string `json:"title"`

	// NewChapters is the badge count, and Badge is the same thing in the plain
	// language PLAN §12.2 asks for: "3 new chapters". Both are sent so the UI
	// never has to pluralise, and never has to decide what a number means.
	NewChapters int    `json:"newChapters"`
	Badge       string `json:"badge,omitempty"`

	// State is one of: "new", "ok", "failed", "checking", "unchecked". It is
	// what a row switches on. It is never "ok" on the strength of a failed
	// check — see checkOne.
	State  string `json:"state"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`

	CheckedAt string `json:"checkedAt,omitempty"`
}

const (
	watchStateNew       = "new"
	watchStateOK        = "ok"
	watchStateFailed    = "failed"
	watchStateChecking  = "checking"
	watchStateUnchecked = "unchecked"
)

// watchListView turns the store's watches into rows, in stored order.
func (s *Service) watchListView() []watchView {
	watches := s.store.Watches()
	views := make([]watchView, 0, len(watches))
	for _, w := range watches {
		views = append(views, s.viewOf(w, ""))
	}
	return views
}

// viewOf renders one watch. override replaces the derived state, for "checking".
func (s *Service) viewOf(w *state.Watch, override string) watchView {
	v := watchView{
		SourceID: w.SourceID, SeriesID: w.SeriesID,
		Title:       w.Title,
		NewChapters: w.NewCount,
	}
	if src, ok := s.store.Get(w.SourceID); ok {
		v.SourceName = src.Name
	}
	if !w.CheckedAt.IsZero() {
		v.CheckedAt = w.CheckedAt.UTC().Format(time.RFC3339)
	}

	switch {
	case override == watchStateChecking:
		v.State, v.Status = watchStateChecking, "Checking…"
	case w.Failure != "":
		// PLAN §12.2: failure is not "new", and it is not "nothing new"
		// either. The row says it could not look.
		//
		// Any count it carries is left in place and still says what it says.
		// That is not a false badge: it was established by a check that *did*
		// succeed, and those chapters really are unread. The lie §12.2 forbids
		// is a count invented by a failed check, and the store refuses to write
		// one — see state.RecordCheckFailure. A series that has never had a
		// successful check therefore shows a failure and no badge at all,
		// which is the honest form of "Quire does not know".
		v.State, v.Status, v.Detail = watchStateFailed, "Couldn’t check", w.Failure
	case w.NewCount > 0:
		v.State = watchStateNew
		v.Badge = plural(w.NewCount)
		v.Status = v.Badge
	case w.CheckedAt.IsZero() && w.SeenAt.IsZero():
		v.State, v.Status = watchStateUnchecked, "Not checked yet"
	default:
		v.State, v.Status = watchStateOK, "Up to date"
	}
	if v.NewChapters > 0 && v.Badge == "" {
		v.Badge = plural(v.NewChapters)
	}
	return v
}

// plural is the whole of PLAN §12.2's "present it in plain language".
func plural(n int) string {
	if n == 1 {
		return "1 new chapter"
	}
	return fmt.Sprintf("%d new chapters", n)
}

// watchSummary is the at-a-glance form of the whole list: the counts, and the
// two sentences that go with them (PLAN §12.2).
//
// The counts travel beside the words so the view can decide *whether* to show
// something without deciding *what* it says — a badge that reads "3 new" is a
// sentence, and PLAN §2 keeps sentences out of QML.
type watchSummary struct {
	// SeriesWithNew is how many watched series carry a badge, and NewChapters
	// is how many chapters that adds up to.
	SeriesWithNew int `json:"seriesWithNew"`
	NewChapters   int `json:"newChapters"`

	// Failed is how many series' last check did not finish.
	Failed int `json:"failed"`

	// Short is the entry-point label and Phrase heads the watched screen. Both
	// are "" when there is nothing to report — never "0 new", and never a
	// cheerful "up to date". An indicator that is always lit is one people stop
	// reading, which would defeat the whole feature.
	Short  string `json:"short"`
	Phrase string `json:"phrase"`
}

// summarise reduces the rows to the summary that ships in the same message.
//
// It is computed from the rows rather than tracked alongside them on purpose:
// two counters maintained separately drift, and the way a user meets that is
// "3 new" over a list showing two.
//
// # What counts as new, and what counts as failed
//
// A row counts as new when it carries a badge, whatever its last check did —
// including a row whose last check failed while an *earlier* one found three
// chapters. Those three are still unread and the list still shows them, so
// leaving them out of the summary would be the drift this function exists to
// prevent. A row therefore can be in both buckets, and the two are not a
// partition of the list.
//
// A row mid-check counts as neither. A check round lands one series at a time
// (see checkWatched), and a summary that counted an in-flight row's stale value
// would tick up and back down as the round progressed.
//
// # Why Short says nothing about failures
//
// One series failing while three have new chapters is not the same as
// everything failing and nothing being new — but neither is a reason to light
// the entry point, and the distinction is drawn in Phrase, where the rows that
// say "Couldn't check" are on screen anyway.
//
// The entry point stays quiet about failures because Quire already has a
// considered position on what a failed fetch means: maybeReprobe treats a
// transport error as a blip — wifi dropped, the tablet woke mid-request — and
// refuses to re-probe on one. A badge lit by that same blip would be crying
// wolf on the strength of evidence the project has already decided is not
// evidence. And when a source really has changed, the re-probe says so on the
// source list, which is where a broken source belongs.
//
// "" is not a claim of success. It renders as no indicator at all, so a user
// whose checks are all failing is told nothing rather than told a lie, and the
// watched screen has the whole truth a tap away.
func summarise(rows []watchView) watchSummary {
	var sum watchSummary
	for _, r := range rows {
		if r.State == watchStateChecking {
			continue
		}
		if r.NewChapters > 0 {
			sum.SeriesWithNew++
			sum.NewChapters += r.NewChapters
		}
		if r.State == watchStateFailed {
			sum.Failed++
		}
	}

	if sum.SeriesWithNew > 0 {
		sum.Short = fmt.Sprintf("%d new", sum.SeriesWithNew)
	}

	switch {
	case sum.SeriesWithNew > 0 && sum.Failed > 0:
		sum.Phrase = fmt.Sprintf("%s, and %s couldn’t be checked",
			seriesHaveNew(sum.SeriesWithNew), countOfSeries(sum.Failed))
	case sum.SeriesWithNew > 0:
		sum.Phrase = seriesHaveNew(sum.SeriesWithNew)
	case sum.Failed > 0:
		sum.Phrase = fmt.Sprintf("%s couldn’t be checked", seriesCount(sum.Failed))
	}
	return sum
}

// seriesHaveNew is "1 series has new chapters" / "4 series have new chapters".
// "series" is its own plural, so only the verb moves.
func seriesHaveNew(n int) string {
	if n == 1 {
		return "1 series has new chapters"
	}
	return fmt.Sprintf("%d series have new chapters", n)
}

// seriesCount is "1 series" / "4 series", for the head of a sentence.
func seriesCount(n int) string { return fmt.Sprintf("%d series", n) }

// countOfSeries is the same count once "series" has already been said, so the
// sentence does not repeat itself: "…, and 1 couldn't be checked".
func countOfSeries(n int) string { return fmt.Sprintf("%d", n) }

func (s *Service) sendWatchList(out Sender) error {
	rows := s.watchListView()
	return send(out, appload.MessageWatchList, map[string]any{
		"watched": rows,
		"summary": summarise(rows),
	})
}

func (s *Service) sendWatchUpdate(out Sender, v watchView) {
	_ = send(out, appload.MessageWatchUpdate, map[string]any{"watch": v})
}

// --- the user's side -------------------------------------------------------

// watchSeries marks a series watched.
//
// The baseline comes from the chapter list the backend last served for this
// series, which in practice is the screen the user is looking at as they tap.
// Without it, the first check would find every chapter unseen and announce a
// whole back catalogue as new — technically true and completely useless.
func (s *Service) watchSeries(out Sender, sourceID, seriesID, title string) error {
	seed := s.lastServedChapters(sourceID, seriesID)
	w, err := s.store.Watch(sourceID, seriesID, title, seed, s.now())
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return s.sendError(out, "not_found", "Quire has no source by that name any more.")
		}
		return s.sendError(out, "not_watched", plain(err))
	}
	s.log.Info("watching a series", "source", sourceID, "series", seriesID,
		"seeded", len(seed) > 0)
	s.sendWatchUpdate(out, s.viewOf(w, ""))
	return s.sendWatchList(out)
}

func (s *Service) unwatchSeries(out Sender, sourceID, seriesID string) error {
	if err := s.store.Unwatch(sourceID, seriesID); err != nil {
		return s.sendError(out, "not_unwatched", plain(err))
	}
	return s.sendWatchList(out)
}

// rememberServedChapters records the chapter list just sent to the UI, so that
// watching the series a moment later starts from what the user can see.
//
// Exactly one series is remembered. The user is looking at one screen, the next
// series detail replaces it, and an unbounded cache of chapter IDs on a device
// that shares 2 GB with xochitl is not worth the convenience.
func (s *Service) rememberServedChapters(sourceID, seriesID string, chapterIDs []string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	s.lastDetailKey = watchKey{SourceID: sourceID, SeriesID: seriesID}
	s.lastDetailIDs = append([]string(nil), chapterIDs...)
}

func (s *Service) lastServedChapters(sourceID, seriesID string) []string {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.lastDetailKey != (watchKey{SourceID: sourceID, SeriesID: seriesID}) {
		return nil
	}
	return append([]string(nil), s.lastDetailIDs...)
}

// seriesSeen is PLAN §12.2's "seeing the series clears it". It is called when a
// series detail has been served, which is the only moment Quire can honestly
// claim the user has looked.
func (s *Service) seriesSeen(out Sender, sourceID, seriesID string, chapterIDs []string) {
	if !s.store.IsWatched(sourceID, seriesID) {
		return
	}
	if err := s.store.MarkSeen(sourceID, seriesID, chapterIDs, s.now()); err != nil {
		s.log.Warn("could not clear a watched series", "source", sourceID, "series", seriesID, "err", err)
		return
	}
	for _, w := range s.store.Watches() {
		if w.SourceID == sourceID && w.SeriesID == seriesID {
			s.sendWatchUpdate(out, s.viewOf(w, ""))
			return
		}
	}
}

// --- checking --------------------------------------------------------------

// startWatchCheck runs a check in the background of the *foreground* app: on
// attach, or when the user asks.
//
// It is never started from a timer and never while the frontend is away — see
// FrontendAttached and FrontendDetached. Quire holds no wakelock, and this is
// the work that would most obviously want one (PLAN §6 M7).
func (s *Service) startWatchCheck(out Sender, force bool, only *watchKey) {
	s.watchMu.Lock()
	if s.watchRunning {
		s.watchMu.Unlock()
		// A second run would interleave its updates with the first's and
		// double the traffic to say the same thing.
		s.log.Debug("a watched-series check is already running")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.watchRunning, s.watchCancel = true, cancel
	s.watchMu.Unlock()

	// Tracked, not a bare `go`: the round streams its results in one at a time
	// and does not block the caller, so without an owner it can still be
	// writing check results into the store after whoever started it has gone.
	// See background.go. The context is detached from the caller's on purpose —
	// a check outlives the message that asked for it — but not from the
	// service's, so Close stops it.
	started := s.bg.start(context.Background(), func(bgCtx context.Context) {
		defer func() {
			s.watchMu.Lock()
			s.watchRunning, s.watchCancel = false, nil
			s.watchMu.Unlock()
			cancel()
		}()
		// Either the service closing or stopWatchCheck ends the round.
		stop := context.AfterFunc(bgCtx, cancel)
		defer stop()
		s.checkWatched(ctx, out, force, only)
	})
	if !started {
		// The service is closing. Undo the claim so the state is not left
		// saying a check is running when none is.
		s.watchMu.Lock()
		s.watchRunning, s.watchCancel = false, nil
		s.watchMu.Unlock()
		cancel()
	}
}

// stopWatchCheck cancels a run in progress. Called when the frontend goes away:
// a poll for a convenience badge has no business keeping the radio up once
// nobody is looking at the badge.
func (s *Service) stopWatchCheck() {
	s.watchMu.Lock()
	cancel := s.watchCancel
	s.watchMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// checkWatched walks the watched series one at a time and reports each as it
// lands.
//
// **Strictly sequential, on purpose.** PLAN §12.2 forbids forty requests in a
// burst on launch, and notes that §7.4's limiter (4 global, 2 per host, 2 s
// minimum between requests to one host) will serialise them anyway. Doing it in
// a loop rather than forty goroutines queued behind the limiter means the
// staggering is visible in the code that decides it, the ordering is the user's
// own list order, and cancellation on the frontend leaving stops the *rest* of
// the work rather than just the part that had not reached the limiter yet.
func (s *Service) checkWatched(ctx context.Context, out Sender, force bool, only *watchKey) {
	watches := s.store.Watches()
	if len(watches) == 0 {
		return
	}

	// The cooldown is snapshotted before the loop. Read per series instead, a
	// source's first check would push its own cooldown forward and skip every
	// other series on the same source — which is how "check my watched series"
	// quietly becomes "check one of them".
	lastBySource := map[string]time.Time{}
	for _, w := range watches {
		if at, ok := lastBySource[w.SourceID]; !ok || w.CheckedAt.After(at) {
			lastBySource[w.SourceID] = w.CheckedAt
		}
	}

	now := s.now()
	checked := 0
	for _, w := range watches {
		if ctx.Err() != nil {
			s.log.Info("watched-series check stopped early", "checked", checked)
			return
		}
		if only != nil && (w.SourceID != only.SourceID || w.SeriesID != only.SeriesID) {
			continue
		}

		src, ok := s.store.Get(w.SourceID)
		if !ok {
			// The source went away underneath us. The store drops the watch
			// with it, so this is a race, not a state to report.
			continue
		}
		if !src.IsEnabled() {
			// A disabled source is one the user has switched off. Polling it
			// anyway would be the app overruling them.
			s.log.Debug("skipped a watched series on a disabled source",
				"source", w.SourceID, "series", w.SeriesID)
			continue
		}
		if !force && !w.CheckedAt.IsZero() {
			if at := lastBySource[w.SourceID]; now.Sub(at) < WatchCooldown {
				s.log.Debug("watch check skipped, still in cooldown",
					"source", w.SourceID, "series", w.SeriesID)
				continue
			}
		}

		s.sendWatchUpdate(out, s.viewOf(w, watchStateChecking))
		s.checkOne(ctx, out, src, w)
		checked++
	}
	if checked > 0 {
		s.log.Info("checked watched series", "count", checked, "forced", force)
		_ = s.sendWatchList(out)
	}
}

// checkOne asks one source what chapters a watched series has now.
func (s *Service) checkOne(ctx context.Context, out Sender, src *theme.Source, w *state.Watch) {
	fail := func(reason string) {
		if err := s.store.RecordCheckFailure(w.SourceID, w.SeriesID, reason, s.now()); err != nil {
			s.log.Warn("could not record a failed watch check",
				"source", w.SourceID, "series", w.SeriesID, "err", err)
		}
		s.send1(out, w.SourceID, w.SeriesID)
	}

	th, ok := s.reg.Lookup(src.Theme)
	if !ok {
		fail(fmt.Sprintf("%s uses a site type Quire no longer has.", src.Name))
		return
	}
	// PLAN §12.2: a watch check is discovery, however the same request would be
	// classified when the user taps the series themselves. A theme that cannot
	// say it is doing that is not asked — the alternative is assuming a theme
	// never retrieves anything, which is how §7.4's narrow exception stops
	// being narrow.
	dc, ok := th.(theme.DiscoveryClassifier)
	if !ok {
		s.log.Warn("a theme cannot make an automated check politely",
			"theme", src.Theme, "source", src.ID)
		fail("Quire can’t check this source automatically without treating it as a crawl.")
		return
	}

	chapters, err := dc.DiscoveryOnly().Chapters(ctx, src, w.SeriesID)
	switch {
	case ctx.Err() != nil:
		// Stopped because the frontend went away. That is not a failure of the
		// source, and recording it as one would put "couldn't check" on a row
		// whose check was never allowed to finish.
		return

	case err != nil:
		s.log.Info("a watched series could not be checked",
			"source", src.ID, "series", w.SeriesID, "err", err)
		fail(plain(err))
		return

	case len(chapters) == 0:
		// The series resolved and has no chapters at all. PLAN §6 M7 and §12.2
		// agree on what that is: evidence the source changed, and the trigger
		// the existing re-probe machinery was built for. It is reported as a
		// failed check, never as "nothing new" — zero-as-success is the exact
		// lie §12.2 forbids.
		fail("The source returned no chapters at all, so Quire is checking whether it has changed.")
		// Run in this goroutine, not a new one: the check's context is
		// cancelled the moment the run ends, and a re-probe on a detached
		// context would either be killed mid-probe (reporting "context
		// canceled" as the site's verdict) or outlive the foreground, which
		// PLAN §6 M7 forbids. Sequential is also the honest cost — a re-probe
		// is six more stages of requests.
		s.maybeReprobe(ctx, out, src, reasonEmptyChapters)
		return
	}

	ids := make([]string, 0, len(chapters))
	for _, c := range chapters {
		ids = append(ids, c.ID)
	}
	n, err := s.store.RecordCheck(w.SourceID, w.SeriesID, ids, s.now())
	if err != nil {
		s.log.Warn("could not record a watch check",
			"source", w.SourceID, "series", w.SeriesID, "err", err)
		return
	}
	if n > 0 {
		s.log.Info("a watched series has new chapters",
			"source", src.ID, "series", w.SeriesID, "new", n)
	}
	s.send1(out, w.SourceID, w.SeriesID)
}

// send1 re-reads one watch from the store and pushes it. Re-reading rather than
// rendering the in-hand copy is deliberate: the stored entry is what the next
// launch will show, so sending anything else would let the badge on screen and
// the badge after a restart disagree.
func (s *Service) send1(out Sender, sourceID, seriesID string) {
	for _, w := range s.store.Watches() {
		if w.SourceID == sourceID && w.SeriesID == seriesID {
			s.sendWatchUpdate(out, s.viewOf(w, ""))
			return
		}
	}
}
