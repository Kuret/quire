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

// bodyChallengeMarkers are strings that only appear on a challenge page. Each
// is a vendor's own asset path or script identifier: a site that is merely
// *behind* a CDN does not serve these, only one actively interstitialling does.
//
// Matched through Page.Contains, which strips HTML comments first — a marker in
// a comment is not evidence (see probe.Page).
var bodyChallengeMarkers = []struct {
	marker string
	vendor string
}{
	{"/cdn-cgi/challenge-platform/", "a Cloudflare browser challenge"},
	{"__cf_chl_", "a Cloudflare browser challenge"},
	{"cf-challenge-running", "a Cloudflare browser challenge"},
	{"challenge-platform/h/b/orchestrate", "a Cloudflare browser challenge"},
	{"/_incapsula_resource?swcgh", "an Imperva/Incapsula browser challenge"},
	{"_incapsula_resource?swjsv", "an Imperva/Incapsula browser challenge"},
	{"sucuri_cloudproxy_js", "a Sucuri browser challenge"},
	{"/ddos-guard/js-challenge", "a DDoS-Guard browser challenge"},
	{"check.ddos-guard.net", "a DDoS-Guard browser challenge"},
	{"/.well-known/captcha/", "a CAPTCHA gate"},
	{"challenges.cloudflare.com/turnstile", "a Cloudflare Turnstile gate"},
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

	// 1. A vendor's own challenge asset or script. Conclusive on its own: this
	//    markup is only emitted by the interstitial itself.
	for _, m := range bodyChallengeMarkers {
		if p.Contains(m.marker) {
			return challengeSignal{Name: "body-marker", Detail: m.vendor}, true
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
	if dest, ok := metaRefreshTarget(p); ok {
		d := strings.ToLower(dest)
		for _, frag := range []string{"/cdn-cgi/", "challenge", "captcha", "__ddg", "_incapsula_resource"} {
			if strings.Contains(d, frag) {
				return challengeSignal{Name: "meta-refresh", Detail: "an automatic redirect to a challenge page"}, true
			}
		}
	}

	// 5. A clearance cookie being issued where we can see no content.
	if refusal || (genericGate && r.noRecognisableContent()) {
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
	out := make([]string, 0, len(bodyChallengeMarkers))
	for _, m := range bodyChallengeMarkers {
		out = append(out, fmt.Sprintf("%s → %s", m.marker, m.vendor))
	}
	return out
}
