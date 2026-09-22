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
	"net/netip"
	"net/url"
	"regexp"
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

// capabilityListing is stage 5's first attempt at a search: PLAN §7.5's "or the
// popular/latest listing if search needs a query", which PLAN §12.1 made
// implementable by defining Browse as a search with an empty query.
//
// CORRECTION 2026-09-16 — this used to be a one-character query, "a", and that
// was the probe manufacturing its own failure. comick.art drops any search
// query shorter than three characters at the edge: `q=a` and `q=ab` answer 444
// with no body, `q=dra` and no query at all answer 200. Quire probed with "a",
// was dropped, and reported the site as one it could not search. A one-
// character query is exactly the shape an API rejects as abusive, and it is not
// what any user would type. The empty-query listing is what stage 5 asked for,
// it is what the user sees on the source's front page, and it invents nothing.
const capabilityListing = ""

// capabilityQuery is the fallback, used only when the listing yields nothing.
//
// Three characters is the floor: that is the length comick.art drops below, and
// short queries are the ones edges treat as abuse. "one" is deliberately
// unremarkable — a common word in titles across languages of romanised manga
// ("One Piece", "One Punch Man", "Someone…"), so a site with a catalogue will
// match it, and it asks for nothing unusual.
const capabilityQuery = "one"

// capabilityCandidateBudget is the wall-clock time limit for attempting
// multiple candidates in a source probe — originally named for the file-based
// (book) branch alone, then widened (2026-09-22) to also gate the page-based
// candidate retry MangaHere's own false `partial` (a licensed, most-popular
// series sampled by stage 5) showed was needed. /api/releases measured at
// ~36s on a live Shelfmark instance on 2026-09-20, so 90s admits the second
// and third candidate in the normal case while stopping a pathological one
// from stacking up three 6-minute fetch.SlowRequestTimeout waits. The first
// candidate is always tried in full, never skipped for time.
const capabilityCandidateBudget = 90 * time.Second

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
// the probe guessing: a redirect that left the domain they typed, two themes
// too close to call, an address that is only reachable if this is a service
// of the user's own, or a page image served from a different domain than the
// source itself.
type Question struct {
	// Kind is "redirect", "theme", "selfhosted" or "imagehost".
	Kind string `json:"kind"`

	// Text is the whole question, in plain language.
	Text string `json:"text"`

	// Options are the answers on offer. The UI renders one button each.
	Options []Option `json:"options"`

	// Input, when set, is an optional value the user may type *alongside*
	// choosing an option — today only the proxy offered with a self-hosted
	// address, because a user adding a host on their own network is exactly the
	// person who may need one and sending them back through the flow to add it
	// afterwards would be a second trip for one field.
	//
	// It rides on the existing question rather than on a second channel. There
	// is one place the probe stops to ask the user something, and it stays one
	// place: a separate "and also type this" message would be a second thing to
	// keep in step with the wizard for no gain.
	Input *Input `json:"input,omitempty"`
}

// Input describes the optional field a Question may carry.
type Input struct {
	// Label says what the field is for, in plain language.
	Label string `json:"label"`

	// Placeholder is an example, shown while the field is empty. It is an
	// example and not a default: nothing is sent unless the user types it.
	Placeholder string `json:"placeholder,omitempty"`
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

	// Text is what was typed into the question's Input, empty when the question
	// had none or the user left it blank. A question that did not ask for text
	// ignores it.
	Text string `json:"text,omitempty"`
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

// SelfHostableGuard is the side interface stage 1 needs to *offer* a private
// address rather than merely refusing it: it reports which address the host
// resolved to, when that address is one a user may confirm as a service of
// their own (fetch.SelfHostableAddr).
//
// It is optional, and silence is the old behaviour: a guard that does not
// implement it makes no offer and the probe stops at `blocked_address`, exactly
// as it did before. That direction is deliberate — the safe answer is the one a
// guard gets by saying nothing.
//
// It grants nothing by itself. The probe takes the address, asks the user, and
// then runs CheckURL again with the confirmation on the policy, so the
// exemption is still granted by the guard and only by the guard.
type SelfHostableGuard interface {
	SelfHostableTarget(ctx context.Context, u *url.URL) (netip.Addr, bool)
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

	// assumedHTTPS records that stage 1 supplied the scheme because the user
	// typed none. Stage 2 says so when the site cannot be reached: Quire
	// guessed, and a guess that failed has to be admitted rather than quietly
	// retried over an unencrypted connection.
	assumedHTTPS bool

	// selfHosted is the confirmation the user gave in stage 1, nil when they
	// were never asked or said no. It carries the address the probe actually
	// resolved, which is what makes it a record of what was agreed rather than
	// of a name (see theme.SelfHosted).
	//
	// Every policy this run builds carries it from that moment on, so stages 2
	// to 6 reach the host the user confirmed and nothing else does: the
	// exemption is per-URL inside the guard, so a cover URL or a redirect
	// pointing anywhere else on that network is still refused.
	selfHosted *theme.SelfHosted

	// proxy is the proxy the user supplied with that confirmation, "" when they
	// supplied none. It is already validated: an unusable one ends the probe at
	// stage 1 rather than being carried to a request that will fail oddly.
	proxy string
}

// policy is the fetch-layer view of the site being probed. Every stage builds
// its policy through here so that a confirmation or a proxy given in stage 1
// applies to the rest of the probe — and so that there is one place to read to
// know what the probe's requests are governed by.
func (r *run) policy(base *url.URL) *fetch.Policy {
	p := &fetch.Policy{BaseURL: base}
	if r.selfHosted != nil {
		p.SelfHostedHost = base.Hostname()
	}
	if r.proxy != "" {
		// Parsed, not re-validated: stage 1 refused anything unusable, and a
		// second opinion here could only disagree with the one the user was
		// shown.
		if pu, err := fetch.ParseProxyURL(r.proxy); err == nil {
			p.Proxy = pu
		}
	}
	return p
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
	base, res, ok, err := r.stageGuard(ctx, rawurl)
	if err != nil {
		return res, err
	}
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
	draft := r.draft(base, th)
	cap, err := r.stageCapability(ctx, th, draft)
	if err != nil {
		return Result{}, err
	}
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
func (r *run) stageGuard(ctx context.Context, rawurl string) (*url.URL, Result, bool, error) {
	raw := strings.TrimSpace(rawurl)
	if raw == "" {
		return nil, r.result(theme.VerdictInvalidURL, "That address is empty. Type the site's address, like weebcentral.com."), false, nil
	}
	raw, assumed, problem := normaliseScheme(raw)
	if problem != "" {
		return nil, r.result(theme.VerdictInvalidURL, problem), false, nil
	}
	r.assumedHTTPS = assumed

	u, err := url.Parse(raw)
	if err != nil {
		return nil, r.result(theme.VerdictInvalidURL, fmt.Sprintf("That doesn't look like a web address: %v", err)), false, nil
	}
	// A typed scheme is the user being specific, so credentials in it are the
	// guard's business (PLAN §7.4). When Quire supplied the scheme, an "@" is
	// far more likely to be an email address than a URL, and saying so beats
	// reporting "credentials in URL are not allowed" at somebody who typed
	// their address book.
	if assumed && u.User != nil {
		return nil, r.result(theme.VerdictInvalidURL,
			"That looks like an email address, not a site. Type just the site's address, like weebcentral.com."), false, nil
	}
	// The probe only ever wants the site root. A pasted deep link is trimmed
	// back to it rather than being probed as if it were the homepage.
	u = &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}

	if err := r.p.guard.CheckURL(ctx, u, r.policy(u)); err != nil {
		// A refusal here is usually the end of the probe. There are two cases
		// where it is a *question* instead, and they are the only way a
		// self-hosted source has ever been addable: a host that resolves into a
		// private or CGNAT range, and a host that does not resolve here at all.
		// Both may be a service on the user's own network, which is a fact only
		// they can supply.
		res, offered, askErr := r.offerSelfHosted(ctx, u, err)
		if askErr != nil {
			return nil, Result{}, false, askErr
		}
		if !offered {
			return nil, res, false, nil
		}
		// The guard has the last word. It is re-run with the confirmation on the
		// policy rather than assumed to pass, so the exemption is granted by the
		// same code that refuses everything else — and a host that resolves
		// somewhere no confirmation can cover is still refused here.
		if err := r.p.guard.CheckURL(ctx, u, r.policy(u)); err != nil {
			verdict, detail := guardVerdict(err)
			return nil, r.result(verdict, detail), false, nil
		}
	}
	return u, Result{}, true, nil
}

// offerSelfHosted is PLAN §7.5 stage 1's one question.
//
// The probe used to stop here: a base URL resolving into a private or CGNAT
// range was `blocked_address` and that was the end of it, which left
// hand-editing sources.json as the only way to add a service on your own
// network. The address rules are still exactly right for the case they were
// written for — a *scraped* URL pointing at the local network, where the
// request is the attack (see fetch.Guard) — but they cannot tell that case apart
// from the user typing their own NAS, and this is the one place where the user
// can.
//
// **Two refusals reach this, not one.** The address one above, and a host that
// could not be looked up at all — which on the device is the commoner of the
// two and was found by trying it (2026-09-20): the owner's source is a MagicDNS
// name on their mesh, the tablet resolves against public DNS, the kernel has no
// TUN device so the userspace VPN installs no resolver, and the name simply
// does not resolve here. Offering the question only on `blocked_address` meant
// the probe died with a lookup error and the user was never offered the proxy
// that makes it work. A proxy is needed *before* resolution, and for that
// source resolution will never succeed at all.
//
// So the refusal becomes an offer, on three conditions that keep it from being
// a way in:
//
//   - the address is named plainly, so what is being agreed to is visible;
//   - the default is no — an empty, unrecognised or cancelled answer leaves the
//     refusal standing, which is also what an unattended probe gets (silentUI);
//   - only fetch.SelfHostableAddr ranges are offered at all, so loopback,
//     link-local (169.254.169.254 is the cloud metadata service) and the
//     reserved ranges are never on the table.
//
// offered=false means the Result returned is the refusal to report.
func (r *run) offerSelfHosted(ctx context.Context, u *url.URL, refusal error) (Result, bool, error) {
	verdict, detail := guardVerdict(refusal)
	refused := r.result(verdict, detail)

	host := u.Hostname()
	addr, known := r.offerableAddr(ctx, u, refusal)
	if !known && !unresolvedHost(refusal) {
		// Neither of the two refusals this question is for.
		return refused, false, nil
	}

	// One question, two situations, and the wording says which it is rather
	// than flattening them. Claiming an address for a name that resolved to
	// nothing would be inventing the very fact the user is being asked about.
	text := fmt.Sprintf("Quire can't look up %s on this network, so it can't tell where it would be connecting. "+
		"Is this a service on your own network that you run? If it is, Quire can reach it through a proxy "+
		"— the proxy looks the name up instead.", host)
	if known {
		text = fmt.Sprintf("%s resolves to %s, which is a private address. Quire doesn't connect to addresses like that, "+
			"because a site it scrapes could use one to reach something on your network. "+
			"Is this a service on your own network that you run?", host, addr)
	}

	ans, err := r.ui.Ask(ctx, Question{
		Kind: "selfhosted",
		Text: text,
		// "No" first, and it is the answer an empty reply means: the safe
		// direction here is the refusal that was already in force.
		Options: []Option{
			{ID: "cancel", Label: "No, stop"},
			{ID: "continue", Label: "Yes, it's mine — add it"},
		},
		// Offered here rather than afterwards because a user adding a host on
		// their own network is exactly the person whose device may not be able
		// to reach it directly (see theme.Source.Proxy for the measurement).
		// When the name did not resolve at all it is not a convenience but the
		// only way through, and the label says so.
		Input: r.proxyInput(known),
	})
	if err != nil {
		return Result{}, false, err
	}
	if ans.ID != "continue" {
		// The refusal that was already in force stays in force, *as itself*: the
		// same verdict and the same sentence the probe would have ended with
		// before the offer existed.
		//
		// Not "Stopped", which is what declining the redirect question gives.
		// That one is a question about which site to add and nothing has been
		// judged when it is declined; this one is an offer to override a
		// judgement already made, so declining leaves the judgement — and with
		// it the wizard's "Edit the address", which is what a user who simply
		// mistyped a name needs next.
		return refused, false, nil
	}

	if proxy := strings.TrimSpace(ans.Text); proxy != "" {
		pu, err := fetch.ParseProxyURL(proxy)
		if err != nil {
			// Refused now, in the words of the field it was typed into, rather
			// than carried into a request that would fail as a timeout.
			return r.result(theme.VerdictInvalidURL,
				fmt.Sprintf("Quire can't use that proxy: %v. A proxy is just a host and port, like http://localhost:1055.", err)), false, nil
		}
		r.proxy = pu.String()
	}

	if !known {
		// Nothing resolved, so there is no address to record and none is
		// invented. Without a proxy there is also no way to reach the site:
		// saying yes does not make a name resolvable, and the honest answer is
		// the lookup failure with the one thing that would fix it named.
		if r.proxy == "" {
			return r.result(theme.VerdictInvalidURL,
				fmt.Sprintf("Quire still can't look up %s. If it's only reachable through a proxy, type the proxy "+
					"when Quire asks — without one there is no way to reach it from here.", host)), false, nil
		}
		r.selfHosted = &theme.SelfHosted{ViaProxy: true, ConfirmedAt: r.p.now().UTC()}
		return Result{}, true, nil
	}
	r.selfHosted = &theme.SelfHosted{ConfirmedAddr: addr.String(), ConfirmedAt: r.p.now().UTC()}
	return Result{}, true, nil
}

// offerableAddr asks the guard which address the host resolved to, for the
// refusal that is about an address. It answers for no other refusal, and a
// guard that cannot say makes no offer — silence is the old behaviour.
func (r *run) offerableAddr(ctx context.Context, u *url.URL, refusal error) (netip.Addr, bool) {
	var ge *fetch.GuardError
	if !errors.As(refusal, &ge) || !errors.Is(ge.Kind, fetch.ErrBlockedAddress) {
		return netip.Addr{}, false
	}
	g, ok := r.p.guard.(SelfHostableGuard)
	if !ok {
		return netip.Addr{}, false
	}
	return g.SelfHostableTarget(ctx, u)
}

// unresolvedHost reports the refusal that is not a judgement about the site at
// all: the name could not be looked up here.
//
// It gets the same question because on this device it is at least as likely to
// mean "a service on my own network": a mesh VPN's names resolve only inside
// the mesh, and the tablet's resolver is public DNS (measured 2026-09-20). A
// mistyped domain lands here too, which is why the default is still no and the
// question still says plainly that Quire could not look the name up.
func unresolvedHost(err error) bool {
	var ge *fetch.GuardError
	return errors.As(err, &ge) && ge.Unresolved
}

// proxyInput is the question's field. Its label changes with the situation
// because its importance does: with a private address a proxy is optional, and
// with a name that does not resolve it is the whole of the way through.
func (r *run) proxyInput(addrKnown bool) *Input {
	if addrKnown {
		return &Input{
			Label:       "If Quire has to go through a proxy to reach it, type the proxy here. Leave it empty otherwise.",
			Placeholder: "http://localhost:1055",
		}
	}
	return &Input{
		Label:       "Type the proxy Quire should reach it through. Without one, Quire can't look this name up at all.",
		Placeholder: "http://localhost:1055",
	}
}

// schemeLike matches the "scheme:" of RFC 3986, and schemeSlashes the same
// token with the colon left out — the shape `https//example.com` has, which is
// a typo rather than a host.
var (
	schemeLike    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)
	schemeSlashes = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*//`)
)

// normaliseScheme is the "normalise" half of PLAN §7.5 stage 1. It returns the
// address to parse, whether Quire supplied the scheme itself, and — when the
// input is a near-miss rather than an address — the plain-language reason to
// refuse it.
//
// Typing on an e-ink display is slow, so a bare weebcentral.com is treated as
// what the user plainly meant rather than thrown back at them. Two rules keep
// that from becoming a guessing game:
//
//   - The scheme is only ever filled in as https. A failure is reported as a
//     failure (see reachError), never retried over plain http — silently
//     downgrading somebody to an unencrypted connection is not a convenience.
//   - Anything that *carries* a scheme keeps it untouched, so a typed http://
//     is respected and an ftp://, file:// or javascript: still reaches the
//     scheme check in the guard and is refused there.
//
// A scheme that is *nearly* right is refused with the typo named, because
// https:/example.com could be read as either a missing slash or a host called
// "https:", and choosing between them for the user is exactly the guessing this
// avoids.
func normaliseScheme(raw string) (out string, assumedHTTPS bool, problem string) {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw, false, ""
	}
	if loc := schemeLike.FindStringIndex(raw); loc != nil && !hasPortAfterColon(raw) {
		scheme := strings.ToLower(raw[:loc[1]-1])
		if scheme == "http" || scheme == "https" {
			typed := raw[:loc[1]] + strings.Repeat("/", len(raw[loc[1]:])-len(strings.TrimLeft(raw[loc[1]:], "/")))
			return "", false, fmt.Sprintf(
				"%q isn't quite right — it should be %q. Try again, or leave the scheme off and type just the site, like weebcentral.com.",
				typed, scheme+"://")
		}
		// Some other scheme, typo or not. Keep it: the guard names it.
		return raw, false, ""
	}
	if schemeSlashes.MatchString(raw) {
		return "", false, "That address is missing its \":\" — a web address starts https:// or http://, or you can leave the scheme off and type just the site, like weebcentral.com."
	}
	return "https://" + raw, true, ""
}

// hasPortAfterColon reports whether raw's first colon is followed only by
// digits, which makes it example.com:8080 rather than a scheme.
func hasPortAfterColon(raw string) bool {
	i := strings.IndexByte(raw, ':')
	if i < 0 {
		return false
	}
	rest := raw[i+1:]
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		rest = rest[:j]
	}
	if rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
	pol := r.policy(base)
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
		//
		// **Including the two things the user asserted about the host they
		// typed.** A confirmation that this is a service on their own network,
		// and a proxy to reach it through, were both given for the host that has
		// just been replaced; carrying them to a host the *site* chose would
		// hand content the exemption, which is the one thing none of this may
		// do. They are dropped, and whatever the new host needs it can be asked
		// for again.
		if base.Host != final.Host && (r.selfHosted != nil || r.proxy != "") {
			r.selfHosted, r.proxy = nil, ""
		}
		base.Scheme, base.Host = final.Scheme, final.Host
	}

	if r.page.Status >= 400 {
		// A 403 or 503 is the usual shape of a challenge, so stage 3 gets to
		// look at this body before we call the site unreachable.
		if res, blocked := r.stageChallenge(); blocked {
			return res, false, nil
		}
		// Plain language, not an error code (PLAN §6 M3). Stage 3 has already
		// had its look, so whatever this is, it is not a challenge.
		//
		// 404 is the one status that means something different here than it
		// does later: no theme has been chosen yet, so this is the address the
		// *user* typed not existing, not a path one of our themes expected.
		detail := "Quire couldn't read this site: " + fetch.StatusSentence("the site", r.page.Status) + "."
		if r.page.Status == 404 {
			detail = "There's nothing at that address — the site answered HTTP 404. Check the address, or try the site's home page."
		}
		return r.result(theme.VerdictUnreachable, detail), false, nil
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
	detail := fmt.Sprintf("Quire couldn't reach %s: %v", base.Hostname(), err)
	// Stage 1 filled in the scheme, so the guess is part of why this failed and
	// the user is owed it. Quire does not retry over plain http by itself: an
	// unencrypted connection is the user's decision to make, not a fallback to
	// slip past them.
	if r.assumedHTTPS {
		detail += fmt.Sprintf(" You didn't type a scheme, so Quire assumed https://. If this site only works without encryption, type http://%s and try again.", base.Host)
	}
	return r.result(theme.VerdictUnreachable, detail)
}

// stageFingerprint is PLAN §7.5 stage 4. A nil theme with a nil error means the
// Result is final.
func (r *run) stageFingerprint(ctx context.Context) (theme.Theme, Result, error) {
	scores := r.fingerprint()
	if len(scores) == 0 || scores[0].Score < r.p.threshold {
		r.dropGateWarnings()
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
		r.dropGateWarnings()
		return nil, r.result(theme.VerdictUnrecognised, r.unrecognisedDetail()), nil
	}
	return th, Result{}, nil
}

// dropGateWarnings discards stage 3's non-blocking findings when stage 4
// recognised nothing.
//
// Added 2026-09-16, from a real complaint. A site was told "the first thing
// this site shows is a sign-in form" — on a page that had just handed Quire 158
// series links. The warning came from two password inputs in a WordPress nav,
// and it fired because its second branch is gated on `noThemeMatch()`, which
// was true only because the fingerprint was broken. The branch was not itself
// wrong; it was downstream of a different bug.
//
// But the presentation was wrong regardless of that bug. When no theme matched,
// "we didn't recognise this site, here is what we tried" is the whole story,
// and a login-wall warning beside it is noise that points the user at a
// question that is not the one they need to answer. Warnings describe what a
// *working* source will be like; there is no source here to describe.
//
// This is a presentation rule, not a detection change: nothing in stage 3 is
// relaxed, and a challenge is still terminal whatever stage 4 would have said.
func (r *run) dropGateWarnings() { r.warnings = nil }

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
func (r *run) draft(base *url.URL, th theme.Theme) *theme.Source {
	// PLAN §7.5 stage 6: a theme that knows its site's name says so, and that
	// wins over the page title.
	//
	// The title is the right default for a family of independent sites and
	// wrong for a single one: adding https://api.mangadex.org produced a
	// source called "MangaDex API documentation", which is exactly what that
	// page's <title> says and means nothing to a user browsing their sources.
	//
	// Note what is deliberately absent — any attempt to *clean* the title.
	// Stripping " API documentation" here would mangle the next site whose
	// real name ends that way. A theme either knows the name or has no
	// opinion; anything else is the user's rename to make.
	name := th.SuggestedName()
	if name == "" && r.page != nil {
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
		Theme:   th.ID(),
		BaseURL: strings.TrimSuffix(base.String(), "/"),
		// PLAN §7.2/§7.5 stage 6: seed the source's allowedHosts from what the
		// theme declares it needs, so a theme whose images live on a separate
		// registrable domain works on the first run.
		//
		// Seeded here rather than in stageAccept for two reasons. The draft is
		// what stage 5 exercises the theme against, so the capability check
		// runs under the same policy the stored source will have — otherwise
		// stage 5 could pass on a policy nobody ends up using. And it is
		// *copied onto the source*, not read back from the theme at request
		// time, so the list is visible to the user, editable by them, and
		// unchanged if a later version of the theme changes its mind.
		AllowedHosts: allowedHosts(th),
		// What the user answered in stage 1, carried onto the draft for the same
		// reason allowedHosts is: stage 5 exercises the theme against this
		// source, so the capability check has to run under the policy the stored
		// source will have. A confirmed private host that stage 5 probed without
		// the confirmation would fail every request and report the site as
		// unreadable.
		//
		// The draft is the service's to persist, and it does not persist these
		// two as they stand: state.Store.ConfirmSelfHosted and
		// state.Store.SetProxy are the only writers of them, and the add flow
		// goes through both. See service.confirmAdd.
		SelfHosted: r.selfHosted,
		Proxy:      r.proxy,
		AddedAt:    r.p.now().UTC(),
	}
}

// allowedHosts copies a theme's declared hosts, defensively: the theme owns
// that slice and a source entry that aliased it would let an edit to one show
// up in the other.
func allowedHosts(th theme.Theme) []string {
	declared := th.AllowedHosts()
	if len(declared) == 0 {
		return nil
	}
	return append([]string(nil), declared...)
}

// stageAccept is PLAN §7.5 stage 6, plus the refusals stage 5 can produce.
func (r *run) stageAccept(draft *theme.Source, themeID string, cap capability) Result {
	var res Result
	switch {
	case cap.Challenge != nil:
		// PLAN §7.6, reached from stage 5 rather than stage 3: the site said no
		// somewhere other than its front door. It is refused on the same terms
		// — no degraded add, no retry prompt, no workaround — and the sentence
		// says *where*, because a challenge reported after a search that
		// worked is otherwise baffling.
		where := "the image host it serves its pages from"
		if cap.ImageHost != "" {
			where = cap.ImageHost + ", where it serves its pages from,"
		}
		res = r.result(theme.VerdictBlockedChallenge,
			"Quire could search this site, but "+where+" requires a browser challenge Quire can't pass ("+
				cap.Challenge.Detail+"). Quire doesn't work around challenges, so this site can't be added.")
	case cap.ok():
		res = r.result(theme.VerdictOK, fmt.Sprintf("Quire read this site as a %s site and searched it successfully.", themeID))
		res.Addable = true
	case cap.addableDegraded():
		res = r.result(theme.VerdictPartial, "Quire can search this site and list chapters, but "+cap.failure()+
			" You can add it, but expect gaps.")
		res.Addable = true
	case cap.addableEmptyReleases():
		// Part B (2026-09-20): the strong check answered directly — this is a
		// positive identification of the application itself, stronger evidence
		// than a markup fingerprint — so an empty result from a handful of
		// books is a fact about those books, not about the source. Refusing
		// here is the false refusal this case exists to stop.
		res = r.result(theme.VerdictPartial, "Quire found this site and confirmed it really is the application it "+
			"looks like. But none of the books it tried had a downloadable epub or pdf. Whether one does depends "+
			"on the sources this instance is set up to search, not on Quire's connection to it. You can add it, "+
			"but it may find nothing to download until sources are configured for the books you want.")
		res.Addable = true
	case cap.addableEmptySearch():
		// 2026-09-20: measured against a live Shelfmark instance, whose
		// metadata provider (openlibrary) intermittently answers every query
		// with zero results for a window and then recovers. The strong check
		// already identified the site as the real application — a request
		// only it could have answered came back right — so an empty search
		// right now is a fact about that provider's own upstream, not about
		// whether Quire can talk to the instance.
		res = r.result(theme.VerdictPartial, "Quire found this site and confirmed it really is the application it "+
			"looks like. But it returned no results at all just now. This can happen when the source's own "+
			"upstream book listing is temporarily empty. You can add it, but it won't find anything until that "+
			"comes back.")
		res.Addable = true
	default:
		// PLAN §7.5: if page extraction fails the source is useless, so refuse.
		// What "page extraction" means depends on what the source serves — see
		// capability.uselessWithout.
		res = r.result(theme.VerdictPartial, "Quire recognised this site but "+cap.failure()+
			" "+cap.uselessWithout())
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
