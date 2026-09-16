package prober

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// PLAN §7.5 stage 3 — challenge and gate detection.
//
// This is a *hard* gate. If any signal below fires, the verdict is
// blocked_challenge and the source is refused: not added in a degraded state,
// no retry offered, no workaround suggested. PLAN §7.6 gives both reasons — a
// challenge is a site operator saying no and we take the answer, and the device
// has no browser engine, so there would be no honest implementation even if we
// wanted one. There is deliberately no option, override or environment variable
// that skips this function.
//
// **What is and is not verified.** PLAN §7.5 asks each signal to be checked
// against a live example. PLAN §1.3 forbids this repository from naming or
// bundling an aggregator, and the developer cannot hit one from CI, so the
// signals here are implemented from *structural* facts — a vendor's own
// documented endpoint paths, header names and status codes — and the fixtures
// that exercise them are synthetic, at example.invalid, like every other
// fixture in this repo. docs/THEME-NOTES.md records, signal by signal, which
// ones rest on a published protocol fact and which are inference. Do not read a
// green test run as proof that a live challenge page is caught.

// challengeSignal is one reason to refuse a site.
type challengeSignal struct {
	// Name is short and internal; Detail is what the user reads.
	Name   string
	Detail string
}

// conclusiveBodyMarkers are strings that exist *only* on an interstitial, and
// are therefore allowed to fire alone.
//
// CORRECTED 2026-09-16 — this list used to be twice as long, and the premise
// written above it ("a site that is merely behind a CDN does not serve these,
// only one actively interstitialling does") was **false for half its entries**.
// It cost a user a site: a real comic site answered 200 with 168 KB of markup
// and 71 series links, carrying one `/cdn-cgi/challenge-platform/` script tag —
// which Cloudflare injects on *ordinary served pages* when bot management is
// on — and Quire refused it as `blocked_challenge`, which is terminal, so the
// capability check never ran. A false refusal is as much a lie as a false `ok`.
//
// The governing rule, which is what the split below encodes:
//
//	**A response that served us real content is not a challenge, whatever
//	markers it carries.** An interstitial's whole purpose is to withhold
//	content. If we got the content, we were not interstitialled.
//
// Each entry here is justified against that rule individually, in the comment
// beside it. A marker that cannot be justified as interstitial-only belongs in
// corroboratedBodyMarkers, not here — when in doubt, demote it: the cost of a
// demotion is that one challenge shape needs a status code to be caught, and
// the cost of a wrong promotion is a working site the user cannot add.
//
// Matched through Page.Contains, which strips HTML comments first — a marker in
// a comment is not evidence (see probe.Page).
var conclusiveBodyMarkers = []struct {
	marker string
	vendor string
}{
	// The challenge page's own options blob (`window._cf_chl_opt = {...}`).
	// It configures the widget; there is nothing for it to configure on a page
	// that is not one.
	{"__cf_chl_", "a Cloudflare browser challenge"},
	// The class the challenge page puts on <body> while it runs.
	{"cf-challenge-running", "a Cloudflare browser challenge"},
	// DDoS-Guard's challenge document itself. Note that its *ordinary* script
	// (check.ddos-guard.net) is not here: that one is injected on served pages
	// and was one of the wrong entries.
	{"/ddos-guard/js-challenge", "a DDoS-Guard browser challenge"},
	// Sucuri's challenge loader, which exists to reload the page once solved.
	{"sucuri_cloudproxy_js", "a Sucuri browser challenge"},
	// Incapsula's resource endpoint with the challenge query parameters. The
	// bare path is used for ordinary instrumentation, so the parameters are
	// load-bearing and must stay in the string.
	{"/_incapsula_resource?swcgh", "an Imperva/Incapsula browser challenge"},
	{"_incapsula_resource?swjsv", "an Imperva/Incapsula browser challenge"},
}

// corroboratedBodyMarkers are strings that appear on challenge pages **and on
// ordinary served pages**. They fire only alongside a refusal status or a
// response with nothing recognisable in it — the same shape challengeCookies
// has always used, and correctly.
//
// This is not a weaker version of the list above; it is the honest home for
// every marker whose presence proves a vendor is *involved*, not that content
// was *withheld*.
var corroboratedBodyMarkers = []struct {
	marker string
	vendor string
}{
	// Injected on served pages whenever a site turns on bot management. This
	// is the exact string that cost us a working site.
	{"/cdn-cgi/challenge-platform/", "a Cloudflare browser challenge"},
	// Same script family, same problem.
	{"challenge-platform/h/b/orchestrate", "a Cloudflare browser challenge"},
	// Turnstile is routinely embedded in a login or comment form on a page
	// that is otherwise served in full. A widget on a page is not a gate in
	// front of it.
	{"challenges.cloudflare.com/turnstile", "a Cloudflare Turnstile gate"},
	// A CAPTCHA endpoint a page *references* may equally be one a form posts
	// to. Only a refusal makes it a gate.
	{"/.well-known/captcha/", "a CAPTCHA gate"},
	// DDoS-Guard's ordinary client script, present on pages it serves normally.
	{"check.ddos-guard.net", "a DDoS-Guard browser challenge"},
}

// titleChallengeMarkers are interstitial <title> strings. A title is a much
// weaker signal than an asset path — a page can legitimately be called almost
// anything — so these only fire alongside a CDN-managed status (403/503) or a
// vendor header.
var titleChallengeMarkers = []string{
	"just a moment",
	"attention required",
	"checking your browser",
	"please wait while we verify",
	"verifying you are human",
	"ddos-guard",
	"access denied",
	"security check",
}

// edgeHeaders are headers that identify a CDN-managed edge. Their presence is
// not itself a challenge — most of the web sits behind one of these — so they
// are only ever used as the second half of a combined signal.
var edgeHeaders = []struct {
	name   string
	value  string // required substring, empty means "any value"
	vendor string
}{
	{"cf-ray", "", "Cloudflare"},
	{"cf-mitigated", "", "Cloudflare"},
	{"server", "cloudflare", "Cloudflare"},
	{"server", "ddos-guard", "DDoS-Guard"},
	{"server", "sucuri/cloudproxy", "Sucuri"},
	{"x-sucuri-id", "", "Sucuri"},
	{"x-iinfo", "", "Imperva/Incapsula"},
	{"x-datadome", "", "DataDome"},
	{"x-datadome-cid", "", "DataDome"},
}

// challengeCookies are cookies whose *purpose* is to carry proof that a
// challenge was solved. Seeing one being set on a response we never solved
// means content is gated behind one.
//
// These only fire alongside a refusal status or a page with no recognisable
// content, because a long-lived clearance cookie can also be re-issued on an
// ordinary page view, and refusing a site that served us its real content would
// be the wrong kind of mistake.
var challengeCookies = []struct {
	name   string
	vendor string
}{
	{"cf_clearance", "Cloudflare"},
	{"__ddg", "DDoS-Guard"},
	{"sucuri_cloudproxy_uuid", "Sucuri"},
	{"incap_ses_", "Imperva/Incapsula"},
	{"visid_incap_", "Imperva/Incapsula"},
	{"datadome", "DataDome"},
}

// genericGateMaxBody is how small a 200 OK body has to be before "there is
// nothing here but a script" becomes a fair reading. A real listing page of any
// theme we support is tens of KiB; a JS gate is a few hundred bytes plus a
// loader.
const genericGateMaxBody = 6 << 10

// stageChallenge runs stage 3. The bool is true when the source is refused.
//
// It also collects the non-blocking warnings PLAN §7.5 asks for — login wall,
// paywall, age gate — which are surfaced and left to the user.
func (r *run) stageChallenge() (Result, bool) {
	if r.page == nil {
		return Result{}, false
	}
	r.warnings = append(r.warnings, r.gateWarnings()...)

	if sig, found := r.challengeSignal(); found {
		// One sentence, complete and final. PLAN §6 M3: "This site requires a
		// browser challenge that Quire can't pass" is a whole answer, not a
		// retry prompt, so the text never suggests trying again or changing a
		// setting.
		detail := "This site requires a browser challenge that Quire can't pass (" + sig.Detail + "). " +
			"Quire doesn't work around challenges, so this site can't be added."
		return r.result(theme.VerdictBlockedChallenge, detail), true
	}
	return Result{}, false
}

// challengeSignal reports the first signal that fires on the probed home page.
func (r *run) challengeSignal() (challengeSignal, bool) {
	return r.challengeSignalFor(r.page, true)
}

// challengeSignalFor reports the first signal that fires on p.
//
// The page is a parameter because stage 5 asks the same question of a second
// response — the page image it fetches, which may come from an image host that
// challenges even though the site itself did not (PLAN §7.5 stage 5, corrected
// 2026-09-16). The signals are the site's, not the page's: a vendor's own
// interstitial markup, its mitigation header, a refusal from a managed edge, a
// challenge redirect, a clearance cookie.
//
// genericGate is false for anything but the home page. The generic JS-gate
// signal asks "200, tiny, no theme recognises it, and a client-side render
// placeholder" — a question that only makes sense about a *document*. Asking it
// of an image response would make every small non-image body a challenge, which
// is exactly the false terminal verdict §7.6 must not produce.
func (r *run) challengeSignalFor(p *probe.Page, genericGate bool) (challengeSignal, bool) {
	if p == nil {
		return challengeSignal{}, false
	}
	title := strings.ToLower(theme.Collapse(p.Title()))
	refusal := p.Status == http.StatusForbidden || p.Status == http.StatusServiceUnavailable ||
		p.Status == http.StatusTooManyRequests

	// corroborated is the answer to "is there any reason to read a marker on
	// this response as an interstitial rather than as a script on a page that
	// was served to us?"
	//
	// Two things can supply it: the site refused us, or there is nothing here
	// that any theme recognises and barely a page at all. A 200 carrying real
	// content supplies neither, which is the governing rule of this file (see
	// conclusiveBodyMarkers) expressed as one boolean.
	//
	// The content half is asked only of the probed document. A non-document
	// response — stage 5's page image — has no theme to fingerprint and no
	// markup to be missing, so for it only a refusal corroborates anything.
	corroborated := refusal || (genericGate && r.noRecognisableContent())

	// 1. A vendor's own challenge asset or script, of the kind that exists only
	//    on an interstitial. Conclusive on its own — see the list for the audit
	//    behind that claim, and for what happened when it was assumed rather
	//    than audited.
	for _, m := range conclusiveBodyMarkers {
		if p.Contains(m.marker) {
			return challengeSignal{Name: "body-marker", Detail: m.vendor}, true
		}
	}

	// 1b. Markers that also appear on served pages. These need corroboration,
	//     and without it their presence means only that a vendor is involved —
	//     which is true of much of the web and is not a reason to refuse a
	//     site that just handed us its content.
	if corroborated {
		for _, m := range corroboratedBodyMarkers {
			if p.Contains(m.marker) {
				return challengeSignal{Name: "body-marker-corroborated", Detail: m.vendor}, true
			}
		}
	}

	// 2. Cloudflare states the mitigation in a header of its own. Nothing else
	//    sends cf-mitigated, and "challenge" is its only interesting value.
	if strings.Contains(strings.ToLower(p.Header.Get("cf-mitigated")), "challenge") {
		return challengeSignal{Name: "cf-mitigated", Detail: "a Cloudflare browser challenge"}, true
	}

	vendor, edge := edgeVendor(p)

	// 3. A refusal status from a CDN-managed edge, with an interstitial title.
	//    Either half alone is ordinary; together they are the classic shape.
	if refusal && edge {
		for _, t := range titleChallengeMarkers {
			if strings.Contains(title, t) {
				return challengeSignal{Name: "edge-refusal-title", Detail: "a " + vendor + " challenge page"}, true
			}
		}
		if p.Status == http.StatusServiceUnavailable {
			return challengeSignal{Name: "edge-503", Detail: "a " + vendor + " interstitial"}, true
		}
	}

	// 4. A meta refresh pointing at a challenge endpoint.
	//
	//    Corroboration required here too, for the same reason as 1b and found
	//    by the same audit: a served page may carry a refresh to almost
	//    anything, and an interstitial that redirects is a 403 or a body with
	//    nothing in it. Requiring it costs no true positive we know of.
	if dest, ok := metaRefreshTarget(p); ok && corroborated {
		d := strings.ToLower(dest)
		for _, frag := range []string{"/cdn-cgi/", "challenge", "captcha", "__ddg", "_incapsula_resource"} {
			if strings.Contains(d, frag) {
				return challengeSignal{Name: "meta-refresh", Detail: "an automatic redirect to a challenge page"}, true
			}
		}
	}

	// 5. A clearance cookie being issued where we can see no content.
	if corroborated {
		for _, c := range setCookieNames(p.Header) {
			for _, want := range challengeCookies {
				if strings.HasPrefix(c, want.name) {
					return challengeSignal{Name: "clearance-cookie", Detail: "a " + want.vendor + " clearance cookie, which only a solved challenge produces"}, true
				}
			}
		}
	}

	// 6. The generic JS gate: 200 OK, almost no markup, nothing any theme
	//    recognises, and a <noscript> or an empty mount point where the content
	//    should be. This is the signal that catches a vendor we have never
	//    heard of, at the cost of being the least certain — which is why it
	//    needs all four halves.
	if genericGate && p.Status == http.StatusOK && len(p.Body) < genericGateMaxBody && r.noThemeMatch() {
		if why, ok := clientSideOnly(p); ok {
			return challengeSignal{Name: "js-gate", Detail: "a page that only renders in a browser — " + why}, true
		}
	}

	return challengeSignal{}, false
}

// edgeVendor reports whether the response came through a CDN-managed edge, and
// which one.
func edgeVendor(p *probe.Page) (string, bool) {
	for _, h := range edgeHeaders {
		v := strings.ToLower(p.Header.Get(h.name))
		if v == "" {
			continue
		}
		if h.value == "" || strings.Contains(v, h.value) {
			return h.vendor, true
		}
	}
	return "", false
}

// noThemeMatch reports whether stage 4's fan-out would come up empty. Stage 3
// runs first but needs the answer, so the scores are computed once here and
// reused (see run.fingerprint).
func (r *run) noThemeMatch() bool {
	scores := r.fingerprint()
	return len(scores) == 0 || scores[0].Score < r.p.threshold
}

// noRecognisableContent is noThemeMatch plus "and there is barely a page here".
func (r *run) noRecognisableContent() bool {
	return len(r.page.Body) < genericGateMaxBody && r.noThemeMatch()
}

// setCookieNames returns the cookie names a response sets, lowercased.
func setCookieNames(h http.Header) []string {
	var names []string
	for _, sc := range h.Values("Set-Cookie") {
		name, _, ok := strings.Cut(sc, "=")
		if !ok {
			continue
		}
		names = append(names, strings.ToLower(strings.TrimSpace(name)))
	}
	return names
}

// metaRefreshTarget returns the URL of a <meta http-equiv="refresh">.
func metaRefreshTarget(p *probe.Page) (string, bool) {
	doc, err := p.Document()
	if err != nil {
		return "", false
	}
	content := ""
	doc.Find("meta").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if strings.EqualFold(strings.TrimSpace(s.AttrOr("http-equiv", "")), "refresh") {
			content = s.AttrOr("content", "")
			return false
		}
		return true
	})
	if content == "" {
		return "", false
	}
	_, after, ok := strings.Cut(strings.ToLower(content), "url=")
	if !ok {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(after), `"'`), true
}

// clientSideOnly reports whether the page is a shell with nothing in it, and
// says which shape it is.
func clientSideOnly(p *probe.Page) (string, bool) {
	doc, err := p.Document()
	if err != nil {
		return "", false
	}
	// Visible text, ignoring script and style, is what a reader would see.
	body := doc.Find("body").Clone()
	body.Find("script, style, noscript").Remove()
	visible := theme.Collapse(body.Text())

	if doc.Find("noscript").Length() > 0 && len(visible) < 200 {
		return "it asks for JavaScript and shows nothing without it", true
	}
	for _, sel := range []string{"#root:empty", "#app:empty", "#__next:empty", "div[data-server-rendered=false]:empty"} {
		if doc.Find(sel).Length() > 0 {
			return "its content area is empty until a script fills it", true
		}
	}
	if len(visible) < 60 && doc.Find("script").Length() > 0 {
		return "it contains scripts and almost no text", true
	}
	return "", false
}

// gateWarnings are PLAN §7.5's *non-blocking* findings: a login wall, paywall
// or age gate the theme cannot satisfy. These are surfaced and the user
// decides — the opposite of a challenge, which is refused outright. Keeping the
// two apart is the whole point: overreporting a paywall as a block would make
// Quire refuse sites it could read.
func (r *run) gateWarnings() []string {
	p := r.page
	doc, err := p.Document()
	if err != nil {
		return nil
	}
	var out []string

	passwords := doc.Find(`input[type="password"]`).Length()
	if passwords > 0 && p.Status >= 400 {
		out = append(out, "This site asked Quire to sign in before showing anything. Quire has no account for it, so some pages may be empty.")
	} else if passwords > 0 && r.noThemeMatch() {
		out = append(out, "The first thing this site shows is a sign-in form. If its listings need an account, Quire won't see them.")
	}

	for _, m := range []struct{ marker, warn string }{
		{"age verification", "This site has an age gate. Quire can't answer it, so some pages may be empty."},
		{"are you over 18", "This site has an age gate. Quire can't answer it, so some pages may be empty."},
		{"confirm your age", "This site has an age gate. Quire can't answer it, so some pages may be empty."},
		{"subscribe to continue reading", "Parts of this site are behind a paywall. Quire will only see what's free."},
		{"subscribers only", "Parts of this site are behind a paywall. Quire will only see what's free."},
		{"members only", "Parts of this site are behind a paywall. Quire will only see what's free."},
	} {
		if p.Contains(m.marker) && !containsString(out, m.warn) {
			out = append(out, m.warn)
		}
	}
	return out
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// DescribeSignals is used by the tests and by docs/THEME-NOTES.md's table to
// keep the two in step: every marker we ship is listed somewhere a human reads.
func DescribeSignals() []string {
	out := make([]string, 0, len(conclusiveBodyMarkers)+len(corroboratedBodyMarkers))
	for _, m := range conclusiveBodyMarkers {
		out = append(out, fmt.Sprintf("%s → %s", m.marker, m.vendor))
	}
	for _, m := range corroboratedBodyMarkers {
		out = append(out, fmt.Sprintf("%s → %s", m.marker, m.vendor))
	}
	return out
}
