package prober_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/shelfmark"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.5 stage 5 against a theme.FileTheme — a source whose "chapters" are
// finished files (2026-09-20).
//
// Two things are being pinned here and they pull in opposite directions, which
// is why both are tested:
//
//   - A file theme must not be refused for want of page images. It has none by
//     design; Pages() returns an error rather than an empty slice. Reporting
//     "it couldn't find the page images in a chapter" for a source that works
//     is a false *refusal*.
//   - A file theme must not be accepted on the fingerprint alone. A self-hosted
//     application serves a near-empty shell, so the markup is weak evidence;
//     the strong evidence is an API response only that application produces.
//     Accepting without it is the false `ok` the comment at the top of
//     capability.go is most emphatic about.

// shelfmarkFixture reads one of the shelfmark package's captures.
//
// They are read from where they live rather than copied here on purpose: they
// are one real instance's responses from 2026-09-20, and a second copy in this
// package would be a second thing to keep true.
func shelfmarkFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "theme", "shelfmark", "testdata", name))
	if err != nil {
		t.Fatalf("read shelfmark fixture %s: %v", name, err)
	}
	return string(b)
}

// shelfmarkRoutes is a working instance: the web shell the probe lands on, the
// config that identifies it, and the three API calls stage 5 makes.
func shelfmarkRoutes(t *testing.T) map[string]themetest.Route {
	t.Helper()
	return map[string]themetest.Route{
		"GET /":                    {Body: shelfmarkFixture(t, "index.html")},
		"GET /api/config":          {Body: shelfmarkFixture(t, "config.json")},
		"GET /api/metadata/search": {Body: shelfmarkFixture(t, "metadata-search-dune.json")},
		"GET /api/metadata/book/openlibrary/OL893414W": {Body: shelfmarkFixture(t, "metadata-book-dune.json")},
		"GET /api/releases":                            {Body: shelfmarkFixture(t, "releases-dune.json")},
	}
}

// The happy path: a real instance is recognised, verified and accepted —
// without a single page image, because there are none to have.
func TestStageFiveAcceptsAShelfmarkInstance(t *testing.T) {
	f := themetest.New(t, shelfmarkRoutes(t))
	res := runWithFetcher(t, f, shelfmark.New(f))

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
	if res.Draft == nil || res.Draft.Theme != shelfmark.ID {
		t.Fatalf("draft = %+v, want one bound to the shelfmark theme", res.Draft)
	}
	// The strong check must actually have been made. Without it this verdict
	// rests on a <head> anyone can write.
	if !f.Requested("GET", shelfmark.ConfigPath) {
		t.Error("stage 5 accepted the source without ever asking for /api/config")
	}
	// And the behavioural half: it searched, and it listed something to fetch.
	if !f.Requested("GET", "/api/metadata/search") {
		t.Error("stage 5 never searched the instance")
	}
	if !f.Requested("GET", "/api/releases") {
		t.Error("stage 5 never asked what could be downloaded for a book")
	}
	// The one sentence the user reads. It must not mention pages: this source
	// has none, and saying it found some would be a claim about a check that
	// never ran.
	if strings.Contains(strings.ToLower(res.Detail), "page") {
		t.Errorf("detail %q talks about pages for a source that has none", res.Detail)
	}
}

func TestStageFiveWorksAroundAListingThatRequiresAQuery(t *testing.T) {
	routes := shelfmarkRoutes(t)
	delete(routes, "GET /api/metadata/search")
	routes["GET /api/metadata/search?page=1&query="] = themetest.Route{
		Status: 400,
		Body:   `{"error":"Either 'query' or search field values are required"}`,
	}
	routes["GET /api/metadata/search?page=1&query=one"] = themetest.Route{
		Body: `{"books":[],"has_more":false,"page":1,"total_found":0}`,
	}
	routes["GET /api/metadata/search?page=1&query=dune"] = themetest.Route{
		Body: shelfmarkFixture(t, "metadata-search-dune.json"),
	}

	f := themetest.New(t, routes)
	res := runWithFetcher(t, f, shelfmark.New(f))

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
	if !f.Requested("GET", "/api/metadata/search") {
		t.Error("stage 5 never searched the instance")
	}
}

// The rejection that matters. The markup says Shelfmark — that is stage 4's
// whole evidence, and it is evidence anyone can manufacture — but /api/config
// does not carry the key set, so this is some other server wearing the name.
func TestStageFiveRejectsAnInstanceWhoseConfigIsWrong(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			// The instance's own answer to an unknown path, which arrives with
			// **HTTP 200**. A check that trusted the status would confirm any
			// server at all.
			name: "an unknown path answered 200",
			body: shelfmarkFixture(t, "not-found.json"),
		},
		{
			// A config object that is a config object, and is not this
			// application's: three of the four keys, which is what makes the
			// co-occurrence rule worth having.
			name: "three of the four keys",
			body: `{"supported_formats":["epub"],"release_search_timeout":300,` +
				`"metadata_search_fields":["title"]}`,
		},
		{
			name: "not JSON at all",
			body: `<!doctype html><html><body>Shelfmark</body></html>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := shelfmarkRoutes(t)
			routes["GET /api/config"] = themetest.Route{Body: tc.body}
			f := themetest.New(t, routes)
			res := runWithFetcher(t, f, shelfmark.New(f))

			if res.Addable || res.Draft != nil {
				t.Fatalf("a server that is not a Shelfmark instance was offered for adding: %q (%s)",
					res.Verdict, res.Detail)
			}
			if res.Verdict != theme.VerdictPartial {
				t.Errorf("verdict = %q (%s), want partial", res.Verdict, res.Detail)
			}
			// The failing step has to be named, or the user is told a site they
			// can see working was refused for no stated reason.
			if !strings.Contains(res.Detail, "isn't the application Quire took it for") {
				t.Errorf("detail %q does not say the strong check is what failed", res.Detail)
			}
			// And nothing else was asked of it: a server that is not the
			// application cannot be searched as one, and a second failure on
			// top of the real one only obscures it.
			if f.Requested("GET", "/api/metadata/search") {
				t.Error("stage 5 went on searching a server that had already failed the strong check")
			}
		})
	}
}

// A file theme with nothing to fetch is refused. The mirror of the page check:
// extraction that yields nothing is not a working source, whatever shape the
// source has — and the two ways it can yield nothing are kept apart, because
// "the book has no releases" and "the releases have no address" are different
// failures and only one of them is the site's fault.
func TestStageFiveRefusesAFileThemeWithNothingToFetch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chapters []theme.Chapter
		says     string
	}{
		{
			name:     "the book has no releases at all",
			chapters: nil,
			says:     "couldn't list anything to download",
		},
		{
			// A release the theme could not address: it is in the list and
			// there is no way to ask for it. Accepting this would be the false
			// `ok` — the user adds the source and every download fails.
			name:     "the releases have no address",
			chapters: []theme.Chapter{{ID: "", Title: "EPUB · 1.7MB · Direct Download", Number: -1}},
			says:     "nothing Quire could actually ask it to fetch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runWithTheme(t, map[string]themetest.Route{"GET /": {File: "home-unrecognised.html"}},
				fileStub{id: "books", score: 90, chapters: tc.chapters})

			if res.Addable || res.Draft != nil {
				t.Fatalf("a source offering nothing to download was offered for adding: %s", res.Detail)
			}
			if !strings.Contains(res.Detail, tc.says) {
				t.Errorf("detail %q does not name the failing step", res.Detail)
			}
			// The refusal says what this kind of source would have to do, in
			// its own words. "Pages" would be meaningless to somebody adding a
			// book service.
			if strings.Contains(strings.ToLower(res.Detail), "page") {
				t.Errorf("detail %q refuses a book source for want of page images", res.Detail)
			}
			if !strings.Contains(res.Detail, "download a book") {
				t.Errorf("detail %q does not say what the source could not do", res.Detail)
			}
		})
	}
}

// A theme with no strong check is unaffected: eight of the nine themes drive
// families of independently hosted sites with no endpoint that could identify
// them, and refusing them for not answering a question nobody asked would
// refuse every source Quire can already read.
func TestAThemeWithNoStrongCheckIsStillAccepted(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	res := runWithFetcher(t, f, stubTheme{id: "alpha", score: 90})

	if res.Verdict != theme.VerdictOK || !res.Addable {
		t.Fatalf("verdict = %q (%s), want an addable ok", res.Verdict, res.Detail)
	}
}

// fileStub is a theme.FileTheme with nothing to offer: a book, and no releases
// for it. It stands in for an instance whose sources were all unreachable.
type fileStub struct {
	id    string
	score int

	// chapters, when set, are the releases the book has.
	chapters []theme.Chapter
}

func (f fileStub) ID() string                             { return f.id }
func (f fileStub) Fingerprint(*probe.Page) int            { return f.score }
func (f fileStub) AllowedHosts() []string                 { return nil }
func (f fileStub) SuggestedName() string                  { return "" }
func (f fileStub) ValidateOverrides(map[string]any) error { return nil }
func (f fileStub) OverrideKeys() []theme.OverrideDoc      { return nil }

func (f fileStub) Search(context.Context, *theme.Source, string, int) ([]theme.SeriesStub, error) {
	return []theme.SeriesStub{{ID: "/book/one", Title: "One"}}, nil
}

func (f fileStub) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return &theme.Series{ID: "/book/one", Title: "One"}, nil
}

func (f fileStub) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return f.chapters, nil
}

// Pages errors rather than returning nothing, exactly as a real file theme
// does: an empty slice would read as "this chapter has no pages", which is a
// different and false claim.
func (f fileStub) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return nil, errors.New("books: this theme has no page images")
}

func (f fileStub) Retrieve(context.Context, *theme.Source, string, func(string)) (string, string, error) {
	return "", "", errors.New("books: nothing to retrieve")
}
