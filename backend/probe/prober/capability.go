package prober

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// PLAN §7.5 stage 5 — the capability check.
//
// This is the stage that separates "looks like Madara" from "works as Madara".
// The fingerprint is structural guesswork on one page; this drives the whole
// path the app will actually use — search, series, chapters, page images — and
// requires all four to produce plausible, non-empty results.
//
// It is also the only verification in the project that touches the site the
// user actually chose. docs/THEME-NOTES.md is blunt about this: the committed
// fixtures pin *our parser*, and prove nothing about any live site. What proves
// a live site works is this stage, at runtime, and M7's re-probe when a working
// source goes quiet.

// capability is the outcome of each step.
//
// CORRECTION 2026-09-16 — there are five steps, not four. §7.5 asked for "one
// page-image extraction", and a real probe showed why that is not the question:
// a site of the mangakakalot family extracted 76 page URLs perfectly and then
// answered **403 with a Cloudflare interstitial** on the image host. Under the
// four-step check that site was accepted as `ok`, and the user found out it was
// useless minutes later when a download failed. A false `ok` is the one thing
// §7.5 and §6 M3 are most emphatic about not producing, and "if page extraction
// fails the source is useless, so refuse" plainly meant *can we get pages*.
// So stage 5 now fetches one.
type capability struct {
	Search   stepResult `json:"search"`
	Series   stepResult `json:"series"`
	Chapters stepResult `json:"chapters"`
	Pages    stepResult `json:"pages"`

	// Image is the fetch of one page image, through the real client: the
	// limiter, the honest User-Agent, the size cap and — the point — the SSRF
	// guard under the draft source's seeded allowedHosts. A theme that
	// extracts URLs on a host it never declared fails here, which is where it
	// is explicable, rather than at download time on a source already added.
	Image stepResult `json:"image"`

	// Challenge is set when the image host answered with a browser challenge.
	// It is terminal (PLAN §7.6) and outranks every other outcome: a site that
	// refuses us is refusing us wherever it does it.
	Challenge *challengeSignal `json:"-"`

	// ImageHost is the host the image came from, so the verdict can say where
	// the challenge was. A bare blocked_challenge after a successful search is
	// baffling; "it serves its pages from images.example, which requires a
	// browser challenge" is actionable.
	ImageHost string `json:"imageHost,omitempty"`
}

// stepResult is one step's outcome. Err is kept for the log; Note is what the
// user may see.
type stepResult struct {
	OK    bool   `json:"ok"`
	Count int    `json:"count,omitempty"`
	Note  string `json:"note,omitempty"`
}

func (c capability) ok() bool {
	return c.Challenge == nil && c.Search.OK && c.Series.OK && c.Chapters.OK && c.Pages.OK && c.Image.OK
}

// addableDegraded is PLAN §7.5's narrow allowance: "offer to add in a degraded
// state only if search and chapters work; if page extraction fails the source is
// useless, so refuse." Series detail is the only step that may be missing.
func (c capability) addableDegraded() bool {
	return !c.ok() && c.Challenge == nil &&
		c.Search.OK && c.Chapters.OK && c.Pages.OK && c.Image.OK
}

// failure names the first failing step, in plain language and as a sentence
// fragment that reads after "but".
func (c capability) failure() string {
	switch {
	case !c.Search.OK:
		return "searching it didn't work: " + c.Search.Note
	case !c.Chapters.OK:
		return "it couldn't list any chapters: " + c.Chapters.Note
	case !c.Pages.OK:
		return "it couldn't find the page images in a chapter: " + c.Pages.Note
	case !c.Image.OK:
		return "it found the page images but couldn't fetch one: " + c.Image.Note
	case !c.Series.OK:
		return "it couldn't read a series' details: " + c.Series.Note
	}
	return ""
}

// summary is the progress line shown while the stage finishes.
func (c capability) summary() string {
	if c.ok() {
		return fmt.Sprintf("Found %d series, %d chapters and %d page images, and fetched one.",
			c.Search.Count, c.Chapters.Count, c.Pages.Count)
	}
	return strings.ToUpper(c.failure()[:1]) + c.failure()[1:]
}

// stageCapability exercises the whole path. Every step is attempted even after
// an earlier one fails where that is still meaningful, so the report can say
// what does work rather than only where it stopped.
func (r *run) stageCapability(ctx context.Context, th theme.Theme, src *theme.Source) capability {
	var cap capability

	stubs, err := th.Search(ctx, src, capabilityQuery, 1)
	switch {
	case err != nil:
		cap.Search.Note = plainError(err)
	case len(stubs) == 0:
		cap.Search.Note = "the site returned no results at all."
	default:
		cap.Search.OK = true
		cap.Search.Count = len(stubs)
	}
	if !cap.Search.OK {
		return cap
	}

	// The first result with a usable ID; a theme may legitimately return a stub
	// it could not fully parse, and picking it would test our patience rather
	// than the site.
	var stub theme.SeriesStub
	for _, s := range stubs {
		if strings.TrimSpace(s.ID) != "" && strings.TrimSpace(s.Title) != "" {
			stub = s
			break
		}
	}
	if stub.ID == "" {
		cap.Search.OK = false
		cap.Search.Note = "the results had no titles Quire could follow."
		return cap
	}

	series, err := th.Series(ctx, src, stub.ID)
	switch {
	case err != nil:
		cap.Series.Note = plainError(err)
	case series == nil || strings.TrimSpace(series.Title) == "":
		cap.Series.Note = "the series page had no title."
	default:
		cap.Series.OK = true
	}

	chapters, err := th.Chapters(ctx, src, stub.ID)
	switch {
	case err != nil:
		cap.Chapters.Note = plainError(err)
	case len(chapters) == 0:
		cap.Chapters.Note = "the series listed no chapters."
	default:
		cap.Chapters.OK = true
		cap.Chapters.Count = len(chapters)
	}
	if !cap.Chapters.OK {
		return cap
	}

	// The *newest* chapter, which is the last one now that PLAN §7.2 has
	// Chapters return ascending reading order (2026-09-15).
	//
	// Deliberate, not incidental. A site that has changed its reader markup
	// keeps the old chapters exactly as they were, so probing the earliest
	// chapter can report a capability the user will not actually have on
	// anything they read. The most recent chapter is the one that reflects
	// what the site does today, which is what stage 5 is asking about.
	newest := chapters[len(chapters)-1]
	pages, err := th.Pages(ctx, src, newest.ID)
	switch {
	case err != nil:
		cap.Pages.Note = plainError(err)
	case len(pages) == 0:
		cap.Pages.Note = "the chapter contained no images."
	default:
		good := 0
		for _, p := range pages {
			if plausibleImageURL(p) {
				good++
			}
		}
		if good == 0 {
			cap.Pages.Note = "the chapter's image addresses were not usable."
		} else {
			cap.Pages.OK = true
			cap.Pages.Count = good
		}
	}
	if !cap.Pages.OK {
		return cap
	}

	// One image, not a chapter. Proportionate — stage 5 already costs several
	// requests and the device is on a battery — and enough to answer the only
	// question extraction leaves open: will the bytes actually arrive?
	first := ""
	for _, p := range pages {
		if plausibleImageURL(p) {
			first = p
			break
		}
	}
	cap.Image, cap.Challenge, cap.ImageHost = r.fetchOnePageImage(ctx, th, src, newest.ID, first)
	return cap
}

// fetchOnePageImage performs stage 5's image fetch and classifies the answer.
//
// Three outcomes, and keeping them apart is the whole point:
//
//   - a browser challenge — terminal, PLAN §7.6, and the verdict says which
//     host did it, because a challenge reported after a successful search is
//     otherwise baffling;
//   - any other refusal or a body that is not an image — a failing step, which
//     stage 6 turns into `partial` naming page fetching. Not a challenge: a
//     403 with no markers is a site saying no to *this request*, and dressing
//     it up as a challenge would assert something we did not observe;
//   - success — the bytes arrived and look like an image.
//
// The request is classified as **discovery**. PLAN §7.4's worked table puts
// "the probe's crawl" there without qualification: the user asked to add a
// site, not for this image, and the probe is automated from the moment it
// starts.
func (r *run) fetchOnePageImage(ctx context.Context, th theme.Theme, src *theme.Source, chapterID, rawurl string) (stepResult, *challengeSignal, string) {
	var step stepResult
	if rawurl == "" {
		step.Note = "the chapter's image addresses were not usable."
		return step, nil, ""
	}
	host := ""
	if u, err := url.Parse(rawurl); err == nil {
		host = u.Hostname()
	}

	pol, err := src.Policy()
	if err != nil {
		step.Note = plainError(err)
		return step, nil, host
	}

	// The Referer of the chapter page this image address came out of, when the
	// theme has one to name (PLAN §7.6). Three sites answer 403 on the image
	// host without it and 200 with it, so a probe that omitted it would report
	// "page fetching failed" for a source that works perfectly.
	//
	// It is *this* chapter's page — the one whose markup was just parsed — and
	// never a stand-in: a theme that cannot say yields the zero Referrer and
	// the request carries no header at all, which is the honest answer.
	from, err := theme.PageRefererFor(th, src, chapterID)
	if err != nil {
		// A theme naming a page address Quire cannot use is a theme bug. Say
		// so in the step note rather than inventing a substitute.
		step.Note = plainError(err)
		return step, nil, host
	}

	resp, err := r.p.fetch.GetFrom(ctx, pol, rawurl, from)
	if err != nil {
		// The SSRF guard lives here, and its refusal is reported as itself.
		// A theme extracting images from a host it never declared in
		// AllowedHosts is a theme bug, and naming the host is what makes it
		// one someone can fix — guessing "challenge" instead would send the
		// reader looking for a CAPTCHA that does not exist.
		step.Note = plainError(err)
		return step, nil, host
	}

	page := probe.NewPage(nil, nil, resp.StatusCode, resp.Header, resp.Body)
	if sig, found := r.challengeSignalFor(page, false); found {
		return step, &sig, host
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Not a bare code: a status the user can act on (PLAN §6 M3). The
		// challenge check above has already run, so this is a plain refusal.
		step.Note = fetch.StatusSentence("the image host", resp.StatusCode) + "."
		return step, nil, host
	}
	if !looksLikeImage(resp.Header.Get("Content-Type"), resp.Body) {
		step.Note = "the image address returned a page, not an image."
		return step, nil, host
	}

	step.OK = true
	step.Count = len(resp.Body)
	return step, nil, host
}

// looksLikeImage trusts the declared type when there is one and sniffs when
// there is not. Sniffing is the fallback rather than the rule because a host
// that labels its own bytes is telling us something, and http.DetectContentType
// only reads the first 512.
func looksLikeImage(contentType string, body []byte) bool {
	if len(body) == 0 {
		return false
	}
	if ct := strings.ToLower(strings.TrimSpace(contentType)); ct != "" {
		if strings.HasPrefix(ct, "image/") {
			return true
		}
		// A declared non-image type is an answer, not an absence of one — but
		// a generic octet-stream says nothing, so that one is sniffed.
		if !strings.HasPrefix(ct, "application/octet-stream") {
			return false
		}
	}
	return strings.HasPrefix(http.DetectContentType(body), "image/")
}

// plausibleImageURL is a cheap sanity check on a page URL. It is not a fetch:
// stage 5 already costs several requests, and a theme that returns a relative
// fragment or a base64 placeholder — the most common real failure — is caught
// without another one.
func plausibleImageURL(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "data:") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

// plainError turns a fetch-layer error into something a person can read. It
// deliberately does not leak Go error wrapping into the UI (PLAN §6 M3: plain
// language, not error codes), but keeps enough to be actionable.
// A theme's error for a refusal reduces to "HTTP 444" once the wrapping is
// gone, which is exactly the bare code PLAN §6 M3 forbids, so a trailing
// status is expanded into something the reader can act on.
func plainError(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i > 0 && i < len(msg)-2 {
		msg = msg[i+2:]
	}
	if msg == "" {
		return "the site didn't answer as expected."
	}
	return fetch.ExplainStatus(msg, "the site") + "."
}
