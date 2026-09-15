package prober

import (
	"context"
	"fmt"
	"net/url"
	"strings"

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

// capability is the outcome of each of the four steps.
type capability struct {
	Search   stepResult `json:"search"`
	Series   stepResult `json:"series"`
	Chapters stepResult `json:"chapters"`
	Pages    stepResult `json:"pages"`
}

// stepResult is one step's outcome. Err is kept for the log; Note is what the
// user may see.
type stepResult struct {
	OK    bool   `json:"ok"`
	Count int    `json:"count,omitempty"`
	Note  string `json:"note,omitempty"`
}

func (c capability) ok() bool {
	return c.Search.OK && c.Series.OK && c.Chapters.OK && c.Pages.OK
}

// addableDegraded is PLAN §7.5's narrow allowance: "offer to add in a degraded
// state only if search and chapters work; if page extraction fails the source is
// useless, so refuse." Series detail is the only step that may be missing.
func (c capability) addableDegraded() bool {
	return !c.ok() && c.Search.OK && c.Chapters.OK && c.Pages.OK
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
	case !c.Series.OK:
		return "it couldn't read a series' details: " + c.Series.Note
	}
	return ""
}

// summary is the progress line shown while the stage finishes.
func (c capability) summary() string {
	if c.ok() {
		return fmt.Sprintf("Found %d series, %d chapters and %d page images.",
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

	pages, err := th.Pages(ctx, src, chapters[0].ID)
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
	return cap
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
func plainError(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i > 0 && i < len(msg)-2 {
		msg = msg[i+2:]
	}
	if msg == "" {
		return "the site didn't answer as expected."
	}
	return msg + "."
}
