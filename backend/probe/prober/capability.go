package prober

import (
	"context"
	"errors"
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
//
// CORRECTION 2026-09-20 — the five steps are the *page-based* shape, and a
// theme.FileTheme has no page images at all: its "chapters" are finished epubs
// and its Pages() returns an error by design. Running the check above against
// one would report "it couldn't find the page images in a chapter" for a source
// that works perfectly, which is a false *refusal* — the mirror image of the
// mistake the 2026-09-16 correction fixed, and just as wrong.
//
// So a file theme is verified by what it actually does: search → book →
// releases → a release that can be asked for, plus theme.Confirmer's strong
// check where the theme has one. See fileSteps for what that deliberately does
// *not* claim, and why stage 5 does not start a real retrieval.
type capability struct {
	Search   stepResult `json:"search"`
	Series   stepResult `json:"series"`
	Chapters stepResult `json:"chapters"`
	Pages    stepResult `json:"pages"`

	// Confirm is theme.Confirmer's answer: a request to an endpoint only this
	// theme's application serves, judged by the theme. It runs first and ends
	// the stage when it fails — a site that is not the application cannot be
	// searched as one, and asking it to anyway produces a second, less
	// informative failure on top of the real one.
	//
	// A theme that implements no such check passes here. That is not a hole:
	// it is a theme making no claim beyond what the other steps exercise, and
	// the steps below still have to hold.
	Confirm stepResult `json:"confirm"`

	// Release is the file-theme counterpart of Pages: one chapter of the book
	// that names a release Quire could go and ask for.
	Release stepResult `json:"release,omitempty"`

	// fileBased records which shape was checked, so ok(), failure() and summary()
	// do not have to re-derive it from an empty step and get it wrong. It is not
	// serialised: it describes the check, not the site.
	fileBased bool

	// strongConfirmed records whether theme.Confirmer's check actually *ran and
	// passed* — not merely whether Confirm.OK is true, which is also true when
	// the theme has no such check at all (see confirmTheme). Confirm.OK keeps
	// meaning "nothing here refuses the site" for ok(), addableDegraded() and
	// failure(); strongConfirmed is the narrower fact those cannot answer —
	// "a request only the real application could have answered came back
	// right" — which is what addableEmptyReleases and the empty-search
	// allowance below both actually need: positive evidence strong enough to
	// excuse a step that found nothing, not merely the absence of a failure.
	strongConfirmed bool

	// fileEmpty records that every book candidate stageCapability tried (up to
	// three) came back with a genuinely empty release list — no error, just
	// nothing to download — rather than that fetching one of them failed. See
	// addableEmptyReleases: this is what turns a false refusal into an honest
	// "these particular books had nothing".
	fileEmpty bool

	// searchEmpty records that capabilitySearch reached the site — the
	// fallback query ran and answered with no error — and simply found
	// nothing, rather than that the request itself failed. See
	// addableEmptySearch: this is what stops a metadata provider's own empty
	// window (measured against a live Shelfmark instance, 2026-09-20) from
	// reading as "the source doesn't work" for an instance the strong check
	// already confirmed is the real application.
	searchEmpty bool

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

// pagesOK is the "can we get pages?" half of the check, and it is what a file
// theme has no version of. Keeping it in one function is what stops the file
// branch from being bolted onto every boolean below one `&&` at a time.
func (c capability) pagesOK() bool {
	if c.fileBased {
		return c.Release.OK
	}
	return c.Pages.OK && c.Image.OK
}

func (c capability) ok() bool {
	return c.Challenge == nil && c.Confirm.OK && c.Search.OK && c.Series.OK && c.Chapters.OK && c.pagesOK()
}

// addableDegraded is PLAN §7.5's narrow allowance: "offer to add in a degraded
// state only if search and chapters work; if page extraction fails the source is
// useless, so refuse." Series detail is the only step that may be missing.
//
// The strong check is not degradable. A theme that has one and failed it is not
// a weaker version of this source; it is a different site, and adding it would
// be the false `ok` §7.5 is most emphatic about.
func (c capability) addableDegraded() bool {
	return !c.ok() && c.Challenge == nil && c.Confirm.OK &&
		c.Search.OK && c.Chapters.OK && c.pagesOK()
}

// addableEmptyReleases is the file-theme counterpart Part B (2026-09-20)
// adds, for a case addableDegraded does not and must not cover: every book
// candidate tried came back with nothing to download, and nothing failed.
//
// Release availability for a book service is a property of the book and of
// which indexers the user has configured on their own instance — narrowed
// further by Quire's own epub/pdf-only filter (see shelfmark's release
// filtering) — not a property of the instance being broken. Trying up to
// three books before giving up (stageCapability) already rules out "the
// first search hit happened to be obscure"; three-for-three empty is still
// routine for a self-hosted instance with few or no indexers configured, and
// it is a fact about those books, not about whether the source works.
//
// It requires the strong check to have actually run and passed —
// strongConfirmed, not merely Confirm.OK, which is also true when the theme
// has no such check at all and so has offered no positive evidence to weigh
// against three empty books — plus fileBased and the recorded fileEmpty, and
// is deliberately its own function rather than a relaxation of
// addableDegraded: that one is PLAN §7.5's specific allowance for a
// page-based source missing only page extraction, and loosening it to also
// mean this would make it stop saying what its name says.
func (c capability) addableEmptyReleases() bool {
	return c.fileBased && c.fileEmpty && c.Challenge == nil && c.strongConfirmed && c.Search.OK
}

// addableEmptySearch is stage 5's other confirmed-but-empty case (2026-09-20):
// the strong check ran and passed, so the site is positively identified as the
// application, but the search itself — the listing and the fallback query —
// reached the site and came back with nothing, no error. Measured against a
// live Shelfmark instance: its metadata provider intermittently answers every
// query with zero results for a window and then recovers, which is a fact
// about that provider's own upstream, not about whether Quire can talk to the
// instance.
//
// Gated on strongConfirmed, not Confirm.OK, for the same reason
// addableEmptyReleases is: a theme with no strong check that finds nothing
// has offered no positive evidence at all, and must still be refused — an
// unconfirmed site that searches empty could just as easily be broken.
//
// searchEmpty is deliberately a recorded fact rather than a check on
// cap.Search.Note's text — capabilitySearch's own note collapses a fetch
// error and a reached-but-empty result into similar-looking strings, and
// matching on that text would risk treating a real failure as the confirmed,
// merely-quiet source this exists for.
func (c capability) addableEmptySearch() bool {
	return c.strongConfirmed && c.searchEmpty && c.Challenge == nil && !c.Search.OK
}

// failure names the first failing step, in plain language and as a sentence
// fragment that reads after "but".
func (c capability) failure() string {
	switch {
	case !c.Confirm.OK:
		return "it isn't the application Quire took it for: " + c.Confirm.Note
	case !c.Search.OK:
		return "searching it didn't work: " + c.Search.Note
	case !c.Chapters.OK:
		if c.fileBased {
			return "it couldn't list anything to download for a book: " + c.Chapters.Note
		}
		return "it couldn't list any chapters: " + c.Chapters.Note
	case c.fileBased && !c.Release.OK:
		return "it listed nothing Quire could actually ask it to fetch: " + c.Release.Note
	case !c.fileBased && !c.Pages.OK:
		return "it couldn't find the page images in a chapter: " + c.Pages.Note
	case !c.fileBased && !c.Image.OK:
		return "it found the page images but couldn't fetch one: " + c.Image.Note
	case !c.Series.OK:
		return "it couldn't read a series' details: " + c.Series.Note
	}
	return ""
}

// uselessWithout is the clause stage 6 ends a refusal with: what this kind of
// source would have to do to be worth adding, in the words of what it is.
//
// "A source Quire can't read pages from" is exactly right for a comic site and
// simply untrue of a book service, which has no pages to read.
func (c capability) uselessWithout() string {
	if c.fileBased {
		return "A source Quire can't download a book from is no use, so nothing was added."
	}
	return "A source Quire can't read pages from is no use, so nothing was added."
}

// summary is the progress line shown while the stage finishes.
func (c capability) summary() string {
	if c.ok() {
		if c.fileBased {
			return fmt.Sprintf("Found %d books and %d things to download for one of them.",
				c.Search.Count, c.Chapters.Count)
		}
		return fmt.Sprintf("Found %d series, %d chapters and %d page images, and fetched one.",
			c.Search.Count, c.Chapters.Count, c.Pages.Count)
	}
	return strings.ToUpper(c.failure()[:1]) + c.failure()[1:]
}

// capabilitySearch gets stage 5 a handful of series to work with, and returns
// an empty note when it managed to. PLAN §7.5 stage 5: "a search (or the
// popular/latest listing if search needs a query)".
//
// The listing goes first, and that order is the point (corrected 2026-09-16).
// Probing with a one-character query made Quire generate the very failure it
// then reported: comick.art drops any query under three characters at the edge,
// so "a" came back as a bare 444 and a site that works perfectly was written
// off. The listing is the request a user makes by opening Browse, so it is both
// the gentlest thing to ask for and the most representative.
//
// The text query is the fallback for a site whose listing is genuinely empty —
// a search-only front page — and it is never shorter than capabilityQuery,
// because short queries are the shape edges treat as abuse.
//
// The query itself is capabilityQuery by default, but a theme that knows its
// own backend better may say so (theme.ProbeQuerier) — see shelfmark.Theme's
// implementation for why "one" is the wrong word to ask a book service.
// The bool return is structural, not derived from note: true exactly when the
// fallback query reached the site and answered with zero results and no
// error, false for every other outcome (including success). Callers that need
// to tell "reached the site, found nothing" apart from "failed to reach the
// site" — addableEmptySearch — must use this rather than matching note text,
// which reads similarly for both.
func (r *run) capabilitySearch(ctx context.Context, th theme.Theme, src *theme.Source) ([]theme.SeriesStub, string, bool) {
	stubs, listErr := th.Search(ctx, src, capabilityListing, 1)
	if listErr == nil && len(stubs) > 0 {
		return stubs, "", false
	}

	query := capabilityQuery
	if pq, ok := th.(theme.ProbeQuerier); ok {
		query = pq.ProbeQuery()
	}

	// Nothing from the listing — either it is empty or the theme needs a query
	// to search at all. One more request, with a word rather than a letter.
	stubs, err := th.Search(ctx, src, query, 1)
	switch {
	case err != nil:
		// The fallback itself failed to reach the site: this is the more
		// informative of the two errors, because it is the one that actually
		// ran a search rather than the listing the theme may legitimately
		// refuse.
		return nil, plainError(err), false
	case len(stubs) == 0:
		// The fallback reached the site and found nothing — whether or not
		// the listing errored first. That matters: a theme may legitimately
		// require a query and 400 on an empty one (correct behaviour, not a
		// fault), and blaming the site for that 400 here would be reporting
		// a refusal that never happened as the reason the probe failed. The
		// only honest statement is the one about the request that actually
		// ran: it reached the site, and found nothing.
		return nil, "the site returned no results at all.", true
	}
	return stubs, "", false
}

// stageCapability exercises the whole path. Every step is attempted even after
// an earlier one fails where that is still meaningful, so the report can say
// what does work rather than only where it stopped.
//
// It returns an error only when asking the user went wrong (a cancelled
// context, a UI that went away) — the same meaning an error carries from every
// other stage. A "no" answer to a question raised in here is not an error; it
// is recorded on cap and read back through failure() like any other refusal.
func (r *run) stageCapability(ctx context.Context, th theme.Theme, src *theme.Source) (capability, error) {
	var cap capability
	_, cap.fileBased = th.(theme.FileTheme)

	// The strong check first, where the theme has one. A self-hosted
	// application's markup is a near-empty shell, so the fingerprint that got us
	// here is weak evidence on its own; this is the request only the real
	// application can answer. Everything below asks the site to behave like the
	// theme, which is a question worth nothing until this one says yes.
	cap.Confirm, cap.strongConfirmed = r.confirmTheme(ctx, th, src)
	if !cap.Confirm.OK {
		return cap, nil
	}

	stubs, note, searchEmpty := r.capabilitySearch(ctx, th, src)
	if note != "" {
		cap.Search.Note = note
		cap.searchEmpty = searchEmpty
		return cap, nil
	}
	cap.Search.OK = true
	cap.Search.Count = len(stubs)

	// The results with a usable ID and title; a theme may legitimately return a
	// stub it could not fully parse, and picking one of those would test our
	// patience rather than the site. Up to three are kept — see the file-based
	// branch below for why more than one candidate matters there.
	var candidates []theme.SeriesStub
	for _, s := range stubs {
		if strings.TrimSpace(s.ID) != "" && strings.TrimSpace(s.Title) != "" {
			candidates = append(candidates, s)
			if len(candidates) == 3 {
				break
			}
		}
	}
	if len(candidates) == 0 {
		cap.Search.OK = false
		cap.Search.Note = "the results had no titles Quire could follow."
		return cap, nil
	}

	if cap.fileBased {
		// A page-based site's first search hit is representative of the whole
		// catalogue; a book service's is not. Whether one particular book has
		// a downloadable release depends on that book and on which indexers
		// the user's own instance is configured to search — an empty result
		// for the first hit says nothing about the second. So up to three
		// candidates are tried, stopping at the first with something to
		// download, before the source is judged on what remains: routinely
		// empty across all three tried, not obviously broken.
		allEmptyNoError := true
		loopStart := r.p.now()
		for i, s := range candidates {
			// The first candidate is always tried in full. Before each subsequent
			// candidate, check the wall-clock budget: if it has elapsed, stop
			// trying further candidates and fall through to the existing
			// "none of the candidates had anything" handling.
			if i > 0 && r.p.now().Sub(loopStart) >= capabilityFileBudget {
				break
			}

			series, err := th.Series(ctx, src, s.ID)
			switch {
			case err != nil:
				cap.Series = stepResult{Note: plainError(err)}
			case series == nil || strings.TrimSpace(series.Title) == "":
				cap.Series = stepResult{Note: "the series page had no title."}
			default:
				cap.Series = stepResult{OK: true}
			}

			chapters, err := th.Chapters(ctx, src, s.ID)
			switch {
			case err != nil:
				cap.Chapters = stepResult{Note: plainError(err)}
				allEmptyNoError = false
			case len(chapters) == 0:
				cap.Chapters = stepResult{Note: "the series listed no chapters."}
			default:
				cap.Chapters = stepResult{OK: true, Count: len(chapters)}
			}

			if cap.Chapters.OK {
				cap.Release = fileSteps(chapters)
				return cap, nil
			}
		}
		// None of the candidates tried had anything. fileEmpty is only set
		// when every attempt was a genuine, error-free empty list — a real
		// fetch failure along the way is a different problem and stays a
		// plain refusal (see addableEmptyReleases).
		cap.fileEmpty = allEmptyNoError
		return cap, nil
	}

	// Page-based themes keep the single-candidate behaviour stage 5 has always
	// had: the first search hit is representative of the site, unlike a book
	// service's.
	stub := candidates[0]
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
		return cap, nil
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
		return cap, nil
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
	cap.Image, cap.Challenge, cap.ImageHost, err = r.fetchOnePageImage(ctx, th, src, newest.ID, first)
	return cap, err
}

// confirmTheme runs theme.Confirmer's strong check, or passes when the theme
// has none.
//
// The request is the theme's own — shelfmark GETs its /api/config and runs
// CheckConfig over the body — and it goes through the same guarded client
// everything else does, because the theme is built over it. It is classified as
// discovery for the same reason the rest of the probe is (PLAN §7.4): the user
// asked to add a site, not for this endpoint.
//
// **A theme with no check passes, and that is not a hole.** Eight of the nine
// themes drive families of independently hosted sites with no endpoint that
// could identify them; for those the fingerprint plus the four behavioural steps
// *is* the evidence, and inventing a failing step for "did not answer a question
// nobody asked" would refuse every source Quire can already read.
// The bool return is strongConfirmed: true exactly when a theme.Confirmer
// existed and its check passed, false both when the theme has none (Confirm.OK
// is still true there — see the doc above) and when the check ran but failed.
// Callers that need "positive evidence the site is the application", rather
// than merely "nothing here refuses the site", must use this, not Confirm.OK.
func (r *run) confirmTheme(ctx context.Context, th theme.Theme, src *theme.Source) (stepResult, bool) {
	c, ok := th.(theme.Confirmer)
	if !ok {
		return stepResult{OK: true}, false
	}
	if err := c.Confirm(ctx, src); err != nil {
		// The theme's own sentence, trimmed the way every other step's is: it
		// names which keys were missing, which is the actionable part.
		return stepResult{Note: plainError(err)}, false
	}
	return stepResult{OK: true}, true
}

// fileSteps is stage 5's last step for a theme.FileTheme: is there something
// here Quire could actually go and ask for?
//
// # What this proves, and what it deliberately does not
//
// A page theme's last step fetches one image, because extraction succeeding and
// the bytes arriving turned out to be different questions (see the 2026-09-16
// correction above). The equivalent here would be to run FileTheme.Retrieve —
// and stage 5 must not, for reasons that are about the user rather than about
// cost:
//
//   - Retrieve has a *side effect on someone else's machine*. It asks the
//     instance to go and download a book from a third-party source into the
//     owner's library. Pasting a URL into the "add a source" box must not put a
//     file in somebody's library, and a probe that did it once per attempt is
//     not something a user could take back.
//   - It can legitimately take minutes — the instance's own release search
//     timeout is 300s per source and it tries several (measured 2026-09-20) —
//     against a wizard that is showing "step 5 of 6".
//
// So the claim made here is the narrower one the evidence supports: the
// instance is the application (Confirm proved that against its own config), it
// found this book, and it listed at least one release, in a format the device
// opens, that names a source Quire can ask it to fetch. What is *not* proved is
// that the transfer will succeed — and that is stated plainly rather than
// papered over, because the alternative is a false `ok`, which the comment at
// the top of this file is right that we would rather refuse a good site than
// produce.
func fileSteps(chapters []theme.Chapter) stepResult {
	good := 0
	for _, c := range chapters {
		if strings.TrimSpace(c.ID) != "" && strings.TrimSpace(c.Title) != "" {
			good++
		}
	}
	if good == 0 {
		return stepResult{Note: "the things it offered had no titles or addresses Quire could follow."}
	}
	return stepResult{OK: true, Count: good}
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
//
// One refusal is a question rather than a plain failure (added 2026-09-21):
// the image host is off the source's registrable domain and was not already
// declared — fetch.GuardError.OffDomain, set nowhere else. Most Madara sites
// serve pages from a separate CDN and this was measured against one,
// toonily.com: the fingerprint, search and series steps all pass and stage 5
// then failed at the last step with no way for the user to say the CDN is
// expected. See offerImageHost for the scope of what a "yes" buys.
func (r *run) fetchOnePageImage(ctx context.Context, th theme.Theme, src *theme.Source, chapterID, rawurl string) (stepResult, *challengeSignal, string, error) {
	var step stepResult
	if rawurl == "" {
		step.Note = "the chapter's image addresses were not usable."
		return step, nil, "", nil
	}
	host := ""
	if u, err := url.Parse(rawurl); err == nil {
		host = u.Hostname()
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
		return step, nil, host, nil
	}

	// Tried up to twice: once as declared, and once more if the only reason it
	// failed was the off-domain question above and the user said yes. asked
	// stops a "yes" from being asked for twice were the second attempt to fail
	// the same way, which fetch.Guard's re-run of CheckURL cannot do once the
	// host is on src.AllowedHosts, but a defensive stop is one line and cheap.
	asked := false
	for {
		pol, err := src.Policy()
		if err != nil {
			step.Note = plainError(err)
			return step, nil, host, nil
		}

		resp, err := r.p.fetch.GetFrom(ctx, pol, rawurl, from)
		if err != nil {
			var ge *fetch.GuardError
			if !asked && errors.As(err, &ge) && ge.OffDomain {
				asked = true
				granted, askErr := r.offerImageHost(ctx, host)
				if askErr != nil {
					return step, nil, host, askErr
				}
				if granted {
					// Scoped to this one host, on this one source: appended to
					// the draft's own AllowedHosts (theme.Source.Policy reads
					// it fresh above), never widened to the CDN's registrable
					// domain and never touched by any other source's guard.
					// Lower-cased because that is the only form
					// schema/source.schema.json's allowedHosts pattern
					// accepts, and hostMatches already compares case-folded.
					src.AllowedHosts = append(src.AllowedHosts, strings.ToLower(host))
					continue
				}
			}
			// The SSRF guard lives here, and its refusal is reported as
			// itself — including a decline of the question just asked, which
			// leaves this exact refusal standing rather than a generic
			// "stopped" (see offerImageHost). A theme extracting images from
			// a host it never declared in AllowedHosts is a theme bug, and
			// naming the host is what makes it one someone can fix —
			// guessing "challenge" instead would send the reader looking for
			// a CAPTCHA that does not exist.
			step.Note = plainError(err)
			return step, nil, host, nil
		}

		page := probe.NewPage(nil, nil, resp.StatusCode, resp.Header, resp.Body)
		if sig, found := r.challengeSignalFor(page, false); found {
			return step, &sig, host, nil
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			// Not a bare code: a status the user can act on (PLAN §6 M3). The
			// challenge check above has already run, so this is a plain refusal.
			step.Note = fetch.StatusSentence("the image host", resp.StatusCode) + "."
			return step, nil, host, nil
		}
		if !looksLikeImage(resp.Header.Get("Content-Type"), resp.Body) {
			step.Note = "the image address returned a page, not an image."
			return step, nil, host, nil
		}

		step.OK = true
		step.Count = len(resp.Body)
		return step, nil, host, nil
	}
}

// offerImageHost is stage 5's other question (PLAN §7.5, added 2026-09-21):
// a page image refused for exactly one reason, fetch.GuardError.OffDomain —
// its host is a perfectly ordinary address, just outside the source's
// registrable domain and not already in its declared allowedHosts.
//
// **A "yes" here is scoped to this one host, on this one source, and nothing
// wider.** The caller appends the exact host string to the draft's own
// AllowedHosts — not "*.host" and not the CDN's own registrable domain, so it
// buys only what schema/source.schema.json documents an exact entry as
// buying, matched by fetch's hostMatches. It is never read by any other
// source's policy, and it never reaches checkAddress: the guard still refuses
// a private, loopback, link-local or reserved address however the host that
// carries it was named, because that check runs unconditionally after this
// one and does not consult allowedHosts at all.
//
// A "no" leaves the refusal already in force standing, reported as itself —
// the same choice offerSelfHosted makes for the same reason: nothing has been
// granted, so nothing should be said to have happened beyond the refusal that
// was always going to be reported.
func (r *run) offerImageHost(ctx context.Context, host string) (bool, error) {
	ans, err := r.ui.Ask(ctx, Question{
		Kind: "imagehost",
		Text: fmt.Sprintf("This site keeps its page images on a different address, %s, rather than its own. "+
			"That's normal for sites that use a separate image server. Let Quire fetch images from %s for this source?",
			host, host),
		Options: []Option{
			{ID: "cancel", Label: "No, stop"},
			{ID: "continue", Label: "Yes, allow it"},
		},
	})
	if err != nil {
		return false, err
	}
	return ans.ID == "continue", nil
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
