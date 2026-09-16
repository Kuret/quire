package prober_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// The false-refusal regression of 2026-09-16.
//
// A real comic site answered 200 with a large body full of series links and one
// `/cdn-cgi/challenge-platform/` script tag — the script Cloudflare injects on
// *served* pages under bot management. Stage 3's body markers fired on it
// unconditionally, the verdict was `blocked_challenge`, and because that is
// terminal the capability check never ran and the user could not add a site
// that had just handed over its front page.
//
// The rule these tests hold in place: **a response that served us real content
// is not a challenge, whatever markers it carries.** An interstitial's whole
// purpose is to withhold content; if we got the content, we were not
// interstitialled.
//
// They are paired on purpose with TestVerdictBlockedChallenge, which must stay
// green: the fix is a corroboration requirement, not a softening.
func TestServedPageCarryingAChallengeScriptIsNotRefused(t *testing.T) {
	routes := madaraRoutes()
	routes["GET /"] = themetest.Route{
		File: "home-served-with-challenge-script.html",
		// Behind Cloudflare, as the real site is. The edge headers are not the
		// bug and must not become one: most of the web sits behind a CDN.
		Header: http.Header{
			"Server": []string{"cloudflare"},
			"Cf-Ray": []string{"0000000000000000-AMS"},
		},
	}

	res := run(t, routes, &recordUI{})

	if res.Verdict == theme.VerdictBlockedChallenge {
		t.Fatalf("a site that served 47 KB of real content was refused as a challenge: %s", res.Detail)
	}
	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
}

// Each corroboration-required marker, on a page that was served in full. None
// of them may refuse it on its own.
func TestCorroborationRequiredMarkersOnAServedPage(t *testing.T) {
	markers := []string{
		"/cdn-cgi/challenge-platform/scripts/jsd/main.js",
		"challenge-platform/h/b/orchestrate/jsch/v1",
		"https://challenges.cloudflare.com/turnstile/v0/api.js",
		"/.well-known/captcha/verify",
		"https://check.ddos-guard.net/check.js",
	}

	for _, marker := range markers {
		t.Run(marker, func(t *testing.T) {
			routes := madaraRoutes()
			routes["GET /"] = themetest.Route{
				Body: servedPageWith(marker),
				Header: http.Header{
					"Server": []string{"cloudflare"},
					"Cf-Ray": []string{"0000000000000000-AMS"},
				},
			}
			res := run(t, routes, &recordUI{})

			if res.Verdict == theme.VerdictBlockedChallenge {
				t.Fatalf("%q alone refused a page that carried its content: %s", marker, res.Detail)
			}
		})
	}
}

// The same markers on a refusal are the classic shape and must still be caught.
// Corroboration is the fix; removing the signal would have been a different and
// worse bug.
func TestCorroborationRequiredMarkersOnARefusalStillRefuse(t *testing.T) {
	markers := []string{
		"/cdn-cgi/challenge-platform/scripts/jsd/main.js",
		"challenge-platform/h/b/orchestrate/jsch/v1",
		"https://challenges.cloudflare.com/turnstile/v0/api.js",
		"/.well-known/captcha/verify",
		"https://check.ddos-guard.net/check.js",
	}

	for _, marker := range markers {
		t.Run(marker, func(t *testing.T) {
			ui := &recordUI{answers: []string{"continue", "continue"}}
			res := run(t, map[string]themetest.Route{
				"GET /": {
					Status: http.StatusForbidden,
					Body: `<html><head><title>example.invalid</title></head><body>` +
						`<script src="` + marker + `"></script></body></html>`,
				},
			}, ui)

			if res.Verdict != theme.VerdictBlockedChallenge {
				t.Fatalf("verdict = %q (%s), want blocked_challenge: a 403 carrying %q is the classic shape",
					res.Verdict, res.Detail, marker)
			}
			if res.Addable || res.Draft != nil {
				t.Fatal("a challenge-protected site must never be addable")
			}
		})
	}
}

// A conclusive marker is one that exists only on an interstitial, and those
// still fire alone — including on a 200, because a challenge page served with a
// cheerful status is still a challenge page.
func TestConclusiveMarkersStillFireAlone(t *testing.T) {
	for _, marker := range []string{
		"window._cf_chl_opt = {}",
		"cf-challenge-running",
		"/ddos-guard/js-challenge",
		"sucuri_cloudproxy_js",
		"/_incapsula_resource?swcgh=1",
		"_incapsula_resource?swjsv=1",
	} {
		t.Run(marker, func(t *testing.T) {
			ui := &recordUI{answers: []string{"continue", "continue"}}
			res := run(t, map[string]themetest.Route{
				"GET /": {Body: `<html><head><title>Example</title></head><body><script>` + marker + `</script></body></html>`},
			}, ui)

			if res.Verdict != theme.VerdictBlockedChallenge {
				t.Fatalf("verdict = %q (%s); %q appears only on an interstitial and must refuse alone",
					res.Verdict, res.Detail, marker)
			}
		})
	}
}

// Every marker Quire ships is either conclusive or corroboration-required, and
// docs/THEME-NOTES.md says which. This asserts the code and the doc agree about
// the *tier*, not merely about the string — the distinction that failed review
// was exactly this one.
func TestEverySignalTierIsDocumented(t *testing.T) {
	b, err := os.ReadFile("../../../docs/THEME-NOTES.md")
	if err != nil {
		t.Fatal(err)
	}
	notes := string(b)
	for _, marker := range []string{
		"__cf_chl_", "cf-challenge-running", "/ddos-guard/js-challenge",
		"sucuri_cloudproxy_js", "_incapsula_resource",
	} {
		if !strings.Contains(notes, marker) {
			t.Errorf("conclusive marker %q is not in docs/THEME-NOTES.md", marker)
		}
	}
	for _, marker := range []string{
		"/cdn-cgi/challenge-platform/", "challenges.cloudflare.com/turnstile",
		"/.well-known/captcha/", "check.ddos-guard.net",
	} {
		if !strings.Contains(notes, marker) {
			t.Errorf("corroboration-required marker %q is not in docs/THEME-NOTES.md", marker)
		}
	}
	if !strings.Contains(notes, "Corroboration required") {
		t.Error("docs/THEME-NOTES.md does not distinguish the two tiers of body marker")
	}
}

// servedPageWith is a full listing page — madara-shaped, so stage 4 recognises
// it — carrying one extra script.
func servedPageWith(marker string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en-GB"><head><meta charset="utf-8">` +
		`<title>Example Reader - Read comics online, free and updated daily</title>` +
		`<link rel="stylesheet" href="https://example.invalid/wp-content/plugins/madara/css/style.css">` +
		`</head><body class="home blog post-type-archive-wp-manga"><div class="c-tabs-item">`)
	for i := 0; i < 30; i++ {
		b.WriteString(`<div class="c-tabs-item__content"><div class="tab-thumb c-image-hover">` +
			`<a href="https://example.invalid/manga/the-lantern-keeper/"><img src="https://example.invalid/wp-content/uploads/a.jpg" alt=""></a>` +
			`</div><div class="tab-summary"><div class="post-title"><h3>` +
			`<a href="https://example.invalid/manga/the-lantern-keeper/">The Lantern Keeper</a></h3></div>` +
			`<div class="summary-content">A standfirst long enough that this page is a page and not a placeholder.</div>` +
			`</div></div>`)
	}
	b.WriteString(`</div><script src="` + marker + `"></script></body></html>`)
	return b.String()
}

// When stage 4 recognises nothing, stage 3's gate warnings are noise that
// misdirects.
//
// From a real complaint: a user was told "the first thing this site shows is a
// sign-in form" about a page that had just handed Quire 158 series links. Two
// password inputs in a WordPress nav triggered it, via the branch that fires
// when no theme matched — which was true only because the madara fingerprint
// was broken. The fingerprint is fixed separately; this pins the presentation,
// because even with a correct fingerprint "we didn't recognise this site" is
// the whole story and a login-wall note beside it points somewhere else.
func TestUnrecognisedSiteCarriesNoGateWarnings(t *testing.T) {
	body := `<!DOCTYPE html><html><head><title>Example</title></head><body>` +
		`<nav><form><input type="text" name="user"><input type="password" name="pass">` +
		`<input type="password" name="pass2"></form></nav>` +
		`<main><p>A site of a shape no theme here knows, with plenty of content on it.</p></main>` +
		`</body></html>`

	res := run(t, map[string]themetest.Route{"GET /": {Body: body}}, &recordUI{})

	if res.Verdict != theme.VerdictUnrecognised {
		t.Fatalf("verdict = %q (%s), want unrecognised", res.Verdict, res.Detail)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unrecognised verdict carried warnings that misdirect: %v", res.Warnings)
	}
	// The honest headline names what was tried, which is the actionable part.
	if !strings.Contains(res.Detail, "compared it against") {
		t.Errorf("detail %q does not name the themes that were tried", res.Detail)
	}
}

// Warnings still reach the user when a theme *did* match, because then they
// describe what their source will be like. Suppression is scoped to the case
// where there is no source to describe.
//
// The marker used here is an age gate rather than the sign-in form, because a
// password input on a page we read in full deliberately warns about nothing:
// that branch needs a refusal status or a page nothing recognised. A login form
// in a site's nav is not a wall, and saying so would be the same misdirection
// in a different place.
func TestRecognisedSiteKeepsItsGateWarnings(t *testing.T) {
	routes := madaraRoutes()
	home, err := os.ReadFile("testdata/home-madara.html")
	if err != nil {
		t.Fatal(err)
	}
	withGate := strings.Replace(string(home), "</body>",
		`<div class="gate"><p>Age verification required before reading.</p></div></body>`, 1)
	routes["GET /"] = themetest.Route{Body: withGate}

	res := run(t, routes, &recordUI{})

	if res.Verdict != theme.VerdictOK {
		t.Fatalf("verdict = %q (%s), want ok", res.Verdict, res.Detail)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("a recognised site lost the age-gate warning that describes what its source will be like")
	}
	if !strings.Contains(strings.ToLower(res.Warnings[0]), "age gate") {
		t.Errorf("warning = %q, want the age gate", res.Warnings[0])
	}
}
