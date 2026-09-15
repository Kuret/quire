// Package probe carries the one thing the source probe (PLAN §7.5) and the
// theme engine (§7.2) both need: a fetched page that every theme can inspect.
//
// It exists as its own package purely to keep the dependency graph acyclic —
// theme.Theme.Fingerprint takes a *probe.Page, and the probe itself (M3) will
// take a theme.Registry. The rest of the probe's stages land in M3.
package probe

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

// Page is one fetched HTML page, ready to be scored by every registered theme.
//
// The parsed document is built once, lazily, and shared: PLAN §7.5 stage 4
// asks *every* theme to score the same page, and parsing a few hundred KiB of
// HTML once per theme is exactly the kind of waste the device cannot afford.
type Page struct {
	// URL is what we asked for; FinalURL is where we ended up after redirects.
	URL      *url.URL
	FinalURL *url.URL

	Status int
	Header http.Header
	Body   []byte

	once sync.Once
	doc  *goquery.Document
	err  error

	strippedOnce sync.Once
	body         string
}

// NewPage builds a Page. FinalURL defaults to URL when no redirect happened.
func NewPage(u, final *url.URL, status int, header http.Header, body []byte) *Page {
	if final == nil {
		final = u
	}
	if header == nil {
		header = http.Header{}
	}
	return &Page{URL: u, FinalURL: final, Status: status, Header: header, Body: body}
}

// Document returns the parsed page. The error is memoised alongside the
// document, so a page that fails to parse fails identically for every theme.
func (p *Page) Document() (*goquery.Document, error) {
	p.once.Do(func() {
		p.doc, p.err = goquery.NewDocumentFromReader(strings.NewReader(string(p.Body)))
	})
	return p.doc, p.err
}

// HTML is the raw body as a string. Fingerprints use it for the signals that
// live in inline script rather than in the DOM — which, for both tier-1 theme
// families, is where the most distinctive signals are.
func (p *Page) HTML() string { return string(p.Body) }

// commentRE matches an HTML comment, including an unterminated one at EOF.
var commentRE = regexp.MustCompile(`(?s)<!--.*?(-->|$)`)

// Contains is a case-insensitive substring test over the body with HTML
// comments removed.
//
// Removing comments is not tidiness. A fingerprint signal found in a comment
// is not evidence: sites carry commented-out markup from the theme they used
// to run, build tools leave whole blocks of dead HTML behind, and a page that
// merely *mentions* another theme would otherwise be assigned to it. The probe
// must be able to say "unrecognised" (PLAN §7.5), and it cannot do that if
// dead text can score.
func (p *Page) Contains(needle string) bool {
	return strings.Contains(strings.ToLower(p.stripped()), strings.ToLower(needle))
}

// stripped is the body with comments removed, computed once.
func (p *Page) stripped() string {
	p.strippedOnce.Do(func() {
		p.body = commentRE.ReplaceAllString(string(p.Body), "")
	})
	return p.body
}

// Has reports whether the parsed document matches a CSS selector. A parse
// failure is reported as "no match" rather than propagated: a fingerprint is a
// score, not an assertion, and an unparseable page simply scores zero.
func (p *Page) Has(selector string) bool {
	doc, err := p.Document()
	if err != nil {
		return false
	}
	return doc.Find(selector).Length() > 0
}

// Title is the page's <title>, trimmed. PLAN §7.5 stage 6 defaults a source's
// display name from it.
func (p *Page) Title() string {
	doc, err := p.Document()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Find("title").First().Text())
}
