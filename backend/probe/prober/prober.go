// Package prober is PLAN §7.5: the six-stage source probe that runs when a user
// pastes a URL, and again when a working source starts failing (PLAN §6 M7).
//
// It is a subpackage of probe rather than probe itself for one mechanical
// reason: PLAN §7.2 spells the theme interface as Fingerprint(p *probe.Page),
// so theme imports probe. The stage machinery needs the theme registry, and Go
// will not have the cycle. probe therefore keeps the leaf type every theme
// depends on, and probe/prober holds the orchestration that depends on themes.
//
// The probe is what makes "sources are data, not code" (PLAN §1.1) usable: the
// user supplies an address and nothing else, and everything the app needs to
// read the site is worked out here. Stages run in order and any stage may end
// the probe with a final verdict. Progress is streamed as each stage starts and
// finishes, so a slow site does not look hung.
//
// Two rules in this file are not negotiable:
//
//   - Stage 3 is a hard gate. A detected browser challenge means the source is
//     refused: no degraded add, no retry, no workaround (PLAN §7.6). There is
//     deliberately no flag, option or code path that skips it.
//   - A verdict is a complete, honest answer. The probe never reports a refusal
//     that did not happen — an unreadable robots.txt is `unreachable`, not
//     `robots_denied` (PLAN §7.4).
package prober

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// StageCount is how many stages PLAN §7.5 defines. The UI shows "step n of 6"
// rather than an animated spinner: e-ink ghosting rules out anything that
// redraws continuously (PLAN §6 M3).
const StageCount = 6

// Stage names, as sent to the UI and as used in progress text.
const (
	StageGuard       = 1
	StageReachable   = 2
	StageChallenge   = 3
	StageFingerprint = 4
	StageCapability  = 5
	StageAccept      = 6
)

var stageNames = map[int]string{
	StageGuard:       "Checking the address",
	StageReachable:   "Contacting the site",
	StageChallenge:   "Looking for a browser challenge",
	StageFingerprint: "Recognising the site",
	StageCapability:  "Trying a real search",
	StageAccept:      "Finishing up",
}

// DefaultThreshold is the fingerprint confidence floor (PLAN §7.5 stage 4).
// Measured against the M2 fixtures, winners score 65–100, losers 0 and the best
// near-miss 5, so 60 sits in open space rather than on a boundary; the gap is
// pinned by backend/theme/fingerprint_test.go.
const DefaultThreshold = 60

// NearMiss is how close the runner-up may come to the winner before the choice
// goes to the user instead of being made for them (PLAN §7.5 stage 4: "ties or
// near-ties go to the user as a choice").
const NearMiss = 10

// capabilityQuery is the query used for stage 5's search. PLAN §7.5 allows the
// popular/latest listing instead "if search needs a query"; every theme we have
// implements search over a plain query string, so one short, common substring
// exercises the same path with one request.
const capabilityQuery = "a"

// Progress is one streamed stage update (message type 13). It is deliberately
// tiny: PLAN §7.1's real payload ceiling is a few hundred KB and this is sent
// several times per probe.
type Progress struct {
	Stage int    `json:"stage"`
	Total int    `json:"total"`
	Name  string `json:"name"`

	// Text is plain language shown as-is. Never an error code.
	Text string `json:"text,omitempty"`

	// Done marks the end of a stage rather than its start.
	Done bool `json:"done"`
}

// Question is a point where PLAN §7.5 requires the user to decide rather than
// the probe guessing: a redirect that left the domain they typed, or two themes
// too close to call.
type Question struct {
	// Kind is "redirect" or "theme".
	Kind string `json:"kind"`

	// Text is the whole question, in plain language.
	Text string `json:"text"`

	// Options are the answers on offer. The UI renders one button each.
	Options []Option `json:"options"`
}

// Option is one answer to a Question.
type Option struct {
	// ID is what comes back in an Answer: "continue", "cancel", or a theme ID.
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Answer is the user's reply. An empty or unrecognised ID is treated as
// "cancel": when in doubt the probe stops rather than proceeding on a guess.
type Answer struct {
	ID string `json:"id"`
}

// UI is how the prober talks to the frontend. The service implementation sends
// message types 13 and 14 over the AppLoad socket; tests supply a script.
type UI interface {
	// Progress reports a stage starting or finishing.
	Progress(p Progress)

	// Ask puts a question to the user and blocks for the answer. Returning an
	// error cancels the probe.
	Ask(ctx context.Context, q Question) (Answer, error)
}

// Result is the outcome of a probe.
type Result struct {
	// Verdict is the PLAN §7.5 enum, empty only when Cancelled is set.
	Verdict string `json:"verdict,omitempty"`

	// Cancelled means the user answered "no" to a question. That is not a
	// verdict about the site — we stopped before reaching one — so it is a
	// separate field rather than a ninth enum value.
	Cancelled bool `json:"cancelled,omitempty"`

	// Detail is plain language, shown to the user as-is (PLAN §6 M3).
	Detail string `json:"detail,omitempty"`

	// ThemeID is the winning theme, when there is one.
	ThemeID string `json:"theme,omitempty"`

	// ThemeScores is every theme's opinion, kept so an "unrecognised" verdict
	// can name what was tried.
	ThemeScores map[string]int `json:"themeScores,omitempty"`

	// Warnings are non-blocking findings — a login wall, a paywall, an age
	// gate. PLAN §7.5 stage 3 is explicit that these are surfaced, not acted
	// on: the user decides.
	Warnings []string `json:"warnings,omitempty"`

	// Addable is whether the UI may offer to add this source at all. It is
	// false for every failure verdict, and false for a `partial` whose page
	// extraction failed — a source that cannot yield page images is useless.
	Addable bool `json:"addable"`

	// Draft is the stage 6 source entry, ready to be confirmed and persisted.
	// Nil unless Addable.
	Draft *theme.Source `json:"draft,omitempty"`

	// FinalURL is where we ended up after redirects, and Title is the site
	// <title> the default name comes from.
	FinalURL string `json:"finalUrl,omitempty"`
	Title    string `json:"title,omitempty"`

	// At is when the probe finished, for the stored lastProbe block.
	At time.Time `json:"at"`
}

// AddressGuard is the slice of fetch.Guard stage 1 needs. It is an interface so
// a test can refuse an address without a resolver, and so the probe cannot
// accidentally acquire a second, unguarded network path.
type AddressGuard interface {
	CheckURL(ctx context.Context, u *url.URL, p *fetch.Policy) error
}

// Options configures a Prober.
type Options struct {
	// Fetcher is the guarded, rate-limited, robots-respecting client. The probe
	// has no other way to reach the network.
	Fetcher theme.Fetcher

	// Registry supplies the stage 4 fan-out and the stage 5 theme.
	Registry *theme.Registry

	// Guard is stage 1's SSRF check. Nil means a real fetch.Guard.
	Guard AddressGuard

	// Now is injectable for tests. Nil means time.Now.
	Now func() time.Time

	// Threshold overrides DefaultThreshold. Zero means the default.
	Threshold int

	// NewID mints the draft source's local ID. Nil means a timestamp-derived
	// one; the store gives it a final, collision-free value on save.
	NewID func() string
}

// Prober runs PLAN §7.5.
type Prober struct {
	fetch     theme.Fetcher
	reg       *theme.Registry
	guard     AddressGuard
	now       func() time.Time
	threshold int
	newID     func() string
}

// New builds a Prober.
func New(opts Options) *Prober {
	p := &Prober{
		fetch:     opts.Fetcher,
		reg:       opts.Registry,
		guard:     opts.Guard,
		now:       opts.Now,
		threshold: opts.Threshold,
	}
	if p.guard == nil {
		p.guard = &fetch.Guard{}
	}
	if p.now == nil {
		p.now = time.Now
	}
	if p.threshold == 0 {
		p.threshold = DefaultThreshold
	}
	p.newID = opts.NewID
	if p.newID == nil {
		p.newID = func() string {
			return "src-" + p.now().UTC().Format("20060102-150405")
		}
	}
	return p
}

// silentUI is used when a caller passes no UI. Questions are answered "cancel":
// PLAN §7.5 says a redirect off the typed domain must be *asked about*, so an
// unattended probe stops rather than helping itself to a yes.
type silentUI struct{}

func (silentUI) Progress(Progress) {}
func (silentUI) Ask(context.Context, Question) (Answer, error) {
	return Answer{ID: "cancel"}, nil
}

// Run executes the six stages against rawurl.
//
// It returns a Result rather than an error for anything the user should see: a
// verdict *is* the answer, and turning it into an error would tempt callers to
// print a Go error string at somebody who pasted a URL. A returned error means
// the probe itself could not run (a cancelled context, a UI that went away).
func (p *Prober) Run(ctx context.Context, rawurl string, ui UI) (Result, error) {
	if ui == nil {
		ui = silentUI{}
	}
	r := &run{p: p, ui: ui}
	res, err := r.exec(ctx, rawurl)
	res.At = p.now().UTC()
	return res, err
}

// run is one probe's mutable state. A Prober is reusable and concurrent; a run
// is neither.
type run struct {
	p  *Prober
	ui UI

	page     *probe.Page
	scores   []theme.Score
	warnings []string
}

func (r *run) start(stage int) {
	r.ui.Progress(Progress{Stage: stage, Total: StageCount, Name: stageNames[stage]})
}

func (r *run) done(stage int, text string) {
	r.ui.Progress(Progress{Stage: stage, Total: StageCount, Name: stageNames[stage], Text: text, Done: true})
}

func (r *run) exec(ctx context.Context, rawurl string) (Result, error) {
	// Stage 1 — normalise and guard.
	r.start(StageGuard)
	base, res, ok := r.stageGuard(ctx, rawurl)
	if !ok {
		return res, nil
	}
	r.done(StageGuard, "Address looks usable.")

	// Stage 2 — reachability.
	r.start(StageReachable)
	res, cont, err := r.stageReachable(ctx, base)
	if err != nil || !cont {
		return res, err
	}
	r.done(StageReachable, fmt.Sprintf("The site answered (%d).", r.page.Status))

	// Stage 3 — challenge and gate detection. A hard gate.
	r.start(StageChallenge)
	if res, blocked := r.stageChallenge(); blocked {
		return res, nil
	}
	r.done(StageChallenge, "No browser challenge detected.")

	// Stage 4 — theme fingerprint.
	r.start(StageFingerprint)
	th, res, err := r.stageFingerprint(ctx)
	if err != nil {
		return res, err
	}
	if th == nil {
		return res, nil
	}
	r.done(StageFingerprint, fmt.Sprintf("This looks like a %s site.", th.ID()))

	// Stage 5 — capability check.
	r.start(StageCapability)
	draft := r.draft(base, th.ID())
	cap := r.stageCapability(ctx, th, draft)
	r.done(StageCapability, cap.summary())

	// Stage 6 — accept.
	r.start(StageAccept)
	res = r.stageAccept(draft, th.ID(), cap)
	r.done(StageAccept, res.Detail)
	return res, nil
}

// result builds a Result carrying the state gathered so far.
func (r *run) result(verdict, detail string) Result {
	res := Result{
		Verdict:     verdict,
		Detail:      detail,
		ThemeScores: theme.ScoreMap(r.scores),
		Warnings:    r.warnings,
	}
	if r.page != nil {
		res.Title = r.page.Title()
		if r.page.FinalURL != nil {
			res.FinalURL = r.page.FinalURL.String()
		}
	}
	return res
}

// stageGuard is PLAN §7.5 stage 1: parse, require http(s), resolve DNS, apply
// the SSRF guard.
func (r *run) stageGuard(ctx context.Context, rawurl string) (*url.URL, Result, bool) {
	raw := strings.TrimSpace(rawurl)
	if raw == "" {
		return nil, r.result(theme.VerdictInvalidURL, "That address is empty. Paste the site's web address, starting with https://."), false
	}
	// "Normalise" is the first word of the stage's name: a user pasting
	// example.com from a browser's address bar has not made a mistake, so the
	// missing scheme is filled in rather than thrown back at them. Anything
	// that *does* carry a scheme keeps it, so a typed http:// or ftp:// is
	// judged on its own merits below.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, r.result(theme.VerdictInvalidURL, fmt.Sprintf("That doesn't look like a web address: %v", err)), false
	}
	// The probe only ever wants the site root. A pasted deep link is trimmed
	// back to it rather than being probed as if it were the homepage.
	u = &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}

	if err := r.p.guard.CheckURL(ctx, u, &fetch.Policy{BaseURL: u}); err != nil {
		verdict, detail := guardVerdict(err)
		return nil, r.result(verdict, detail), false
	}
	return u, Result{}, true
}

// guardVerdict maps a guard or fetch error onto the §7.5 enum, in plain
// language. It is the one place that translation happens, so a new caller
// cannot invent a different wording for the same refusal.
func guardVerdict(err error) (verdict, detail string) {
	var ge *fetch.GuardError
	if errors.As(err, &ge) {
		if errors.Is(ge.Kind, fetch.ErrBlockedAddress) {
			return theme.VerdictBlockedAddress, "Quire won't connect to that address: " + ge.Reason + "."
		}
		return theme.VerdictInvalidURL, "Quire can't use that address: " + ge.Reason + "."
	}
	switch {
	case errors.Is(err, fetch.ErrBlockedAddress):
		return theme.VerdictBlockedAddress, "Quire won't connect to that address."
	case errors.Is(err, fetch.ErrInvalidURL):
		return theme.VerdictInvalidURL, "That doesn't look like a web address Quire can use."
	}
	return "", ""
}

// stageReachable is PLAN §7.5 stage 2. It returns cont=false when the probe is
// over, with the Result to report.
func (r *run) stageReachable(ctx context.Context, base *url.URL) (Result, bool, error) {
	pol := &fetch.Policy{BaseURL: base}
	resp, err := r.p.fetch.Get(ctx, pol, base.String())
	if err != nil {
		return r.reachError(base, err), false, nil
	}

	r.page = probe.NewPage(base, resp.FinalURL, resp.StatusCode, resp.Header, resp.Body)

	// A redirect that leaves the registrable domain the user typed is a
	// question, not a log line (PLAN §7.5 stage 2).
	typed := fetch.RegistrableDomain(base.Hostname())
	final := base
	if resp.FinalURL != nil {
		final = resp.FinalURL
	}
	if landed := fetch.RegistrableDomain(final.Hostname()); landed != "" && typed != "" && landed != typed {
		ans, err := r.ui.Ask(ctx, Question{
			Kind: "redirect",
			Text: fmt.Sprintf("You asked for %s, but it sent Quire to %s. Add %s instead?", typed, landed, landed),
			Options: []Option{
				{ID: "continue", Label: "Yes, use " + landed},
				{ID: "cancel", Label: "No, stop"},
			},
		})
		if err != nil {
			return Result{}, false, err
		}
		if ans.ID != "continue" {
			res := r.result("", "Stopped: the site redirected somewhere you didn't ask for.")
			res.Cancelled = true
			return res, false, nil
		}
		// Everything from here on is judged against where we actually landed.
		base.Scheme, base.Host = final.Scheme, final.Host
	}

	if r.page.Status >= 400 {
		// A 403 or 503 is the usual shape of a challenge, so stage 3 gets to
		// look at this body before we call the site unreachable.
		if res, blocked := r.stageChallenge(); blocked {
			return res, false, nil
		}
		return r.result(theme.VerdictUnreachable,
			fmt.Sprintf("The site answered with HTTP %d, so Quire couldn't read it.", r.page.Status)), false, nil
	}
	return Result{}, true, nil
}

// reachError turns a transport-layer failure into a verdict.
func (r *run) reachError(base *url.URL, err error) Result {
	switch {
	case errors.Is(err, fetch.ErrRobotsDenied):
		return r.result(theme.VerdictRobotsDenied,
			"This site's robots.txt asks automated tools not to read the pages Quire needs, so Quire won't add it.")
	case errors.Is(err, fetch.ErrRobotsUnavailable):
		// PLAN §7.4: we could not *ask*. Reporting that as a denial would be a
		// refusal that never happened.
		return r.result(theme.VerdictUnreachable,
			"Quire couldn't read this site's robots.txt, so it can't tell whether it's allowed to read the site. Nothing was added.")
	case errors.Is(err, fetch.ErrTooLarge):
		return r.result(theme.VerdictUnreachable, "The site's home page is far larger than Quire will download.")
	}
	if verdict, detail := guardVerdict(err); verdict != "" {
		return r.result(verdict, detail)
	}
	return r.result(theme.VerdictUnreachable,
		fmt.Sprintf("Quire couldn't reach %s: %v", base.Hostname(), err))
}

// stageFingerprint is PLAN §7.5 stage 4. A nil theme with a nil error means the
// Result is final.
func (r *run) stageFingerprint(ctx context.Context) (theme.Theme, Result, error) {
	scores := r.fingerprint()
	if len(scores) == 0 || scores[0].Score < r.p.threshold {
		return nil, r.result(theme.VerdictUnrecognised, r.unrecognisedDetail()), nil
	}

	winner := scores[0]
	// A runner-up within NearMiss is a near-tie, and PLAN §7.5 hands that to
	// the user. Guessing here is how a site ends up half-working with no
	// explanation.
	if len(scores) > 1 && scores[1].Score >= r.p.threshold && winner.Score-scores[1].Score <= NearMiss {
		opts := make([]Option, 0, 3)
		for _, s := range scores[:2] {
			opts = append(opts, Option{ID: s.ThemeID, Label: fmt.Sprintf("%s (%d%% match)", s.ThemeID, s.Score)})
		}
		opts = append(opts, Option{ID: "cancel", Label: "Stop"})
		ans, err := r.ui.Ask(ctx, Question{
			Kind:    "theme",
			Text:    "Quire can't tell which kind of site this is. Which one should it use?",
			Options: opts,
		})
		if err != nil {
			return nil, Result{}, err
		}
		if ans.ID == "cancel" || ans.ID == "" {
			res := r.result("", "Stopped: no site type chosen.")
			res.Cancelled = true
			return nil, res, nil
		}
		th, ok := r.p.reg.Lookup(ans.ID)
		if !ok {
			res := r.result("", "Stopped: that site type is not one Quire knows.")
			res.Cancelled = true
			return nil, res, nil
		}
		return th, Result{}, nil
	}

	th, ok := r.p.reg.Lookup(winner.ThemeID)
	if !ok {
		// Only reachable if a theme scored and then vanished from the registry.
		return nil, r.result(theme.VerdictUnrecognised, r.unrecognisedDetail()), nil
	}
	return th, Result{}, nil
}

// fingerprint runs the stage 4 fan-out once and memoises it. Stage 3's generic
// JS-gate signal needs the same answer, and PLAN §7.5's own reason for caching
// the parsed page applies to the scoring too.
func (r *run) fingerprint() []theme.Score {
	if r.scores == nil && r.page != nil {
		r.scores = r.p.reg.Fingerprint(r.page)
	}
	return r.scores
}

// unrecognisedDetail names every theme that was tried, so the user can report
// the site usefully (PLAN §7.5 stage 4).
func (r *run) unrecognisedDetail() string {
	tried := make([]string, 0, len(r.scores))
	for _, s := range r.scores {
		tried = append(tried, fmt.Sprintf("%s (%d)", s.ThemeID, s.Score))
	}
	if len(tried) == 0 {
		return "Quire doesn't recognise this site's layout, and it has no site types to compare it against."
	}
	return "Quire doesn't recognise this site's layout. It compared it against: " +
		strings.Join(tried, ", ") + ". Nothing was added."
}

// draft is the stage 6 source entry, built before stage 5 because the
// capability check needs a Source to exercise the theme against.
func (r *run) draft(base *url.URL, themeID string) *theme.Source {
	name := ""
	if r.page != nil {
		name = siteName(r.page.Title())
	}
	if name == "" {
		name = base.Hostname()
	}
	lang := "en"
	if r.page != nil {
		if l := htmlLang(r.page); l != "" {
			lang = l
		}
	}
	return &theme.Source{
		ID:      r.p.newID(),
		Name:    name,
		Lang:    lang,
		Theme:   themeID,
		BaseURL: strings.TrimSuffix(base.String(), "/"),
		AddedAt: r.p.now().UTC(),
	}
}

// stageAccept is PLAN §7.5 stage 6, plus the refusals stage 5 can produce.
func (r *run) stageAccept(draft *theme.Source, themeID string, cap capability) Result {
	var res Result
	switch {
	case cap.ok():
		res = r.result(theme.VerdictOK, fmt.Sprintf("Quire read this site as a %s site and searched it successfully.", themeID))
		res.Addable = true
	case cap.addableDegraded():
		res = r.result(theme.VerdictPartial, "Quire can search this site and list chapters, but "+cap.failure()+
			" You can add it, but expect gaps.")
		res.Addable = true
	default:
		// PLAN §7.5: if page extraction fails the source is useless, so refuse.
		res = r.result(theme.VerdictPartial, "Quire recognised this site but "+cap.failure()+
			" A source Quire can't read pages from is no use, so nothing was added.")
	}
	res.ThemeID = themeID
	if res.Addable {
		draft.LastProbe = &theme.ProbeResult{
			Verdict:     res.Verdict,
			At:          r.p.now().UTC(),
			Detail:      res.Detail,
			ThemeScores: res.ThemeScores,
		}
		res.Draft = draft
	}
	return res
}

// siteName trims the boilerplate a WordPress <title> carries, so the default
// source name is "Example Reader" rather than "Example Reader - Read manga
// online free".
func siteName(title string) string {
	t := theme.Collapse(title)
	for _, sep := range []string{" - ", " – ", " — ", " | ", " :: "} {
		if i := strings.Index(t, sep); i > 0 {
			t = t[:i]
			break
		}
	}
	if len(t) > 60 {
		t = strings.TrimSpace(t[:60])
	}
	return t
}

// htmlLang reads <html lang="…">, which is a better default than assuming
// English and costs nothing — the document is already parsed.
func htmlLang(p *probe.Page) string {
	doc, err := p.Document()
	if err != nil {
		return ""
	}
	lang := strings.TrimSpace(doc.Find("html").AttrOr("lang", ""))
	if lang == "" {
		return ""
	}
	// Keep only what schema/source.schema.json's BCP-47 pattern accepts.
	for _, part := range strings.Split(lang, "-") {
		if part == "" {
			return ""
		}
	}
	base := strings.Split(lang, "-")[0]
	if len(base) < 2 || len(base) > 3 {
		return ""
	}
	for _, c := range base {
		if c < 'a' || c > 'z' {
			if c < 'A' || c > 'Z' {
				return ""
			}
		}
	}
	return strings.ToLower(base)
}
