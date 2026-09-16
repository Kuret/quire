package service_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.6, decided 2026-09-16: a page image may carry a Referer naming the
// page its address was extracted from, and may carry nothing else. These tests
// are the download half of that — the probe half is in backend/probe/prober.
//
// The bug worth pinning is not "no header": it is a header naming the *wrong*
// page. One queue fetches a whole volume, so the tempting implementation gives
// every page of a forty-chapter volume the first chapter's Referer, which is a
// statement about a page that image did not come from.

// volumeChapters are the four chapters of the volume the M3 fixtures describe.
// A download of any one of them fetches all four (PLAN §6 M4), which is what
// makes "its own chapter's Referer" a question with a wrong answer available.
var volumeChapters = []string{"chapter-1", "chapter-3", "chapter-3-5", "chapter-4"}

// perChapterRoutes gives every chapter its own reader page and its own image
// paths, so an image request can be traced back to exactly one chapter.
func perChapterRoutes(t *testing.T) map[string]themetest.Route {
	t.Helper()
	reader, err := os.ReadFile("testdata/reader.html")
	if err != nil {
		t.Fatal(err)
	}

	r := routes()
	page := jpegPage(t)
	jpg := themetest.Route{Body: page, Header: http.Header{"Content-Type": []string{"image/jpeg"}}}

	for _, slug := range volumeChapters {
		body := strings.ReplaceAll(string(reader),
			"pages/lantern-keeper/4/", "pages/lantern-keeper/"+slug+"/")
		r["GET /manga/the-lantern-keeper/"+slug+"/"] = themetest.Route{Body: body}
		for _, n := range []string{"001", "002", "003", "004"} {
			r["GET /pages/lantern-keeper/"+slug+"/"+n+".jpg"] = jpg
		}
		r["GET /wp-content/uploads/pages/lantern-keeper/"+slug+"/005.jpg"] = jpg
	}
	return r
}

// refererTheme is the fixture theme plus PLAN §7.6's side interface, standing
// in for webtoons, fanfox and comick — whose image hosts answer 403 without a
// Referer. It names the chapter page the theme itself reads, per chapter.
//
// It wraps volumeTheme rather than madara directly because the volume labels
// are what make runVolumeDownload's volume download available at all (PLAN §6
// M4, revised 2026-09-16). The Referer is what this file is about.
type refererTheme struct{ volumeTheme }

func (refererTheme) PageReferer(s *theme.Source, chapterID string) string {
	if strings.HasPrefix(chapterID, "http") {
		return chapterID
	}
	return strings.TrimSuffix(s.BaseURL, "/") + "/" + strings.TrimPrefix(chapterID, "/")
}

// imageReferers maps each fetched page image's path to the Referer it carried.
func imageReferers(t *testing.T, f *themetest.Fetcher) map[string]themetest.Request {
	t.Helper()
	out := map[string]themetest.Request{}
	for _, c := range f.Calls() {
		u, err := url.Parse(c.URL)
		if err != nil {
			continue
		}
		if strings.HasSuffix(u.EscapedPath(), ".jpg") {
			out[u.EscapedPath()] = c
		}
	}
	return out
}

// runVolumeDownload downloads the fixture volume with th registered as the
// source's theme, and hands back the fetcher so the requests can be inspected.
func runVolumeDownload(t *testing.T, newTheme func(*themetest.Fetcher) theme.Theme) *themetest.Fetcher {
	t.Helper()

	f := themetest.New(t, perChapterRoutes(t))
	reg := theme.NewRegistry()
	reg.MustRegister(newTheme(f))

	svc, store, _, _, rec := newDownloadServiceWith(t, perChapterRoutes(t), func(o *service.Options) {
		o.Registry = reg
		o.Fetcher = f
	})
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	waitForPhase(t, rec, "done")
	return f
}

func TestDownloadedPagesCarryTheirOwnChaptersReferer(t *testing.T) {
	f := runVolumeDownload(t, func(f *themetest.Fetcher) theme.Theme {
		return refererTheme{volumeTheme{madara.NewWithClock(f, func() time.Time { return fixedNow })}}
	})

	got := imageReferers(t, f)
	if len(got) == 0 {
		t.Fatal("no page images were fetched")
	}
	for _, slug := range volumeChapters {
		want := "https://example.invalid/manga/the-lantern-keeper/" + slug + "/"
		seen := 0
		for path, req := range got {
			if !strings.Contains(path, "/lantern-keeper/"+slug+"/") {
				continue
			}
			seen++
			if req.Referrer.IsZero() {
				t.Errorf("%s was fetched with no Referer, want %q", path, want)
				continue
			}
			if req.Referrer.String() != want {
				t.Errorf("%s carried Referer %q, want its own chapter's %q", path, req.Referrer.String(), want)
			}
		}
		if seen == 0 {
			t.Errorf("no page of %s was fetched", slug)
		}
	}
}

func TestDownloadedPagesCarryNoRefererWhenTheThemeHasNone(t *testing.T) {
	f := runVolumeDownload(t, func(f *themetest.Fetcher) theme.Theme {
		return volumeTheme{madara.NewWithClock(f, func() time.Time { return fixedNow })}
	})

	got := imageReferers(t, f)
	if len(got) == 0 {
		t.Fatal("no page images were fetched")
	}
	// Absence, explicitly: a theme that cannot name the page it read sends no
	// Referer header at all. An empty one would be a different request, and a
	// fabricated one is what PLAN §7.6 forbids.
	for path, req := range got {
		if !req.Referrer.IsZero() {
			t.Errorf("%s carried Referer %q; a theme with no PageReferrer must send none",
				path, req.Referrer.String())
		}
	}
}
